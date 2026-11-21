package node

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"maps"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

const (
	// exportSpec ist der erste Eintrag eines Umzugs-Stroms, exportMarker der letzte. Fehlt der Vermerk, ist der Strom
	// abgebrochen und der Import wird verworfen.
	exportSpec   = "spec.json"
	exportMarker = ".lodestone-export-complete"
	exportData   = "data"
	// importReserve muss nach einem Import oder einer Kopie auf der Maschine frei bleiben.
	importReserve     = int64(512) << 20
	importMaxEntries  = 5_000_000
	importMaxBytes    = int64(4) << 40
	importSpecMaxSize = 1 << 20
	// SizeHeader trägt beim Import die erwartete Größe in Bytes (für die Prüfung des freien Platzes).
	SizeHeader = "X-Lodestone-Size"
)

// CloneRequest legt eine Kopie eines Servers auf demselben Node an.
type CloneRequest struct {
	// ID ist die ID der Kopie (vergibt das Panel).
	ID   string `json:"id"`
	Name string `json:"name"`
	// WithWorlds kopiert auch die Welten (Ordner mit level.dat). Ohne entsteht die Welt beim ersten Start neu.
	WithWorlds bool `json:"withWorlds"`
}

// ---------------------------------------------------------------- Anlegen vorbereiten

// beginCreate reserviert eine ID für Klonen oder Import. Die Rückgabe gibt sie wieder frei.
func (m *Manager) beginCreate(id string) (func(), error) {
	if !idPattern.MatchString(id) {
		return nil, badRequest("ungültige Server-ID")
	}
	if _, exists := m.Get(id); exists {
		return nil, fmt.Errorf("%w: die ID %q gibt es auf diesem Node schon", ErrConflict, id)
	}
	m.ops.mu.Lock()
	defer m.ops.mu.Unlock()
	m.ops.initLocked()
	if m.ops.reserved[id] {
		return nil, fmt.Errorf("%w: der Server %q wird gerade angelegt", ErrConflict, id)
	}
	m.ops.reserved[id] = true
	return func() {
		m.ops.mu.Lock()
		delete(m.ops.reserved, id)
		m.ops.mu.Unlock()
	}, nil
}

// checkFree lehnt ab, wenn für needed Bytes (plus Reserve) kein Platz ist. Unbekannte Werte gelten als genug.
func (m *Manager) checkFree(needed int64) error {
	free, _ := diskSpace(m.cfg.DataDir)
	if needed <= 0 || free <= 0 || free >= needed+importReserve {
		return nil
	}
	return fmt.Errorf("%w: Auf dem Node ist zu wenig Platz frei (frei %s, nötig %s). Lösche alte Backups oder Server",
		ErrConflict, formatBytes(free), formatBytes(needed+importReserve))
}

// freshSpec macht aus einer vorhandenen Beschreibung die eines neuen Servers: neue Host-Ports, eigene Kopien von
// Listen und Maps. installed/java bleiben, weil die Server-Datei mitkommt.
func freshSpec(spec Spec, id, name string) (Spec, error) {
	spec.ID = id
	if name != "" {
		spec.Name = name
	}
	ports := make([]PortMapping, len(spec.Ports))
	for i, port := range spec.Ports {
		ports[i] = PortMapping{Container: port.Container, Protocol: port.Protocol}
	}
	spec.Ports = ports
	spec.Env = maps.Clone(spec.Env)
	if spec.Autoconfig != nil {
		config := *spec.Autoconfig
		spec.Autoconfig = &config
	}
	installed, java := spec.Installed, spec.JavaMajor
	if err := spec.Normalize(); err != nil {
		return Spec{}, badRequest("%v", err)
	}
	spec.Installed, spec.JavaMajor = installed, java
	return spec, nil
}

