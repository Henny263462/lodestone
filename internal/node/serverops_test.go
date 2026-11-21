package node

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// serverWithFiles legt einen Server mit Welt, Nether, Plugin und Config an.
func serverWithFiles(t *testing.T, manager *Manager, name string) *Server {
	t.Helper()
	server := createPaper(t, manager, name)
	put(t, server, "world/level.dat", []byte("welt"))
	put(t, server, "world/region/r.0.0.mca", bytes.Repeat([]byte("r"), 4096))
	put(t, server, "world_nether/level.dat", []byte("nether"))
	put(t, server, "plugins/LuckPerms.jar", []byte("plugin"))
	put(t, server, "server.properties", []byte("motd=Hallo\n"))
	put(t, server, "logs/latest.log", []byte("log"))
	return server
}

func readFile(t *testing.T, server *Server, name string) string {
	t.Helper()
	data, err := server.FS().Read(name, 1<<20)
	if err != nil {
		t.Fatalf("%s lesen: %v", name, err)
	}
	return string(data)
}

func TestCloneCopiesFilesWithNewPorts(t *testing.T) {
	manager := newTestManager(t, testConfig(t), newFakeRuntime())
	source := serverWithFiles(t, manager, "Survival")
	_ = source.SetDiskLimit(2048)

	clone, err := manager.Clone(context.Background(), source, CloneRequest{ID: "kopie1", Name: "Survival-Kopie", WithWorlds: false})
	if err != nil {
		t.Fatal(err)
	}
	view := clone.View()
	if view.ID != "kopie1" || view.Name != "Survival-Kopie" || !view.Installed || view.DiskLimitMB != 2048 || view.State != StateOffline {
		t.Fatalf("Kopie: %+v", view)
	}
	if len(view.Ports) != 1 || view.Ports[0].Host == 0 || view.Ports[0].Host == source.View().Ports[0].Host {
		t.Fatalf("Die Kopie braucht einen eigenen Port: %+v / %+v", view.Ports, source.View().Ports)
	}
	if readFile(t, clone, "server.properties") != "motd=Hallo\n" || readFile(t, clone, "plugins/LuckPerms.jar") != "plugin" || readFile(t, clone, "server.jar") != "jar:fake-1.21.8.jar" {
		t.Fatal("Dateien fehlen in der Kopie")
	}
	if clone.FS().Exists("world") || clone.FS().Exists("world_nether") {
		t.Fatal("Ohne Welten dürfen world und world_nether nicht mitkommen")
	}
	if !source.FS().Exists("world/region/r.0.0.mca") {
		t.Fatal("Das Original muss unverändert bleiben")
	}
	// Die Kopie überlebt einen Neustart des Daemons
	again := newTestManager(t, manager.cfg, newFakeRuntime())
	if _, ok := again.Get("kopie1"); !ok {
		t.Fatal("Die Kopie wurde nach dem Neustart nicht geladen")
	}
	entries, _ := os.ReadDir(manager.serversDir())
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".") {
			t.Fatalf("Hilfsordner übrig: %s", entry.Name())
		}
	}

	withWorlds, err := manager.Clone(context.Background(), source, CloneRequest{ID: "kopie2", Name: "Mit Welt", WithWorlds: true})
	if err != nil {
		t.Fatal(err)
	}
	if readFile(t, withWorlds, "world/region/r.0.0.mca") != strings.Repeat("r", 4096) || !withWorlds.FS().Exists("world_nether/level.dat") {
		t.Fatal("Mit Welten fehlt die Welt")
	}
	if _, err := manager.Clone(context.Background(), source, CloneRequest{ID: "kopie2", Name: "Doppelt"}); !errors.Is(err, ErrConflict) {
		t.Fatalf("doppelte ID: %v", err)
	}
	if _, err := manager.Clone(context.Background(), source, CloneRequest{ID: "../böse", Name: "x"}); !errors.Is(err, errBadRequest) {
		t.Fatalf("ungültige ID: %v", err)
	}
}

