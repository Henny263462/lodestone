package panel

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/lodestone-mc/lodestone/internal/node"
)

// fakeSources spielt Hangar, Spiget, CurseForge, GeyserMC und die Teile von Modrinth nach, die Bausteine brauchen.
type fakeSources struct {
	mu           sync.Mutex
	hangarLatest string // neueste Version von ViaVersion
	curseKeys    []string
}

func newFakeSources(t *testing.T, p *Panel) *fakeSources {
	t.Helper()
	fake := &fakeSources{hangarLatest: "5.0"}
	mux := http.NewServeMux()
	reply := func(w http.ResponseWriter, value any) { writeJSON(w, 200, value) }

	// Hangar
	project := map[string]any{"id": 31, "name": "ViaVersion", "namespace": map[string]string{"owner": "ViaVersion", "slug": "ViaVersion"},
		"stats": map[string]int{"downloads": 900}, "description": "Neue Clients auf alten Servern", "avatarUrl": "https://hangarcdn.papermc.io/avatars/project/31.webp",
		"settings": map[string]any{"tags": []string{"SUPPORTS_FOLIA"}}}
	mux.HandleFunc("GET /hangar/projects", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("platform") != "PAPER" {
			reply(w, map[string]any{"result": []any{}})
			return
		}
		reply(w, map[string]any{"result": []any{project}})
	})
	mux.HandleFunc("GET /hangar/projects/31", func(w http.ResponseWriter, _ *http.Request) { reply(w, project) })
	mux.HandleFunc("GET /hangar/projects/{id}", func(w http.ResponseWriter, _ *http.Request) { writeJSON(w, 404, map[string]string{}) })
	version := func(name, channel string, external bool) map[string]any {
		download := map[string]any{"fileInfo": map[string]any{"name": "ViaVersion-" + name + ".jar", "sha256Hash": strings.Repeat("a", 64)}, "downloadUrl": "https://hangarcdn.papermc.io/ViaVersion-" + name + ".jar"}
		if external {
			download = map[string]any{"fileInfo": nil, "externalUrl": "https://example.org", "downloadUrl": nil}
		}
		return map[string]any{"name": name, "createdAt": "2026-10-01T00:00:00Z", "channel": map[string]string{"name": channel},
			"downloads": map[string]any{"PAPER": download}, "platformDependencies": map[string][]string{"PAPER": {"1.21"}}}
	}
	mux.HandleFunc("GET /hangar/projects/31/versions", func(w http.ResponseWriter, r *http.Request) {
		// wie Hangar: Das Plugin nennt nur 1.21, nicht 1.21.4
		if r.URL.Query().Get("platformVersion") != "1.21" {
			reply(w, map[string]any{"result": []any{}})
			return
		}
		fake.mu.Lock()
		latest := fake.hangarLatest
		fake.mu.Unlock()
		reply(w, map[string]any{"result": []any{version(latest+"-SNAPSHOT", "Snapshot", false), version(latest, "Release", false), version("4.0", "Release", true)}})
	})

	// Spiget
	resources := map[string]map[string]any{
		"100": {"id": 100, "name": "Gutes Plugin", "tag": "kann was", "file": map[string]string{"type": ".jar"}, "testedVersions": []string{"1.20", "1.21"}, "downloads": 50, "version": map[string]int{"id": 7}},
		"200": {"id": 200, "name": "Extern", "external": true, "file": map[string]string{"type": "external"}, "testedVersions": []string{"1.21"}, "version": map[string]int{"id": 8}},
		"300": {"id": 300, "name": "Bezahlt", "premium": true, "file": map[string]string{"type": ".jar"}, "version": map[string]int{"id": 9}},
		"400": {"id": 400, "name": "Uralt", "file": map[string]string{"type": ".jar"}, "testedVersions": []string{"1.8", "1.12"}, "version": map[string]int{"id": 10}},
	}
	mux.HandleFunc("GET /spiget/search/resources/{query}", func(w http.ResponseWriter, r *http.Request) {
		if r.PathValue("query") == "nichts" {
			writeJSON(w, 404, map[string]string{"error": "not found"})
			return
		}
		reply(w, []any{resources["100"], resources["200"], resources["300"], resources["400"]})
	})
	mux.HandleFunc("GET /spiget/resources/{id}", func(w http.ResponseWriter, r *http.Request) {
		if resource, ok := resources[r.PathValue("id")]; ok {
			reply(w, resource)
			return
		}
		writeJSON(w, 404, map[string]string{})
	})
	mux.HandleFunc("GET /spiget/resources/{id}/versions/latest", func(w http.ResponseWriter, r *http.Request) {
		resource := resources[r.PathValue("id")]
		reply(w, map[string]any{"id": resource["version"].(map[string]int)["id"], "name": "2.1", "releaseDate": 1780000000})
	})

	// CurseForge
	mods := map[string]map[string]any{
		"11": {"id": 11, "name": "Freier Mod", "slug": "frei", "summary": "darf", "downloadCount": 1000.0, "classId": 6, "allowModDistribution": true, "links": map[string]string{"websiteUrl": "https://www.curseforge.com/minecraft/mc-mods/frei"}, "authors": []any{map[string]string{"name": "Autorin"}}},
		"22": {"id": 22, "name": "Gesperrter Mod", "slug": "gesperrt", "summary": "darf nicht", "downloadCount": 5.0, "classId": 6, "allowModDistribution": false},
	}
	mux.HandleFunc("GET /curse/", func(w http.ResponseWriter, r *http.Request) {
		fake.mu.Lock()
		fake.curseKeys = append(fake.curseKeys, r.Header.Get("X-Api-Key"))
		fake.mu.Unlock()
		path := strings.TrimPrefix(r.URL.Path, "/curse")
		switch {
		case path == "/mods/search":
			if r.URL.Query().Get("modLoaderType") != "4" || r.URL.Query().Get("classId") != "6" {
				reply(w, map[string]any{"data": []any{}})
				return
			}
			reply(w, map[string]any{"data": []any{mods["11"], mods["22"]}})
		case strings.HasSuffix(path, "/files"):
			id := strings.TrimSuffix(strings.TrimPrefix(path, "/mods/"), "/files")
			download := "https://edge.forgecdn.net/files/1/1/mod-" + id + ".jar"
			if id == "22" {
				download = ""
			}
			reply(w, map[string]any{"data": []any{
				map[string]any{"id": 901, "displayName": "Mod 2.0 (Server-Paket)", "fileName": "server.zip", "releaseType": 1, "isServerPack": true, "isAvailable": true, "downloadUrl": download},
				map[string]any{"id": 900, "displayName": "Mod 1.0", "fileName": "mod-" + id + ".jar", "releaseType": 1, "isAvailable": true, "downloadUrl": download, "gameVersions": []string{"1.21.4", "Fabric"}, "hashes": []any{map[string]any{"value": "abc", "algo": 2}, map[string]any{"value": "sha1-mod", "algo": 1}}},
			}})
		case strings.HasPrefix(path, "/mods/"):
			if mod, ok := mods[strings.TrimPrefix(path, "/mods/")]; ok {
				reply(w, map[string]any{"data": mod})
				return
			}
			writeJSON(w, 404, map[string]string{})
		default:
			writeJSON(w, 404, map[string]string{})
		}
	})

	// GeyserMC
	mux.HandleFunc("GET /geyser/projects/{project}/versions/latest/builds/latest", func(w http.ResponseWriter, r *http.Request) {
		names := map[string]map[string]string{
			"geyser":    {"spigot": "Geyser-Spigot.jar", "velocity": "Geyser-Velocity.jar"},
			"floodgate": {"spigot": "floodgate-spigot.jar", "velocity": "floodgate-velocity.jar"},
		}[r.PathValue("project")]
		downloads := map[string]any{}
		for platform, name := range names {
			downloads[platform] = map[string]string{"name": name, "sha256": strings.Repeat("b", 64)}
		}
		reply(w, map[string]any{"version": "2.12.1", "build": 1306, "downloads": downloads})
	})

	// Modrinth: keine Datei ist bekannt; BlueMap und Floodgate gibt es
	modrinthVersion := func(project, name string, loaders []string) map[string]any {
		return map[string]any{"id": project + "-v1", "project_id": project, "version_number": "1.0", "version_type": "release", "loaders": loaders,
			"files": []any{map[string]any{"url": "https://cdn.modrinth.com/" + name, "filename": name, "primary": true, "hashes": map[string]string{"sha1": "s"}}}}
	}
	mux.HandleFunc("POST /modrinth/version_files", func(w http.ResponseWriter, _ *http.Request) { reply(w, map[string]any{}) })
	mux.HandleFunc("POST /modrinth/version_files/update", func(w http.ResponseWriter, _ *http.Request) { reply(w, map[string]any{}) })
	mux.HandleFunc("GET /modrinth/project/bluemap/version", func(w http.ResponseWriter, _ *http.Request) {
		reply(w, []any{modrinthVersion("bluemap", "bluemap-5.jar", []string{"paper", "fabric"})})
	})
	mux.HandleFunc("GET /modrinth/project/floodgate/version", func(w http.ResponseWriter, _ *http.Request) {
		reply(w, []any{modrinthVersion("floodgate", "floodgate-fabric.jar", []string{"fabric"})})
	})
	mux.HandleFunc("GET /modrinth/project/{id}/version", func(w http.ResponseWriter, _ *http.Request) { reply(w, []any{}) })

	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)
	client := server.Client()
	p.content = &contentSources{
		hangar:     &hangarSource{base: server.URL + "/hangar", http: client},
		spigot:     &spigetSource{base: server.URL + "/spiget", http: client},
		curseforge: &curseForgeSource{base: server.URL + "/curse", http: client},
		geyser:     &geyserDownloads{base: server.URL + "/geyser", http: client},
	}
	p.modrinth = &Modrinth{Base: server.URL + "/modrinth", HTTP: client}
	return fake
}

