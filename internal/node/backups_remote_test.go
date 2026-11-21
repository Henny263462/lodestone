package node

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestMatchExclude(t *testing.T) {
	cases := []struct {
		patterns []string
		name     string
		want     bool
	}{
		{[]string{"dynmap/**"}, "dynmap", true},
		{[]string{"dynmap/**"}, "dynmap/web/tiles/a.png", true},
		{[]string{"dynmap/**"}, "plugins/dynmap/x", false},
		{[]string{"plugins/dynmap/web/**"}, "plugins/dynmap/web/tiles/1.png", true},
		{[]string{"plugins/dynmap/web/**"}, "plugins/dynmap/config.txt", false},
		{[]string{"logs/"}, "logs/latest.log", true},
		{[]string{"*.log"}, "logs/latest.log", true},
		{[]string{"*.log"}, "server.properties", false},
		{[]string{"cache"}, "plugins/x/cache", true},
		{[]string{"**/cache/**"}, "a/b/cache/c", true},
		{[]string{"**/cache/**"}, "cache", true},
		{[]string{"world_*/region"}, "world_nether/region", true},
		{[]string{"world_*/region"}, "world/region", false},
		{nil, "world", false},
	}
	for _, c := range cases {
		if got := matchExclude(c.patterns, c.name); got != c.want {
			t.Errorf("matchExclude(%v, %q) = %v, erwartet %v", c.patterns, c.name, got, c.want)
		}
	}
}

func TestValidateExcludes(t *testing.T) {
	for _, bad := range [][]string{{"../welt"}, {"/abs"}, {""}, {"a/../b"}, {"a\\b"}, {"[a"}, {strings.Repeat("a", 201)}, {"a//b"}} {
		if err := validateExcludes(bad); !errors.Is(err, errBadRequest) {
			t.Errorf("%q wurde akzeptiert: %v", bad, err)
		}
	}
	many := make([]string, maxExcludes+1)
	for i := range many {
		many[i] = fmt.Sprintf("x%d", i)
	}
	if validateExcludes(many) == nil {
		t.Error("zu viele Ausschlüsse wurden akzeptiert")
	}
	if err := validateExcludes([]string{"dynmap/**", "logs/", "*.log", "plugins/*/cache/**"}); err != nil {
		t.Errorf("gültige Muster abgelehnt: %v", err)
	}
}

func archiveNames(t *testing.T, file string) []string {
	t.Helper()
	data, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	gz, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	reader := tar.NewReader(gz)
	names := []string{}
	for {
		header, err := reader.Next()
		if errors.Is(err, io.EOF) {
			return names
		}
		if err != nil {
			t.Fatal(err)
		}
		names = append(names, header.Name)
	}
}

func TestBackupExcludesAndRestoreKeepsExcluded(t *testing.T) {
	manager := newTestManager(t, testConfig(t), newFakeRuntime())
	server := createPaper(t, manager, "Ausschluss")
	put(t, server, "world/level.dat", []byte("alt"))
	put(t, server, "dynmap/tiles/a.png", []byte("kachel"))
	put(t, server, "plugins/dynmap/web/b.png", []byte("kachel"))
	put(t, server, "plugins/dynmap/config.txt", []byte("einstellung"))
	put(t, server, "logs/latest.log", []byte("log"))

	if _, err := server.CreateBackupExcluding("ohne", []string{"../x"}); !errors.Is(err, errBadRequest) {
		t.Fatalf("ungültiges Muster: %v", err)
	}
	if _, err := server.CreateBackupExcluding("ohne", []string{"dynmap/**", "plugins/dynmap/web/**", "*.log"}); err != nil {
		t.Fatal(err)
	}
	list := waitBackup(t, server, 1)
	file, _ := server.BackupFile(list[0].Name)
	names := strings.Join(archiveNames(t, file), " ")
	if !strings.HasPrefix(names, backupMetaName) {
		t.Errorf("die Angaben müssen am Anfang des Archivs stehen: %s", names)
	}
	for _, missing := range []string{" dynmap/", "a.png", "b.png", "latest.log"} {
		if strings.Contains(names, missing) {
			t.Errorf("%s hätte fehlen müssen: %s", missing, names)
		}
	}
	if !strings.Contains(names, "plugins/dynmap/config.txt") || !strings.Contains(names, "world/level.dat") {
		t.Errorf("es fehlt etwas: %s", names)
	}

	// Danach ändert sich der Server; die ausgeschlossenen Dateien müssen das Einspielen überstehen
	_ = server.FS().Write("world/level.dat", strings.NewReader("neu"), 100, false)
	put(t, server, "dynmap/tiles/neu.png", []byte("neue kachel"))
	put(t, server, "extra.txt", []byte("weg damit"))
	if err := server.RestoreBackup(list[0].Name); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "Wiederherstellung", func() bool { return server.View().State == StateOffline && !server.backupBusy.Load() })
	if got := read(t, server.FS(), "world/level.dat"); got != "alt" {
		t.Errorf("level.dat = %q", got)
	}
	if got := read(t, server.FS(), "dynmap/tiles/neu.png"); got != "neue kachel" {
		t.Errorf("ausgeschlossener Ordner wurde angetastet: %q", got)
	}
	if got := read(t, server.FS(), "plugins/dynmap/web/b.png"); got != "kachel" {
		t.Errorf("ausgeschlossener Unterordner wurde angetastet: %q", got)
	}
	if got := read(t, server.FS(), "plugins/dynmap/config.txt"); got != "einstellung" {
		t.Errorf("config.txt = %q", got)
	}
	if server.FS().Exists("extra.txt") || server.FS().Exists(backupMetaName) || server.FS().Exists(restoreTemp) {
		t.Error("neue Dateien, Angaben oder Hilfsordner sind übrig")
	}
}

