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
	"strconv"
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
	// Region ist der Name der gewählten Region (leer bei „Automatisch“).
	Region string `json:"region,omitempty"`
	// Domain ist die eigene Domain, unter der der Tunnel erreichbar ist (leer ohne eigene Domain).
	Domain string `json:"domain,omitempty"`
}

// PlayitTunnelRequest beschreibt einen neuen Tunnel.
type PlayitTunnelRequest struct {
	// Name macht den Tunnel wiederfindbar (Kleinbuchstaben, Ziffern, Bindestrich).
	Name string `json:"name"`
	// Kind: java oder voicechat.
	Kind string `json:"kind"`
	// LocalPort ist der Port auf dieser Maschine, auf den der Tunnel zeigt.
	LocalPort int `json:"localPort"`
	// Region legt fest, über welches Rechenzentrum der Tunnel läuft (leer = automatisch). Braucht playit Premium.
	Region string `json:"region,omitempty"`
	// DomainID ist die Kennung einer eigenen Domain aus dem playit-Konto (nur für Java). Braucht playit Premium.
	DomainID string `json:"domainId,omitempty"`
}

// PlayitRegion ist eine Region, in der playit Tunnel betreibt.
type PlayitRegion struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// PlayitDomain ist eine Domain im playit-Konto.
type PlayitDomain struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	External bool   `json:"external"`
	// Used sagt, dass die Domain schon auf etwas zeigt und deshalb nicht noch einmal vergeben werden kann.
	Used bool `json:"used"`
}

// PlayitOptions fasst zusammen, was das playit-Konto beim Anlegen eines Tunnels erlaubt.
type PlayitOptions struct {
	// Premium sagt, ob das Konto Regionen und eigene Domains nutzen darf.
	Premium bool           `json:"premium"`
	Regions []PlayitRegion `json:"regions"`
	Domains []PlayitDomain `json:"domains"`
	// DomainsProblem erklärt, warum die Domains fehlen (z. B. playit hat die Liste abgelehnt).
	DomainsProblem string `json:"domainsProblem,omitempty"`
}

// playitRegions sind die Regionen, die Lodestone zur Wahl anbietet („global“ ist die automatische Wahl und steht nicht darin).
var playitRegions = []PlayitRegion{
	{"europe", "Europa"}, {"germany", "Deutschland"}, {"united-kingdom", "Vereinigtes Königreich"}, {"sweden", "Schweden"},
	{"poland", "Polen"}, {"romania", "Rumänien"},
	{"north-america", "Nordamerika"}, {"new-york", "New York"}, {"chicago-illinois", "Chicago"}, {"dallas-texas", "Dallas"},
	{"denver-colorado", "Denver"}, {"los-angeles-california", "Los Angeles"}, {"seattle-washington", "Seattle"},
	{"south-america", "Südamerika"}, {"chile", "Chile"},
	{"asia", "Asien"}, {"japan", "Japan"}, {"india", "Indien"}, {"australia", "Australien"},
}

// playitRegionName übersetzt die Kennung einer Region in ihren Namen ("" für die automatische Wahl).
func playitRegionName(id string) string {
	if id == "" || id == "global" {
		return ""
	}
	for _, region := range playitRegions {
		if region.ID == id {
			return region.Name
		}
	}
	return id
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
	if secret != "" { // die Verbindung mit playit (playit_claim.go) läuft noch ohne Schlüssel
		request.Header.Set("Authorization", "Agent-Key "+secret)
	}
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
	_, code := playitErrorInfo(data)
	return code
}

