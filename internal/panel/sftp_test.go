package panel

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/rsa"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/lodestone-mc/lodestone/internal/node"
	"golang.org/x/crypto/ssh"
)

// fakeSFTPNode spielt die SFTP-Seite eines Nodes: Status mit Ereignissen und die Zugänge je Server.
type fakeSFTPNode struct {
	mu     sync.Mutex
	down   bool
	bootID string
	events []node.SFTPEvent
	access map[string]node.SFTPAccess
	puts   int
	// orphans sind Server, die nur der Node noch kennt.
	orphans []string
}

func newFakeSFTPNode(t *testing.T) (*fakeSFTPNode, *httptest.Server) {
	fake := &fakeSFTPNode{bootID: "boot-1", access: map[string]node.SFTPAccess{}}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/sftp", func(w http.ResponseWriter, r *http.Request) {
		fake.mu.Lock()
		defer fake.mu.Unlock()
		if fake.down {
			writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "aus"})
			return
		}
		after, _ := strconv.ParseInt(r.URL.Query().Get("after"), 10, 64)
		status := node.SFTPStatus{Enabled: true, Port: 2022, Fingerprint: "SHA256:testfingerabdruck", BootID: fake.bootID, Events: []node.SFTPEvent{}}
		for _, event := range fake.events {
			if event.Seq > after {
				status.Events = append(status.Events, event)
			}
		}
		writeJSON(w, http.StatusOK, status)
	})
	mux.HandleFunc("GET /api/servers", func(w http.ResponseWriter, r *http.Request) {
		fake.mu.Lock()
		defer fake.mu.Unlock()
		views := []node.View{}
		for _, id := range fake.orphans {
			views = append(views, node.View{Spec: node.Spec{ID: id}})
		}
		writeJSON(w, http.StatusOK, views)
	})
	mux.HandleFunc("PUT /api/servers/{id}/sftp-access", func(w http.ResponseWriter, r *http.Request) {
		fake.mu.Lock()
		defer fake.mu.Unlock()
		if fake.down {
			writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "aus"})
			return
		}
		var access node.SFTPAccess
		decoder := json.NewDecoder(r.Body)
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&access); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		fake.access[r.PathValue("id")] = access
		fake.puts++
		writeJSON(w, http.StatusOK, map[string]int{"grants": len(access.Grants)})
	})
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)
	return fake, server
}

func (f *fakeSFTPNode) grant(serverID, username string) (node.SFTPGrant, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, grant := range f.access[serverID].Grants {
		if grant.Username == username {
			return grant, true
		}
	}
	return node.SFTPGrant{}, false
}

func (f *fakeSFTPNode) putCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.puts
}

func testAuthorizedKey(t *testing.T) string {
	t.Helper()
	public, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	key, err := ssh.NewPublicKey(public)
	if err != nil {
		t.Fatal(err)
	}
	return strings.TrimSpace(string(ssh.MarshalAuthorizedKey(key))) + " anna@laptop"
}

