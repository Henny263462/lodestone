package node

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/hashicorp/yamux"
	"github.com/lodestone-mc/lodestone/internal/nodelink"
)

// fakePanel spielt das Panel: Koppeln mit einem festen Join-Token, Verbinden mit dem ausgegebenen Geheimnis.
type fakePanel struct {
	mu       sync.Mutex
	joins    int
	secret   string
	sessions chan *yamux.Session
}

func newFakePanel(t *testing.T) (*fakePanel, *httptest.Server) {
	panel := &fakePanel{secret: "ldn_erstes", sessions: make(chan *yamux.Session, 4)}
	mux := http.NewServeMux()
	mux.HandleFunc("POST "+nodelink.PathJoin, func(w http.ResponseWriter, r *http.Request) {
		var body nodelink.JoinRequest
		_ = json.NewDecoder(r.Body).Decode(&body)
		panel.mu.Lock()
		defer panel.mu.Unlock()
		panel.joins++
		if body.Token != "ldj_gut" {
			writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "Join-Token ungültig"})
			return
		}
		writeJSON(w, http.StatusOK, nodelink.JoinResponse{NodeID: "0123456789ab", Secret: panel.secret})
	})
	mux.HandleFunc("GET "+nodelink.PathConnect, func(w http.ResponseWriter, r *http.Request) {
		panel.mu.Lock()
		ok := r.Header.Get("Authorization") == "Bearer "+panel.secret && r.Header.Get(nodelink.HeaderNode) == "0123456789ab"
		panel.mu.Unlock()
		if !ok {
			writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "falsches Geheimnis"})
			return
		}
		session, err := nodelink.Accept(w, r)
		if err != nil {
			return
		}
		panel.sessions <- session
		<-session.CloseChan()
	})
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)
	return panel, server
}

func (f *fakePanel) joinCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.joins
}

func (f *fakePanel) next(t *testing.T) *yamux.Session {
	t.Helper()
	select {
	case session := <-f.sessions:
		t.Cleanup(func() { session.Close() })
		return session
	case <-time.After(5 * time.Second):
		t.Fatal("der Node hat sich nicht verbunden")
		return nil
	}
}

// ask schickt eine Anfrage über die Verbindung an den Node, wie es das Panel tut.
func ask(t *testing.T, session *yamux.Session, path string) string {
	t.Helper()
	client := &http.Client{Timeout: 5 * time.Second, Transport: &http.Transport{
		DialContext: func(context.Context, string, string) (net.Conn, error) { return session.OpenStream() },
	}}
	response, err := client.Get("http://node.link" + path)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	data, _ := io.ReadAll(response.Body)
	return string(data)
}

func startLink(t *testing.T, cfg LinkConfig) (*Link, context.CancelFunc, chan struct{}) {
	t.Helper()
	api := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, r.URL.Path+" "+r.Header.Get("Authorization"))
	})
	link, err := NewLink(cfg, "api-token-des-daemons-1234567890", api, slog.New(slog.NewTextHandler(io.Discard, nil)), "test")
	if err != nil {
		t.Fatal(err)
	}
	link.minBackoff, link.maxBackoff, link.rejectedWait = 5*time.Millisecond, 20*time.Millisecond, 20*time.Millisecond
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		link.Run(ctx)
		close(done)
	}()
	t.Cleanup(func() {
		cancel()
		<-done
	})
	return link, cancel, done
}

func TestLinkJoinServeReconnect(t *testing.T) {
	panel, server := newFakePanel(t)
	state := filepath.Join(t.TempDir(), "data", "panel-link.json")
	startLink(t, LinkConfig{PanelURL: server.URL, JoinToken: "ldj_gut", StatePath: state})

	session := panel.next(t)
	// Anfragen über die Verbindung gelten als angemeldet: Der Node setzt sein eigenes Token ein
	if got := ask(t, session, "/api/system"); got != "/api/system Bearer api-token-des-daemons-1234567890" {
		t.Fatalf("Antwort %q", got)
	}
	data, err := os.ReadFile(state)
	if err != nil {
		t.Fatal(err)
	}
	var saved linkState
	_ = json.Unmarshal(data, &saved)
	if saved.NodeID != "0123456789ab" || saved.Secret != "ldn_erstes" || saved.PanelURL != server.URL {
		t.Fatalf("gespeichert: %+v", saved)
	}

	// Verbindung bricht ab: Der Node verbindet sich mit dem gespeicherten Geheimnis neu, ohne neu zu koppeln
	session.Close()
	session = panel.next(t)
	if got := ask(t, session, "/api/servers"); got != "/api/servers Bearer api-token-des-daemons-1234567890" {
		t.Fatalf("nach dem Wiederverbinden: %q", got)
	}
	if panel.joinCount() != 1 {
		t.Fatalf("%d-mal gekoppelt, erwartet einmal", panel.joinCount())
	}
}

