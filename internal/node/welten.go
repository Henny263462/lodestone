package node

import (
	"archive/zip"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"mime"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

// Welten, server.properties und Datapacks eines Servers. Alles läuft über das FS des Servers (os.Root); Welt- und
// Dateinamen werden zusätzlich streng geprüft, bevor sie in einen Pfad wandern.

const (
	propertiesFile     = "server.properties"
	maxPropertySets    = 256
	maxPropertyValue   = 4096
	defaultLevelName   = "world"
	maxWorldSizeWalked = 2_000_000 // Einträge, die beim Messen einer Welt höchstens gezählt werden
)

// Hilfsdateien beim Hochladen einer Welt. Sie beginnen mit restoreTemp, damit Backups sie auslassen.
var (
	worldUploadZip   = restoreTemp + "-welt.zip"
	worldUploadDir   = restoreTemp + "-welt"
	worldPreviousDir = restoreTemp + "-welt-alt"
)

var (
	worldNamePattern    = regexp.MustCompile(`^[A-Za-z0-9_][A-Za-z0-9 _.+-]{0,63}$`)
	propertyKeyPattern  = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,99}$`)
	datapackNamePattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9 _.+()\[\]-]{0,127}$`)
	errWorldRunning     = fmt.Errorf("%w: Stoppe den Server, bevor du die Welt änderst", ErrBadState)
)

// notFound ist ein „gibt es nicht“ mit deutscher Meldung (die Schnittstelle macht daraus 404).
type notFound string

func (e notFound) Error() string        { return string(e) }
func (e notFound) Is(target error) bool { return target == fs.ErrNotExist }

// alreadyThere ist ein „gibt es schon“ mit deutscher Meldung (die Schnittstelle macht daraus 409).
type alreadyThere string

func (e alreadyThere) Error() string        { return string(e) }
func (e alreadyThere) Is(target error) bool { return target == fs.ErrExist }

// World ist eine Welt im Serverordner (ein Ordner mit level.dat).
type World struct {
	Name string `json:"name"`
	// Kind: overworld, nether, end oder other.
	Kind string `json:"kind"`
	// Main ist die Welt aus level-name, die der Server lädt.
	Main      bool  `json:"main"`
	Size      int64 `json:"size"`
	Modified  int64 `json:"modified"`
	Datapacks int   `json:"datapacks"`
}

// WorldList ist die Antwort der Weltenliste.
type WorldList struct {
	LevelName string  `json:"levelName"`
	Seed      string  `json:"seed"`
	Worlds    []World `json:"worlds"`
}

// Datapack ist eine Datei oder ein Ordner in <welt>/datapacks.
type Datapack struct {
	Name     string `json:"name"`
	Dir      bool   `json:"dir"`
	Size     int64  `json:"size"`
	Modified int64  `json:"modified"`
}

// PropertiesView ist der Inhalt der server.properties.
type PropertiesView struct {
	Exists  bool       `json:"exists"`
	Entries []Property `json:"entries"`
}

// ---------------------------------------------------------------- server.properties

func (s *Server) readProperties() (*PropertiesFile, bool, error) {
	data, err := s.fs.Read(propertiesFile, maxConfigSize)
	if errors.Is(err, fs.ErrNotExist) {
		return ParseProperties(nil), false, nil
	}
	if err != nil {
		return nil, false, err
	}
	return ParseProperties(data), true, nil
}

// Properties liefert die server.properties. Gibt es sie noch nicht (der Server lief nie), ist die Liste leer.
func (s *Server) Properties() (PropertiesView, error) {
	file, exists, err := s.readProperties()
	if err != nil {
		return PropertiesView{}, err
	}
	return PropertiesView{Exists: exists, Entries: file.Entries()}, nil
}

