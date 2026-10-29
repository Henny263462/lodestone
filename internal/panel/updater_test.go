package panel

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"slices"
	"sync/atomic"
	"testing"
)

// updateSetup startet ein Panel mit einem Node, der „0.1.0“ meldet, und einem falschen GitHub, dessen neuestes Release
// release.Store(...) bestimmt (leer: GitHub antwortet mit einem Fehler).
func updateSetup(t *testing.T) (p *Panel, admin *client, fake *fakeNode, nodeID, projectID string, release *atomic.Value) {
	p, admin, fake, nodeID, _, projectID = serverProject(t)
	p.cfg.Version = "0.1.0"
	fake.setVersion("0.1.0")
	release = &atomic.Value{}
	release.Store("v0.3.0")
	github := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		tag, _ := release.Load().(string)
		if tag == "" {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"message": "kaputt"})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"tag_name":     tag,
			"name":         "Version " + tag,
			"html_url":     "https://github.com/Henny263462/lodestone/releases/tag/" + tag,
			"published_at": "2026-10-29T20:00:00Z",
			"body":         "Neu: Auto-Update",
		})
	}))
	t.Cleanup(github.Close)
	t.Setenv("LODESTONE_UPDATE_URL", github.URL)
	return
}

func countEvents(t *testing.T, p *Panel, projectID, kind string) int {
	t.Helper()
	events, err := p.store.ListEvents(projectID, 100)
	if err != nil {
		t.Fatal(err)
	}
	count := 0
	for _, event := range events {
		if event.Kind == kind {
			count++
		}
	}
	return count
}

func TestUpdateRoutesNeedAdmin(t *testing.T) {
	p, admin, fake, nodeID, _, _ := updateSetup(t)
	hash, _ := HashPassword("geheim1234")
	if _, err := p.store.CreateUser("normalo", hash, false); err != nil {
		t.Fatal(err)
	}
	user := newClient(t, admin.base)
	user.expect(200, "POST", "/api/auth/login", map[string]string{"username": "normalo", "password": "geheim1234"})
	anonymous := newClient(t, admin.base)
	for _, route := range [][2]string{
		{"GET", "/api/updates"}, {"POST", "/api/updates/check"}, {"PUT", "/api/updates/settings"},
		{"POST", "/api/panel/update"}, {"POST", "/api/nodes/" + nodeID + "/update"}, {"POST", "/api/nodes/update-all"},
	} {
		var body any
		if route[0] == "PUT" {
			body = map[string]bool{"autoUpdate": true}
		}
		user.expect(403, route[0], route[1], body)
		anonymous.expect(401, route[0], route[1], body)
	}
	if log := fake.updateLog(); len(log) != 0 {
		t.Fatalf("Ohne Rechte darf nichts aktualisiert werden: %v", log)
	}
}

func TestUpdateStatusAndInstall(t *testing.T) {
	_, admin, fake, nodeID, _, _ := updateSetup(t)

	var status UpdateStatus
	_ = json.Unmarshal(admin.expect(200, "POST", "/api/updates/check", nil), &status)
	if !status.UpdateAvailable || !status.PanelUpdateAvailable || status.LatestVersion != "0.3.0" || status.PanelVersion != "0.1.0" || status.CheckFailed || status.LastChecked == 0 {
		t.Fatalf("Status unerwartet: %+v", status)
	}
	if len(status.Nodes) != 1 || !status.Nodes[0].UpdateAvailable || !status.Nodes[0].Online || status.Nodes[0].Version != "0.1.0" {
		t.Fatalf("Node-Status unerwartet: %+v", status.Nodes)
	}

	var nodes []nodeDTO
	_ = json.Unmarshal(admin.expect(200, "GET", "/api/nodes", nil), &nodes)
	if len(nodes) != 1 || !nodes[0].UpdateAvailable {
		t.Fatalf("Die Nodes-Liste kennt das Update nicht: %+v", nodes)
	}

	admin.expect(200, "POST", "/api/nodes/"+nodeID+"/update", nil)
	admin.expect(200, "POST", "/api/nodes/update-all", nil)
	admin.expect(200, "POST", "/api/panel/update", nil)
	if got, want := fake.updateLog(), []string{"node:0.3.0", "node:0.3.0", "panel:0.3.0"}; !slices.Equal(got, want) {
		t.Fatalf("Aufträge an den Node = %v, erwartet %v", got, want)
	}
}

