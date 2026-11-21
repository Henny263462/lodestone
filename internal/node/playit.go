package node

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Der playit-Agent verbindet die Maschine mit playit.gg, damit Server ohne Portfreigabe im Router erreichbar sind.
// Die Tunnel (welche öffentliche Adresse auf welchen lokalen Port zeigt) legt man im playit-Dashboard an oder lässt sie
// von Lodestone automatisch über die playit.gg-API erstellen.

const (
	playitContainer = "lodestone-playit"
	// DefaultPlayitImage ist das offizielle Image des Agenten.
	DefaultPlayitImage = "ghcr.io/playit-cloud/playit-agent:0.15"
	playitLogLines     = 30
	// DefaultPlayitAPIURL ist die Standard-Adresse der playit.gg-Schnittstelle.
	DefaultPlayitAPIURL = "https://api.playit.gg"
)

var playitSecret = regexp.MustCompile(`^[A-Za-z0-9._-]{16,256}$`)

type playitFile struct {
	Secret string `json:"secret"`
}

// PlayitTunnel beschreibt einen aktiven Tunnel über playit.gg.
type PlayitTunnel struct {
	ID            string `json:"id"`
	Name          string `json:"name"`
	TunnelType    string `json:"tunnelType"`
	PortType      string `json:"portType"`
	LocalIP       string `json:"localIp"`
	LocalPort     int    `json:"localPort"`
	PublicAddress string `json:"publicAddress"`
	Enabled       bool   `json:"enabled"`
	CreatedAt     int64  `json:"createdAt,omitempty"`
}

// CreateTunnelRequest enthält die Angaben zum automatischen Anlegen eines Tunnels.
type CreateTunnelRequest struct {
	Name       string `json:"name"`
	TunnelType string `json:"tunnelType"`
	PortType   string `json:"portType"`
	LocalPort  int    `json:"localPort"`
	LocalIP    string `json:"localIp,omitempty"`
}

// PlayitStatus beschreibt den Agenten; der Schlüssel selbst wird nie ausgegeben.
type PlayitStatus struct {
	Configured bool           `json:"configured"`
	Running    bool           `json:"running"`
	Image      string         `json:"image"`
	Logs       []string       `json:"logs"`
	Error      string         `json:"error,omitempty"`
	// Since ist der Zeitpunkt der letzten Einrichtung (Millisekunden).
	Since   int64          `json:"since,omitempty"`
	Tunnels []PlayitTunnel `json:"tunnels,omitempty"`
}

type playitState struct {
	mu            sync.Mutex
	apiURL        string
	httpClient    *http.Client
	cachedTunnels []PlayitTunnel
	cacheExpires  time.Time
	agentID       string
}

func (m *Manager) playitPath() string { return filepath.Join(m.cfg.DataDir, "playit.json") }

func (m *Manager) playitImage() string {
	if m.cfg.PlayitImage != "" {
		return m.cfg.PlayitImage
	}
	return DefaultPlayitImage
}

func (m *Manager) readPlayit() (string, time.Time, error) {
	info, err := os.Stat(m.playitPath())
	if errors.Is(err, os.ErrNotExist) {
		return "", time.Time{}, nil
	}
	if err != nil {
		return "", time.Time{}, err
	}
	data, err := os.ReadFile(m.playitPath())
	if err != nil {
		return "", time.Time{}, err
	}
	var file playitFile
	if err := json.Unmarshal(data, &file); err != nil {
		return "", time.Time{}, fmt.Errorf("playit.json ist ungültig: %w", err)
	}
	return file.Secret, info.ModTime(), nil
}

// ConfigurePlayit speichert den Schlüssel und (neu) startet den Agenten.
func (m *Manager) ConfigurePlayit(ctx context.Context, secret string) error {
	secret = strings.TrimSpace(secret)
	if !playitSecret.MatchString(secret) {
		return badRequest("das ist kein gültiger playit-Schlüssel (der „Secret Key“ eines Agenten aus dem playit-Dashboard)")
	}
	m.playit.mu.Lock()
	m.playit.cachedTunnels = nil
	m.playit.agentID = ""
	m.playit.cacheExpires = time.Time{}
	m.playit.mu.Unlock()
	data, _ := json.Marshal(playitFile{Secret: secret})
	if err := os.WriteFile(m.playitPath(), append(data, '\n'), 0o600); err != nil {
		return err
	}
	return m.startPlayitLocked(ctx, true)
}

