package panel

import (
	"bytes"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

// worldNode ist eine Attrappe der Welt-Routen des Nodes. Sie merkt sich, was ankommt.
type worldNode struct {
	mu       sync.Mutex
	requests []string
	bodies   map[string]string
}

func newWorldNode(t *testing.T) (*worldNode, *httptest.Server) {
	fake := &worldNode{bodies: map[string]string{}}
	record := func(r *http.Request) string {
		data, _ := io.ReadAll(r.Body)
		key := r.Method + " " + r.URL.Path
		if r.URL.RawQuery != "" {
			key += "?" + r.URL.RawQuery
		}
		fake.mu.Lock()
		fake.requests = append(fake.requests, key)
		fake.bodies[key] = string(data)
		fake.mu.Unlock()
		return string(data)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/servers", func(w http.ResponseWriter, _ *http.Request) { writeJSON(w, 200, []any{}) })
	mux.HandleFunc("GET /api/servers/{id}/properties", func(w http.ResponseWriter, r *http.Request) {
		record(r)
		writeJSON(w, 200, map[string]any{"exists": true, "entries": []map[string]string{{"key": "motd", "value": "Hallo"}, {"key": "server-port", "value": "25565"}}})
	})
	mux.HandleFunc("PUT /api/servers/{id}/properties", func(w http.ResponseWriter, r *http.Request) {
		record(r)
		writeJSON(w, 200, map[string]any{"exists": true, "entries": []map[string]string{{"key": "motd", "value": "Neu"}}})
	})
	mux.HandleFunc("GET /api/servers/{id}/worlds", func(w http.ResponseWriter, r *http.Request) {
		record(r)
		writeJSON(w, 200, map[string]any{"levelName": "world", "seed": "", "worlds": []map[string]any{{"name": "world", "kind": "overworld", "main": true}}})
	})
	mux.HandleFunc("POST /api/servers/{id}/worlds/{world}/reset", func(w http.ResponseWriter, r *http.Request) {
		record(r)
		writeJSON(w, 202, map[string]any{"state": "restoring"})
	})
	mux.HandleFunc("GET /api/servers/{id}/worlds/{world}/download", func(w http.ResponseWriter, r *http.Request) {
		record(r)
		w.Header().Set("Content-Type", "application/zip")
		_, _ = w.Write([]byte("PK-zip-" + r.PathValue("world")))
	})
	mux.HandleFunc("POST /api/servers/{id}/worlds/{world}/upload", func(w http.ResponseWriter, r *http.Request) {
		record(r)
		writeJSON(w, 201, map[string]string{"world": r.PathValue("world")})
	})
	mux.HandleFunc("GET /api/servers/{id}/worlds/{world}/datapacks", func(w http.ResponseWriter, r *http.Request) {
		record(r)
		writeJSON(w, 200, []any{})
	})
	mux.HandleFunc("POST /api/servers/{id}/worlds/{world}/datapacks", func(w http.ResponseWriter, r *http.Request) {
		record(r)
		writeJSON(w, 201, map[string]string{"name": "pack.zip"})
	})
	mux.HandleFunc("DELETE /api/servers/{id}/worlds/{world}/datapacks/{name}", func(w http.ResponseWriter, r *http.Request) {
		record(r)
		w.WriteHeader(204)
	})
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)
	return fake, server
}

func (f *worldNode) saw(request string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, seen := range f.requests {
		if seen == request {
			return true
		}
	}
	return false
}

func (f *worldNode) body(request string) string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.bodies[request]
}

type worldSetup struct {
	admin, operator, viewer, stranger *client
	project, network                  ProjectDTO
	server, backend, proxy            ServerRow
	node                              *worldNode
	userIDs                           map[string]string
	panel                             *Panel
}