// playitErrorInfo zerlegt die Daten einer Fehlerantwort in Art („auth“, „validation“, „internal“ …) und Meldung.
// Bei „internal“ ist die Meldung die Referenz (trace_id), die der Support von playit braucht.
func playitErrorInfo(data json.RawMessage) (kind, message string) {
	var text string
	if json.Unmarshal(data, &text) == nil {
		return "", text
	}
	var object struct {
		Type    string          `json:"type"`
		Message json.RawMessage `json:"message"`
	}
	if json.Unmarshal(data, &object) != nil {
		return "", ""
	}
	if json.Unmarshal(object.Message, &text) == nil {
		return object.Type, text
	}
	var trace struct {
		TraceID string `json:"trace_id"`
	}
	if json.Unmarshal(object.Message, &trace) == nil && trace.TraceID != "" {
		return object.Type, trace.TraceID
	}
	return object.Type, ""
}

// playitDefaultAgent erklärt, dass der Schlüssel zu einem Standard-Agenten gehört, der keine Tunnel per Schnittstelle anlegen darf.
func playitDefaultAgent() error {
	return badRequest("der Schlüssel gehört zu einem Standard-Agenten, der keine Tunnel anlegen darf. Lege im playit-Dashboard einen eigenen Agenten an und hinterlege dessen Secret Key unter Nodes")
}

func playitFailure(data json.RawMessage) error {
	code := playitCode(data)
	switch code {
	case "RequiresVerifiedAccount":
		return badRequest("das playit-Konto ist noch nicht bestätigt. Bestätige deine E-Mail-Adresse bei playit.gg")
	case "RegionRequiresPlayitPremium":
		return badRequest("diese Region erlaubt playit nur mit Premium. Wähle „Automatisch“ oder hole dir playit Premium")
	case "RegionNotSupported":
		return badRequest("playit bietet diese Region für den Tunnel nicht an. Wähle eine andere oder „Automatisch“")
	case "RequiresPlayitPremium", "PublicPortRequiresPlayitPremium":
		return badRequest("diesen Tunnel erlaubt playit nur mit Premium")
	case "InvalidHostnameId":
		return badRequest("playit kennt diese Domain nicht (mehr). Lade die Auswahl neu")
	case "HostnameHasTunnelTypeTarget":
		return badRequest("diese Domain zeigt bei playit schon auf einen anderen Tunnel. Wähle eine freie Domain")
	case "AgentNotFound", "InvalidAgentId":
		return badRequest("playit kennt diesen Agenten nicht. Hinterlege den Schlüssel unter Nodes neu")
	case "AgentVersionTooOld":
		return badRequest("der playit-Agent ist zu alt. Hinterlege den Schlüssel unter Nodes neu, damit er aktualisiert wird")
	case "DefaultAgentNotSupported":
		return playitDefaultAgent()
	case "TunnelNotFound":
		return badRequest("diesen Tunnel gibt es bei playit nicht (mehr)")
	case "CodeExpired", "CodeNotFound", "InvalidCode", "NotSetup":
		return errClaimExpired
	case "UserRejected":
		return errClaimRejected
	case "NotAccepted":
		return errClaimPending
	}
	if code == "" || len(code) > 60 {
		code = "unbekannt"
	}
	return fmt.Errorf("%w: playit hat die Anfrage abgelehnt (%s). Prüfe im playit-Dashboard, ob das Tunnel-Limit erreicht ist", errUpstream, code)
}