// memberClient legt einen Benutzer mit Rolle im Projekt an und meldet ihn an.
func memberClient(t *testing.T, p *Panel, admin *client, projectID, name, role string) *client {
	t.Helper()
	admin.expect(201, "POST", "/api/users", map[string]any{"username": name, "password": "ein-gutes-passwort"})
	users, _ := p.store.ListUsers()
	for _, user := range users {
		if user.Username == name {
			admin.expect(200, "PUT", "/api/projects/"+projectID+"/members/"+user.ID, map[string]string{"role": role})
		}
	}
	member := newClient(t, admin.base)
	member.expect(200, "POST", "/api/auth/login", map[string]string{"username": name, "password": "ein-gutes-passwort"})
	return member
}

func pulledFiles(fake *fakeNode) []string {
	fake.mu.Lock()
	defer fake.mu.Unlock()
	return append([]string{}, fake.pulled...)
}

func TestSourcesListDependsOnSoftwareAndKey(t *testing.T) {
	p, admin, _, _, server, _ := serverProject(t)
	newFakeSources(t, p)
	var list []SourceInfo
	_ = json.Unmarshal(admin.expect(200, "GET", "/api/servers/"+server.ID+"/sources", nil), &list)
	got := map[string]SourceInfo{}
	for _, info := range list {
		got[info.ID] = info
	}
	if !got["modrinth"].Available || !got["hangar"].Available || !got["spigot"].Available {
		t.Fatalf("Paper hat Modrinth, Hangar und Spigot: %+v", list)
	}
	if got["curseforge"].Available || !strings.Contains(got["curseforge"].Reason, "Fabric") {
		t.Fatalf("CurseForge gibt es nicht für Paper: %+v", got["curseforge"])
	}
	admin.expect(400, "GET", "/api/servers/"+server.ID+"/sources/curseforge/search?q=x", nil)
	admin.expect(404, "GET", "/api/servers/"+server.ID+"/sources/unbekannt/search?q=x", nil)
}