// SetProperties setzt Werte in der server.properties. Kommentare, Reihenfolge und alle anderen Schlüssel bleiben,
// wie sie sind. Fehlt die Datei, wird sie angelegt (der Server ergänzt beim Start die übrigen Werte).
func (s *Server) SetProperties(values map[string]string) (PropertiesView, error) {
	if len(values) == 0 {
		return PropertiesView{}, badRequest("keine Werte angegeben")
	}
	if len(values) > maxPropertySets {
		return PropertiesView{}, badRequest("zu viele Werte auf einmal")
	}
	for key, value := range values {
		if !propertyKeyPattern.MatchString(key) {
			return PropertiesView{}, badRequest("ungültiger Schlüssel %q", key)
		}
		if len(value) > maxPropertyValue || strings.ContainsRune(value, 0) {
			return PropertiesView{}, badRequest("der Wert von %s ist zu lang oder enthält ungültige Zeichen", key)
		}
	}
	// Dieselbe Sperre wie die Autoconfig, damit sich zwei Schreiber nicht gegenseitig überschreiben
	s.applyMu.Lock()
	defer s.applyMu.Unlock()
	file, _, err := s.readProperties()
	if err != nil {
		return PropertiesView{}, err
	}
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys) // neue Schlüssel landen in fester Reihenfolge am Ende
	changed := false
	for _, key := range keys {
		changed = file.Set(key, values[key]) || changed
	}
	if changed {
		if err := writeFile(s.fs, propertiesFile, string(file.Bytes())); err != nil {
			return PropertiesView{}, err
		}
		s.logf("server.properties geändert: %s", strings.Join(keys, ", "))
	}
	return PropertiesView{Exists: true, Entries: file.Entries()}, nil
}

func (s *Server) levelName() (string, string) {
	file, _, err := s.readProperties()
	if err != nil {
		return defaultLevelName, ""
	}
	seed, _ := file.Get("level-seed")
	if name, ok := file.Get("level-name"); ok && strings.TrimSpace(name) != "" {
		return strings.TrimSpace(name), seed
	}
	return defaultLevelName, seed
}

// ---------------------------------------------------------------- Welten

// checkWorldName prüft einen Weltnamen des Benutzers. Er ist immer genau ein Ordner direkt im Serverordner.
func checkWorldName(name string) error {
	if !worldNamePattern.MatchString(name) || strings.HasPrefix(name, restoreTemp) {
		return badRequest("ungültiger Weltname %q (erlaubt sind Buchstaben, Ziffern, Leerzeichen und _ . + -)", name)
	}
	return nil
}

func (s *Server) isWorld(name string) bool {
	dir, err := s.fs.root.Lstat(name)
	if err != nil || !dir.IsDir() {
		return false
	}
	level, err := s.fs.root.Lstat(path.Join(name, "level.dat"))
	return err == nil && level.Mode().IsRegular()
}

func (s *Server) requireWorld(name string) error {
	if err := checkWorldName(name); err != nil {
		return err
	}
	if !s.isWorld(name) {
		return notFound("die Welt „" + name + "“ gibt es nicht")
	}
	return nil
}

// Worlds listet die Welten: jeder Ordner direkt im Serverordner, der eine level.dat hat.
func (s *Server) Worlds() (WorldList, error) {
	level, seed := s.levelName()
	list := WorldList{LevelName: level, Seed: seed, Worlds: []World{}}
	entries, err := s.fs.List(".")
	if err != nil {
		return list, err
	}
	for _, entry := range entries {
		if !entry.Dir || entry.Symlink || !worldNamePattern.MatchString(entry.Name) || !s.isWorld(entry.Name) {
			continue
		}
		world := World{Name: entry.Name, Kind: "other", Modified: entry.Modified}
		switch entry.Name {
		case level:
			world.Kind, world.Main = "overworld", true
		case level + "_nether":
			world.Kind = "nether"
		case level + "_the_end":
			world.Kind = "end"
		}
		world.Size = s.treeSize(entry.Name)
		if packs, err := s.Datapacks(entry.Name); err == nil {
			world.Datapacks = len(packs)
		}
		list.Worlds = append(list.Worlds, world)
	}
	order := map[string]int{"overworld": 0, "nether": 1, "end": 2, "other": 3}
	sort.SliceStable(list.Worlds, func(i, j int) bool {
		a, b := list.Worlds[i], list.Worlds[j]
		if order[a.Kind] != order[b.Kind] {
			return order[a.Kind] < order[b.Kind]
		}
		return strings.ToLower(a.Name) < strings.ToLower(b.Name)
	})
	return list, nil
}