// playitError übersetzt Antworten mit dem Status „error“. Nur ein echter interner Fehler von playit wird so benannt;
// alles andere (Schlüssel, Rechte, Anfrage) bekommt eine eigene Meldung, damit man weiß, was zu tun ist.
func playitError(data json.RawMessage) error {
	kind, message := playitErrorInfo(data)
	switch kind {
	case "auth":
		switch message {
		case "AgentNotSelfManaged", "DefaultAgentBlocked":
			return playitDefaultAgent()
		case "EmailMustBeVerified", "GuestAccountNotAllowed":
			return badRequest("das playit-Konto ist noch nicht bestätigt. Bestätige deine E-Mail-Adresse bei playit.gg")
		case "NotAllowedWithReadOnly":
			return badRequest("dieser playit-Schlüssel darf Tunnel nur lesen, nicht anlegen (so sind Schlüssel aus dem playit-Dashboard). Klicke unter Nodes auf „Mit playit verbinden“, dann bekommt der Node einen Schlüssel, der Tunnel anlegen darf")
		case "ScopeNotAllowed", "SelfManagedAgentCanOnlyAffectSelf", "AccountNotAuthorized", "AdminOnly":
			return badRequest("der playit-Schlüssel darf das nicht (%s). Lege unter Nodes den Secret Key des Agenten fest, dem die Tunnel gehören", message)
		}
		return badRequest("playit lehnt den Schlüssel ab. Lege unter Nodes einen gültigen Secret Key eines Agenten fest")
	case "validation":
		return fmt.Errorf("%w: playit versteht die Anfrage nicht (%s). Bitte melde das als Fehler in Lodestone", errUpstream, limitText(message))
	case "path-not-found":
		return fmt.Errorf("%w: diese Schnittstelle gibt es bei playit nicht (mehr). Bitte melde das als Fehler in Lodestone", errUpstream)
	case "internal":
		if message != "" {
			return fmt.Errorf("%w: interner Fehler bei playit (Referenz %s). Versuche es später noch einmal", errUpstream, limitText(message))
		}
		return fmt.Errorf("%w: interner Fehler bei playit. Versuche es später noch einmal", errUpstream)
	}
	if kind == "" {
		kind = "unbekannt"
	}
	return fmt.Errorf("%w: unerwartete Fehlerantwort (%s %s)", errUpstream, limitText(kind), limitText(message))
}

// limitText kürzt Texte aus fremden Antworten, bevor sie in einer Meldung landen.
func limitText(text string) string {
	if len(text) > 80 {
		return text[:80] + "…"
	}
	return text
}

// playitRunData ist, was playit über den Agenten und das Konto sagt.
type playitRunData struct {
	AgentID         string `json:"agent_id"`
	AccountFeatures struct {
		RegionalTunnels bool `json:"regional_tunnels"`
	} `json:"account_features"`
}

// playitRun holt die Laufdaten des Agenten.
func (m *Manager) playitRun(ctx context.Context, secret string) (playitRunData, error) {
	data, err := m.playitCall(ctx, secret, "/agents/rundata", struct{}{})
	if err != nil {
		return playitRunData{}, err
	}
	var run playitRunData
	if json.Unmarshal(data, &run) != nil || !playitTunnelID.MatchString(run.AgentID) {
		return playitRunData{}, fmt.Errorf("%w: die Kennung des Agenten fehlt in der Antwort", errUpstream)
	}
	return run, nil
}

// playitSecret liest den Schlüssel oder erklärt, dass es keinen gibt.
func (m *Manager) playitSecret() (string, error) {
	secret, _, err := m.readPlayit()
	if err != nil {
		return "", err
	}
	if secret == "" {
		return "", badRequest("playit ist auf diesem Node noch nicht eingerichtet. Hinterlege zuerst unter Nodes den Secret Key")
	}
	return secret, nil
}

// playitSession liest den Schlüssel und holt die Kennung des Agenten.
func (m *Manager) playitSession(ctx context.Context) (secret, agentID string, err error) {
	secret, err = m.playitSecret()
	if err != nil {
		return "", "", err
	}
	run, err := m.playitRun(ctx, secret)
	if err != nil {
		return "", "", err
	}
	return secret, run.AgentID, nil
}