func TestUpdateRefusedWhenAlreadyCurrent(t *testing.T) {
	p, admin, fake, nodeID, _, _ := updateSetup(t)
	p.cfg.Version = "0.3.0"
	fake.setVersion("0.3.0")

	var status UpdateStatus
	_ = json.Unmarshal(admin.expect(200, "POST", "/api/updates/check", nil), &status)
	if status.UpdateAvailable || status.PanelUpdateAvailable {
		t.Fatalf("Es ist alles aktuell, der Status sagt: %+v", status)
	}
	admin.expect(409, "POST", "/api/panel/update", nil)
	admin.expect(409, "POST", "/api/nodes/"+nodeID+"/update", nil)
	admin.expect(409, "POST", "/api/nodes/update-all", nil)
	admin.expect(404, "POST", "/api/nodes/gibt-es-nicht/update", nil)
	if log := fake.updateLog(); len(log) != 0 {
		t.Fatalf("Aktuelles darf nicht neu gestartet werden: %v", log)
	}
}

func TestUpdateWithoutReleaseInformation(t *testing.T) {
	_, admin, fake, nodeID, _, release := updateSetup(t)
	release.Store("")

	var status UpdateStatus
	_ = json.Unmarshal(admin.expect(200, "POST", "/api/updates/check", nil), &status)
	if !status.CheckFailed || status.UpdateAvailable {
		t.Fatalf("Ohne GitHub muss der Status die fehlgeschlagene Prüfung melden: %+v", status)
	}
	admin.expect(502, "POST", "/api/panel/update", nil)
	admin.expect(502, "POST", "/api/nodes/"+nodeID+"/update", nil)
	admin.expect(502, "POST", "/api/nodes/update-all", nil)
	if log := fake.updateLog(); len(log) != 0 {
		t.Fatalf("Ohne bekannte Version darf nichts aktualisiert werden: %v", log)
	}

	// Ein früher gefundenes Release bleibt gültig, wenn GitHub später ausfällt.
	release.Store("v0.3.0")
	admin.expect(200, "POST", "/api/updates/check", nil)
	release.Store("")
	_ = json.Unmarshal(admin.expect(200, "POST", "/api/updates/check", nil), &status)
	if status.CheckFailed || status.LatestVersion != "0.3.0" {
		t.Fatalf("Der letzte bekannte Stand ging verloren: %+v", status)
	}
}

func TestUpdateFailuresAreReported(t *testing.T) {
	_, admin, fake, nodeID, _, _ := updateSetup(t)
	fake.mu.Lock()
	fake.updateFails = true
	fake.mu.Unlock()
	admin.expect(502, "POST", "/api/nodes/"+nodeID+"/update", nil)
	admin.expect(502, "POST", "/api/nodes/update-all", nil)
	admin.expect(502, "POST", "/api/panel/update", nil)
}

func TestOnlyOneUpdateAtATime(t *testing.T) {
	p, admin, fake, nodeID, _, _ := updateSetup(t)
	p.updateMu.Lock()
	admin.expect(409, "POST", "/api/panel/update", nil)
	admin.expect(409, "POST", "/api/nodes/"+nodeID+"/update", nil)
	admin.expect(409, "POST", "/api/nodes/update-all", nil)
	p.updateMu.Unlock()
	if log := fake.updateLog(); len(log) != 0 {
		t.Fatalf("Während eines Updates darf kein zweites starten: %v", log)
	}
	admin.expect(200, "POST", "/api/panel/update", nil)
}

func TestAutoUpdateSettingIsStored(t *testing.T) {
	p, admin, _, _, _, _ := updateSetup(t)
	if p.autoUpdateEnabled() {
		t.Fatal("Auto-Update muss standardmäßig aus sein")
	}
	admin.expect(200, "PUT", "/api/updates/settings", map[string]bool{"autoUpdate": true})
	var status UpdateStatus
	_ = json.Unmarshal(admin.expect(200, "GET", "/api/updates", nil), &status)
	if !p.autoUpdateEnabled() || !status.AutoUpdate {
		t.Fatalf("Einstellung nicht gespeichert: %+v", status)
	}
	admin.expect(400, "PUT", "/api/updates/settings", map[string]any{"autoUpdate": true, "unbekannt": 1})
	admin.expect(200, "PUT", "/api/updates/settings", map[string]bool{"autoUpdate": false})
	if p.autoUpdateEnabled() {
		t.Fatal("Auto-Update ließ sich nicht ausschalten")
	}
}

func TestBackgroundCheckOnlyNotifiesWithoutAutoUpdate(t *testing.T) {
	p, _, fake, _, projectID, _ := updateSetup(t)
	ctx := context.Background()
	p.checkAndAutoUpdate(ctx)
	p.checkAndAutoUpdate(ctx)
	if n := countEvents(t, p, projectID, "update_available"); n != 1 {
		t.Fatalf("Der Hinweis muss genau einmal je Version erscheinen, erschien %d-mal", n)
	}
	if log := fake.updateLog(); len(log) != 0 {
		t.Fatalf("Ohne Auto-Update darf nichts installiert werden: %v", log)
	}
}

