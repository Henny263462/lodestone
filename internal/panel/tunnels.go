package panel

import (
	"context"
	"errors"
	"net/http"
	"regexp"
	"time"

	"github.com/lodestone-mc/lodestone/internal/node"
	"github.com/lodestone-mc/lodestone/internal/software"
)

// Tunnel (playit) pro Server: Das Panel lässt den Node bei playit.gg einen Tunnel auf den veröffentlichten Port des
// Servers anlegen und trägt die öffentliche Adresse beim Server ein. Der Tunnel wird am Namen erkannt, es gibt dafür keine
// eigene Tabelle.

// TunnelDTO ist ein Tunnel eines Servers.
type TunnelDTO struct {
	// Kind: java oder voicechat.
	Kind    string `json:"kind"`
	ID      string `json:"id"`
	Address string `json:"address"`
	// State: allocated (bereit), pending (wird eingerichtet) oder disabled.
	State string `json:"state"`
	// Region und Domain stehen nur da, wenn sie gewählt wurden.
	Region string `json:"region,omitempty"`
	Domain string `json:"domain,omitempty"`
}

// TunnelsDTO fasst zusammen, was für die Tunnel eines Servers gilt.
type TunnelsDTO struct {
	// Configured sagt, ob auf dem Node ein playit-Schlüssel hinterlegt ist.
	Configured bool        `json:"configured"`
	Tunnels    []TunnelDTO `json:"tunnels"`
	// Available sagt, für welche Arten der Server einen veröffentlichten Port hat.
	Available struct {
		Java      bool `json:"java"`
		Voicechat bool `json:"voicechat"`
	} `json:"available"`
	// Problem erklärt, warum die Liste fehlt (z. B. playit.gg nicht erreichbar).
	Problem string `json:"problem,omitempty"`
}

var tunnelSuffix = map[string]string{node.TunnelJava: "java", node.TunnelVoicechat: "voice"}

func tunnelName(serverID, kind string) string { return "ls-" + serverID + "-" + tunnelSuffix[kind] }

// tunnelPort liefert den Port auf der Maschine, auf den ein Tunnel dieser Art zeigen würde (0 = keiner veröffentlicht).
func tunnelPort(row ServerRow, view node.View, kind string) int {
	info, _ := software.Lookup(software.ID(row.Software))
	for _, mapping := range view.Ports {
		if mapping.Host == 0 {
			continue
		}
		switch {
		case kind == node.TunnelJava && mapping.Protocol == "tcp" && mapping.Container == info.DefaultPort:
			return mapping.Host
		case kind == node.TunnelVoicechat && mapping.Protocol == "udp" && (mapping.Container == voicePort || row.Role == RoleProxy && mapping.Container == proxyPort):
			return mapping.Host
		}
	}
	return 0
}

// serverView holt den Zustand des Servers vom Node.
func (p *Panel) serverView(ctx context.Context, row ServerRow) (node.View, error) {
	views, err := p.nodeViews(ctx, row.NodeID)
	if err != nil {
		return node.View{}, err
	}
	view, ok := views[row.ID]
	if !ok {
		return node.View{}, fail(http.StatusBadGateway, "der Node kennt diesen Server nicht. Prüfe, ob der Node läuft")
	}
	return view, nil
}

// serverTunnels liest die Tunnel dieses Servers vom Node.
func serverTunnels(ctx context.Context, client *NodeClient, row ServerRow) ([]TunnelDTO, error) {
	var all []node.PlayitTunnel
	if err := client.Do(ctx, http.MethodGet, "/api/playit/tunnels", nil, &all); err != nil {
		return nil, err
	}
	tunnels := []TunnelDTO{}
	for _, kind := range []string{node.TunnelJava, node.TunnelVoicechat} {
		for _, tunnel := range all {
			if tunnel.Name == tunnelName(row.ID, kind) {
				tunnels = append(tunnels, TunnelDTO{Kind: kind, ID: tunnel.ID, Address: tunnel.Address, State: tunnel.State, Region: tunnel.Region, Domain: tunnel.Domain})
			}
		}
	}
	return tunnels, nil
}

