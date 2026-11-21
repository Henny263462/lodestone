package node

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

const testSecret = "a1b2c3d4e5f60718293a4b5c6d7e8f90"

func TestPlayitLifecycle(t *testing.T) {
	rt := newFakeRuntime()
	manager := newTestManager(t, testConfig(t), rt)
	ctx := context.Background()

	if status := manager.PlayitStatus(ctx); status.Configured || status.Running {
		t.Fatalf("ohne Einrichtung: %+v", status)
	}
	for _, bad := range []string{"", "kurz", "mit leerzeichen und viel zu lang abc", "../../etc/passwd-und-mehr"} {
		if err := manager.ConfigurePlayit(ctx, bad); !errors.Is(err, errBadRequest) {
			t.Errorf("%q hätte abgelehnt werden müssen: %v", bad, err)
		}
	}

	if err := manager.ConfigurePlayit(ctx, "  "+testSecret+"\n"); err != nil {
		t.Fatal(err)
	}
	container := rt.find(playitContainer)
	if container == nil || !container.running {
		t.Fatal("der Agent muss laufen")
	}
	spec := container.spec
	if spec.Network != "host" || spec.Restart != "unless-stopped" || spec.Labels[LabelService] != "playit" || spec.Labels[LabelServer] != "" {
		t.Errorf("Container-Spec: %+v", spec)
	}
	if len(spec.Env) != 1 || spec.Env[0] != "SECRET_KEY="+testSecret {
		t.Errorf("Umgebung: %v", spec.Env)
	}
	if spec.Image != DefaultPlayitImage {
		t.Errorf("Image: %s", spec.Image)
	}
	info, err := os.Stat(manager.playitPath())
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm()&0o077 != 0 && !strings.Contains(strings.ToLower(os.Getenv("OS")), "windows") {
		t.Errorf("die Schlüsseldatei darf nur für den Daemon lesbar sein: %v", info.Mode())
	}

	// Der Schlüssel taucht in der Anzeige nicht auf, auch wenn der Agent ihn ausgibt
	container.say("Agent gestartet mit " + testSecret)
	container.say("Tunnel bereit")
	status := manager.PlayitStatus(ctx)
	if !status.Configured || !status.Running || len(status.Logs) < 2 {
		t.Fatalf("Zustand: %+v", status)
	}
	for _, line := range status.Logs {
		if strings.Contains(line, testSecret) {
			t.Errorf("der Schlüssel steht in der Ausgabe: %q", line)
		}
	}

	// Ein neuer Schlüssel ersetzt den Container
	other := "ffffffffffffffffffffffffffffffff"
	if err := manager.ConfigurePlayit(ctx, other); err != nil {
		t.Fatal(err)
	}
	if rt.count() != 1 || rt.find(playitContainer).spec.Env[0] != "SECRET_KEY="+other {
		t.Errorf("der alte Container muss ersetzt sein (%d Container)", rt.count())
	}

	// Nach einem Neustart des Daemons wird ein fehlender oder gestoppter Agent wieder gestartet
	_ = rt.Remove(ctx, playitContainer)
	if err := manager.EnsurePlayit(ctx); err != nil {
		t.Fatal(err)
	}
	if c := rt.find(playitContainer); c == nil || !c.running {
		t.Fatal("EnsurePlayit muss den Agenten wieder anlegen")
	}

	if err := manager.RemovePlayit(ctx); err != nil {
		t.Fatal(err)
	}
	if rt.find(playitContainer) != nil || manager.PlayitStatus(ctx).Configured {
		t.Error("nach dem Entfernen darf nichts übrig sein")
	}
	if err := manager.EnsurePlayit(ctx); err != nil || rt.find(playitContainer) != nil {
		t.Errorf("ohne Schlüssel startet EnsurePlayit nichts: %v", err)
	}
}

