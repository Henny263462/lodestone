package node_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/lodestone-mc/lodestone/internal/node"
	"github.com/lodestone-mc/lodestone/internal/panel"
	"github.com/pkg/sftp"
	"golang.org/x/crypto/ssh"
)

// Ende zu Ende: Das Panel rechnet die Zugänge aus und schiebt sie zum echten Node; danach klappt die Anmeldung per SFTP,
// und nach dem Widerruf im Panel nicht mehr.
func TestSFTPEndToEndWithThePanel(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	cfg := node.ConfigForTest(t)
	cfg.SFTPListen = "127.0.0.1:0"
	manager, rt := node.NewManagerForTest(t, cfg)
	service, err := node.NewSFTPService(cfg, manager, logger)
	if err != nil {
		t.Fatal(err)
	}
	api := node.NewAPI(cfg, manager, rt, node.SoftwareForTest, logger, "test")
	api.SetSFTP(service)
	nodeWeb := httptest.NewServer(api.Handler())
	t.Cleanup(nodeWeb.Close)
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	go func() { _ = service.Serve(ctx, listener) }()
	for deadline := time.Now().Add(5 * time.Second); !service.Status(0).Enabled; time.Sleep(10 * time.Millisecond) {
		if time.Now().After(deadline) {
			t.Fatal("SFTP startet nicht")
		}
	}

	store, err := panel.OpenStore(filepath.Join(t.TempDir(), "panel.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })
	p := panel.New(panel.Config{Version: "test"}, store, logger, nil)
	panelWeb := httptest.NewServer(p.Handler())
	t.Cleanup(panelWeb.Close)
	registered, err := store.CreateNode(panel.Node{Name: "local", URL: nodeWeb.URL, Token: cfg.Token, PublicHost: "127.0.0.1"})
	if err != nil {
		t.Fatal(err)
	}
	go p.Maintain(ctx)

	jar, _ := cookiejar.New(nil)
	browser := &http.Client{Jar: jar}
	call := func(want int, method, path string, body any) []byte {
		t.Helper()
		data, _ := json.Marshal(body)
		request, _ := http.NewRequest(method, panelWeb.URL+path, bytes.NewReader(data))
		request.Header.Set("X-Lodestone", "1")
		response, err := browser.Do(request)
		if err != nil {
			t.Fatal(err)
		}
		defer response.Body.Close()
		answer, _ := io.ReadAll(response.Body)
		if response.StatusCode != want {
			t.Fatalf("%s %s: %d %s", method, path, response.StatusCode, answer)
		}
		return answer
	}
	call(201, "POST", "/api/auth/setup", map[string]string{"username": "admin", "password": "ein-gutes-passwort"})
	var project struct{ ID string }
	_ = json.Unmarshal(call(201, "POST", "/api/projects", map[string]string{"name": "Survival", "kind": "server"}), &project)
	var server struct{ ID string }
	_ = json.Unmarshal(call(201, "POST", "/api/projects/"+project.ID+"/servers", map[string]any{
		"name": "Main", "nodeId": registered.ID, "software": "paper", "version": "1.21.8", "eula": true,
	}), &server)
	call(200, "PUT", "/api/me/sftp/password", map[string]string{"password": "sftp-passwort-123"})
	var card panel.SFTPServerDTO
	_ = json.Unmarshal(call(200, "GET", "/api/servers/"+server.ID+"/sftp", nil), &card)
	if !card.Enabled || card.Username != "admin."+server.ID[:8] || card.Fingerprint != service.Fingerprint() {
		t.Fatalf("Karte: %+v", card)
	}

	hostKey, err := ssh.ParsePublicKey(service.HostKeyForTest())
	if err != nil {
		t.Fatal(err)
	}
	dial := func(password string) (*sftp.Client, *ssh.Client, error) {
		conn, err := ssh.Dial("tcp", listener.Addr().String(), &ssh.ClientConfig{
			User: card.Username, Auth: []ssh.AuthMethod{ssh.Password(password)}, HostKeyCallback: ssh.FixedHostKey(hostKey), Timeout: 5 * time.Second,
		})
		if err != nil {
			return nil, nil, err
		}
		client, err := sftp.NewClient(conn)
		if err != nil {
			conn.Close()
			return nil, nil, err
		}
		return client, conn, nil
	}
	waitUntil := func(what string, condition func() bool) {
		t.Helper()
		deadline := time.Now().Add(15 * time.Second)
		for !condition() {
			if time.Now().After(deadline) {
				t.Fatalf("Zeitüberschreitung: %s", what)
			}
			time.Sleep(100 * time.Millisecond)
		}
	}

	var client *sftp.Client
	var conn *ssh.Client
	waitUntil("Anmeldung nach dem Abgleich", func() bool {
		client, conn, err = dial("sftp-passwort-123")
		return err == nil
	})
	file, err := client.Create("/hallo.txt")
	if err != nil {
		t.Fatal(err)
	}
	_, _ = file.Write([]byte("über SFTP"))
	file.Close()
	client.Close()
	conn.Close()
	// Dieselbe Datei ist über die Dateien-Schnittstelle des Panels sichtbar
	request, _ := http.NewRequest("GET", panelWeb.URL+"/api/servers/"+server.ID+"/files/content?path=hallo.txt", nil)
	response, err := browser.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	content, _ := io.ReadAll(response.Body)
	response.Body.Close()
	if string(content) != "über SFTP" {
		t.Fatalf("Inhalt über das Panel: %d %q", response.StatusCode, content)
	}

	// Passwort im Panel löschen: der Node lässt niemanden mehr herein
	call(200, "DELETE", "/api/me/sftp/password", nil)
	waitUntil("Widerruf auf dem Node", func() bool {
		client, conn, err = dial("sftp-passwort-123")
		if err == nil {
			client.Close()
			conn.Close()
		}
		return err != nil
	})
}