// adopt schreibt spec.json in den fertigen Hilfsordner, schiebt ihn an seinen Platz und übernimmt den Server.
// Die Ports müssen schon vergeben sein. Bei einem Fehler wird alles entfernt.
func (m *Manager) adopt(staging string, spec Spec) (*Server, error) {
	undo := func(err error) (*Server, error) {
		_ = os.RemoveAll(staging)
		m.setPorts(spec.ID, nil)
		return nil, err
	}
	data, err := json.MarshalIndent(spec, "", "  ")
	if err != nil {
		return undo(err)
	}
	if err := os.WriteFile(filepath.Join(staging, "spec.json"), data, 0o600); err != nil {
		return undo(err)
	}
	dir := filepath.Join(m.serversDir(), spec.ID)
	if _, err := os.Stat(dir); err == nil {
		return undo(fmt.Errorf("%w: der Ordner für %q gibt es auf dem Node schon", ErrConflict, spec.ID))
	}
	if err := renameDir(staging, dir); err != nil {
		return undo(err)
	}
	server, err := newServer(m, dir, spec)
	if err != nil {
		_ = os.RemoveAll(dir)
		m.setPorts(spec.ID, nil)
		return nil, err
	}
	m.mu.Lock()
	m.servers[spec.ID] = server
	m.mu.Unlock()
	go m.measure(context.Background(), server)
	return server, nil
}

// renameDir benennt einen Ordner um. Unter Windows halten Virenscanner frisch geschriebene Dateien kurz offen, dann
// schlägt das Umbenennen mit „Zugriff verweigert“ fehl; deshalb wird es einige Male wiederholt.
func renameDir(from, to string) error {
	var err error
	for attempt := 0; attempt < 20; attempt++ {
		if err = os.Rename(from, to); err == nil || !errors.Is(err, fs.ErrPermission) {
			return err
		}
		time.Sleep(50 * time.Millisecond)
	}
	return err
}

// ---------------------------------------------------------------- Klonen

// Clone kopiert einen Server auf diesem Node: Dateien (wahlweise ohne Welten), Einstellungen und Server-Datei.
// Die Kopie bekommt neue Host-Ports und keine Autoconfig (die setzt das Panel neu). Ein laufender Server wird wie
// beim Backup kurz angehalten, auf die Platte zu schreiben. Bricht ctx ab, bleibt nichts zurück.
func (m *Manager) Clone(ctx context.Context, source *Server, request CloneRequest) (*Server, error) {
	spec, err := freshSpec(source.snapshot(), request.ID, strings.TrimSpace(request.Name))
	if err != nil {
		return nil, err
	}
	spec.Autoconfig, spec.AutoconfigApplied = nil, 0

	source.mu.Lock()
	state := source.state
	source.mu.Unlock()
	if state == StateInstalling || state == StateRestoring {
		return nil, fmt.Errorf("%w: Der Server wird gerade installiert oder wiederhergestellt", ErrBadState)
	}
	if !source.backupBusy.CompareAndSwap(false, true) {
		return nil, errBusy
	}
	defer source.backupBusy.Store(false)

	release, err := m.beginCreate(spec.ID)
	if err != nil {
		return nil, err
	}
	defer release()
	usage, _ := m.usageOf(source.ID())
	if err := m.checkFree(usage.Bytes); err != nil {
		return nil, err
	}
	if err := m.allocatePorts(&spec, spec.ID); err != nil {
		return nil, err
	}
	staging := filepath.Join(m.serversDir(), ".clone-"+spec.ID)
	_ = os.RemoveAll(staging)
	target, err := OpenFS(filepath.Join(staging, "data"))
	if err != nil {
		m.setPorts(spec.ID, nil)
		return nil, err
	}
	quiet := source.quiesce()
	source.logf("Der Server wird als %q kopiert", spec.Name)
	err = copyTree(ctx, source.fs, target, request.WithWorlds)
	if quiet {
		_ = source.Command("save-on")
	}
	err = errors.Join(err, target.Close())
	if err != nil {
		_ = os.RemoveAll(staging)
		m.setPorts(spec.ID, nil)
		source.logf("Kopieren fehlgeschlagen: %v", err)
		return nil, err
	}
	server, err := m.adopt(staging, spec)
	if err == nil {
		server.logf("Kopie von %q angelegt", source.snapshot().Name)
	}
	return server, err
}