func (p *Panel) listTunnels(w http.ResponseWriter, r *http.Request) {
	row, _, _, err := p.requireServer(r, rankViewer)
	if err != nil {
		writeError(w, err)
		return
	}
	client, _, err := p.client(row.NodeID)
	if err != nil {
		writeError(w, err)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), time.Minute)
	defer cancel()
	view, err := p.serverView(ctx, row)
	if err != nil {
		writeError(w, err)
		return
	}
	result := TunnelsDTO{Tunnels: []TunnelDTO{}}
	result.Available.Java = tunnelPort(row, view, node.TunnelJava) > 0
	result.Available.Voicechat = tunnelPort(row, view, node.TunnelVoicechat) > 0

	var status node.PlayitStatus
	if err := client.Do(ctx, http.MethodGet, "/api/playit", nil, &status); err != nil {
		writeError(w, err)
		return
	}
	result.Configured = status.Configured
	if status.Configured {
		if tunnels, err := serverTunnels(ctx, client, row); err != nil {
			result.Problem = err.Error()
		} else {
			result.Tunnels = tunnels
		}
	}
	writeJSON(w, http.StatusOK, result)
}

// tunnelOptions sagt, ob das playit-Konto Regionen und eigene Domains erlaubt, und listet sie auf.
func (p *Panel) tunnelOptions(w http.ResponseWriter, r *http.Request) {
	row, _, _, err := p.requireServer(r, rankAdmin)
	if err != nil {
		writeError(w, err)
		return
	}
	client, _, err := p.client(row.NodeID)
	if err != nil {
		writeError(w, err)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), time.Minute)
	defer cancel()
	var options node.PlayitOptions
	if err := client.Do(ctx, http.MethodGet, "/api/playit/options", nil, &options); err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, options)
}

// tunnelChoice erlaubt nur Zeichen, die in Regionen- und Domain-Kennungen vorkommen.
var tunnelChoice = regexp.MustCompile(`^[a-zA-Z0-9-]{0,64}$`)

func (p *Panel) createTunnel(w http.ResponseWriter, r *http.Request) {
	row, project, _, err := p.requireServer(r, rankAdmin)
	if err != nil {
		writeError(w, err)
		return
	}
	var body struct {
		Kind     string `json:"kind"`
		Region   string `json:"region"`
		DomainID string `json:"domainId"`
	}
	if err := decode(r, &body); err != nil {
		writeError(w, err)
		return
	}
	if _, ok := tunnelSuffix[body.Kind]; !ok {
		writeError(w, fail(http.StatusBadRequest, "unbekannte Tunnel-Art (erlaubt sind java und voicechat)"))
		return
	}
	if !tunnelChoice.MatchString(body.Region) || !tunnelChoice.MatchString(body.DomainID) {
		writeError(w, fail(http.StatusBadRequest, "Region oder Domain enthalten ungültige Zeichen"))
		return
	}
	client, _, err := p.client(row.NodeID)
	if err != nil {
		writeError(w, err)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Minute)
	defer cancel()
	view, err := p.serverView(ctx, row)
	if err != nil {
		writeError(w, err)
		return
	}
	port := tunnelPort(row, view, body.Kind)
	if port == 0 {
		message := "Dieser Server hat keinen veröffentlichten Port für Minecraft. Lege den Tunnel am Proxy an."
		if body.Kind == node.TunnelVoicechat {
			message = "Für Voicechat muss der Server einen veröffentlichten UDP-Port haben. Schalte zuerst Voicechat ein."
		}
		writeError(w, fail(http.StatusConflict, message))
		return
	}
	var status node.PlayitStatus
	if err := client.Do(ctx, http.MethodGet, "/api/playit", nil, &status); err != nil {
		writeError(w, err)
		return
	}
	if !status.Configured {
		writeError(w, fail(http.StatusConflict, "playit ist auf diesem Node nicht eingerichtet. Ein Administrator hinterlegt den Schlüssel unter Nodes."))
		return
	}

	// Gibt es den Tunnel schon (z. B. weil playit die Adresse beim ersten Mal noch nicht hatte), wird er übernommen
	existing, err := serverTunnels(ctx, client, row)
	if err != nil {
		writeError(w, err)
		return
	}
	var tunnel node.PlayitTunnel
	found := false
	for _, entry := range existing {
		if entry.Kind == body.Kind {
			tunnel, found = node.PlayitTunnel{ID: entry.ID, Address: entry.Address, State: entry.State, Region: entry.Region, Domain: entry.Domain}, true
		}
	}
	if !found {
		request := node.PlayitTunnelRequest{Name: tunnelName(row.ID, body.Kind), Kind: body.Kind, LocalPort: port, Region: body.Region, DomainID: body.DomainID}
		if err := client.Do(ctx, http.MethodPost, "/api/playit/tunnels", request, &tunnel); err != nil {
			writeError(w, err)
			return
		}
	}

	if tunnel.Address != "" {
		if body.Kind == node.TunnelJava {
			row.PublicAddress = tunnel.Address
		} else {
			row.PublicVoiceAddress = tunnel.Address
		}
		if err := p.store.UpdateServer(row); err != nil {
			writeError(w, err)
			return
		}
		p.applyVoiceAddress(row, project, body.Kind)
	}
	code := http.StatusCreated
	if tunnel.Address == "" {
		code = http.StatusAccepted
	}
	writeJSON(w, code, TunnelDTO{Kind: body.Kind, ID: tunnel.ID, Address: tunnel.Address, State: tunnel.State, Region: tunnel.Region, Domain: tunnel.Domain})
}

