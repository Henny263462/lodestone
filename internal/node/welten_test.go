package node

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/url"
	"strings"
	"testing"
)

// multipartFile baut einen Upload mit einer Datei im Feld "file".
func multipartFile(t *testing.T, name string, data []byte) ([]byte, string) {
	t.Helper()
	var buffer bytes.Buffer
	writer := multipart.NewWriter(&buffer)
	part, err := writer.CreateFormFile("file", name)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = part.Write(data)
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	return buffer.Bytes(), writer.FormDataContentType()
}

func worldPath(id, world, rest string) string {
	return "/api/servers/" + id + "/worlds/" + url.PathEscape(world) + rest
}

func TestAPIPropertiesForm(t *testing.T) {
	a := newTestAPI(t)
	id := a.create("Eigenschaften")
	base := "/api/servers/" + id + "/properties"

	status, body := a.do("GET", base, nil)
	var view PropertiesView
	_ = json.Unmarshal(body, &view)
	if status != 200 || view.Exists || len(view.Entries) != 0 {
		t.Fatalf("ohne Datei: %d %s", status, body)
	}

	server, _ := a.manager.Get(id)
	put(t, server, "server.properties", []byte("#Minecraft server properties\nmotd=A Minecraft Server\nmein-plugin=bleibt\npvp=true\n"))
	if status, body := a.do("PUT", base, map[string]any{"set": map[string]string{"motd": "Hallo: Welt", "difficulty": "hard"}}); status != 200 {
		t.Fatalf("setzen: %d %s", status, body)
	}
	content, _ := server.FS().Read("server.properties", 1<<20)
	want := "#Minecraft server properties\nmotd=Hallo\\: Welt\nmein-plugin=bleibt\npvp=true\ndifficulty=hard\n"
	if string(content) != want {
		t.Fatalf("Datei:\n%q\nerwartet:\n%q", content, want)
	}
	_, body = a.do("GET", base, nil)
	_ = json.Unmarshal(body, &view)
	if !view.Exists || len(view.Entries) != 4 || view.Entries[0] != (Property{Key: "motd", Value: "Hallo: Welt"}) {
		t.Fatalf("gelesen: %s", body)
	}

	for _, bad := range []map[string]any{
		{"set": map[string]string{"../evil": "x"}},
		{"set": map[string]string{"a b": "x"}},
		{"set": map[string]string{}},
		{"set": map[string]string{"motd": strings.Repeat("x", maxPropertyValue+1)}},
		{"unbekannt": true},
	} {
		if status, body := a.do("PUT", base, bad); status != http.StatusBadRequest {
			t.Errorf("%v: %d %s", bad, status, body)
		}
	}
}