func TestSFTPAccessFollowsThePanel(t *testing.T) {
	p, web, _, _ := setupPanel(t)
	fake, fakeServer := newFakeSFTPNode(t)
	sftpNode, err := p.store.CreateNode(Node{Name: "sftp", URL: fakeServer.URL, Token: "token", PublicHost: "mc.example.org"})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()

	admin := newClient(t, web.URL)
	admin.expect(201, "POST", "/api/auth/setup", map[string]string{"username": "admin", "password": "ein-gutes-passwort"})
	ids := map[string]string{}
	for _, name := range []string{"Anna", "viewer", "fremd"} {
		var user User
		_ = json.Unmarshal(admin.expect(201, "POST", "/api/users", map[string]any{"username": name, "password": "ein-gutes-passwort"}), &user)
		ids[name] = user.ID
	}
	var project ProjectDTO
	_ = json.Unmarshal(admin.expect(201, "POST", "/api/projects", map[string]string{"name": "Survival", "kind": "server"}), &project)
	admin.expect(200, "PUT", "/api/projects/"+project.ID+"/members/"+ids["Anna"], map[string]string{"role": "operator"})
	admin.expect(200, "PUT", "/api/projects/"+project.ID+"/members/"+ids["viewer"], map[string]string{"role": "viewer"})
	row, err := p.store.CreateServer(ServerRow{ProjectID: project.ID, NodeID: sftpNode.ID, Name: "Main", Slug: "main", Role: "standalone", Software: "paper"})
	if err != nil {
		t.Fatal(err)
	}
	login := func(name string) *client {
		c := newClient(t, web.URL)
		c.expect(200, "POST", "/api/auth/login", map[string]string{"username": name, "password": "ein-gutes-passwort"})
		return c
	}
	anna, viewer, stranger := login("Anna"), login("viewer"), login("fremd")
	annaLogin := "anna." + row.ID[:8]

	// Konto: Passwort und Schlüssel
	anna.expect(400, "PUT", "/api/me/sftp/password", map[string]string{"password": "kurz"})
	var account SFTPAccountDTO
	_ = json.Unmarshal(anna.expect(200, "PUT", "/api/me/sftp/password", map[string]string{"password": "sftp-passwort-123"}), &account)
	if !account.PasswordSet || !account.PasswordLogin || len(account.Keys) != 0 {
		t.Fatalf("Konto: %+v", account)
	}
	if strings.Contains(string(anna.expect(200, "GET", "/api/me/sftp", nil)), "argon2") {
		t.Fatal("der Hash geht an den Browser")
	}
	anna.expect(400, "POST", "/api/me/sftp/keys", map[string]string{"publicKey": "kein schlüssel"})
	anna.expect(400, "POST", "/api/me/sftp/keys", map[string]string{"publicKey": `command="rm -rf /" ` + testAuthorizedKey(t)})
	weak, _ := rsa.GenerateKey(rand.Reader, 1024)
	weakKey, _ := ssh.NewPublicKey(&weak.PublicKey)
	anna.expect(400, "POST", "/api/me/sftp/keys", map[string]string{"publicKey": string(ssh.MarshalAuthorizedKey(weakKey))})
	authorized := testAuthorizedKey(t)
	var key SFTPKey
	_ = json.Unmarshal(anna.expect(201, "POST", "/api/me/sftp/keys", map[string]string{"publicKey": authorized}), &key)
	if key.Name != "anna@laptop" || !strings.HasPrefix(key.Fingerprint, "SHA256:") || strings.Contains(key.PublicKey, "anna@laptop") {
		t.Fatalf("Schlüssel: %+v", key)
	}
	anna.expect(409, "POST", "/api/me/sftp/keys", map[string]string{"publicKey": authorized, "name": "nochmal"})
	viewer.expect(200, "PUT", "/api/me/sftp/password", map[string]string{"password": "sftp-passwort-123"})

	// Abgleich: Anna (Operator) bekommt Zugang mit Passwort und Schlüssel, der Betrachter nicht, der Admin ohne Daten nicht
	p.sftpSync(ctx, true)
	grant, ok := fake.grant(row.ID, annaLogin)
	if !ok || !strings.HasPrefix(grant.PasswordHash, "$argon2id$v=19$m=32768,t=2,p=2$") || len(grant.Keys) != 1 || grant.ReadOnly {
		t.Fatalf("Zugang von Anna: %+v %v", grant, ok)
	}
	if _, ok := fake.grant(row.ID, "viewer."+row.ID[:8]); ok {
		t.Fatal("ein Betrachter hat SFTP-Zugang")
	}
	if len(fake.access[row.ID].Grants) != 1 {
		t.Fatalf("Zugänge: %+v", fake.access[row.ID])
	}
	puts := fake.putCount()
	p.sftpSync(ctx, false)
	if fake.putCount() != puts {
		t.Fatal("unveränderte Zugänge werden erneut geschickt")
	}

	// Karte im Dateien-Tab
	var card SFTPServerDTO
	_ = json.Unmarshal(anna.expect(200, "GET", "/api/servers/"+row.ID+"/sftp", nil), &card)
	if !card.Enabled || card.Host != "mc.example.org" || card.Port != 2022 || card.Username != annaLogin || card.Fingerprint != "SHA256:testfingerabdruck" || card.CanConfigure || !card.PasswordReady || card.KeyCount != 1 {
		t.Fatalf("Karte: %+v", card)
	}
	viewer.expect(403, "GET", "/api/servers/"+row.ID+"/sftp", nil)
	// Die Zugangsliste des Nodes ist aus dem Browser nie erreichbar, auch nicht für Administratoren
	admin.expect(404, "PUT", "/api/servers/"+row.ID+"/sftp-access", node.SFTPAccess{Grants: []node.SFTPGrant{{Username: "evil." + row.ID[:8]}}})
	stranger.expect(404, "GET", "/api/servers/"+row.ID+"/sftp", nil)
	anna.expect(403, "PUT", "/api/servers/"+row.ID+"/sftp", map[string]bool{"operatorsReadOnly": false})
	admin.expect(400, "PUT", "/api/servers/"+row.ID+"/sftp", map[string]any{"operatorsReadOnly": true, "extra": 1})

	// Nur lesen für Operatoren
	admin.expect(200, "PUT", "/api/servers/"+row.ID+"/sftp", map[string]bool{"operatorsReadOnly": true})
	p.sftpSync(ctx, false)
	if grant, _ := fake.grant(row.ID, annaLogin); !grant.ReadOnly {
		t.Fatal("Operator ist nicht auf nur lesen gestellt")
	}
	_ = json.Unmarshal(anna.expect(200, "GET", "/api/servers/"+row.ID+"/sftp", nil), &card)
	if !card.ReadOnly || !card.OperatorsReadOnly {
		t.Fatalf("Karte nur lesen: %+v", card)
	}

	// Passwort-Login abschalten: nur noch der Schlüssel
	anna.expect(200, "PUT", "/api/me/sftp/password-login", map[string]bool{"enabled": false})
	p.sftpSync(ctx, false)
	if grant, _ := fake.grant(row.ID, annaLogin); grant.PasswordHash != "" || len(grant.Keys) != 1 {
		t.Fatalf("ohne Passwort-Login: %+v", grant)
	}
	// Schlüssel weg und Passwort aus: kein Zugang mehr
	anna.expect(204, "DELETE", "/api/me/sftp/keys/"+key.ID, nil)
	anna.expect(404, "DELETE", "/api/me/sftp/keys/"+key.ID, nil)
	p.sftpSync(ctx, false)
	if _, ok := fake.grant(row.ID, annaLogin); ok {
		t.Fatal("Zugang ohne Passwort und Schlüssel")
	}
	anna.expect(200, "PUT", "/api/me/sftp/password-login", map[string]bool{"enabled": true})
	p.sftpSync(ctx, false)
	if _, ok := fake.grant(row.ID, annaLogin); !ok {
		t.Fatal("Zugang mit Passwort fehlt")
	}

	// Rolle auf Betrachter, dann wieder Operator, dann entfernt
	admin.expect(200, "PUT", "/api/projects/"+project.ID+"/members/"+ids["Anna"], map[string]string{"role": "viewer"})
	p.sftpSync(ctx, false)
	if _, ok := fake.grant(row.ID, annaLogin); ok {
		t.Fatal("Betrachter behält den Zugang")
	}
	admin.expect(200, "PUT", "/api/projects/"+project.ID+"/members/"+ids["Anna"], map[string]string{"role": "operator"})
	p.sftpSync(ctx, false)
	if _, ok := fake.grant(row.ID, annaLogin); !ok {
		t.Fatal("Zugang nach Rückstufung fehlt")
	}
	admin.expect(204, "DELETE", "/api/projects/"+project.ID+"/members/"+ids["Anna"], nil)
	p.sftpSync(ctx, false)
	if _, ok := fake.grant(row.ID, annaLogin); ok {
		t.Fatal("entferntes Mitglied behält den Zugang")
	}

	// Ein Administrator mit Passwort kommt überall hinein; gesperrt verliert er den Zugang
	admin.expect(200, "PUT", "/api/me/sftp/password", map[string]string{"password": "sftp-passwort-123"})
	p.sftpSync(ctx, false)
	if grant, ok := fake.grant(row.ID, "admin."+row.ID[:8]); !ok || grant.ReadOnly {
		t.Fatalf("Administrator: %+v %v", grant, ok)
	}

	// Node nicht erreichbar: nichts geht verloren, nach der Rückkehr wird alles neu geschickt
	fake.mu.Lock()
	fake.down = true
	fake.mu.Unlock()
	p.sftpSync(ctx, true)
	fake.mu.Lock()
	fake.down, fake.access = false, map[string]node.SFTPAccess{}
	fake.mu.Unlock()
	p.sftpSync(ctx, true)
	if _, ok := fake.grant(row.ID, "admin."+row.ID[:8]); !ok {
		t.Fatal("nach der Rückkehr des Nodes fehlen die Zugänge")
	}
	// Neustart des Nodes (neue BootID): ebenfalls alles neu
	fake.mu.Lock()
	fake.bootID, fake.access = "boot-2", map[string]node.SFTPAccess{}
	fake.mu.Unlock()
	p.sftpSync(ctx, true)
	if _, ok := fake.grant(row.ID, "admin."+row.ID[:8]); !ok {
		t.Fatal("nach dem Neustart des Nodes fehlen die Zugänge")
	}

	// Server im Panel gelöscht, der Node hat ihn aber noch (force): Er verliert alle Zugänge, und das nur einmal
	if err := p.store.DeleteServer(row.ID); err != nil {
		t.Fatal(err)
	}
	fake.mu.Lock()
	fake.orphans = []string{row.ID}
	fake.mu.Unlock()
	p.invalidate(sftpNode.ID)
	p.sftpSync(ctx, false)
	fake.mu.Lock()
	left, known := fake.access[row.ID]
	fake.mu.Unlock()
	if !known || len(left.Grants) != 0 {
		t.Fatalf("verwaister Server behält Zugänge: %+v", left)
	}
	puts = fake.putCount()
	p.invalidate(sftpNode.ID)
	p.sftpSync(ctx, false)
	if fake.putCount() != puts {
		t.Fatal("ein verwaister Server wird immer wieder beliefert")
	}
	// Gibt es ihn auch auf dem Node nicht mehr, wird er nicht mehr beliefert
	fake.mu.Lock()
	fake.orphans = nil
	fake.mu.Unlock()
	p.invalidate(sftpNode.ID)
	p.sftpSync(ctx, false)
	if fake.putCount() != puts {
		t.Fatal("ein gelöschter Server wird weiter beliefert")
	}

	// API-Schlüssel kommen nicht an die SFTP-Daten
	var created struct{ Key string }
	_ = json.Unmarshal(admin.expect(201, "POST", "/api/me/keys", map[string]any{"name": "Skript"}), &created)
	if status, _ := newClient(t, web.URL).do("GET", "/api/me/sftp", nil, "Authorization", "Bearer "+created.Key); status != http.StatusForbidden {
		t.Errorf("API-Schlüssel an /api/me/sftp: %d", status)
	}
}

