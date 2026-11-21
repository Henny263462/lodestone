package panel

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/lodestone-mc/lodestone/internal/node"
	"github.com/lodestone-mc/lodestone/internal/software"
)

// ---------------------------------------------------------------- Modpacks

type modpackRef struct {
	Project   string `json:"project"`
	VersionID string `json:"versionId"`
}

type resolvedPack struct {
	Info   ModpackInfo
	URL    string
	SHA1   string
	SHA512 string
}

func (p *Panel) modpackSearch(w http.ResponseWriter, r *http.Request) {
	offset, _ := strconv.Atoi(r.URL.Query().Get("offset"))
	result, err := p.modrinth.SearchModpacks(r.Context(), strings.TrimSpace(r.URL.Query().Get("q")), max(offset, 0))
	if err != nil {
		writeError(w, fail(http.StatusBadGateway, err.Error()))
		return
	}
	writeJSON(w, http.StatusOK, result)
}

// modpackVersions liefert die Fabric-Versionen eines Modpacks.
func (p *Panel) modpackVersions(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("project")
	info, err := p.modrinth.Project(r.Context(), id)
	if err != nil {
		writeError(w, fail(http.StatusBadGateway, err.Error()))
		return
	}
	if info.ProjectType != "modpack" {
		writeError(w, fail(http.StatusBadRequest, "das ist kein Modpack"))
		return
	}
	versions, err := p.modrinth.Versions(r.Context(), id, []string{"fabric"}, "")
	if err != nil {
		writeError(w, fail(http.StatusBadGateway, err.Error()))
		return
	}
	type brief struct {
		ID        string   `json:"id"`
		Name      string   `json:"name"`
		Number    string   `json:"versionNumber"`
		Type      string   `json:"type"`
		Published string   `json:"published"`
		Games     []string `json:"gameVersions"`
	}
	list := make([]brief, 0, len(versions))
	for _, version := range versions {
		list = append(list, brief{version.ID, version.Name, version.VersionNumber, version.VersionType, version.DatePublished, version.GameVersions})
	}
	writeJSON(w, http.StatusOK, map[string]any{"project": info, "link": info.Link(), "versions": list})
}

// resolveModpack prüft die gewählte Modpack-Version und liefert alles, was zum Anlegen und Installieren nötig ist.
func (p *Panel) resolveModpack(ctx context.Context, ref modpackRef) (*resolvedPack, error) {
	if ref.VersionID == "" || strings.ContainsAny(ref.VersionID+ref.Project, "/?#\\ ") {
		return nil, fail(http.StatusBadRequest, "wähle eine Version des Modpacks")
	}
	version, err := p.modrinth.Version(ctx, ref.VersionID)
	if errors.Is(err, ErrNotFound) {
		return nil, fail(http.StatusNotFound, "die Modpack-Version gibt es nicht")
	}
	if err != nil {
		return nil, fail(http.StatusBadGateway, err.Error())
	}
	info, err := p.modrinth.Project(ctx, version.ProjectID)
	if err != nil {
		return nil, fail(http.StatusBadGateway, err.Error())
	}
	if ref.Project != "" && ref.Project != info.ID && ref.Project != info.Slug {
		return nil, fail(http.StatusBadRequest, "die Version gehört nicht zu diesem Modpack")
	}
	if info.ProjectType != "modpack" {
		return nil, fail(http.StatusBadRequest, "das ist kein Modpack")
	}
	fabric := false
	for _, loader := range version.Loaders {
		fabric = fabric || loader == "fabric"
	}
	if !fabric {
		return nil, fail(http.StatusBadRequest, "Nur Fabric-Modpacks lassen sich installieren – dieses nutzt: "+strings.Join(version.Loaders, ", "))
	}
	if len(version.GameVersions) == 0 {
		return nil, fail(http.StatusBadGateway, "das Modpack nennt keine Minecraft-Version")
	}
	file, ok := version.PrimaryFile()
	if !ok || !strings.HasSuffix(strings.ToLower(file.Filename), ".mrpack") {
		return nil, fail(http.StatusBadGateway, "die Version enthält kein .mrpack")
	}
	return &resolvedPack{
		Info: ModpackInfo{ProjectID: info.ID, Slug: info.Slug, Title: info.Title, IconURL: info.IconURL, VersionID: version.ID, VersionNumber: version.VersionNumber, Minecraft: version.GameVersions[0]},
		URL:  file.URL, SHA1: file.Hashes.SHA1, SHA512: file.Hashes.SHA512,
	}, nil
}

