package panel

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/hashicorp/yamux"
	"github.com/lodestone-mc/lodestone/internal/nodelink"
)

// tunnelNode ist ein Node am Tunnel: Er bedient die Attrappe fakeNode und dazu Routen für Ströme und die Konsole.
type tunnelNode struct {
	handler http.Handler
	// gate hält den großen Download nach dem ersten Stück an, bis der Test es gelesen hat (zeigt, dass nichts puffert).
	gate chan struct{}
	// aborted schließt sich, wenn der endlose Download auf dem Node abgebrochen wird.
	aborted chan struct{}
	// uploaded meldet die Größe des letzten Uploads.
	uploaded chan int64
}

const bigDownload = 8 << 20

func newTunnelNode(t *testing.T) (*tunnelNode, *fakeNode) {
	fake, fakeServer := newFakeNode(t)
	node := &tunnelNode{gate: make(chan struct{}), aborted: make(chan struct{}), uploaded: make(chan int64, 1)}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/servers/{id}/files/download", func(w http.ResponseWriter, r *http.Request) {
		chunk := bytes.Repeat([]byte("x"), 1<<20)
		flusher := w.(http.Flusher)
		switch r.URL.Query().Get("path") {
		case "big":
			_, _ = w.Write(chunk)
			flusher.Flush()
			select {
			case <-node.gate:
			case <-r.Context().Done():
				return
			}
			for written := len(chunk); written < bigDownload; written += len(chunk) {
				_, _ = w.Write(chunk)
			}
		case "endless":
			for {
				if _, err := w.Write(chunk[:32<<10]); err != nil {
					close(node.aborted)
					return
				}
				flusher.Flush()
				select {
				case <-r.Context().Done():
					close(node.aborted)
					return
				case <-time.After(10 * time.Millisecond):
				}
			}
		}
	})
	mux.HandleFunc("POST /api/servers/{id}/files/upload", func(w http.ResponseWriter, r *http.Request) {
		size, _ := io.Copy(io.Discard, r.Body)
		node.uploaded <- size
		writeJSON(w, http.StatusOK, map[string]int64{"size": size})
	})
	mux.HandleFunc("GET /api/servers/{id}/ws", func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer conn.CloseNow()
		_ = conn.Write(r.Context(), websocket.MessageText, []byte(`{"type":"hello"}`))
		for {
			kind, data, err := conn.Read(r.Context())
			if err != nil {
				return
			}
			_ = conn.Write(r.Context(), kind, append([]byte("echo:"), data...))
		}
	})
	mux.Handle("/", fakeServer.Config.Handler)
	node.handler = mux
	return node, fake
}

// connect verbindet den Node über den Tunnel mit dem Panel und bedient ihn, bis die Sitzung endet.
func (n *tunnelNode) connect(t *testing.T, panelURL, nodeID, secret string) (*yamux.Session, error) {
	t.Helper()
	_, connect, _, _ := nodelink.Endpoints(panelURL)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	session, err := nodelink.Dial(ctx, connect, nodeID, secret, "0.3.0-test", &http.Client{})
	if err != nil {
		return nil, err
	}
	server := &http.Server{Handler: n.handler}
	go server.Serve(session)
	t.Cleanup(func() {
		session.Close()
		server.Close()
	})
	return session, nil
}