// RemovePlayit stoppt den Agenten und vergisst den Schlüssel.
func (m *Manager) RemovePlayit(ctx context.Context) error {
	m.playit.mu.Lock()
	m.playit.cachedTunnels = nil
	m.playit.agentID = ""
	m.playit.cacheExpires = time.Time{}
	m.playit.mu.Unlock()
	if err := m.rt.Remove(ctx, playitContainer); err != nil {
		return err
	}
	if err := os.Remove(m.playitPath()); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

// EnsurePlayit sorgt dafür, dass der Agent läuft, wenn ein Schlüssel eingerichtet ist (nach einem Neustart des Daemons).
func (m *Manager) EnsurePlayit(ctx context.Context) error {
	m.playit.mu.Lock()
	defer m.playit.mu.Unlock()
	return m.startPlayitLocked(ctx, false)
}

func (m *Manager) startPlayitLocked(ctx context.Context, recreate bool) error {
	secret, _, err := m.readPlayit()
	if err != nil || secret == "" {
		return err
	}
	state, err := m.rt.Inspect(ctx, playitContainer)
	if err != nil {
		return err
	}
	if state.Exists && !recreate {
		if state.Running {
			return nil
		}
		return m.rt.Start(ctx, playitContainer)
	}
	if state.Exists {
		if err := m.rt.Remove(ctx, playitContainer); err != nil {
			return err
		}
	}
	image := m.playitImage()
	if !imagePattern.MatchString(image) {
		return fmt.Errorf("ungültiges Image für playit: %q", image)
	}
	if err := m.rt.EnsureImage(ctx, image); err != nil {
		return err
	}
	if _, err := m.rt.Create(ctx, ContainerSpec{
		Name:        playitContainer,
		Image:       image,
		Env:         []string{"SECRET_KEY=" + secret},
		Network:     "host", // der Agent muss die Server über 127.0.0.1 und ihre veröffentlichten Ports erreichen
		Restart:     "unless-stopped",
		MemoryBytes: 192 << 20,
		Labels:      map[string]string{LabelService: "playit"},
	}); err != nil {
		return err
	}
	return m.rt.Start(ctx, playitContainer)
}

// PlayitStatus liest Zustand und letzte Zeilen der Ausgabe (der Schlüssel wird entfernt).
func (m *Manager) PlayitStatus(ctx context.Context) PlayitStatus {
	status := PlayitStatus{Image: m.playitImage(), Logs: []string{}}
	secret, since, err := m.readPlayit()
	if err != nil {
		status.Error = err.Error()
		return status
	}
	if secret == "" {
		return status
	}
	status.Configured = true
	status.Since = since.UnixMilli()
	state, err := m.rt.Inspect(ctx, playitContainer)
	if err != nil {
		status.Error = "Docker ist nicht erreichbar: " + err.Error()
		return status
	}
	status.Running = state.Running
	if !state.Exists {
		status.Error = "Der Agent-Container fehlt und wird neu angelegt."
		return status
	}
	logs, err := m.rt.Logs(ctx, playitContainer, playitLogLines)
	if err == nil {
		for _, line := range strings.Split(strings.ReplaceAll(logs, "\r", ""), "\n") {
			line = strings.TrimSpace(strings.ReplaceAll(line, secret, "••••"))
			if line != "" {
				status.Logs = append(status.Logs, line)
			}
		}
	}
	if status.Running {
		if tunnels, err := m.PlayitTunnels(ctx, false); err == nil {
			status.Tunnels = tunnels
		}
	}
	return status
}

func (m *Manager) playitBaseURL() string {
	m.playit.mu.Lock()
	defer m.playit.mu.Unlock()
	if m.playit.apiURL != "" {
		return m.playit.apiURL
	}
	return DefaultPlayitAPIURL
}

func (m *Manager) playitHTTPClient() *http.Client {
	m.playit.mu.Lock()
	defer m.playit.mu.Unlock()
	if m.playit.httpClient != nil {
		return m.playit.httpClient
	}
	return &http.Client{Timeout: 15 * time.Second}
}

// SetPlayitAPIForTest erlaubt Tests, API-Aufrufe an eine lokale Attrappe zu leiten.
func (m *Manager) SetPlayitAPIForTest(apiURL string, client *http.Client) {
	m.playit.mu.Lock()
	defer m.playit.mu.Unlock()
	m.playit.apiURL = apiURL
	m.playit.httpClient = client
}

type playitApiResponse struct {
	Status string          `json:"status"`
	Data   json.RawMessage `json:"data"`
}

type playitRundata struct {
	AgentID string            `json:"agent_id"`
	Tunnels []playitRawTunnel `json:"tunnels"`
}

type playitRawTunnel struct {
	ID             string `json:"id"`
	Name           string `json:"name"`
	TunnelType     string `json:"tunnel_type"`
	PortType       string `json:"port_type"`
	AssignedDomain string `json:"assigned_domain"`
	CustomDomain   string `json:"custom_domain"`
	DisplayAddress string `json:"display_address"`
	Port           struct {
		From int `json:"from"`
		To   int `json:"to"`
	} `json:"port"`
	Origin struct {
		Type string `json:"type"`
		Data struct {
			AgentID   string `json:"agent_id"`
			LocalIP   string `json:"local_ip"`
			LocalPort int    `json:"local_port"`
		} `json:"data"`
	} `json:"origin"`
	LocalIP   string `json:"local_ip"`
	LocalPort int    `json:"local_port"`
	Enabled   bool   `json:"enabled"`
	CreatedAt int64  `json:"created_at"`
}

func (r playitRawTunnel) toTunnel() PlayitTunnel {
	localIP := r.Origin.Data.LocalIP
	if localIP == "" {
		localIP = r.LocalIP
	}
	if localIP == "" {
		localIP = "127.0.0.1"
	}

	localPort := r.Origin.Data.LocalPort
	if localPort == 0 {
		localPort = r.LocalPort
	}
	if localPort == 0 {
		localPort = r.Port.To
	}

	pubAddr := r.DisplayAddress
	if pubAddr == "" {
		domain := r.CustomDomain
		if domain == "" {
			domain = r.AssignedDomain
		}
		if domain != "" {
			if r.Port.From > 0 && r.Port.From != 25565 {
				pubAddr = domain + ":" + strconv.Itoa(r.Port.From)
			} else {
				pubAddr = domain
			}
		}
	}

	return PlayitTunnel{
		ID:            r.ID,
		Name:          r.Name,
		TunnelType:    r.TunnelType,
		PortType:      r.PortType,
		LocalIP:       localIP,
		LocalPort:     localPort,
		PublicAddress: pubAddr,
		Enabled:       r.Enabled,
		CreatedAt:     r.CreatedAt,
	}
}

// PlayitTunnels holt die Liste der aktiven Tunnel dieses Agenten von playit.gg.
func (m *Manager) PlayitTunnels(ctx context.Context, force bool) ([]PlayitTunnel, error) {
	secret, _, err := m.readPlayit()
	if err != nil || secret == "" {
		return nil, badRequest("playit.gg ist auf diesem Node nicht eingerichtet")
	}

	m.playit.mu.Lock()
	if !force && m.playit.cachedTunnels != nil && time.Now().Before(m.playit.cacheExpires) {
		tunnels := append([]PlayitTunnel(nil), m.playit.cachedTunnels...)
		m.playit.mu.Unlock()
		return tunnels, nil
	}
	m.playit.mu.Unlock()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, m.playitBaseURL()+"/agents/rundata", strings.NewReader("{}"))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Agent-Key "+secret)
	req.Header.Set("Content-Type", "application/json")

	resp, err := m.playitHTTPClient().Do(req)
	if err != nil {
		return nil, fmt.Errorf("playit.gg ist nicht erreichbar: %w", err)
	}
	defer resp.Body.Close()

	respBytes, err := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
	if err != nil {
		return nil, fmt.Errorf("Antwort von playit.gg konnte nicht gelesen werden: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, parsePlayitError(respBytes, resp.StatusCode)
	}

	var rundata playitRundata
	var apiResp playitApiResponse
	if err := json.Unmarshal(respBytes, &apiResp); err == nil && apiResp.Status != "" {
		if apiResp.Status != "success" {
			return nil, parsePlayitError(respBytes, resp.StatusCode)
		}
		if err := json.Unmarshal(apiResp.Data, &rundata); err != nil {
			return nil, fmt.Errorf("unerwartete Antwort von playit.gg: %w", err)
		}
	} else if err := json.Unmarshal(respBytes, &rundata); err != nil {
		return nil, fmt.Errorf("unerwartete Antwort von playit.gg: %w", err)
	}

	tunnels := make([]PlayitTunnel, len(rundata.Tunnels))
	for i, raw := range rundata.Tunnels {
		tunnels[i] = raw.toTunnel()
	}

	m.playit.mu.Lock()
	m.playit.agentID = rundata.AgentID
	m.playit.cachedTunnels = append([]PlayitTunnel(nil), tunnels...)
	m.playit.cacheExpires = time.Now().Add(20 * time.Second)
	m.playit.mu.Unlock()

	return tunnels, nil
}

// CreatePlayitTunnel legt automatisch einen neuen Tunnel für diesen Agenten bei playit.gg an.
func (m *Manager) CreatePlayitTunnel(ctx context.Context, req CreateTunnelRequest) (PlayitTunnel, error) {
	secret, _, err := m.readPlayit()
	if err != nil || secret == "" {
		return PlayitTunnel{}, badRequest("playit.gg ist auf diesem Node nicht eingerichtet")
	}

	if req.LocalPort <= 0 || req.LocalPort > 65535 {
		return PlayitTunnel{}, badRequest("ungültiger lokaler Port")
	}
	if req.LocalIP == "" {
		req.LocalIP = "127.0.0.1"
	}
	if strings.TrimSpace(req.Name) == "" {
		req.Name = "Server"
	}

	m.playit.mu.Lock()
	agentID := m.playit.agentID
	m.playit.mu.Unlock()

	if agentID == "" {
		if _, err := m.PlayitTunnels(ctx, true); err == nil {
			m.playit.mu.Lock()
			agentID = m.playit.agentID
			m.playit.mu.Unlock()
		}
	}

	tunnelType := strings.TrimSpace(req.TunnelType)
	portType := strings.ToLower(strings.TrimSpace(req.PortType))

	switch strings.ToLower(tunnelType) {
	case "voice", "voicechat", "simple voice chat", "mc: simple voice chat":
		tunnelType = "mc: simple voice chat"
		portType = "udp"
	case "minecraft-java", "java", "server", "":
		tunnelType = "minecraft-java"
		if portType == "" {
			portType = "tcp"
		}
	case "minecraft-bedrock", "bedrock":
		tunnelType = "minecraft-bedrock"
		if portType == "" {
			portType = "udp"
		}
	default:
		if portType == "" {
			portType = "tcp"
		}
	}

	tunnel, err := m.createTunnelCall(ctx, secret, agentID, req.Name, tunnelType, portType, req.LocalIP, req.LocalPort)
	if err != nil && tunnelType == "mc: simple voice chat" {
		// Fallback auf "udp", falls playit den Preset-Namen ablehnt
		if altTunnel, altErr := m.createTunnelCall(ctx, secret, agentID, req.Name, "udp", "udp", req.LocalIP, req.LocalPort); altErr == nil {
			tunnel = altTunnel
			err = nil
		}
	}
	if err != nil {
		return PlayitTunnel{}, err
	}

	m.playit.mu.Lock()
	m.playit.cachedTunnels = nil
	m.playit.cacheExpires = time.Time{}
	m.playit.mu.Unlock()

	return tunnel, nil
}

func (m *Manager) createTunnelCall(ctx context.Context, secret, agentID, name, tunnelType, portType, localIP string, localPort int) (PlayitTunnel, error) {
	payload := map[string]any{
		"name":        name,
		"tunnel_type": tunnelType,
		"port_type":   portType,
		"port_count":  1,
		"enabled":     true,
	}
	originData := map[string]any{
		"local_ip":   localIP,
		"local_port": localPort,
	}
	if agentID != "" {
		originData["agent_id"] = agentID
	}
	payload["origin"] = map[string]any{
		"type": "agent",
		"data": originData,
	}

	bodyBytes, _ := json.Marshal(payload)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, m.playitBaseURL()+"/tunnels/create", bytes.NewReader(bodyBytes))
	if err != nil {
		return PlayitTunnel{}, err
	}
	req.Header.Set("Authorization", "Agent-Key "+secret)
	req.Header.Set("Content-Type", "application/json")

	resp, err := m.playitHTTPClient().Do(req)
	if err != nil {
		return PlayitTunnel{}, fmt.Errorf("playit.gg ist nicht erreichbar: %w", err)
	}
	defer resp.Body.Close()

	respBytes, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return PlayitTunnel{}, fmt.Errorf("Antwort von playit.gg konnte nicht gelesen werden: %w", err)
	}

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		return PlayitTunnel{}, parsePlayitError(respBytes, resp.StatusCode)
	}

	var apiResp playitApiResponse
	if err := json.Unmarshal(respBytes, &apiResp); err == nil && apiResp.Status != "" {
		if apiResp.Status != "success" {
			return PlayitTunnel{}, parsePlayitError(respBytes, resp.StatusCode)
		}
		var raw playitRawTunnel
		if err := json.Unmarshal(apiResp.Data, &raw); err != nil {
			return PlayitTunnel{}, fmt.Errorf("unerwartete Antwort von playit.gg: %w", err)
		}
		return raw.toTunnel(), nil
	}

	var raw playitRawTunnel
	if err := json.Unmarshal(respBytes, &raw); err != nil {
		return PlayitTunnel{}, fmt.Errorf("unerwartete Antwort von playit.gg: %w", err)
	}
	return raw.toTunnel(), nil
}