// installModpackWhenReady wartet, bis der Server installiert ist, und schickt dem Node dann das Modpack.
func (p *Panel) installModpackWhenReady(row ServerRow, pack *resolvedPack) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Minute)
	defer cancel()
	client, _, err := p.client(row.NodeID)
	if err != nil {
		return
	}
	for attempt := 0; attempt < 400; attempt++ {
		var view node.View
		if err := client.Do(ctx, http.MethodGet, "/api/servers/"+row.ID, nil, &view); err != nil {
			var nodeErr *NodeError
			if errors.As(err, &nodeErr) && nodeErr.Status == http.StatusNotFound {
				return // inzwischen gelöscht
			}
		} else if view.State == node.StateOffline && view.Installed {
			request := node.ModpackRequest{URL: pack.URL, SHA1: pack.SHA1, SHA512: pack.SHA512}
			if err := client.Do(ctx, http.MethodPost, "/api/servers/"+row.ID+"/modpack", request, nil); err != nil {
				p.log.Warn("Modpack ließ sich nicht starten", "server", row.Name, "error", err)
			} else {
				p.log.Info("Modpack wird installiert", "server", row.Name, "modpack", pack.Info.Title, "version", pack.Info.VersionNumber)
			}
			return
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(startPoll):
		}
	}
}

// ---------------------------------------------------------------- Installierte Erweiterungen

type addonDetail struct {
	File          string `json:"file"`
	Dir           string `json:"dir"`
	Known         bool   `json:"known"`
	ProjectID     string `json:"projectId,omitempty"`
	Slug          string `json:"slug,omitempty"`
	Title         string `json:"title,omitempty"`
	Description   string `json:"description,omitempty"`
	IconURL       string `json:"iconUrl,omitempty"`
	ProjectType   string `json:"projectType,omitempty"`
	Link          string `json:"link,omitempty"`
	Downloads     int    `json:"downloads,omitempty"`
	SourceURL     string `json:"sourceUrl,omitempty"`
	IssuesURL     string `json:"issuesUrl,omitempty"`
	License       string `json:"license,omitempty"`
	VersionID     string `json:"versionId,omitempty"`
	VersionNumber string `json:"versionNumber,omitempty"`
	VersionName   string `json:"versionName,omitempty"`
	VersionType   string `json:"versionType,omitempty"`
	Published     string `json:"published,omitempty"`
	VersionLink   string `json:"versionLink,omitempty"`
}

