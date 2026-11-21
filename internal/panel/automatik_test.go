package panel

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/lodestone-mc/lodestone/internal/node"
)

// autoNode ergänzt die fakeNode um die Routen der Automatik; alles andere reicht es an sie weiter.
type autoNode struct {
	mu       sync.Mutex
	sleep    map[string]node.SleepConfig
	requests []node.WatchRequest
	queued   []node.ConsoleEvent
	seq      int64
	analysis node.CrashAnalysis
}

func withAutoNode(t *testing.T, p *Panel, nodeID string) *autoNode {
	t.Helper()
	stored, err := p.store.NodeByID(nodeID)
	if err != nil {
		t.Fatal(err)
	}
	target, _ := url.Parse(stored.URL)
	fallback := httputil.NewSingleHostReverseProxy(target)
	fake := &autoNode{sleep: map[string]node.SleepConfig{}}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/servers/{id}/sleep", func(w http.ResponseWriter, r *http.Request) {
		fake.mu.Lock()
		defer fake.mu.Unlock()
		config, ok := fake.sleep[r.PathValue("id")]
		if !ok {
			config = node.SleepConfig{IdleMinutes: 10}
		}
		writeJSON(w, 200, node.SleepStatus{SleepConfig: config, Supported: true, Port: 28000})
	})
	mux.HandleFunc("PUT /api/servers/{id}/sleep", func(w http.ResponseWriter, r *http.Request) {
		var config node.SleepConfig
		_ = json.NewDecoder(r.Body).Decode(&config)
		if config.IdleMinutes == 0 {
			config.IdleMinutes = 10
		}
		fake.mu.Lock()
		fake.sleep[r.PathValue("id")] = config
		fake.mu.Unlock()
		writeJSON(w, 200, node.SleepStatus{SleepConfig: config, Supported: true, Listening: config.Enabled, Port: 28000})
	})
	mux.HandleFunc("GET /api/servers/{id}/crash-analysis", func(w http.ResponseWriter, r *http.Request) {
		fake.mu.Lock()
		defer fake.mu.Unlock()
		writeJSON(w, 200, fake.analysis)
	})
	mux.HandleFunc("POST /api/console-watch", func(w http.ResponseWriter, r *http.Request) {
		var request node.WatchRequest
		_ = json.NewDecoder(r.Body).Decode(&request)
		fake.mu.Lock()
		defer fake.mu.Unlock()
		fake.requests = append(fake.requests, request)
		events := fake.queued
		fake.queued = nil
		writeJSON(w, 200, node.WatchResponse{Boot: "boot-1", Latest: fake.seq, Events: append([]node.ConsoleEvent{}, events...)})
	})
	mux.Handle("/", fallback)
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)
	stored.URL = server.URL
	if err := p.store.UpdateNode(stored); err != nil {
		t.Fatal(err)
	}
	return fake
}

func (a *autoNode) queue(events ...node.ConsoleEvent) {
	a.mu.Lock()
	defer a.mu.Unlock()
	for _, event := range events {
		a.seq++
		event.Seq = a.seq
		a.queued = append(a.queued, event)
	}
}

func (f *fakeNode) setState(id string, state node.State) {
	f.mu.Lock()
	defer f.mu.Unlock()
	view := f.servers[id]
	view.State = state
	f.servers[id] = view
}

func (f *fakeNode) poweredList() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.powered...)
}

// member legt einen Benutzer mit Rolle im Projekt an und meldet ihn an.
func member(t *testing.T, admin *client, base, projectID, name, role string) *client {
	t.Helper()
	var user User
	_ = json.Unmarshal(admin.expect(201, "POST", "/api/users", map[string]any{"username": name, "password": "ein-gutes-passwort"}), &user)
	if role != "" {
		admin.expect(200, "PUT", "/api/projects/"+projectID+"/members/"+user.ID, map[string]string{"role": role})
	}
	c := newClient(t, base)
	c.expect(200, "POST", "/api/auth/login", map[string]string{"username": name, "password": "ein-gutes-passwort"})
	return c
}

func eventsOf(t *testing.T, p *Panel, projectID string) []Event {
	t.Helper()
	list, err := p.store.ListEvents(projectID, 100)
	if err != nil {
		t.Fatal(err)
	}
	return list
}

func hasEvent(list []Event, kind, contains string) bool {
	for _, event := range list {
		if event.Kind == kind && strings.Contains(event.Message, contains) {
			return true
		}
	}
	return false
}

// ---------------------------------------------------------------- Schlafmodus