func (p *Panel) deleteTunnel(w http.ResponseWriter, r *http.Request) {
	row, project, _, err := p.requireServer(r, rankAdmin)
	if err != nil {
		writeError(w, err)
		return
	}
	kind := r.PathValue("kind")
	if _, ok := tunnelSuffix[kind]; !ok {
		writeError(w, errNotFound)
		return
	}
	client, _, err := p.client(row.NodeID)
	if err != nil {
		writeError(w, err)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Minute)
	defer cancel()
	existing, err := serverTunnels(ctx, client, row)
	if err != nil {
		writeError(w, err)
		return
	}
	found := false
	for _, tunnel := range existing {
		if tunnel.Kind != kind {
			continue
		}
		found = true
		if err := client.Do(ctx, http.MethodDelete, "/api/playit/tunnels/"+tunnel.ID, nil, nil); err != nil {
			writeError(w, err)
			return
		}
		// Die Adresse des Tunnels gilt nicht mehr
		if kind == node.TunnelJava && row.PublicAddress == tunnel.Address {
			row.PublicAddress = ""
		}
		if kind == node.TunnelVoicechat && row.PublicVoiceAddress == tunnel.Address {
			row.PublicVoiceAddress = ""
		}
	}
	if !found {
		writeError(w, errNotFound)
		return
	}
	if err := p.store.UpdateServer(row); err != nil {
		writeError(w, err)
		return
	}
	p.applyVoiceAddress(row, project, kind)
	w.WriteHeader(http.StatusNoContent)
}

// applyVoiceAddress schreibt die Voicechat-Adresse in die Einstellungen der Server (im Hintergrund, die Antwort wartet nicht).
// In einem Netzwerk geschieht das über den Abgleich, ein einzelner Server bekommt die Änderung direkt.
func (p *Panel) applyVoiceAddress(row ServerRow, project Project, kind string) {
	if kind != node.TunnelVoicechat {
		return
	}
	p.invalidate(row.NodeID)
	if project.Kind == "network" {
		go p.afterNetworkChange(project.ID, "")
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
		defer cancel()
		if err := p.writeStandaloneVoiceHost(ctx, row); err != nil {
			p.log.Warn("Voicechat-Adresse nicht eingetragen", "server", row.Name, "error", err)
		}
	}()
}

// writeStandaloneVoiceHost trägt voice_host bei einem Server ein, der in keinem Netzwerk liegt.
func (p *Panel) writeStandaloneVoiceHost(ctx context.Context, row ServerRow) error {
	var file string
	switch {
	case IsPaperFamily(software.ID(row.Software)):
		file = "plugins/voicechat/voicechat-server.properties"
	case row.Software == "fabric":
		file = "config/voicechat/voicechat-server.properties"
	default:
		return nil
	}
	n, err := p.store.NodeByID(row.NodeID)
	if err != nil {
		return err
	}
	view, err := p.serverView(ctx, row)
	if err != nil {
		return err
	}
	_, voice := publicAddresses(n.PublicHost, row, view)
	if voice == "" {
		return nil
	}
	client := p.nodeClient(n)
	config := node.Autoconfig{
		Edits: []node.Edit{{File: file, Format: "properties", Set: []node.SetOp{
			{Path: []string{"port"}, Value: voicePort},
			{Path: []string{"voice_host"}, Value: voice},
		}}},
		Restart: true,
	}
	err = client.Do(ctx, http.MethodPut, "/api/servers/"+row.ID+"/autoconfig", config, nil)
	var nodeErr *NodeError
	if errors.As(err, &nodeErr) && nodeErr.Status == http.StatusNotFound {
		return nil
	}
	return err
}