func setupWorlds(t *testing.T) worldSetup {
	p, web, _, _ := setupPanel(t)
	fake, fakeServer := newWorldNode(t)
	worldNodeRow, err := p.store.CreateNode(Node{Name: "welten", URL: fakeServer.URL, Token: "token", PublicHost: "mc.example.org"})
	if err != nil {
		t.Fatal(err)
	}
	s := worldSetup{node: fake, panel: p, userIDs: map[string]string{}}
	s.admin = newClient(t, web.URL)
	s.admin.expect(201, "POST", "/api/auth/setup", map[string]string{"username": "admin", "password": "ein-gutes-passwort"})
	for _, name := range []string{"viewer", "operator", "fremd"} {
		s.admin.expect(201, "POST", "/api/users", map[string]any{"username": name, "password": "ein-gutes-passwort"})
	}
	var users []User
	_ = json.Unmarshal(s.admin.expect(200, "GET", "/api/users", nil), &users)
	for _, user := range users {
		s.userIDs[user.Username] = user.ID
	}
	_ = json.Unmarshal(s.admin.expect(201, "POST", "/api/projects", map[string]string{"name": "Survival", "kind": "server"}), &s.project)
	_ = json.Unmarshal(s.admin.expect(201, "POST", "/api/projects", map[string]string{"name": "Netz", "kind": "network"}), &s.network)
	for _, project := range []string{s.project.ID, s.network.ID} {
		s.admin.expect(200, "PUT", "/api/projects/"+project+"/members/"+s.userIDs["viewer"], map[string]string{"role": "viewer"})
		s.admin.expect(200, "PUT", "/api/projects/"+project+"/members/"+s.userIDs["operator"], map[string]string{"role": "operator"})
	}
	create := func(row ServerRow) ServerRow {
		row.NodeID = worldNodeRow.ID
		created, err := p.store.CreateServer(row)
		if err != nil {
			t.Fatal(err)
		}
		return created
	}
	s.server = create(ServerRow{ProjectID: s.project.ID, Name: "Main", Slug: "main", Role: RoleStandalone, Software: "paper"})
	s.backend = create(ServerRow{ProjectID: s.network.ID, Name: "Lobby", Slug: "lobby", Role: RoleBackend, Software: "paper"})
	s.proxy = create(ServerRow{ProjectID: s.network.ID, Name: "Proxy", Slug: "proxy", Role: RoleProxy, Software: "velocity"})
	login := func(name string) *client {
		c := newClient(t, web.URL)
		c.expect(200, "POST", "/api/auth/login", map[string]string{"username": name, "password": "ein-gutes-passwort"})
		return c
	}
	s.viewer, s.operator, s.stranger = login("viewer"), login("operator"), login("fremd")
	return s
}

func TestPropertiesFormPermissionsAndValidation(t *testing.T) {
	s := setupWorlds(t)
	base := "/api/servers/" + s.server.ID + "/properties"

	s.stranger.expect(404, "GET", base, nil)
	s.viewer.expect(403, "GET", base, nil)
	var view struct {
		Exists  bool              `json:"exists"`
		Entries []map[string]any  `json:"entries"`
		Locked  map[string]string `json:"locked"`
	}
	_ = json.Unmarshal(s.operator.expect(200, "GET", base, nil), &view)
	if !view.Exists || len(view.Entries) != 2 || view.Locked["server-port"] == "" || view.Locked["online-mode"] != "" {
		t.Fatalf("Antwort: %+v", view)
	}

	s.operator.expect(403, "PUT", base, map[string]any{"values": map[string]string{"motd": "x"}})
	for _, bad := range []map[string]string{
		{"difficulty": "brutal"},
		{"max-players": "0"},
		{"view-distance": "64"},
		{"pvp": "ja"},
		{"server-port": "25566"},
		{"level-name": "../plugins"},
		{"level-seed": "a\nb"},
		{"motd": strings.Repeat("x", 513)},
		{},
	} {
		s.admin.expect(400, "PUT", base, map[string]any{"values": bad})
	}
	s.admin.expect(400, "PUT", base, map[string]any{"values": map[string]string{"motd": "x"}, "extra": 1})
	if len(s.node.requests) != 1 {
		t.Fatalf("abgelehnte Werte dürfen den Node nie erreichen: %v", s.node.requests)
	}

	values := map[string]string{"motd": "Zeile 1\nZeile 2", "difficulty": "hard", "mein-plugin.option": "frei"}
	var saved struct {
		Locked map[string]string `json:"locked"`
	}
	_ = json.Unmarshal(s.admin.expect(200, "PUT", base, map[string]any{"values": values}), &saved)
	if saved.Locked["server-port"] == "" {
		t.Error("auch die Antwort aufs Speichern nennt die gesperrten Schlüssel")
	}
	var sent struct {
		Set map[string]string `json:"set"`
	}
	_ = json.Unmarshal([]byte(s.node.body("PUT /api/servers/"+s.server.ID+"/properties")), &sent)
	if sent.Set["difficulty"] != "hard" || sent.Set["mein-plugin.option"] != "frei" || sent.Set["motd"] != "Zeile 1\nZeile 2" {
		t.Fatalf("an den Node: %+v", sent)
	}

	// Im Netzwerk setzt die Autoconfig online-mode, das Formular darf es nicht ändern
	backend := "/api/servers/" + s.backend.ID + "/properties"
	_ = json.Unmarshal(s.operator.expect(200, "GET", backend, nil), &view)
	if view.Locked["online-mode"] == "" {
		t.Fatal("online-mode muss im Netzwerk gesperrt sein")
	}
	s.admin.expect(400, "PUT", backend, map[string]any{"values": map[string]string{"online-mode": "true"}})
	s.admin.expect(200, "PUT", backend, map[string]any{"values": map[string]string{"pvp": "false"}})
	// Velocity hat keine server.properties
	s.admin.expect(400, "GET", "/api/servers/"+s.proxy.ID+"/properties", nil)
}

