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

const devSHA = "abc1234def5678abc1234def5678abc1234def56"

// devGitHub startet ein falsches GitHub für den Dev-Kanal: Es meldet den Lauf mit dem Commit sha (leer: noch kein Lauf).
func devGitHub(t *testing.T, sha *atomic.Value) {
	t.Helper()
	github := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("branch") != "main" || r.URL.Query().Get("status") != "success" {
			http.Error(w, "falsche Abfrage", http.StatusBadRequest)
			return
		}
		runs := []map[string]any{}
		if value, _ := sha.Load().(string); value != "" {
			runs = append(runs, map[string]any{
				"head_sha":    value,
				"updated_at":  "2026-10-30T22:45:27Z",
				"head_commit": map[string]string{"message": "Neue Funktion\n\nLanger Text"},
			})
		}
		writeJSON(w, http.StatusOK, map[string]any{"workflow_runs": runs})
	}))
	t.Cleanup(github.Close)
	t.Setenv("LODESTONE_DEV_UPDATE_URL", github.URL+"?branch=main&event=push&status=success&per_page=1")
}

func TestNeedsUpdateByChannel(t *testing.T) {
	for _, c := range []struct {
		channel         string
		explicit        bool
		target, current string
		want            bool
	}{
		{channelStable, false, "0.3.1", "0.3.0", true},
		{channelStable, false, "0.3.0", "0.3.0", false},
		{channelStable, false, "0.3.0", "0.4.0", false}, // ein neueres Release wird nicht zurückgesetzt
		{channelStable, false, "0.3.0", "dev", false},   // selbst gebaut und nie gewählt: in Ruhe lassen
		{channelStable, true, "0.3.0", "dev", true},     // ausdrücklich gewählt: darf wechseln
		{channelStable, true, "0.3.0", "dev-abc1234", true},
		{channelStable, false, "0.3.0", "dev-abc1234", true},
		{channelDev, true, "dev-def5678", "dev-abc1234", true},
		{channelDev, true, "dev-abc1234", "dev-abc1234", false},
		{channelDev, true, "dev-abc1234", "0.3.0", true},
		{channelDev, true, "dev-abc1234", "dev", true},
		{channelDev, false, "dev-abc1234", "dev", false},
		{channelDev, true, "", "0.3.0", false},
		{channelDev, true, "dev-abc1234", "", false},
	} {
		if got := needsUpdate(c.channel, c.explicit, c.target, c.current); got != c.want {
			t.Errorf("needsUpdate(%s, explicit=%v, %q, %q) = %v, erwartet %v", c.channel, c.explicit, c.target, c.current, got, c.want)
		}
	}
}

func TestUpdateChannelDefaultFollowsRunningVersion(t *testing.T) {
	p, admin, _, _, _, _ := updateSetup(t)
	if channel, explicit := p.updateChannel(); channel != channelStable || explicit {
		t.Fatalf("ein Release-Panel beginnt im stabilen Kanal: %s %v", channel, explicit)
	}
	p.cfg.Version = "dev-abc1234"
	if channel, explicit := p.updateChannel(); channel != channelDev || explicit {
		t.Fatalf("ein Dev-Build bleibt im Dev-Kanal: %s %v", channel, explicit)
	}
	admin.expect(200, "PUT", "/api/updates/settings", map[string]string{"channel": "stable"})
	if channel, explicit := p.updateChannel(); channel != channelStable || !explicit {
		t.Fatalf("die ausdrückliche Wahl gewinnt: %s %v", channel, explicit)
	}
}

func TestChannelSettingValidatesAndSwitchesImmediately(t *testing.T) {
	p, admin, _, _, _, _ := updateSetup(t)
	sha := &atomic.Value{}
	sha.Store(devSHA)
	devGitHub(t, sha)

	admin.expect(400, "PUT", "/api/updates/settings", map[string]string{"channel": "nightly"})
	admin.expect(400, "PUT", "/api/updates/settings", map[string]string{})
	admin.expect(400, "PUT", "/api/updates/settings", map[string]any{"channel": "dev", "unbekannt": 1})
	if channel, explicit := p.updateChannel(); channel != channelStable || explicit {
		t.Fatalf("Ungültiges darf nichts ändern: %s %v", channel, explicit)
	}

	var status UpdateStatus
	_ = json.Unmarshal(admin.expect(200, "GET", "/api/updates", nil), &status)
	if status.Channel != "stable" || status.LatestVersion != "0.3.0" {
		t.Fatalf("stabiler Kanal: %+v", status)
	}
	var saved struct {
		AutoUpdate bool   `json:"autoUpdate"`
		Channel    string `json:"channel"`
	}
	_ = json.Unmarshal(admin.expect(200, "PUT", "/api/updates/settings", map[string]string{"channel": "dev"}), &saved)
	if saved.Channel != "dev" || saved.AutoUpdate {
		t.Fatalf("Antwort: %+v", saved)
	}
	// Ohne neue Abfrage von Hand zeigt der Status sofort den Stand des neuen Kanals (die alte Version wird nicht wiederverwendet)
	_ = json.Unmarshal(admin.expect(200, "GET", "/api/updates", nil), &status)
	if status.Channel != "dev" || status.LatestVersion != "dev-abc1234" || status.ReleaseTitle != "Neue Funktion" ||
		status.ReleaseURL != "https://github.com/Henny263462/lodestone/commit/"+devSHA || !status.PanelUpdateAvailable {
		t.Fatalf("Dev-Kanal: %+v", status)
	}
	// Die Auto-Update-Wahl bleibt beim Kanalwechsel erhalten
	admin.expect(200, "PUT", "/api/updates/settings", map[string]bool{"autoUpdate": true})
	if channel, _ := p.updateChannel(); channel != channelDev || !p.autoUpdateEnabled() {
		t.Fatal("Kanal und Auto-Update müssen unabhängig voneinander gelten")
	}
}