func TestSFTPLoginsGoToTheAuditLog(t *testing.T) {
	p, web, _, _ := setupPanel(t)
	fake, fakeServer := newFakeSFTPNode(t)
	if _, err := p.store.CreateNode(Node{Name: "sftp", URL: fakeServer.URL, Token: "token"}); err != nil {
		t.Fatal(err)
	}
	admin := newClient(t, web.URL)
	admin.expect(201, "POST", "/api/auth/setup", map[string]string{"username": "admin", "password": "ein-gutes-passwort"})
	authorized := testAuthorizedKey(t)
	var key SFTPKey
	_ = json.Unmarshal(admin.expect(201, "POST", "/api/me/sftp/keys", map[string]string{"publicKey": authorized, "name": "Laptop"}), &key)

	fake.mu.Lock()
	fake.events = []node.SFTPEvent{
		{Seq: 1, At: 4102444800000, ServerID: "abc", Username: "admin.abcdef12", IP: "203.0.113.7", Method: "publickey", KeyFingerprint: key.Fingerprint, Success: true},
		{Seq: 2, At: 4102444800001, ServerID: "abc", Username: "admin.abcdef12", IP: "203.0.113.8", Method: "password"},
	}
	fake.mu.Unlock()
	p.sftpSync(context.Background(), true)
	p.sftpSync(context.Background(), true) // dieselben Ereignisse nicht doppelt

	var entries []AuditEntry
	_ = json.Unmarshal(admin.expect(200, "GET", "/api/audit?limit=50", nil), &entries)
	found := map[string]int{}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Action, "SFTP-") {
			found[entry.Action+" "+entry.IP]++
			if entry.Username != "admin" || entry.UserID == "" || entry.Target != "/api/servers/abc" {
				t.Errorf("Eintrag: %+v", entry)
			}
		}
	}
	if found["SFTP-Anmeldung (SSH-Schlüssel) 203.0.113.7"] != 1 || found["SFTP-Anmeldung abgelehnt (falsches Passwort) 203.0.113.8"] != 1 {
		t.Fatalf("Protokoll: %v", found)
	}
	var account SFTPAccountDTO
	_ = json.Unmarshal(admin.expect(200, "GET", "/api/me/sftp", nil), &account)
	if len(account.Keys) != 1 || account.Keys[0].LastUsed != 4102444800000 {
		t.Fatalf("zuletzt benutzt: %+v", account.Keys)
	}
}