func TestLinkWrongJoinToken(t *testing.T) {
	panel, server := newFakePanel(t)
	state := filepath.Join(t.TempDir(), "panel-link.json")
	_, cancel, done := startLink(t, LinkConfig{PanelURL: server.URL, JoinToken: "ldj_falsch", StatePath: state})
	deadline := time.Now().Add(5 * time.Second)
	for panel.joinCount() < 2 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if panel.joinCount() < 2 {
		t.Fatal("der Node versucht es nach einer Ablehnung nicht erneut")
	}
	if _, err := os.Stat(state); !os.IsNotExist(err) {
		t.Fatal("ohne gültiges Token darf nichts gespeichert werden")
	}
	select {
	case session := <-panel.sessions:
		session.Close()
		t.Fatal("ohne Kopplung verbunden")
	default:
	}
	// Abbruch beendet die Schleife
	cancel()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Run endet nicht mit dem Kontext")
	}
}

func TestLinkStaleSecretRejoins(t *testing.T) {
	panel, server := newFakePanel(t)
	state := filepath.Join(t.TempDir(), "panel-link.json")
	stale, _ := json.Marshal(linkState{PanelURL: server.URL, NodeID: "0123456789ab", Secret: "ldn_veraltet"})
	if err := os.WriteFile(state, stale, 0o600); err != nil {
		t.Fatal(err)
	}
	startLink(t, LinkConfig{PanelURL: server.URL, JoinToken: "ldj_gut", StatePath: state})
	session := panel.next(t)
	if got := ask(t, session, "/healthz"); got == "" {
		t.Fatal("keine Antwort")
	}
	if panel.joinCount() != 1 {
		t.Fatalf("nach abgelehntem Geheimnis %d-mal gekoppelt", panel.joinCount())
	}
}

func TestLinkConfigFromEnv(t *testing.T) {
	t.Setenv("LODESTONE_NODE_PANEL_URL", "")
	if _, ok := LinkConfigFromEnv(Config{DataDir: "/srv/lodestone"}); ok {
		t.Fatal("ohne Adresse keine Verbindung")
	}
	t.Setenv("LODESTONE_NODE_PANEL_URL", " https://panel.example.org/ ")
	t.Setenv("LODESTONE_NODE_JOIN_TOKEN", "ldj_abc")
	cfg, ok := LinkConfigFromEnv(Config{DataDir: "/srv/lodestone"})
	if !ok || cfg.PanelURL != "https://panel.example.org" || cfg.JoinToken != "ldj_abc" || cfg.StatePath != filepath.Join("/srv/lodestone", "panel-link.json") {
		t.Fatalf("%+v", cfg)
	}
	if _, err := NewLink(LinkConfig{PanelURL: "ftp://x"}, "t", http.NotFoundHandler(), slog.Default(), ""); err == nil {
		t.Fatal("ftp:// angenommen")
	}
}

func TestLoadConfigLinkedWithoutToken(t *testing.T) {
	t.Setenv("LODESTONE_NODE_TOKEN", "")
	t.Setenv("LODESTONE_NODE_PANEL_URL", "https://panel.example.org")
	path := filepath.Join(t.TempDir(), "config.json")
	config, created, err := LoadConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	if created || len(config.Token) < 24 {
		t.Fatalf("created=%v token=%d Zeichen", created, len(config.Token))
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("im verbundenen Betrieb wird keine Config-Datei angelegt")
	}
}