type playitTunnelData struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	TunnelType string `json:"tunnel_type"`
	Region     string `json:"region"`
	Domain     *struct {
		Name string `json:"name"`
	} `json:"domain"`
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
	tunnel := PlayitTunnel{ID: t.ID, Name: t.Name, State: t.Alloc.Status, Region: playitRegionName(t.Region)}
	if tunnel.State != "allocated" && tunnel.State != "disabled" {
		tunnel.State = "pending"
	}
	if t.Domain != nil {
		tunnel.Domain = t.Domain.Name
	}
	switch data := t.Alloc.Data; {
	case tunnel.State != "allocated":
	case tunnel.Domain != "" && t.TunnelType == "minecraft-java":
		tunnel.Address = tunnel.Domain // Minecraft findet den Port über den SRV-Eintrag der Domain
	case tunnel.Domain != "" && data.PortStart > 0:
		tunnel.Address = fmt.Sprintf("%s:%d", tunnel.Domain, data.PortStart)
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
	if request.Region == "global" {
		request.Region = ""
	}
	if request.Region != "" && playitRegionName(request.Region) == request.Region {
		return PlayitTunnel{}, badRequest("unbekannte Region")
	}
	if request.DomainID != "" {
		switch {
		case !playitTunnelID.MatchString(request.DomainID):
			return PlayitTunnel{}, badRequest("die Kennung der Domain ist ungültig")
		case request.Kind != TunnelJava:
			return PlayitTunnel{}, badRequest("eine eigene Domain gibt es nur für den Minecraft-Tunnel")
		case request.Region != "":
			return PlayitTunnel{}, badRequest("Region und Domain schließen sich aus: Die Region einer Domain legt sie bei playit fest")
		}
	}
	if request.Region != "" {
		body["alloc"] = map[string]any{"type": "region", "details": map[string]any{"region": request.Region}}
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
	path := "/tunnels/create"
	if request.DomainID != "" {
		// Eine eigene Domain lässt sich nur mit der neueren Schnittstelle zuweisen
		path = "/v1/tunnels/create"
		body = map[string]any{
			"name":    request.Name,
			"enabled": true,
			"ports":   map[string]any{"type": "tunnel-type", "details": "minecraft-java"},
			"alloc":   map[string]any{"type": "hostname", "details": map[string]any{"hostname_id": request.DomainID}},
			"origin": map[string]any{"type": "agent", "data": map[string]any{
				"agent_id": agentID,
				"config": map[string]any{"fields": []map[string]string{
					{"name": "local_ip", "value": "127.0.0.1"},
					{"name": "local_port", "value": strconv.Itoa(request.LocalPort)},
				}},
			}},
		}
	} else {
		body["origin"] = map[string]any{"type": "agent", "data": map[string]any{
			"agent_id": agentID, "local_ip": "127.0.0.1", "local_port": request.LocalPort,
		}}
	}
	data, err := m.playitCall(ctx, secret, path, body)
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

// PlayitOptions sagt, ob das Konto Premium-Funktionen hat, und listet die wählbaren Regionen und die Domains des Kontos.
func (m *Manager) PlayitOptions(ctx context.Context) (PlayitOptions, error) {
	secret, err := m.playitSecret()
	if err != nil {
		return PlayitOptions{}, err
	}
	run, err := m.playitRun(ctx, secret)
	if err != nil {
		return PlayitOptions{}, err
	}
	options := PlayitOptions{Premium: run.AccountFeatures.RegionalTunnels, Regions: playitRegions, Domains: []PlayitDomain{}}
	data, err := m.playitCall(ctx, secret, "/domains/list", struct{}{})
	if err == nil {
		var list struct {
			Domains []struct {
				ID         string          `json:"id"`
				Name       string          `json:"name"`
				IsExternal bool            `json:"is_external"`
				Target     json.RawMessage `json:"target"`
			} `json:"domains"`
		}
		if json.Unmarshal(data, &list) != nil {
			err = fmt.Errorf("%w: die Domainliste ließ sich nicht lesen", errUpstream)
		}
		for _, domain := range list.Domains {
			if !playitTunnelID.MatchString(domain.ID) || domain.Name == "" {
				continue
			}
			used := len(domain.Target) > 0 && string(domain.Target) != "null"
			options.Domains = append(options.Domains, PlayitDomain{ID: domain.ID, Name: domain.Name, External: domain.IsExternal, Used: used})
		}
	}
	if err != nil {
		options.DomainsProblem = err.Error()
	}
	return options, nil
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
