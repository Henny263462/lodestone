package node

import (
	"archive/tar"
	"context"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"
)

// Ausschlüsse, Festhalten und Übertragungen zu einem externen Ziel. Der Node kennt keine Zugangsdaten: Das Panel
// schickt vorsignierte Adressen, der Node lädt damit direkt hoch bzw. herunter.

const (
	backupMetaName  = ".lodestone-backup.json"
	maxExcludes     = 50
	maxExcludeChars = 200
	maxUploadParts  = 1000
)

var (
	errLocked       = fmt.Errorf("%w: das Backup ist festgehalten. Entsperre es zuerst", ErrConflict)
	errTransferBusy = fmt.Errorf("%w: es läuft gerade eine Übertragung zum oder vom Backup-Ziel", ErrBadState)
	// transferRetryWait ist die Pause vor einem neuen Versuch (Tests verkürzen sie).
	transferRetryWait = 2 * time.Second
)

// ---------------------------------------------------------------- Metadaten im Archiv

// backupMeta steht als erster Eintrag im Archiv, wenn beim Erstellen etwas ausgeschlossen wurde. Beim Einspielen
// bleiben die ausgeschlossenen Pfade dann stehen, statt gelöscht zu werden.
type backupMeta struct {
	Version int      `json:"version"`
	Exclude []string `json:"exclude,omitempty"`
}

func writeBackupMeta(archive *tar.Writer, exclude []string) error {
	if len(exclude) == 0 {
		return nil
	}
	data, err := json.Marshal(backupMeta{Version: 1, Exclude: exclude})
	if err != nil {
		return err
	}
	header := &tar.Header{Name: backupMetaName, Mode: 0o644, Size: int64(len(data)), Typeflag: tar.TypeReg, ModTime: time.Now()}
	if err := archive.WriteHeader(header); err != nil {
		return err
	}
	_, err = archive.Write(data)
	return err
}

func readBackupMeta(reader io.Reader, meta *backupMeta) error {
	data, err := io.ReadAll(io.LimitReader(reader, 64<<10))
	if err != nil {
		return err
	}
	if err := json.Unmarshal(data, meta); err != nil {
		return fmt.Errorf("die Angaben im Archiv sind unlesbar: %w", err)
	}
	return validateExcludes(meta.Exclude)
}

// ---------------------------------------------------------------- Ausschlüsse

// validateExcludes prüft Pfadmuster: relativ, ohne "..", höchstens 50 Stück zu je 200 Zeichen.
func validateExcludes(patterns []string) error {
	if len(patterns) > maxExcludes {
		return badRequest("höchstens %d Ausschlüsse", maxExcludes)
	}
	for _, pattern := range patterns {
		if pattern == "" || len(pattern) > maxExcludeChars || strings.ContainsAny(pattern, "\x00\\\r\n") || strings.HasPrefix(pattern, "/") {
			return badRequest("ungültiger Ausschluss %q", pattern)
		}
		for _, part := range strings.Split(strings.TrimSuffix(pattern, "/"), "/") {
			if part == "" || part == "." || part == ".." {
				return badRequest("ungültiger Ausschluss %q", pattern)
			}
			if _, err := path.Match(part, ""); err != nil {
				return badRequest("ungültiger Ausschluss %q", pattern)
			}
		}
	}
	return nil
}

// matchExclude sagt, ob ein Pfad (relativ, mit "/") ausgeschlossen ist. Muster ohne "/" gelten für jeden Datei- oder
// Ordnernamen in beliebiger Tiefe ("*.log"), Muster mit "/" ab dem Serverordner ("plugins/dynmap/web/**"). "**" steht
// für beliebig viele Ordner, ein "/" am Ende ist dasselbe wie "/**". Ein ausgeschlossener Ordner fehlt samt Inhalt.
func matchExclude(patterns []string, name string) bool {
	for _, pattern := range patterns {
		if strings.HasSuffix(pattern, "/") {
			pattern += "**"
		}
		if !strings.Contains(pattern, "/") {
			for _, part := range strings.Split(name, "/") {
				if ok, _ := path.Match(pattern, part); ok {
					return true
				}
			}
			continue
		}
		if matchSegments(strings.Split(pattern, "/"), strings.Split(name, "/")) {
			return true
		}
	}
	return false
}

func matchSegments(pattern, name []string) bool {
	for len(pattern) > 0 {
		if pattern[0] == "**" {
			if len(pattern) == 1 {
				return true
			}
			for i := 0; i <= len(name); i++ {
				if matchSegments(pattern[1:], name[i:]) {
					return true
				}
			}
			return false
		}
		if len(name) == 0 {
			return false
		}
		if ok, _ := path.Match(pattern[0], name[0]); !ok {
			return false
		}
		pattern, name = pattern[1:], name[1:]
	}
	return len(name) == 0
}

