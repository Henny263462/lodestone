package panel

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestAdminPermissions(t *testing.T) {
	_, web, _, _ := setupPanel(t)

	// 1. Initialer Admin
	cAdmin := newClient(t, web.URL)
	cAdmin.expect(201, "POST", "/api/auth/setup", map[string]string{"username": "admin", "password": "ein-gutes-passwort"})

	// 2. Normaler Benutzer (Nicht-Admin)
	cAdmin.expect(201, "POST", "/api/users", map[string]any{"username": "normaluser", "password": "normal-password-123", "isAdmin": false})
	cNormal := newClient(t, web.URL)
	cNormal.expect(200, "POST", "/api/auth/login", map[string]string{"username": "normaluser", "password": "normal-password-123"})

	// 3. Nicht angemeldeter Client
	cAnon := newClient(t, web.URL)

	testRoutes := []struct {
		method string
		path   string
	}{
		{"GET", "/api/admin/overview"},
		{"GET", "/api/admin/users"},
		{"GET", "/api/admin/users/any-id"},
		{"DELETE", "/api/admin/users/any-id/sessions"},
		{"GET", "/api/admin/projects"},
	}

	for _, route := range testRoutes {
		// Nicht angemeldet -> 401
		if status, _ := cAnon.do(route.method, route.path, nil); status != 401 {
			t.Errorf("Anonym %s %s: erwartete 401, bekam %d", route.method, route.path, status)
		}
		// Nicht-Admin -> 403
		if status, _ := cNormal.do(route.method, route.path, nil); status != 403 {
			t.Errorf("Nicht-Admin %s %s: erwartete 403, bekam %d", route.method, route.path, status)
		}
	}
}

func TestAdminNotFound(t *testing.T) {
	_, web, _, _ := setupPanel(t)
	cAdmin := newClient(t, web.URL)
	cAdmin.expect(201, "POST", "/api/auth/setup", map[string]string{"username": "admin", "password": "ein-gutes-passwort"})

	cAdmin.expect(404, "GET", "/api/admin/users/non-existent-id", nil)
	cAdmin.expect(404, "DELETE", "/api/admin/users/non-existent-id/sessions", nil)
}

