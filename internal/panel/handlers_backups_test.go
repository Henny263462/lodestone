package panel

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/lodestone-mc/lodestone/internal/node"
)

// ---------------------------------------------------------------- Attrappe eines S3-Ziels

// fakeS3 prüft jede Signatur (Kopf und vorsignierte Adresse) unabhängig nach und spielt Multipart-Uploads.
type fakeS3 struct {
	mu       sync.Mutex
	secret   string
	objects  map[string][]byte
	uploads  map[string]map[int][]byte
	aborted  []string
	deleted  []string
	denyPart bool // Teile werden abgelehnt (Hochladen scheitert)
	server   *httptest.Server
}

func newFakeS3(t *testing.T, secret string) *fakeS3 {
	fake := &fakeS3{secret: secret, objects: map[string][]byte{}, uploads: map[string]map[int][]byte{}}
	fake.server = httptest.NewServer(http.HandlerFunc(fake.serve))
	t.Cleanup(fake.server.Close)
	return fake
}

func s3Fail(w http.ResponseWriter, status int, code string) {
	w.WriteHeader(status)
	fmt.Fprintf(w, "<Error><Code>%s</Code></Error>", code)
}

// verify rechnet die Signatur aus der empfangenen Anfrage nach, so wie es ein S3-Dienst tut.
func (f *fakeS3) verify(r *http.Request, body []byte) bool {
	query := r.URL.Query()
	if signature := query.Get("X-Amz-Signature"); signature != "" {
		query.Del("X-Amz-Signature")
		at, err := time.Parse(s3AmzDate, query.Get("X-Amz-Date"))
		if err != nil || query.Get("X-Amz-SignedHeaders") != "host" {
			return false
		}
		region := strings.Split(query.Get("X-Amz-Credential"), "/")[2]
		canonical := strings.Join([]string{r.Method, s3Escape(r.URL.Path, true), s3Query(query), "host:" + r.Host + "\n", "host", s3Unsigned}, "\n")
		want, _ := s3Signature(f.secret, region, at, canonical)
		return want == signature
	}
	auth := r.Header.Get("Authorization")
	var credential, signed, signature string
	for _, part := range strings.Split(strings.TrimPrefix(auth, s3Algorithm+" "), ",") {
		key, value, _ := strings.Cut(part, "=")
		switch key {
		case "Credential":
			credential = value
		case "SignedHeaders":
			signed = value
		case "Signature":
			signature = value
		}
	}
	at, err := time.Parse(s3AmzDate, r.Header.Get("X-Amz-Date"))
	if err != nil || credential == "" {
		return false
	}
	sum := sha256.Sum256(body)
	if r.Header.Get("X-Amz-Content-Sha256") != hex.EncodeToString(sum[:]) {
		return false
	}
	var headers strings.Builder
	for _, name := range strings.Split(signed, ";") {
		value := r.Header.Get(name)
		if name == "host" {
			value = r.Host
		}
		headers.WriteString(name + ":" + strings.TrimSpace(value) + "\n")
	}
	canonical := strings.Join([]string{r.Method, s3Escape(r.URL.Path, true), s3Query(r.URL.Query()), headers.String(), signed, r.Header.Get("X-Amz-Content-Sha256")}, "\n")
	want, _ := s3Signature(f.secret, strings.Split(credential, "/")[2], at, canonical)
	return want == signature
}