func TestSleepSettingRightsAndValidation(t *testing.T) {
	p, admin, _, nodeID, server, projectID := serverProject(t)
	fake := withAutoNode(t, p, nodeID)
	base := strings.TrimSuffix(admin.base, "/")
	viewer := member(t, admin, base, projectID, "betrachter", "viewer")
	operator := member(t, admin, base, projectID, "operator", "operator")
	stranger := member(t, admin, base, projectID, "fremd", "")
	path := "/api/servers/" + server.ID + "/sleep"

	var status sleepDTO
	_ = json.Unmarshal(viewer.expect(200, "GET", path, nil), &status)
	if !status.Allowed || status.Enabled || !status.Supported {
		t.Fatalf("Status: %+v", status)
	}
	viewer.expect(403, "PUT", path, map[string]any{"enabled": true})
	operator.expect(403, "PUT", path, map[string]any{"enabled": true})
	stranger.expect(404, "GET", path, nil)
	admin.expect(400, "PUT", path, map[string]any{"enabled": true, "idleMinutes": 5000})
	admin.expect(400, "PUT", path, map[string]any{"enabled": true, "unbekannt": 1})
	_ = json.Unmarshal(admin.expect(200, "PUT", path, map[string]any{"enabled": true, "idleMinutes": 15}), &status)
	if !status.Enabled || !status.Listening || status.IdleMinutes != 15 {
		t.Fatalf("eingeschaltet: %+v", status)
	}
	fake.mu.Lock()
	config := fake.sleep[server.ID]
	fake.mu.Unlock()
	if !config.Enabled || config.IdleMinutes != 15 {
		t.Errorf("beim Node angekommen: %+v", config)
	}
}

func TestSleepNotForBackends(t *testing.T) {
	p, web, _, nodeID := setupPanel(t)
	fake := withAutoNode(t, p, nodeID)
	admin := newClient(t, web.URL)
	admin.expect(201, "POST", "/api/auth/setup", map[string]string{"username": "admin", "password": "ein-gutes-passwort"})
	var project ProjectDTO
	_ = json.Unmarshal(admin.expect(201, "POST", "/api/projects", map[string]string{"name": "Netz", "kind": "network"}), &project)
	var proxy, backend ServerDTO
	_ = json.Unmarshal(admin.expect(201, "POST", "/api/projects/"+project.ID+"/servers", serverRequest{Name: "Proxy", NodeID: nodeID, Software: "velocity", Version: "3.4.0"}), &proxy)
	_ = json.Unmarshal(admin.expect(201, "POST", "/api/projects/"+project.ID+"/servers", serverRequest{Name: "Lobby", NodeID: nodeID, Software: "paper", Version: "1.21.4", EULA: true}), &backend)

	var status sleepDTO
	_ = json.Unmarshal(admin.expect(200, "GET", "/api/servers/"+backend.ID+"/sleep", nil), &status)
	if status.Allowed || !strings.Contains(status.Reason, "Proxy") {
		t.Fatalf("Backend: %+v", status)
	}
	body := admin.expect(400, "PUT", "/api/servers/"+backend.ID+"/sleep", map[string]any{"enabled": true})
	if !strings.Contains(string(body), "Backend-Server hinter dem Proxy") {
		t.Errorf("Meldung: %s", body)
	}
	// Ausschalten geht immer
	admin.expect(200, "PUT", "/api/servers/"+backend.ID+"/sleep", map[string]any{"enabled": false})
	admin.expect(200, "PUT", "/api/servers/"+proxy.ID+"/sleep", map[string]any{"enabled": true})
	fake.mu.Lock()
	defer fake.mu.Unlock()
	if !fake.sleep[proxy.ID].Enabled || fake.sleep[backend.ID].Enabled {
		t.Errorf("Node: %+v", fake.sleep)
	}
}

func TestProxySleepAndWakeTakeBackendsAlong(t *testing.T) {
	p, web, fakeBase, nodeID := setupPanel(t)
	fake := withAutoNode(t, p, nodeID)
	admin := newClient(t, web.URL)
	admin.expect(201, "POST", "/api/auth/setup", map[string]string{"username": "admin", "password": "ein-gutes-passwort"})
	var project ProjectDTO
	_ = json.Unmarshal(admin.expect(201, "POST", "/api/projects", map[string]string{"name": "Netz", "kind": "network"}), &project)
	var proxy, backend ServerDTO
	_ = json.Unmarshal(admin.expect(201, "POST", "/api/projects/"+project.ID+"/servers", serverRequest{Name: "Proxy", NodeID: nodeID, Software: "velocity", Version: "3.4.0"}), &proxy)
	_ = json.Unmarshal(admin.expect(201, "POST", "/api/projects/"+project.ID+"/servers", serverRequest{Name: "Lobby", NodeID: nodeID, Software: "paper", Version: "1.21.4", EULA: true}), &backend)
	fakeBase.setState(backend.ID, node.StateRunning)

	ctx := context.Background()
	p.automatikTick(ctx, time.Now()) // erster Kontakt
	fake.queue(node.ConsoleEvent{ServerID: proxy.ID, Kind: "sleep"})
	p.invalidate(nodeID)
	p.automatikTick(ctx, time.Now())
	waitUntil(t, "Backend wird gestoppt", func() bool { return strings.Contains(strings.Join(fakeBase.poweredList(), " "), "stop:"+backend.ID) })

	fake.queue(node.ConsoleEvent{ServerID: proxy.ID, Kind: "wake", Player: "Steve"})
	p.invalidate(nodeID)
	p.automatikTick(ctx, time.Now())
	waitUntil(t, "Backend wird gestartet", func() bool { return strings.Contains(strings.Join(fakeBase.poweredList(), " "), "start:"+backend.ID) })

	events := eventsOf(t, p, project.ID)
	if !hasEvent(events, "sleep", "Proxy schläft ein") || !hasEvent(events, "wake", "weil Steve beitreten möchte") {
		t.Fatalf("Ereignisse: %+v", events)
	}
}