// addonsDetails sagt zu jeder installierten Datei, welches Modrinth-Projekt und welche Version sie ist.
func (p *Panel) addonsDetails(w http.ResponseWriter, r *http.Request) {
	row, _, err := p.modrinthContext(r)
	if err != nil {
		writeError(w, err)
		return
	}
	client, _, err := p.client(row.NodeID)
	if err != nil {
		writeError(w, err)
		return
	}
	var addons []node.Addon
	if err := client.Do(r.Context(), http.MethodGet, "/api/servers/"+row.ID+"/addons", nil, &addons); err != nil {
		writeError(w, err)
		return
	}
	hashes := []string{}
	for _, addon := range addons {
		if addon.SHA1 != "" {
			hashes = append(hashes, addon.SHA1)
		}
	}
	versions, err := p.modrinth.ByHashes(r.Context(), hashes)
	if err != nil {
		writeError(w, fail(http.StatusBadGateway, err.Error()))
		return
	}
	seen := map[string]bool{}
	ids := []string{}
	for _, version := range versions {
		if !seen[version.ProjectID] {
			seen[version.ProjectID] = true
			ids = append(ids, version.ProjectID)
		}
	}
	sort.Strings(ids)
	projects, err := p.modrinth.Projects(r.Context(), ids)
	if err != nil {
		writeError(w, fail(http.StatusBadGateway, err.Error()))
		return
	}
	result := make([]addonDetail, 0, len(addons))
	for _, addon := range addons {
		detail := addonDetail{File: addon.File, Dir: addon.Dir}
		if version, ok := versions[addon.SHA1]; ok {
			project := projects[version.ProjectID]
			link := project.Link()
			detail.Known = true
			detail.ProjectID, detail.Slug, detail.Title, detail.Description, detail.IconURL = version.ProjectID, project.Slug, project.Title, project.Description, project.IconURL
			detail.ProjectType, detail.Link, detail.Downloads = project.ProjectType, link, project.Downloads
			detail.SourceURL, detail.IssuesURL, detail.License = project.SourceURL, project.IssuesURL, project.License.Name
			detail.VersionID, detail.VersionNumber, detail.VersionName, detail.VersionType, detail.Published = version.ID, version.VersionNumber, version.Name, version.VersionType, version.DatePublished
			detail.VersionLink = link + "/version/" + version.ID
		}
		result = append(result, detail)
	}
	writeJSON(w, http.StatusOK, result)
}

// addonsSwitch tauscht eine installierte Erweiterung gegen eine andere Version desselben Projekts.
func (p *Panel) addonsSwitch(w http.ResponseWriter, r *http.Request) {
	row, gameVersion, err := p.modrinthContext(r, PermAddons)
	if err != nil {
		writeError(w, err)
		return
	}
	var body struct {
		File      string `json:"file"`
		VersionID string `json:"versionId"`
	}
	if err := decode(r, &body); err != nil {
		writeError(w, err)
		return
	}
	client, _, err := p.client(row.NodeID)
	if err != nil {
		writeError(w, err)
		return
	}
	var addons []node.Addon
	if err := client.Do(r.Context(), http.MethodGet, "/api/servers/"+row.ID+"/addons", nil, &addons); err != nil {
		writeError(w, err)
		return
	}
	var current *node.Addon
	for i := range addons {
		if addons[i].File == body.File {
			current = &addons[i]
		}
	}
	if current == nil || current.SHA1 == "" {
		writeError(w, fail(http.StatusNotFound, "die Datei gibt es nicht (mehr)"))
		return
	}
	known, err := p.modrinth.ByHashes(r.Context(), []string{current.SHA1})
	if err != nil {
		writeError(w, fail(http.StatusBadGateway, err.Error()))
		return
	}
	version, found := known[current.SHA1]
	if !found {
		writeError(w, fail(http.StatusConflict, "diese Datei stammt nicht von Modrinth, sie lässt sich nicht tauschen"))
		return
	}
	target, err := p.modrinth.Version(r.Context(), body.VersionID)
	if err != nil || target.ProjectID != version.ProjectID {
		writeError(w, fail(http.StatusBadRequest, "die gewählte Version gehört nicht zu diesem Projekt"))
		return
	}
	if target.ID == version.ID {
		writeError(w, fail(http.StatusConflict, "diese Version ist schon installiert"))
		return
	}
	installed := p.installedProjects(r.Context(), client, row)
	delete(installed, version.ProjectID) // das Projekt selbst wird ersetzt, nicht übersprungen
	done, err := p.installVersion(r.Context(), client, row, current.Dir, gameVersion, version.ProjectID, target.ID, true, installed)
	switch {
	case errors.Is(err, errNoVersion):
		writeError(w, fail(http.StatusNotFound, "diese Version passt nicht zu Software und Spielversion des Servers"))
		return
	case errors.Is(err, errAlreadyInstalled):
		writeError(w, fail(http.StatusConflict, "eine Datei mit dem Namen der neuen Version liegt schon auf dem Server"))
		return
	case err != nil:
		writeError(w, err)
		return
	}
	removed := ""
	if len(done) == 0 || done[0] != current.File {
		query := url.Values{"path": {current.Dir + "/" + current.File}}
		if err := client.Do(r.Context(), http.MethodDelete, "/api/servers/"+row.ID+"/files?"+query.Encode(), nil, nil); err == nil {
			removed = current.File
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"installed": done, "removed": removed})
}