func TestCloneCanceledLeavesNothing(t *testing.T) {
	manager := newTestManager(t, testConfig(t), newFakeRuntime())
	source := serverWithFiles(t, manager, "Abbruch")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := manager.Clone(ctx, source, CloneRequest{ID: "weg", Name: "Weg", WithWorlds: true}); !errors.Is(err, context.Canceled) {
		t.Fatalf("abgebrochen: %v", err)
	}
	if _, ok := manager.Get("weg"); ok {
		t.Fatal("Nach dem Abbruch darf es keine Kopie geben")
	}
	if _, err := os.Stat(filepath.Join(manager.serversDir(), ".clone-weg")); !os.IsNotExist(err) {
		t.Fatalf("Hilfsordner übrig: %v", err)
	}
	// Die Ports sind wieder frei und das Original ist nicht gesperrt
	if _, err := manager.Clone(context.Background(), source, CloneRequest{ID: "weg", Name: "Weg"}); err != nil {
		t.Fatal(err)
	}
}

func TestExportImportRoundTripOverHTTP(t *testing.T) {
	from, to := newTestAPI(t), newTestAPI(t)
	id := from.create("Umzug")
	source, _ := from.manager.Get(id)
	put(t, source, "world/level.dat", []byte("welt"))
	put(t, source, "plugins/a.jar", bytes.Repeat([]byte("a"), 100_000))
	_ = source.SetDiskLimit(1024)

	request, _ := http.NewRequest("GET", from.url+"/api/servers/"+id+"/export", nil)
	request.Header.Set("Authorization", "Bearer "+from.token)
	response, err := http.DefaultClient.Do(request)
	if err != nil || response.StatusCode != 200 {
		t.Fatalf("Export: %v %v", err, response)
	}
	// Der Strom geht direkt weiter, ohne ihn zwischenzuspeichern
	upload, _ := http.NewRequest("POST", to.url+"/api/servers/import", response.Body)
	upload.Header.Set("Authorization", "Bearer "+to.token)
	upload.Header.Set(SizeHeader, "100000")
	result, err := http.DefaultClient.Do(upload)
	response.Body.Close()
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(result.Body)
	result.Body.Close()
	if result.StatusCode != http.StatusCreated {
		t.Fatalf("Import: %d %s", result.StatusCode, body)
	}
	var view View
	_ = json.Unmarshal(body, &view)
	moved, ok := to.manager.Get(id)
	if !ok || view.ID != id || view.Name != "Umzug" || view.DiskLimitMB != 1024 || !view.Installed || len(view.Ports) != 1 || view.Ports[0].Host == 0 {
		t.Fatalf("Ziel: %+v", view)
	}
	if readFile(t, moved, "world/level.dat") != "welt" || len(readFile(t, moved, "plugins/a.jar")) != 100_000 || readFile(t, moved, "server.jar") != "jar:fake-1.21.8.jar" {
		t.Fatal("Dateien kamen nicht vollständig an")
	}
	if _, ok := from.manager.Get(id); !ok {
		t.Fatal("Der Quellserver bleibt, bis das Panel ihn löscht")
	}
	// Ein zweiter Import derselben ID wird abgelehnt
	var again bytes.Buffer
	if err := source.writeExport(context.Background(), &again); err != nil {
		t.Fatal(err)
	}
	if status, _ := to.do("POST", "/api/servers/import", again.Bytes()); status != http.StatusConflict {
		t.Fatalf("doppelter Import: %d", status)
	}

	// Ein laufender Server lässt sich nicht exportieren
	if status, _ := from.do("POST", "/api/servers/"+id+"/power", map[string]string{"action": "start"}); status != http.StatusAccepted {
		t.Fatal("Start fehlgeschlagen")
	}
	waitState(t, source, StateRunning)
	if status, _ := from.do("GET", "/api/servers/"+id+"/export", nil); status != http.StatusConflict {
		t.Fatalf("Export eines laufenden Servers: %d", status)
	}
}

// stream baut einen Umzugs-Strom von Hand (für kaputte Fälle).
func stream(t *testing.T, spec Spec, entries map[string]string, marker bool) []byte {
	t.Helper()
	var buffer bytes.Buffer
	gz := gzip.NewWriter(&buffer)
	archive := tar.NewWriter(gz)
	data, _ := json.Marshal(spec)
	_ = archive.WriteHeader(&tar.Header{Name: exportSpec, Mode: 0o600, Size: int64(len(data)), Typeflag: tar.TypeReg})
	_, _ = archive.Write(data)
	for name, content := range entries {
		_ = archive.WriteHeader(&tar.Header{Name: name, Mode: 0o644, Size: int64(len(content)), Typeflag: tar.TypeReg})
		_, _ = archive.Write([]byte(content))
	}
	if marker {
		_ = archive.WriteHeader(&tar.Header{Name: exportMarker, Mode: 0o600, Typeflag: tar.TypeReg})
	}
	_ = archive.Close()
	_ = gz.Close()
	return buffer.Bytes()
}