func joinNode(t *testing.T, panelURL, token string) (int, nodelink.JoinResponse) {
	t.Helper()
	data, _ := json.Marshal(nodelink.JoinRequest{Token: token, Version: "test"})
	response, err := http.Post(panelURL+nodelink.PathJoin, "application/json", bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	var answer nodelink.JoinResponse
	_ = json.NewDecoder(response.Body).Decode(&answer)
	return response.StatusCode, answer
}

// waitLink wartet, bis der Node verbunden bzw. getrennt ist (die Annahme läuft im Handler des Panels nebenher).
func waitLink(t *testing.T, p *Panel, nodeID string, want bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if _, ok := p.link.status(nodeID); ok == want {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("Node %s: verbunden sollte %v sein", nodeID, want)
}

func nodeOnline(t *testing.T, admin *client, nodeID string) bool {
	t.Helper()
	var nodes []nodeDTO
	_ = json.Unmarshal(admin.expect(200, "GET", "/api/nodes", nil), &nodes)
	for _, n := range nodes {
		if n.ID == nodeID {
			if !n.Linked || n.URL != "" {
				t.Fatalf("Node %s sollte als verbunden ohne Adresse erscheinen: %+v", nodeID, n)
			}
			return n.Online
		}
	}
	t.Fatalf("Node %s fehlt in der Liste", nodeID)
	return false
}

func TestNodeLinkTunnel(t *testing.T) {
	p, web, _, _ := setupPanel(t)
	admin := newClient(t, web.URL)
	admin.expect(201, "POST", "/api/auth/setup", map[string]string{"username": "admin", "password": "ein-gutes-passwort"})
	node, fake := newTunnelNode(t)

	// Node im Panel anlegen: einmaliges Join-Token, noch nicht verbunden
	var created joinTokenDTO
	_ = json.Unmarshal(admin.expect(201, "POST", "/api/nodes/link", map[string]string{"name": "remote", "publicHost": "mc2.example.org"}), &created)
	if !strings.HasPrefix(created.Token, nodelink.JoinPrefix) || created.ExpiresAt <= time.Now().UnixMilli() || !created.Node.Linked {
		t.Fatalf("Join-Token: %+v", created)
	}
	nodeID := created.Node.ID
	admin.expect(400, "POST", "/api/nodes/link", map[string]string{"name": "", "publicHost": ""})
	admin.expect(400, "POST", "/api/nodes/link", map[string]string{"name": "x", "publicHost": "mc.example.org; rm -rf"})
	admin.expect(409, "POST", "/api/nodes/link", map[string]string{"name": "remote", "publicHost": ""})
	var links []nodeLinkDTO
	_ = json.Unmarshal(admin.expect(200, "GET", "/api/nodes/link", nil), &links)
	if len(links) != 1 || links[0].NodeID != nodeID || links[0].Connected || links[0].JoinExpiresAt == 0 || links[0].JoinedAt != 0 {
		t.Fatalf("Status vor dem Koppeln: %+v", links)
	}
	if nodeOnline(t, admin, nodeID) {
		t.Fatal("ein nie verbundener Node ist online")
	}

	// Koppeln: falsches Token, richtiges Token, dasselbe Token ein zweites Mal
	if status, _ := joinNode(t, web.URL, "ldj_falsch"); status != http.StatusUnauthorized {
		t.Fatalf("falsches Join-Token: %d", status)
	}
	status, joined := joinNode(t, web.URL, created.Token)
	if status != http.StatusOK || joined.NodeID != nodeID || !strings.HasPrefix(joined.Secret, nodelink.SecretPrefix) {
		t.Fatalf("Koppeln: %d %+v", status, joined)
	}
	if status, _ := joinNode(t, web.URL, created.Token); status != http.StatusUnauthorized {
		t.Fatalf("Join-Token zweimal benutzt: %d", status)
	}
	if hash, _ := p.store.NodeLinkSecretHash(nodeID); hash != hashToken(joined.Secret) || strings.Contains(hash, joined.Secret) {
		t.Fatal("das Panel speichert nicht den Hash des Geheimnisses")
	}

	// Verbinden: falsches Geheimnis, fremde ID, dann richtig
	var rejected *nodelink.RejectedError
	if _, err := node.connect(t, web.URL, nodeID, "ldn_falsch"); !errors.As(err, &rejected) || rejected.Status != http.StatusUnauthorized {
		t.Fatalf("falsches Geheimnis: %v", err)
	}
	if _, err := node.connect(t, web.URL, "aaaaaaaaaaaa", joined.Secret); !errors.As(err, &rejected) {
		t.Fatalf("fremde ID: %v", err)
	}
	session, err := node.connect(t, web.URL, nodeID, joined.Secret)
	if err != nil {
		t.Fatal(err)
	}
	waitLink(t, p, nodeID, true)
	if !nodeOnline(t, admin, nodeID) {
		t.Fatal("der verbundene Node ist nicht online")
	}
	links = nil
	_ = json.Unmarshal(admin.expect(200, "GET", "/api/nodes/link", nil), &links)
	if !links[0].Connected || !links[0].Insecure || links[0].Version != "0.3.0-test" || links[0].JoinExpiresAt != 0 || links[0].JoinedAt == 0 {
		t.Fatalf("Status nach dem Verbinden: %+v", links[0])
	}

	// Server über den Tunnel anlegen und ansprechen
	var project ProjectDTO
	_ = json.Unmarshal(admin.expect(201, "POST", "/api/projects", map[string]string{"name": "Fern", "kind": "server"}), &project)
	var server ServerDTO
	_ = json.Unmarshal(admin.expect(201, "POST", "/api/projects/"+project.ID+"/servers",
		serverRequest{Name: "Main", NodeID: nodeID, Software: "paper", Version: "1.21.4", EULA: true}), &server)
	fake.mu.Lock()
	_, onNode := fake.servers[server.ID]
	fake.mu.Unlock()
	if !onNode {
		t.Fatal("der Server kam nicht über den Tunnel beim Node an")
	}
	admin.expect(202, "POST", "/api/servers/"+server.ID+"/power", map[string]string{"action": "start"})

	t.Run("Download als Strom", func(t *testing.T) {
		request, _ := http.NewRequest(http.MethodGet, web.URL+"/api/servers/"+server.ID+"/files/download?path=big", nil)
		response, err := admin.http.Do(request)
		if err != nil {
			t.Fatal(err)
		}
		defer response.Body.Close()
		first := make(chan error, 1)
		go func() {
			_, err := io.ReadFull(response.Body, make([]byte, 1<<20))
			first <- err
		}()
		select {
		case err := <-first:
			if err != nil {
				t.Fatal(err)
			}
		case <-time.After(10 * time.Second):
			t.Fatal("das erste Stück kam nicht an: der Download wird gepuffert")
		}
		close(node.gate)
		rest, err := io.Copy(io.Discard, response.Body)
		if err != nil || rest+(1<<20) != bigDownload {
			t.Fatalf("Download: %d Bytes, %v", rest+(1<<20), err)
		}
	})

	t.Run("Upload als Strom", func(t *testing.T) {
		request, _ := http.NewRequest(http.MethodPost, web.URL+"/api/servers/"+server.ID+"/files/upload?path=/", bytes.NewReader(make([]byte, 3<<20)))
		request.Header.Set(csrfHeader, "1")
		response, err := admin.http.Do(request)
		if err != nil {
			t.Fatal(err)
		}
		response.Body.Close()
		if response.StatusCode != http.StatusOK || <-node.uploaded != 3<<20 {
			t.Fatalf("Upload: %d", response.StatusCode)
		}
	})

	t.Run("Abbruch erreicht den Node", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		request, _ := http.NewRequestWithContext(ctx, http.MethodGet, web.URL+"/api/servers/"+server.ID+"/files/download?path=endless", nil)
		response, err := admin.http.Do(request)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := io.ReadFull(response.Body, make([]byte, 64<<10)); err != nil {
			t.Fatal(err)
		}
		cancel()
		response.Body.Close()
		select {
		case <-node.aborted:
		case <-time.After(5 * time.Second):
			t.Fatal("der Node schreibt nach dem Abbruch weiter")
		}
	})

	t.Run("Konsole über den Tunnel", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		address := "ws" + strings.TrimPrefix(web.URL, "http") + "/api/servers/" + server.ID + "/ws"
		conn, _, err := websocket.Dial(ctx, address, &websocket.DialOptions{HTTPClient: admin.http})
		if err != nil {
			t.Fatal(err)
		}
		defer conn.CloseNow()
		if _, data, err := conn.Read(ctx); err != nil || !strings.Contains(string(data), "hello") {
			t.Fatalf("Begrüßung: %q %v", data, err)
		}
		_ = conn.Write(ctx, websocket.MessageText, []byte(`{"type":"command","data":"list"}`))
		if _, data, err := conn.Read(ctx); err != nil || !strings.HasPrefix(string(data), "echo:") {
			t.Fatalf("Antwort: %q %v", data, err)
		}
	})

	// Der Wächter erkennt den Abbruch der Verbindung wie bei einem direkt angesprochenen Node
	p.watchOnce(context.Background())
	session.Close()
	waitLink(t, p, nodeID, false)
	if nodeOnline(t, admin, nodeID) {
		t.Fatal("der getrennte Node ist noch online")
	}
	admin.expect(502, "GET", "/api/servers/"+server.ID+"/logs", nil)
	p.watchOnce(context.Background())
	if events := admin.expect(200, "GET", "/api/projects/"+project.ID+"/events", nil); !strings.Contains(string(events), "node_offline") {
		t.Fatalf("kein Ereignis node_offline: %s", events)
	}

	// Wiederverbinden mit demselben Geheimnis
	if _, err := node.connect(t, web.URL, nodeID, joined.Secret); err != nil {
		t.Fatal(err)
	}
	waitLink(t, p, nodeID, true)
	if !nodeOnline(t, admin, nodeID) {
		t.Fatal("nach dem Wiederverbinden nicht online")
	}
	p.watchOnce(context.Background())
	if events := admin.expect(200, "GET", "/api/projects/"+project.ID+"/events", nil); !strings.Contains(string(events), "node_online") {
		t.Fatalf("kein Ereignis node_online: %s", events)
	}

	// Bearbeiten: Name ändern geht, Adresse bleibt leer
	admin.expect(200, "PATCH", "/api/nodes/"+nodeID, map[string]string{"name": "fern", "url": "", "token": "", "publicHost": "mc3.example.org"})
	if n, _ := p.store.NodeByID(nodeID); n.Name != "fern" || n.URL != "" || n.PublicHost != "mc3.example.org" {
		t.Fatalf("nach dem Bearbeiten: %+v", n)
	}

	// Neu koppeln: das alte Geheimnis gilt danach nicht mehr, die alte Verbindung wird getrennt
	var renewed joinTokenDTO
	_ = json.Unmarshal(admin.expect(200, "POST", "/api/nodes/"+nodeID+"/join-token", nil), &renewed)
	status, rejoined := joinNode(t, web.URL, renewed.Token)
	if status != http.StatusOK || rejoined.Secret == joined.Secret {
		t.Fatalf("neu koppeln: %d", status)
	}
	waitLink(t, p, nodeID, false)
	if _, err := node.connect(t, web.URL, nodeID, joined.Secret); !errors.As(err, &rejected) {
		t.Fatalf("altes Geheimnis nach dem Neukoppeln: %v", err)
	}
	if _, err := node.connect(t, web.URL, nodeID, rejoined.Secret); err != nil {
		t.Fatal(err)
	}
	waitLink(t, p, nodeID, true)

	// Löschen trennt die Verbindung
	admin.expect(204, "DELETE", "/api/servers/"+server.ID, nil)
	admin.expect(204, "DELETE", "/api/nodes/"+nodeID, nil)
	waitLink(t, p, nodeID, false)
	if _, err := node.connect(t, web.URL, nodeID, rejoined.Secret); !errors.As(err, &rejected) {
		t.Fatalf("gelöschter Node kann sich verbinden: %v", err)
	}
}

