package panel

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"testing"

	"github.com/lodestone-mc/lodestone/internal/software"
)

func TestPositionPacksListAndExport(t *testing.T) {
	_, web, _, _ := setupPanel(t)
	admin := newClient(t, web.URL)
	admin.expect(201, "POST", "/api/auth/setup", map[string]string{"username": "admin", "password": "ein-gutes-passwort"})

	// Packs auflisten
	var summaries []software.PackSummary
	data := admin.expect(200, "GET", "/api/packs", nil)
	if err := json.Unmarshal(data, &summaries); err != nil {
		t.Fatalf("Unmarshal summaries: %v", err)
	}

	if len(summaries) < 9 {
		t.Fatalf("zu wenige Position-Packs: %d", len(summaries))
	}

	foundPaper := false
	foundFabric := false
	foundVelocity := false
	for _, s := range summaries {
		if s.ID == "paper" {
			foundPaper = true
			if !s.CanBeBackend || !s.CanVoice || !s.HasAutoconfig {
				t.Errorf("Paper-Eigenschaften falsch: %+v", s)
			}
		}
		if s.ID == "fabric" {
			foundFabric = true
			if !s.CanBeBackend || !s.HasAutoconfig {
				t.Errorf("Fabric-Eigenschaften falsch: %+v", s)
			}
		}
		if s.ID == "velocity" {
			foundVelocity = true
			if s.Kind != "proxy" {
				t.Errorf("Velocity-Kind falsch: %s", s.Kind)
			}
		}
	}
	if !foundPaper || !foundFabric || !foundVelocity {
		t.Fatalf("Standard-Packs nicht vollständig: paper=%v fabric=%v velocity=%v", foundPaper, foundFabric, foundVelocity)
	}

	// Einzelnes Pack abrufen
	var detail struct {
		Meta       software.PackMeta       `json:"meta"`
		Autoconfig software.PackAutoconfig `json:"autoconfig"`
		Script     string                  `json:"script"`
	}
	detailData := admin.expect(200, "GET", "/api/packs/paper", nil)
	if err := json.Unmarshal(detailData, &detail); err != nil {
		t.Fatalf("Unmarshal detail: %v", err)
	}
	if detail.Meta.ID != "paper" || detail.Autoconfig.Backend == nil {
		t.Fatalf("Paper-Details unvollständig: %+v", detail)
	}

	// Pack als ZIP exportieren
	status, zipBytes := admin.do("GET", "/api/packs/paper/export", nil)
	if status != 200 {
		t.Fatalf("Export fehlgeschlagen mit Status %d", status)
	}
	zr, err := zip.NewReader(bytes.NewReader(zipBytes), int64(len(zipBytes)))
	if err != nil {
		t.Fatalf("Exportiertes ZIP ungültig: %v", err)
	}
	foundMeta := false
	for _, f := range zr.File {
		if f.Name == "meta.json" {
			foundMeta = true
		}
	}
	if !foundMeta {
		t.Error("meta.json im exportierten ZIP nicht gefunden")
	}
}