// treeSize zählt die Größe aller Dateien eines Ordners (Symlinks nicht).
func (s *Server) treeSize(dir string) int64 {
	var total int64
	count := 0
	_ = fs.WalkDir(s.fs.root.FS(), dir, func(_ string, entry fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		count++
		if count > maxWorldSizeWalked {
			return fs.SkipAll
		}
		if entry.Type().IsRegular() {
			if info, err := entry.Info(); err == nil {
				total += info.Size()
			}
		}
		return nil
	})
	return total
}

// beginWorldOp sperrt den Server für eine Weltänderung: Er muss aus sein und bleibt so lange im Zustand
// „restoring“, damit ihn niemand währenddessen startet.
func (s *Server) beginWorldOp() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.state != StateOffline {
		return errWorldRunning
	}
	if s.modpackBusy.Load() {
		return fmt.Errorf("%w: Das Modpack wird noch installiert", ErrBadState)
	}
	if !s.backupBusy.CompareAndSwap(false, true) {
		return errBusy
	}
	s.setStateLocked(StateRestoring, "")
	return nil
}

func (s *Server) endWorldOp(problem string) {
	s.backupBusy.Store(false)
	s.setState(StateOffline, problem)
}

// backupNow erstellt ein Backup sofort und wartet darauf. Der Aufrufer hält backupBusy (über beginWorldOp).
func (s *Server) backupNow(label string) (string, error) {
	label = strings.Trim(labelClean.ReplaceAllString(strings.ToLower(label), "-"), "-")
	if len(label) > 40 {
		label = strings.Trim(label[:40], "-")
	}
	name := time.Now().UTC().Format("20060102-150405") + ".tar.gz"
	if label != "" {
		name = strings.TrimSuffix(name, ".tar.gz") + "-" + label + ".tar.gz"
	}
	if err := os.MkdirAll(s.backupDir(), 0o755); err != nil {
		return "", err
	}
	part := filepath.Join(s.backupDir(), name+".part")
	if err := s.writeArchive(part); err != nil {
		_ = os.Remove(part)
		return "", fmt.Errorf("Backup fehlgeschlagen: %w", err)
	}
	if err := os.Rename(part, filepath.Join(s.backupDir(), name)); err != nil {
		_ = os.Remove(part)
		return "", err
	}
	s.logf("Backup %s fertig", name)
	s.pruneBackups()
	return name, nil
}

// WorldReset sagt, wie eine Welt zurückgesetzt wird.
type WorldReset struct {
	// Backup erstellt vorher ein Backup des ganzen Serverordners.
	Backup bool `json:"backup"`
	// Seed setzt level-seed neu (leer = zufällig). nil lässt den Seed, wie er ist.
	Seed *string `json:"seed"`
	// Dimensions setzt bei der Hauptwelt auch <name>_nether und <name>_the_end zurück.
	Dimensions bool `json:"dimensions"`
}