// removeExcept löscht im Serverordner alles außer den ausgeschlossenen Pfaden (und den Ordnern, die sie enthalten).
func (s *Server) removeExcept(dir string, exclude []string) error {
	entries, err := s.fs.List(dir)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		name := entry.Name
		if dir != "." {
			name = dir + "/" + entry.Name
		}
		if name == restoreTemp || matchExclude(exclude, name) {
			continue
		}
		if entry.Dir && !entry.Symlink {
			if err := s.removeExcept(name, exclude); err != nil {
				return err
			}
			_ = s.fs.root.Remove(name) // bleibt stehen, wenn darin Ausgeschlossenes liegt
			continue
		}
		if err := s.fs.root.Remove(name); err != nil {
			return err
		}
	}
	return nil
}

// mergeMove verschiebt den Inhalt von from nach to; vorhandene Ordner werden zusammengeführt.
func (s *Server) mergeMove(from, to string) error {
	entries, err := s.fs.List(from)
	if err != nil {
		return err
	}
	root := s.fs.root
	for _, entry := range entries {
		source, target := path.Join(from, entry.Name), entry.Name
		if to != "." {
			target = path.Join(to, entry.Name)
		}
		if info, err := root.Lstat(target); err == nil {
			if info.IsDir() && entry.Dir && !entry.Symlink {
				if err := s.mergeMove(source, target); err != nil {
					return err
				}
				continue
			}
			if err := root.RemoveAll(target); err != nil {
				return err
			}
		}
		if err := root.Rename(source, target); err != nil {
			return err
		}
	}
	return nil
}

// ---------------------------------------------------------------- Festhalten

func (s *Server) lockFile(name string) string { return filepath.Join(s.backupDir(), name+".locked") }

func (s *Server) backupLocked(name string) bool {
	_, err := os.Stat(s.lockFile(name))
	return err == nil
}

// SetBackupLocked hält ein Backup fest oder gibt es wieder frei.
func (s *Server) SetBackupLocked(name string, locked bool) (Backup, error) {
	if _, err := s.BackupFile(name); err != nil {
		return Backup{}, err
	}
	if locked {
		if err := os.WriteFile(s.lockFile(name), nil, 0o644); err != nil {
			return Backup{}, err
		}
	} else if err := os.Remove(s.lockFile(name)); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return Backup{}, err
	}
	list, err := s.ListBackups()
	if err != nil {
		return Backup{}, err
	}
	for _, backup := range list {
		if backup.Name == name {
			return backup, nil
		}
	}
	return Backup{}, fs.ErrNotExist
}

// ---------------------------------------------------------------- Übertragungen

// UploadRequest beschreibt einen Multipart-Upload: für jeden Teil (PartSize Bytes, der letzte kürzer) eine
// vorsignierte PUT-Adresse, in der Reihenfolge der Teilnummern.
type UploadRequest struct {
	Size     int64    `json:"size"`
	PartSize int64    `json:"partSize"`
	URLs     []string `json:"urls"`
	// AllowInternal erlaubt Ziele im internen Netz (das Panel setzt es nur mit LODESTONE_BACKUP_ALLOW_INTERNAL).
	AllowInternal bool `json:"allowInternal,omitempty"`
}

// FetchRequest lädt ein Backup über eine vorsignierte GET-Adresse in den Backup-Ordner, auf Wunsch mit Einspielen.
type FetchRequest struct {
	URL     string `json:"url"`
	Size    int64  `json:"size"`
	Restore bool   `json:"restore"`
	// AllowInternal erlaubt Ziele im internen Netz (das Panel setzt es nur mit LODESTONE_BACKUP_ALLOW_INTERNAL).
	AllowInternal bool `json:"allowInternal,omitempty"`
}

// UploadedPart ist ein fertig hochgeladener Teil mit der ETag, die das Ziel geliefert hat.
type UploadedPart struct {
	Number int    `json:"number"`
	ETag   string `json:"etag"`
}

// TransferStatus ist der Stand der letzten Übertragung eines Servers.
type TransferStatus struct {
	State string         `json:"state"` // idle, running, done, failed
	Kind  string         `json:"kind,omitempty"`
	Name  string         `json:"name,omitempty"`
	Done  int64          `json:"done"`
	Total int64          `json:"total"`
	Parts []UploadedPart `json:"parts,omitempty"`
	Error string         `json:"error,omitempty"`
}

