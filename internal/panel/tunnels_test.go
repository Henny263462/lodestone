package panel

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/lodestone-mc/lodestone/internal/node"
)

const tunnelSecret = "a1b2c3d4e5f60718293a4b5c6d7e8f90"

func serverTunnelList(t *testing.T, c *client, serverID string) TunnelsDTO {
	t.Helper()
	var list TunnelsDTO
	if err := json.Unmarshal(c.expect(200, "GET", "/api/servers/"+serverID+"/tunnels", nil), &list); err != nil {
		t.Fatal(err)
	}
	return list
}

func publicAddressOf(t *testing.T, p *Panel, serverID string) (address, voice string) {
	t.Helper()
	row, err := p.store.ServerByID(serverID)
	if err != nil {
		t.Fatal(err)
	}
	return row.PublicAddress, row.PublicVoiceAddress
}

func TestJavaTunnelIsCreatedAndRemoved(t *testing.T) {
	p, admin, fake, nodeID, server, _ := serverProject(t)
	path := "/api/servers/" + server.ID + "/tunnels"

	// Ohne Schlüssel gibt es nichts anzulegen
	list := serverTunnelList(t, admin, server.ID)
	if list.Configured || len(list.Tunnels) != 0 || !list.Available.Java || list.Available.Voicechat {
		t.Fatalf("Zustand ohne playit: %+v", list)
	}
	admin.expect(409, "POST", path, map[string]string{"kind": "java"})

	admin.expect(200, "PUT", "/api/nodes/"+nodeID+"/playit", map[string]string{"secret": tunnelSecret})
	admin.expect(400, "POST", path, map[string]string{"kind": "rust"})
	admin.expect(400, "POST", path, map[string]any{"kind": "java", "port": 1})
	admin.expect(409, "POST", path, map[string]string{"kind": "voicechat"}) // kein UDP-Port veröffentlicht

	var tunnel TunnelDTO
	_ = json.Unmarshal(admin.expect(201, "POST", path, map[string]string{"kind": "java"}), &tunnel)
	if tunnel.Kind != "java" || tunnel.Address != "mein-server.joinmc.link" || tunnel.State != "allocated" {
		t.Fatalf("Tunnel: %+v", tunnel)
	}
	fake.mu.Lock()
	request := fake.tunnelReqs[0]
	hostPort := fake.servers[server.ID].Ports[0].Host
	fake.mu.Unlock()
	if request.Name != "ls-"+server.ID+"-java" || request.Kind != node.TunnelJava || request.LocalPort != hostPort {
		t.Errorf("Anfrage an den Node: %+v (Port des Servers: %d)", request, hostPort)
	}
	if address, _ := publicAddressOf(t, p, server.ID); address != "mein-server.joinmc.link" {
		t.Errorf("die Adresse gehört jetzt zum Server: %q", address)
	}

	list = serverTunnelList(t, admin, server.ID)
	if !list.Configured || len(list.Tunnels) != 1 || list.Tunnels[0].Address != "mein-server.joinmc.link" {
		t.Errorf("Liste: %+v", list)
	}

	// Ein zweites Mal wird nichts doppelt angelegt
	admin.expect(201, "POST", path, map[string]string{"kind": "java"})
	fake.mu.Lock()
	count := len(fake.tunnelReqs)
	fake.mu.Unlock()
	if count != 1 {
		t.Errorf("es darf nur eine Anfrage beim Node ankommen: %d", count)
	}

	admin.expect(404, "DELETE", path+"/voicechat", nil)
	admin.expect(404, "DELETE", path+"/unsinn", nil)
	admin.expect(204, "DELETE", path+"/java", nil)
	if address, _ := publicAddressOf(t, p, server.ID); address != "" {
		t.Errorf("die Adresse muss mit dem Tunnel verschwinden: %q", address)
	}
	if list := serverTunnelList(t, admin, server.ID); len(list.Tunnels) != 0 {
		t.Errorf("nach dem Löschen: %+v", list)
	}
}