func TestAdminEndpoints(t *testing.T) {
	p, web, _, nodeID := setupPanel(t)

	// Admin einrichten
	cAdmin := newClient(t, web.URL)
	adminResp := cAdmin.expect(201, "POST", "/api/auth/setup", map[string]string{"username": "admin", "password": "ein-gutes-passwort"})
	var adminUser User
	_ = json.Unmarshal(adminResp, &adminUser)

	// Weiteren normalen Benutzer anlegen
	normalUserResp := cAdmin.expect(201, "POST", "/api/users", map[string]any{"username": "bob", "password": "bob-password-123", "isAdmin": false})
	var normalUser User
	_ = json.Unmarshal(normalUserResp, &normalUser)

	// Normaler Benutzer meldet sich an (damit hat er eine Sitzung)
	cNormal := newClient(t, web.URL)
	cNormal.expect(200, "POST", "/api/auth/login", map[string]string{"username": "bob", "password": "bob-password-123"})

	// Deaktivierten Benutzer anlegen
	disabledUserResp := cAdmin.expect(201, "POST", "/api/users", map[string]any{"username": "clara", "password": "clara-password-123", "isAdmin": false})
	var disabledUser User
	_ = json.Unmarshal(disabledUserResp, &disabledUser)
	cAdmin.expect(200, "PATCH", "/api/users/"+disabledUser.ID, map[string]any{"disabled": true})

	// Projekte erstellen:
	// Projekt 1 (durch Normaluser): "alpha-net" (kind: network)
	p1Resp := cNormal.expect(201, "POST", "/api/projects", map[string]string{"name": "alpha-net", "kind": "network"})
	var p1 Project
	_ = json.Unmarshal(p1Resp, &p1)

	// Projekt 2 (durch Admin): "Beta-Server" (kind: server)
	p2Resp := cAdmin.expect(201, "POST", "/api/projects", map[string]string{"name": "Beta-Server", "kind": "server"})
	var p2 Project
	_ = json.Unmarshal(p2Resp, &p2)

	// Server in Projekt 2 anlegen
	cAdmin.expect(201, "POST", "/api/projects/"+p2.ID+"/servers", map[string]any{
		"name":     "Survival-1",
		"nodeId":   nodeID,
		"software": "paper",
	})

	// Normaluser zu Projekt 2 als Operator hinzufügen
	cAdmin.expect(200, "PUT", "/api/projects/"+p2.ID+"/members/"+normalUser.ID, map[string]string{"role": "operator"})

	// Verwaistes Projekt direkt in DB einfügen (Eigentümer existiert nicht in users)
	_, err := p.store.db.Exec("INSERT INTO projects (id, name, kind, description, secret, owner_id, created_at) VALUES (?, ?, ?, ?, ?, ?, ?)",
		"orphan-id", "Zeta-Orphan", "server", "Ohne Besitzer", "topsecret", "nonexistent-owner", 1000)
	if err != nil {
		t.Fatalf("Verwaistes Projekt anlegen fehlgeschlagen: %v", err)
	}

	// -------------------------------------------------------------
	// 1. GET /api/admin/overview
	// -------------------------------------------------------------
	overviewData := cAdmin.expect(200, "GET", "/api/admin/overview", nil)
	var overview AdminOverview
	if err := json.Unmarshal(overviewData, &overview); err != nil {
		t.Fatalf("Overview unmarshal: %v", err)
	}

	if overview.Users != 3 {
		t.Errorf("overview.Users = %d, erwartet 3", overview.Users)
	}
	if overview.Admins != 1 {
		t.Errorf("overview.Admins = %d, erwartet 1", overview.Admins)
	}
	if overview.DisabledUsers != 1 {
		t.Errorf("overview.DisabledUsers = %d, erwartet 1", overview.DisabledUsers)
	}
	if overview.Projects != 3 {
		t.Errorf("overview.Projects = %d, erwartet 3", overview.Projects)
	}
	if overview.Servers != 1 {
		t.Errorf("overview.Servers = %d, erwartet 1", overview.Servers)
	}
	if overview.Nodes != 1 {
		t.Errorf("overview.Nodes = %d, erwartet 1", overview.Nodes)
	}
	// members: p1 hat bob (owner), p2 hat admin (owner) + bob (operator) = 3
	if overview.Members != 3 {
		t.Errorf("overview.Members = %d, erwartet 3", overview.Members)
	}
	// sessions: admin (1) + bob (1) = 2
	if overview.Sessions != 2 {
		t.Errorf("overview.Sessions = %d, erwartet 2", overview.Sessions)
	}

	// -------------------------------------------------------------
	// 2. GET /api/admin/users
	// -------------------------------------------------------------
	usersData := cAdmin.expect(200, "GET", "/api/admin/users", nil)
	var adminUsers []AdminUser
	if err := json.Unmarshal(usersData, &adminUsers); err != nil {
		t.Fatalf("Users unmarshal: %v", err)
	}

	if len(adminUsers) != 3 {
		t.Fatalf("Anzahl Benutzer: %d, erwartet 3", len(adminUsers))
	}

	// Sortierung prüfen: "admin", "bob", "clara"
	if adminUsers[0].Username != "admin" || adminUsers[1].Username != "bob" || adminUsers[2].Username != "clara" {
		t.Errorf("Benutzer nicht alphabetisch sortiert: %s, %s, %s",
			adminUsers[0].Username, adminUsers[1].Username, adminUsers[2].Username)
	}

	// Bob prüfen: projectCount = 2 (alpha-net + Beta-Server), sessionCount = 1
	bobItem := adminUsers[1]
	if bobItem.ProjectCount != 2 {
		t.Errorf("Bob projectCount = %d, erwartet 2", bobItem.ProjectCount)
	}
	if bobItem.SessionCount != 1 {
		t.Errorf("Bob sessionCount = %d, erwartet 1", bobItem.SessionCount)
	}
	if bobItem.TotpEnabled {
		t.Errorf("Bob TotpEnabled sollte false sein")
	}

	// Admin prüfen: projectCount = 1 (Beta-Server), sessionCount = 1
	adminItem := adminUsers[0]
	if adminItem.ProjectCount != 1 {
		t.Errorf("Admin projectCount = %d, erwartet 1", adminItem.ProjectCount)
	}
	if adminItem.SessionCount != 1 {
		t.Errorf("Admin sessionCount = %d, erwartet 1", adminItem.SessionCount)
	}

	// Keine Geheimnisse in Antwort
	rawUsersStr := string(usersData)
	if strings.Contains(rawUsersStr, "password") || strings.Contains(rawUsersStr, "secret") {
		t.Errorf("Geheimnisse in GET /api/admin/users gefunden: %s", rawUsersStr)
	}

	// -------------------------------------------------------------
	// 3. GET /api/admin/users/{id}
	// -------------------------------------------------------------
	bobDetailData := cAdmin.expect(200, "GET", "/api/admin/users/"+normalUser.ID, nil)
	var bobDetail AdminUserDetail
	if err := json.Unmarshal(bobDetailData, &bobDetail); err != nil {
		t.Fatalf("UserDetail unmarshal: %v", err)
	}

	if bobDetail.User.Username != "bob" || bobDetail.User.ProjectCount != 2 {
		t.Errorf("Falsche Benutzerdaten in UserDetail: %+v", bobDetail.User)
	}
	if len(bobDetail.Projects) != 2 {
		t.Fatalf("Bob Projekte Anzahl: %d, erwartet 2", len(bobDetail.Projects))
	}
	// Sortiert nach Name COLLATE NOCASE: "alpha-net", "Beta-Server"
	if bobDetail.Projects[0].Name != "alpha-net" || bobDetail.Projects[0].Role != "owner" || bobDetail.Projects[0].Kind != "network" {
		t.Errorf("Projekt 0 unerwartet: %+v", bobDetail.Projects[0])
	}
	if bobDetail.Projects[1].Name != "Beta-Server" || bobDetail.Projects[1].Role != "operator" || bobDetail.Projects[1].Kind != "server" {
		t.Errorf("Projekt 1 unerwartet: %+v", bobDetail.Projects[1])
	}

	// Benutzer ohne Projekte liefert leere Liste (nicht null)
	claraDetailData := cAdmin.expect(200, "GET", "/api/admin/users/"+disabledUser.ID, nil)
	var claraDetail AdminUserDetail
	if err := json.Unmarshal(claraDetailData, &claraDetail); err != nil {
		t.Fatalf("Clara unmarshal: %v", err)
	}
	if claraDetail.Projects == nil || len(claraDetail.Projects) != 0 {
		t.Errorf("Clara Projekte sollten leeres Slice sein, bekam: %+v", claraDetail.Projects)
	}
	if !strings.Contains(string(claraDetailData), `"projects":[]`) {
		t.Errorf("projects sollte in JSON als [] serialisiert werden: %s", string(claraDetailData))
	}

	// -------------------------------------------------------------
	// 4. GET /api/admin/projects
	// -------------------------------------------------------------
	projectsData := cAdmin.expect(200, "GET", "/api/admin/projects", nil)
	var adminProjects []AdminProject
	if err := json.Unmarshal(projectsData, &adminProjects); err != nil {
		t.Fatalf("Projects unmarshal: %v", err)
	}

	if len(adminProjects) != 3 {
		t.Fatalf("Anzahl Admin-Projekte: %d, erwartet 3", len(adminProjects))
	}

	// Sortiert nach Name COLLATE NOCASE: "alpha-net", "Beta-Server", "Zeta-Orphan"
	if adminProjects[0].Name != "alpha-net" || adminProjects[1].Name != "Beta-Server" || adminProjects[2].Name != "Zeta-Orphan" {
		t.Errorf("Projekte nicht sortiert: %s, %s, %s",
			adminProjects[0].Name, adminProjects[1].Name, adminProjects[2].Name)
	}

	// alpha-net: ownerName = "bob", memberCount = 1, serverCount = 0
	if adminProjects[0].OwnerName != "bob" || adminProjects[0].MemberCount != 1 || adminProjects[0].ServerCount != 0 {
		t.Errorf("alpha-net Werte unerwartet: %+v", adminProjects[0])
	}

	// Beta-Server: ownerName = "admin", memberCount = 2, serverCount = 1
	if adminProjects[1].OwnerName != "admin" || adminProjects[1].MemberCount != 2 || adminProjects[1].ServerCount != 1 {
		t.Errorf("Beta-Server Werte unerwartet: %+v", adminProjects[1])
	}

	// Zeta-Orphan: ownerName = "" (Eigentümer gelöscht / existiert nicht)
	if adminProjects[2].OwnerName != "" {
		t.Errorf("Zeta-Orphan ownerName sollte leer sein, bekam: %q", adminProjects[2].OwnerName)
	}

	// Keine Geheimnisse (secret) in der Antwort
	if strings.Contains(string(projectsData), "topsecret") {
		t.Errorf("Projekt-Secret in Antwort gefunden: %s", string(projectsData))
	}

	// -------------------------------------------------------------
	// 5. DELETE /api/admin/users/{id}/sessions
	// -------------------------------------------------------------
	// Vorher: Bob kann /api/me abfragen
	cNormal.expect(200, "GET", "/api/me", nil)

	// Admin beendet Bobs Sitzungen
	cAdmin.expect(204, "DELETE", "/api/admin/users/"+normalUser.ID+"/sessions", nil)

	// Bobs Sitzung ist jetzt ungültig (401)
	if status, _ := cNormal.do("GET", "/api/me", nil); status != 401 {
		t.Errorf("Bob nach Sitzungsbeendigung: erwartete 401, bekam %d", status)
	}

	// Zähler für Bob ist 0
	bobDetailAfter := cAdmin.expect(200, "GET", "/api/admin/users/"+normalUser.ID, nil)
	_ = json.Unmarshal(bobDetailAfter, &bobDetail)
	if bobDetail.User.SessionCount != 0 {
		t.Errorf("Bobs sessionCount nach Beendigung = %d, erwartet 0", bobDetail.User.SessionCount)
	}

	// Eigene Sitzung beenden ist erlaubt (Admin wird abgemeldet)
	cAdmin.expect(204, "DELETE", "/api/admin/users/"+adminUser.ID+"/sessions", nil)
	if status, _ := cAdmin.do("GET", "/api/me", nil); status != 401 {
		t.Errorf("Admin nach Beendigung eigener Sitzungen: erwartete 401, bekam %d", status)
	}
}

func TestAdminEmptyProjects(t *testing.T) {
	_, web, _, _ := setupPanel(t)
	cAdmin := newClient(t, web.URL)
	cAdmin.expect(201, "POST", "/api/auth/setup", map[string]string{"username": "admin", "password": "ein-gutes-passwort"})

	// Wenn keine Projekte existieren: Liste muss [] sein, nie null
	data := cAdmin.expect(200, "GET", "/api/admin/projects", nil)
	if !strings.HasPrefix(strings.TrimSpace(string(data)), "[") {
		t.Fatalf("Leere Projektliste sollte JSON Array sein: %s", string(data))
	}
	var list []AdminProject
	if err := json.Unmarshal(data, &list); err != nil {
		t.Fatalf("Unmarshal leere Projekte: %v", err)
	}
	if list == nil {
		t.Fatal("Projektliste darf nicht nil sein")
	}
}