func TestAPIWorldsListResetAndDownload(t *testing.T) {
	a := newTestAPI(t)
	id := a.create("Welten")
	server, _ := a.manager.Get(id)
	put(t, server, "server.properties", []byte("level-name=world\nlevel-seed=123\n"))
	put(t, server, "world/level.dat", []byte("hauptwelt"))
	put(t, server, "world/region/r.0.0.mca", bytes.Repeat([]byte("x"), 4096))
	put(t, server, "world/session.lock", []byte("lock"))
	put(t, server, "world_nether/level.dat", []byte("nether"))
	put(t, server, "world_the_end/level.dat", []byte("end"))
	put(t, server, "creative/level.dat", []byte("andere"))
	put(t, server, "plugins/LuckPerms/config.yml", []byte("kein: welt"))

	status, body := a.do("GET", "/api/servers/"+id+"/worlds", nil)
	var list WorldList
	_ = json.Unmarshal(body, &list)
	if status != 200 || list.LevelName != "world" || list.Seed != "123" || len(list.Worlds) != 4 {
		t.Fatalf("Liste: %d %s", status, body)
	}
	kinds := []string{}
	for _, world := range list.Worlds {
		kinds = append(kinds, world.Name+":"+world.Kind)
	}
	if strings.Join(kinds, ",") != "world:overworld,world_nether:nether,world_the_end:end,creative:other" || !list.Worlds[0].Main || list.Worlds[0].Size < 4096 {
		t.Fatalf("Welten: %v %+v", kinds, list.Worlds[0])
	}

	// Herunterladen: der Weltordner liegt im Zip, die Sperrdatei des Servers nicht
	status, body = a.do("GET", worldPath(id, "world", "/download"), nil)
	if status != 200 {
		t.Fatalf("Download: %d %s", status, body)
	}
	archive, err := zip.NewReader(bytes.NewReader(body), int64(len(body)))
	if err != nil {
		t.Fatal(err)
	}
	names := map[string]bool{}
	for _, entry := range archive.File {
		names[entry.Name] = true
	}
	if !names["world/level.dat"] || !names["world/region/r.0.0.mca"] || names["world/session.lock"] {
		t.Fatalf("Zip-Inhalt: %v", names)
	}

	// Pfade und Namen werden geprüft
	for _, evil := range []string{"..", "../world", "plugins", "world/../plugins", ".lodestone-restore", "gibt-es-nicht"} {
		if status, _ := a.do("GET", worldPath(id, evil, "/download"), nil); status == 200 {
			t.Errorf("%q wurde ausgeliefert", evil)
		}
		if status, _ := a.do("POST", worldPath(id, evil, "/reset"), map[string]any{}); status == http.StatusAccepted {
			t.Errorf("%q wurde zurückgesetzt", evil)
		}
	}
	if !server.FS().Exists("plugins/LuckPerms/config.yml") {
		t.Fatal("ein Ordner ohne level.dat darf nie gelöscht werden")
	}

	// Zurücksetzen geht nur, wenn der Server aus ist
	_ = server.Start()
	waitState(t, server, StateRunning)
	if status, body := a.do("POST", worldPath(id, "world", "/reset"), map[string]any{}); status != http.StatusConflict {
		t.Fatalf("Zurücksetzen bei laufendem Server: %d %s", status, body)
	}
	_ = server.Stop()
	waitState(t, server, StateOffline)

	seed := "neuer-seed"
	if status, body := a.do("POST", worldPath(id, "world", "/reset"), WorldReset{Backup: true, Seed: &seed, Dimensions: true}); status != http.StatusAccepted {
		t.Fatalf("Zurücksetzen: %d %s", status, body)
	}
	waitState(t, server, StateOffline)
	if server.FS().Exists("world") || server.FS().Exists("world_nether") || server.FS().Exists("world_the_end") {
		t.Fatal("Hauptwelt samt Nether und End müssen weg sein")
	}
	if !server.FS().Exists("creative/level.dat") || !server.FS().Exists("plugins/LuckPerms/config.yml") {
		t.Fatal("andere Welten und Ordner müssen bleiben")
	}
	if value, _ := ParseProperties(mustRead(t, server, "server.properties")).Get("level-seed"); value != seed {
		t.Fatalf("Seed: %q", value)
	}
	backups, _ := server.ListBackups()
	if len(backups) != 1 || !strings.Contains(backups[0].Name, "vor-reset-world") || backups[0].Creating {
		t.Fatalf("Backup vor dem Zurücksetzen: %+v", backups)
	}
	if !hasLine(server, "Welt world gelöscht") {
		t.Error("das Zurücksetzen gehört in die Konsole")
	}
}