// ResetWorld löscht eine Welt, damit der Server beim nächsten Start eine neue erzeugt. Das Löschen (und das Backup
// davor) läuft im Hintergrund; der Server steht so lange auf „restoring“.
func (s *Server) ResetWorld(name string, reset WorldReset) error {
	if err := s.requireWorld(name); err != nil {
		return err
	}
	if reset.Seed != nil && (len(*reset.Seed) > 128 || strings.ContainsAny(*reset.Seed, "\r\n\x00")) {
		return badRequest("der Seed darf höchstens 128 Zeichen haben und keine Zeilenumbrüche")
	}
	targets := []string{name}
	level, _ := s.levelName()
	if reset.Dimensions && name == level {
		for _, extra := range []string{level + "_nether", level + "_the_end"} {
			if s.isWorld(extra) {
				targets = append(targets, extra)
			}
		}
	}
	if err := s.beginWorldOp(); err != nil {
		return err
	}
	if reset.Seed != nil {
		if _, err := s.SetProperties(map[string]string{"level-seed": strings.TrimSpace(*reset.Seed)}); err != nil {
			s.endWorldOp("")
			return err
		}
	}
	go func() {
		if reset.Backup {
			s.logf("Backup vor dem Zurücksetzen von %s wird erstellt", strings.Join(targets, ", "))
			if _, err := s.backupNow("vor-reset-" + name); err != nil {
				s.logf("Die Welt wurde nicht zurückgesetzt, weil das Backup fehlgeschlagen ist: %v", err)
				s.endWorldOp("Zurücksetzen abgebrochen: " + err.Error())
				return
			}
		}
		for _, target := range targets {
			if err := s.fs.Remove(target, true); err != nil {
				s.logf("Welt %s ließ sich nicht löschen: %v", target, err)
				s.endWorldOp("Zurücksetzen fehlgeschlagen: " + err.Error())
				return
			}
			s.logf("Welt %s gelöscht, sie entsteht beim nächsten Start neu", target)
		}
		s.endWorldOp("")
	}()
	return nil
}

// WriteWorldZip schreibt eine Welt als Zip; der Weltordner liegt darin als oberster Ordner. Symlinks werden übersprungen.
func (s *Server) WriteWorldZip(w io.Writer, name string) error {
	if err := s.requireWorld(name); err != nil {
		return err
	}
	archive := zip.NewWriter(w)
	walkErr := fs.WalkDir(s.fs.root.FS(), name, func(rel string, entry fs.DirEntry, err error) error {
		if err != nil {
			return nil // unlesbares überspringen
		}
		info, err := entry.Info()
		if err != nil || info.Mode()&fs.ModeSymlink != 0 || (!info.IsDir() && !info.Mode().IsRegular()) {
			return nil
		}
		header, err := zip.FileInfoHeader(info)
		if err != nil {
			return nil
		}
		header.Name = rel
		if info.IsDir() {
			header.Name += "/"
			_, err := archive.CreateHeader(header)
			return err
		}
		if path.Base(rel) == "session.lock" {
			return nil // gehört zum laufenden Server, nicht zur Welt
		}
		header.Method = zip.Deflate
		out, err := archive.CreateHeader(header)
		if err != nil {
			return err
		}
		return s.fs.copyTo(out, rel)
	})
	return errors.Join(walkErr, archive.Close())
}