// worldDirs sind die Ordner der obersten Ebene, die eine Minecraft-Welt enthalten (level.dat).
func worldDirs(files *FS) map[string]bool {
	worlds := map[string]bool{}
	entries, err := files.List(".")
	if err != nil {
		return worlds
	}
	for _, entry := range entries {
		if entry.Dir && !entry.Symlink && files.Exists(path.Join(entry.Name, "level.dat")) {
			worlds[entry.Name] = true
		}
	}
	return worlds
}

// transient sind Hilfsdateien des Daemons, die nie mitkopiert werden.
func transient(name string) bool {
	return strings.HasPrefix(name, restoreTemp) || strings.HasSuffix(name, ".lodestone-part")
}

// copyTree kopiert den Inhalt von src nach dst. Symlinks und Sonderdateien werden übersprungen, nichts wird
// überschrieben.
func copyTree(ctx context.Context, src, dst *FS, withWorlds bool) error {
	skip := map[string]bool{}
	if !withWorlds {
		skip = worldDirs(src)
	}
	return fs.WalkDir(src.root.FS(), ".", func(name string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if name == "." {
			return nil
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		top, _, _ := strings.Cut(name, "/")
		if skip[top] || transient(name) {
			if entry.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		if entry.Type()&fs.ModeSymlink != 0 {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return dst.root.MkdirAll(name, 0o755)
		}
		if !info.Mode().IsRegular() {
			return nil
		}
		return copyFile(ctx, src, dst, name, info)
	})
}

func copyFile(ctx context.Context, src, dst *FS, name string, info fs.FileInfo) error {
	in, err := src.root.Open(name)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := dst.root.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_EXCL, info.Mode().Perm()|0o600)
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(out, ctxReader{ctx, in})
	if err := errors.Join(copyErr, out.Close()); err != nil {
		return err
	}
	return dst.root.Chtimes(name, info.ModTime(), info.ModTime())
}

// ctxReader bricht das Lesen ab, sobald der Kontext endet.
type ctxReader struct {
	ctx context.Context
	r   io.Reader
}

func (c ctxReader) Read(p []byte) (int, error) {
	if err := c.ctx.Err(); err != nil {
		return 0, err
	}
	return c.r.Read(p)
}

// ---------------------------------------------------------------- Export

// beginExport sperrt einen ausgeschalteten Server für die Dauer des Exports: kein Start, kein Backup, keine
// Wiederherstellung. Die Rückgabe hebt die Sperre auf.
func (m *Manager) beginExport(s *Server) (func(), error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.state != StateOffline {
		return nil, fmt.Errorf("%w: Der Server muss für den Umzug ausgeschaltet sein", ErrBadState)
	}
	if !s.backupBusy.CompareAndSwap(false, true) {
		return nil, errBusy
	}
	m.ops.mu.Lock()
	m.ops.initLocked()
	m.ops.busy[s.spec.ID] = "Der Server zieht gerade auf einen anderen Node um"
	m.ops.mu.Unlock()
	return func() {
		m.ops.mu.Lock()
		delete(m.ops.busy, s.spec.ID)
		m.ops.mu.Unlock()
		s.backupBusy.Store(false)
	}, nil
}

// writeExport schreibt den Server als tar.gz: zuerst spec.json, dann den Serverordner unter data/, zuletzt den
// Abschlussvermerk. Symlinks und Hilfsdateien bleiben weg.
func (s *Server) writeExport(ctx context.Context, w io.Writer) error {
	spec, err := json.Marshal(s.snapshot())
	if err != nil {
		return err
	}
	gz, err := gzip.NewWriterLevel(w, gzip.BestSpeed)
	if err != nil {
		return err
	}
	archive := tar.NewWriter(gz)
	now := time.Now()
	if err := archive.WriteHeader(&tar.Header{Name: exportSpec, Mode: 0o600, Size: int64(len(spec)), ModTime: now, Typeflag: tar.TypeReg}); err != nil {
		return err
	}
	if _, err := archive.Write(spec); err != nil {
		return err
	}
	walkErr := fs.WalkDir(s.fs.root.FS(), ".", func(name string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if name != "." && transient(name) {
			if entry.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		if entry.Type()&fs.ModeSymlink != 0 {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if !entry.IsDir() && !info.Mode().IsRegular() {
			return nil
		}
		header, err := tar.FileInfoHeader(info, "")
		if err != nil {
			return err
		}
		header.Name = path.Join(exportData, name)
		header.Uid, header.Gid, header.Uname, header.Gname = 0, 0, "", ""
		if entry.IsDir() {
			header.Name += "/"
			return archive.WriteHeader(header)
		}
		file, err := s.fs.root.Open(name)
		if err != nil {
			return err
		}
		defer file.Close()
		if err := archive.WriteHeader(header); err != nil {
			return err
		}
		_, err = io.Copy(archive, io.LimitReader(file, header.Size))
		return err
	})
	if walkErr != nil {
		return walkErr
	}
	if err := archive.WriteHeader(&tar.Header{Name: exportMarker, Mode: 0o600, ModTime: now, Typeflag: tar.TypeReg}); err != nil {
		return err
	}
	return errors.Join(archive.Close(), gz.Close())
}

// ---------------------------------------------------------------- Import

// Import legt einen Server aus einem Umzugs-Strom (siehe writeExport) an. Er behält ID, Einstellungen und Dateien,
// bekommt aber neue Host-Ports. Es wird erst in einen Hilfsordner entpackt; ist der Strom unvollständig, ungültig
// oder bricht ctx ab, bleibt nichts zurück. expected ist die erwartete Größe (0 = unbekannt).
func (m *Manager) Import(ctx context.Context, body io.Reader, expected int64) (*Server, error) {
	if err := m.checkFree(expected); err != nil {
		return nil, err
	}
	gz, err := gzip.NewReader(ctxReader{ctx, body})
	if err != nil {
		return nil, badRequest("kein gültiger Umzugs-Strom: %v", err)
	}
	defer gz.Close()
	archive := tar.NewReader(gz)
	header, err := archive.Next()
	if err != nil || header.Name != exportSpec || header.Size > importSpecMaxSize {
		return nil, badRequest("dem Umzugs-Strom fehlt die Beschreibung des Servers")
	}
	raw, err := io.ReadAll(io.LimitReader(archive, importSpecMaxSize))
	if err != nil {
		return nil, err
	}
	var source Spec
	if err := json.Unmarshal(raw, &source); err != nil {
		return nil, badRequest("die Beschreibung des Servers ist ungültig: %v", err)
	}
	spec, err := freshSpec(source, source.ID, "")
	if err != nil {
		return nil, err
	}
	release, err := m.beginCreate(spec.ID)
	if err != nil {
		return nil, err
	}
	defer release()
	if err := m.allocatePorts(&spec, spec.ID); err != nil {
		return nil, err
	}
	staging := filepath.Join(m.serversDir(), ".import-"+spec.ID)
	_ = os.RemoveAll(staging)
	files, err := OpenFS(filepath.Join(staging, "data"))
	if err != nil {
		m.setPorts(spec.ID, nil)
		return nil, err
	}
	err = unpackExport(archive, files, expected)
	if err == nil {
		// Erst der Rest des gzip-Stroms prüft die Prüfsumme
		_, err = io.Copy(io.Discard, gz)
	}
	err = errors.Join(err, files.Close())
	if err != nil {
		_ = os.RemoveAll(staging)
		m.setPorts(spec.ID, nil)
		return nil, err
	}
	server, err := m.adopt(staging, spec)
	if err == nil {
		server.logf("Der Server ist auf diesen Node umgezogen")
	}
	return server, err
}

// unpackExport entpackt die Einträge unter data/ nach files. Pfade nach außen und Symlinks werden verworfen, der
// Abschlussvermerk muss der letzte Eintrag sein.
func unpackExport(archive *tar.Reader, files *FS, expected int64) error {
	budget := importMaxBytes
	if expected > 0 {
		budget = min(budget, expected*2+(1<<30))
	}
	entries, complete := 0, false
	for {
		header, err := archive.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return badRequest("der Umzugs-Strom ist beschädigt: %v", err)
		}
		if complete {
			return badRequest("der Umzugs-Strom enthält Daten nach dem Abschlussvermerk")
		}
		if entries++; entries > importMaxEntries {
			return badRequest("der Umzugs-Strom enthält zu viele Dateien")
		}
		name, ok := archiveName(header.Name)
		if !ok {
			return badRequest("der Umzugs-Strom enthält einen ungültigen Pfad: %q", header.Name)
		}
		if name == exportMarker {
			complete = true
			continue
		}
		rel, inside := strings.CutPrefix(name, exportData+"/")
		if name == exportData {
			continue
		}
		if !inside {
			return badRequest("der Umzugs-Strom enthält einen unbekannten Eintrag: %q", header.Name)
		}
		switch header.Typeflag {
		case tar.TypeDir:
			if err := files.root.MkdirAll(rel, 0o755); err != nil {
				return translate(err)
			}
		case tar.TypeReg:
			if dir := path.Dir(rel); dir != "." {
				if err := files.root.MkdirAll(dir, 0o755); err != nil {
					return translate(err)
				}
			}
			out, err := files.root.OpenFile(rel, os.O_WRONLY|os.O_CREATE|os.O_EXCL, header.FileInfo().Mode().Perm()&0o755|0o600)
			if err != nil {
				return translate(err)
			}
			written, copyErr := io.Copy(out, io.LimitReader(archive, budget+1))
			budget -= written
			if err := errors.Join(copyErr, out.Close()); err != nil {
				return err
			}
			if budget < 0 {
				return badRequest("der Umzugs-Strom ist größer als angekündigt")
			}
			_ = files.root.Chtimes(rel, header.ModTime, header.ModTime)
		}
	}
	if !complete {
		return badRequest("die Übertragung ist unvollständig (der Abschlussvermerk fehlt)")
	}
	return nil
}

// ---------------------------------------------------------------- Schnittstelle

func (a *API) registerServerOpsRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/storage", a.nodeStorage)
	mux.HandleFunc("GET /api/servers/{id}/storage", a.server(a.serverStorage))
	mux.HandleFunc("PUT /api/servers/{id}/storage", a.server(a.putStorageLimit))
	mux.HandleFunc("POST /api/servers/{id}/storage/measure", a.server(a.measureStorage))
	mux.HandleFunc("POST /api/servers/{id}/clone", a.server(a.cloneServer))
	mux.HandleFunc("GET /api/servers/{id}/export", a.server(a.exportServer))
	mux.HandleFunc("POST /api/servers/import", a.importServer)
}

func (a *API) cloneServer(w http.ResponseWriter, r *http.Request, source *Server) {
	var request CloneRequest
	if err := decode(r, &request); err != nil {
		writeError(w, err)
		return
	}
	server, err := a.manager.Clone(r.Context(), source, request)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, server.View())
}

// exportServer streamt den Server zum Panel. Geht unterwegs etwas schief, wird die Verbindung hart abgebrochen,
// damit das Ziel einen halben Strom nie für vollständig hält.
func (a *API) exportServer(w http.ResponseWriter, r *http.Request, server *Server) {
	release, err := a.manager.beginExport(server)
	if err != nil {
		writeError(w, err)
		return
	}
	defer release()
	server.logf("Der Server wird für den Umzug übertragen")
	w.Header().Set("Content-Type", "application/gzip")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusOK)
	if err := server.writeExport(r.Context(), w); err != nil {
		server.logf("Übertragung abgebrochen: %v", err)
		panic(http.ErrAbortHandler)
	}
}

func (a *API) importServer(w http.ResponseWriter, r *http.Request) {
	expected, _ := strconv.ParseInt(r.Header.Get(SizeHeader), 10, 64)
	server, err := a.manager.Import(r.Context(), r.Body, expected)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, server.View())
}