func TestLockedBackupsSurvivePruneAndDelete(t *testing.T) {
	manager := newTestManager(t, testConfig(t), newFakeRuntime())
	server := createPaper(t, manager, "Festhalten")
	keep := 1
	if err := server.Update(Patch{BackupKeep: &keep}); err != nil {
		t.Fatal(err)
	}
	if _, err := server.CreateBackup("erstes"); err != nil {
		t.Fatal(err)
	}
	first := waitBackup(t, server, 1)[0]
	if locked, err := server.SetBackupLocked(first.Name, true); err != nil || !locked.Locked {
		t.Fatalf("festhalten: %+v %v", locked, err)
	}
	if err := server.DeleteBackup(first.Name); !errors.Is(err, ErrConflict) {
		t.Fatalf("Löschen eines festgehaltenen Backups: %v", err)
	}
	time.Sleep(1100 * time.Millisecond) // anderer Zeitstempel im Namen
	if _, err := server.CreateBackup("zweites"); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "Backup fertig", func() bool { return !server.backupBusy.Load() })
	list := waitBackup(t, server, 2) // Aufbewahrung 1, aber das festgehaltene zählt nicht
	if list[1].Name != first.Name || !list[1].Locked || list[0].Locked {
		t.Fatalf("Liste: %+v", list)
	}
	if _, err := server.SetBackupLocked(first.Name, false); err != nil {
		t.Fatal(err)
	}
	if err := server.DeleteBackup(first.Name); err != nil {
		t.Fatalf("nach dem Entsperren muss Löschen gehen: %v", err)
	}
	if _, err := server.SetBackupLocked("../spec.json", true); err == nil {
		t.Error("ungültiger Name wurde akzeptiert")
	}
}

// fakeBucket nimmt Teile per PUT an und liefert Objekte per GET aus.
type fakeBucket struct {
	mu      sync.Mutex
	parts   map[string][]byte
	objects map[string][]byte
	fail    int // so viele Anfragen scheitern zuerst mit 500
}

func newFakeBucket(t *testing.T) (*fakeBucket, *httptest.Server) {
	bucket := &fakeBucket{parts: map[string][]byte{}, objects: map[string][]byte{}}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		bucket.mu.Lock()
		defer bucket.mu.Unlock()
		if bucket.fail > 0 {
			bucket.fail--
			w.WriteHeader(500)
			_, _ = w.Write([]byte("<Error><Code>InternalError</Code></Error>"))
			return
		}
		if r.URL.Query().Get("X-Amz-Signature") == "" {
			w.WriteHeader(403)
			_, _ = w.Write([]byte("<Error><Code>AccessDenied</Code></Error>"))
			return
		}
		switch r.Method {
		case http.MethodPut:
			data, _ := io.ReadAll(r.Body)
			key := r.URL.Path + "#" + r.URL.Query().Get("partNumber")
			bucket.parts[key] = data
			w.Header().Set("ETag", fmt.Sprintf(`"etag-%s-%d"`, r.URL.Query().Get("partNumber"), len(data)))
		case http.MethodGet:
			data, ok := bucket.objects[r.URL.Path]
			if !ok {
				w.WriteHeader(404)
				_, _ = w.Write([]byte("<Error><Code>NoSuchKey</Code></Error>"))
				return
			}
			_, _ = w.Write(data)
		}
	}))
	t.Cleanup(server.Close)
	return bucket, server
}