// DeletePlayitTunnel löscht einen Tunnel bei playit.gg.
func (m *Manager) DeletePlayitTunnel(ctx context.Context, tunnelID string) error {
	secret, _, err := m.readPlayit()
	if err != nil || secret == "" {
		return badRequest("playit.gg ist auf diesem Node nicht eingerichtet")
	}
	payload := map[string]string{"tunnel_id": tunnelID}
	bodyBytes, _ := json.Marshal(payload)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, m.playitBaseURL()+"/tunnels/delete", bytes.NewReader(bodyBytes))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Agent-Key "+secret)
	req.Header.Set("Content-Type", "application/json")

	resp, err := m.playitHTTPClient().Do(req)
	if err != nil {
		return fmt.Errorf("playit.gg ist nicht erreichbar: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNoContent {
		respBytes, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
		return parsePlayitError(respBytes, resp.StatusCode)
	}

	m.playit.mu.Lock()
	m.playit.cachedTunnels = nil
	m.playit.cacheExpires = time.Time{}
	m.playit.mu.Unlock()

	return nil
}

func parsePlayitError(data []byte, statusCode int) error {
	var errResp struct {
		Status string `json:"status"`
		Data   struct {
			Type    string `json:"type"`
			Message any    `json:"message"`
		} `json:"data"`
		Error string `json:"error"`
	}
	if err := json.Unmarshal(data, &errResp); err == nil {
		msg := ""
		if errResp.Data.Message != nil {
			switch v := errResp.Data.Message.(type) {
			case string:
				msg = v
			default:
				b, _ := json.Marshal(v)
				msg = string(b)
			}
		}
		if msg == "" {
			msg = errResp.Error
		}
		if msg != "" {
			if strings.Contains(msg, "InvalidAgentKey") || strings.Contains(msg, "AuthRequired") {
				return errors.New("der playit-Schlüssel ist ungültig oder abgelaufen (prüfe ihn unter Nodes)")
			}
			return fmt.Errorf("playit.gg: %s", msg)
		}
	}
	return fmt.Errorf("playit.gg antwortete mit HTTP %d", statusCode)
}
