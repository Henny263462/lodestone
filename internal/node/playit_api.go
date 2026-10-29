package node

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"time"
)

// Mit dem Schlüssel des Agenten legt der Daemon Tunnel selbst bei playit.gg an, statt dass man sie im Dashboard klickt.
// Die Schnittstelle ist die des Agenten (alles POST mit JSON, Kopfzeile „Authorization: Agent-Key <Schlüssel>“).

const (
	defaultPlayitAPI = "https://api.playit.gg"
	playitMaxBody    = 1 << 20

	// Tunnel-Arten, die Lodestone anlegen kann.
	TunnelJava      = "java"      // Minecraft Java (TCP)
	TunnelVoicechat = "voicechat" // Simple Voice Chat (UDP)
)

var (
	playitTunnelName = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,39}$`)
	playitTunnelID   = regexp.MustCompile(`^[0-9a-fA-F-]{8,64}$`)

	// errUpstream steht für Antworten von playit.gg, die der Benutzer nicht selbst verursacht hat.
	errUpstream = errors.New("playit.gg meldet ein Problem")

	// ErrTunnelExists sagt, dass es einen Tunnel mit diesem Namen schon gibt.
	ErrTunnelExists = errors.New("diesen Tunnel gibt es schon")
)

// PlayitTunnel ist ein Tunnel des Agenten, wie ihn die Oberfläche braucht.
type PlayitTunnel struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	// Address ist die öffentliche Adresse (leer, solange playit sie noch nicht zugewiesen hat).
	Address string `json:"address"`
	// State: allocated (bereit), pending (wird eingerichtet) oder disabled.
	State string `json:"state"`
}

// PlayitTunnelRequest beschreibt einen neuen Tunnel.
type PlayitTunnelRequest struct {
	// Name macht den Tunnel wiederfindbar (Kleinbuchstaben, Ziffern, Bindestrich).
	Name string `json:"name"`
	// Kind: java oder voicechat.
	Kind string `json:"kind"`
	// LocalPort ist der Port auf dieser Maschine, auf den der Tunnel zeigt.
	LocalPort int `json:"localPort"`
}

type playitEnvelope struct {
	Status string          `json:"status"`
	Data   json.RawMessage `json:"data"`
}

// playitCall schickt eine Anfrage an die Schnittstelle und gibt bei Erfolg den Inhalt von „data“ zurück.
func (m *Manager) playitCall(ctx context.Context, secret, path string, body any) (json.RawMessage, error) {
	payload, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	base := m.playit.apiBase
	if base == "" {
		base = defaultPlayitAPI
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, base+path, bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Authorization", "Agent-Key "+secret)
	client := &http.Client{
		Timeout:       20 * time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
	response, err := client.Do(request)
	if err != nil {
		return nil, fmt.Errorf("%w: playit.gg ist nicht erreichbar. Prüfe die Internetverbindung des Nodes", errUpstream)
	}
	defer response.Body.Close()
	if response.StatusCode == http.StatusTooManyRequests {
		return nil, fmt.Errorf("%w: zu viele Anfragen. Versuche es in einer Minute noch einmal", errUpstream)
	}
	raw, err := io.ReadAll(io.LimitReader(response.Body, playitMaxBody))
	if err != nil {
		return nil, fmt.Errorf("%w: die Antwort ließ sich nicht lesen", errUpstream)
	}
	var envelope playitEnvelope
	if err := json.Unmarshal(raw, &envelope); err != nil {
		return nil, fmt.Errorf("%w: unerwartete Antwort (HTTP %d)", errUpstream, response.StatusCode)
	}
	switch envelope.Status {
	case "success":
		return envelope.Data, nil
	case "fail":
		return nil, playitFailure(envelope.Data)
	default:
		return nil, playitError(envelope.Data)
	}
}

// playitCode holt den Namen aus einer Fehlerantwort („Name“ oder {"type":…,"message":"Name"}).
func playitCode(data json.RawMessage) string {
	var text string
	if json.Unmarshal(data, &text) == nil {
		return text
	}
	var object struct {
		Message json.RawMessage `json:"message"`
	}
	if json.Unmarshal(data, &object) == nil && len(object.Message) > 0 {
		if json.Unmarshal(object.Message, &text) == nil {
			return text
		}
		return strings.Trim(string(object.Message), `"{} `)
	}
	return ""
}