// remoteTransfer hält den Zustand der Übertragungen eines Servers (eine zur Zeit).
type remoteTransfer struct {
	mu     sync.Mutex
	busy   bool
	status TransferStatus
}

func (s *Server) transferStatus() TransferStatus {
	s.transfer.mu.Lock()
	defer s.transfer.mu.Unlock()
	status := s.transfer.status
	if status.State == "" {
		status.State = "idle"
	}
	status.Parts = append([]UploadedPart(nil), status.Parts...)
	return status
}

func (s *Server) beginTransfer(kind, name string, total int64) bool {
	s.transfer.mu.Lock()
	defer s.transfer.mu.Unlock()
	if s.transfer.busy {
		return false
	}
	s.transfer.busy = true
	s.transfer.status = TransferStatus{State: "running", Kind: kind, Name: name, Total: total}
	return true
}

func (s *Server) progressTransfer(done int64, part *UploadedPart) {
	s.transfer.mu.Lock()
	defer s.transfer.mu.Unlock()
	s.transfer.status.Done = done
	if part != nil {
		s.transfer.status.Parts = append(s.transfer.status.Parts, *part)
	}
}

func (s *Server) endTransfer(err error) {
	s.transfer.mu.Lock()
	defer s.transfer.mu.Unlock()
	s.transfer.busy = false
	if err != nil {
		s.transfer.status.State, s.transfer.status.Error = "failed", err.Error()
		return
	}
	s.transfer.status.State = "done"
}

func validTransferURL(raw string) error {
	address, err := url.Parse(raw)
	if err != nil || (address.Scheme != "https" && address.Scheme != "http") || address.Host == "" || len(raw) > 4096 {
		return badRequest("ungültige Adresse des Backup-Ziels")
	}
	return nil
}

// errTransferInternal meldet ein gesperrtes internes Ziel, bewusst ohne die aufgelöste Adresse.
var errTransferInternal = errors.New("Verbindungen ins interne Netz sind für Backup-Ziele gesperrt. Ein Administrator kann sie am Panel mit LODESTONE_BACKUP_ALLOW_INTERNAL erlauben")