func TestNodeLinkPermissions(t *testing.T) {
	_, web, _, directID := setupPanel(t)
	admin := newClient(t, web.URL)
	admin.expect(201, "POST", "/api/auth/setup", map[string]string{"username": "admin", "password": "ein-gutes-passwort"})
	admin.expect(201, "POST", "/api/users", map[string]any{"username": "anna", "password": "ein-gutes-passwort"})
	anna := newClient(t, web.URL)
	anna.expect(200, "POST", "/api/auth/login", map[string]string{"username": "anna", "password": "ein-gutes-passwort"})

	anna.expect(403, "POST", "/api/nodes/link", map[string]string{"name": "x", "publicHost": ""})
	anna.expect(403, "GET", "/api/nodes/link", nil)
	anna.expect(403, "POST", "/api/nodes/"+directID+"/join-token", nil)
	admin.expect(400, "POST", "/api/nodes/"+directID+"/join-token", nil)
	admin.expect(404, "POST", "/api/nodes/ffffffffffff/join-token", nil)
	admin.expect(400, "POST", "/api/nodes/link", map[string]any{"name": "x", "publicHost": "", "url": "http://x"})

	// API-Schlüssel dürfen keine Join-Tokens erzeugen
	var key struct{ Key string }
	_ = json.Unmarshal(admin.expect(201, "POST", "/api/me/keys", map[string]any{"name": "skript"}), &key)
	keyClient := newClient(t, web.URL)
	if status, _ := keyClient.do("POST", "/api/nodes/link", map[string]string{"name": "y", "publicHost": ""}, "Authorization", "Bearer "+key.Key); status != http.StatusForbidden {
		t.Fatalf("API-Schlüssel: %d", status)
	}
}

func TestNodeLinkThrottle(t *testing.T) {
	_, web, _, _ := setupPanel(t)
	for range throttleLimit {
		if status, _ := joinNode(t, web.URL, "ldj_geraten"); status != http.StatusUnauthorized {
			t.Fatalf("Fehlversuch: %d", status)
		}
	}
	if status, _ := joinNode(t, web.URL, "ldj_geraten"); status != http.StatusTooManyRequests {
		t.Fatalf("nach %d Fehlversuchen: %d", throttleLimit, status)
	}
	// auch das Verbinden ist für diese Adresse gesperrt
	node, _ := newTunnelNode(t)
	var rejected *nodelink.RejectedError
	if _, err := node.connect(t, web.URL, "aaaaaaaaaaaa", "ldn_x"); err == nil || errors.As(err, &rejected) {
		t.Fatalf("Verbinden trotz Sperre: %v", err)
	}
}
