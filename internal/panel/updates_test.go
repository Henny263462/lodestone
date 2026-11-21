package panel

import (
	"bufio"
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/lodestone-mc/lodestone/internal/node"
)

// liveStream liest GET /api/live eines Clients und reicht die Meldungen weiter.
type liveStream struct {
	updates chan Update
	closed  chan struct{}
}

func openLive(t *testing.T, c *client) *liveStream {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	request, _ := http.NewRequestWithContext(ctx, http.MethodGet, c.base+"/api/live", nil)
	response, err := c.http.Do(request)
	if err != nil {
		cancel()
		t.Fatal(err)
	}
	if response.StatusCode != http.StatusOK {
		cancel()
		t.Fatalf("GET /api/live: Status %d", response.StatusCode)
	}
	if kind := response.Header.Get("Content-Type"); kind != "text/event-stream" {
		t.Errorf("Content-Type %q", kind)
	}
	stream := &liveStream{updates: make(chan Update, 32), closed: make(chan struct{})}
	go func() {
		defer close(stream.closed)
		scanner := bufio.NewScanner(response.Body)
		for scanner.Scan() {
			line, ok := strings.CutPrefix(scanner.Text(), "data: ")
			if !ok {
				continue
			}
			var update Update
			if json.Unmarshal([]byte(line), &update) == nil && update.Topic != "" {
				stream.updates <- update
			}
		}
	}()
	t.Cleanup(func() {
		cancel()
		response.Body.Close()
	})
	return stream
}

func (s *liveStream) next(t *testing.T) Update {
	t.Helper()
	select {
	case update := <-s.updates:
		return update
	case <-time.After(5 * time.Second):
		t.Fatal("keine Meldung auf /api/live")
		return Update{}
	}
}

// expectSentinel prüft, dass vor der Meldung „node“ (die jeder sieht) nichts anderes ankam.
func (s *liveStream) expectSentinel(t *testing.T, who string) {
	t.Helper()
	if got := s.next(t); got.Topic != "node" || got.NodeID != "wache" {
		t.Fatalf("%s hat eine Meldung bekommen, die nicht für ihn ist: %+v", who, got)
	}
}