func TestPlayitIsNotTreatedAsAServer(t *testing.T) {
	rt := newFakeRuntime()
	manager := newTestManager(t, testConfig(t), rt)
	if err := manager.ConfigurePlayit(context.Background(), testSecret); err != nil {
		t.Fatal(err)
	}
	if err := manager.Reconcile(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(manager.List()) != 0 {
		t.Fatal("der Agent ist kein Server")
	}
}

func TestPlayitTunnelsAndCreation(t *testing.T) {
	rt := newFakeRuntime()
	manager := newTestManager(t, testConfig(t), rt)
	ctx := context.Background()

	// Vor der Einrichtung
	if _, err := manager.PlayitTunnels(ctx, true); err == nil {
		t.Fatal("ohne Einrichtung muss PlayitTunnels fehlschlagen")
	}

	createdTunnels := []map[string]any{}
	fakeServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Agent-Key "+testSecret {
			w.WriteHeader(http.StatusUnauthorized)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"status": "error",
				"data":   map[string]string{"type": "auth", "message": "InvalidAgentKey"},
			})
			return
		}
		switch r.URL.Path {
		case "/agents/rundata":
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"status": "success",
				"data": map[string]any{
					"agent_id": "test-agent-123",
					"tunnels": []map[string]any{
						{
							"id":              "tun-1",
							"name":            "Survival",
							"tunnel_type":     "minecraft-java",
							"port_type":       "tcp",
							"assigned_domain": "survival.ply.gg",
							"display_address": "survival.ply.gg:25565",
							"port":            map[string]int{"from": 25565, "to": 25565},
							"origin": map[string]any{
								"type": "agent",
								"data": map[string]any{
									"agent_id":   "test-agent-123",
									"local_ip":   "127.0.0.1",
									"local_port": 25565,
								},
							},
							"enabled": true,
						},
					},
				},
			})
		case "/tunnels/create":
			var req map[string]any
			_ = json.NewDecoder(r.Body).Decode(&req)
			createdTunnels = append(createdTunnels, req)
			name, _ := req["name"].(string)
			tunnelType, _ := req["tunnel_type"].(string)
			portType, _ := req["port_type"].(string)
			origin, _ := req["origin"].(map[string]any)
			originData, _ := origin["data"].(map[string]any)
			localPort := 0
			if p, ok := originData["local_port"].(float64); ok {
				localPort = int(p)
			}
			addr := "new.ply.gg:12345"
			if tunnelType == "mc: simple voice chat" {
				addr = "voice.ply.gg:24454"
			}
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"status": "success",
				"data": map[string]any{
					"id":              "tun-created",
					"name":            name,
					"tunnel_type":     tunnelType,
					"port_type":       portType,
					"display_address": addr,
					"origin": map[string]any{
						"type": "agent",
						"data": map[string]any{
							"agent_id":   "test-agent-123",
							"local_ip":   "127.0.0.1",
							"local_port": localPort,
						},
					},
					"enabled": true,
				},
			})
		case "/tunnels/delete":
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(map[string]any{"status": "success"})
		default:
			http.NotFound(w, r)
		}
	}))
	defer fakeServer.Close()

	manager.SetPlayitAPIForTest(fakeServer.URL, fakeServer.Client())

	if err := manager.ConfigurePlayit(ctx, testSecret); err != nil {
		t.Fatal(err)
	}

	// 1. Tunnels abrufen
	tunnels, err := manager.PlayitTunnels(ctx, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(tunnels) != 1 || tunnels[0].PublicAddress != "survival.ply.gg:25565" || tunnels[0].LocalPort != 25565 {
		t.Fatalf("unerwartete Tunnel: %+v", tunnels)
	}

	// 2. Minecraft Java Tunnel erstellen
	mcTunnel, err := manager.CreatePlayitTunnel(ctx, CreateTunnelRequest{
		Name:       "Lobby",
		TunnelType: "minecraft-java",
		LocalPort:  25565,
	})
	if err != nil {
		t.Fatal(err)
	}
	if mcTunnel.PublicAddress != "new.ply.gg:12345" || mcTunnel.TunnelType != "minecraft-java" || mcTunnel.PortType != "tcp" {
		t.Fatalf("unerwarteter MC-Tunnel: %+v", mcTunnel)
	}

	// 3. Voicechat Tunnel erstellen (Tunnel-Typ "mc: simple voice chat", UDP)
	voiceTunnel, err := manager.CreatePlayitTunnel(ctx, CreateTunnelRequest{
		Name:       "Survival Voice",
		TunnelType: "voice",
		LocalPort:  24454,
	})
	if err != nil {
		t.Fatal(err)
	}
	if voiceTunnel.PublicAddress != "voice.ply.gg:24454" || voiceTunnel.TunnelType != "mc: simple voice chat" || voiceTunnel.PortType != "udp" {
		t.Fatalf("unerwarteter Voice-Tunnel: %+v", voiceTunnel)
	}

	// 4. Tunnel löschen
	if err := manager.DeletePlayitTunnel(ctx, "tun-created"); err != nil {
		t.Fatal(err)
	}
}