// ---------------------------------------------------------------- Öffentliche Adresse (z. B. playit)

var addressPattern = regexp.MustCompile(`^[A-Za-z0-9]([A-Za-z0-9.-]{0,251}[A-Za-z0-9])?(:[0-9]{1,5})?$`)

func (p *Panel) putPublicAddress(w http.ResponseWriter, r *http.Request) {
	row, _, _, err := p.requireServerPermission(r, PermSettings)
	if err != nil {
		writeError(w, err)
		return
	}
	var body struct {
		Address      *string `json:"address"`
		VoiceAddress *string `json:"voiceAddress"`
	}
	if err := decode(r, &body); err != nil {
		writeError(w, err)
		return
	}
	validateAddr := func(raw string) (string, error) {
		address := strings.ToLower(strings.TrimSpace(raw))
		if address == "" {
			return "", nil
		}
		portless := address
		if cut := strings.LastIndex(address, ":"); cut >= 0 {
			port, err := strconv.Atoi(address[cut+1:])
			if err != nil || port < 1 || port > 65535 {
				return "", fail(http.StatusBadRequest, "ungültiger Port in der Adresse")
			}
			portless = address[:cut]
		}
		if !addressPattern.MatchString(address) || portless == "" {
			return "", fail(http.StatusBadRequest, "ungültige Adresse (erlaubt ist z. B. mc.example.org oder abc.joinmc.link:12345)")
		}
		return address, nil
	}

	voiceChanged := false
	if body.Address != nil {
		addr, err := validateAddr(*body.Address)
		if err != nil {
			writeError(w, err)
			return
		}
		row.PublicAddress = addr
	}
	if body.VoiceAddress != nil {
		voiceAddr, err := validateAddr(*body.VoiceAddress)
		if err != nil {
			writeError(w, err)
			return
		}
		if row.PublicVoiceAddress != voiceAddr {
			voiceChanged = true
			row.PublicVoiceAddress = voiceAddr
		}
	}
	if err := p.store.UpdateServer(row); err != nil {
		writeError(w, err)
		return
	}
	if voiceChanged {
		p.syncServerVoice(r.Context(), row)
	}
	writeJSON(w, http.StatusOK, p.serverDTOs(r.Context(), []ServerRow{row})[0])
}

func (p *Panel) syncServerVoice(ctx context.Context, row ServerRow) {
	if row.Role == RoleProxy || row.Role == RoleBackend {
		_, _ = p.syncNetwork(ctx, row.ProjectID)
		return
	}
	if row.Role == RoleStandalone && row.Voicechat {
		var voiceFile string
		if IsPaperFamily(software.ID(row.Software)) {
			voiceFile = "plugins/voicechat/voicechat-server.properties"
		} else if row.Software == "fabric" {
			voiceFile = "config/voicechat/voicechat-server.properties"
		}
		if voiceFile != "" {
			config := node.Autoconfig{
				Edits: []node.Edit{{
					File:   voiceFile,
					Format: "properties",
					Create: true,
					Set: []node.SetOp{
						{Path: []string{"port"}, Value: voicePort},
						{Path: []string{"voice_host"}, Value: row.PublicVoiceAddress},
					},
				}},
			}
			client, _, err := p.client(row.NodeID)
			if err == nil {
				var answer struct {
					Pending bool `json:"pending"`
				}
				_ = client.Do(ctx, http.MethodPut, "/api/servers/"+row.ID+"/autoconfig", config, &answer)
			}
		}
	}
}