func TestLiveUpdatesRespectAccess(t *testing.T) {
	p, web, _, nodeID := setupPanel(t)
	admin := newClient(t, web.URL)
	admin.expect(201, "POST", "/api/auth/setup", map[string]string{"username": "admin", "password": "ein-gutes-passwort"})
	for _, name := range []string{"mitglied", "fremd"} {
		admin.expect(201, "POST", "/api/users", map[string]any{"username": name, "password": "ein-gutes-passwort"})
	}
	ids := userIDs(t, p)
	var project ProjectDTO
	_ = json.Unmarshal(admin.expect(201, "POST", "/api/projects", map[string]string{"name": "Live", "kind": "server"}), &project)
	members := "/api/projects/" + project.ID + "/members/"
	admin.expect(200, "PUT", members+ids["mitglied"], map[string]string{"role": "viewer"})
	var server ServerDTO
	_ = json.Unmarshal(admin.expect(201, "POST", "/api/projects/"+project.ID+"/servers", serverRequest{Name: "Main", NodeID: nodeID, Software: "paper", Version: "1.21.4", EULA: true}), &server)

	login := func(name string) *client {
		c := newClient(t, web.URL)
		c.expect(200, "POST", "/api/auth/login", map[string]string{"username": name, "password": "ein-gutes-passwort"})
		return c
	}
	member, stranger := login("mitglied"), login("fremd")
	memberLive, strangerLive, adminLive := openLive(t, member), openLive(t, stranger), openLive(t, admin)
	sentinel := func() { p.publish(Update{Topic: "node", NodeID: "wache"}) }

	// Eine Änderung über die Schnittstelle geht an Mitglieder des Projekts, nicht an Fremde
	admin.expect(200, "PUT", "/api/servers/"+server.ID+"/auto-restart", map[string]bool{"enabled": true})
	sentinel()
	for name, stream := range map[string]*liveStream{"mitglied": memberLive, "admin": adminLive} {
		got := stream.next(t)
		if got.Topic != "server" || got.ServerID != server.ID || got.ProjectID != project.ID || got.Area != "auto-restart" {
			t.Fatalf("%s: unerwartete Meldung %+v", name, got)
		}
		stream.expectSentinel(t, name)
	}
	strangerLive.expectSentinel(t, "fremd")

	// Fehlgeschlagene Anfragen melden nichts (der Betrachter darf das nicht)
	member.expect(403, "PUT", "/api/servers/"+server.ID+"/auto-restart", map[string]bool{"enabled": false})
	sentinel()
	adminLive.expectSentinel(t, "admin nach 403")
	memberLive.expectSentinel(t, "mitglied nach 403")
	strangerLive.expectSentinel(t, "fremd nach 403")

	// Ereignisse aus dem Hintergrund (emit) kommen ebenso nur bei Mitgliedern an
	p.emit(project.ID, server.ID, "crash", "error", "Main ist abgestürzt.")
	sentinel()
	if got := memberLive.next(t); got.Topic != "events" || got.ProjectID != project.ID {
		t.Fatalf("mitglied: unerwartete Meldung %+v", got)
	}
	memberLive.expectSentinel(t, "mitglied nach emit")
	strangerLive.expectSentinel(t, "fremd nach emit")
	if got := adminLive.next(t); got.Topic != "events" {
		t.Fatalf("admin: unerwartete Meldung %+v", got)
	}
	adminLive.expectSentinel(t, "admin nach emit")

	// Backups brauchen „Backups ansehen“: ohne dieses Recht kommt die Meldung nicht an
	admin.expect(200, "PUT", members+ids["mitglied"], map[string]any{"role": "viewer", "permissions": []Permission{PermConsoleRead}})
	if got := adminLive.next(t); got.Topic != "project" || got.Area != "members" {
		t.Fatalf("admin: unerwartete Meldung %+v", got)
	}
	if got := memberLive.next(t); got.Topic != "project" {
		t.Fatalf("mitglied: unerwartete Meldung %+v", got)
	}
	p.publish(Update{Topic: "backups", ServerID: server.ID})
	sentinel()
	if got := adminLive.next(t); got.Topic != "backups" || got.ProjectID != project.ID {
		t.Fatalf("admin: unerwartete Meldung %+v", got)
	}
	adminLive.expectSentinel(t, "admin nach backups")
	memberLive.expectSentinel(t, "mitglied ohne backups.read")

	// Wer aus dem Projekt fliegt, hört sofort nichts mehr davon
	admin.expect(204, "DELETE", members+ids["mitglied"], nil)
	p.emit(project.ID, server.ID, "crash", "error", "Main ist wieder abgestürzt.")
	sentinel()
	memberLive.expectSentinel(t, "ehemaliges Mitglied")
}

func TestLiveUpdatesEndWithSession(t *testing.T) {
	updateRecheck, updateBeat = 30*time.Millisecond, 20*time.Millisecond
	t.Cleanup(func() { updateRecheck, updateBeat = 30*time.Second, 25*time.Second })
	_, web, _, _ := setupPanel(t)
	admin := newClient(t, web.URL)
	admin.expect(201, "POST", "/api/auth/setup", map[string]string{"username": "admin", "password": "ein-gutes-passwort"})

	// ohne Anmeldung gibt es keinen Kanal
	if status, _ := newClient(t, web.URL).do("GET", "/api/live", nil); status != http.StatusUnauthorized {
		t.Fatalf("ohne Anmeldung: Status %d", status)
	}

	stream := openLive(t, admin)
	admin.expect(204, "POST", "/api/auth/logout", nil)
	select {
	case <-stream.closed:
	case <-time.After(5 * time.Second):
		t.Fatal("der Kanal bleibt nach dem Abmelden offen")
	}
}