func (f *fakeS3) serve(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(r.Body)
	f.mu.Lock()
	defer f.mu.Unlock()
	if !f.verify(r, body) {
		s3Fail(w, 403, "SignatureDoesNotMatch")
		return
	}
	key, query := r.URL.Path, r.URL.Query()
	switch {
	case r.Method == http.MethodPost && query.Has("uploads"):
		id := fmt.Sprintf("upload-%d", len(f.uploads)+1)
		f.uploads[id] = map[int][]byte{}
		fmt.Fprintf(w, "<InitiateMultipartUploadResult><UploadId>%s</UploadId></InitiateMultipartUploadResult>", id)
	case r.Method == http.MethodPut && query.Has("partNumber"):
		parts, ok := f.uploads[query.Get("uploadId")]
		if !ok {
			s3Fail(w, 404, "NoSuchUpload")
			return
		}
		if f.denyPart {
			s3Fail(w, 403, "AccessDenied")
			return
		}
		var number int
		fmt.Sscan(query.Get("partNumber"), &number)
		parts[number] = body
		w.Header().Set("ETag", fmt.Sprintf(`"part-%d"`, number))
	case r.Method == http.MethodPost && query.Has("uploadId"):
		parts, ok := f.uploads[query.Get("uploadId")]
		if !ok {
			s3Fail(w, 404, "NoSuchUpload")
			return
		}
		var complete struct {
			Parts []s3Part `xml:"Part"`
		}
		if err := xml.Unmarshal(body, &complete); err != nil || len(complete.Parts) != len(parts) {
			w.WriteHeader(200) // S3 meldet Fehler beim Zusammensetzen mit 200 im Körper
			fmt.Fprint(w, "<Error><Code>InvalidPart</Code></Error>")
			return
		}
		var object []byte
		for _, part := range complete.Parts {
			if part.ETag != fmt.Sprintf(`"part-%d"`, part.Number) {
				s3Fail(w, 400, "InvalidPart")
				return
			}
			object = append(object, parts[part.Number]...)
		}
		f.objects[key] = object
		delete(f.uploads, query.Get("uploadId"))
		fmt.Fprint(w, "<CompleteMultipartUploadResult><ETag>\"x\"</ETag></CompleteMultipartUploadResult>")
	case r.Method == http.MethodDelete && query.Has("uploadId"):
		f.aborted = append(f.aborted, query.Get("uploadId"))
		delete(f.uploads, query.Get("uploadId"))
		w.WriteHeader(204)
	case r.Method == http.MethodPut:
		f.objects[key] = body
	case r.Method == http.MethodGet:
		object, ok := f.objects[key]
		if !ok {
			s3Fail(w, 404, "NoSuchKey")
			return
		}
		_, _ = w.Write(object)
	case r.Method == http.MethodDelete:
		f.deleted = append(f.deleted, key)
		delete(f.objects, key)
		w.WriteHeader(204)
	default:
		s3Fail(w, 400, "NotImplemented")
	}
}