func TestTunnelWithoutAddressIsAdoptedLater(t *testing.T) {
	p, admin, fake, nodeID, server, _ := serverProject(t)
	path := "/api/servers/" + server.ID + "/tunnels"
	admin.expect(200, "PUT", "/api/nodes/"+nodeID+"/playit", map[string]string{"secret": tunnelSecret})

	fake.mu.Lock()
	fake.tunnelNoAddress = true
	fake.mu.Unlock()
	admin.expect(202, "POST", path, map[string]string{"kind": "java"})
	if address, _ := publicAddressOf(t, p, server.ID); address != "" {
		t.Fatalf("ohne Adresse darf nichts gespeichert werden: %q", address)
	}

	// playit hat die Adresse inzwischen vergeben
	fake.mu.Lock()
	fake.tunnels[0].Address, fake.tunnels[0].State = "spaet.joinmc.link", "allocated"
	fake.mu.Unlock()
	admin.expect(201, "POST", path, map[string]string{"kind": "java"})
	if address, _ := publicAddressOf(t, p, server.ID); address != "spaet.joinmc.link" {
		t.Errorf("übernommene Adresse: %q", address)
	}
	fake.mu.Lock()
	count := len(fake.tunnelReqs)
	fake.mu.Unlock()
	if count != 1 {
		t.Errorf("der Tunnel wird übernommen, nicht neu angelegt: %d Anfragen", count)
	}
}

func voiceHostOf(config node.Autoconfig) string {
	for _, edit := range config.Edits {
		for _, op := range edit.Set {
			if len(op.Path) == 1 && op.Path[0] == "voice_host" {
				text, _ := op.Value.(string)
				return text
			}
		}
	}
	return ""
}

func TestVoicechatTunnelSetsVoiceHost(t *testing.T) {
	p, admin, fake, nodeID, server, _ := serverProject(t)
	path := "/api/servers/" + server.ID + "/tunnels"
	admin.expect(200, "PUT", "/api/nodes/"+nodeID+"/playit", map[string]string{"secret": tunnelSecret})

	// Voicechat ist eingeschaltet, wenn der Server einen veröffentlichten UDP-Port hat
	fake.mu.Lock()
	view := fake.servers[server.ID]
	view.Ports = append(view.Ports, node.PortMapping{Container: voicePort, Host: 28010, Protocol: "udp"})
	fake.servers[server.ID] = view
	fake.mu.Unlock()
	refresh(p, nodeID)
	row, _ := p.store.ServerByID(server.ID)
	row.Voicechat = true
	if err := p.store.UpdateServer(row); err != nil {
		t.Fatal(err)
	}

	if list := serverTunnelList(t, admin, server.ID); !list.Available.Voicechat {
		t.Fatalf("Voicechat müsste möglich sein: %+v", list)
	}
	admin.expect(201, "POST", path, map[string]string{"kind": "voicechat"})
	fake.mu.Lock()
	request := fake.tunnelReqs[0]
	fake.mu.Unlock()
	if request.Name != "ls-"+server.ID+"-voice" || request.Kind != node.TunnelVoicechat || request.LocalPort != 28010 {
		t.Errorf("Anfrage an den Node: %+v", request)
	}
	address, voice := publicAddressOf(t, p, server.ID)
	if address != "" || voice != "abc.gl.at.ply.gg:4711" {
		t.Fatalf("Adressen: %q / %q", address, voice)
	}

	// Die Oberfläche zeigt die Voicechat-Adresse, und der Server bekommt sie als voice_host
	var dto ProjectDTO
	_ = json.Unmarshal(admin.expect(200, "GET", "/api/projects/"+server.ProjectID, nil), &dto)
	if dto.Servers[0].VoiceAddress != "abc.gl.at.ply.gg:4711" {
		t.Errorf("Voicechat-Adresse in der Anzeige: %q", dto.Servers[0].VoiceAddress)
	}
	waitUntil(t, "voice_host beim Node", func() bool {
		fake.mu.Lock()
		defer fake.mu.Unlock()
		return voiceHostOf(fake.autoconfigs[server.ID]) == "abc.gl.at.ply.gg:4711"
	})

	// Mit dem Tunnel verschwindet die Adresse, voice_host zeigt wieder auf die Maschine
	admin.expect(204, "DELETE", path+"/voicechat", nil)
	if _, voice := publicAddressOf(t, p, server.ID); voice != "" {
		t.Errorf("die Voicechat-Adresse muss mit dem Tunnel verschwinden: %q", voice)
	}
	waitUntil(t, "voice_host zurückgesetzt", func() bool {
		fake.mu.Lock()
		defer fake.mu.Unlock()
		return strings.HasPrefix(voiceHostOf(fake.autoconfigs[server.ID]), "mc.example.org:")
	})
}