// ReplaceWorld legt eine Welt aus einem Zip an oder ersetzt eine vorhandene. Das Zip wird erst vollständig und sicher
// entpackt (keine Symlinks, keine Pfade nach außen, Größenlimit) und geprüft, bevor die alte Welt weicht.
func (s *Server) ReplaceWorld(name string, upload io.Reader, limit int64, backup bool) error {
	if err := checkWorldName(name); err != nil {
		return err
	}
	if s.fs.Exists(name) && !s.isWorld(name) {
		return alreadyThere("den Ordner „" + name + "“ gibt es schon und er ist keine Welt; er wird nicht überschrieben")
	}
	if err := s.beginWorldOp(); err != nil {
		return err
	}
	root := s.fs.root
	cleanup := func() {
		_ = root.RemoveAll(worldUploadZip)
		_ = root.RemoveAll(worldUploadDir)
	}
	cleanup()
	problem := ""
	defer func() {
		cleanup()
		s.endWorldOp(problem)
	}()
	fail := func(err error) error {
		problem = "Welt nicht übernommen: " + err.Error()
		s.logf("Welt %s nicht übernommen: %v", name, err)
		return err
	}

	if err := s.fs.Write(worldUploadZip, upload, limit, true); err != nil {
		return fail(err)
	}
	if err := s.fs.Extract(worldUploadZip, worldUploadDir); err != nil {
		return fail(badRequest("%v", err))
	}
	source, err := s.findLevelRoot(worldUploadDir)
	if err != nil {
		return fail(err)
	}
	if backup {
		if _, err := s.backupNow("vor-upload-" + name); err != nil {
			return fail(err)
		}
	}
	replaced := false
	if s.fs.Exists(name) {
		_ = root.RemoveAll(worldPreviousDir)
		if err := root.Rename(name, worldPreviousDir); err != nil {
			return fail(translate(err))
		}
		replaced = true
	}
	if err := root.Rename(source, name); err != nil {
		if replaced {
			_ = root.Rename(worldPreviousDir, name) // alte Welt zurück an ihren Platz
		}
		return fail(translate(err))
	}
	if replaced {
		_ = root.RemoveAll(worldPreviousDir)
	}
	_ = root.Remove(path.Join(name, "session.lock"))
	s.logf("Welt %s aus einem Zip übernommen", name)
	return nil
}

// findLevelRoot sucht im entpackten Zip den Ordner mit level.dat: direkt darin oder in genau einem Unterordner.
func (s *Server) findLevelRoot(dir string) (string, error) {
	if info, err := s.fs.root.Lstat(path.Join(dir, "level.dat")); err == nil && info.Mode().IsRegular() {
		return dir, nil
	}
	entries, err := s.fs.List(dir)
	if err != nil {
		return "", err
	}
	var candidates []string
	for _, entry := range entries {
		if entry.Dir && !entry.Symlink && entry.Name != "__MACOSX" {
			if info, err := s.fs.root.Lstat(path.Join(dir, entry.Name, "level.dat")); err == nil && info.Mode().IsRegular() {
				candidates = append(candidates, path.Join(dir, entry.Name))
			}
		}
	}
	if len(candidates) != 1 {
		return "", badRequest("im Zip liegt keine eindeutige Welt: Pack den Weltordner so, dass level.dat ganz oben oder in genau einem Ordner liegt")
	}
	return candidates[0], nil
}

// ---------------------------------------------------------------- Datapacks

func datapackDir(world string) string { return path.Join(world, "datapacks") }

// Datapacks listet die Datapacks einer Welt.
func (s *Server) Datapacks(world string) ([]Datapack, error) {
	if err := s.requireWorld(world); err != nil {
		return nil, err
	}
	list := []Datapack{}
	entries, err := s.fs.List(datapackDir(world))
	if errors.Is(err, fs.ErrNotExist) {
		return list, nil
	}
	if err != nil {
		return nil, err
	}
	for _, entry := range entries {
		if entry.Symlink || (!entry.Dir && !strings.HasSuffix(strings.ToLower(entry.Name), ".zip")) {
			continue
		}
		list = append(list, Datapack{Name: entry.Name, Dir: entry.Dir, Size: entry.Size, Modified: entry.Modified})
	}
	return list, nil
}