func TestHangarInstallDetailsAndUpdate(t *testing.T) {
	p, admin, fake, nodeID, server, projectID := serverProject(t)
	sources := newFakeSources(t, p)
	base := "/api/servers/" + server.ID + "/sources/hangar"

	var search struct{ Hits []SourceHit }
	_ = json.Unmarshal(admin.expect(200, "GET", base+"/search?q=via", nil), &search)
	if len(search.Hits) != 1 || search.Hits[0].ID != "31" || search.Hits[0].Link != "https://hangar.papermc.io/ViaVersion/ViaVersion" {
		t.Fatalf("Suche: %+v", search.Hits)
	}
	var detail struct {
		Project  SourceHit
		Versions []SourceVersion
	}
	_ = json.Unmarshal(admin.expect(200, "GET", base+"/project/31", nil), &detail)
	if len(detail.Versions) != 3 || !detail.Versions[1].Downloadable || detail.Versions[2].Downloadable || detail.Versions[2].Reason == "" {
		t.Fatalf("Versionen (externe sind nicht ladbar): %+v", detail.Versions)
	}
	admin.expect(400, "GET", base+"/project/..%2F..", nil)

	// Rechte: Betrachter dürfen suchen, aber nicht installieren
	viewer := memberClient(t, p, admin, projectID, "betrachter", "viewer")
	viewer.expect(200, "GET", base+"/search?q=via", nil)
	viewer.expect(403, "POST", base+"/install", map[string]string{"project": "31"})

	// ohne Version: die neueste Release-Version, nicht der Snapshot
	admin.expect(200, "POST", base+"/install", map[string]string{"project": "31"})
	if pulled := pulledFiles(fake); len(pulled) != 1 || pulled[0] != "plugins/ViaVersion-5.0.jar" {
		t.Fatalf("geladen: %v", pulled)
	}
	admin.expect(409, "POST", base+"/install", map[string]string{"project": "31"})
	admin.expect(409, "POST", base+"/install", map[string]string{"project": "31", "versionId": "4.0"}) // nur extern
	admin.expect(400, "POST", base+"/install", map[string]string{"project": "31", "unbekannt": "x"})

	fake.mu.Lock()
	fake.addons = []node.Addon{{File: "ViaVersion-5.0.jar", Dir: "plugins", Enabled: true, Name: "ViaVersion", Version: "5.0", SHA1: "sha1-via"}}
	fake.mu.Unlock()
	refresh(p, nodeID)
	var details []addonDetail
	_ = json.Unmarshal(admin.expect(200, "GET", "/api/servers/"+server.ID+"/addons/details", nil), &details)
	if len(details) != 1 || !details[0].Known || details[0].Source != "hangar" || details[0].Title != "ViaVersion" || details[0].VersionNumber != "5.0" {
		t.Fatalf("Details: %+v", details)
	}

	var checks []updateEntry
	_ = json.Unmarshal(admin.expect(200, "POST", "/api/servers/"+server.ID+"/addons/check", nil), &checks)
	if len(checks) != 1 || checks[0].UpdateAvailable || !checks[0].Known {
		t.Fatalf("noch aktuell: %+v", checks)
	}
	sources.mu.Lock()
	sources.hangarLatest = "5.1"
	sources.mu.Unlock()
	_ = json.Unmarshal(admin.expect(200, "POST", "/api/servers/"+server.ID+"/addons/check", nil), &checks)
	if len(checks) != 1 || !checks[0].UpdateAvailable || checks[0].Latest != "5.1" || checks[0].Source != "hangar" {
		t.Fatalf("Update erwartet: %+v", checks)
	}
	var results []sourceOutcome
	_ = json.Unmarshal(admin.expect(200, "POST", "/api/servers/"+server.ID+"/addons/update", map[string][]string{"files": {"ViaVersion-5.0.jar"}}), &results)
	if len(results) != 1 || !results[0].Updated || results[0].To != "5.1" {
		t.Fatalf("Update: %+v", results)
	}
	fake.mu.Lock()
	removed := append([]string{}, fake.removed...)
	fake.mu.Unlock()
	if pulled := pulledFiles(fake); pulled[len(pulled)-1] != "plugins/ViaVersion-5.1.jar" || len(removed) != 1 || removed[0] != "plugins/ViaVersion-5.0.jar" {
		t.Fatalf("neue Datei laden, alte löschen: %v / %v", pulled, removed)
	}
	known, _ := p.store.AddonSources(server.ID)
	if _, old := known["plugins/ViaVersion-5.0.jar"]; old || known["plugins/ViaVersion-5.1.jar"].VersionID != "5.1" {
		t.Fatalf("gemerkte Herkunft nach dem Update: %+v", known)
	}

	// Herkunft verschwindet, wenn die Datei weg ist
	fake.mu.Lock()
	fake.addons = nil
	fake.mu.Unlock()
	refresh(p, nodeID)
	admin.expect(200, "GET", "/api/servers/"+server.ID+"/addons/details", nil)
	if known, _ := p.store.AddonSources(server.ID); len(known) != 0 {
		t.Fatalf("veraltete Herkunft: %+v", known)
	}
}