func TestTunnelRightsAndOtherProjects(t *testing.T) {
	p, admin, _, nodeID, server, projectID := serverProject(t)
	path := "/api/servers/" + server.ID + "/tunnels"
	admin.expect(200, "PUT", "/api/nodes/"+nodeID+"/playit", map[string]string{"secret": tunnelSecret})

	admin.expect(201, "POST", "/api/users", map[string]any{"username": "betrachter", "password": "ein-gutes-passwort"})
	admin.expect(201, "POST", "/api/users", map[string]any{"username": "fremder", "password": "ein-gutes-passwort"})
	users, _ := p.store.ListUsers()
	for _, user := range users {
		if user.Username == "betrachter" {
			admin.expect(200, "PUT", "/api/projects/"+projectID+"/members/"+user.ID, map[string]string{"role": "viewer"})
		}
	}
	viewer := newClient(t, admin.base)
	viewer.expect(200, "POST", "/api/auth/login", map[string]string{"username": "betrachter", "password": "ein-gutes-passwort"})
	viewer.expect(200, "GET", path, nil)
	viewer.expect(403, "POST", path, map[string]string{"kind": "java"})
	viewer.expect(403, "DELETE", path+"/java", nil)

	stranger := newClient(t, admin.base)
	stranger.expect(200, "POST", "/api/auth/login", map[string]string{"username": "fremder", "password": "ein-gutes-passwort"})
	stranger.expect(404, "GET", path, nil)
	stranger.expect(404, "POST", path, map[string]string{"kind": "java"})
	stranger.expect(404, "DELETE", path+"/java", nil)

	newClient(t, admin.base).expect(401, "GET", path, nil)
	admin.expect(404, "GET", "/api/servers/gibtsnicht/tunnels", nil)
}

func TestVoiceAddressOverridesTheMachineInNetworks(t *testing.T) {
	servers := []NetServer{
		{Row: ServerRow{ID: "aaa", Name: "Proxy", Slug: "proxy", Role: RoleProxy, Software: "velocity"}, VoiceHostPort: 28001, PublicHost: "mc.example.org", VoiceAddress: "abc.gl.at.ply.gg:4711"},
		{Row: ServerRow{ID: "bbb", Name: "Survival", Slug: "survival", Role: RoleBackend, Software: "paper", Voicechat: true}, VoiceAddress: "xyz.gl.at.ply.gg:4712"},
	}
	proxy := BuildAutoconfigs("geheim", servers, true)["aaa"]
	if got := setValue(t, editFor(t, proxy, "plugins/voicechat/voicechat-proxy.properties"), "voice_host"); got != "abc.gl.at.ply.gg:4711" {
		t.Errorf("voice_host am Proxy = %v", got)
	}
	backend := BuildAutoconfigs("geheim", servers, false)["bbb"]
	if got := setValue(t, editFor(t, backend, "plugins/voicechat/voicechat-server.properties"), "voice_host"); got != "xyz.gl.at.ply.gg:4712" {
		t.Errorf("voice_host am Server = %v", got)
	}
}
