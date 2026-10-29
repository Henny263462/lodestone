package node

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

const testAgentID = "11111111-2222-3333-4444-555555555555"

// fakePlayit spielt die Schnittstelle von playit.gg.
type fakePlayit struct {
	mu       sync.Mutex
	tunnels  []map[string]any
	created  []map[string]any
	deleted  []string
	failWith string // Antwort mit status „fail“ beim Anlegen
	pending  int    // so viele Listenabfragen lang bleibt ein neuer Tunnel „pending“
	badKey   bool
}

func (f *fakePlayit) handler(t *testing.T) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		reply := func(status string, data any) {
			_ = json.NewEncoder(w).Encode(map[string]any{"status": status, "data": data})
		}
		if r.Method != http.MethodPost {
			t.Errorf("playit-Aufruf mit %s", r.Method)
		}
		if f.badKey || r.Header.Get("Authorization") != "Agent-Key "+testSecret {
			reply("error", map[string]string{"type": "auth", "message": "InvalidAgentKey"})
			return
		}
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		switch r.URL.Path {
		case "/agents/rundata":
			reply("success", map[string]any{"agent_id": testAgentID})
		case "/tunnels/list":
			if body["agent_id"] != testAgentID {
				t.Errorf("Liste ohne Agent: %v", body)
			}
			list := []map[string]any{}
			for _, tunnel := range f.tunnels {
				entry := map[string]any{"id": tunnel["id"], "name": tunnel["name"], "alloc": tunnel["alloc"]}
				if left, _ := tunnel["pending"].(int); left > 0 {
					tunnel["pending"] = left - 1
					entry["alloc"] = map[string]any{"status": "pending"}
				}
				list = append(list, entry)
			}
			reply("success", map[string]any{"tunnels": list})
		case "/tunnels/create":
			if f.failWith != "" {
				reply("fail", f.failWith)
				return
			}
			f.created = append(f.created, body)
			id := "aaaaaaaa-0000-0000-0000-00000000000" + string(rune('0'+len(f.created)))
			alloc := map[string]any{"status": "allocated", "data": map[string]any{
				"assigned_domain": "abc.gl.at.ply.gg", "assigned_srv": "", "port_start": 4711,
			}}
			if body["tunnel_type"] == "minecraft-java" {
				alloc["data"].(map[string]any)["assigned_srv"] = "mein-server.joinmc.link"
			}
			f.tunnels = append(f.tunnels, map[string]any{"id": id, "name": body["name"], "alloc": alloc, "pending": f.pending})
			reply("success", map[string]string{"id": id})
		case "/tunnels/delete":
			f.deleted = append(f.deleted, body["tunnel_id"].(string))
			reply("success", nil)
		default:
			t.Errorf("unbekannter Pfad %s", r.URL.Path)
		}
	})
}

func newPlayitTestManager(t *testing.T, fake *fakePlayit) *Manager {
	t.Helper()
	server := httptest.NewServer(fake.handler(t))
	t.Cleanup(server.Close)
	manager := newTestManager(t, testConfig(t), newFakeRuntime())
	manager.playit.apiBase = server.URL
	manager.playit.pollEvery = time.Millisecond
	if err := manager.ConfigurePlayit(context.Background(), testSecret); err != nil {
		t.Fatal(err)
	}
	return manager
}

func TestPlayitTunnelsNeedASecret(t *testing.T) {
	manager := newTestManager(t, testConfig(t), newFakeRuntime())
	if _, err := manager.PlayitTunnels(context.Background()); !errors.Is(err, errBadRequest) {
		t.Fatalf("ohne Schlüssel muss abgelehnt werden: %v", err)
	}
	_, err := manager.CreatePlayitTunnel(context.Background(), PlayitTunnelRequest{Name: "ls-abc-java", Kind: TunnelJava, LocalPort: 25565})
	if !errors.Is(err, errBadRequest) {
		t.Fatalf("ohne Schlüssel muss abgelehnt werden: %v", err)
	}
}