// ---------------------------------------------------------------- Konsolen-Auslöser

func TestConsoleTriggersValidationAndRights(t *testing.T) {
	p, admin, _, nodeID, server, projectID := serverProject(t)
	_ = withAutoNode(t, p, nodeID)
	base := strings.TrimSuffix(admin.base, "/")
	operator := member(t, admin, base, projectID, "operator", "operator")
	stranger := member(t, admin, base, projectID, "fremd", "")
	path := "/api/projects/" + projectID + "/console-triggers"

	var other ProjectDTO
	_ = json.Unmarshal(admin.expect(201, "POST", "/api/projects", map[string]string{"name": "Anderes", "kind": "server"}), &other)
	var otherServer ServerDTO
	_ = json.Unmarshal(admin.expect(201, "POST", "/api/projects/"+other.ID+"/servers", serverRequest{Name: "Fremd", NodeID: nodeID, Software: "paper", Version: "1.21.4", EULA: true}), &otherServer)

	admin.expect(400, "POST", path, map[string]any{"name": "Kaputt", "pattern": "([a-z"})
	admin.expect(400, "POST", path, map[string]any{"name": "Alles", "pattern": ".*"})
	admin.expect(400, "POST", path, map[string]any{"name": "Lang", "pattern": strings.Repeat("a", 201)})
	admin.expect(400, "POST", path, map[string]any{"name": "", "pattern": "x"})
	admin.expect(400, "POST", path, map[string]any{"name": "Stufe", "pattern": "x", "level": "kritisch"})
	admin.expect(400, "POST", path, map[string]any{"name": "Fremd", "pattern": "x", "serverId": otherServer.ID})
	admin.expect(400, "POST", path, map[string]any{"name": "Feld", "pattern": "x", "unbekannt": true})
	operator.expect(403, "GET", path, nil)
	operator.expect(403, "POST", path, map[string]any{"name": "Lag", "pattern": "Can't keep up"})
	stranger.expect(404, "GET", path, nil)

	var created ConsoleTrigger
	_ = json.Unmarshal(admin.expect(201, "POST", path, map[string]any{"name": "Lag", "pattern": `Can't keep up!`, "level": "warn", "serverId": server.ID}), &created)
	if created.ID == "" || !created.Enabled || created.Level != "warn" {
		t.Fatalf("angelegt: %+v", created)
	}
	var updated ConsoleTrigger
	_ = json.Unmarshal(admin.expect(200, "PATCH", "/api/console-triggers/"+created.ID, map[string]any{"enabled": false}), &updated)
	if updated.Enabled || updated.Pattern != `Can't keep up!` {
		t.Fatalf("geändert: %+v", updated)
	}
	operator.expect(403, "PATCH", "/api/console-triggers/"+created.ID, map[string]any{"enabled": true})
	stranger.expect(404, "DELETE", "/api/console-triggers/"+created.ID, nil)

	for i := 1; i < maxTriggers; i++ {
		admin.expect(201, "POST", path, map[string]any{"name": "T" + strconv.Itoa(i), "pattern": "Muster " + strconv.Itoa(i)})
	}
	admin.expect(409, "POST", path, map[string]any{"name": "Zu viel", "pattern": "x"})
	admin.expect(204, "DELETE", "/api/console-triggers/"+created.ID, nil)
	admin.expect(404, "PATCH", "/api/console-triggers/"+created.ID, map[string]any{"enabled": true})
}