// ---------------------------------------------------------------- playit (Verwaltung des Agenten pro Node)

func (p *Panel) nodePlayit(w http.ResponseWriter, r *http.Request) {
	if err := requireAdmin(r); err != nil {
		writeError(w, err)
		return
	}
	client, _, err := p.client(r.PathValue("id"))
	if err != nil {
		writeError(w, errNotFound)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Minute)
	defer cancel()
	switch r.Method {
	case http.MethodGet:
		var status map[string]any
		if err := client.Do(ctx, http.MethodGet, "/api/playit", nil, &status); err != nil {
			writeError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, status)
	case http.MethodPut:
		var body struct {
			Secret string `json:"secret"`
		}
		if err := decode(r, &body); err != nil {
			writeError(w, err)
			return
		}
		var status map[string]any
		if err := client.Do(ctx, http.MethodPut, "/api/playit", map[string]string{"secret": body.Secret}, &status); err != nil {
			writeError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, status)
	case http.MethodDelete:
		if err := client.Do(ctx, http.MethodDelete, "/api/playit", nil, nil); err != nil {
			writeError(w, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

func (p *Panel) nodePlayitTunnels(w http.ResponseWriter, r *http.Request) {
	if err := requireAdmin(r); err != nil {
		writeError(w, err)
		return
	}
	client, _, err := p.client(r.PathValue("id"))
	if err != nil {
		writeError(w, errNotFound)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	var tunnels []node.PlayitTunnel
	if err := client.Do(ctx, http.MethodGet, "/api/playit/tunnels", nil, &tunnels); err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, tunnels)
}

func (p *Panel) nodePlayitTunnelCreate(w http.ResponseWriter, r *http.Request) {
	if err := requireAdmin(r); err != nil {
		writeError(w, err)
		return
	}
	client, _, err := p.client(r.PathValue("id"))
	if err != nil {
		writeError(w, errNotFound)
		return
	}
	var req node.CreateTunnelRequest
	if err := decode(r, &req); err != nil {
		writeError(w, err)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 45*time.Second)
	defer cancel()
	var tunnel node.PlayitTunnel
	if err := client.Do(ctx, http.MethodPost, "/api/playit/tunnels", req, &tunnel); err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, tunnel)
}

func (p *Panel) nodePlayitTunnelDelete(w http.ResponseWriter, r *http.Request) {
	if err := requireAdmin(r); err != nil {
		writeError(w, err)
		return
	}
	client, _, err := p.client(r.PathValue("id"))
	if err != nil {
		writeError(w, errNotFound)
		return
	}
	tunnelID := r.PathValue("tunnelId")
	if tunnelID == "" {
		writeError(w, fail(http.StatusBadRequest, "Tunnel-ID fehlt"))
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	if err := client.Do(ctx, http.MethodDelete, "/api/playit/tunnels/"+url.PathEscape(tunnelID), nil, nil); err != nil {
		writeError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ---------------------------------------------------------------- Server-Tunnel (automatische Erstellung über playit)

func (p *Panel) postServerTunnel(w http.ResponseWriter, r *http.Request) {
	row, _, _, err := p.requireServerPermission(r, PermNetwork)
	if err != nil {
		writeError(w, err)
		return
	}
	client, _, err := p.client(row.NodeID)
	if err != nil {
		writeError(w, errNotFound)
		return
	}
	var body struct {
		Type string `json:"type"` // "server" oder "voice"
		Port int    `json:"port"` // optional; falls nicht angegeben, wird er aus den Ports des Servers ermittelt
	}
	if r.ContentLength > 0 {
		_ = decode(r, &body)
	}
	kind := strings.ToLower(strings.TrimSpace(body.Type))
	isVoice := kind == "voice" || kind == "voicechat" || kind == "mc: simple voice chat"

	ctx, cancel := context.WithTimeout(r.Context(), 45*time.Second)
	defer cancel()

	// Node-View abfragen, um Host-Port des Servers zu ermitteln
	var view node.View
	if err := client.Do(ctx, http.MethodGet, "/api/servers/"+row.ID, nil, &view); err != nil {
		writeError(w, err)
		return
	}

	targetPort := body.Port
	if targetPort <= 0 {
		if isVoice {
			// Suche nach dem UDP-Port für Voicechat (Container-Port 24454 oder am Proxy proxyPort)
			for _, m := range view.Ports {
				if m.Protocol == "udp" && (m.Container == voicePort || (row.Role == RoleProxy && m.Container == proxyPort)) {
					targetPort = m.Host
					break
				}
			}
			if targetPort <= 0 {
				writeError(w, fail(http.StatusBadRequest, "Voicechat ist für diesen Server noch nicht aktiviert oder hat keinen UDP-Port freigegeben."))
				return
			}
		} else {
			// Suche nach dem TCP-Port für Minecraft
			for _, m := range view.Ports {
				if m.Protocol != "udp" {
					targetPort = m.Host
					break
				}
			}
			if targetPort <= 0 {
				writeError(w, fail(http.StatusBadRequest, "Dieser Server hat keinen freigegebenen Port auf dem Node."))
				return
			}
		}
	}

	tunnelReq := node.CreateTunnelRequest{
		LocalIP:   "127.0.0.1",
		LocalPort: targetPort,
	}
	if isVoice {
		tunnelReq.Name = row.Name + " (Voice)"
		tunnelReq.TunnelType = "mc: simple voice chat"
		tunnelReq.PortType = "udp"
	} else {
		tunnelReq.Name = row.Name
		tunnelReq.TunnelType = "minecraft-java"
		tunnelReq.PortType = "tcp"
	}

	var tunnel node.PlayitTunnel
	if err := client.Do(ctx, http.MethodPost, "/api/playit/tunnels", tunnelReq, &tunnel); err != nil {
		writeError(w, err)
		return
	}

	if isVoice {
		row.PublicVoiceAddress = tunnel.PublicAddress
	} else {
		row.PublicAddress = tunnel.PublicAddress
	}
	if err := p.store.UpdateServer(row); err != nil {
		writeError(w, err)
		return
	}
	if isVoice {
		p.syncServerVoice(r.Context(), row)
	}

	dto := p.serverDTOs(r.Context(), []ServerRow{row})[0]
	writeJSON(w, http.StatusCreated, map[string]any{
		"tunnel": tunnel,
		"server": dto,
	})
}

func (p *Panel) deleteServerTunnel(w http.ResponseWriter, r *http.Request) {
	row, _, _, err := p.requireServerPermission(r, PermNetwork)
	if err != nil {
		writeError(w, err)
		return
	}
	kind := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("type")))
	isVoice := kind == "voice" || kind == "voicechat"

	addrToDelete := row.PublicAddress
	if isVoice {
		addrToDelete = row.PublicVoiceAddress
	}

	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()

	// Falls ein Node-Client da ist, versuchen wir den Tunnel auf playit.gg zu finden und zu löschen
	client, _, err := p.client(row.NodeID)
	if err == nil && addrToDelete != "" {
		var tunnels []node.PlayitTunnel
		if err := client.Do(ctx, http.MethodGet, "/api/playit/tunnels", nil, &tunnels); err == nil {
			for _, t := range tunnels {
				if t.PublicAddress == addrToDelete {
					_ = client.Do(ctx, http.MethodDelete, "/api/playit/tunnels/"+url.PathEscape(t.ID), nil, nil)
					break
				}
			}
		}
	}

	if isVoice {
		row.PublicVoiceAddress = ""
	} else {
		row.PublicAddress = ""
	}
	if err := p.store.UpdateServer(row); err != nil {
		writeError(w, err)
		return
	}
	if isVoice {
		p.syncServerVoice(r.Context(), row)
	}

	dto := p.serverDTOs(r.Context(), []ServerRow{row})[0]
	writeJSON(w, http.StatusOK, map[string]any{
		"server": dto,
	})
}