func TestWorldRoutesPermissions(t *testing.T) {
	s := setupWorlds(t)
	id := s.server.ID
	worlds := "/api/servers/" + id + "/worlds"

	s.stranger.expect(404, "GET", worlds, nil)
	s.viewer.expect(403, "GET", worlds, nil)
	s.operator.expect(200, "GET", worlds, nil)
	s.admin.expect(400, "GET", "/api/servers/"+s.proxy.ID+"/worlds", nil)

	// Zurücksetzen nur als Admin, mit geprüftem Namen und Seed
	s.operator.expect(403, "POST", worlds+"/world/reset", map[string]any{"backup": true})
	s.admin.expect(400, "POST", worlds+"/.versteckt/reset", map[string]any{})
	s.admin.expect(400, "POST", worlds+"/world/reset", map[string]any{"seed": "a\nb"})
	s.admin.expect(400, "POST", worlds+"/world/reset", map[string]any{"unbekannt": true})
	s.admin.expect(202, "POST", worlds+"/Meine%20Welt/reset", map[string]any{"backup": true, "seed": "42", "dimensions": true})
	var reset struct {
		Backup     bool   `json:"backup"`
		Seed       string `json:"seed"`
		Dimensions bool   `json:"dimensions"`
	}
	_ = json.Unmarshal([]byte(s.node.body("POST /api/servers/"+id+"/worlds/Meine Welt/reset")), &reset)
	if !reset.Backup || reset.Seed != "42" || !reset.Dimensions {
		t.Fatalf("an den Node: %+v (%v)", reset, s.node.requests)
	}

	// Herunterladen als Operator, als Strom durchgereicht
	s.viewer.expect(403, "GET", worlds+"/world/download", nil)
	if body := s.operator.expect(200, "GET", worlds+"/world/download", nil); string(body) != "PK-zip-world" {
		t.Fatalf("Download: %q", body)
	}

	// Hochladen nur als Admin; die Abfrage (backup=1) reist mit
	var buffer bytes.Buffer
	writer := multipart.NewWriter(&buffer)
	part, _ := writer.CreateFormFile("file", "welt.zip")
	_, _ = part.Write([]byte("PK"))
	writer.Close()
	send := func(c *client, path string) int {
		request, _ := http.NewRequest("POST", c.base+path, bytes.NewReader(buffer.Bytes()))
		request.Header.Set("Content-Type", writer.FormDataContentType())
		request.Header.Set(csrfHeader, "1")
		response, err := c.http.Do(request)
		if err != nil {
			t.Fatal(err)
		}
		response.Body.Close()
		return response.StatusCode
	}
	if status := send(s.operator, worlds+"/world/upload?backup=1"); status != 403 {
		t.Fatalf("Upload als Operator: %d", status)
	}
	if status := send(s.admin, worlds+"/world/upload?backup=1"); status != 201 {
		t.Fatalf("Upload als Admin: %d", status)
	}
	if !s.node.saw("POST /api/servers/" + id + "/worlds/world/upload?backup=1") {
		t.Fatalf("Upload kam nicht beim Node an: %v", s.node.requests)
	}
	if !strings.Contains(s.node.body("POST /api/servers/"+id+"/worlds/world/upload?backup=1"), "welt.zip") {
		t.Fatal("der Upload muss unverändert durchgereicht werden")
	}

	// Datapacks als Operator
	s.viewer.expect(403, "GET", worlds+"/world/datapacks", nil)
	s.operator.expect(200, "GET", worlds+"/world/datapacks", nil)
	if status := send(s.operator, worlds+"/world/datapacks"); status != 201 {
		t.Fatalf("Datapack hochladen: %d", status)
	}
	s.viewer.expect(403, "DELETE", worlds+"/world/datapacks/pack.zip", nil)
	s.operator.expect(204, "DELETE", worlds+"/world/datapacks/pack.zip", nil)
	if !s.node.saw("DELETE /api/servers/" + id + "/worlds/world/datapacks/pack.zip") {
		t.Fatalf("Löschen kam nicht an: %v", s.node.requests)
	}
}