func TestSFTPTargets(t *testing.T) {
	snapshot := sftpSnapshot{
		users: []User{
			{ID: "a", Username: "Admin", IsAdmin: true},
			{ID: "o", Username: "op"},
			{ID: "g", Username: "gesperrt", Disabled: true},
			{ID: "n", Username: "ohne-daten"},
		},
		members: map[string]map[string]string{"p1": {"o": "operator", "g": "owner", "n": "admin"}},
		credentials: map[string]SFTPCredential{
			"a": {PasswordHash: "hash-a", PasswordLogin: true},
			"o": {PasswordHash: "hash-o", PasswordLogin: true},
			"g": {PasswordHash: "hash-g", PasswordLogin: true},
		},
		keys:     map[string][]string{},
		readOnly: map[string]bool{"s1aaaaaaaaaa": true},
		servers:  []ServerRow{{ID: "s1aaaaaaaaaa", ProjectID: "p1"}, {ID: "s2bbbbbbbbbb", ProjectID: "p2"}},
	}
	targets := sftpTargets(snapshot)
	first := targets["s1aaaaaaaaaa"].Grants
	if len(first) != 2 || first[0].Username != "admin.s1aaaaaa" || first[0].ReadOnly || first[1].Username != "op.s1aaaaaa" || !first[1].ReadOnly {
		t.Fatalf("Server 1: %+v", first)
	}
	second := targets["s2bbbbbbbbbb"].Grants
	if len(second) != 1 || second[0].Username != "admin.s2bbbbbb" {
		t.Fatalf("Server 2: %+v", second)
	}
	if targets["s2bbbbbbbbbb"].Grants == nil {
		t.Fatal("leere Liste muss [] sein")
	}
}