func playitFailure(data json.RawMessage) error {
	code := playitCode(data)
	switch code {
	case "RequiresVerifiedAccount":
		return badRequest("das playit-Konto ist noch nicht bestätigt. Bestätige deine E-Mail-Adresse bei playit.gg")
	case "RequiresPlayitPremium", "PublicPortRequiresPlayitPremium", "RegionRequiresPlayitPremium":
		return badRequest("diesen Tunnel erlaubt playit nur mit Premium")
	case "AgentNotFound", "InvalidAgentId":
		return badRequest("playit kennt diesen Agenten nicht. Hinterlege den Schlüssel unter Nodes neu")
	case "AgentVersionTooOld":
		return badRequest("der playit-Agent ist zu alt. Hinterlege den Schlüssel unter Nodes neu, damit er aktualisiert wird")
	case "TunnelNotFound":
		return badRequest("diesen Tunnel gibt es bei playit nicht (mehr)")
	}
	if code == "" || len(code) > 60 {
		code = "unbekannt"
	}
	return fmt.Errorf("%w: playit hat die Anfrage abgelehnt (%s). Prüfe im playit-Dashboard, ob das Tunnel-Limit erreicht ist", errUpstream, code)
}

func playitError(data json.RawMessage) error {
	switch code := playitCode(data); {
	case strings.HasPrefix(code, "Invalid") && strings.Contains(code, "Key"), strings.HasPrefix(code, "Auth"), code == "InvalidSignature":
		return badRequest("playit lehnt den Schlüssel ab. Lege unter Nodes einen gültigen Secret Key eines Agenten fest")
	default:
		return fmt.Errorf("%w: interner Fehler bei playit", errUpstream)
	}
}

// playitSession liest den Schlüssel und holt die Kennung des Agenten.
func (m *Manager) playitSession(ctx context.Context) (secret, agentID string, err error) {
	secret, _, err = m.readPlayit()
	if err != nil {
		return "", "", err
	}
	if secret == "" {
		return "", "", badRequest("playit ist auf diesem Node noch nicht eingerichtet. Hinterlege zuerst unter Nodes den Secret Key")
	}
	data, err := m.playitCall(ctx, secret, "/agents/rundata", struct{}{})
	if err != nil {
		return "", "", err
	}
	var run struct {
		AgentID string `json:"agent_id"`
	}
	if json.Unmarshal(data, &run) != nil || !playitTunnelID.MatchString(run.AgentID) {
		return "", "", fmt.Errorf("%w: die Kennung des Agenten fehlt in der Antwort", errUpstream)
	}
	return secret, run.AgentID, nil
}

type playitTunnelData struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Alloc struct {
		Status string `json:"status"`
		Data   struct {
			AssignedDomain string `json:"assigned_domain"`
			AssignedSRV    string `json:"assigned_srv"`
			PortStart      int    `json:"port_start"`
		} `json:"data"`
	} `json:"alloc"`
}

func (t playitTunnelData) tunnel() PlayitTunnel {
	tunnel := PlayitTunnel{ID: t.ID, Name: t.Name, State: t.Alloc.Status}
	if tunnel.State != "allocated" && tunnel.State != "disabled" {
		tunnel.State = "pending"
	}
	switch data := t.Alloc.Data; {
	case tunnel.State != "allocated":
	case data.AssignedSRV != "":
		tunnel.Address = data.AssignedSRV
	case data.AssignedDomain != "" && data.PortStart > 0:
		tunnel.Address = fmt.Sprintf("%s:%d", data.AssignedDomain, data.PortStart)
	}
	return tunnel
}

func (m *Manager) listPlayitTunnels(ctx context.Context, secret, agentID string) ([]PlayitTunnel, error) {
	data, err := m.playitCall(ctx, secret, "/tunnels/list", map[string]string{"agent_id": agentID})
	if err != nil {
		return nil, err
	}
	var list struct {
		Tunnels []playitTunnelData `json:"tunnels"`
	}
	if err := json.Unmarshal(data, &list); err != nil {
		return nil, fmt.Errorf("%w: die Tunnelliste ließ sich nicht lesen", errUpstream)
	}
	tunnels := make([]PlayitTunnel, 0, len(list.Tunnels))
	for _, tunnel := range list.Tunnels {
		tunnels = append(tunnels, tunnel.tunnel())
	}
	return tunnels, nil
}