func TestPositionPacksUploadAndServerCreation(t *testing.T) {
	_, web, fake, nodeID := setupPanel(t)
	admin := newClient(t, web.URL)
	admin.expect(201, "POST", "/api/auth/setup", map[string]string{"username": "admin", "password": "ein-gutes-passwort"})

	// Eigenes Position-Pack bauen
	customPack := &software.PositionPack{
		Meta: software.PackMeta{
			ID:           "sponge",
			Name:         "SpongeVanilla",
			Description:  "Plugin-Plattform für Minecraft.",
			Version:      "1.0.0",
			Kind:         software.KindServer,
			Extensions:   software.ExtPlugins,
			DefaultPort:  25565,
			Startup:      "exec java -jar {{JAR}} nogui",
			StopCommand:  "stop",
			ReadyPattern: `Done \(`,
			Java:         software.PackJava{Strategy: "mojang", Major: 21},
			Provider:     software.PackProvider{Type: "vanilla"},
			Backend: software.PackBackend{
				Supported:       true,
				VoiceConfigFile: "config/sponge/voicechat.properties",
			},
		},
		Autoconfig: software.PackAutoconfig{
			Backend: &software.PackAutoconfigTarget{
				Restart: true,
				Edits: []software.PackEdit{
					{
						File:   "server.properties",
						Format: "properties",
						Set:    []software.PackSetOp{{Path: []string{"online-mode"}, Value: false}},
					},
					{
						File:   "config/sponge/global.conf",
						Format: "yaml",
						Create: true,
						Set: []software.PackSetOp{
							{Path: []string{"modules", "bungeecord"}, Value: true},
							{Path: []string{"bungeecord", "secret"}, Value: "{{SECRET}}"},
						},
					},
				},
			},
		},
		Script: "#!/bin/sh\necho 'Sponge pack initialized'\n",
	}

	zipData, err := software.CreateZipPack(customPack)
	if err != nil {
		t.Fatalf("CreateZipPack: %v", err)
	}

	// Als multipart-form-data hochladen
	body := new(bytes.Buffer)
	writer := multipart.NewWriter(body)
	part, err := writer.CreateFormFile("file", "sponge.positionpack.zip")
	if err != nil {
		t.Fatalf("CreateFormFile: %v", err)
	}
	_, _ = part.Write(zipData)
	_ = writer.Close()

	req, _ := http.NewRequest("POST", web.URL+"/api/packs/upload", body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	req.Header.Set(csrfHeader, "1")
	resp, err := admin.http.Do(req)
	if err != nil {
		t.Fatalf("Upload-Request: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("Upload: Status %d, erwartet 201", resp.StatusCode)
	}

	// Prüfen, ob Pack in der Softwareliste auftaucht
	var softwareList []software.Info
	_ = json.Unmarshal(admin.expect(200, "GET", "/api/software", nil), &softwareList)
	foundSponge := false
	for _, sw := range softwareList {
		if sw.ID == "sponge" {
			foundSponge = true
			if !sw.CanBeBackend || !sw.Custom {
				t.Errorf("Software-Info für Sponge fehlerhaft: %+v", sw)
			}
		}
	}
	if !foundSponge {
		t.Fatal("Sponge taucht nicht in /api/software auf")
	}

	// Projekt anlegen und Server mit neuem Pack erstellen
	var project struct{ ID string }
	_ = json.Unmarshal(admin.expect(201, "POST", "/api/projects", map[string]string{"name": "Sponge-Projekt", "kind": "server"}), &project)

	var server struct{ ID, Software string }
	_ = json.Unmarshal(admin.expect(201, "POST", "/api/projects/"+project.ID+"/servers", serverRequest{
		Name:     "Sponge-1",
		NodeID:   nodeID,
		Software: "sponge",
		Version:  "1.21.4",
		EULA:     true,
	}), &server)

	if server.Software != "sponge" {
		t.Fatalf("Erstellter Server hat falsche Software: %s", server.Software)
	}

	fake.mu.Lock()
	nodeView, ok := fake.servers[server.ID]
	fake.mu.Unlock()
	if !ok || nodeView.Spec.Software != "sponge" {
		t.Fatalf("Server auf dem Node nicht angelegt: %+v", nodeView)
	}
}

func TestPositionPacksDeleteAndPermissions(t *testing.T) {
	_, web, _, _ := setupPanel(t)
	admin := newClient(t, web.URL)
	admin.expect(201, "POST", "/api/auth/setup", map[string]string{"username": "admin", "password": "ein-gutes-passwort"})

	// Weiteren Benutzer anlegen (kein Admin)
	admin.expect(201, "POST", "/api/users", map[string]any{"username": "bob", "password": "passwort-fuer-bob", "isAdmin": false})
	bob := newClient(t, web.URL)
	bob.expect(200, "POST", "/api/auth/login", map[string]string{"username": "bob", "password": "passwort-fuer-bob"})

	// Bob darf Packs ansehen
	bob.expect(200, "GET", "/api/packs", nil)

	// Bob darf nicht hochladen oder löschen
	bob.expect(403, "POST", "/api/packs/upload", []byte("xyz"))
	bob.expect(403, "DELETE", "/api/packs/paper", nil)

	// Standard-Pack löschen verbieten
	admin.expect(400, "DELETE", "/api/packs/paper", nil)

	// Custom Pack hochladen und löschen
	testPack := &software.PositionPack{
		Meta: software.PackMeta{
			ID:          "temp-pack",
			Name:        "Temp",
			Kind:        software.KindServer,
			Extensions:  software.ExtNone,
			DefaultPort: 25565,
		},
	}
	zipBytes, _ := software.CreateZipPack(testPack)
	req, _ := http.NewRequest("POST", web.URL+"/api/packs/upload", bytes.NewReader(zipBytes))
	req.Header.Set("Content-Type", "application/zip")
	req.Header.Set(csrfHeader, "1")
	resp, err := admin.http.Do(req)
	if err != nil || resp.StatusCode != 201 {
		t.Fatalf("Upload: err=%v status=%d", err, resp.StatusCode)
	}
	resp.Body.Close()

	// Löschen
	admin.expect(204, "DELETE", "/api/packs/temp-pack", nil)
	admin.expect(404, "GET", "/api/packs/temp-pack", nil)
}