func TestConsoleEventsBecomeEventsAndRunSchedule(t *testing.T) {
	p, admin, fakeBase, nodeID, server, projectID := serverProject(t)
	fake := withAutoNode(t, p, nodeID)
	fakeBase.setState(server.ID, node.StateRunning)

	var schedule scheduleDTO
	_ = json.Unmarshal(admin.expect(201, "POST", "/api/projects/"+projectID+"/schedules", map[string]any{
		"serverId": server.ID, "name": "Aufräumen", "kind": "daily", "atMinute": 240,
		"steps": []Step{{Action: "command", Value: "say Aufräumen läuft"}},
	}), &schedule)
	var trigger ConsoleTrigger
	_ = json.Unmarshal(admin.expect(201, "POST", "/api/projects/"+projectID+"/console-triggers", map[string]any{
		"name": "Lag", "pattern": `Can't keep up!`, "level": "warn", "scheduleId": schedule.ID,
	}), &trigger)
	// ein Zeitplan aus einem fremden Projekt geht nicht
	var other ProjectDTO
	_ = json.Unmarshal(admin.expect(201, "POST", "/api/projects", map[string]string{"name": "Anderes", "kind": "server"}), &other)
	admin.expect(400, "POST", "/api/projects/"+other.ID+"/console-triggers", map[string]any{"name": "Fremd", "pattern": "x", "scheduleId": schedule.ID})

	ctx := context.Background()
	now := time.Date(2026, 11, 21, 12, 0, 0, 0, time.UTC)
	// Ereignisse vor dem ersten Kontakt werden übersprungen
	fake.queue(node.ConsoleEvent{ServerID: server.ID, Kind: "join", Player: "Alt"})
	p.automatikTick(ctx, now)
	fake.mu.Lock()
	sent := fake.requests[len(fake.requests)-1]
	fake.mu.Unlock()
	if len(sent.Patterns[server.ID]) != 1 || sent.Patterns[server.ID][0].ID != trigger.ID || sent.Patterns[server.ID][0].Pattern != `Can't keep up!` {
		t.Fatalf("Muster an den Node: %+v", sent.Patterns)
	}

	fake.queue(
		node.ConsoleEvent{ServerID: server.ID, Kind: "join", Player: "Steve"},
		node.ConsoleEvent{ServerID: server.ID, Kind: "leave", Player: "Steve"},
		node.ConsoleEvent{ServerID: server.ID, Kind: "match", Pattern: trigger.ID, Line: "[WARN]: Can't keep up! Running 2000ms behind"},
		node.ConsoleEvent{ServerID: server.ID, Kind: "match", Pattern: trigger.ID, Line: "[WARN]: Can't keep up! zweites Mal"},
		node.ConsoleEvent{ServerID: server.ID, Kind: "match", Pattern: "unbekannt", Line: "x"},
		node.ConsoleEvent{ServerID: server.ID, Kind: "match", Pattern: trigger.ID, Line: "drittes Mal @everyone"}, // Abklingzeit
		node.ConsoleEvent{ServerID: "fremder-server", Kind: "join", Player: "Mallory"},
	)
	p.automatikTick(ctx, now.Add(5*time.Second))
	fake.mu.Lock()
	cursor := fake.requests[len(fake.requests)-1]
	fake.mu.Unlock()
	if cursor.Boot != "boot-1" || cursor.After != 1 {
		t.Errorf("Cursor: %+v", cursor)
	}

	events := eventsOf(t, p, projectID)
	if hasEvent(events, "player_join", "Alt") || hasEvent(events, "player_join", "Mallory") {
		t.Error("alte oder fremde Ereignisse gemeldet")
	}
	if !hasEvent(events, "player_join", "Steve hat Main betreten.") || !hasEvent(events, "player_leave", "Steve hat Main verlassen.") {
		t.Fatalf("Beitritt/Verlassen fehlt: %+v", events)
	}
	matches := 0
	for _, event := range events {
		if event.Kind == "console_match" {
			matches++
			if event.Level != "warn" || !strings.Contains(event.Message, "„Lag“") || !strings.Contains(event.Message, "2000ms") {
				t.Errorf("Treffer: %+v", event)
			}
		}
	}
	if matches != 1 {
		t.Errorf("%d Treffer gemeldet, erwartet 1 (Abklingzeit)", matches)
	}
	stored, _ := p.store.TriggerByID(trigger.ID)
	if stored.LastMatch != now.Add(5*time.Second).UnixMilli() || !strings.Contains(stored.LastLine, "2000ms") {
		t.Errorf("letzter Treffer: %+v", stored)
	}
	waitUntil(t, "Zeitplan läuft", func() bool {
		fakeBase.mu.Lock()
		defer fakeBase.mu.Unlock()
		return strings.Contains(strings.Join(fakeBase.commands, "|"), "say Aufräumen läuft")
	})

	// nach der Abklingzeit meldet er wieder
	fake.queue(node.ConsoleEvent{ServerID: server.ID, Kind: "match", Pattern: trigger.ID, Line: "<Mallory> Can't keep up! später @everyone"})
	waitUntil(t, "Zeitplan fertig", func() bool { return p.beginRun(schedule.ID) })
	p.endRun(schedule.ID)
	p.automatikTick(ctx, now.Add(time.Minute))
	events = eventsOf(t, p, projectID)
	if !hasEvent(events, "console_match", "später") {
		t.Fatal("nach der Abklingzeit kein Treffer")
	}
	if hasEvent(events, "console_match", "@everyone") || !hasEvent(events, "console_match", "@​everyone") {
		t.Error("eine Erwähnung aus dem Chat würde in Discord pingen")
	}
}