func TestImportRejectsBrokenStreamsAndCleansUp(t *testing.T) {
	manager := newTestManager(t, testConfig(t), newFakeRuntime())
	spec := Spec{ID: "neu", Name: "Neu", Software: "paper", Version: "1.21.8", Installed: true}
	good := stream(t, spec, map[string]string{"data/server.properties": "motd=x\n"}, true)

	cases := map[string][]byte{
		"ohne Vermerk":   stream(t, spec, map[string]string{"data/a.txt": "a"}, false),
		"Pfad nach oben": stream(t, spec, map[string]string{"data/../../evil.txt": "x"}, true),
		"fremder Ordner": stream(t, spec, map[string]string{"etc/passwd": "x"}, true),
		"abgeschnitten":  good[:len(good)-30],
		"kein gzip":      []byte("hallo"),
	}
	for name, body := range cases {
		if _, err := manager.Import(context.Background(), bytes.NewReader(body), 0); err == nil {
			t.Errorf("%s: wurde angenommen", name)
		}
		if _, ok := manager.Get("neu"); ok {
			t.Fatalf("%s: Server trotz Fehler angelegt", name)
		}
		if _, err := os.Stat(filepath.Join(manager.serversDir(), ".import-neu")); !os.IsNotExist(err) {
			t.Fatalf("%s: Hilfsordner übrig", name)
		}
	}
	if _, err := os.Stat(filepath.Join(manager.cfg.DataDir, "evil.txt")); !os.IsNotExist(err) {
		t.Fatal("Eine Datei ist außerhalb gelandet")
	}

	// Abbruch mitten im Strom (die Verbindung des Panels reißt ab)
	reader, writer := io.Pipe()
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		_, _ = writer.Write(good[:len(good)/2])
		cancel()
		_ = writer.CloseWithError(context.Canceled)
	}()
	if _, err := manager.Import(ctx, reader, 0); err == nil {
		t.Fatal("Ein abgebrochener Import wurde angenommen")
	}
	if _, ok := manager.Get("neu"); ok {
		t.Fatal("Nach dem Abbruch darf es keinen Server geben")
	}

	server, err := manager.Import(context.Background(), bytes.NewReader(good), 0)
	if err != nil {
		t.Fatal(err)
	}
	if readFile(t, server, "server.properties") != "motd=x\n" {
		t.Fatal("Inhalt falsch")
	}
}

func TestExportLocksTheServer(t *testing.T) {
	manager := newTestManager(t, testConfig(t), newFakeRuntime())
	server := createPaper(t, manager, "Gesperrt")
	release, err := manager.beginExport(server)
	if err != nil {
		t.Fatal(err)
	}
	if err := server.Start(); !errors.Is(err, ErrBadState) {
		t.Fatalf("Start während des Umzugs: %v", err)
	}
	if _, err := server.CreateBackup(""); err == nil {
		t.Fatal("Backup während des Umzugs")
	}
	release()
	if err := server.Start(); err != nil {
		t.Fatal(err)
	}
	waitState(t, server, StateRunning)
}