func TestSpigotOnlyLoadsFreeHostedMatchingPlugins(t *testing.T) {
	p, admin, fake, _, server, _ := serverProject(t)
	newFakeSources(t, p)
	base := "/api/servers/" + server.ID + "/sources/spigot"
	var search struct{ Hits []SourceHit }
	_ = json.Unmarshal(admin.expect(200, "GET", base+"/search?q=plugin", nil), &search)
	installable := map[string]bool{}
	for _, hit := range search.Hits {
		installable[hit.ID] = hit.Installable
		if !hit.Installable && hit.Note == "" {
			t.Errorf("nicht ladbar ohne Grund: %+v", hit)
		}
	}
	if !installable["100"] || installable["200"] || installable["300"] || installable["400"] {
		t.Fatalf("ladbar: %v", installable)
	}
	_ = json.Unmarshal(admin.expect(200, "GET", base+"/search?q=nichts", nil), &search)
	if len(search.Hits) != 0 {
		t.Fatalf("ohne Treffer: %+v", search.Hits)
	}
	for _, id := range []string{"200", "300", "400"} {
		admin.expect(409, "POST", base+"/install", map[string]string{"project": id})
	}
	admin.expect(409, "POST", base+"/install", map[string]string{"project": "100", "versionId": "6"}) // ältere Version
	admin.expect(200, "POST", base+"/install", map[string]string{"project": "100"})
	if pulled := pulledFiles(fake); len(pulled) != 1 || pulled[0] != "plugins/Gutes_Plugin-2.1.jar" {
		t.Fatalf("geladen: %v", pulled)
	}
}