func TestNewEventKindsWorkInWebhooks(t *testing.T) {
	_, admin, _, _, _, projectID := serverProject(t)
	admin.expect(201, "POST", "/api/projects/"+projectID+"/webhooks", map[string]any{
		"name": "Spieler", "url": "https://example.org/hook", "kind": "generic", "events": []string{"player_join", "player_leave", "console_match", "sleep", "wake"},
	})
	if titles["player_join"] == "" || titles["wake"] == "" {
		t.Error("Beschriftungen fehlen")
	}
}

// ---------------------------------------------------------------- Absturzanalyse

func TestCrashEventNamesSuspectAndAnalysisRoute(t *testing.T) {
	p, admin, fakeBase, nodeID, server, projectID := serverProject(t)
	fake := withAutoNode(t, p, nodeID)
	fake.mu.Lock()
	fake.analysis = node.CrashAnalysis{Found: true, Summary: "Verdächtige Mod laut Absturzbericht: „Bad Mod“ (badmod).", Suspects: []node.CrashSuspect{{ModID: "badmod", Name: "Bad Mod", Reason: "suspected"}}, Missing: []node.MissingDependency{}, Mixins: []string{}}
	fake.mu.Unlock()

	// erste Runde merkt sich den Stand, die zweite sieht den Absturz
	p.watchOnce(context.Background())
	fakeBase.setExit(server.ID, &node.ExitInfo{Code: 1, At: time.Now().UnixMilli()})
	p.invalidate(nodeID)
	p.watchOnce(context.Background())
	if !hasEvent(eventsOf(t, p, projectID), "crash", "abgestürzt (Exit-Code 1). Verdächtige Mod laut Absturzbericht: „Bad Mod“ (badmod).") {
		t.Fatalf("Absturz-Ereignis: %+v", eventsOf(t, p, projectID))
	}

	var analysis node.CrashAnalysis
	_ = json.Unmarshal(admin.expect(200, "GET", "/api/servers/"+server.ID+"/crash-analysis", nil), &analysis)
	if !analysis.Found || analysis.Suspects[0].ModID != "badmod" {
		t.Fatalf("Analyse: %+v", analysis)
	}
	stranger := member(t, admin, strings.TrimSuffix(admin.base, "/"), projectID, "fremd", "")
	stranger.expect(404, "GET", "/api/servers/"+server.ID+"/crash-analysis", nil)
}

// ---------------------------------------------------------------- Discord

type discordFixture struct {
	p         *Panel
	admin     *client
	fake      *fakeNode
	server    ServerDTO
	projectID string
	private   ed25519.PrivateKey
	public    string
	token     string
	web       string
}

func newDiscordFixture(t *testing.T) *discordFixture {
	t.Helper()
	p, admin, fake, _, server, projectID := serverProject(t)
	public, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	return &discordFixture{p: p, admin: admin, fake: fake, server: server, projectID: projectID, private: private, public: hex.EncodeToString(public),
		token: "test-bot-token-nicht-echt", web: strings.TrimSuffix(admin.base, "/")}
}

func (f *discordFixture) configure(t *testing.T) {
	t.Helper()
	f.admin.expect(200, "PUT", "/api/projects/"+f.projectID+"/discord", map[string]any{
		"applicationId": "123456789012345678", "publicKey": f.public, "botToken": f.token, "enabled": true,
		"rules": []DiscordRule{
			{Kind: "role", ID: "111111111111111111", Label: "Alle", Actions: []string{"status", "players"}},
			{Kind: "user", ID: "222222222222222222", Label: "Anna", Actions: []string{"start", "stop", "restart"}},
		},
	})
}

// interact schickt eine von Discord signierte Anfrage.
func (f *discordFixture) interact(t *testing.T, payload any, sign bool, at time.Time) (int, map[string]any) {
	t.Helper()
	body, _ := json.Marshal(payload)
	request, _ := http.NewRequest("POST", f.web+"/api/public/discord/"+f.projectID+"/interactions", bytes.NewReader(body))
	timestamp := strconv.FormatInt(at.Unix(), 10)
	signature := strings.Repeat("00", ed25519.SignatureSize)
	if sign {
		signature = hex.EncodeToString(ed25519.Sign(f.private, append([]byte(timestamp), body...)))
	}
	request.Header.Set("X-Signature-Ed25519", signature)
	request.Header.Set("X-Signature-Timestamp", timestamp)
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	var out map[string]any
	_ = json.NewDecoder(response.Body).Decode(&out)
	return response.StatusCode, out
}

func command(name, user string, roles []string, server string) map[string]any {
	data := map[string]any{"name": name, "options": []any{}}
	if server != "" {
		data["options"] = []any{map[string]any{"name": "server", "type": 3, "value": server}}
	}
	return map[string]any{"type": 2, "data": data, "member": map[string]any{"user": map[string]string{"id": user, "username": "tester"}, "roles": roles}}
}

