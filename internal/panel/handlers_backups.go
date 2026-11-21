package panel

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"path"
	"regexp"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/lodestone-mc/lodestone/internal/node"
)

// Backups ausgebaut: externes S3-kompatibles Ziel pro Projekt, Hochladen und Wiederherstellen über vorsignierte
// Adressen, Ausschlüsse, Festhalten und getrennte Aufbewahrung. Die lokalen Archive verwaltet weiter der Node.

const (
	maxPanelExcludes = 50
	backupJobLimit   = 12 * time.Hour
	presignUpload    = 24 * time.Hour
	presignDownload  = 12 * time.Hour
	transferBusyWait = 30 * time.Minute
)

var (
	bucketPattern    = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9._-]{1,61}[a-zA-Z0-9]$`)
	regionPattern    = regexp.MustCompile(`^[a-z0-9-]{1,40}$`)
	prefixPattern    = regexp.MustCompile(`^[A-Za-z0-9._/-]{0,200}$`)
	accessKeyPattern = regexp.MustCompile(`^[\x21-\x7e]{1,128}$`)
	secretPattern    = regexp.MustCompile(`^[\x21-\x7e]{1,256}$`)
	backupFileName   = regexp.MustCompile(`^\d{8}-\d{6}(?:-[a-z0-9-]{1,40})?\.tar\.gz$`)
	errNoTarget      = fail(http.StatusBadRequest, "Für dieses Projekt ist kein Backup-Ziel eingerichtet. Richte es unter Projekt → Einstellungen → Backup-Ziel ein.")
)

// backupRuntime hält, was gerade läuft (nur im Speicher; nach einem Neustart gilt Laufendes als unterbrochen).
type backupRuntime struct {
	jobs    sync.Map // serverID/name → *backupJob
	uploads sync.Map // serverID → chan struct{}: eine Übertragung pro Server, damit sich die Stände nicht überschreiben
}

type backupJob struct {
	progress atomic.Int64 // Prozent beim Hochladen
}

func (p *Panel) registerBackupRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/projects/{id}/backup-target", p.authenticate(p.getBackupTarget))
	mux.HandleFunc("PUT /api/projects/{id}/backup-target", p.authenticate(p.putBackupTarget))
	mux.HandleFunc("DELETE /api/projects/{id}/backup-target", p.authenticate(p.deleteBackupTarget))
	mux.HandleFunc("POST /api/projects/{id}/backup-target/test", p.authenticate(p.testBackupTarget))
	mux.HandleFunc("GET /api/servers/{id}/backup-settings", p.authenticate(p.getBackupSettings))
	mux.HandleFunc("PUT /api/servers/{id}/backup-settings", p.authenticate(p.putBackupSettings))
	mux.HandleFunc("GET /api/servers/{id}/backups", p.authenticate(p.listServerBackups))
	mux.HandleFunc("POST /api/servers/{id}/backups", p.authenticate(p.createServerBackup))
	mux.HandleFunc("DELETE /api/servers/{id}/backups/{name}", p.authenticate(p.deleteServerBackup))
	mux.HandleFunc("POST /api/servers/{id}/backups/{name}/lock", p.authenticate(p.lockServerBackup))
	mux.HandleFunc("POST /api/servers/{id}/backups/{name}/upload", p.authenticate(p.uploadServerBackup))
	mux.HandleFunc("POST /api/servers/{id}/backups/{name}/restore-remote", p.authenticate(p.restoreRemoteBackup))
}

// ---------------------------------------------------------------- Backup-Ziel des Projekts

type backupTargetDTO struct {
	Configured bool   `json:"configured"`
	Endpoint   string `json:"endpoint"`
	Region     string `json:"region"`
	Bucket     string `json:"bucket"`
	Prefix     string `json:"prefix"`
	AccessKey  string `json:"accessKey"`
	PathStyle  bool   `json:"pathStyle"`
	HasSecret  bool   `json:"hasSecret"`
	UpdatedAt  int64  `json:"updatedAt,omitempty"`
}

type backupTargetRequest struct {
	Endpoint  string `json:"endpoint"`
	Region    string `json:"region"`
	Bucket    string `json:"bucket"`
	Prefix    string `json:"prefix"`
	AccessKey string `json:"accessKey"`
	SecretKey string `json:"secretKey"` // leer = das gespeicherte behalten
	PathStyle bool   `json:"pathStyle"`
}

func targetDTO(target BackupTarget) backupTargetDTO {
	return backupTargetDTO{Configured: true, Endpoint: target.Endpoint, Region: target.Region, Bucket: target.Bucket, Prefix: target.Prefix,
		AccessKey: target.AccessKey, PathStyle: target.PathStyle, HasSecret: target.SecretSealed != "", UpdatedAt: target.UpdatedAt}
}

// normalize prüft die Eingabe und liefert das Ziel mit Klartext-Secret (leer, wenn keins angegeben wurde).
func (body backupTargetRequest) normalize() (s3Target, error) {
	endpoint := strings.TrimRight(strings.TrimSpace(body.Endpoint), "/")
	address, err := url.Parse(endpoint)
	if err != nil || (address.Scheme != "https" && address.Scheme != "http") || address.Host == "" || address.User != nil ||
		address.Path != "" || address.RawQuery != "" || address.Fragment != "" || len(endpoint) > 200 {
		return s3Target{}, fail(http.StatusBadRequest, "Gib den Endpoint als Adresse ohne Pfad an, z. B. https://s3.eu-central-1.amazonaws.com.")
	}
	host := address.Host
	if (address.Scheme == "https" && strings.HasSuffix(host, ":443")) || (address.Scheme == "http" && strings.HasSuffix(host, ":80")) {
		host = host[:strings.LastIndex(host, ":")]
	}
	target := s3Target{
		Endpoint: address.Scheme + "://" + strings.ToLower(host), Region: strings.TrimSpace(body.Region), Bucket: strings.TrimSpace(body.Bucket),
		AccessKey: strings.TrimSpace(body.AccessKey), Secret: strings.TrimSpace(body.SecretKey), PathStyle: body.PathStyle,
	}
	if target.Region == "" {
		target.Region = "us-east-1"
	}
	if !regionPattern.MatchString(target.Region) {
		return s3Target{}, fail(http.StatusBadRequest, "Die Region besteht aus Kleinbuchstaben, Ziffern und Bindestrichen, z. B. eu-central-1 oder auto.")
	}
	if !bucketPattern.MatchString(target.Bucket) || strings.Contains(target.Bucket, "..") {
		return s3Target{}, fail(http.StatusBadRequest, "Der Bucket-Name braucht 3 bis 63 Zeichen aus Buchstaben, Ziffern, Punkt und Bindestrich.")
	}
	if !target.PathStyle && strings.ToLower(target.Bucket) != target.Bucket {
		return s3Target{}, fail(http.StatusBadRequest, "Bucket-Namen mit Großbuchstaben gehen nur mit Pfad-Adressen. Schalte „Pfad-Adressen“ ein.")
	}
	prefix := strings.Trim(strings.TrimSpace(body.Prefix), "/")
	if !prefixPattern.MatchString(prefix) || strings.Contains(prefix, "//") || strings.Contains("/"+prefix+"/", "/../") || strings.Contains("/"+prefix+"/", "/./") {
		return s3Target{}, fail(http.StatusBadRequest, "Das Präfix darf nur Buchstaben, Ziffern, Punkt, Unterstrich, Bindestrich und / enthalten.")
	}
	if prefix != "" {
		prefix += "/"
	}
	target.Prefix = prefix
	if !accessKeyPattern.MatchString(target.AccessKey) {
		return s3Target{}, fail(http.StatusBadRequest, "Gib den Access Key an (ohne Leerzeichen).")
	}
	if target.Secret != "" && !secretPattern.MatchString(target.Secret) {
		return s3Target{}, fail(http.StatusBadRequest, "Das Secret darf keine Leerzeichen enthalten und höchstens 256 Zeichen lang sein.")
	}
	return target, nil
}

// backupTarget liefert das entschlüsselte Ziel eines Projekts.
func (p *Panel) backupTarget(projectID string) (s3Target, error) {
	stored, err := p.store.BackupTarget(projectID)
	if errors.Is(err, ErrNotFound) {
		return s3Target{}, errNoTarget
	}
	if err != nil {
		return s3Target{}, err
	}
	secret, err := p.unseal(stored.SecretSealed)
	if err != nil {
		return s3Target{}, fail(http.StatusInternalServerError, "Das Secret des Backup-Ziels lässt sich nicht entschlüsseln (wurde secret.key ersetzt?). Gib es neu ein.")
	}
	return s3Target{Endpoint: stored.Endpoint, Region: stored.Region, Bucket: stored.Bucket, Prefix: stored.Prefix, AccessKey: stored.AccessKey, Secret: secret, PathStyle: stored.PathStyle}, nil
}

func (p *Panel) getBackupTarget(w http.ResponseWriter, r *http.Request) {
	project, _, err := p.requireProjectPermission(r, r.PathValue("id"), PermSettings)
	if err != nil {
		writeError(w, err)
		return
	}
	stored, err := p.store.BackupTarget(project.ID)
	if errors.Is(err, ErrNotFound) {
		writeJSON(w, http.StatusOK, backupTargetDTO{PathStyle: true})
		return
	}
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, targetDTO(stored))
}

// mergeSecret nimmt das gespeicherte Secret, wenn keins angegeben wurde.
func (p *Panel) mergeSecret(projectID string, target *s3Target) error {
	if target.Secret != "" {
		return nil
	}
	stored, err := p.backupTarget(projectID)
	if err != nil {
		if errors.Is(err, errNoTarget) {
			return fail(http.StatusBadRequest, "Gib das Secret an.")
		}
		return err
	}
	target.Secret = stored.Secret
	return nil
}

func (p *Panel) putBackupTarget(w http.ResponseWriter, r *http.Request) {
	project, _, err := p.requireProjectPermission(r, r.PathValue("id"), PermSettings)
	if err != nil {
		writeError(w, err)
		return
	}
	var body backupTargetRequest
	if err := decode(r, &body); err != nil {
		writeError(w, err)
		return
	}
	target, err := body.normalize()
	if err == nil {
		err = p.mergeSecret(project.ID, &target)
	}
	if err != nil {
		writeError(w, err)
		return
	}
	sealed, err := p.seal(target.Secret)
	if err != nil {
		writeError(w, err)
		return
	}
	stored := BackupTarget{ProjectID: project.ID, Endpoint: target.Endpoint, Region: target.Region, Bucket: target.Bucket, Prefix: target.Prefix,
		AccessKey: target.AccessKey, SecretSealed: sealed, PathStyle: target.PathStyle}
	if err := p.store.SetBackupTarget(stored); err != nil {
		writeError(w, err)
		return
	}
	stored, err = p.store.BackupTarget(project.ID)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, targetDTO(stored))
}

func (p *Panel) deleteBackupTarget(w http.ResponseWriter, r *http.Request) {
	project, _, err := p.requireProjectPermission(r, r.PathValue("id"), PermSettings)
	if err != nil {
		writeError(w, err)
		return
	}
	if err := p.store.DeleteBackupTarget(project.ID); err != nil {
		writeError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// testBackupTarget prüft die eingegebenen (noch nicht gespeicherten) Werte.
func (p *Panel) testBackupTarget(w http.ResponseWriter, r *http.Request) {
	project, _, err := p.requireProjectPermission(r, r.PathValue("id"), PermSettings)
	if err != nil {
		writeError(w, err)
		return
	}
	var body backupTargetRequest
	if err := decode(r, &body); err != nil {
		writeError(w, err)
		return
	}
	target, err := body.normalize()
	if err == nil {
		err = p.mergeSecret(project.ID, &target)
	}
	if err != nil {
		writeError(w, err)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	if err := p.s3(target).test(ctx); err != nil {
		writeError(w, fail(http.StatusBadGateway, err.Error()))
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// ---------------------------------------------------------------- Einstellungen eines Servers

type backupSettingsDTO struct {
	Exclude    []string `json:"exclude"`
	RemoteKeep int      `json:"remoteKeep"`
	// TargetConfigured sagt, ob das Projekt ein Backup-Ziel hat (nur lesen; das Ziel selbst sieht nur, wer „Einstellungen“ hat).
	TargetConfigured bool `json:"targetConfigured"`
}

// cleanExcludes prüft Pfadmuster (dieselben Regeln wie der Node) und entfernt leere und doppelte.
func cleanExcludes(patterns []string) ([]string, error) {
	out, seen := []string{}, map[string]bool{}
	for _, pattern := range patterns {
		pattern = strings.TrimSpace(pattern)
		if pattern == "" || seen[pattern] {
			continue
		}
		seen[pattern] = true
		bad := len(pattern) > 200 || strings.ContainsAny(pattern, "\x00\\\r\n") || strings.HasPrefix(pattern, "/")
		for _, part := range strings.Split(strings.TrimSuffix(pattern, "/"), "/") {
			if _, err := path.Match(part, ""); part == "" || part == "." || part == ".." || err != nil {
				bad = true
			}
		}
		if bad {
			return nil, fail(http.StatusBadRequest, fmt.Sprintf("Der Ausschluss „%s“ ist ungültig. Nutze relative Pfade wie dynmap/** oder *.log.", pattern))
		}
		out = append(out, pattern)
	}
	if len(out) > maxPanelExcludes {
		return nil, fail(http.StatusBadRequest, fmt.Sprintf("Es gehen höchstens %d Ausschlüsse.", maxPanelExcludes))
	}
	return out, nil
}

func (p *Panel) getBackupSettings(w http.ResponseWriter, r *http.Request) {
	row, _, _, err := p.requireServerPermission(r, PermBackupsRead)
	if err != nil {
		writeError(w, err)
		return
	}
	settings, err := p.store.BackupSettingsFor(row.ID)
	if err != nil {
		writeError(w, err)
		return
	}
	_, err = p.store.BackupTarget(row.ProjectID)
	writeJSON(w, http.StatusOK, backupSettingsDTO{Exclude: settings.Exclude, RemoteKeep: settings.RemoteKeep, TargetConfigured: err == nil})
}

func (p *Panel) putBackupSettings(w http.ResponseWriter, r *http.Request) {
	row, _, _, err := p.requireServerPermission(r, PermSettings)
	if err != nil {
		writeError(w, err)
		return
	}
	var body struct {
		Exclude    []string `json:"exclude"`
		RemoteKeep int      `json:"remoteKeep"`
	}
	if err := decode(r, &body); err != nil {
		writeError(w, err)
		return
	}
	if body.RemoteKeep < 1 || body.RemoteKeep > 200 {
		writeError(w, fail(http.StatusBadRequest, "Extern werden 1 bis 200 Backups aufbewahrt."))
		return
	}
	exclude, err := cleanExcludes(body.Exclude)
	if err != nil {
		writeError(w, err)
		return
	}
	if err := p.store.SetBackupSettings(BackupSettings{ServerID: row.ID, Exclude: exclude, RemoteKeep: body.RemoteKeep}); err != nil {
		writeError(w, err)
		return
	}
	_, err = p.store.BackupTarget(row.ProjectID)
	writeJSON(w, http.StatusOK, backupSettingsDTO{Exclude: exclude, RemoteKeep: body.RemoteKeep, TargetConfigured: err == nil})
}

// ---------------------------------------------------------------- Liste

type backupDTO struct {
	Name     string `json:"name"`
	Label    string `json:"label,omitempty"`
	Size     int64  `json:"size"`
	Created  int64  `json:"created"`
	Status   string `json:"status"` // running, done, failed
	Local    bool   `json:"local"`
	Remote   bool   `json:"remote"`
	Upload   string `json:"upload,omitempty"` // running, done, failed: das Hochladen
	Progress int64  `json:"progress,omitempty"`
	Error    string `json:"error,omitempty"`
	Locked   bool   `json:"locked"`
	// OtherTarget: das externe Backup liegt in einem früheren Backup-Ziel und ist über das Panel nicht mehr erreichbar.
	OtherTarget bool `json:"otherTarget,omitempty"`
}

func jobKey(serverID, name string) string { return serverID + "/" + name }

func (p *Panel) activeJob(serverID, name string) (*backupJob, bool) {
	value, ok := p.backups.jobs.Load(jobKey(serverID, name))
	if !ok {
		return nil, false
	}
	return value.(*backupJob), true
}

func (p *Panel) localBackups(ctx context.Context, row ServerRow) ([]node.Backup, error) {
	client, _, err := p.client(row.NodeID)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	list := []node.Backup{}
	err = client.Do(ctx, http.MethodGet, "/api/servers/"+row.ID+"/backups", nil, &list)
	return list, err
}

// mergeBackups fügt die lokalen Archive des Nodes und die Einträge des Panels zusammen und räumt veraltete Einträge auf.
func (p *Panel) mergeBackups(row ServerRow, local []node.Backup, records []BackupRecord, identity string) []backupDTO {
	byName := map[string]BackupRecord{}
	for _, record := range records {
		byName[record.Name] = record
	}
	list := []backupDTO{}
	seen := map[string]bool{}
	apply := func(dto *backupDTO, record BackupRecord, job *backupJob, running bool) {
		dto.Locked = dto.Locked || record.Locked
		dto.Upload = record.RemoteState
		if record.RemoteState == backupRunning && !running {
			// Das Panel wurde während des Hochladens neu gestartet
			dto.Upload, dto.Error = backupFailed, "Das Hochladen wurde unterbrochen. Lade das Backup erneut hoch."
			record.RemoteState, record.Error = backupFailed, dto.Error
			_ = p.store.SetBackupRemote(record)
			if record.UploadID != "" {
				go p.abortStale(row.ProjectID, record)
			}
		}
		if record.RemoteState == backupDone {
			dto.Remote = true
			dto.OtherTarget = record.RemoteTarget != identity
		}
		if record.RemoteState == backupFailed || record.Status == backupFailed {
			dto.Error = record.Error
		}
		if job != nil && record.RemoteState == backupRunning {
			dto.Progress = job.progress.Load()
		}
	}
	for _, backup := range local {
		seen[backup.Name] = true
		dto := backupDTO{Name: backup.Name, Label: backup.Label, Size: backup.Size, Created: backup.Created, Status: backupDone, Local: !backup.Creating, Locked: backup.Locked}
		if backup.Creating {
			dto.Status = backupRunning
		}
		if record, ok := byName[backup.Name]; ok {
			job, running := p.activeJob(row.ID, backup.Name)
			if record.Status == backupRunning && !running && !backup.Creating {
				_ = p.store.SetBackupStatus(row.ID, backup.Name, backupDone, backup.Size, "")
			}
			apply(&dto, record, job, running)
			if running { // solange das Panel noch daran arbeitet (Hochladen, Abschluss), läuft es
				dto.Status = backupRunning
				if record.WantRemote && dto.Upload == "" {
					dto.Upload = backupRunning
				}
			}
		}
		list = append(list, dto)
	}
	for _, record := range records {
		if seen[record.Name] {
			continue
		}
		job, running := p.activeJob(row.ID, record.Name)
		dto := backupDTO{Name: record.Name, Label: record.Label, Size: record.Size, Created: record.CreatedAt, Status: record.Status}
		switch {
		case running:
			dto.Status = backupRunning
		case record.Status == backupRunning:
			// Nach einem Neustart des Panels: Der Node hat kein Archiv dazu
			dto.Status, record.Error = backupFailed, "Das Backup ist nicht entstanden (siehe Konsole des Servers)."
			_ = p.store.SetBackupStatus(row.ID, record.Name, backupFailed, 0, record.Error)
			record.Status = backupFailed
		}
		apply(&dto, record, job, running)
		if !running && !dto.Remote && dto.Status != backupFailed && dto.Upload != backupFailed {
			// lokal gelöscht oder von der Aufbewahrung entfernt, extern nicht vorhanden: der Eintrag ist überflüssig
			_ = p.store.DeleteBackupRecord(row.ID, record.Name)
			continue
		}
		if dto.Remote || running {
			dto.Error = ""
			if dto.Upload == backupFailed {
				dto.Error = record.Error
			}
		}
		if !running && !dto.Remote {
			dto.Status = backupFailed
		}
		list = append(list, dto)
	}
	sort.Slice(list, func(i, j int) bool {
		if list[i].Created != list[j].Created {
			return list[i].Created > list[j].Created
		}
		return list[i].Name > list[j].Name
	})
	return list
}

func (p *Panel) abortStale(projectID string, record BackupRecord) {
	target, err := p.backupTarget(projectID)
	if err != nil || target.identity() != record.RemoteTarget {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := p.s3(target).abortMultipart(ctx, record.RemoteKey, record.UploadID); err == nil {
		record.UploadID = ""
		_ = p.store.SetBackupRemote(record)
	}
}

func (p *Panel) targetIdentity(projectID string) string {
	stored, err := p.store.BackupTarget(projectID)
	if err != nil {
		return ""
	}
	return s3Target{Endpoint: stored.Endpoint, Bucket: stored.Bucket}.identity()
}

func (p *Panel) listServerBackups(w http.ResponseWriter, r *http.Request) {
	row, _, _, err := p.requireServerPermission(r, PermBackupsRead)
	if err != nil {
		writeError(w, err)
		return
	}
	local, err := p.localBackups(r.Context(), row)
	if err != nil {
		writeError(w, err)
		return
	}
	records, err := p.store.ListBackupRecords(row.ID)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, p.mergeBackups(row, local, records, p.targetIdentity(row.ProjectID)))
}

// ---------------------------------------------------------------- Erstellen und Hochladen

// startBackup lässt den Node ein Archiv anlegen (mit den Ausschlüssen des Servers) und merkt sich den Eintrag.
// Danach muss finishBackup aufgerufen werden.
func (p *Panel) startBackup(ctx context.Context, row ServerRow, label string, remote bool) (BackupRecord, error) {
	if remote {
		if _, err := p.backupTarget(row.ProjectID); err != nil {
			return BackupRecord{}, err
		}
	}
	settings, err := p.store.BackupSettingsFor(row.ID)
	if err != nil {
		return BackupRecord{}, err
	}
	client, _, err := p.client(row.NodeID)
	if err != nil {
		return BackupRecord{}, err
	}
	body := map[string]any{"label": label}
	if len(settings.Exclude) > 0 { // ältere Nodes kennen das Feld nicht
		body["exclude"] = settings.Exclude
	}
	var created node.Backup
	if err := client.Do(ctx, http.MethodPost, "/api/servers/"+row.ID+"/backups", body, &created); err != nil {
		return BackupRecord{}, err
	}
	p.invalidate(row.NodeID)
	if created.Created == 0 {
		created.Created = time.Now().UnixMilli()
	}
	record := BackupRecord{ServerID: row.ID, Name: created.Name, ProjectID: row.ProjectID, Label: created.Label, CreatedAt: created.Created, Status: backupRunning, WantRemote: remote}
	p.backups.jobs.Store(jobKey(row.ID, record.Name), &backupJob{})
	if err := p.store.AddBackupRecord(record); err != nil {
		p.backups.jobs.Delete(jobKey(row.ID, record.Name))
		return BackupRecord{}, err
	}
	return record, nil
}

// finishBackup wartet auf das Archiv, lädt es auf Wunsch hoch und meldet den Erfolg. Fehler meldet der Aufrufer.
func (p *Panel) finishBackup(ctx context.Context, row ServerRow, record BackupRecord) error {
	defer p.backups.jobs.Delete(jobKey(row.ID, record.Name))
	client, _, err := p.client(row.NodeID)
	if err != nil {
		return err
	}
	size, err := p.waitBackupCreated(ctx, client, row, record.Name)
	if err != nil {
		_ = p.store.SetBackupStatus(row.ID, record.Name, backupFailed, 0, err.Error())
		return err
	}
	_ = p.store.SetBackupStatus(row.ID, record.Name, backupDone, size, "")
	if !record.WantRemote {
		p.emit(row.ProjectID, row.ID, "backup", "info", fmt.Sprintf("Backup von %s erstellt (%s).", row.Name, byteSize(size)))
		return nil
	}
	record.Size = size
	if err := p.uploadToTarget(ctx, row, record); err != nil {
		return fmt.Errorf("das Backup liegt lokal, aber das Hochladen ist fehlgeschlagen: %w", err)
	}
	p.emit(row.ProjectID, row.ID, "backup", "info", fmt.Sprintf("Backup von %s erstellt und zum Backup-Ziel hochgeladen (%s).", row.Name, byteSize(size)))
	return nil
}

func (p *Panel) waitBackupCreated(ctx context.Context, client *NodeClient, row ServerRow, name string) (int64, error) {
	deadline := time.Now().Add(backupWaitLimit)
	for {
		var list []node.Backup
		if err := client.Do(ctx, http.MethodGet, "/api/servers/"+row.ID+"/backups", nil, &list); err != nil {
			return 0, err
		}
		creating := false
		for _, backup := range list {
			if backup.Name == name {
				if !backup.Creating {
					return backup.Size, nil
				}
				creating = true
			}
		}
		if !creating {
			return 0, errors.New("das Backup ist nicht entstanden (siehe Konsole des Servers)")
		}
		if time.Now().After(deadline) {
			return 0, errors.New("das Backup dauert zu lange")
		}
		select {
		case <-time.After(pollEvery):
		case <-ctx.Done():
			return 0, ctx.Err()
		}
	}
}

// acquireTransfer wartet, bis für den Server keine andere Übertragung läuft.
func (p *Panel) acquireTransfer(ctx context.Context, serverID string) (func(), error) {
	value, _ := p.backups.uploads.LoadOrStore(serverID, make(chan struct{}, 1))
	slot := value.(chan struct{})
	select {
	case slot <- struct{}{}:
		return func() { <-slot }, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// uploadToTarget lädt ein fertiges lokales Backup als Multipart-Upload hoch: Das Panel beginnt den Upload, schickt dem
// Node vorsignierte Adressen für jeden Teil, wartet auf dessen ETags und setzt das Objekt zusammen.
func (p *Panel) uploadToTarget(ctx context.Context, row ServerRow, record BackupRecord) error {
	markFailed := func(err error) error {
		record.RemoteState, record.Error = backupFailed, err.Error()
		_ = p.store.SetBackupRemote(record)
		return err
	}
	record.WantRemote, record.RemoteState, record.Error = true, backupRunning, ""
	_ = p.store.SetBackupRemote(record)
	target, err := p.backupTarget(row.ProjectID)
	if err != nil {
		return markFailed(err)
	}
	partSize, parts, err := s3PartSize(record.Size)
	if err != nil {
		return markFailed(err)
	}
	release, err := p.acquireTransfer(ctx, row.ID)
	if err != nil {
		return markFailed(err)
	}
	defer release()
	client, _, err := p.client(row.NodeID)
	if err != nil {
		return markFailed(err)
	}
	bucket := p.s3(target)
	key := target.Prefix + row.ID + "/" + record.Name
	uploadID, err := bucket.createMultipart(ctx, key)
	if err != nil {
		return markFailed(err)
	}
	record.RemoteKey, record.RemoteTarget, record.UploadID = key, target.identity(), uploadID
	_ = p.store.SetBackupRemote(record)
	abort := func(cause error) error {
		abortCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if bucket.abortMultipart(abortCtx, key, uploadID) == nil {
			record.UploadID = ""
		}
		return markFailed(cause)
	}
	urls := make([]string, parts)
	for i := range urls {
		urls[i] = bucket.presign(http.MethodPut, key, url.Values{"partNumber": {fmt.Sprint(i + 1)}, "uploadId": {uploadID}}, presignUpload)
	}
	request := node.UploadRequest{Size: record.Size, PartSize: partSize, URLs: urls, AllowInternal: p.cfg.AllowInternalBackupTargets}
	base := "/api/servers/" + row.ID + "/backups/"
	busyUntil := time.Now().Add(transferBusyWait)
	for {
		err := client.Do(ctx, http.MethodPost, base+record.Name+"/upload", request, nil)
		if err == nil {
			break
		}
		if !isConflict(err) || time.Now().After(busyUntil) { // 409: der Node lädt gerade etwas anderes (z. B. eine Wiederherstellung)
			return abort(err)
		}
		select {
		case <-time.After(pollEvery * 5):
		case <-ctx.Done():
			return abort(ctx.Err())
		}
	}
	job, _ := p.activeJob(row.ID, record.Name)
	var status node.TransferStatus
	for {
		select {
		case <-time.After(pollEvery):
		case <-ctx.Done():
			return abort(ctx.Err())
		}
		if err := client.Do(ctx, http.MethodGet, base+"transfer", nil, &status); err != nil {
			return abort(err)
		}
		if status.Name != record.Name || status.Kind != "upload" {
			return abort(errors.New("der Node hat den Upload verloren (wurde er neu gestartet?)"))
		}
		if job != nil && status.Total > 0 {
			job.progress.Store(status.Done * 100 / status.Total)
		}
		if status.State == "failed" {
			return abort(errors.New(status.Error))
		}
		if status.State == "done" {
			break
		}
	}
	if len(status.Parts) != parts {
		return abort(fmt.Errorf("der Node hat %d von %d Teilen hochgeladen", len(status.Parts), parts))
	}
	list := make([]s3Part, len(status.Parts))
	for i, part := range status.Parts {
		list[i] = s3Part{Number: part.Number, ETag: part.ETag}
	}
	if err := bucket.completeMultipart(ctx, key, uploadID, list); err != nil {
		return abort(err)
	}
	record.RemoteState, record.UploadID, record.Error = backupDone, "", ""
	_ = p.store.SetBackupRemote(record)
	p.pruneRemote(ctx, row, target)
	return nil
}

// pruneRemote löscht extern die ältesten Backups über der Aufbewahrung. Festgehaltene zählen nicht mit.
func (p *Panel) pruneRemote(ctx context.Context, row ServerRow, target s3Target) {
	settings, err := p.store.BackupSettingsFor(row.ID)
	if err != nil {
		return
	}
	records, err := p.store.ListBackupRecords(row.ID)
	if err != nil {
		return
	}
	kept := 0
	for _, record := range records {
		if record.RemoteState != backupDone || record.RemoteTarget != target.identity() || record.Locked {
			continue
		}
		if kept++; kept <= settings.RemoteKeep {
			continue
		}
		if err := p.s3(target).deleteObject(ctx, record.RemoteKey); err != nil {
			p.log.Warn("Altes externes Backup nicht gelöscht", "server", row.ID, "backup", record.Name, "error", err)
			continue
		}
		record.RemoteState, record.RemoteKey, record.RemoteTarget = "", "", ""
		_ = p.store.SetBackupRemote(record)
	}
}

func (p *Panel) createServerBackup(w http.ResponseWriter, r *http.Request) {
	row, _, _, err := p.requireServerPermission(r, PermBackupsCreate)
	if err != nil {
		writeError(w, err)
		return
	}
	var body struct {
		Label  string `json:"label"`
		Remote bool   `json:"remote"`
	}
	if r.ContentLength != 0 {
		if err := decode(r, &body); err != nil {
			writeError(w, err)
			return
		}
	}
	if len(body.Label) > 40 {
		writeError(w, fail(http.StatusBadRequest, "Die Bezeichnung darf höchstens 40 Zeichen lang sein."))
		return
	}
	record, err := p.startBackup(r.Context(), row, body.Label, body.Remote)
	if err != nil {
		writeError(w, err)
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), backupJobLimit)
		defer cancel()
		if err := p.finishBackup(ctx, row, record); err != nil {
			p.emit(row.ProjectID, row.ID, "backup_failed", "error", fmt.Sprintf("Das Backup von %s ist fehlgeschlagen: %v", row.Name, err))
		}
	}()
	writeJSON(w, http.StatusAccepted, backupDTO{Name: record.Name, Label: record.Label, Created: record.CreatedAt, Status: backupRunning, Locked: false})
}

// findLocal sucht ein Backup in der Liste des Nodes.
func findLocal(list []node.Backup, name string) (node.Backup, bool) {
	for _, backup := range list {
		if backup.Name == name {
			return backup, true
		}
	}
	return node.Backup{}, false
}

// backupContext prüft die Rechte und lädt Server, lokale Liste und Eintrag zu {name}.
func (p *Panel) backupContext(r *http.Request, needed ...Permission) (ServerRow, string, node.Backup, bool, BackupRecord, bool, error) {
	row, _, _, err := p.requireServerPermission(r, needed...)
	if err != nil {
		return ServerRow{}, "", node.Backup{}, false, BackupRecord{}, false, err
	}
	name := r.PathValue("name")
	if !backupFileName.MatchString(name) {
		return ServerRow{}, "", node.Backup{}, false, BackupRecord{}, false, fail(http.StatusBadRequest, "ungültiger Backup-Name")
	}
	local, err := p.localBackups(r.Context(), row)
	if err != nil {
		return ServerRow{}, "", node.Backup{}, false, BackupRecord{}, false, err
	}
	backup, hasLocal := findLocal(local, name)
	record, err := p.store.BackupRecord(row.ID, name)
	hasRecord := err == nil
	if err != nil && !errors.Is(err, ErrNotFound) {
		return ServerRow{}, "", node.Backup{}, false, BackupRecord{}, false, err
	}
	if !hasLocal && !hasRecord {
		return ServerRow{}, "", node.Backup{}, false, BackupRecord{}, false, errNotFound
	}
	return row, name, backup, hasLocal, record, hasRecord, nil
}

func (p *Panel) uploadServerBackup(w http.ResponseWriter, r *http.Request) {
	row, name, backup, hasLocal, record, hasRecord, err := p.backupContext(r, PermBackupsCreate)
	if err != nil {
		writeError(w, err)
		return
	}
	if !hasLocal || backup.Creating {
		writeError(w, fail(http.StatusConflict, "Das Backup liegt nicht fertig auf dem Node und lässt sich nicht hochladen."))
		return
	}
	if _, err := p.backupTarget(row.ProjectID); err != nil {
		writeError(w, err)
		return
	}
	if _, running := p.activeJob(row.ID, name); running {
		writeError(w, fail(http.StatusConflict, "Das Backup wird gerade erstellt oder hochgeladen."))
		return
	}
	if !hasRecord {
		record = BackupRecord{ServerID: row.ID, Name: name, ProjectID: row.ProjectID, Label: backup.Label, CreatedAt: backup.Created, Status: backupDone}
		if err := p.store.AddBackupRecord(record); err != nil {
			writeError(w, err)
			return
		}
	}
	_ = p.store.SetBackupStatus(row.ID, name, backupDone, backup.Size, "")
	record.Size, record.Status = backup.Size, backupDone
	key := jobKey(row.ID, name)
	if _, loaded := p.backups.jobs.LoadOrStore(key, &backupJob{}); loaded {
		writeError(w, fail(http.StatusConflict, "Das Backup wird gerade erstellt oder hochgeladen."))
		return
	}
	record.WantRemote, record.RemoteState = true, backupRunning
	_ = p.store.SetBackupRemote(record)
	go func() {
		defer p.backups.jobs.Delete(key)
		ctx, cancel := context.WithTimeout(context.Background(), backupJobLimit)
		defer cancel()
		if err := p.uploadToTarget(ctx, row, record); err != nil {
			p.emit(row.ProjectID, row.ID, "backup_failed", "error", fmt.Sprintf("Das Backup %s von %s ließ sich nicht hochladen: %v", name, row.Name, err))
			return
		}
		p.emit(row.ProjectID, row.ID, "backup", "info", fmt.Sprintf("Backup %s von %s zum Backup-Ziel hochgeladen (%s).", name, row.Name, byteSize(record.Size)))
	}()
	w.WriteHeader(http.StatusAccepted)
}

// lockServerBackup hält ein Backup fest oder entsperrt es. Beides entscheidet darüber, ob es gelöscht werden kann
// (von Hand oder durch die Aufbewahrung), und braucht deshalb „Backups löschen“.
func (p *Panel) lockServerBackup(w http.ResponseWriter, r *http.Request) {
	row, name, backup, hasLocal, _, hasRecord, err := p.backupContext(r, PermBackupsDelete)
	if err != nil {
		writeError(w, err)
		return
	}
	var body struct {
		Locked bool `json:"locked"`
	}
	if err := decode(r, &body); err != nil {
		writeError(w, err)
		return
	}
	if hasLocal && !backup.Creating {
		client, _, err := p.client(row.NodeID)
		if err == nil {
			err = client.Do(r.Context(), http.MethodPost, "/api/servers/"+row.ID+"/backups/"+name+"/lock", map[string]bool{"locked": body.Locked}, nil)
		}
		if err != nil {
			writeError(w, err)
			return
		}
	}
	if !hasRecord {
		if err := p.store.AddBackupRecord(BackupRecord{ServerID: row.ID, Name: name, ProjectID: row.ProjectID, Label: backup.Label, Size: backup.Size, CreatedAt: backup.Created, Status: backupDone}); err != nil {
			writeError(w, err)
			return
		}
	}
	if err := p.store.SetBackupLocked(row.ID, name, body.Locked); err != nil {
		writeError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// deleteServerBackup löscht lokal (?where=local), extern (?where=remote) oder beides (Standard).
func (p *Panel) deleteServerBackup(w http.ResponseWriter, r *http.Request) {
	row, name, backup, hasLocal, record, hasRecord, err := p.backupContext(r, PermBackupsDelete)
	if err != nil {
		writeError(w, err)
		return
	}
	where := r.URL.Query().Get("where")
	if where != "" && where != "local" && where != "remote" {
		writeError(w, fail(http.StatusBadRequest, "where ist local, remote oder leer"))
		return
	}
	if backup.Locked || record.Locked {
		writeError(w, fail(http.StatusConflict, "Das Backup ist festgehalten. Entsperre es zuerst, um es zu löschen."))
		return
	}
	if _, running := p.activeJob(row.ID, name); running || backup.Creating {
		writeError(w, fail(http.StatusConflict, "Das Backup wird gerade erstellt oder hochgeladen. Warte, bis es fertig ist."))
		return
	}
	localLeft := hasLocal
	if hasLocal && where != "remote" {
		client, _, err := p.client(row.NodeID)
		if err == nil {
			err = client.Do(r.Context(), http.MethodDelete, "/api/servers/"+row.ID+"/backups/"+name, nil, nil)
		}
		if err != nil {
			writeError(w, err)
			return
		}
		localLeft = false
	}
	remoteLeft := hasRecord && record.RemoteState == backupDone
	if remoteLeft && where != "local" {
		// Liegt es in einem früheren Ziel, vergisst das Panel es nur; im alten Bucket bleibt es liegen
		if record.RemoteTarget == p.targetIdentity(row.ProjectID) {
			target, err := p.backupTarget(row.ProjectID)
			if err == nil {
				ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
				err = p.s3(target).deleteObject(ctx, record.RemoteKey)
				cancel()
			}
			if err != nil {
				writeError(w, err)
				return
			}
		}
		record.RemoteState, record.RemoteKey, record.RemoteTarget, record.Error = "", "", "", ""
		_ = p.store.SetBackupRemote(record)
		remoteLeft = false
	}
	if hasRecord && !localLeft && !remoteLeft {
		_ = p.store.DeleteBackupRecord(row.ID, name)
	}
	w.WriteHeader(http.StatusNoContent)
}

// restoreRemoteBackup lädt ein externes Backup auf den Node und spielt es ein. Der Server muss aus sein.
func (p *Panel) restoreRemoteBackup(w http.ResponseWriter, r *http.Request) {
	row, name, _, _, record, hasRecord, err := p.backupContext(r, PermBackupsRestore)
	if err != nil {
		writeError(w, err)
		return
	}
	if !hasRecord || record.RemoteState != backupDone {
		writeError(w, fail(http.StatusConflict, "Dieses Backup liegt nicht im Backup-Ziel."))
		return
	}
	target, err := p.backupTarget(row.ProjectID)
	if err != nil {
		writeError(w, err)
		return
	}
	if record.RemoteTarget != target.identity() {
		writeError(w, fail(http.StatusConflict, "Dieses Backup liegt in einem früheren Backup-Ziel. Stelle das alte Ziel wieder ein, um es zu laden."))
		return
	}
	client, _, err := p.client(row.NodeID)
	if err != nil {
		writeError(w, err)
		return
	}
	request := node.FetchRequest{URL: p.s3(target).presign(http.MethodGet, record.RemoteKey, nil, presignDownload), Size: record.Size, Restore: true, AllowInternal: p.cfg.AllowInternalBackupTargets}
	if err := client.Do(r.Context(), http.MethodPost, "/api/servers/"+row.ID+"/backups/"+name+"/fetch", request, nil); err != nil {
		writeError(w, err)
		return
	}
	p.invalidate(row.NodeID)
	w.WriteHeader(http.StatusAccepted)
}