// AddDatapack legt ein Datapack (Zip mit pack.mcmeta) in den datapacks-Ordner einer Welt.
func (s *Server) AddDatapack(world, fileName string, content io.Reader, limit int64, overwrite bool) (string, error) {
	if err := s.requireWorld(world); err != nil {
		return "", err
	}
	name := path.Base(strings.ReplaceAll(fileName, "\\", "/"))
	if !datapackNamePattern.MatchString(name) || !strings.HasSuffix(strings.ToLower(name), ".zip") {
		return "", badRequest("ein Datapack ist eine .zip-Datei mit einem einfachen Namen (Buchstaben, Ziffern, Leerzeichen, _ . + - ( ) [ ])")
	}
	if err := s.fs.Mkdir(datapackDir(world)); err != nil {
		return "", err
	}
	target := path.Join(datapackDir(world), name)
	staging := target + ".lodestone-part"
	_ = s.fs.Remove(staging, false)
	if err := s.fs.Write(staging, content, limit, true); err != nil {
		return "", err
	}
	if err := s.checkDatapack(staging); err != nil {
		_ = s.fs.Remove(staging, false)
		return "", err
	}
	if s.fs.Exists(target) {
		if !overwrite {
			_ = s.fs.Remove(staging, false)
			return "", alreadyThere(name + " gibt es in dieser Welt schon")
		}
		if err := s.fs.Remove(target, true); err != nil {
			_ = s.fs.Remove(staging, false)
			return "", err
		}
	}
	if err := s.fs.Rename(staging, target); err != nil {
		_ = s.fs.Remove(staging, false)
		return "", err
	}
	return name, nil
}

// checkDatapack prüft, ob die Datei ein Zip mit pack.mcmeta ist (ganz oben, wie Minecraft es verlangt).
func (s *Server) checkDatapack(name string) error {
	file, info, err := s.fs.Open(name)
	if err != nil {
		return err
	}
	defer file.Close()
	reader, err := zip.NewReader(file, info.Size())
	if err != nil {
		return badRequest("die Datei ist kein gültiges Zip")
	}
	for _, entry := range reader.File {
		if entry.Name == "pack.mcmeta" {
			return nil
		}
	}
	return badRequest("das ist kein Datapack: pack.mcmeta fehlt ganz oben im Zip")
}

// RemoveDatapack löscht ein Datapack (Datei oder Ordner) einer Welt.
func (s *Server) RemoveDatapack(world, name string) error {
	if err := s.requireWorld(world); err != nil {
		return err
	}
	if !datapackNamePattern.MatchString(name) {
		return badRequest("ungültiger Name %q", name)
	}
	target := path.Join(datapackDir(world), name)
	if !s.fs.Exists(target) {
		return notFound("das Datapack „" + name + "“ gibt es nicht")
	}
	return s.fs.Remove(target, true)
}

// ---------------------------------------------------------------- Schnittstelle

func (a *API) registerWorldRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/servers/{id}/properties", a.server(a.getProperties))
	mux.HandleFunc("PUT /api/servers/{id}/properties", a.server(a.putProperties))
	mux.HandleFunc("GET /api/servers/{id}/worlds", a.server(a.listWorlds))
	mux.HandleFunc("POST /api/servers/{id}/worlds/{world}/reset", a.server(a.resetWorld))
	mux.HandleFunc("GET /api/servers/{id}/worlds/{world}/download", a.server(a.downloadWorld))
	mux.HandleFunc("POST /api/servers/{id}/worlds/{world}/upload", a.server(a.uploadWorld))
	mux.HandleFunc("GET /api/servers/{id}/worlds/{world}/datapacks", a.server(a.listDatapacks))
	mux.HandleFunc("POST /api/servers/{id}/worlds/{world}/datapacks", a.server(a.uploadDatapack))
	mux.HandleFunc("DELETE /api/servers/{id}/worlds/{world}/datapacks/{name}", a.server(a.deleteDatapack))
}

func (a *API) getProperties(w http.ResponseWriter, _ *http.Request, server *Server) {
	view, err := server.Properties()
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, view)
}

func (a *API) putProperties(w http.ResponseWriter, r *http.Request, server *Server) {
	var body struct {
		Set map[string]string `json:"set"`
	}
	if err := decode(r, &body); err != nil {
		writeError(w, err)
		return
	}
	view, err := server.SetProperties(body.Set)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, view)
}

func (a *API) listWorlds(w http.ResponseWriter, _ *http.Request, server *Server) {
	list, err := server.Worlds()
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, list)
}