func content(t *testing.T, out map[string]any) (string, bool) {
	t.Helper()
	data, ok := out["data"].(map[string]any)
	if !ok || out["type"] != float64(4) {
		t.Fatalf("Antwort: %+v", out)
	}
	text, _ := data["content"].(string)
	return text, data["flags"] == float64(discordEphemeral)
}

func TestDiscordConfigStoresTokenEncrypted(t *testing.T) {
	f := newDiscordFixture(t)
	path := "/api/projects/" + f.projectID + "/discord"
	var view discordDTO
	_ = json.Unmarshal(f.admin.expect(200, "GET", path, nil), &view)
	if view.Configured || view.InteractionsPath != "/api/public/discord/"+f.projectID+"/interactions" {
		t.Fatalf("leer: %+v", view)
	}
	good := map[string]any{"applicationId": "123456789012345678", "publicKey": f.public, "enabled": true, "rules": []DiscordRule{}}
	for field, value := range map[string]any{"applicationId": "123", "publicKey": "xyz", "botToken": "kurz"} {
		body := map[string]any{}
		for key, entry := range good {
			body[key] = entry
		}
		body[field] = value
		f.admin.expect(400, "PUT", path, body)
	}
	bad := map[string]any{"applicationId": "123456789012345678", "publicKey": f.public, "enabled": true, "rules": []DiscordRule{{Kind: "role", ID: "12", Actions: []string{"status"}}}}
	f.admin.expect(400, "PUT", path, bad)
	bad["rules"] = []DiscordRule{{Kind: "role", ID: "111111111111111111", Actions: []string{"loeschen"}}}
	f.admin.expect(400, "PUT", path, bad)

	f.configure(t)
	data := f.admin.expect(200, "GET", path, nil)
	if strings.Contains(string(data), f.token) || strings.Contains(string(data), "botToken") {
		t.Fatalf("Token in der Antwort: %s", data)
	}
	_ = json.Unmarshal(data, &view)
	if !view.Configured || !view.HasToken || len(view.Rules) != 2 {
		t.Fatalf("gespeichert: %+v", view)
	}
	stored, _ := f.p.store.DiscordBot(f.projectID)
	if stored.BotToken == f.token || stored.BotToken == "" {
		t.Fatal("Token liegt im Klartext in der Datenbank")
	}
	if plain, err := f.p.unseal(stored.BotToken); err != nil || plain != f.token {
		t.Fatalf("Token lässt sich nicht entschlüsseln: %v", err)
	}
	// ohne neues Token bleibt das alte
	good["rules"] = []DiscordRule{}
	f.admin.expect(200, "PUT", path, good)
	if again, _ := f.p.store.DiscordBot(f.projectID); again.BotToken != stored.BotToken {
		t.Error("Token ging beim Speichern ohne Token verloren")
	}

	base := f.web
	viewer := member(t, f.admin, base, f.projectID, "betrachter", "viewer")
	viewer.expect(403, "GET", path, nil)
	viewer.expect(403, "PUT", path, good)
	f.admin.expect(204, "DELETE", path, nil)
	if _, err := f.p.store.DiscordBot(f.projectID); err != ErrNotFound {
		t.Errorf("nach dem Entfernen: %v", err)
	}
}

func TestDiscordRegisterCommands(t *testing.T) {
	f := newDiscordFixture(t)
	f.admin.expect(409, "POST", "/api/projects/"+f.projectID+"/discord/register", nil)
	f.configure(t)
	var got struct {
		method, path, auth string
		commands           []map[string]any
	}
	status := 200
	discord := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got.method, got.path, got.auth = r.Method, r.URL.Path, r.Header.Get("Authorization")
		data, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(data, &got.commands)
		w.WriteHeader(status)
		_, _ = w.Write([]byte("[]"))
	}))
	t.Cleanup(discord.Close)
	f.p.auto.discordBase = discord.URL

	var view discordDTO
	_ = json.Unmarshal(f.admin.expect(200, "POST", "/api/projects/"+f.projectID+"/discord/register", nil), &view)
	if got.method != "PUT" || got.path != "/applications/123456789012345678/commands" || got.auth != "Bot "+f.token {
		t.Fatalf("Anfrage an Discord: %s %s %q", got.method, got.path, got.auth)
	}
	names := []string{}
	for _, entry := range got.commands {
		names = append(names, entry["name"].(string))
	}
	if strings.Join(names, ",") != "status,spieler,start,stop,restart" {
		t.Errorf("Befehle: %v", names)
	}
	options := got.commands[0]["options"].([]any)[0].(map[string]any)
	if choices := options["choices"].([]any); len(choices) != 1 || choices[0].(map[string]any)["value"] != f.server.ID {
		t.Errorf("Auswahl: %+v", options)
	}
	if view.RegisteredAt == 0 {
		t.Error("RegisteredAt fehlt")
	}
	status = 401
	body := f.admin.expect(502, "POST", "/api/projects/"+f.projectID+"/discord/register", nil)
	if !strings.Contains(string(body), "Bot-Token") || strings.Contains(string(body), f.token) {
		t.Errorf("Fehlermeldung: %s", body)
	}
}