// PlayitTunnels listet die Tunnel des Agenten.
func (m *Manager) PlayitTunnels(ctx context.Context) ([]PlayitTunnel, error) {
	secret, agentID, err := m.playitSession(ctx)
	if err != nil {
		return nil, err
	}
	return m.listPlayitTunnels(ctx, secret, agentID)
}

// CreatePlayitTunnel legt einen Tunnel an, der auf einen Port dieser Maschine zeigt, und wartet kurz, bis playit die
// öffentliche Adresse zugewiesen hat.
func (m *Manager) CreatePlayitTunnel(ctx context.Context, request PlayitTunnelRequest) (PlayitTunnel, error) {
	if !playitTunnelName.MatchString(request.Name) {
		return PlayitTunnel{}, badRequest("der Name des Tunnels ist ungültig")
	}
	if request.LocalPort < 1 || request.LocalPort > 65535 {
		return PlayitTunnel{}, badRequest("der Port muss zwischen 1 und 65535 liegen")
	}
	body := map[string]any{
		"name":       request.Name,
		"port_count": 1,
		"enabled":    true,
	}
	switch request.Kind {
	case TunnelJava:
		body["tunnel_type"], body["port_type"] = "minecraft-java", "tcp"
	case TunnelVoicechat:
		// Simple Voice Chat spricht UDP; dafür reicht ein einzelner UDP-Port ohne eigenen Spieltyp
		body["port_type"] = "udp"
	default:
		return PlayitTunnel{}, badRequest("unbekannte Tunnel-Art")
	}

	m.playit.mu.Lock()
	defer m.playit.mu.Unlock()
	secret, agentID, err := m.playitSession(ctx)
	if err != nil {
		return PlayitTunnel{}, err
	}
	existing, err := m.listPlayitTunnels(ctx, secret, agentID)
	if err != nil {
		return PlayitTunnel{}, err
	}
	for _, tunnel := range existing {
		if tunnel.Name == request.Name {
			return PlayitTunnel{}, ErrTunnelExists
		}
	}
	body["origin"] = map[string]any{"type": "agent", "data": map[string]any{
		"agent_id": agentID, "local_ip": "127.0.0.1", "local_port": request.LocalPort,
	}}
	data, err := m.playitCall(ctx, secret, "/tunnels/create", body)
	if err != nil {
		return PlayitTunnel{}, err
	}
	var created struct {
		ID string `json:"id"`
	}
	if json.Unmarshal(data, &created) != nil || !playitTunnelID.MatchString(created.ID) {
		return PlayitTunnel{}, fmt.Errorf("%w: die Kennung des Tunnels fehlt in der Antwort", errUpstream)
	}

	result := PlayitTunnel{ID: created.ID, Name: request.Name, State: "pending"}
	wait := m.playit.pollEvery
	if wait == 0 {
		wait = time.Second
	}
	for attempt := 0; attempt < 10; attempt++ {
		tunnels, err := m.listPlayitTunnels(ctx, secret, agentID)
		if err == nil {
			for _, tunnel := range tunnels {
				if tunnel.ID == created.ID {
					result = tunnel
				}
			}
		}
		if result.Address != "" {
			break
		}
		select {
		case <-ctx.Done():
			return result, nil
		case <-time.After(wait):
		}
	}
	return result, nil
}

// DeletePlayitTunnel löscht einen Tunnel des Agenten.
func (m *Manager) DeletePlayitTunnel(ctx context.Context, id string) error {
	if !playitTunnelID.MatchString(id) {
		return badRequest("die Kennung des Tunnels ist ungültig")
	}
	m.playit.mu.Lock()
	defer m.playit.mu.Unlock()
	secret, _, err := m.playitSession(ctx)
	if err != nil {
		return err
	}
	_, err = m.playitCall(ctx, secret, "/tunnels/delete", map[string]string{"tunnel_id": id})
	return err
}