func TestCreatePlayitTunnels(t *testing.T) {
	fake := &fakePlayit{}
	manager := newPlayitTestManager(t, fake)
	ctx := context.Background()

	java, err := manager.CreatePlayitTunnel(ctx, PlayitTunnelRequest{Name: "ls-abc-java", Kind: TunnelJava, LocalPort: 25570})
	if err != nil {
		t.Fatal(err)
	}
	if java.Address != "mein-server.joinmc.link" || java.State != "allocated" || java.ID == "" {
		t.Errorf("Java-Tunnel: %+v", java)
	}
	body := fake.created[0]
	origin := body["origin"].(map[string]any)
	data := origin["data"].(map[string]any)
	if body["tunnel_type"] != "minecraft-java" || body["port_type"] != "tcp" || body["port_count"] != float64(1) || body["enabled"] != true ||
		origin["type"] != "agent" || data["agent_id"] != testAgentID || data["local_ip"] != "127.0.0.1" || data["local_port"] != float64(25570) {
		t.Errorf("Anfrage für Java: %v", body)
	}

	voice, err := manager.CreatePlayitTunnel(ctx, PlayitTunnelRequest{Name: "ls-abc-voice", Kind: TunnelVoicechat, LocalPort: 24454})
	if err != nil {
		t.Fatal(err)
	}
	if voice.Address != "abc.gl.at.ply.gg:4711" {
		t.Errorf("Adresse des Voicechat-Tunnels: %q", voice.Address)
	}
	body = fake.created[1]
	if body["port_type"] != "udp" || body["tunnel_type"] != nil || body["name"] != "ls-abc-voice" {
		t.Errorf("Anfrage für Voicechat: %v", body)
	}

	if _, err := manager.CreatePlayitTunnel(ctx, PlayitTunnelRequest{Name: "ls-abc-java", Kind: TunnelJava, LocalPort: 25570}); !errors.Is(err, ErrTunnelExists) {
		t.Errorf("doppelter Tunnel: %v", err)
	}
	if len(fake.created) != 2 {
		t.Errorf("es darf nichts doppelt angelegt werden: %d", len(fake.created))
	}

	tunnels, err := manager.PlayitTunnels(ctx)
	if err != nil || len(tunnels) != 2 {
		t.Fatalf("Liste: %v %v", tunnels, err)
	}

	if err := manager.DeletePlayitTunnel(ctx, java.ID); err != nil {
		t.Fatal(err)
	}
	if len(fake.deleted) != 1 || fake.deleted[0] != java.ID {
		t.Errorf("gelöscht: %v", fake.deleted)
	}
}

func TestCreatePlayitTunnelWaitsForTheAddress(t *testing.T) {
	fake := &fakePlayit{pending: 3}
	manager := newPlayitTestManager(t, fake)
	tunnel, err := manager.CreatePlayitTunnel(context.Background(), PlayitTunnelRequest{Name: "ls-abc-voice", Kind: TunnelVoicechat, LocalPort: 24454})
	if err != nil {
		t.Fatal(err)
	}
	if tunnel.Address == "" || tunnel.State != "allocated" {
		t.Errorf("die Adresse muss nach dem Warten da sein: %+v", tunnel)
	}
}

func TestCreatePlayitTunnelRejectsBadInput(t *testing.T) {
	fake := &fakePlayit{}
	manager := newPlayitTestManager(t, fake)
	ctx := context.Background()
	for _, bad := range []PlayitTunnelRequest{
		{Name: "", Kind: TunnelJava, LocalPort: 25565},
		{Name: "Gross Name", Kind: TunnelJava, LocalPort: 25565},
		{Name: "../x", Kind: TunnelJava, LocalPort: 25565},
		{Name: strings.Repeat("a", 41), Kind: TunnelJava, LocalPort: 25565},
		{Name: "ls-abc-java", Kind: "rust", LocalPort: 25565},
		{Name: "ls-abc-java", Kind: TunnelJava, LocalPort: 0},
		{Name: "ls-abc-java", Kind: TunnelJava, LocalPort: 70000},
	} {
		if _, err := manager.CreatePlayitTunnel(ctx, bad); !errors.Is(err, errBadRequest) {
			t.Errorf("%+v hätte abgelehnt werden müssen: %v", bad, err)
		}
	}
	if err := manager.DeletePlayitTunnel(ctx, "../../etc"); !errors.Is(err, errBadRequest) {
		t.Errorf("ungültige Kennung: %v", err)
	}
	if len(fake.created) != 0 || len(fake.deleted) != 0 {
		t.Error("bei ungültiger Eingabe darf nichts bei playit ankommen")
	}
}

func TestPlayitErrorsAreExplained(t *testing.T) {
	fake := &fakePlayit{failWith: "RequiresVerifiedAccount"}
	manager := newPlayitTestManager(t, fake)
	ctx := context.Background()
	request := PlayitTunnelRequest{Name: "ls-abc-java", Kind: TunnelJava, LocalPort: 25565}

	_, err := manager.CreatePlayitTunnel(ctx, request)
	if !errors.Is(err, errBadRequest) || !strings.Contains(err.Error(), "Bestätige") {
		t.Errorf("unbestätigtes Konto: %v", err)
	}
	fake.failWith = "SomethingNew"
	if _, err := manager.CreatePlayitTunnel(ctx, request); !errors.Is(err, errUpstream) || !strings.Contains(err.Error(), "SomethingNew") {
		t.Errorf("unbekannter Fehler: %v", err)
	}
	fake.badKey = true
	_, err = manager.PlayitTunnels(ctx)
	if !errors.Is(err, errBadRequest) || strings.Contains(err.Error(), testSecret) {
		t.Errorf("abgelehnter Schlüssel: %v", err)
	}
}

func TestPlayitIsUnreachable(t *testing.T) {
	manager := newTestManager(t, testConfig(t), newFakeRuntime())
	manager.playit.apiBase = "http://127.0.0.1:1"
	if err := manager.ConfigurePlayit(context.Background(), testSecret); err != nil {
		t.Fatal(err)
	}
	if _, err := manager.PlayitTunnels(context.Background()); !errors.Is(err, errUpstream) {
		t.Errorf("nicht erreichbar: %v", err)
	}
}
