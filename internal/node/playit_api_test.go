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
	mu        sync.Mutex
	tunnels   []map[string]any
	created   []map[string]any
	deleted   []string
	failWith  string // Antwort mit status „fail“ beim Anlegen
	errorWith any    // Antwort mit status „error“ beim Anlegen
	pending   int    // so viele Listenabfragen lang bleibt ein neuer Tunnel „pending“
	badKey    bool

	premium     bool             // account_features.regional_tunnels
	domains     []map[string]any // Antwort von /domains/list
	domainsFail string           // Antwort mit status „fail“ von /domains/list

	claimState string // Antwort von /claim/setup
	claimFail  string // Antwort mit status „fail“ von /claim/setup
	claimCode  string // der zuletzt gemeldete Code
	exchanges  int    // so oft wurde /claim/exchange aufgerufen
}

// claim spielt die Verbindung (/claim/setup und /claim/exchange); beide Aufrufe kommen ohne Schlüssel.
func (f *fakePlayit) claim(t *testing.T, w http.ResponseWriter, r *http.Request, reply func(string, any)) {
	if r.Header.Get("Authorization") != "" {
		t.Errorf("die Verbindung darf keinen Schlüssel schicken: %q", r.Header.Get("Authorization"))
	}
	var body map[string]any
	_ = json.NewDecoder(r.Body).Decode(&body)
	switch r.URL.Path {
	case "/claim/setup":
		if body["agent_type"] != "self-managed" || body["version"] == "" {
			t.Errorf("Anfrage der Verbindung: %v", body)
		}
		if f.claimFail != "" {
			reply("fail", f.claimFail)
			return
		}
		f.claimCode, _ = body["code"].(string)
		reply("success", f.claimState)
	case "/claim/exchange":
		f.exchanges++
		if body["code"] != f.claimCode {
			t.Errorf("Code beim Tausch: %v", body)
		}
		if f.claimState != "UserAccepted" {
			reply("fail", "NotAccepted")
			return
		}
		reply("success", map[string]string{"secret_key": testSecret})
	default:
		t.Errorf("unbekannter Pfad %s", r.URL.Path)
	}
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
		if strings.HasPrefix(r.URL.Path, "/claim/") {
			f.claim(t, w, r, reply)
			return
		}
		if f.badKey || r.Header.Get("Authorization") != "Agent-Key "+testSecret {
			reply("error", map[string]string{"type": "auth", "message": "InvalidAgentKey"})
			return
		}
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		switch r.URL.Path {
		case "/agents/rundata":
			reply("success", map[string]any{"agent_id": testAgentID, "account_features": map[string]any{"regional_tunnels": f.premium}})
		case "/domains/list":
			if f.domainsFail != "" {
				reply("fail", f.domainsFail)
				return
			}
			reply("success", map[string]any{"domains": f.domains})
		case "/tunnels/list":
			if body["agent_id"] != testAgentID {
				t.Errorf("Liste ohne Agent: %v", body)
			}
			list := []map[string]any{}
			for _, tunnel := range f.tunnels {
				entry := map[string]any{"id": tunnel["id"], "name": tunnel["name"], "alloc": tunnel["alloc"], "tunnel_type": tunnel["tunnel_type"], "region": tunnel["region"], "domain": tunnel["domain"]}
				if left, _ := tunnel["pending"].(int); left > 0 {
					tunnel["pending"] = left - 1
					entry["alloc"] = map[string]any{"status": "pending"}
				}
				list = append(list, entry)
			}
			reply("success", map[string]any{"tunnels": list})
		case "/tunnels/create", "/v1/tunnels/create":
			if f.errorWith != nil {
				reply("error", f.errorWith)
				return
			}
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
			entry := map[string]any{"id": id, "name": body["name"], "alloc": alloc, "pending": f.pending, "tunnel_type": body["tunnel_type"]}
			if choice, ok := body["alloc"].(map[string]any); ok && choice["type"] == "region" {
				entry["region"] = choice["details"].(map[string]any)["region"]
			}
			if r.URL.Path == "/v1/tunnels/create" {
				entry["tunnel_type"] = "minecraft-java"
				hostname := body["alloc"].(map[string]any)["details"].(map[string]any)["hostname_id"]
				for _, domain := range f.domains {
					if domain["id"] == hostname {
						entry["domain"] = map[string]any{"id": domain["id"], "name": domain["name"]}
					}
				}
			}
			f.tunnels = append(f.tunnels, entry)
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

const testDomainID = "dddddddd-0000-0000-0000-000000000001"

func TestPlayitOptions(t *testing.T) {
	fake := &fakePlayit{premium: true, domains: []map[string]any{
		{"id": testDomainID, "name": "play.example.org", "is_external": true, "target": nil},
		{"id": "dddddddd-0000-0000-0000-000000000002", "name": "belegt.example.org", "is_external": false, "target": map[string]any{"type": "tunnel", "details": map[string]any{"tunnel_id": "x"}}},
		{"id": "kaputt", "name": "ungueltig.example.org"},
	}}
	manager := newPlayitTestManager(t, fake)
	options, err := manager.PlayitOptions(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !options.Premium || len(options.Regions) == 0 || options.DomainsProblem != "" {
		t.Fatalf("Optionen: %+v", options)
	}
	if len(options.Domains) != 2 || options.Domains[0].Used || !options.Domains[0].External || !options.Domains[1].Used {
		t.Errorf("Domains: %+v", options.Domains)
	}

	// Ohne Premium und wenn playit die Domainliste ablehnt, geht der Rest trotzdem
	fake.premium, fake.domainsFail = false, "Irgendwas"
	options, err = manager.PlayitOptions(context.Background())
	if err != nil || options.Premium || len(options.Domains) != 0 || options.DomainsProblem == "" || len(options.Regions) == 0 {
		t.Errorf("Optionen ohne Domains: %+v %v", options, err)
	}

	empty := newTestManager(t, testConfig(t), newFakeRuntime())
	if _, err := empty.PlayitOptions(context.Background()); !errors.Is(err, errBadRequest) {
		t.Errorf("ohne Schlüssel: %v", err)
	}
}

func TestCreatePlayitTunnelInARegion(t *testing.T) {
	fake := &fakePlayit{premium: true}
	manager := newPlayitTestManager(t, fake)
	ctx := context.Background()

	tunnel, err := manager.CreatePlayitTunnel(ctx, PlayitTunnelRequest{Name: "ls-abc-java", Kind: TunnelJava, LocalPort: 25570, Region: "europe"})
	if err != nil {
		t.Fatal(err)
	}
	alloc, _ := fake.created[0]["alloc"].(map[string]any)
	if alloc["type"] != "region" || alloc["details"].(map[string]any)["region"] != "europe" {
		t.Errorf("Anfrage mit Region: %v", fake.created[0])
	}
	if tunnel.Region != "Europa" {
		t.Errorf("Name der Region: %+v", tunnel)
	}

	// „Automatisch“ schickt keine Wahl mit
	if _, err := manager.CreatePlayitTunnel(ctx, PlayitTunnelRequest{Name: "ls-abc-voice", Kind: TunnelVoicechat, LocalPort: 24454, Region: "global"}); err != nil {
		t.Fatal(err)
	}
	if _, has := fake.created[1]["alloc"]; has {
		t.Errorf("bei „global“ darf keine Region mitgehen: %v", fake.created[1])
	}
}

func TestCreatePlayitTunnelWithADomain(t *testing.T) {
	fake := &fakePlayit{premium: true, domains: []map[string]any{{"id": testDomainID, "name": "play.example.org"}}}
	manager := newPlayitTestManager(t, fake)
	tunnel, err := manager.CreatePlayitTunnel(context.Background(), PlayitTunnelRequest{Name: "ls-abc-java", Kind: TunnelJava, LocalPort: 25570, DomainID: testDomainID})
	if err != nil {
		t.Fatal(err)
	}
	if tunnel.Domain != "play.example.org" || tunnel.Address != "play.example.org" {
		t.Errorf("Tunnel mit Domain: %+v", tunnel)
	}
	body := fake.created[0]
	ports := body["ports"].(map[string]any)
	alloc := body["alloc"].(map[string]any)
	data := body["origin"].(map[string]any)["data"].(map[string]any)
	fields := data["config"].(map[string]any)["fields"].([]any)
	if ports["type"] != "tunnel-type" || ports["details"] != "minecraft-java" || alloc["type"] != "hostname" ||
		alloc["details"].(map[string]any)["hostname_id"] != testDomainID || data["agent_id"] != testAgentID || body["name"] != "ls-abc-java" ||
		len(fields) != 2 || fields[1].(map[string]any)["value"] != "25570" {
		t.Errorf("Anfrage mit Domain: %v", body)
	}
}

func TestCreatePlayitTunnelRejectsBadChoices(t *testing.T) {
	fake := &fakePlayit{premium: true}
	manager := newPlayitTestManager(t, fake)
	ctx := context.Background()
	for _, bad := range []PlayitTunnelRequest{
		{Name: "ls-abc-java", Kind: TunnelJava, LocalPort: 25565, Region: "mars"},
		{Name: "ls-abc-java", Kind: TunnelJava, LocalPort: 25565, DomainID: "../x"},
		{Name: "ls-abc-voice", Kind: TunnelVoicechat, LocalPort: 24454, DomainID: testDomainID},
		{Name: "ls-abc-java", Kind: TunnelJava, LocalPort: 25565, Region: "europe", DomainID: testDomainID},
	} {
		if _, err := manager.CreatePlayitTunnel(ctx, bad); !errors.Is(err, errBadRequest) {
			t.Errorf("%+v hätte abgelehnt werden müssen: %v", bad, err)
		}
	}
	if len(fake.created) != 0 {
		t.Error("bei ungültiger Wahl darf nichts bei playit ankommen")
	}

	for code, contains := range map[string]string{
		"RegionRequiresPlayitPremium": "Premium",
		"RegionNotSupported":          "Region",
		"InvalidHostnameId":           "Domain",
		"HostnameHasTunnelTypeTarget": "Domain",
	} {
		fake.failWith = code
		_, err := manager.CreatePlayitTunnel(ctx, PlayitTunnelRequest{Name: "ls-abc-java", Kind: TunnelJava, LocalPort: 25565})
		if !errors.Is(err, errBadRequest) || !strings.Contains(err.Error(), contains) {
			t.Errorf("%s: %v", code, err)
		}
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

func TestPlayitErrorResponsesAreTold(t *testing.T) {
	fake := &fakePlayit{}
	manager := newPlayitTestManager(t, fake)
	ctx := context.Background()
	request := PlayitTunnelRequest{Name: "ls-abc-voice", Kind: TunnelVoicechat, LocalPort: 24454}

	for _, test := range []struct {
		reply    any
		upstream bool
		contains string
	}{
		{map[string]any{"type": "internal", "message": map[string]string{"trace_id": "abc123"}}, true, "abc123"},
		{map[string]any{"type": "validation", "message": "missing field `port_type`"}, true, "port_type"},
		{map[string]any{"type": "path-not-found", "message": map[string]string{"path": "/x"}}, true, "Schnittstelle"},
		{map[string]any{"type": "auth", "message": "DefaultAgentBlocked"}, false, "Standard-Agenten"},
		{map[string]any{"type": "auth", "message": "AgentNotSelfManaged"}, false, "Standard-Agenten"},
		{map[string]any{"type": "auth", "message": "SelfManagedAgentCanOnlyAffectSelf"}, false, "darf das nicht"},
		{map[string]any{"type": "auth", "message": "EmailMustBeVerified"}, false, "Bestätige"},
		{map[string]any{"type": "auth", "message": "InvalidAgentKey"}, false, "lehnt den Schlüssel ab"},
	} {
		fake.errorWith = test.reply
		_, err := manager.CreatePlayitTunnel(ctx, request)
		if err == nil || !strings.Contains(err.Error(), test.contains) {
			t.Errorf("%v: Meldung %v enthält %q nicht", test.reply, err, test.contains)
			continue
		}
		if errors.Is(err, errUpstream) != test.upstream || errors.Is(err, errBadRequest) == test.upstream {
			t.Errorf("%v: falsche Art des Fehlers: %v", test.reply, err)
		}
		if strings.Contains(err.Error(), "interner Fehler") != (test.reply.(map[string]any)["type"] == "internal") {
			t.Errorf("%v: nur echte interne Fehler heißen so: %v", test.reply, err)
		}
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