// TestSFTPTargetsUseCustomPermissions prüft, dass der Abgleich die wirksamen Rechte nimmt, nicht nur die Rolle.
func TestSFTPTargetsUseCustomPermissions(t *testing.T) {
	credential := SFTPCredential{PasswordHash: "hash", PasswordLogin: true}
	snapshot := sftpSnapshot{
		users:   []User{{ID: "v", Username: "gast"}, {ID: "o", Username: "op"}, {ID: "l", Username: "leser"}, {ID: "s", Username: "chef"}},
		members: map[string]map[string]string{"p1": {"v": "viewer", "o": "operator", "l": "operator", "s": "viewer"}},
		custom: map[string]map[string][]Permission{"p1": {
			"v": {PermSFTP, PermFilesRead, PermFilesWrite},               // Betrachter mit SFTP und Schreiben
			"o": {PermConsoleRead, PermFilesRead, PermFilesWrite},        // Operator ohne SFTP
			"l": {PermSFTP, PermFilesRead},                               // Operator ohne „Dateien ändern“
			"s": {PermSFTP, PermFilesRead, PermFilesWrite, PermSettings}, // darf „nur lesend“ selbst ändern
		}},
		credentials: map[string]SFTPCredential{"v": credential, "o": credential, "l": credential, "s": credential},
		keys:        map[string][]string{},
		readOnly:    map[string]bool{"s1aaaaaaaaaa": true},
		servers:     []ServerRow{{ID: "s1aaaaaaaaaa", ProjectID: "p1"}},
	}
	grants := map[string]node.SFTPGrant{}
	for _, grant := range sftpTargets(snapshot)["s1aaaaaaaaaa"].Grants {
		grants[grant.Username] = grant
	}
	if _, ok := grants["op.s1aaaaaa"]; ok {
		t.Error("Operator ohne Recht „SFTP-Zugang“ kommt hinein")
	}
	if grant, ok := grants["gast.s1aaaaaa"]; !ok || !grant.ReadOnly {
		t.Errorf("Betrachter mit SFTP (Server „nur lesend“): %+v %v", grant, ok)
	}
	if grant, ok := grants["leser.s1aaaaaa"]; !ok || !grant.ReadOnly {
		t.Errorf("ohne „Dateien ändern“ muss SFTP nur lesen: %+v %v", grant, ok)
	}
	if grant, ok := grants["chef.s1aaaaaa"]; !ok || grant.ReadOnly {
		t.Errorf("mit „Einstellungen“ gilt „Operatoren nur lesend“ nicht: %+v %v", grant, ok)
	}
	snapshot.readOnly = map[string]bool{}
	for _, grant := range sftpTargets(snapshot)["s1aaaaaaaaaa"].Grants {
		if want := grant.Username == "leser.s1aaaaaa"; grant.ReadOnly != want {
			t.Errorf("ohne „nur lesend“: %+v", grant)
		}
	}
}