func TestUploadBackupInParts(t *testing.T) {
	transferRetryWait = time.Millisecond
	manager := newTestManager(t, testConfig(t), newFakeRuntime())
	server := createPaper(t, manager, "Hochladen")
	noise := make([]byte, 6<<20) // nicht komprimierbar: das Archiv wird größer als ein Teil
	_, _ = rand.Read(noise)
	put(t, server, "world/noise.bin", noise)
	if _, err := server.CreateBackup("gross"); err != nil {
		t.Fatal(err)
	}
	backup := waitBackup(t, server, 1)[0]
	bucket, remote := newFakeBucket(t)
	bucket.fail = 1 // der erste Versuch scheitert, der zweite klappt
	part := int64(5 << 20)
	urls := []string{remote.URL + "/b/k?partNumber=1&X-Amz-Signature=x", remote.URL + "/b/k?partNumber=2&X-Amz-Signature=x"}

	if err := server.UploadBackup(backup.Name, UploadRequest{Size: backup.Size + 1, PartSize: part, URLs: urls, AllowInternal: true}); !errors.Is(err, errBadRequest) {
		t.Fatalf("falsche Größe: %v", err)
	}
	if err := server.UploadBackup(backup.Name, UploadRequest{Size: backup.Size, PartSize: part, URLs: urls[:1], AllowInternal: true}); !errors.Is(err, errBadRequest) {
		t.Fatalf("zu wenige Adressen: %v", err)
	}
	if err := server.UploadBackup(backup.Name, UploadRequest{Size: backup.Size, PartSize: part, URLs: urls, AllowInternal: true}); err != nil {
		t.Fatal(err)
	}
	if err := server.UploadBackup(backup.Name, UploadRequest{Size: backup.Size, PartSize: part, URLs: urls, AllowInternal: true}); !errors.Is(err, ErrBadState) {
		t.Errorf("zweite Übertragung gleichzeitig: %v", err)
	}
	waitFor(t, "Hochladen", func() bool { return server.transferStatus().State != "running" })
	status := server.transferStatus()
	if status.State != "done" || len(status.Parts) != 2 || status.Done != backup.Size || status.Parts[1].Number != 2 {
		t.Fatalf("Stand: %+v", status)
	}
	file, _ := server.BackupFile(backup.Name)
	original, _ := os.ReadFile(file)
	bucket.mu.Lock()
	joined := append(append([]byte{}, bucket.parts["/b/k#1"]...), bucket.parts["/b/k#2"]...)
	bucket.mu.Unlock()
	if !bytes.Equal(joined, original) || int64(len(bucket.parts["/b/k#1"])) != part {
		t.Fatalf("die Teile ergeben nicht das Archiv (%d + %d Bytes)", len(bucket.parts["/b/k#1"]), len(bucket.parts["/b/k#2"]))
	}

	// Lehnt das Ziel ab, steht der Grund im Stand
	if err := server.UploadBackup(backup.Name, UploadRequest{Size: backup.Size, PartSize: part, URLs: []string{remote.URL + "/b/k?partNumber=1", remote.URL + "/b/k?partNumber=2"}, AllowInternal: true}); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "Fehlschlag", func() bool { return server.transferStatus().State != "running" })
	if status := server.transferStatus(); status.State != "failed" || !strings.Contains(status.Error, "AccessDenied") {
		t.Fatalf("Stand nach Ablehnung: %+v", status)
	}
}