// transferClient folgt keinen Weiterleitungen (eine vorsignierte Adresse gilt nur für ihr Ziel). Ohne allowInternal
// verbindet er sich nie mit internen Adressen: geprüft wird die IP, mit der wirklich verbunden wird, auch wenn ein Name
// erst später (DNS-Rebinding) auf eine interne Adresse zeigt. Den Endpunkt stellt ein Projekt-Admin ein; ohne diese
// Sperre könnte er über den Node dessen Heimnetz (Router, NAS) oder Metadaten-Dienste ansprechen. Ein Proxy aus der
// Umgebung gilt dann nicht, weil er die Prüfung umginge.
func transferClient(allowInternal bool) *http.Client {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.ResponseHeaderTimeout = 2 * time.Minute
	if !allowInternal {
		dialer := &net.Dialer{Timeout: 30 * time.Second, KeepAlive: 30 * time.Second, Control: func(_, address string, _ syscall.RawConn) error {
			host, _, err := net.SplitHostPort(address)
			if err != nil {
				return err
			}
			if ip := net.ParseIP(host); ip == nil || internalIP(ip) {
				return errTransferInternal
			}
			return nil
		}}
		transport.DialContext = dialer.DialContext
		transport.Proxy = nil
	}
	return &http.Client{Transport: transport, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
}

// internalIP erkennt Adressen im internen Netz (wie isInternalIP im Panel): Loopback, privat (auch IPv6-ULA),
// link-local, CGNAT, 0.0.0.0/8, unbestimmt und Multicast.
func internalIP(ip net.IP) bool {
	if v4 := ip.To4(); v4 != nil {
		ip = v4
		if v4[0] == 0 || (v4[0] == 100 && v4[1]&0xc0 == 64) {
			return true
		}
	}
	return ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || ip.IsUnspecified() || ip.IsMulticast()
}

// remoteFailure macht aus einer Fehlerantwort des Ziels einen lesbaren Satz.
func remoteFailure(response *http.Response) error {
	data, _ := io.ReadAll(io.LimitReader(response.Body, 64<<10))
	var failure struct {
		Code string `xml:"Code"`
	}
	_ = xml.Unmarshal(data, &failure)
	if failure.Code != "" {
		return fmt.Errorf("das Backup-Ziel antwortet mit %d (%s)", response.StatusCode, failure.Code)
	}
	return fmt.Errorf("das Backup-Ziel antwortet mit %d", response.StatusCode)
}

// UploadBackup lädt ein fertiges Backup im Hintergrund in Teilen hoch. Den Stand liefert transferStatus.
func (s *Server) UploadBackup(name string, request UploadRequest) error {
	file, err := s.BackupFile(name)
	if err != nil {
		return err
	}
	info, err := os.Stat(file)
	if err != nil {
		return err
	}
	if request.Size != info.Size() {
		return badRequest("die Größe des Backups stimmt nicht (%d statt %d Bytes)", request.Size, info.Size())
	}
	if request.PartSize < 5<<20 && len(request.URLs) > 1 || request.PartSize <= 0 {
		return badRequest("ungültige Teilgröße")
	}
	parts := int((request.Size + request.PartSize - 1) / request.PartSize)
	if parts < 1 || parts > maxUploadParts || len(request.URLs) != parts {
		return badRequest("es werden %d Adressen gebraucht, nicht %d", parts, len(request.URLs))
	}
	for _, raw := range request.URLs {
		if err := validTransferURL(raw); err != nil {
			return err
		}
	}
	if !s.beginTransfer("upload", name, request.Size) {
		return errTransferBusy
	}
	go func() {
		s.logf("Backup %s wird zum Backup-Ziel hochgeladen", name)
		err := s.uploadParts(file, request)
		if err != nil {
			s.logf("Hochladen von %s fehlgeschlagen: %v", name, err)
		} else {
			s.logf("Backup %s hochgeladen", name)
		}
		s.endTransfer(err)
	}()
	return nil
}

func (s *Server) uploadParts(file string, request UploadRequest) error {
	source, err := os.Open(file)
	if err != nil {
		return err
	}
	defer source.Close()
	client := transferClient(request.AllowInternal)
	var done int64
	for index, address := range request.URLs {
		offset := int64(index) * request.PartSize
		length := min(request.PartSize, request.Size-offset)
		var etag string
		var lastErr error
		for attempt := 0; attempt < 3; attempt++ {
			if attempt > 0 {
				time.Sleep(transferRetryWait * time.Duration(attempt))
			}
			etag, lastErr = putPart(client, address, io.NewSectionReader(source, offset, length), length)
			if lastErr == nil {
				break
			}
		}
		if lastErr != nil {
			return fmt.Errorf("Teil %d von %d: %w", index+1, len(request.URLs), lastErr)
		}
		done += length
		s.progressTransfer(done, &UploadedPart{Number: index + 1, ETag: etag})
	}
	return nil
}

func putPart(client *http.Client, address string, body io.Reader, length int64) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()
	request, err := http.NewRequestWithContext(ctx, http.MethodPut, address, body)
	if err != nil {
		return "", err
	}
	request.ContentLength = length
	response, err := client.Do(request)
	if err != nil {
		return "", fmt.Errorf("das Backup-Ziel ist nicht erreichbar: %v", unwrapURL(err))
	}
	defer response.Body.Close()
	if response.StatusCode >= 300 {
		return "", remoteFailure(response)
	}
	_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 64<<10))
	etag := response.Header.Get("ETag")
	if etag == "" {
		return "", errors.New("das Backup-Ziel hat keine ETag geliefert")
	}
	return etag, nil
}

func unwrapURL(err error) error {
	if errors.Is(err, errTransferInternal) {
		return errTransferInternal
	}
	var urlErr *url.Error
	if errors.As(err, &urlErr) {
		return urlErr.Err
	}
	return err
}