// TestSFTPSyncStartsRightAway prüft, dass Änderungen an Mitgliedern und Benutzern den Abgleich sofort anstoßen.
func TestSFTPSyncStartsRightAway(t *testing.T) {
	p, admin, _, _, ids, _ := rightsProject(t, "lea", "max")
	var project ProjectDTO
	_ = json.Unmarshal(admin.expect(201, "POST", "/api/projects", map[string]string{"name": "Zweites", "kind": "server"}), &project)
	p.sftp.init()
	woken := func(what string, call func()) {
		t.Helper()
		select {
		case <-p.sftp.wake:
		default:
		}
		call()
		select {
		case <-p.sftp.wake:
		default:
			t.Errorf("%s stößt den SFTP-Abgleich nicht an", what)
		}
	}
	members := "/api/projects/" + project.ID + "/members/"
	woken("Rolle vergeben", func() { admin.expect(200, "PUT", members+ids["lea"], map[string]string{"role": "operator"}) })
	woken("Rechte anpassen", func() {
		admin.expect(200, "PUT", members+ids["lea"], map[string]any{"role": "operator", "permissions": []Permission{PermFilesRead}})
	})
	woken("Mitglied entfernen", func() { admin.expect(204, "DELETE", members+ids["lea"], nil) })
	woken("Benutzer sperren", func() { admin.expect(200, "PATCH", "/api/users/"+ids["lea"], map[string]bool{"disabled": true}) })
	woken("Benutzer löschen", func() { admin.expect(204, "DELETE", "/api/users/"+ids["max"], nil) })
	woken("Projekt löschen", func() { admin.expect(204, "DELETE", "/api/projects/"+project.ID, nil) })
}

// TestSFTPLoginsBecomeEvents prüft, dass Anmeldungen im Verlauf des Projekts landen (ohne IP-Adresse).
func TestSFTPLoginsBecomeEvents(t *testing.T) {
	p, web, _, _ := setupPanel(t)
	fake, fakeServer := newFakeSFTPNode(t)
	sftpNode, err := p.store.CreateNode(Node{Name: "sftp", URL: fakeServer.URL, Token: "token"})
	if err != nil {
		t.Fatal(err)
	}
	admin := newClient(t, web.URL)
	admin.expect(201, "POST", "/api/auth/setup", map[string]string{"username": "admin", "password": "ein-gutes-passwort"})
	var project ProjectDTO
	_ = json.Unmarshal(admin.expect(201, "POST", "/api/projects", map[string]string{"name": "Survival", "kind": "server"}), &project)
	row, err := p.store.CreateServer(ServerRow{ProjectID: project.ID, NodeID: sftpNode.ID, Name: "Main", Slug: "main", Role: "standalone", Software: "paper"})
	if err != nil {
		t.Fatal(err)
	}
	fake.mu.Lock()
	fake.events = []node.SFTPEvent{
		{Seq: 1, At: 4102444800000, ServerID: row.ID, Username: "admin." + row.ID[:8], IP: "203.0.113.7", Method: "publickey", Success: true},
		{Seq: 2, At: 4102444800001, ServerID: row.ID, Username: "admin." + row.ID[:8], IP: "203.0.113.8", Method: "password"},
	}
	fake.mu.Unlock()
	p.sftpSync(context.Background(), true)

	var events []Event
	_ = json.Unmarshal(admin.expect(200, "GET", "/api/projects/"+project.ID+"/events", nil), &events)
	levels := map[string]string{}
	for _, event := range events {
		if event.Kind != "sftp_login" {
			continue
		}
		levels[event.Level] = event.Message
		if event.ServerID != row.ID || strings.Contains(event.Message, "203.0.113") {
			t.Errorf("Ereignis: %+v", event)
		}
	}
	if !strings.Contains(levels["info"], "SSH-Schlüssel") || !strings.Contains(levels["warn"], "falsches Passwort") {
		t.Fatalf("Ereignisse: %+v", events)
	}
	if !slices.Contains(eventKinds, "sftp_login") || titles["sftp_login"] == "" {
		t.Error("Ereignisart fehlt in der Liste für Webhooks")
	}
}