func TestCurseForgeNeedsKeyAndRespectsDistribution(t *testing.T) {
	p, web, fake, nodeID := setupPanel(t)
	sources := newFakeSources(t, p)
	admin := newClient(t, web.URL)
	admin.expect(201, "POST", "/api/auth/setup", map[string]string{"username": "admin", "password": "ein-gutes-passwort"})
	var project ProjectDTO
	_ = json.Unmarshal(admin.expect(201, "POST", "/api/projects", map[string]string{"name": "Mods", "kind": "server"}), &project)
	var server ServerDTO
	_ = json.Unmarshal(admin.expect(201, "POST", "/api/projects/"+project.ID+"/servers", serverRequest{Name: "Mods", NodeID: nodeID, Software: "fabric", Version: "1.21.4", EULA: true}), &server)
	base := "/api/servers/" + server.ID + "/sources/curseforge"

	body := admin.expect(400, "GET", base+"/search?q=x", nil)
	if !strings.Contains(string(body), "LODESTONE_CURSEFORGE_KEY") {
		t.Fatalf("ohne Schlüssel muss die Meldung sagen, was fehlt: %s", body)
	}
	p.content.curseforge.key = "geheimer-schluessel"

	var search struct{ Hits []SourceHit }
	_ = json.Unmarshal(admin.expect(200, "GET", base+"/search?q=mod", nil), &search)
	if len(search.Hits) != 2 || !search.Hits[0].Installable || search.Hits[1].Installable || !strings.Contains(search.Hits[1].Note, "curseforge.com") {
		t.Fatalf("Suche: %+v", search.Hits)
	}
	var detail struct{ Versions []SourceVersion }
	_ = json.Unmarshal(admin.expect(200, "GET", base+"/project/22", nil), &detail)
	if len(detail.Versions) != 1 || detail.Versions[0].Downloadable || detail.Versions[0].Reason == "" {
		t.Fatalf("gesperrte Dateien sind sichtbar, aber nicht ladbar (Server-Pakete fehlen): %+v", detail.Versions)
	}
	admin.expect(409, "POST", base+"/install", map[string]string{"project": "22"})
	admin.expect(200, "POST", base+"/install", map[string]string{"project": "11"})
	if pulled := pulledFiles(fake); len(pulled) != 1 || pulled[0] != "mods/mod-11.jar" {
		t.Fatalf("geladen: %v", pulled)
	}
	sources.mu.Lock()
	keys := append([]string{}, sources.curseKeys...)
	sources.mu.Unlock()
	for _, key := range keys {
		if key != "geheimer-schluessel" {
			t.Fatalf("CurseForge bekommt den Schlüssel im Header: %v", keys)
		}
	}
	if strings.Contains(string(admin.expect(200, "GET", base+"/project/11", nil)), "geheimer") {
		t.Fatal("der Schlüssel darf nie in einer Antwort stehen")
	}
}