func TestDevChannelInstallsBuildAndStopsWhenCurrent(t *testing.T) {
	p, admin, fake, nodeID, _, _ := updateSetup(t)
	sha := &atomic.Value{}
	sha.Store(devSHA)
	devGitHub(t, sha)
	p.cfg.Version = "0.3.0"
	fake.setVersion("0.3.0")
	admin.expect(200, "PUT", "/api/updates/settings", map[string]string{"channel": "dev"})

	admin.expect(200, "POST", "/api/nodes/"+nodeID+"/update", nil)
	admin.expect(200, "POST", "/api/panel/update", nil)
	if got, want := fake.updateLog(), []string{"node:dev-abc1234", "panel:dev-abc1234"}; !slices.Equal(got, want) {
		t.Fatalf("Aufträge an den Node = %v, erwartet %v", got, want)
	}

	// Läuft schon genau dieser Stand, ist nichts zu tun
	p.cfg.Version = "dev-abc1234"
	fake.setVersion("dev-abc1234")
	admin.expect(409, "POST", "/api/panel/update", nil)
	admin.expect(409, "POST", "/api/nodes/"+nodeID+"/update", nil)
	var status UpdateStatus
	_ = json.Unmarshal(admin.expect(200, "POST", "/api/updates/check", nil), &status)
	if status.UpdateAvailable {
		t.Fatalf("aktueller Dev-Build, aber Update gemeldet: %+v", status)
	}

	// Ein neuer Commit wird gebaut: wieder ein Update
	sha.Store("fedcba9876543210fedcba9876543210fedcba98")
	_ = json.Unmarshal(admin.expect(200, "POST", "/api/updates/check", nil), &status)
	if !status.PanelUpdateAvailable || status.LatestVersion != "dev-fedcba9" {
		t.Fatalf("neuer Dev-Build wird nicht angeboten: %+v", status)
	}
}

func TestSwitchFromDevBackToStable(t *testing.T) {
	p, admin, fake, _, _, _ := updateSetup(t)
	p.cfg.Version = "dev-abc1234"
	fake.setVersion("dev-abc1234")
	admin.expect(200, "PUT", "/api/updates/settings", map[string]string{"channel": "stable"})
	var status UpdateStatus
	_ = json.Unmarshal(admin.expect(200, "GET", "/api/updates", nil), &status)
	if status.Channel != "stable" || !status.PanelUpdateAvailable || status.LatestVersion != "0.3.0" || !status.Nodes[0].UpdateAvailable {
		t.Fatalf("Der Wechsel zurück zu Stabil wird nicht angeboten: %+v", status)
	}
	admin.expect(200, "POST", "/api/panel/update", nil)
	if got := fake.updateLog(); !slices.Equal(got, []string{"panel:0.3.0"}) {
		t.Fatalf("Aufträge an den Node = %v", got)
	}
}

func TestDevChannelHandlesMissingAndBrokenBuilds(t *testing.T) {
	p, admin, _, _, _, _ := updateSetup(t)
	sha := &atomic.Value{}
	devGitHub(t, sha)
	admin.expect(200, "PUT", "/api/updates/settings", map[string]string{"channel": "dev"})

	// Ein Hash, der nicht wie ein Commit aussieht, wird nie zum Image-Tag
	sha.Store("../../evil")
	var status UpdateStatus
	_ = json.Unmarshal(admin.expect(200, "POST", "/api/updates/check", nil), &status)
	if !status.CheckFailed || status.UpdateAvailable {
		t.Fatalf("ungültiger Hash: %+v", status)
	}
	if _, err := p.fetchDevBuild(context.Background()); err == nil {
		t.Fatal("ein ungültiger Commit-Hash muss abgelehnt werden")
	}

	// Noch kein erfolgreicher Lauf: nichts anzubieten, aber kein Fehler
	sha.Store("")
	_ = json.Unmarshal(admin.expect(200, "POST", "/api/updates/check", nil), &status)
	if status.CheckFailed || status.LatestVersion != "" || status.UpdateAvailable {
		t.Fatalf("ohne Lauf: %+v", status)
	}
	admin.expect(502, "POST", "/api/panel/update", nil)
}

func TestIsCommitHash(t *testing.T) {
	for hash, want := range map[string]bool{devSHA: true, "": false, "abc1234": false, "ABC1234DEF5678ABC1234DEF5678ABC1234DEF56": false,
		"abc1234def5678abc1234def5678abc1234def5g": false} {
		if got := isCommitHash(hash); got != want {
			t.Errorf("isCommitHash(%q) = %v, erwartet %v", hash, got, want)
		}
	}
}
