package panel

import (
	"encoding/json"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

// TestRoleTemplates hält fest, welche Rechte jede Rolle als Vorlage hat.
func TestRoleTemplates(t *testing.T) {
	viewer := []Permission{PermConsoleRead, PermBackupsRead}
	operator := append(append([]Permission{}, viewer...), PermConsoleCommand, PermPower, PermFilesRead, PermFilesWrite, PermSFTP,
		PermBackupsCreate, PermBackupsRestore, PermBackupsDelete, PermAddons, PermPlayers)
	admin := append(append([]Permission{}, operator...), PermSettings, PermServers, PermSchedules, PermWebhooks, PermMembers, PermNetwork)
	want := map[int][]Permission{rankNone: {}, rankViewer: viewer, rankOperator: operator, rankAdmin: admin, rankOwner: admin}
	for rank, list := range want {
		got := accessFor(rank, nil)
		if got.Custom {
			t.Errorf("Rang %d: Vorlage gilt als angepasst", rank)
		}
		for _, perm := range list {
			if !got.Has(perm) {
				t.Errorf("Rang %d: %s fehlt", rank, perm)
			}
		}
		if len(got.List()) != len(list) {
			t.Errorf("Rang %d: %v, erwartet %v", rank, got.List(), list)
		}
	}
	// Eigentümer haben immer alles, auch wenn in der Datenbank eine Liste stünde
	if owner := accessFor(rankOwner, []Permission{PermConsoleRead}); owner.Custom || len(owner.List()) != len(permissionCatalog) {
		t.Errorf("Eigentümer: %+v", owner.List())
	}
	if len(permissionByID) != len(permissionCatalog) {
		t.Error("doppelte Rechte im Katalog")
	}
}

// TestProxyPermissionsMatchRanks prüft, dass die durchgereichten Node-Routen mit den Vorlagen genauso streng sind wie
// mit der alten Stufe aus requiredRank.
func TestProxyPermissionsMatchRanks(t *testing.T) {
	routes := [][2]string{
		{"GET", "logs"}, {"GET", "players"}, {"GET", "addons"}, {"GET", "modpack"},
		{"POST", "power"}, {"POST", "command"}, {"POST", "reinstall"},
		{"GET", "files"}, {"GET", "files/content"}, {"GET", "files/download"}, {"PUT", "files/content"}, {"POST", "files/upload"},
		{"POST", "files/delete"}, {"POST", "files/rename"}, {"POST", "files/pull"}, {"DELETE", "files"},
		{"GET", "backups"}, {"POST", "backups"}, {"DELETE", "backups/a.tar.gz"}, {"GET", "backups/a.tar.gz/download"}, {"POST", "backups/a.tar.gz/restore"},
	}
	for _, route := range routes {
		need, ok := requiredRank(route[0], route[1])
		if !ok {
			t.Fatalf("%s %s ist nicht freigegeben", route[0], route[1])
		}
		perms, known := proxyPermissions(route[0], route[1])
		if !known {
			t.Errorf("%s %s: kein Recht hinterlegt", route[0], route[1])
			continue
		}
		for _, rank := range []int{rankViewer, rankOperator, rankAdmin, rankOwner} {
			if allowed := accessFor(rank, nil).Has(perms...); allowed != (rank >= need) {
				t.Errorf("%s %s mit Rang %d: erlaubt=%v, vorher %v", route[0], route[1], rank, allowed, rank >= need)
			}
		}
	}
}

type matrixRoute struct {
	method, path string
	body         any
	minimum      int
}

// TestRolesKeepTheirAccess spielt jede Route mit jeder Rolle durch: Abgelehnt (403) wird genau dann, wenn die Rolle
// unter der Stufe liegt, die die Route vor den Einzelrechten verlangt hat. Damit ändert sich für niemanden etwas.
func TestRolesKeepTheirAccess(t *testing.T) {
	for _, role := range []string{"viewer", "operator", "admin", "owner"} {
		t.Run(role, func(t *testing.T) { checkRoleMatrix(t, role) })
	}
}

func checkRoleMatrix(t *testing.T, role string) {
	p, web, _, nodeID := setupPanel(t)
	p.modrinth = fakeModrinth(t)
	admin := newClient(t, web.URL)
	admin.expect(201, "POST", "/api/auth/setup", map[string]string{"username": "admin", "password": "ein-gutes-passwort"})
	for _, name := range []string{"mitglied", "fremd"} {
		admin.expect(201, "POST", "/api/users", map[string]any{"username": name, "password": "ein-gutes-passwort"})
	}
	ids := userIDs(t, p)
	var project ProjectDTO
	_ = json.Unmarshal(admin.expect(201, "POST", "/api/projects", map[string]string{"name": "Netz", "kind": "network"}), &project)
	admin.expect(200, "PUT", "/api/projects/"+project.ID+"/members/"+ids["mitglied"], map[string]string{"role": role})
	var server ServerDTO
	_ = json.Unmarshal(admin.expect(201, "POST", "/api/projects/"+project.ID+"/servers", serverRequest{Name: "Main", NodeID: nodeID, Software: "paper", Version: "1.21.4", EULA: true}), &server)
	group, err := p.store.CreateGroup(Group{ProjectID: project.ID, Name: "Lobby", Slug: "lobby", Software: "paper", Version: "1.21.4", MemoryMB: 1024, MaxInstances: 0, PlayersPerInstance: 10, Plugins: []string{}})
	if err != nil {
		t.Fatal(err)
	}
	schedule, err := p.store.CreateSchedule(Schedule{ProjectID: project.ID, ServerID: server.ID, Name: "Ansage", Steps: []Step{{Action: "command", Value: "say hallo"}}, Kind: "daily", Weekdays: 127})
	if err != nil {
		t.Fatal(err)
	}
	hook, err := p.store.CreateWebhook(Webhook{ProjectID: project.ID, Name: "Test", URL: "http://127.0.0.1:1/hook", Kind: "generic", Events: []string{}, Enabled: true})
	if err != nil {
		t.Fatal(err)
	}

	member := newClient(t, web.URL)
	member.expect(200, "POST", "/api/auth/login", map[string]string{"username": "mitglied", "password": "ein-gutes-passwort"})

	bad := map[string]any{"unbekanntesFeld": true} // wer darf, scheitert danach an der strengen Eingabeprüfung (400)
	pp, sp := "/api/projects/"+project.ID, "/api/servers/"+server.ID
	routes := []matrixRoute{
		{"GET", pp, nil, rankViewer},
		{"PATCH", pp, bad, rankAdmin},
		{"GET", pp + "/metrics", nil, rankViewer},
		{"GET", pp + "/events", nil, rankViewer},
		{"GET", pp + "/schedules", nil, rankViewer},
		{"POST", pp + "/schedules", bad, rankAdmin},
		{"GET", pp + "/webhooks", nil, rankAdmin},
		{"POST", pp + "/webhooks", bad, rankAdmin},
		{"PUT", pp + "/network", bad, rankAdmin},
		{"POST", pp + "/network/sync", nil, rankAdmin},
		{"PUT", pp + "/voicechat", bad, rankAdmin},
		{"PUT", pp + "/motd", bad, rankAdmin},
		{"PUT", pp + "/status", bad, rankAdmin},
		{"POST", pp + "/servers", bad, rankAdmin},
		{"POST", pp + "/groups", bad, rankAdmin},
		{"PUT", pp + "/members/" + ids["fremd"], bad, rankAdmin},
		{"DELETE", pp + "/members/" + ids["fremd"], nil, rankAdmin},
		{"PATCH", "/api/groups/" + group.ID, bad, rankAdmin},
		{"POST", "/api/groups/" + group.ID + "/instances", nil, rankOperator},
		{"PATCH", "/api/schedules/" + schedule.ID, bad, rankAdmin},
		{"POST", "/api/schedules/" + schedule.ID + "/run", nil, rankOperator},
		{"PATCH", "/api/webhooks/" + hook.ID, bad, rankAdmin},
		{"POST", "/api/webhooks/" + hook.ID + "/test", nil, rankAdmin},
		{"GET", sp, nil, rankViewer},
		{"PATCH", sp, bad, rankAdmin},
		{"GET", sp + "/metrics", nil, rankViewer},
		{"GET", sp + "/player-history", nil, rankViewer},
		{"GET", sp + "/modrinth/search?q=x", nil, rankViewer},
		{"GET", sp + "/modrinth/project/x", nil, rankViewer},
		{"POST", sp + "/modrinth/install", bad, rankOperator},
		{"GET", sp + "/addons/details", nil, rankViewer},
		{"POST", sp + "/addons/switch", bad, rankOperator},
		{"POST", sp + "/addons/check", nil, rankViewer},
		{"POST", sp + "/addons/update", bad, rankOperator},
		{"PUT", sp + "/public-address", bad, rankAdmin},
		{"POST", sp + "/tunnel", nil, rankAdmin},
		{"DELETE", sp + "/tunnel", nil, rankAdmin},
		{"PUT", sp + "/auto-restart", bad, rankAdmin},
		{"GET", sp + "/player-lists", nil, rankOperator},
		{"POST", sp + "/player-action", bad, rankOperator},
		{"GET", sp + "/logs", nil, rankViewer},
		{"GET", sp + "/players", nil, rankViewer},
		{"GET", sp + "/addons", nil, rankViewer},
		{"GET", sp + "/modpack", nil, rankViewer},
		{"POST", sp + "/power", map[string]string{"action": "start"}, rankOperator},
		{"POST", sp + "/command", map[string]string{"command": "list"}, rankOperator},
		{"GET", sp + "/files?path=/", nil, rankOperator},
		{"GET", sp + "/files/content?path=server.properties", nil, rankOperator},
		{"PUT", sp + "/files/content?path=x.txt", nil, rankOperator},
		{"POST", sp + "/files/delete", map[string]any{"paths": []string{}}, rankOperator},
		{"GET", sp + "/backups", nil, rankViewer},
		{"POST", sp + "/backups", map[string]string{"label": "x"}, rankOperator},
		{"GET", sp + "/backups/x.tar.gz/download", nil, rankOperator},
		{"POST", sp + "/backups/x.tar.gz/restore", nil, rankOperator},
		{"DELETE", sp + "/backups/x.tar.gz", nil, rankOperator},
		{"POST", sp + "/reinstall", map[string]string{"software": "paper", "version": "1.21.4"}, rankAdmin},
		// zerstörend, darum am Ende
		{"DELETE", "/api/schedules/" + schedule.ID, nil, rankAdmin},
		{"DELETE", "/api/webhooks/" + hook.ID, nil, rankAdmin},
		{"DELETE", "/api/groups/" + group.ID, nil, rankAdmin},
		{"DELETE", sp, nil, rankAdmin},
		{"DELETE", pp, nil, rankOwner},
	}
	rank := roleRanks[role]
	for _, route := range routes {
		status, data := member.do(route.method, route.path, route.body)
		if denied := status == 403; denied != (rank < route.minimum) {
			t.Errorf("%s %s als %s: Status %d (%s), vorher nötig: %s", route.method, route.path, role, status, strings.TrimSpace(string(data)), rankName(route.minimum))
		}
	}
}

func userIDs(t *testing.T, p *Panel) map[string]string {
	t.Helper()
	users, err := p.store.ListUsers()
	if err != nil {
		t.Fatal(err)
	}
	ids := map[string]string{}
	for _, user := range users {
		ids[user.Username] = user.ID
	}
	return ids
}

// rightsProject legt ein Server-Projekt an und dazu Benutzer, die sich anmelden können.
func rightsProject(t *testing.T, names ...string) (*Panel, *client, ServerDTO, string, map[string]string, func(string) *client) {
	p, admin, _, _, server, projectID := serverProject(t)
	for _, name := range names {
		admin.expect(201, "POST", "/api/users", map[string]any{"username": name, "password": "ein-gutes-passwort"})
	}
	login := func(name string) *client {
		c := newClient(t, admin.base)
		c.expect(200, "POST", "/api/auth/login", map[string]string{"username": name, "password": "ein-gutes-passwort"})
		return c
	}
	return p, admin, server, projectID, userIDs(t, p), login
}

func memberByName(t *testing.T, data []byte, name string) MemberDTO {
	t.Helper()
	var members []MemberDTO
	if err := json.Unmarshal(data, &members); err != nil {
		t.Fatal(err)
	}
	for _, member := range members {
		if member.Username == name {
			return member
		}
	}
	t.Fatalf("%s fehlt in %s", name, data)
	return MemberDTO{}
}

func TestCustomPermissions(t *testing.T) {
	_, admin, server, projectID, ids, login := rightsProject(t, "lea")
	members := "/api/projects/" + projectID + "/members/"
	sp := "/api/servers/" + server.ID

	// Betrachter mit angepasster Liste: Dateien ansehen ja, ändern nein, Konsole nicht
	data := admin.expect(200, "PUT", members+ids["lea"], map[string]any{"role": "viewer", "permissions": []Permission{PermFilesRead, PermBackupsRead, PermFilesRead}})
	lea := memberByName(t, data, "lea")
	if !lea.CustomPermissions || !reflect.DeepEqual(lea.Permissions, []Permission{PermFilesRead, PermBackupsRead}) {
		t.Fatalf("Mitglied: %+v", lea)
	}
	c := login("lea")
	var answer struct {
		MyRole        string       `json:"myRole"`
		MyPermissions []Permission `json:"myPermissions"`
	}
	_ = json.Unmarshal(c.expect(200, "GET", sp, nil), &answer)
	if answer.MyRole != "viewer" || !reflect.DeepEqual(answer.MyPermissions, []Permission{PermFilesRead, PermBackupsRead}) {
		t.Errorf("Server-Antwort: %+v", answer)
	}
	var project ProjectDTO
	_ = json.Unmarshal(c.expect(200, "GET", "/api/projects/"+projectID, nil), &project)
	if !reflect.DeepEqual(project.MyPermissions, answer.MyPermissions) {
		t.Errorf("Projekt-Antwort: %v", project.MyPermissions)
	}
	var list []ProjectDTO
	_ = json.Unmarshal(c.expect(200, "GET", "/api/projects", nil), &list)
	if len(list) != 1 || !reflect.DeepEqual(list[0].MyPermissions, answer.MyPermissions) {
		t.Errorf("Projektliste: %+v", list)
	}
	if status, _ := c.do("GET", sp+"/files?path=/", nil); status == 403 {
		t.Error("Dateien ansehen wird abgelehnt")
	}
	if status, _ := c.do("GET", sp+"/backups/x.tar.gz/download", nil); status == 403 {
		t.Error("Backup herunterladen wird trotz Dateien ansehen abgelehnt")
	}
	if body := string(c.expect(403, "PUT", sp+"/files/content?path=a.txt", nil)); !strings.Contains(body, "Dateien ändern") {
		t.Errorf("Meldung nennt das Recht nicht: %s", body)
	}
	c.expect(403, "GET", sp+"/logs", nil)
	if body := string(c.expect(403, "GET", sp+"/ws", nil)); !strings.Contains(body, "Konsole lesen") {
		t.Errorf("Konsole ohne Recht: %s", body)
	}

	// Mehr Rechte als die Rolle: ein Betrachter, der Einstellungen ändern darf
	admin.expect(200, "PUT", members+ids["lea"], map[string]any{"role": "viewer", "permissions": []Permission{PermSettings}})
	if status, _ := c.do("PUT", sp+"/auto-restart", map[string]bool{"enabled": true}); status == 403 {
		t.Error("angepasstes Recht Einstellungen greift nicht")
	}
	c.expect(403, "GET", "/api/projects/"+projectID+"/webhooks", nil)

	// Rolle ohne Liste setzt auf die Vorlage zurück; eine Liste gleich der Vorlage gilt als Vorlage
	lea = memberByName(t, admin.expect(200, "PUT", members+ids["lea"], map[string]string{"role": "operator"}), "lea")
	if lea.CustomPermissions || len(lea.Permissions) != len(accessFor(rankOperator, nil).List()) {
		t.Errorf("Vorlage: %+v", lea)
	}
	lea = memberByName(t, admin.expect(200, "PUT", members+ids["lea"], map[string]any{"role": "viewer", "permissions": []Permission{PermBackupsRead, PermConsoleRead}}), "lea")
	if lea.CustomPermissions {
		t.Errorf("Liste gleich Vorlage gilt als angepasst: %+v", lea)
	}

	// Ungültiges
	admin.expect(400, "PUT", members+ids["lea"], map[string]any{"role": "viewer", "permissions": []string{"alles"}})
	admin.expect(400, "PUT", members+ids["lea"], map[string]any{"role": "owner", "permissions": []Permission{}})

	// Der Katalog für die Oberfläche
	var catalog permissionCatalogDTO
	_ = json.Unmarshal(c.expect(200, "GET", "/api/permissions", nil), &catalog)
	if len(catalog.Permissions) != len(permissionCatalog) || catalog.Permissions[0].Label == "" || catalog.Permissions[0].Role != "viewer" {
		t.Errorf("Katalog: %+v", catalog.Permissions)
	}
	if len(catalog.Roles["admin"]) != len(accessFor(rankAdmin, nil).List()) || len(catalog.Roles["viewer"]) != 2 {
		t.Errorf("Vorlagen: %+v", catalog.Roles)
	}
}

func TestNobodyGrantsMoreThanTheyHave(t *testing.T) {
	_, admin, _, projectID, ids, login := rightsProject(t, "mod", "oper", "neu", "chef", "gast")
	members := "/api/projects/" + projectID + "/members/"
	withoutFiles := []Permission{}
	for _, perm := range accessFor(rankAdmin, nil).List() {
		if perm != PermFilesWrite {
			withoutFiles = append(withoutFiles, perm)
		}
	}
	admin.expect(200, "PUT", members+ids["mod"], map[string]any{"role": "admin", "permissions": withoutFiles})
	admin.expect(200, "PUT", members+ids["oper"], map[string]string{"role": "operator"})
	admin.expect(200, "PUT", members+ids["chef"], map[string]string{"role": "owner"})
	mod := login("mod")

	// Was mod selbst nicht hat, kann er nicht vergeben – weder direkt noch über eine Rolle
	if body := string(mod.expect(403, "PUT", members+ids["neu"], map[string]any{"role": "viewer", "permissions": []Permission{PermFilesWrite}})); !strings.Contains(body, "Dateien ändern") {
		t.Errorf("Meldung: %s", body)
	}
	mod.expect(403, "PUT", members+ids["neu"], map[string]string{"role": "operator"})
	// sich selbst etwas geben geht auch nicht
	mod.expect(403, "PUT", members+ids["mod"], map[string]string{"role": "admin"})
	mod.expect(403, "PUT", members+ids["mod"], map[string]any{"role": "admin", "permissions": accessFor(rankAdmin, nil).List()})
	// Was er hat, darf er weitergeben; wegnehmen darf er auch Rechte, die er selbst nicht hat
	mod.expect(200, "PUT", members+ids["neu"], map[string]any{"role": "viewer", "permissions": []Permission{PermConsoleRead, PermMembers}})
	op := memberByName(t, mod.expect(200, "PUT", members+ids["oper"], map[string]any{"role": "operator", "permissions": []Permission{PermConsoleRead, PermPower}}), "oper")
	if !reflect.DeepEqual(op.Permissions, []Permission{PermConsoleRead, PermPower}) {
		t.Errorf("Operator nach dem Wegnehmen: %+v", op)
	}
	// Rangordnung gilt weiter: keine höhere Rolle, keine Eigentümer ändern
	mod.expect(403, "PUT", members+ids["neu"], map[string]string{"role": "owner"})
	mod.expect(403, "PUT", members+ids["chef"], map[string]string{"role": "viewer"})
	mod.expect(403, "DELETE", members+ids["chef"], nil)

	// Ein Betrachter mit „Mitglieder“ vergibt nur Betrachter, nur seine Rechte und ändert keine höher Gestellten
	neu := login("neu")
	neu.expect(403, "PUT", members+ids["oper"], map[string]any{"role": "viewer", "permissions": []Permission{PermConsoleRead}})
	neu.expect(403, "PUT", members+ids["gast"], map[string]string{"role": "operator"})
	neu.expect(403, "PUT", members+ids["gast"], map[string]any{"role": "viewer", "permissions": []Permission{PermConsoleRead, PermPower}})
	neu.expect(200, "PUT", members+ids["gast"], map[string]any{"role": "viewer", "permissions": []Permission{PermConsoleRead}})
	// Ohne „Mitglieder“ geht nichts, außer selbst zu gehen
	op2 := login("oper")
	op2.expect(403, "PUT", members+ids["neu"], map[string]string{"role": "viewer"})
	op2.expect(403, "DELETE", members+ids["neu"], nil)
	op2.expect(204, "DELETE", members+ids["oper"], nil)

	// Eigentümer-Dinge hängen an der Rolle: auch mit allen Rechten löscht ein Admin das Projekt nicht
	admin.expect(200, "PUT", members+ids["mod"], map[string]string{"role": "admin"})
	mod.expect(403, "DELETE", "/api/projects/"+projectID, nil)
}

func TestSchedulesNeedTheRightsOfTheirSteps(t *testing.T) {
	_, admin, server, projectID, ids, login := rightsProject(t, "planer")
	admin.expect(200, "PUT", "/api/projects/"+projectID+"/members/"+ids["planer"], map[string]any{"role": "viewer", "permissions": []Permission{PermSchedules, PermPower}})
	c := login("planer")
	path := "/api/projects/" + projectID + "/schedules"
	plan := func(steps ...Step) map[string]any {
		return map[string]any{"serverId": server.ID, "name": "x", "kind": "daily", "atMinute": 60, "steps": steps}
	}
	if body := string(c.expect(403, "POST", path, plan(Step{Action: "command", Value: "op jemand"}))); !strings.Contains(body, "Befehle senden") {
		t.Errorf("Meldung: %s", body)
	}
	c.expect(403, "POST", path, plan(Step{Action: "backup"}))
	var created scheduleDTO
	_ = json.Unmarshal(c.expect(201, "POST", path, plan(Step{Action: "restart"})), &created)
	c.expect(403, "PATCH", "/api/schedules/"+created.ID, map[string]any{"steps": []Step{{Action: "command", Value: "stop"}}})
	c.expect(200, "PATCH", "/api/schedules/"+created.ID, map[string]any{"steps": []Step{{Action: "wait", Value: "1"}, {Action: "restart"}}})

	// Ein Zeitplan mit Befehl, den ein Admin angelegt hat, startet der Planer nicht von Hand
	var other scheduleDTO
	_ = json.Unmarshal(admin.expect(201, "POST", path, plan(Step{Action: "command", Value: "say hallo"})), &other)
	c.expect(403, "POST", "/api/schedules/"+other.ID+"/run", nil)
	c.expect(202, "POST", "/api/schedules/"+created.ID+"/run", nil)
	time.Sleep(50 * time.Millisecond) // der Lauf im Hintergrund soll vor dem Aufräumen fertig sein
}

func TestGlobalAdminHasAllPermissions(t *testing.T) {
	_, admin, server, _, _, _ := rightsProject(t)
	var answer struct {
		MyRole        string       `json:"myRole"`
		MyPermissions []Permission `json:"myPermissions"`
	}
	_ = json.Unmarshal(admin.expect(200, "GET", "/api/servers/"+server.ID, nil), &answer)
	if answer.MyRole != "owner" || len(answer.MyPermissions) != len(permissionCatalog) {
		t.Errorf("Administrator: %+v", answer)
	}
}

func TestMemberPermissionsAreStored(t *testing.T) {
	store, err := OpenStore(filepath.Join(t.TempDir(), "rechte.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	user, err := store.CreateUser("lea", "hash", false)
	if err != nil {
		t.Fatal(err)
	}
	project, err := store.CreateProject(Project{Name: "P", Kind: "server", Secret: "s", OwnerID: user.ID})
	if err != nil {
		t.Fatal(err)
	}
	if role, perms, err := store.MemberAccess(project.ID, user.ID); err != nil || role != "owner" || perms != nil {
		t.Fatalf("neues Projekt: %q %v %v", role, perms, err)
	}
	if err := store.SetMemberAccess(project.ID, user.ID, "viewer", []Permission{PermPower}); err != nil {
		t.Fatal(err)
	}
	if role, perms, _ := store.MemberAccess(project.ID, user.ID); role != "viewer" || !reflect.DeepEqual(perms, []Permission{PermPower}) {
		t.Errorf("gespeichert: %q %v", role, perms)
	}
	if all, _ := store.MemberPermissions(project.ID); len(all) != 1 {
		t.Errorf("Liste: %v", all)
	}
	if err := store.SetMemberAccess(project.ID, user.ID, "operator", nil); err != nil {
		t.Fatal(err)
	}
	if _, perms, _ := store.MemberAccess(project.ID, user.ID); perms != nil {
		t.Errorf("Vorlage nach Rollenwechsel: %v", perms)
	}
	if _, _, err := store.MemberAccess(project.ID, "fremd"); err != ErrNotFound {
		t.Errorf("Fremder: %v", err)
	}
	// Eine kaputte Liste gibt keine Rechte statt zu vielen
	if got := decodePermissions("{kaputt"); got == nil || len(got) != 0 {
		t.Errorf("kaputte Liste: %v", got)
	}
}