func TestForgeModpackAndBackendRule(t *testing.T) {
	p, web, fake, nodeID := setupPanel(t)
	p.modrinth = fakeModrinth(t)
	admin := newClient(t, web.URL)
	admin.expect(201, "POST", "/api/auth/setup", map[string]string{"username": "admin", "password": "ein-gutes-passwort"})
	var project ProjectDTO
	_ = json.Unmarshal(admin.expect(201, "POST", "/api/projects", map[string]string{"name": "Forge-Pack", "kind": "server"}), &project)
	var server ServerDTO
	_ = json.Unmarshal(admin.expect(201, "POST", "/api/projects/"+project.ID+"/servers", serverRequest{Name: "Pack", NodeID: nodeID, EULA: true, Modpack: &modpackRef{Project: "mypack", VersionID: "v-forge"}}), &server)
	if server.Software != "forge" || server.View == nil || server.View.Version != "1.20.1" {
		t.Fatalf("ein Forge-Modpack braucht einen Forge-Server: %+v %+v", server.ServerRow, server.View)
	}
	waitUntil(t, "Modpack beim Node", func() bool {
		fake.mu.Lock()
		defer fake.mu.Unlock()
		return len(fake.modpacks) == 1
	})

	// hinter Velocity: gesperrt, mit Begründung
	var network ProjectDTO
	_ = json.Unmarshal(admin.expect(201, "POST", "/api/projects", map[string]string{"name": "Netz", "kind": "network"}), &network)
	admin.expect(201, "POST", "/api/projects/"+network.ID+"/servers", serverRequest{Name: "Proxy", NodeID: nodeID, Software: "velocity", Version: "3.5.1"})
	body := admin.expect(400, "POST", "/api/projects/"+network.ID+"/servers", serverRequest{Name: "Forge", NodeID: nodeID, Software: "forge", Version: "1.20.1", EULA: true})
	if !strings.Contains(string(body), "Forwarding") {
		t.Fatalf("Begründung fehlt: %s", body)
	}
}

func portsOf(fake *fakeNode, id string) []node.PortMapping {
	fake.mu.Lock()
	defer fake.mu.Unlock()
	return append([]node.PortMapping{}, fake.servers[id].Ports...)
}