func (a *API) resetWorld(w http.ResponseWriter, r *http.Request, server *Server) {
	var body WorldReset
	if err := decode(r, &body); err != nil {
		writeError(w, err)
		return
	}
	if err := server.ResetWorld(r.PathValue("world"), body); err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusAccepted, server.View())
}

func (a *API) downloadWorld(w http.ResponseWriter, r *http.Request, server *Server) {
	name := r.PathValue("world")
	if err := server.requireWorld(name); err != nil {
		writeError(w, err)
		return
	}
	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": name + ".zip"}))
	w.Header().Set("Cache-Control", "no-store")
	if err := server.WriteWorldZip(w, name); err != nil {
		// Der Kopf ist schon raus; mehr als abbrechen geht nicht
		a.log.Warn("Welt-Download abgebrochen", "server", server.ID(), "world", name, "error", err)
	}
}

// uploadFile liest den ersten Teil "file" eines Multipart-Uploads.
func uploadFile(w http.ResponseWriter, r *http.Request, limit int64) (io.Reader, string, error) {
	r.Body = http.MaxBytesReader(w, r.Body, limit)
	reader, err := r.MultipartReader()
	if err != nil {
		return nil, "", badRequest("kein Multipart-Upload")
	}
	for {
		part, err := reader.NextPart()
		if errors.Is(err, io.EOF) {
			return nil, "", badRequest("es wurde keine Datei mitgeschickt")
		}
		if err != nil {
			return nil, "", badRequest("%v", err)
		}
		if part.FormName() == "file" && part.FileName() != "" {
			return part, part.FileName(), nil
		}
	}
}

// uploadError übersetzt das Überschreiten der Upload-Grenze in eine verständliche Meldung.
func uploadError(err error, maxMB int) error {
	var tooLarge *http.MaxBytesError
	if errors.As(err, &tooLarge) {
		return badRequest("die Datei ist zu groß (erlaubt sind %d MB, einstellbar über maxUploadMB in der config.json des Nodes)", maxMB)
	}
	return err
}

func (a *API) uploadWorld(w http.ResponseWriter, r *http.Request, server *Server) {
	name := r.PathValue("world")
	if err := checkWorldName(name); err != nil {
		writeError(w, err)
		return
	}
	limit := int64(a.cfg.MaxUploadMB) << 20
	part, fileName, err := uploadFile(w, r, limit)
	if err != nil {
		writeError(w, err)
		return
	}
	if !strings.HasSuffix(strings.ToLower(fileName), ".zip") {
		writeError(w, badRequest("eine Welt wird als .zip hochgeladen"))
		return
	}
	if err := server.ReplaceWorld(name, part, limit, r.URL.Query().Get("backup") == "1"); err != nil {
		writeError(w, uploadError(err, a.cfg.MaxUploadMB))
		return
	}
	writeJSON(w, http.StatusCreated, map[string]string{"world": name})
}

func (a *API) listDatapacks(w http.ResponseWriter, r *http.Request, server *Server) {
	list, err := server.Datapacks(r.PathValue("world"))
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, list)
}

func (a *API) uploadDatapack(w http.ResponseWriter, r *http.Request, server *Server) {
	world := r.PathValue("world")
	if err := server.requireWorld(world); err != nil {
		writeError(w, err)
		return
	}
	limit := int64(a.cfg.MaxUploadMB) << 20
	part, fileName, err := uploadFile(w, r, limit)
	if err != nil {
		writeError(w, err)
		return
	}
	name, err := server.AddDatapack(world, fileName, part, limit, r.URL.Query().Get("overwrite") == "1")
	if err != nil {
		writeError(w, uploadError(err, a.cfg.MaxUploadMB))
		return
	}
	writeJSON(w, http.StatusCreated, map[string]string{"name": name})
}

func (a *API) deleteDatapack(w http.ResponseWriter, r *http.Request, server *Server) {
	if err := server.RemoveDatapack(r.PathValue("world"), r.PathValue("name")); err != nil {
		writeError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