func TestBackgroundAutoUpdateInstallsOnce(t *testing.T) {
	p, admin, fake, _, projectID, _ := updateSetup(t)
	admin.expect(200, "PUT", "/api/updates/settings", map[string]bool{"autoUpdate": true})
	ctx := context.Background()

	p.checkAndAutoUpdate(ctx)
	if got, want := fake.updateLog(), []string{"node:0.3.0", "panel:0.3.0"}; !slices.Equal(got, want) {
		t.Fatalf("Aufträge an den Node = %v, erwartet %v", got, want)
	}
	if countEvents(t, p, projectID, "update_started") != 1 {
		t.Fatal("Das gestartete Update fehlt im Verlauf")
	}

	// Nach dem Neustart melden beide die neue Version: Es gibt nichts mehr zu tun.
	p.cfg.Version = "0.3.0"
	fake.setVersion("0.3.0")
	p.checkAndAutoUpdate(ctx)
	if n := len(fake.updateLog()); n != 2 {
		t.Fatalf("Aktuelles wurde noch einmal aktualisiert (%d Aufträge)", n)
	}
}

func TestBackgroundAutoUpdateSkipsPanelWhenNodesFail(t *testing.T) {
	p, admin, fake, _, projectID, _ := updateSetup(t)
	admin.expect(200, "PUT", "/api/updates/settings", map[string]bool{"autoUpdate": true})
	fake.mu.Lock()
	fake.updateFails = true
	fake.mu.Unlock()
	p.checkAndAutoUpdate(context.Background())
	if countEvents(t, p, projectID, "update_started") != 0 {
		t.Fatal("Ein gescheitertes Update darf nicht als gestartet gemeldet werden")
	}
}

func TestIsNewer(t *testing.T) {
	for _, c := range []struct {
		latest, current string
		want            bool
	}{
		{"0.3.0", "0.1.0", true},
		{"v0.3.0", "0.2.9", true},
		{"0.10.0", "0.9.0", true},
		{"0.3.0", "0.3.0", false},
		{"v0.3.0", "0.3.0", false},
		{"0.2.0", "0.3.0", false},
		{"0.3.0", "dev", false},
		{"", "0.1.0", false},
		{"0.3.0", "", false},
	} {
		if got := isNewer(c.latest, c.current); got != c.want {
			t.Errorf("isNewer(%q, %q) = %v, erwartet %v", c.latest, c.current, got, c.want)
		}
	}
}

func TestHostsPanel(t *testing.T) {
	for url, want := range map[string]bool{
		"http://node:8480":          true,
		"http://127.0.0.1:8480":     true,
		"http://localhost:8480/":    true,
		"http://192.168.1.20:8480":  false,
		"https://node2.example.org": false,
		"::kaputt::":                false,
	} {
		if got := hostsPanel(Node{URL: url}); got != want {
			t.Errorf("hostsPanel(%q) = %v, erwartet %v", url, got, want)
		}
	}
}

func TestPanelFlagIsIndependentOfNodes(t *testing.T) {
	p, admin, _, _, _, _ := updateSetup(t)
	p.cfg.Version = "0.3.0"
	var status UpdateStatus
	_ = json.Unmarshal(admin.expect(200, "POST", "/api/updates/check", nil), &status)
	if status.PanelUpdateAvailable || !status.UpdateAvailable {
		t.Fatalf("Nur der Node ist veraltet, das Panel nicht: %+v", status)
	}
}

func TestFailedReleaseCheckBacksOff(t *testing.T) {
	p, _, _, _, _, release := updateSetup(t)
	var hits atomic.Int32
	github := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits.Add(1)
		tag, _ := release.Load().(string)
		if tag == "" {
			http.Error(w, "kaputt", http.StatusInternalServerError)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"tag_name": tag})
	}))
	defer github.Close()
	t.Setenv("LODESTONE_UPDATE_URL", github.URL)

	ctx := context.Background()
	if _, err := p.checkRelease(ctx, true); err != nil {
		t.Fatal(err)
	}
	// Mit abgelaufenem Cache und kaputtem GitHub wird einmal gefragt, dann kurz gewartet.
	release.Store("")
	p.releaseMu.Lock()
	p.releaseEntry.at = p.releaseEntry.at.Add(-2 * releaseCacheTTL)
	p.releaseEntry.tried = p.releaseEntry.tried.Add(-2 * releaseCacheTTL)
	p.releaseMu.Unlock()
	before := hits.Load()
	for range 5 {
		info, err := p.checkRelease(ctx, false)
		if err != nil || info.Version != "0.3.0" {
			t.Fatalf("der letzte bekannte Stand fehlt: %+v, %v", info, err)
		}
	}
	if n := hits.Load() - before; n != 1 {
		t.Fatalf("GitHub wurde %d-mal gefragt, erwartet 1", n)
	}
}