func TestFetchBackupAndRestore(t *testing.T) {
	manager := newTestManager(t, testConfig(t), newFakeRuntime())
	server := createPaper(t, manager, "Laden")
	put(t, server, "world/level.dat", []byte("aus der cloud"))
	if _, err := server.CreateBackup("extern"); err != nil {
		t.Fatal(err)
	}
	backup := waitBackup(t, server, 1)[0]
	file, _ := server.BackupFile(backup.Name)
	archive, _ := os.ReadFile(file)
	if err := os.Remove(file); err != nil { // nur noch extern vorhanden
		t.Fatal(err)
	}
	_ = server.FS().Write("world/level.dat", strings.NewReader("lokal geändert"), 100, false)

	bucket, remote := newFakeBucket(t)
	bucket.objects["/b/"+backup.Name] = archive
	address := remote.URL + "/b/" + backup.Name + "?X-Amz-Signature=x"

	// Unvollständige Daten werden verworfen
	if err := server.FetchBackup(backup.Name, FetchRequest{URL: address, Size: int64(len(archive)) + 10, Restore: true, AllowInternal: true}); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "Abbruch", func() bool { return server.View().State == StateOffline && !server.backupBusy.Load() })
	if server.View().LastError == "" || read(t, server.FS(), "world/level.dat") != "lokal geändert" {
		t.Fatalf("ein unvollständiges Backup darf nichts ändern: %q", server.View().LastError)
	}
	if _, err := os.Stat(filepath.Join(server.backupDir(), backup.Name+".part")); err == nil {
		t.Error("die halbe Datei ist liegen geblieben")
	}

	_ = server.Start()
	waitState(t, server, StateRunning)
	if err := server.FetchBackup(backup.Name, FetchRequest{URL: address, Size: int64(len(archive)), Restore: true, AllowInternal: true}); !errors.Is(err, ErrBadState) {
		t.Fatalf("Einspielen bei laufendem Server: %v", err)
	}
	_ = server.Stop()
	waitState(t, server, StateOffline)

	if err := server.FetchBackup(backup.Name, FetchRequest{URL: address, Size: int64(len(archive)), Restore: true, AllowInternal: true}); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "Wiederherstellung", func() bool { return server.View().State == StateOffline && !server.backupBusy.Load() })
	if got := read(t, server.FS(), "world/level.dat"); got != "aus der cloud" {
		t.Fatalf("level.dat = %q (%s)", got, server.View().LastError)
	}
	if _, err := server.BackupFile(backup.Name); err != nil {
		t.Error("das geladene Backup muss danach lokal liegen")
	}
	if err := server.FetchBackup("../x", FetchRequest{URL: address, Size: 1}); !errors.Is(err, errBadRequest) {
		t.Errorf("ungültiger Name: %v", err)
	}
	if err := server.FetchBackup(backup.Name, FetchRequest{URL: "file:///etc/passwd", Size: 1}); !errors.Is(err, errBadRequest) {
		t.Errorf("ungültige Adresse: %v", err)
	}
}

func TestBackupRemoteRoutes(t *testing.T) {
	api := newTestAPI(t)
	server, err := api.manager.Create(Spec{Name: "Routen", Software: "paper", Version: "1.21.8", EULA: true})
	if err != nil {
		t.Fatal(err)
	}
	waitFor(t, "Installation", func() bool { return server.View().Installed })
	base := "/api/servers/" + server.ID()
	if status, data := api.do("POST", base+"/backups", map[string]any{"label": "a", "exclude": []string{"logs/**"}}); status != http.StatusAccepted {
		t.Fatalf("anlegen: %d %s", status, data)
	}
	if status, _ := api.do("POST", base+"/backups", map[string]any{"exclude": []string{"../x"}}); status != http.StatusBadRequest {
		t.Errorf("ungültiger Ausschluss: %d", status)
	}
	backup := waitBackup(t, server, 1)[0]
	if status, data := api.do("POST", base+"/backups/"+backup.Name+"/lock", map[string]bool{"locked": true}); status != 200 || !strings.Contains(string(data), `"locked":true`) {
		t.Fatalf("festhalten: %d %s", status, data)
	}
	if status, _ := api.do("DELETE", base+"/backups/"+backup.Name, nil); status != http.StatusConflict {
		t.Errorf("Löschen eines festgehaltenen Backups: %d", status)
	}
	if status, data := api.do("GET", base+"/backups/transfer", nil); status != 200 || !strings.Contains(string(data), `"idle"`) {
		t.Errorf("Stand: %d %s", status, data)
	}
	if status, _ := api.do("POST", base+"/backups/"+backup.Name+"/upload", map[string]any{"size": 1, "partSize": 1, "urls": []string{"x"}}); status != http.StatusBadRequest {
		t.Errorf("ungültiger Upload: %d", status)
	}
}