func mustRead(t *testing.T, server *Server, name string) []byte {
	t.Helper()
	data, err := server.FS().Read(name, 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func zipOf(t *testing.T, files map[string]string) []byte {
	t.Helper()
	var buffer bytes.Buffer
	writer := zip.NewWriter(&buffer)
	for name, content := range files {
		entry, err := writer.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		_, _ = entry.Write([]byte(content))
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	return buffer.Bytes()
}

func TestAPIWorldUploadIsSafe(t *testing.T) {
	a := newTestAPI(t)
	id := a.create("Upload")
	server, _ := a.manager.Get(id)
	put(t, server, "world/level.dat", []byte("alt"))
	put(t, server, "world/alt.txt", []byte("alt"))
	put(t, server, "plugins/x.jar", []byte("jar"))

	upload := func(world string, data []byte, query string) (int, string) {
		body, contentType := multipartFile(t, "welt.zip", data)
		status, answer := a.do("POST", worldPath(id, world, "/upload"+query), body, "Content-Type", contentType)
		return status, string(answer)
	}

	// Ein Zip mit Ausbruchsversuch wird abgelehnt, die alte Welt bleibt
	if status, body := upload("world", zipOf(t, map[string]string{"level.dat": "neu", "../../evil.txt": "x"}), ""); status != http.StatusBadRequest {
		t.Fatalf("Ausbruch: %d %s", status, body)
	}
	if string(mustRead(t, server, "world/level.dat")) != "alt" || server.FS().Exists("evil.txt") {
		t.Fatal("die alte Welt muss unverändert bleiben")
	}
	// Ohne level.dat ist es keine Welt
	if status, body := upload("world", zipOf(t, map[string]string{"readme.txt": "x"}), ""); status != http.StatusBadRequest || !strings.Contains(body, "level.dat") {
		t.Fatalf("ohne level.dat: %d %s", status, body)
	}
	// Ein Ordner, der keine Welt ist, wird nie überschrieben
	if status, body := upload("plugins", zipOf(t, map[string]string{"level.dat": "x"}), ""); status != http.StatusConflict {
		t.Fatalf("plugins: %d %s", status, body)
	}
	// Ungültige Namen
	if status, _ := upload("..", zipOf(t, map[string]string{"level.dat": "x"}), ""); status == http.StatusCreated {
		t.Fatal("„..“ darf kein Weltname sein")
	}

	// Welt in einem Unterordner ersetzt die alte, vorher mit Backup
	data := zipOf(t, map[string]string{"MeineWelt/level.dat": "neu", "MeineWelt/region/r.0.0.mca": "region", "MeineWelt/session.lock": "x"})
	if status, body := upload("world", data, "?backup=1"); status != http.StatusCreated {
		t.Fatalf("ersetzen: %d %s", status, body)
	}
	if string(mustRead(t, server, "world/level.dat")) != "neu" || server.FS().Exists("world/alt.txt") || server.FS().Exists("world/session.lock") {
		t.Fatal("die Welt wurde nicht ersetzt")
	}
	if backups, _ := server.ListBackups(); len(backups) != 1 {
		t.Fatalf("Backup vor dem Ersetzen: %+v", backups)
	}
	// Neue Welt unter neuem Namen, level.dat ganz oben
	if status, body := upload("Creative Welt", zipOf(t, map[string]string{"level.dat": "c"}), ""); status != http.StatusCreated {
		t.Fatalf("neue Welt: %d %s", status, body)
	}
	if !server.isWorld("Creative Welt") {
		t.Fatal("die neue Welt fehlt")
	}
	entries, _ := server.FS().List(".")
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name, restoreTemp) {
			t.Errorf("Hilfsdatei liegt noch herum: %s", entry.Name)
		}
	}
	if server.View().State != StateOffline {
		t.Fatalf("Zustand danach: %s", server.View().State)
	}

	// Bei laufendem Server geht nichts
	_ = server.Start()
	waitState(t, server, StateRunning)
	if status, body := upload("world", zipOf(t, map[string]string{"level.dat": "x"}), ""); status != http.StatusConflict {
		t.Fatalf("bei laufendem Server: %d %s", status, body)
	}
}

func TestAPIDatapacks(t *testing.T) {
	a := newTestAPI(t)
	id := a.create("Datapacks")
	server, _ := a.manager.Get(id)
	put(t, server, "world/level.dat", []byte("welt"))
	base := worldPath(id, "world", "/datapacks")

	status, body := a.do("GET", base, nil)
	if status != 200 || strings.TrimSpace(string(body)) != "[]" {
		t.Fatalf("leer: %d %s", status, body)
	}
	pack := zipOf(t, map[string]string{"pack.mcmeta": `{"pack":{"pack_format":48}}`, "data/x/function/a.mcfunction": "say hi"})
	send := func(name string, data []byte, query string) int {
		payload, contentType := multipartFile(t, name, data)
		status, _ := a.do("POST", base+query, payload, "Content-Type", contentType)
		return status
	}
	if status := send(`..\..\Mein Pack.zip`, pack, ""); status != http.StatusCreated {
		t.Fatalf("hochladen: %d", status)
	}
	if !server.FS().Exists("world/datapacks/Mein Pack.zip") {
		t.Fatal("der Name muss auf den Dateinamen gekürzt landen")
	}
	if status := send("Mein Pack.zip", pack, ""); status != http.StatusConflict {
		t.Fatalf("doppelt: %d", status)
	}
	if status := send("Mein Pack.zip", pack, "?overwrite=1"); status != http.StatusCreated {
		t.Fatalf("überschreiben: %d", status)
	}
	if status := send("kaputt.zip", zipOf(t, map[string]string{"readme.txt": "x"}), ""); status != http.StatusBadRequest {
		t.Fatalf("ohne pack.mcmeta: %d", status)
	}
	if status := send("kein-zip.jar", pack, ""); status != http.StatusBadRequest {
		t.Fatalf("keine .zip: %d", status)
	}
	if server.FS().Exists("world/datapacks/kaputt.zip") || server.FS().Exists("world/datapacks/kaputt.zip.lodestone-part") {
		t.Fatal("abgelehnte Datei liegt noch da")
	}
	put(t, server, "world/datapacks/ordner-pack/pack.mcmeta", []byte("{}"))

	var packs []Datapack
	_, body = a.do("GET", base, nil)
	_ = json.Unmarshal(body, &packs)
	if len(packs) != 2 {
		t.Fatalf("Liste: %s", body)
	}
	if status, _ := a.do("DELETE", base+"/"+url.PathEscape(".."), nil); status != http.StatusBadRequest && status != http.StatusNotFound {
		t.Fatalf("„..“ löschen: %d", status)
	}
	if status, _ := a.do("DELETE", base+"/gibt-es-nicht.zip", nil); status != http.StatusNotFound {
		t.Fatalf("fehlendes Pack: %d", status)
	}
	if status, _ := a.do("DELETE", base+"/"+url.PathEscape("Mein Pack.zip"), nil); status != http.StatusNoContent {
		t.Fatalf("löschen: %d", status)
	}
	if status, _ := a.do("DELETE", base+"/ordner-pack", nil); status != http.StatusNoContent {
		t.Fatalf("Ordner löschen: %d", status)
	}
	if status, _ := a.do("GET", worldPath(id, "keine-welt", "/datapacks"), nil); status != http.StatusNotFound {
		t.Fatalf("fehlende Welt: %d", status)
	}
}