func TestGeyserOnSingleServer(t *testing.T) {
	p, admin, fake, nodeID, server, projectID := serverProject(t)
	newFakeSources(t, p)
	path := "/api/servers/" + server.ID + "/blocks/geyser"
	operator := memberClient(t, p, admin, projectID, "operator", "operator")
	operator.expect(403, "PUT", path, map[string]bool{"enabled": true})
	operator.expect(200, "GET", "/api/servers/"+server.ID+"/blocks", nil)

	var blocks []BlockDTO
	_ = json.Unmarshal(admin.expect(200, "PUT", path, map[string]bool{"enabled": true}), &blocks)
	if !blocks[0].Enabled || blocks[0].Address != "mc.example.org:19132" {
		t.Fatalf("Geyser: %+v", blocks[0])
	}
	if !hasUDPPort(portsOf(fake, server.ID), bedrockPort) || mappingHost(portsOf(fake, server.ID), bedrockPort, "tcp") != bedrockPort {
		t.Fatalf("Bedrock braucht TCP und UDP auf 19132: %+v", portsOf(fake, server.ID))
	}
	if pulled := pulledFiles(fake); fmt.Sprint(pulled) != "[plugins/Geyser-Spigot.jar plugins/floodgate-spigot.jar]" {
		t.Fatalf("geladen: %v", pulled)
	}
	fake.mu.Lock()
	config := fake.autoconfigs[server.ID]
	fake.mu.Unlock()
	if len(config.Edits) != 0 || len(config.Writes) != 0 {
		t.Fatalf("auf 19132 muss Geyser nichts ändern, Floodgate liest seinen Schlüssel selbst: %+v", config)
	}

	// ein zweiter Server auf demselben Node bekommt den nächsten Port und die passende Einstellung
	var other ProjectDTO
	_ = json.Unmarshal(admin.expect(201, "POST", "/api/projects", map[string]string{"name": "Zweiter", "kind": "server"}), &other)
	var second ServerDTO
	_ = json.Unmarshal(admin.expect(201, "POST", "/api/projects/"+other.ID+"/servers", serverRequest{Name: "Zwei", NodeID: nodeID, Software: "purpur", Version: "1.21.4", EULA: true}), &second)
	refresh(p, nodeID)
	admin.expect(200, "PUT", "/api/servers/"+second.ID+"/blocks/geyser", map[string]bool{"enabled": true})
	fake.mu.Lock()
	config = fake.autoconfigs[second.ID]
	fake.mu.Unlock()
	if len(config.Edits) != 1 || config.Edits[0].File != "plugins/Geyser-Spigot/config.yml" || fmt.Sprint(config.Edits[0].Set[0].Value) != "19133" {
		t.Fatalf("zweiter Geyser: %+v", config)
	}

	// läuft der Server, gehen keine neuen Ports
	fake.mu.Lock()
	view := fake.servers[server.ID]
	view.State = node.StateRunning
	fake.servers[server.ID] = view
	fake.mu.Unlock()
	refresh(p, nodeID)
	admin.expect(409, "PUT", path, map[string]bool{"enabled": false})
	fake.mu.Lock()
	view.State = node.StateOffline
	fake.servers[server.ID] = view
	fake.mu.Unlock()

	_ = json.Unmarshal(admin.expect(200, "PUT", path, map[string]bool{"enabled": false}), &blocks)
	if blocks[0].Enabled || hasUDPPort(portsOf(fake, server.ID), bedrockPort) || len(portsOf(fake, server.ID)) != 1 {
		t.Fatalf("ausgeschaltet: %+v %+v", blocks[0], portsOf(fake, server.ID))
	}
	fake.mu.Lock()
	removed := fmt.Sprint(fake.removed)
	fake.mu.Unlock()
	if removed != "[plugins/Geyser-Spigot.jar plugins/floodgate-spigot.jar]" {
		t.Fatalf("entfernt: %s", removed)
	}
	admin.expect(404, "PUT", "/api/servers/"+server.ID+"/blocks/gibtsnicht", map[string]bool{"enabled": true})
}

func TestBlueMapOnSingleServer(t *testing.T) {
	p, admin, fake, _, server, _ := serverProject(t)
	newFakeSources(t, p)
	var blocks []BlockDTO
	_ = json.Unmarshal(admin.expect(200, "PUT", "/api/servers/"+server.ID+"/blocks/bluemap", map[string]bool{"enabled": true}), &blocks)
	host := mappingHost(portsOf(fake, server.ID), blueMapPort, "tcp")
	if host == 0 || !blocks[1].Enabled || blocks[1].URL != fmt.Sprintf("http://mc.example.org:%d/", host) {
		t.Fatalf("BlueMap: %+v, Ports %+v", blocks[1], portsOf(fake, server.ID))
	}
	if pulled := pulledFiles(fake); fmt.Sprint(pulled) != "[plugins/bluemap-5.jar]" {
		t.Fatalf("geladen: %v", pulled)
	}
	fake.mu.Lock()
	config := fake.autoconfigs[server.ID]
	fake.mu.Unlock()
	if len(config.Edits) != 1 || config.Edits[0].File != "plugins/BlueMap/core.conf" || config.Edits[0].Format != "hocon" || config.Edits[0].Set[0].Value != true {
		t.Fatalf("BlueMap braucht accept-download: %+v", config)
	}
	if err := config.Validate(); err != nil {
		t.Fatalf("der Node muss die Autoconfig annehmen: %v", err)
	}
}