func TestDiskUsageIsMeasuredAndLimitBlocksStartAndUploads(t *testing.T) {
	a := newTestAPI(t)
	a.manager.ops.remeasureDelay = 10 * time.Millisecond
	id := a.create("Voll")
	server, _ := a.manager.Get(id)
	put(t, server, "world/region/r.0.0.mca", bytes.Repeat([]byte("x"), 300_000))

	a.manager.MeasureAll(context.Background())
	var info StorageInfo
	status, body := a.do("GET", "/api/servers/"+id+"/storage", nil)
	_ = json.Unmarshal(body, &info)
	if status != 200 || info.Bytes < 300_000 || info.MeasuredAt == 0 || info.LimitMB != 0 || info.Blocked {
		t.Fatalf("Messung: %d %s", status, body)
	}
	if status, _ := a.do("PUT", "/api/servers/"+id+"/storage", map[string]int{"limitMB": 5}); status != http.StatusBadRequest {
		t.Fatalf("zu kleines Limit: %d", status)
	}
	if status, body := a.do("PUT", "/api/servers/"+id+"/storage", map[string]int{"limitMB": 256}); status != 200 {
		t.Fatalf("Limit setzen: %d %s", status, body)
	}
	if server.View().DiskLimitMB != 256 {
		t.Fatal("Das Limit wurde nicht gespeichert")
	}

	// Das Limit ist überschritten (der Ordner wird nicht wirklich so groß, die Messung wird gesetzt)
	a.manager.ops.mu.Lock()
	a.manager.ops.usage[id] = DiskUsage{Bytes: 300 << 20, MeasuredAt: time.Now().UnixMilli()}
	a.manager.ops.mu.Unlock()
	if err := server.Start(); !errors.Is(err, ErrConflict) || !strings.Contains(err.Error(), "Speicherlimit") {
		t.Fatalf("Start trotz vollem Limit: %v", err)
	}
	var form bytes.Buffer
	writer := multipart.NewWriter(&form)
	part, _ := writer.CreateFormFile("file", "gross.bin")
	_, _ = part.Write([]byte("daten"))
	_ = writer.Close()
	status, body = a.do("POST", "/api/servers/"+id+"/files/upload", form.Bytes(), "Content-Type", writer.FormDataContentType())
	if status != http.StatusConflict || !strings.Contains(string(body), "Speicherlimit") {
		t.Fatalf("Upload trotz vollem Limit: %d %s", status, body)
	}
	if status, _ := a.do("POST", "/api/servers/"+id+"/files/extract", map[string]string{"path": "a.zip", "dest": ""}); status != http.StatusConflict {
		t.Fatalf("Entpacken trotz vollem Limit: %d", status)
	}
	// Auch die anderen Wege, auf denen ein Server wächst, sind gesperrt
	for _, request := range []struct{ method, path string }{
		{"POST", "/files/archive"}, {"PUT", "/files/content?path=gross.txt"},
		{"POST", "/worlds/neu/upload"}, {"POST", "/worlds/world/datapacks"},
	} {
		if status, body := a.do(request.method, "/api/servers/"+id+request.path, "daten"); status != http.StatusConflict || !strings.Contains(string(body), "Speicherlimit") {
			t.Errorf("%s %s trotz vollem Limit: %d %s", request.method, request.path, status, body)
		}
	}
	var storage NodeStorage
	_, body = a.do("GET", "/api/storage", nil)
	_ = json.Unmarshal(body, &storage)
	if len(storage.Servers) != 1 || storage.Servers[0].LimitMB != 256 || storage.Servers[0].Bytes != 300<<20 {
		t.Fatalf("Übersicht: %s", body)
	}

	// Ohne Limit geht wieder alles; nach dem Upload wird nachgemessen
	if status, _ := a.do("PUT", "/api/servers/"+id+"/storage", map[string]int{"limitMB": 0}); status != 200 {
		t.Fatal("Limit entfernen")
	}
	status, body = a.do("POST", "/api/servers/"+id+"/files/upload", form.Bytes(), "Content-Type", writer.FormDataContentType())
	if status != http.StatusCreated {
		t.Fatalf("Upload ohne Limit: %d %s", status, body)
	}
	waitFor(t, "Nachmessung", func() bool {
		usage, _ := a.manager.usageOf(id)
		return usage.Bytes < 300<<20
	})
	if err := server.Start(); err != nil {
		t.Fatal(err)
	}
	waitState(t, server, StateRunning)
}

func TestFormatBytes(t *testing.T) {
	for value, want := range map[int64]string{512 << 10: "512 KB", 300 << 20: "300 MB", 10 << 30: "10,0 GB", 1610612736: "1,5 GB"} {
		if got := formatBytes(value); got != want {
			t.Errorf("%d: %q, erwartet %q", value, got, want)
		}
	}
	if free, total := diskSpace(t.TempDir()); total > 0 && (free <= 0 || free > total) {
		t.Fatalf("freier Platz: %d von %d", free, total)
	}
}