// FetchBackup lädt ein Backup vom Backup-Ziel in den Backup-Ordner. Mit Restore wird es danach eingespielt; dafür
// muss der Server aus sein, und er bleibt während des Ladens im Zustand „restoring“.
func (s *Server) FetchBackup(name string, request FetchRequest) error {
	if !backupName.MatchString(name) {
		return badRequest("ungültiger Backup-Name")
	}
	if err := validTransferURL(request.URL); err != nil {
		return err
	}
	if request.Size <= 0 {
		return badRequest("ungültige Größe")
	}
	if _, err := s.BackupFile(name); err == nil {
		if request.Restore {
			return s.RestoreBackup(name) // liegt schon lokal
		}
		return nil
	}
	if err := os.MkdirAll(s.backupDir(), 0o755); err != nil {
		return err
	}
	if request.Restore {
		s.mu.Lock()
		if s.state != StateOffline {
			s.mu.Unlock()
			return ErrBadState
		}
		if !s.backupBusy.CompareAndSwap(false, true) {
			s.mu.Unlock()
			return errBusy
		}
		if !s.beginTransfer("download", name, request.Size) {
			s.backupBusy.Store(false)
			s.mu.Unlock()
			return errTransferBusy
		}
		s.setStateLocked(StateRestoring, "")
		s.mu.Unlock()
	} else if !s.beginTransfer("download", name, request.Size) {
		return errTransferBusy
	}
	go func() {
		s.logf("Backup %s wird vom Backup-Ziel geladen", name)
		err := s.download(name, request)
		s.endTransfer(err)
		if !request.Restore {
			if err != nil {
				s.logf("Laden von %s fehlgeschlagen: %v", name, err)
			}
			return
		}
		defer s.backupBusy.Store(false)
		if err == nil {
			s.logf("Backup %s wird eingespielt", name)
			fetched := filepath.Join(s.backupDir(), name)
			if err = s.extract(fetched); err != nil {
				// Was sich nicht einspielen lässt, bleibt nicht liegen: Sonst ließe sich jede fremde Antwort mit
				// passender Länge als „Backup“ herunterladen. Im Backup-Ziel liegt das Original weiter.
				_ = os.Remove(fetched)
			}
		}
		if err != nil {
			s.logf("Wiederherstellung fehlgeschlagen, die bisherigen Dateien sind unverändert: %v", err)
			s.setState(StateOffline, "Wiederherstellung fehlgeschlagen: "+err.Error())
			return
		}
		s.logf("Backup %s eingespielt", name)
		s.setState(StateOffline, "")
	}()
	return nil
}

func (s *Server) download(name string, request FetchRequest) error {
	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Hour)
	defer cancel()
	call, err := http.NewRequestWithContext(ctx, http.MethodGet, request.URL, nil)
	if err != nil {
		return err
	}
	response, err := transferClient(request.AllowInternal).Do(call)
	if err != nil {
		return fmt.Errorf("das Backup-Ziel ist nicht erreichbar: %v", unwrapURL(err))
	}
	defer response.Body.Close()
	if response.StatusCode >= 300 {
		return remoteFailure(response)
	}
	part := filepath.Join(s.backupDir(), name+".part")
	_ = os.Remove(part)
	out, err := os.OpenFile(part, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		return err
	}
	written, copyErr := io.Copy(out, &progressReader{reader: io.LimitReader(response.Body, request.Size+1), report: func(n int64) { s.progressTransfer(n, nil) }})
	if err := errors.Join(copyErr, out.Close()); err != nil {
		_ = os.Remove(part)
		return err
	}
	if written != request.Size {
		_ = os.Remove(part)
		return fmt.Errorf("das Backup ist unvollständig angekommen (%d von %d Bytes)", written, request.Size)
	}
	return os.Rename(part, filepath.Join(s.backupDir(), name))
}

type progressReader struct {
	reader io.Reader
	total  int64
	report func(int64)
}

func (r *progressReader) Read(p []byte) (int, error) {
	n, err := r.reader.Read(p)
	r.total += int64(n)
	r.report(r.total)
	return n, err
}

// ---------------------------------------------------------------- Schnittstelle

func (a *API) registerBackupRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/servers/{id}/backups/transfer", a.server(func(w http.ResponseWriter, _ *http.Request, server *Server) {
		writeJSON(w, http.StatusOK, server.transferStatus())
	}))
	mux.HandleFunc("POST /api/servers/{id}/backups/{name}/lock", a.server(a.lockBackup))
	mux.HandleFunc("POST /api/servers/{id}/backups/{name}/upload", a.server(a.uploadBackup))
	mux.HandleFunc("POST /api/servers/{id}/backups/{name}/fetch", a.server(a.fetchBackup))
}

func (a *API) lockBackup(w http.ResponseWriter, r *http.Request, server *Server) {
	var body struct {
		Locked bool `json:"locked"`
	}
	if err := decode(r, &body); err != nil {
		writeError(w, err)
		return
	}
	backup, err := server.SetBackupLocked(r.PathValue("name"), body.Locked)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, backup)
}

func (a *API) uploadBackup(w http.ResponseWriter, r *http.Request, server *Server) {
	var request UploadRequest
	if err := decode(r, &request); err != nil {
		writeError(w, err)
		return
	}
	if err := server.UploadBackup(r.PathValue("name"), request); err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusAccepted, server.transferStatus())
}

func (a *API) fetchBackup(w http.ResponseWriter, r *http.Request, server *Server) {
	var request FetchRequest
	if err := decode(r, &request); err != nil {
		writeError(w, err)
		return
	}
	if err := server.FetchBackup(r.PathValue("name"), request); err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusAccepted, server.transferStatus())
}