func TestLiveUpdatesLimitPerUser(t *testing.T) {
	_, web, _, _ := setupPanel(t)
	admin := newClient(t, web.URL)
	admin.expect(201, "POST", "/api/auth/setup", map[string]string{"username": "admin", "password": "ein-gutes-passwort"})
	for range updatePerUser {
		openLive(t, admin)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	request, _ := http.NewRequestWithContext(ctx, http.MethodGet, admin.base+"/api/live", nil)
	response, err := admin.http.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusTooManyRequests {
		t.Fatalf("Verbindung über dem Limit: Status %d", response.StatusCode)
	}
}

func TestObserveViewsReportsChanges(t *testing.T) {
	p, web, _, nodeID := setupPanel(t)
	admin := newClient(t, web.URL)
	admin.expect(201, "POST", "/api/auth/setup", map[string]string{"username": "admin", "password": "ein-gutes-passwort"})
	var project ProjectDTO
	_ = json.Unmarshal(admin.expect(201, "POST", "/api/projects", map[string]string{"name": "Zustand", "kind": "server"}), &project)
	var server ServerDTO
	_ = json.Unmarshal(admin.expect(201, "POST", "/api/projects/"+project.ID+"/servers", serverRequest{Name: "Main", NodeID: nodeID, Software: "paper", Version: "1.21.4", EULA: true}), &server)
	stream := openLive(t, admin)

	view := func(state node.State) map[string]node.View {
		return map[string]node.View{server.ID: {State: state}}
	}
	p.observeViews("knoten", view(node.StateOffline), nil)
	p.observeViews("knoten", view(node.StateOffline), nil) // gleich geblieben: keine Meldung
	p.observeViews("knoten", view(node.StateStarting), nil)
	if got := stream.next(t); got.Topic != "server" || got.ServerID != server.ID || got.ProjectID != project.ID {
		t.Fatalf("Zustandswechsel: %+v", got)
	}
	p.observeViews("knoten", nil, ErrNodeOffline)
	if got := stream.next(t); got.Topic != "node" || got.NodeID != "knoten" {
		t.Fatalf("Node nicht erreichbar: %+v", got)
	}
}

func TestRecentEventsOnlyOwnProjects(t *testing.T) {
	p, web, _, _ := setupPanel(t)
	admin := newClient(t, web.URL)
	admin.expect(201, "POST", "/api/auth/setup", map[string]string{"username": "admin", "password": "ein-gutes-passwort"})
	admin.expect(201, "POST", "/api/users", map[string]any{"username": "mitglied", "password": "ein-gutes-passwort"})
	ids := userIDs(t, p)
	var mine, other ProjectDTO
	_ = json.Unmarshal(admin.expect(201, "POST", "/api/projects", map[string]string{"name": "Meins", "kind": "server"}), &mine)
	_ = json.Unmarshal(admin.expect(201, "POST", "/api/projects", map[string]string{"name": "Fremd", "kind": "server"}), &other)
	admin.expect(200, "PUT", "/api/projects/"+mine.ID+"/members/"+ids["mitglied"], map[string]string{"role": "viewer"})
	p.emit(mine.ID, "", "backup", "info", "Backup von Meins erstellt.")
	p.emit(other.ID, "", "crash", "error", "Fremd ist abgestürzt.")

	member := newClient(t, web.URL)
	member.expect(200, "POST", "/api/auth/login", map[string]string{"username": "mitglied", "password": "ein-gutes-passwort"})
	var events []Event
	_ = json.Unmarshal(member.expect(200, "GET", "/api/events/recent", nil), &events)
	if len(events) != 1 || events[0].ProjectID != mine.ID {
		t.Fatalf("Mitglied sieht %+v", events)
	}
	_ = json.Unmarshal(admin.expect(200, "GET", "/api/events/recent?limit=5", nil), &events)
	if len(events) != 2 || events[0].ProjectID != other.ID {
		t.Fatalf("Administrator sieht %+v", events)
	}
	member.expect(400, "GET", "/api/events/recent?limit=0", nil)
	member.expect(400, "GET", "/api/events/recent?limit=viele", nil)
	newClient(t, web.URL).expect(401, "GET", "/api/events/recent", nil)
}