func TestProjectPrefsArePerUserAndChecked(t *testing.T) {
	s := setupWorlds(t)
	var prefs []ProjectPref
	_ = json.Unmarshal(s.operator.expect(200, "GET", "/api/me/project-prefs", nil), &prefs)
	if len(prefs) != 0 {
		t.Fatalf("am Anfang: %+v", prefs)
	}

	var pref ProjectPref
	_ = json.Unmarshal(s.operator.expect(200, "PUT", "/api/me/project-prefs/"+s.network.ID, map[string]any{"favorite": true, "tags": []string{" Minigames ", "minigames", "Öffentlich", ""}}), &pref)
	if !pref.Favorite || strings.Join(pref.Tags, ",") != "Minigames,Öffentlich" {
		t.Fatalf("gespeichert: %+v", pref)
	}
	// Nur die Schlagwörter ändern lässt den Favoriten stehen
	_ = json.Unmarshal(s.operator.expect(200, "PUT", "/api/me/project-prefs/"+s.network.ID, map[string]any{"tags": []string{"Netz"}}), &pref)
	if !pref.Favorite || strings.Join(pref.Tags, ",") != "Netz" {
		t.Fatalf("Teiländerung: %+v", pref)
	}
	s.operator.expect(400, "PUT", "/api/me/project-prefs/"+s.network.ID, map[string]any{"tags": []string{"<script>"}})
	s.operator.expect(400, "PUT", "/api/me/project-prefs/"+s.network.ID, map[string]any{"tags": []string{"a", "b", "c", "d", "e", "f", "g", "h", "i"}})
	s.operator.expect(400, "PUT", "/api/me/project-prefs/"+s.network.ID, map[string]any{"favorit": true})
	s.stranger.expect(404, "PUT", "/api/me/project-prefs/"+s.network.ID, map[string]any{"favorite": true})

	// Reihenfolge
	s.operator.expect(400, "PUT", "/api/me/project-order", map[string]any{"projectIds": []string{s.network.ID, s.network.ID}})
	s.operator.expect(400, "PUT", "/api/me/project-order", map[string]any{"projectIds": []string{"gibt-es-nicht"}})
	s.stranger.expect(400, "PUT", "/api/me/project-order", map[string]any{"projectIds": []string{s.project.ID}})
	_ = json.Unmarshal(s.operator.expect(200, "PUT", "/api/me/project-order", map[string]any{"projectIds": []string{s.network.ID, s.project.ID}}), &prefs)
	positions := map[string]int{}
	for _, entry := range prefs {
		positions[entry.ProjectID] = entry.Position
	}
	if positions[s.network.ID] != 1 || positions[s.project.ID] != 2 {
		t.Fatalf("Reihenfolge: %+v", prefs)
	}
	// Die Reihenfolge lässt Favorit und Schlagwörter stehen
	for _, entry := range prefs {
		if entry.ProjectID == s.network.ID && (!entry.Favorite || len(entry.Tags) != 1) {
			t.Fatalf("Reihenfolge hat Favorit/Schlagwörter verloren: %+v", entry)
		}
	}

	// Jeder hat seine eigene Ordnung
	_ = json.Unmarshal(s.admin.expect(200, "GET", "/api/me/project-prefs", nil), &prefs)
	if len(prefs) != 0 {
		t.Fatalf("der Admin sieht die Ordnung des Operators: %+v", prefs)
	}
	// Verliert jemand den Zugang, verschwindet das Projekt aus seiner Ordnung
	s.admin.expect(204, "DELETE", "/api/projects/"+s.network.ID+"/members/"+s.userIDs["operator"], nil)
	_ = json.Unmarshal(s.operator.expect(200, "GET", "/api/me/project-prefs", nil), &prefs)
	if len(prefs) != 1 || prefs[0].ProjectID != s.project.ID {
		t.Fatalf("nach dem Entfernen: %+v", prefs)
	}
}

func TestProjectLivePlayers(t *testing.T) {
	s := setupWorlds(t)
	s.panel.setLive(s.backend.ID, liveInfo{At: time.Now(), Players: 7, Running: true})
	var players map[string]int
	_ = json.Unmarshal(s.viewer.expect(200, "GET", "/api/projects/"+s.network.ID+"/live-players", nil), &players)
	if players[s.backend.ID] != 7 || players[s.proxy.ID] != 0 || len(players) != 2 {
		t.Fatalf("Spieler: %v", players)
	}
	s.stranger.expect(404, "GET", "/api/projects/"+s.network.ID+"/live-players", nil)
}
