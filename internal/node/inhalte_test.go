package node

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/lodestone-mc/lodestone/internal/software"
)

func TestEditHOCONTopLevelOnly(t *testing.T) {
	input := "# BlueMap\r\naccept-download: false\r\nrender-thread-count: 1\r\nwebserver {\r\n  accept-download: false\r\n}\r\n"
	edit := Edit{File: "core.conf", Format: "hocon", Set: []SetOp{{Path: []string{"accept-download"}, Value: true}, {Path: []string{"data"}, Value: `bluemap "x"`}}}
	out, changed := editHOCON([]byte(input), edit)
	want := "# BlueMap\r\naccept-download: true\r\nrender-thread-count: 1\r\nwebserver {\r\n  accept-download: false\r\n}\r\ndata: \"bluemap \\\"x\\\"\"\r\n"
	if !changed || string(out) != want {
		t.Fatalf("HOCON falsch bearbeitet:\n%q\nerwartet\n%q", out, want)
	}
	if _, again := editHOCON(out, edit); again {
		t.Fatal("ein zweiter Durchlauf darf nichts mehr ändern")
	}
	bad := Autoconfig{Edits: []Edit{{File: "core.conf", Format: "hocon", Set: []SetOp{{Path: []string{"a", "b"}, Value: 1}}}}}
	if err := bad.Validate(); err == nil {
		t.Fatal("verschachtelte Pfade in HOCON müssen abgelehnt werden")
	}
}

func TestAutoconfigHOCONWaitsForFile(t *testing.T) {
	manager := newTestManager(t, testConfig(t), newFakeRuntime())
	server := createPaper(t, manager, "Karte")
	config := Autoconfig{Edits: []Edit{{File: "plugins/BlueMap/core.conf", Format: "hocon", Set: []SetOp{{Path: []string{"accept-download"}, Value: true}}}}}
	_, missing, err := ApplyAutoconfig(server.FS(), config)
	if err != nil || len(missing) != 1 {
		t.Fatalf("ohne core.conf muss der Satz warten: %v %v", missing, err)
	}
	put(t, server, "plugins/BlueMap/core.conf", []byte("accept-download: false\n"))
	if changed, missing, err := ApplyAutoconfig(server.FS(), config); err != nil || !changed || len(missing) != 0 {
		t.Fatalf("Anwenden: %v %v %v", changed, missing, err)
	}
	data, _ := server.FS().Read("plugins/BlueMap/core.conf", 1<<20)
	if string(data) != "accept-download: true\n" {
		t.Fatalf("core.conf = %q", data)
	}
}

func TestPullChecksSHA256(t *testing.T) {
	file := []byte("ein plugin")
	sum := sha256.Sum256(file)
	tls := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write(file) }))
	t.Cleanup(tls.Close)
	cfg := testConfig(t)
	cfg.PullHosts = []string{"127.0.0.1"}
	manager := newTestManager(t, cfg, newFakeRuntime())
	manager.pullClient = tls.Client()
	server := createPaper(t, manager, "Pull")
	ctx := context.Background()
	if err := manager.Pull(ctx, server, PullRequest{URL: tls.URL + "/a.jar", Path: "plugins/a.jar", SHA256: strings.Repeat("0", 64)}); err == nil || !strings.Contains(err.Error(), "Prüfsumme") {
		t.Fatalf("falsche SHA-256 muss scheitern: %v", err)
	}
	if server.FS().Exists("plugins/a.jar") {
		t.Fatal("bei falscher Prüfsumme darf keine Datei liegen bleiben")
	}
	if err := manager.Pull(ctx, server, PullRequest{URL: tls.URL + "/a.jar", Path: "plugins/a.jar", SHA256: hex.EncodeToString(sum[:])}); err != nil {
		t.Fatal(err)
	}
}

// loaderPack liefert ein Modpack ohne Dateien, das die angegebenen Abhängigkeiten nennt.
func loaderPack(t *testing.T, dependencies map[string]string) (*Manager, string) {
	t.Helper()
	index, _ := json.Marshal(map[string]any{"formatVersion": 1, "game": "minecraft", "versionId": "1.0", "name": "Loaderpack", "files": []any{}, "dependencies": dependencies})
	pack := jar(t, map[string]string{"modrinth.index.json": string(index), "overrides/config/a.toml": "a = 1\n"})
	tls := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write(pack) }))
	t.Cleanup(tls.Close)
	cfg := testConfig(t)
	cfg.PullHosts = []string{"127.0.0.1"}
	manager := newTestManager(t, cfg, newFakeRuntime())
	manager.pullClient = tls.Client()
	return manager, tls.URL + "/pack.mrpack"
}

func createModded(t *testing.T, manager *Manager, id software.ID, version string) *Server {
	t.Helper()
	server, err := manager.Create(Spec{Name: string(id), Software: id, Version: version, EULA: true})
	if err != nil {
		t.Fatal(err)
	}
	waitFor(t, "Installation", func() bool { return server.View().Installed })
	waitState(t, server, StateOffline)
	return server
}

func TestModpackPinsLoaderVersion(t *testing.T) {
	cases := []struct {
		name     string
		software software.ID
		base     string
		deps     map[string]string
		want     string
	}{
		{"NeoForge", software.NeoForge, "1.21.1", map[string]string{"minecraft": "1.21.1", "neoforge": "21.1.77"}, "21.1.77"},
		{"Forge", software.Forge, "1.20.1", map[string]string{"minecraft": "1.20.1", "forge": "47.2.0"}, "1.20.1-47.2.0"},
		{"Forge mit voller Version", software.Forge, "1.20.1", map[string]string{"minecraft": "1.20.1", "forge": "1.20.1-47.3.0"}, "1.20.1-47.3.0"},
		{"Fabric bleibt", software.Fabric, "1.21.1", map[string]string{"minecraft": "1.21.1", "fabric-loader": "0.16.0"}, "1.21.1"},
		{"unbekannte Version: alte bleibt", software.NeoForge, "1.21.1", map[string]string{"minecraft": "1.21.1", "neoforge": "0.0"}, "1.21.1"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			manager, packURL := loaderPack(t, tc.deps)
			server := createModded(t, manager, tc.software, tc.base)
			if err := manager.InstallModpack(server, ModpackRequest{URL: packURL}); err != nil {
				t.Fatal(err)
			}
			status := waitModpack(t, server)
			if status.State != "done" {
				t.Fatalf("Modpack: %+v", status)
			}
			view := server.View()
			if view.Version != tc.want || !view.Installed {
				t.Fatalf("Version %q (installiert %v), erwartet %q", view.Version, view.Installed, tc.want)
			}
			if !server.FS().Exists("config/a.toml") {
				t.Fatal("die Dateien des Modpacks fehlen")
			}
		})
	}
}