func (f *fakeS3) keys() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	keys := []string{}
	for key := range f.objects {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

// ---------------------------------------------------------------- Erweiterung des Fake-Nodes

// fakeRemote spielt die Node-Seite: lädt mit den vorsignierten Adressen wirklich zur S3-Attrappe hoch bzw. herunter.
type fakeRemote struct {
	exclude  []string
	transfer node.TransferStatus
	fetched  map[string][]byte
	fetches  []node.FetchRequest
}

func backupContent(size int64) []byte { return bytes.Repeat([]byte("L"), int(size)) }

func registerFakeBackupRoutes(mux *http.ServeMux, fake *fakeNode) *fakeRemote {
	remote := &fakeRemote{fetched: map[string][]byte{}}
	find := func(name string) int {
		for i, backup := range fake.backups {
			if backup.Name == name {
				return i
			}
		}
		return -1
	}
	mux.HandleFunc("POST /api/servers/{id}/backups/{name}/lock", func(w http.ResponseWriter, r *http.Request) {
		var body struct{ Locked bool }
		_ = json.NewDecoder(r.Body).Decode(&body)
		fake.mu.Lock()
		defer fake.mu.Unlock()
		i := find(r.PathValue("name"))
		if i < 0 {
			writeJSON(w, 404, map[string]string{"error": "nicht gefunden"})
			return
		}
		fake.backups[i].Locked = body.Locked
		writeJSON(w, 200, fake.backups[i])
	})
	mux.HandleFunc("DELETE /api/servers/{id}/backups/{name}", func(w http.ResponseWriter, r *http.Request) {
		fake.mu.Lock()
		defer fake.mu.Unlock()
		i := find(r.PathValue("name"))
		if i < 0 {
			writeJSON(w, 404, map[string]string{"error": "nicht gefunden"})
			return
		}
		if fake.backups[i].Locked {
			writeJSON(w, 409, map[string]string{"error": "festgehalten"})
			return
		}
		fake.backups = append(fake.backups[:i], fake.backups[i+1:]...)
		w.WriteHeader(204)
	})
	mux.HandleFunc("POST /api/servers/{id}/backups/{name}/upload", func(w http.ResponseWriter, r *http.Request) {
		var request node.UploadRequest
		_ = json.NewDecoder(r.Body).Decode(&request)
		content := backupContent(request.Size)
		status := node.TransferStatus{State: "done", Kind: "upload", Name: r.PathValue("name"), Total: request.Size, Done: request.Size}
		for i, address := range request.URLs {
			start := int64(i) * request.PartSize
			part := content[start:min(start+request.PartSize, request.Size)]
			call, _ := http.NewRequest(http.MethodPut, address, bytes.NewReader(part))
			response, err := http.DefaultClient.Do(call)
			if err != nil {
				status.State, status.Error = "failed", err.Error()
				break
			}
			response.Body.Close()
			if response.StatusCode >= 300 {
				status.State, status.Error = "failed", fmt.Sprintf("das Backup-Ziel antwortet mit %d", response.StatusCode)
				break
			}
			status.Parts = append(status.Parts, node.UploadedPart{Number: i + 1, ETag: response.Header.Get("ETag")})
		}
		fake.mu.Lock()
		remote.transfer = status
		fake.mu.Unlock()
		writeJSON(w, 202, status)
	})
	mux.HandleFunc("GET /api/servers/{id}/backups/transfer", func(w http.ResponseWriter, r *http.Request) {
		fake.mu.Lock()
		defer fake.mu.Unlock()
		writeJSON(w, 200, remote.transfer)
	})
	mux.HandleFunc("POST /api/servers/{id}/backups/{name}/fetch", func(w http.ResponseWriter, r *http.Request) {
		var request node.FetchRequest
		_ = json.NewDecoder(r.Body).Decode(&request)
		response, err := http.Get(request.URL)
		if err != nil {
			writeJSON(w, 502, map[string]string{"error": err.Error()})
			return
		}
		data, _ := io.ReadAll(response.Body)
		response.Body.Close()
		if response.StatusCode >= 300 {
			writeJSON(w, 502, map[string]string{"error": fmt.Sprintf("das Backup-Ziel antwortet mit %d", response.StatusCode)})
			return
		}
		fake.mu.Lock()
		remote.fetches = append(remote.fetches, request)
		remote.fetched[r.PathValue("name")] = data
		fake.mu.Unlock()
		writeJSON(w, 202, node.TransferStatus{State: "running", Kind: "download"})
	})
	return remote
}

// ---------------------------------------------------------------- Hilfen

const testSecret = "geheimes-secret-1234567890"

func targetBody(endpoint string) map[string]any {
	return map[string]any{"endpoint": endpoint, "region": "eu-central-1", "bucket": "lodestone-backups", "prefix": "/panel/", "accessKey": "AKIATEST", "secretKey": testSecret, "pathStyle": true}
}

func backupList(t *testing.T, c *client, serverID string) []backupDTO {
	t.Helper()
	var list []backupDTO
	if err := json.Unmarshal(c.expect(200, "GET", "/api/servers/"+serverID+"/backups", nil), &list); err != nil {
		t.Fatal(err)
	}
	return list
}

func waitBackups(t *testing.T, c *client, serverID string, done func([]backupDTO) bool) []backupDTO {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		list := backupList(t, c, serverID)
		if done(list) {
			return list
		}
		if time.Now().After(deadline) {
			t.Fatalf("Zeitüberschreitung, Liste: %+v", list)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func memberClient(t *testing.T, p *Panel, admin *client, webURL, projectID, name, role string) *client {
	t.Helper()
	var user User
	_ = json.Unmarshal(admin.expect(201, "POST", "/api/users", map[string]any{"username": name, "password": "ein-gutes-passwort"}), &user)
	if role != "" {
		admin.expect(200, "PUT", "/api/projects/"+projectID+"/members/"+user.ID, map[string]string{"role": role})
	}
	c := newClient(t, webURL)
	c.expect(200, "POST", "/api/auth/login", map[string]string{"username": name, "password": "ein-gutes-passwort"})
	return c
}

// ---------------------------------------------------------------- Tests

func TestBackupTargetSettingsAndPermissions(t *testing.T) {
	p, web, _, nodeID := setupPanel(t)
	p.cfg.AllowInternalBackupTargets = true
	admin := newClient(t, web.URL)
	admin.expect(201, "POST", "/api/auth/setup", map[string]string{"username": "admin", "password": "ein-gutes-passwort"})
	var project ProjectDTO
	_ = json.Unmarshal(admin.expect(201, "POST", "/api/projects", map[string]string{"name": "Ziel", "kind": "server"}), &project)
	admin.expect(201, "POST", "/api/projects/"+project.ID+"/servers", serverRequest{Name: "Main", NodeID: nodeID, Software: "paper", Version: "1.21.4", EULA: true})
	operator := memberClient(t, p, admin, web.URL, project.ID, "operator", "operator")
	stranger := memberClient(t, p, admin, web.URL, project.ID, "fremd", "")
	bucket := newFakeS3(t, testSecret)
	path := "/api/projects/" + project.ID + "/backup-target"

	var empty backupTargetDTO
	_ = json.Unmarshal(admin.expect(200, "GET", path, nil), &empty)
	if empty.Configured {
		t.Fatal("ohne Ziel darf nichts eingerichtet sein")
	}
	operator.expect(403, "GET", path, nil)
	operator.expect(403, "PUT", path, targetBody(bucket.server.URL))
	stranger.expect(404, "PUT", path, targetBody(bucket.server.URL))

	for field, value := range map[string]any{"endpoint": "ftp://x", "bucket": "a", "region": "EU Central", "prefix": "../x", "accessKey": "mit leerzeichen"} {
		body := targetBody(bucket.server.URL)
		body[field] = value
		admin.expect(400, "PUT", path, body)
	}
	unknown := targetBody(bucket.server.URL)
	unknown["extra"] = true
	admin.expect(400, "PUT", path, unknown)
	noSecret := targetBody(bucket.server.URL)
	noSecret["secretKey"] = ""
	admin.expect(400, "PUT", path, noSecret) // beim ersten Speichern ist das Secret Pflicht

	var saved backupTargetDTO
	data := admin.expect(200, "PUT", path, targetBody(bucket.server.URL+"/"))
	_ = json.Unmarshal(data, &saved)
	if !saved.Configured || !saved.HasSecret || saved.Prefix != "panel/" || saved.Endpoint != bucket.server.URL || strings.Contains(string(data), testSecret) {
		t.Fatalf("gespeichert: %s", data)
	}
	if got := admin.expect(200, "GET", path, nil); strings.Contains(string(got), testSecret) {
		t.Fatal("das Secret darf nie an den Browser gehen")
	}
	stored, _ := p.store.BackupTarget(project.ID)
	if stored.SecretSealed == "" || strings.Contains(stored.SecretSealed, testSecret) {
		t.Fatal("das Secret muss verschlüsselt in der Datenbank liegen")
	}

	// Ohne neues Secret bleibt das alte; der Verbindungstest prüft vorsignierte Adresse und Signatur
	admin.expect(200, "PUT", path, noSecret)
	admin.expect(200, "POST", path+"/test", noSecret)
	if len(bucket.deleted) != 1 || bucket.deleted[0] != "/lodestone-backups/panel/.lodestone-test" {
		t.Errorf("Testdatei: %v", bucket.deleted)
	}
	wrong := targetBody(bucket.server.URL)
	wrong["secretKey"] = "falsches-secret"
	if status, body := admin.do("POST", path+"/test", wrong); status != http.StatusBadGateway || !strings.Contains(string(body), "Signatur") {
		t.Errorf("falsches Secret: %d %s", status, body)
	}
	operator.expect(403, "POST", path+"/test", noSecret)

	// Interne Adressen sind gesperrt, solange es nicht erlaubt ist
	p.cfg.AllowInternalBackupTargets = false
	if status, body := admin.do("POST", path+"/test", noSecret); status != http.StatusBadGateway || !strings.Contains(string(body), "gesperrt") {
		t.Errorf("internes Ziel: %d %s", status, body)
	}
	operator.expect(403, "DELETE", path, nil)
	admin.expect(204, "DELETE", path, nil)
}

func TestBackupSettingsValidation(t *testing.T) {
	p, admin, _, _, server, projectID := serverProject(t)
	path := "/api/servers/" + server.ID + "/backup-settings"
	var settings backupSettingsDTO
	_ = json.Unmarshal(admin.expect(200, "GET", path, nil), &settings)
	if settings.RemoteKeep != defaultRemoteKeep || settings.Exclude == nil {
		t.Fatalf("Standard: %+v", settings)
	}
	admin.expect(400, "PUT", path, map[string]any{"exclude": []string{"../welt"}, "remoteKeep": 3})
	admin.expect(400, "PUT", path, map[string]any{"exclude": []string{"/abs"}, "remoteKeep": 3})
	admin.expect(400, "PUT", path, map[string]any{"exclude": []string{}, "remoteKeep": 0})
	_ = json.Unmarshal(admin.expect(200, "PUT", path, map[string]any{"exclude": []string{" dynmap/** ", "", "logs/**", "logs/**"}, "remoteKeep": 3}), &settings)
	if strings.Join(settings.Exclude, ",") != "dynmap/**,logs/**" || settings.RemoteKeep != 3 {
		t.Fatalf("gespeichert: %+v", settings)
	}
	operator := memberClient(t, p, admin, admin.base, projectID, "operator", "operator")
	operator.expect(200, "GET", path, nil)
	operator.expect(403, "PUT", path, map[string]any{"exclude": []string{}, "remoteKeep": 3})
}

func TestBackupUploadLockDeleteAndRestoreRemote(t *testing.T) {
	p, admin, fake, _, server, projectID := serverProject(t)
	p.cfg.AllowInternalBackupTargets = true
	bucket := newFakeS3(t, testSecret)
	base := "/api/servers/" + server.ID + "/backups"

	// Ohne Ziel geht „auch extern“ nicht
	admin.expect(400, "POST", base, map[string]any{"label": "x", "remote": true})
	admin.expect(200, "PUT", "/api/projects/"+projectID+"/backup-target", targetBody(bucket.server.URL))
	admin.expect(200, "PUT", "/api/servers/"+server.ID+"/backup-settings", map[string]any{"exclude": []string{"dynmap/**"}, "remoteKeep": 1})

	admin.expect(202, "POST", base, map[string]any{"label": "eins", "remote": true})
	list := waitBackups(t, admin, server.ID, func(list []backupDTO) bool { return len(list) == 1 && list[0].Upload == backupDone })
	first := list[0]
	if !first.Local || !first.Remote || first.Status != backupDone || first.Size != 5<<20 || first.Locked {
		t.Fatalf("Eintrag: %+v", first)
	}
	fake.mu.Lock()
	exclude := strings.Join(fake.remote.exclude, ",")
	fake.mu.Unlock()
	if exclude != "dynmap/**" {
		t.Errorf("die Ausschlüsse müssen an den Node gehen: %q", exclude)
	}
	key := "/lodestone-backups/panel/" + server.ID + "/" + first.Name
	if keys := bucket.keys(); len(keys) != 1 || keys[0] != key {
		t.Fatalf("Objekte im Bucket: %v", keys)
	}
	if object := bucket.objects[key]; !bytes.Equal(object, backupContent(5<<20)) {
		t.Fatal("das Objekt im Bucket stimmt nicht mit dem Archiv überein")
	}

	// Festhalten: nicht löschbar, und die Aufbewahrung (extern 1) lässt es stehen
	admin.expect(204, "POST", base+"/"+first.Name+"/lock", map[string]bool{"locked": true})
	admin.expect(409, "DELETE", base+"/"+first.Name, nil)
	admin.expect(202, "POST", base, map[string]any{"label": "zwei", "remote": true})
	list = waitBackups(t, admin, server.ID, func(list []backupDTO) bool { return len(list) == 2 && list[0].Upload == backupDone })
	if !list[1].Locked || !list[1].Remote || len(bucket.keys()) != 2 {
		t.Fatalf("festgehaltenes Backup muss extern bleiben: %+v %v", list, bucket.keys())
	}
	admin.expect(202, "POST", base, map[string]any{"label": "drei", "remote": true})
	list = waitBackups(t, admin, server.ID, func(list []backupDTO) bool { return len(list) == 3 && list[0].Upload == backupDone })
	if list[1].Remote || !list[1].Local || !list[2].Remote {
		t.Fatalf("extern bleibt nur das neueste (plus das festgehaltene): %+v", list)
	}

	// Entsperren, dann lokal löschen: extern bleibt es und lässt sich wiederherstellen
	admin.expect(204, "POST", base+"/"+first.Name+"/lock", map[string]bool{"locked": false})
	admin.expect(204, "DELETE", base+"/"+first.Name+"?where=local", nil)
	found := false
	for _, entry := range backupList(t, admin, server.ID) {
		if entry.Name == first.Name {
			found = true
			if entry.Local || !entry.Remote {
				t.Fatalf("nur extern: %+v", entry)
			}
		}
	}
	if !found {
		t.Fatal("das externe Backup fehlt in der Liste")
	}
	admin.expect(202, "POST", base+"/"+first.Name+"/restore-remote", nil)
	fake.mu.Lock()
	fetched := fake.remote.fetched[first.Name]
	fetches := append([]node.FetchRequest{}, fake.remote.fetches...)
	fake.mu.Unlock()
	if len(fetches) != 1 || !fetches[0].Restore || fetches[0].Size != 5<<20 || !bytes.Equal(fetched, backupContent(5<<20)) {
		t.Fatalf("Wiederherstellen: %+v (%d Bytes)", fetches, len(fetched))
	}
	if strings.Contains(fetches[0].URL, testSecret) {
		t.Fatal("das Secret darf nicht zum Node")
	}

	// Extern löschen entfernt das Objekt und den Eintrag
	admin.expect(204, "DELETE", base+"/"+first.Name, nil)
	if keys := bucket.keys(); len(keys) != 1 {
		t.Fatalf("nach dem Löschen: %v", keys)
	}
	if list := backupList(t, admin, server.ID); len(list) != 2 {
		t.Fatalf("Liste nach dem Löschen: %+v", list)
	}
	admin.expect(404, "DELETE", base+"/"+first.Name, nil)
	admin.expect(400, "DELETE", base+"/..%2Fx", nil)

	// Nachträglich hochladen (mit größerer Aufbewahrung, sonst fiele das ältere sofort wieder heraus)
	admin.expect(200, "PUT", "/api/servers/"+server.ID+"/backup-settings", map[string]any{"exclude": []string{}, "remoteKeep": 5})
	middle := list[1]
	if middle.Remote {
		t.Fatalf("das mittlere Backup darf extern nicht mehr liegen: %+v", middle)
	}
	admin.expect(202, "POST", base+"/"+middle.Name+"/upload", nil)
	waitBackups(t, admin, server.ID, func(list []backupDTO) bool {
		for _, entry := range list {
			if entry.Name == middle.Name && entry.Upload == backupDone {
				return true
			}
		}
		return false
	})
	kinds := eventKindsOf(t, p, projectID)
	if len(kinds) != 4 || kinds[0] != "backup" || kinds[3] != "backup" {
		t.Errorf("Ereignisse: %v", kinds)
	}
}

func TestBackupUploadFailureIsReportedAndAborted(t *testing.T) {
	p, admin, _, _, server, projectID := serverProject(t)
	p.cfg.AllowInternalBackupTargets = true
	bucket := newFakeS3(t, testSecret)
	bucket.denyPart = true
	admin.expect(200, "PUT", "/api/projects/"+projectID+"/backup-target", targetBody(bucket.server.URL))
	admin.expect(202, "POST", "/api/servers/"+server.ID+"/backups", map[string]any{"remote": true})
	list := waitBackups(t, admin, server.ID, func(list []backupDTO) bool { return len(list) == 1 && list[0].Upload == backupFailed })
	if !list[0].Local || list[0].Remote || !strings.Contains(list[0].Error, "403") {
		t.Fatalf("Eintrag: %+v", list[0])
	}
	bucket.mu.Lock()
	aborted := len(bucket.aborted)
	bucket.mu.Unlock()
	if aborted != 1 {
		t.Error("ein gescheiterter Upload muss im Ziel verworfen werden")
	}
	waitFor := time.Now().Add(2 * time.Second)
	for {
		kinds := eventKindsOf(t, p, projectID)
		if len(kinds) == 1 && kinds[0] == "backup_failed" {
			break
		}
		if time.Now().After(waitFor) {
			t.Fatalf("Ereignisse: %v", kinds)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestBackupPermissionsAndInterruptedUploads(t *testing.T) {
	p, admin, fake, _, server, projectID := serverProject(t)
	base := "/api/servers/" + server.ID + "/backups"
	viewer := memberClient(t, p, admin, admin.base, projectID, "viewer", "viewer")
	stranger := memberClient(t, p, admin, admin.base, projectID, "fremd", "")
	viewer.expect(200, "GET", base, nil)
	viewer.expect(403, "POST", base, map[string]any{"label": "x"})
	stranger.expect(404, "GET", base, nil)
	admin.expect(400, "POST", base, map[string]any{"label": "x", "unbekannt": 1})
	admin.expect(202, "POST", base, map[string]any{"label": "a"})
	list := waitBackups(t, admin, server.ID, func(list []backupDTO) bool { return len(list) == 1 && list[0].Status == backupDone })
	viewer.expect(403, "POST", base+"/"+list[0].Name+"/lock", map[string]bool{"locked": true})
	viewer.expect(403, "DELETE", base+"/"+list[0].Name, nil)
	viewer.expect(403, "POST", base+"/"+list[0].Name+"/restore-remote", nil)
	admin.expect(400, "POST", base+"/"+list[0].Name+"/upload", nil) // kein Ziel eingerichtet
	admin.expect(409, "POST", base+"/"+list[0].Name+"/restore-remote", nil)

	// Ein Upload, der beim Neustart des Panels lief, gilt danach als unterbrochen
	for deadline := time.Now().Add(2 * time.Second); ; time.Sleep(5 * time.Millisecond) {
		if _, running := p.activeJob(server.ID, list[0].Name); !running || time.Now().After(deadline) {
			break
		}
	}
	record, _ := p.store.BackupRecord(server.ID, list[0].Name)
	record.RemoteState = backupRunning
	_ = p.store.SetBackupRemote(record)
	list = backupList(t, admin, server.ID)
	if list[0].Upload != backupFailed || !strings.Contains(list[0].Error, "unterbrochen") {
		t.Fatalf("unterbrochen: %+v", list[0])
	}

	// Lokal gelöschte Backups ohne externe Kopie verschwinden aus der Liste
	fake.mu.Lock()
	fake.backups = nil
	fake.mu.Unlock()
	_ = p.store.SetBackupRemote(BackupRecord{ServerID: server.ID, Name: list[0].Name})
	if list := backupList(t, admin, server.ID); len(list) != 0 {
		t.Fatalf("veraltete Einträge: %+v", list)
	}
}

func TestScheduledBackupUploads(t *testing.T) {
	p, admin, _, _, server, projectID := serverProject(t)
	p.cfg.AllowInternalBackupTargets = true
	bucket := newFakeS3(t, testSecret)
	admin.expect(200, "PUT", "/api/projects/"+projectID+"/backup-target", targetBody(bucket.server.URL))
	var created scheduleDTO
	_ = json.Unmarshal(admin.expect(201, "POST", "/api/projects/"+projectID+"/schedules", map[string]any{
		"serverId": server.ID, "name": "Extern", "kind": "daily", "atMinute": 60, "steps": []Step{{Action: "backup", Value: "nachts", Remote: true}},
	}), &created)
	if len(created.Steps) != 1 || !created.Steps[0].Remote {
		t.Fatalf("Schritt: %+v", created.Steps)
	}
	schedule, _ := p.store.ScheduleByID(created.ID)
	p.runSchedule(context.Background(), schedule)
	after, _ := p.store.ScheduleByID(created.ID)
	if after.LastStatus != "ok" || len(bucket.keys()) != 1 {
		t.Fatalf("Lauf: %+v, Objekte: %v", after, bucket.keys())
	}
	if kinds := eventKindsOf(t, p, projectID); len(kinds) != 1 || kinds[0] != "backup" {
		t.Errorf("Ereignisse: %v", kinds)
	}
}