func TestDiscordInteractions(t *testing.T) {
	f := newDiscordFixture(t)
	now := time.Now()
	// ohne Einrichtung gibt es nichts
	if status, _ := f.interact(t, map[string]int{"type": 1}, true, now); status != 401 {
		t.Fatalf("ohne Einrichtung: %d", status)
	}
	f.configure(t)
	if status, _ := f.interact(t, map[string]int{"type": 1}, false, now); status != 401 {
		t.Fatalf("falsche Signatur: %d", status)
	}
	if status, _ := f.interact(t, map[string]int{"type": 1}, true, now.Add(-10*time.Minute)); status != 401 {
		t.Fatalf("alter Zeitstempel: %d", status)
	}
	status, out := f.interact(t, map[string]int{"type": 1}, true, now)
	if status != 200 || out["type"] != float64(1) {
		t.Fatalf("PING: %d %+v", status, out)
	}

	// Status für die Rolle „Alle“
	_, out = f.interact(t, command("status", "333333333333333333", []string{"111111111111111111"}, ""), true, now)
	if text, private := content(t, out); !strings.Contains(text, "**Main** – aus") || private {
		t.Fatalf("Status: %q %v", text, private)
	}
	// Start ohne Recht
	_, out = f.interact(t, command("start", "333333333333333333", []string{"111111111111111111"}, ""), true, now)
	if text, private := content(t, out); !strings.Contains(text, "keine Berechtigung") || !private {
		t.Fatalf("ohne Recht: %q", text)
	}
	if strings.Contains(strings.Join(f.fake.poweredList(), " "), "start:") {
		t.Fatal("trotzdem gestartet")
	}
	// Start durch Anna (Benutzer-Regel), Server über die Auswahl
	_, out = f.interact(t, command("start", "222222222222222222", nil, f.server.ID), true, now)
	if text, _ := content(t, out); !strings.Contains(text, "**Main** wird gestartet.") {
		t.Fatalf("Start: %q", text)
	}
	if !strings.Contains(strings.Join(f.fake.poweredList(), " "), "start:"+f.server.ID) {
		t.Fatal("nicht gestartet")
	}
	audit, _ := f.p.store.ListAudit(10, 0, "")
	found := false
	for _, entry := range audit {
		found = found || strings.HasPrefix(entry.Username, "discord:") && strings.Contains(entry.Target, "222222222222222222")
	}
	if !found {
		t.Errorf("kein Eintrag im Protokoll: %+v", audit)
	}
	// unbekannter Server
	_, out = f.interact(t, command("stop", "222222222222222222", nil, "gibtsnicht"), true, now)
	if text, private := content(t, out); !strings.Contains(text, "gibt es im Projekt nicht") || !private {
		t.Fatalf("unbekannter Server: %q", text)
	}
	// Spieler
	f.p.setLive(f.server.ID, liveInfo{At: now, Players: 2, MaxPlayers: 20, Names: []string{"Alex", "Luna"}, Running: true})
	_, out = f.interact(t, command("spieler", "333333333333333333", []string{"111111111111111111"}, "Main"), true, now)
	if text, _ := content(t, out); !strings.Contains(text, "2 Spieler online: Alex, Luna") {
		t.Fatalf("Spieler: %q", text)
	}
	// abgeschaltet: Discord bekommt 401
	f.admin.expect(200, "PUT", "/api/projects/"+f.projectID+"/discord", map[string]any{"applicationId": "123456789012345678", "publicKey": f.public, "enabled": false, "rules": []DiscordRule{}})
	if status, _ := f.interact(t, map[string]int{"type": 1}, true, now); status != 401 {
		t.Fatalf("abgeschaltet: %d", status)
	}
}

// Unsignierte Anfragen dürfen das Kontingent des Projekts nicht aufbrauchen, sonst legt jeder ohne Konto den Bot lahm.
func TestDiscordUnsignedFloodDoesNotBlockTheBot(t *testing.T) {
	f := newDiscordFixture(t)
	f.configure(t)
	now := time.Now()
	for range discordRequestLimit + 5 {
		if status, _ := f.interact(t, map[string]int{"type": 1}, false, now); status != 401 {
			t.Fatalf("unsignierte Anfrage: %d", status)
		}
	}
	if status, out := f.interact(t, map[string]int{"type": 1}, true, now); status != 200 || out["type"] != float64(1) {
		t.Fatalf("echte Anfrage von Discord nach der Flut: %d %+v", status, out)
	}
	// Unbekannte Projekt-IDs landen nicht im Limiter (sonst ließe er sich mit vielen IDs leeren)
	request, _ := http.NewRequest("POST", f.web+"/api/public/discord/ffffffffffffffff/interactions", strings.NewReader(`{"type":1}`))
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	f.p.auto.discordLimit.mu.Lock()
	_, unknown := f.p.auto.discordLimit.windows["ffffffffffffffff"]
	count := f.p.auto.discordLimit.windows[f.projectID].count
	f.p.auto.discordLimit.mu.Unlock()
	if response.StatusCode != 401 || unknown || count != 1 {
		t.Fatalf("Limiter: Status %d, unbekannte ID gezählt %v, Projekt %d", response.StatusCode, unknown, count)
	}
}