func TestGeyserInNetworkSharesFloodgateKey(t *testing.T) {
	p, admin, fake, project := newNetwork(t)
	newFakeSources(t, p)
	nodes, _ := p.store.ListNodes()
	var lobby ServerDTO
	_ = json.Unmarshal(admin.expect(201, "POST", "/api/projects/"+project.ID+"/servers", serverRequest{Name: "Lobby", NodeID: nodes[0].ID, Software: "paper", Version: "1.21.4", EULA: true}), &lobby)
	rows, _ := p.store.ListServers(project.ID)
	proxyID := ""
	for _, row := range rows {
		if row.Role == RoleProxy {
			proxyID = row.ID
		}
	}
	admin.expect(409, "PUT", "/api/servers/"+lobby.ID+"/blocks/geyser", map[string]bool{"enabled": true})

	var blocks []BlockDTO
	_ = json.Unmarshal(admin.expect(200, "PUT", "/api/projects/"+project.ID+"/blocks/geyser", map[string]bool{"enabled": true}), &blocks)
	if !blocks[0].Enabled || blocks[0].Address != "mc.example.org:19132" {
		t.Fatalf("Geyser im Netzwerk: %+v", blocks[0])
	}
	if !hasUDPPort(portsOf(fake, proxyID), bedrockPort) {
		t.Fatalf("der Proxy bekommt den Bedrock-Port: %+v", portsOf(fake, proxyID))
	}
	waitUntil(t, "Schlüssel auf dem Server hinter dem Proxy", func() bool {
		fake.mu.Lock()
		defer fake.mu.Unlock()
		return len(fake.autoconfigs[lobby.ID].Writes) == 1
	})
	fake.mu.Lock()
	proxyConfig, lobbyConfig := fake.autoconfigs[proxyID], fake.autoconfigs[lobby.ID]
	pulled := fmt.Sprint(fake.pulled)
	fake.mu.Unlock()
	var proxyKey string
	sendsData := false
	for _, write := range proxyConfig.Writes {
		if write.Path == "plugins/floodgate/key.pem" {
			proxyKey = write.Content
		}
	}
	for _, edit := range proxyConfig.Edits {
		sendsData = sendsData || edit.File == "plugins/floodgate/config.yml"
	}
	if len(proxyKey) != 16 || !sendsData || lobbyConfig.Writes[0].Path != "plugins/floodgate/key.pem" || lobbyConfig.Writes[0].Content != proxyKey {
		t.Fatalf("Proxy %+v, Lobby %+v", proxyConfig, lobbyConfig)
	}
	for _, want := range []string{"plugins/Geyser-Velocity.jar", "plugins/floodgate-velocity.jar", "plugins/floodgate-spigot.jar"} {
		if !strings.Contains(pulled, want) {
			t.Errorf("%s fehlt: %s", want, pulled)
		}
	}
	// Der Schlüssel steht verschlüsselt in der Datenbank und nie in einer Antwort
	block, _ := p.store.ProjectBlockByName(project.ID, BlockGeyser)
	if block.Secret == "" || strings.Contains(block.Secret, proxyKey) {
		t.Fatalf("Schlüssel in der Datenbank: %q", block.Secret)
	}
	refresh(p, nodes[0].ID)
	for _, address := range []string{"/api/servers/" + proxyID, "/api/servers/" + lobby.ID, "/api/projects/" + project.ID, "/api/projects/" + project.ID + "/blocks"} {
		if body := admin.expect(200, "GET", address, nil); strings.Contains(string(body), proxyKey) {
			t.Fatalf("%s verrät den Floodgate-Schlüssel", address)
		}
	}

	_ = json.Unmarshal(admin.expect(200, "PUT", "/api/projects/"+project.ID+"/blocks/geyser", map[string]bool{"enabled": false}), &blocks)
	if blocks[0].Enabled || hasUDPPort(portsOf(fake, proxyID), bedrockPort) {
		t.Fatalf("ausgeschaltet: %+v", blocks[0])
	}
	if lobbyBlocks, _ := p.store.ServerBlocks(lobby.ID); len(lobbyBlocks) != 0 {
		t.Fatalf("Floodgate auf der Lobby bleibt gemerkt: %+v", lobbyBlocks)
	}
}