// Den Endpunkt des Backup-Ziels stellt ein Projekt-Admin ein. Ohne Freigabe spricht der Node damit nie interne
// Adressen an (hier 127.0.0.1), weder beim Hochladen noch beim Laden, und die Meldung nennt keine Adresse.
func TestTransfersRefuseInternalTargets(t *testing.T) {
	transferRetryWait = time.Millisecond
	manager := newTestManager(t, testConfig(t), newFakeRuntime())
	server := createPaper(t, manager, "Intern")
	put(t, server, "world/level.dat", []byte("x"))
	if _, err := server.CreateBackup("intern"); err != nil {
		t.Fatal(err)
	}
	backup := waitBackup(t, server, 1)[0]
	hits := 0
	var mu sync.Mutex
	internal := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		hits++
		mu.Unlock()
		w.Header().Set("ETag", `"x"`)
	}))
	t.Cleanup(internal.Close)

	if err := server.UploadBackup(backup.Name, UploadRequest{Size: backup.Size, PartSize: backup.Size, URLs: []string{internal.URL + "/b/k?partNumber=1"}}); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "Hochladen", func() bool { return server.transferStatus().State != "running" })
	status := server.transferStatus()
	if status.State != "failed" || !strings.Contains(status.Error, "gesperrt") || strings.Contains(status.Error, "127.0.0.1") {
		t.Fatalf("Hochladen ins interne Netz: %+v", status)
	}
	if err := server.FetchBackup("20260101-000001.tar.gz", FetchRequest{URL: internal.URL + "/b/x.tar.gz", Size: 10}); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "Laden", func() bool { return server.transferStatus().State != "running" })
	if status := server.transferStatus(); status.State != "failed" || !strings.Contains(status.Error, "gesperrt") {
		t.Fatalf("Laden aus dem internen Netz: %+v", status)
	}
	mu.Lock()
	defer mu.Unlock()
	if hits != 0 {
		t.Fatalf("das interne Ziel wurde %d-mal erreicht", hits)
	}
	if !internalIP(net.ParseIP("169.254.169.254")) || !internalIP(net.ParseIP("100.64.1.1")) || !internalIP(net.ParseIP("fd00::1")) || internalIP(net.ParseIP("203.0.113.5")) {
		t.Error("Erkennung interner Adressen")
	}
}

// Ein Backup aus dem eigenen Bucket kann eine Gzip-Bombe sein (gleiche Größe, anderer Inhalt). Entpackt darf es nicht
// mehr Platz belegen als frei ist; die alten Dateien bleiben, und die geladene Datei bleibt nicht liegen.
func TestRestoreStopsAGzipBomb(t *testing.T) {
	manager := newTestManager(t, testConfig(t), newFakeRuntime())
	server := createPaper(t, manager, "Bombe")
	put(t, server, "world/level.dat", []byte("vorher"))
	manager.ops.mu.Lock()
	manager.ops.restoreLimit = 1 << 20
	manager.ops.mu.Unlock()

	var archive bytes.Buffer
	gz := gzip.NewWriter(&archive)
	tw := tar.NewWriter(gz)
	zeros := make([]byte, 4<<20) // komprimiert wenige KB, entpackt 4 MB
	_ = tw.WriteHeader(&tar.Header{Name: "world/bombe.bin", Mode: 0o644, Size: int64(len(zeros)), Typeflag: tar.TypeReg})
	_, _ = tw.Write(zeros)
	_ = tw.Close()
	_ = gz.Close()
	name := "20260101-000000.tar.gz"
	bucket, remote := newFakeBucket(t)
	bucket.objects["/b/"+name] = archive.Bytes()

	if err := server.FetchBackup(name, FetchRequest{URL: remote.URL + "/b/" + name + "?X-Amz-Signature=x", Size: int64(archive.Len()), Restore: true, AllowInternal: true}); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "Einspielen", func() bool { return server.View().State == StateOffline && !server.backupBusy.Load() })
	if !strings.Contains(server.View().LastError, "größer als der freie Platz") || read(t, server.FS(), "world/level.dat") != "vorher" {
		t.Fatalf("Bombe: %q", server.View().LastError)
	}
	if _, err := server.BackupFile(name); err == nil {
		t.Error("die Datei, die sich nicht einspielen ließ, liegt noch als Backup herum")
	}
	if entries, _ := server.FS().List("."); len(entries) == 0 {
		t.Fatal("die alten Dateien sind weg")
	}
	if _, err := os.Stat(filepath.Join(server.dir, "data", restoreTemp)); err == nil {
		t.Error("der Hilfsordner ist liegen geblieben")
	}
}