func TestDiscordAllowedAndPickServer(t *testing.T) {
	rules := []DiscordRule{{Kind: "role", ID: "1", Actions: []string{"status"}}, {Kind: "user", ID: "2", Actions: []string{"stop"}}}
	cases := []struct {
		user   string
		roles  []string
		action string
		want   bool
	}{
		{"9", []string{"1"}, "status", true},
		{"9", []string{"1"}, "stop", false},
		{"2", nil, "stop", true},
		{"", nil, "stop", false},
		{"1", nil, "status", false}, // eine Rollen-ID als Benutzer zählt nicht
	}
	for _, c := range cases {
		if got := discordAllowed(rules, c.user, c.roles, c.action); got != c.want {
			t.Errorf("%+v: %v", c, got)
		}
	}
	servers := []ServerRow{{ID: "a", Name: "Proxy", Role: "proxy"}, {ID: "b", Name: "Lobby", Slug: "lobby", Role: "backend"}}
	if row, _ := pickServer(servers, "", true); row.ID != "a" {
		t.Error("ohne Angabe sollte der Proxy gelten")
	}
	if _, problem := pickServer(servers, "", false); !strings.Contains(problem, "Proxy, Lobby") {
		t.Errorf("Hinweis: %q", problem)
	}
	if row, _ := pickServer(servers, "LOBBY", false); row.ID != "b" {
		t.Error("Name ohne Groß-/Kleinschreibung")
	}
}

// ---------------------------------------------------------------- Prometheus

func TestPrometheusMetrics(t *testing.T) {
	t.Setenv("LODESTONE_METRICS_TOKEN", "geheimes-messwert-token")
	p, _, fake, nodeID, server, _ := serverProject(t)
	fake.mu.Lock()
	view := fake.servers[server.ID]
	view.State = node.StateRunning
	view.Stats = &node.Stats{CPUPercent: 12.5, MemoryBytes: 1 << 30, MemoryLimit: 2 << 30}
	view.StartedAt = time.Now().Add(-time.Hour).UnixMilli()
	fake.servers[server.ID] = view
	fake.mu.Unlock()
	p.invalidate(nodeID)
	p.setLive(server.ID, liveInfo{At: time.Now(), Players: 3, MaxPlayers: 20, Running: true})

	web := httptest.NewServer(p.Handler())
	t.Cleanup(web.Close)
	get := func(token string) (int, string) {
		request, _ := http.NewRequest("GET", web.URL+"/metrics", nil)
		if token != "" {
			request.Header.Set("Authorization", "Bearer "+token)
		}
		response, err := http.DefaultClient.Do(request)
		if err != nil {
			t.Fatal(err)
		}
		defer response.Body.Close()
		data, _ := io.ReadAll(response.Body)
		return response.StatusCode, string(data)
	}
	if status, _ := get(""); status != 401 {
		t.Fatalf("ohne Token: %d", status)
	}
	if status, _ := get("falsch"); status != 401 {
		t.Fatalf("falsches Token: %d", status)
	}
	status, text := get("geheimes-messwert-token")
	if status != 200 {
		t.Fatalf("Status %d: %s", status, text)
	}
	labels := `server="Main",server_id="` + server.ID + `"`
	for _, want := range []string{
		"lodestone_up 1",
		`lodestone_node_up{node="test",node_id="` + nodeID + `"} 1`,
		`lodestone_node_servers{node="test",node_id="` + nodeID + `"} 1`,
		"lodestone_server_up{" + labels,
		`state="running"} 1`,
		"lodestone_server_players{" + labels,
		"lodestone_server_cpu_percent{" + labels,
		"} 12.5",
		"} 1073741824",
		"# TYPE lodestone_server_players gauge",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("fehlt: %s\n%s", want, text)
		}
	}
	if !strings.Contains(text, `project="Mein Server"`) || !strings.Contains(text, "lodestone_server_players{"+labels+`,project="Mein Server",project_id=`) {
		t.Errorf("Projekt-Label fehlt:\n%s", text)
	}
}

func TestPrometheusOffWithoutToken(t *testing.T) {
	t.Setenv("LODESTONE_METRICS_TOKEN", "")
	_, web, _, _ := setupPanel(t)
	response, err := http.Get(web.URL + "/metrics")
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != 404 {
		t.Fatalf("ohne Token in der Umgebung: %d", response.StatusCode)
	}
}

func TestPromLabelsEscape(t *testing.T) {
	if got := promLabels("server", "A \"B\"\\C\nD"); got != `server="A \"B\"\\C\nD"` {
		t.Errorf("maskiert: %s", got)
	}
}
