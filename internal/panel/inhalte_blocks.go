package panel

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/lodestone-mc/lodestone/internal/node"
	"github.com/lodestone-mc/lodestone/internal/software"
)

// ---------------------------------------------------------------- Modpacks, Proxy-Regeln, Ansicht

// modpackLoaders sind die Loader, deren Modrinth-Modpacks Lodestone installieren kann, in dieser Vorrangfolge.
// Quilt fehlt bewusst: Lodestone bietet Quilt nicht als Server-Software an.
var modpackLoaders = []string{"fabric", "neoforge", "forge"}

func modpackFacets() []string {
	facets := make([]string, len(modpackLoaders))
	for i, loader := range modpackLoaders {
		facets[i] = "categories:" + loader
	}
	return facets
}

// modpackSoftware wählt die Server-Software für die Loader einer Modpack-Version.
func modpackSoftware(loaders []string) (string, error) {
	for _, want := range modpackLoaders {
		if containsString(loaders, want) {
			return want, nil
		}
	}
	if containsString(loaders, "quilt") {
		return "", fail(http.StatusBadRequest, "Quilt-Modpacks lassen sich nicht installieren, weil Lodestone Quilt nicht als Server-Software anbietet. Wähle ein Modpack für Fabric, NeoForge oder Forge.")
	}
	return "", fail(http.StatusBadRequest, "Dieses Modpack nutzt "+strings.Join(loaders, ", ")+". Installieren lassen sich Modpacks für Fabric, NeoForge und Forge.")
}

// backendRefusal erklärt, warum eine Software nicht hinter Velocity laufen kann.
func backendRefusal(info software.Info) string {
	if info.ID == software.Forge || info.ID == software.NeoForge {
		return info.Name + " kann in Lodestone nicht hinter Velocity laufen: Dafür braucht der Server einen Forwarding-Mod und der Proxy ein eigenes Plugin, " +
			"und beides funktioniert nicht für alle Versionen zuverlässig. Lege den Server als eigenes Server-Projekt an."
	}
	return info.Name + " kann nicht hinter Velocity laufen (Paper, Purpur, Folia und Fabric gehen)"
}

// redactView entfernt den Inhalt geschriebener Dateien (Forwarding-Geheimnis, Floodgate-Schlüssel) aus der Ansicht,
// bevor sie an den Browser geht. Der Zwischenspeicher der Ansichten bleibt unverändert.
func redactView(view node.View) node.View {
	if view.Autoconfig != nil {
		copied := *view.Autoconfig
		copied.Writes = make([]node.FileWrite, len(view.Autoconfig.Writes))
		for i, write := range view.Autoconfig.Writes {
			copied.Writes[i] = node.FileWrite{Path: write.Path}
		}
		view.Autoconfig = &copied
	}
	return view
}

// ---------------------------------------------------------------- Bausteine

// Bausteine sind fertige Erweiterungen, die Lodestone samt Port und Konfiguration einrichtet (wie Simple Voice Chat).
const (
	BlockGeyser    = "geyser"
	BlockFloodgate = "floodgate"
	BlockBlueMap   = "bluemap"

	// bedrockPort ist der Standard-Port von Bedrock (Geyser hört über TCP und UDP darauf).
	bedrockPort = 19132
	// blueMapPort ist der Port des eingebauten Webservers von BlueMap.
	blueMapPort = 8100
)

// BlockDTO beschreibt einen Baustein für die Oberfläche.
type BlockDTO struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Available   bool   `json:"available"`
	Reason      string `json:"reason,omitempty"`
	Enabled     bool   `json:"enabled"`
	// Managed ist "network", wenn der Baustein über das Netzwerk geschaltet wird.
	Managed string `json:"managed,omitempty"`
	Address string `json:"address,omitempty"`
	URL     string `json:"url,omitempty"`
	Note    string `json:"note,omitempty"`
}

func geyserPlatform(softwareName string) string {
	switch softwareName {
	case "paper", "purpur":
		return "spigot"
	case "velocity":
		return "velocity"
	}
	return ""
}

func geyserConfigFile(softwareName string) string {
	switch geyserPlatform(softwareName) {
	case "spigot":
		return "plugins/Geyser-Spigot/config.yml"
	case "velocity":
		return "plugins/Geyser-Velocity/config.yml"
	}
	return ""
}

// floodgateDir ist der Ordner von Floodgate (dort liegt key.pem); leer, wenn Floodgate dort nicht läuft.
func floodgateDir(softwareName string) string {
	switch softwareName {
	case "paper", "purpur", "velocity":
		return "plugins/floodgate"
	case "fabric":
		return "config/floodgate"
	}
	return ""
}

func blueMapConfigFile(softwareName string) string {
	switch softwareName {
	case "paper", "purpur", "folia":
		return "plugins/BlueMap/core.conf"
	case "fabric", "forge", "neoforge":
		return "config/bluemap/core.conf"
	}
	return ""
}

// newFloodgateKey erzeugt den gemeinsamen Schlüssel für Geyser und Floodgate. Floodgate liest key.pem als rohen
// AES-Schlüssel; 16 druckbare Zeichen sind ein gültiger AES-128-Schlüssel (96 Bit Zufall) und lassen sich als Text
// über die Autoconfig schreiben.
func newFloodgateKey() (string, error) {
	buffer := make([]byte, 12)
	if _, err := rand.Read(buffer); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(buffer), nil
}

// mappingHost ist der Port auf der Maschine für einen Container-Port (0, wenn es keinen gibt).
func mappingHost(ports []node.PortMapping, container int, protocol string) int {
	for _, mapping := range ports {
		if mapping.Container == container && mapping.Protocol == protocol {
			return mapping.Host
		}
	}
	return 0
}

func (p *Panel) serverBlockDTOs(ctx context.Context, row ServerRow, project Project) ([]BlockDTO, error) {
	blocks, err := p.store.ServerBlocks(row.ID)
	if err != nil {
		return nil, err
	}
	host := ""
	if n, err := p.store.NodeByID(row.NodeID); err == nil {
		host = n.PublicHost
	}
	var ports []node.PortMapping
	if views, err := p.nodeViews(ctx, row.NodeID); err == nil {
		ports = views[row.ID].Ports
	}
	geyser := BlockDTO{ID: BlockGeyser, Name: "Geyser + Floodgate", Description: "Bedrock-Spieler (Handy, Konsole, Windows) können mitspielen, ohne ein Java-Konto zu haben."}
	_, geyser.Enabled = blocks[BlockGeyser]
	switch {
	case project.Kind == "network":
		geyser.Managed = "network"
		geyser.Reason = "Im Netzwerk schaltest du Geyser im Projekt unter „Netzwerk“ ein."
		if _, err := p.store.ProjectBlockByName(project.ID, BlockGeyser); err == nil {
			_, floodgate := blocks[BlockFloodgate]
			geyser.Enabled = geyser.Enabled || floodgate
		}
	case geyserPlatform(row.Software) == "":
		geyser.Reason = "Geyser gibt es in Lodestone für Paper, Purpur und Velocity."
	default:
		geyser.Available = true
	}
	if block, ok := blocks[BlockGeyser]; ok && host != "" {
		if port := mappingHost(ports, block.Port, "udp"); port != 0 {
			geyser.Address = host + ":" + strconv.Itoa(port)
		}
	}
	blueMap := BlockDTO{ID: BlockBlueMap, Name: "BlueMap", Description: "Eine 3D-Karte der Welt im Browser. Lodestone veröffentlicht den Port der Karte und erlaubt BlueMap, die nötigen Minecraft-Dateien von Mojang zu laden."}
	_, blueMap.Enabled = blocks[BlockBlueMap]
	if blueMapConfigFile(row.Software) == "" {
		blueMap.Reason = "BlueMap gibt es für Paper, Purpur, Folia, Fabric, Forge und NeoForge."
	} else {
		blueMap.Available = true
	}
	if blueMap.Enabled && host != "" {
		if port := mappingHost(ports, blueMapPort, "tcp"); port != 0 {
			blueMap.URL = "http://" + host + ":" + strconv.Itoa(port) + "/"
		}
	}
	return []BlockDTO{geyser, blueMap}, nil
}

func (p *Panel) listServerBlocks(w http.ResponseWriter, r *http.Request) {
	row, project, _, err := p.requireServerPermission(r)
	if err != nil {
		writeError(w, err)
		return
	}
	list, err := p.serverBlockDTOs(r.Context(), row, project)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, list)
}

// offlineView holt die Ansicht des Servers und verlangt, dass er aus ist (neue Ports gehen nur dann).
func (p *Panel) offlineView(ctx context.Context, client *NodeClient, row ServerRow) (node.View, error) {
	var view node.View
	if err := client.Do(ctx, http.MethodGet, "/api/servers/"+row.ID, nil, &view); err != nil {
		return view, err
	}
	if view.State != node.StateOffline {
		return view, fail(http.StatusConflict, "„"+row.Name+"“ muss dafür ausgeschaltet sein, weil sich seine Ports ändern.")
	}
	return view, nil
}

// changePorts nimmt Ports weg (alle mit dem Container-Port remove) und fügt add hinzu.
func (p *Panel) changePorts(ctx context.Context, client *NodeClient, row ServerRow, view node.View, remove int, add []node.PortMapping) error {
	ports := []node.PortMapping{}
	for _, mapping := range view.Ports {
		if remove == 0 || mapping.Container != remove {
			ports = append(ports, mapping)
		}
	}
	ports = append(ports, add...)
	patch := node.Patch{Ports: &ports}
	if len(ports) == 0 {
		noPublish := true
		patch.NoPublish = &noPublish
	}
	err := client.Do(ctx, http.MethodPatch, "/api/servers/"+row.ID, patch, nil)
	var nodeErr *NodeError
	if errors.As(err, &nodeErr) && nodeErr.Status == http.StatusConflict {
		return fail(http.StatusConflict, "Die Ports von „"+row.Name+"“ lassen sich gerade nicht ändern: "+nodeErr.Message)
	}
	p.invalidate(row.NodeID)
	return err
}

// freeBedrockPort sucht ab 19132 einen Port, den kein anderer Server des Nodes benutzt. Container- und Host-Port sind
// gleich, damit Geyser über TCP und UDP auf demselben Port erreichbar ist.
func (p *Panel) freeBedrockPort(ctx context.Context, nodeID, self string) (int, error) {
	views, err := p.nodeViews(ctx, nodeID)
	if err != nil {
		return 0, err
	}
	used := map[int]bool{}
	for id, view := range views {
		if id == self {
			continue
		}
		for _, mapping := range view.Ports {
			used[mapping.Host] = true
		}
	}
	for port := bedrockPort; port < bedrockPort+100; port++ {
		if !used[port] {
			return port, nil
		}
	}
	return 0, fail(http.StatusConflict, "Auf dem Node ist kein Bedrock-Port zwischen 19132 und 19231 frei.")
}

// installGeyserMC lädt Projekte von GeyserMC (geyser, floodgate) mit Prüfsumme nach plugins/. Dateien, die schon da
// waren, gehören nicht Lodestone und werden nicht zurückgegeben (sie bleiben beim Ausschalten liegen).
func (p *Panel) installGeyserMC(ctx context.Context, client *NodeClient, row ServerRow, platform string, projects ...string) ([]string, error) {
	files := []string{}
	for _, project := range projects {
		file, _, err := p.content.geyser.latest(ctx, project, platform)
		if err != nil {
			return files, err
		}
		name, ok := safeFilename(file.FileName)
		if !ok {
			return files, fail(http.StatusBadGateway, "GeyserMC liefert keinen gültigen Dateinamen")
		}
		path := "plugins/" + name
		err = p.pull(ctx, client, row.ID, node.PullRequest{URL: file.URL, Path: path, SHA256: file.SHA256})
		switch {
		case errors.Is(err, errAlreadyInstalled):
		case err != nil:
			return files, err
		default:
			files = append(files, path)
		}
	}
	return files, nil
}

func (p *Panel) removeFiles(ctx context.Context, client *NodeClient, row ServerRow, files []string) {
	for _, file := range files {
		query := url.Values{"path": {file}}
		if err := client.Do(ctx, http.MethodDelete, "/api/servers/"+row.ID+"/files?"+query.Encode(), nil, nil); err != nil {
			p.log.Warn("Datei eines Bausteins nicht gelöscht", "server", row.Name, "file", file, "error", err)
		}
	}
}

func (p *Panel) putServerBlock(w http.ResponseWriter, r *http.Request) {
	row, project, _, err := p.requireServerPermission(r, PermSettings, PermAddons)
	if err != nil {
		writeError(w, err)
		return
	}
	var body struct {
		Enabled bool `json:"enabled"`
	}
	if err := decode(r, &body); err != nil {
		writeError(w, err)
		return
	}
	name := r.PathValue("block")
	switch {
	case name == BlockGeyser && project.Kind == "network":
		writeError(w, fail(http.StatusConflict, "Im Netzwerk schaltest du Geyser im Projekt unter „Netzwerk“ ein: Geyser kommt dann auf den Proxy, Floodgate auf alle Server."))
		return
	case name == BlockGeyser && geyserPlatform(row.Software) == "":
		writeError(w, fail(http.StatusBadRequest, "Geyser gibt es in Lodestone für Paper, Purpur und Velocity."))
		return
	case name == BlockBlueMap && blueMapConfigFile(row.Software) == "":
		writeError(w, fail(http.StatusBadRequest, "BlueMap gibt es für Paper, Purpur, Folia, Fabric, Forge und NeoForge."))
		return
	case name != BlockGeyser && name != BlockBlueMap:
		writeError(w, fail(http.StatusNotFound, "diesen Baustein gibt es nicht"))
		return
	}
	blocks, err := p.store.ServerBlocks(row.ID)
	if err != nil {
		writeError(w, err)
		return
	}
	current, enabled := blocks[name]
	if enabled != body.Enabled {
		client, _, err := p.client(row.NodeID)
		if err != nil {
			writeError(w, err)
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 5*time.Minute)
		defer cancel()
		view, err := p.offlineView(ctx, client, row)
		if err != nil {
			writeError(w, err)
			return
		}
		if body.Enabled {
			err = p.enableServerBlock(ctx, client, row, view, name)
		} else {
			if err = p.changePorts(ctx, client, row, view, current.Port, nil); err == nil {
				p.removeFiles(ctx, client, row, current.Files)
				err = p.store.DeleteServerBlock(row.ID, name)
			}
		}
		if err != nil {
			writeError(w, err)
			return
		}
		if project.Kind == "network" {
			_, err = p.syncNetwork(ctx, project.ID)
		} else {
			err = p.applyServerAutoconfig(ctx, client, row)
		}
		if err != nil {
			p.log.Warn("Konfiguration der Bausteine nicht übertragen", "server", row.Name, "error", err)
		}
	}
	list, err := p.serverBlockDTOs(r.Context(), row, project)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, list)
}

// enableServerBlock lädt die Dateien eines Bausteins und veröffentlicht seinen Port.
func (p *Panel) enableServerBlock(ctx context.Context, client *NodeClient, row ServerRow, view node.View, name string) error {
	block := ServerBlock{ServerID: row.ID, Block: name}
	var add []node.PortMapping
	switch name {
	case BlockGeyser:
		port, err := p.freeBedrockPort(ctx, row.NodeID, row.ID)
		if err != nil {
			return err
		}
		files, err := p.installGeyserMC(ctx, client, row, geyserPlatform(row.Software), "geyser", "floodgate")
		if err != nil {
			p.removeFiles(ctx, client, row, files)
			return err
		}
		block.Port, block.Files = port, files
		add = []node.PortMapping{{Container: port, Host: port, Protocol: "tcp"}, {Container: port, Host: port, Protocol: "udp"}}
	case BlockBlueMap:
		names, err := p.installFromModrinth(ctx, row, "bluemap", "", true)
		if err != nil && !errors.Is(err, errAlreadyInstalled) {
			if errors.Is(err, errNoVersion) {
				return fail(http.StatusNotFound, "BlueMap gibt es (noch) nicht für diese Software und Minecraft-Version.")
			}
			return err
		}
		for _, file := range names {
			block.Files = append(block.Files, addonDir(row.Software)+"/"+file)
		}
		block.Port = blueMapPort
		add = []node.PortMapping{{Container: blueMapPort, Protocol: "tcp"}}
	}
	if err := p.changePorts(ctx, client, row, view, block.Port, add); err != nil {
		p.removeFiles(ctx, client, row, block.Files)
		return err
	}
	return p.store.SetServerBlock(block)
}

// blockEdits sind die Dateiänderungen der Bausteine eines Servers. key ist der Floodgate-Schlüssel, wenn Geyser im
// Netzwerk eingeschaltet ist.
func blockEdits(row ServerRow, blocks map[string]ServerBlock, key string) ([]node.Edit, []node.FileWrite) {
	var edits []node.Edit
	var writes []node.FileWrite
	if block, ok := blocks[BlockGeyser]; ok && block.Port != bedrockPort && geyserConfigFile(row.Software) != "" {
		edits = append(edits, node.Edit{File: geyserConfigFile(row.Software), Format: "yaml", Set: []node.SetOp{{Path: []string{"bedrock", "port"}, Value: block.Port}}})
	}
	if _, ok := blocks[BlockBlueMap]; ok && blueMapConfigFile(row.Software) != "" {
		edits = append(edits, node.Edit{File: blueMapConfigFile(row.Software), Format: "hocon", Set: []node.SetOp{{Path: []string{"accept-download"}, Value: true}}})
	}
	if key != "" && floodgateDir(row.Software) != "" {
		switch row.Role {
		case RoleProxy:
			// Der Proxy schickt die Daten der Bedrock-Spieler an die Server, die sie mit demselben Schlüssel lesen
			edits = append(edits, node.Edit{File: "plugins/floodgate/config.yml", Format: "yaml", Set: []node.SetOp{{Path: []string{"send-floodgate-data"}, Value: true}}})
			writes = append(writes, node.FileWrite{Path: "plugins/floodgate/key.pem", Content: key})
		case RoleBackend:
			if _, ok := blocks[BlockFloodgate]; ok {
				writes = append(writes, node.FileWrite{Path: floodgateDir(row.Software) + "/key.pem", Content: key})
			}
		}
	}
	return edits, writes
}

// networkFloodgateKey liefert den Schlüssel, wenn Geyser im Netzwerk eingeschaltet ist.
func (p *Panel) networkFloodgateKey(projectID string) (string, bool) {
	block, err := p.store.ProjectBlockByName(projectID, BlockGeyser)
	if err != nil {
		return "", false
	}
	key, err := p.unseal(block.Secret)
	if err != nil {
		p.log.Warn("Floodgate-Schlüssel nicht lesbar", "project", projectID, "error", err)
		return "", true
	}
	return key, true
}

// addBlockConfigs ergänzt die Autoconfig eines Netzwerks um die Bausteine der Server.
func (p *Panel) addBlockConfigs(_ context.Context, project Project, servers []NetServer, configs map[string]node.Autoconfig) {
	key, _ := p.networkFloodgateKey(project.ID)
	for _, server := range servers {
		config, ok := configs[server.Row.ID]
		if !ok {
			continue
		}
		blocks, err := p.store.ServerBlocks(server.Row.ID)
		if err != nil {
			continue
		}
		edits, writes := blockEdits(server.Row, blocks, key)
		config.Edits = append(config.Edits, edits...)
		config.Writes = append(config.Writes, writes...)
		configs[server.Row.ID] = config
	}
}

// applyServerAutoconfig schickt einem einzelnen Server (ohne Netzwerk) seine Autoconfig.
func (p *Panel) applyServerAutoconfig(ctx context.Context, client *NodeClient, row ServerRow) error {
	blocks, err := p.store.ServerBlocks(row.ID)
	if err != nil {
		return err
	}
	config := node.Autoconfig{Restart: true}
	if row.Software == string(software.Velocity) {
		config.Edits, config.Reload = []node.Edit{VelocityBind()}, velocityReload
	}
	edits, writes := blockEdits(row, blocks, "")
	config.Edits = append(config.Edits, edits...)
	config.Writes = append(config.Writes, writes...)
	return client.Do(ctx, http.MethodPut, "/api/servers/"+row.ID+"/autoconfig", config, nil)
}

// ---------------------------------------------------------------- Geyser im Netzwerk

// installFloodgate lädt Floodgate auf einen Server hinter dem Proxy (Paper/Purpur von GeyserMC, Fabric von Modrinth).
func (p *Panel) installFloodgate(ctx context.Context, row ServerRow) error {
	if blocks, err := p.store.ServerBlocks(row.ID); err == nil {
		if _, ok := blocks[BlockFloodgate]; ok {
			return nil
		}
	}
	client, _, err := p.client(row.NodeID)
	if err != nil {
		return err
	}
	block := ServerBlock{ServerID: row.ID, Block: BlockFloodgate}
	switch row.Software {
	case "paper", "purpur":
		if block.Files, err = p.installGeyserMC(ctx, client, row, "spigot", "floodgate"); err != nil {
			return err
		}
	case "fabric":
		names, err := p.installFromModrinth(ctx, row, "floodgate", "", true)
		if err != nil && !errors.Is(err, errAlreadyInstalled) {
			return err
		}
		for _, name := range names {
			block.Files = append(block.Files, "mods/"+name)
		}
	default:
		return nil // Floodgate gibt es dafür nicht; die Netzwerk-Ansicht nennt den Server
	}
	return p.store.SetServerBlock(block)
}

// installBlockRequirements lädt, was ein neuer Server im Netzwerk für eingeschaltete Bausteine braucht.
func (p *Panel) installBlockRequirements(ctx context.Context, row ServerRow) error {
	if row.Role != RoleBackend {
		return nil
	}
	if _, enabled := p.networkFloodgateKey(row.ProjectID); !enabled {
		return nil
	}
	if err := p.installFloodgate(ctx, row); err != nil {
		return errors.New("Floodgate: " + err.Error())
	}
	return nil
}

func (p *Panel) projectBlockDTOs(ctx context.Context, project Project) ([]BlockDTO, error) {
	geyser := BlockDTO{ID: BlockGeyser, Name: "Geyser + Floodgate", Managed: "network",
		Description: "Bedrock-Spieler (Handy, Konsole, Windows) kommen über den Proxy ins Netzwerk. Geyser und Floodgate laufen auf dem Proxy, Floodgate auf allen Servern dahinter, mit gemeinsamem Schlüssel."}
	if project.Kind != "network" {
		geyser.Reason = "Das ist kein Netzwerk. Schalte Geyser beim Server unter „Plugins“ ein."
		return []BlockDTO{geyser}, nil
	}
	rows, err := p.store.ListServers(project.ID)
	if err != nil {
		return nil, err
	}
	_, geyser.Enabled = p.networkFloodgateKey(project.ID)
	missing := []string{}
	for _, row := range rows {
		blocks, err := p.store.ServerBlocks(row.ID)
		if err != nil {
			return nil, err
		}
		switch row.Role {
		case RoleProxy:
			geyser.Available = true
			if _, ok := blocks[BlockGeyser]; ok {
				if dto, err := p.serverBlockDTOs(ctx, row, project); err == nil {
					geyser.Address = dto[0].Address
				}
			}
		case RoleBackend:
			if _, ok := blocks[BlockFloodgate]; !ok {
				missing = append(missing, row.Name)
			}
		}
	}
	if !geyser.Available {
		geyser.Reason = "Lege zuerst den Proxy an."
	}
	if geyser.Enabled && len(missing) > 0 {
		geyser.Note = "Ohne Floodgate: " + strings.Join(missing, ", ") + ". Bedrock-Spieler kommen auf diese Server nicht (Floodgate gibt es nur für Paper, Purpur und Fabric; die Installation läuft nach dem Einschalten im Hintergrund)."
	}
	return []BlockDTO{geyser}, nil
}

func (p *Panel) listProjectBlocks(w http.ResponseWriter, r *http.Request) {
	project, _, err := p.requireProjectPermission(r, r.PathValue("id"))
	if err != nil {
		writeError(w, err)
		return
	}
	list, err := p.projectBlockDTOs(r.Context(), project)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, list)
}

// putProjectBlock schaltet Geyser für ein Netzwerk ein oder aus. Der Proxy bekommt Geyser und Floodgate samt
// Bedrock-Port (dafür muss er aus sein), die Server dahinter Floodgate und den gemeinsamen Schlüssel.
func (p *Panel) putProjectBlock(w http.ResponseWriter, r *http.Request) {
	project, _, err := p.requireProjectPermission(r, r.PathValue("id"), PermNetwork, PermAddons)
	if err != nil {
		writeError(w, err)
		return
	}
	if r.PathValue("block") != BlockGeyser {
		writeError(w, fail(http.StatusNotFound, "diesen Baustein gibt es nicht"))
		return
	}
	if project.Kind != "network" {
		writeError(w, fail(http.StatusBadRequest, "das ist kein Netzwerk"))
		return
	}
	var body struct {
		Enabled bool `json:"enabled"`
	}
	if err := decode(r, &body); err != nil {
		writeError(w, err)
		return
	}
	rows, err := p.store.ListServers(project.ID)
	if err != nil {
		writeError(w, err)
		return
	}
	var proxy *ServerRow
	for i := range rows {
		if rows[i].Role == RoleProxy {
			proxy = &rows[i]
		}
	}
	if proxy == nil {
		writeError(w, fail(http.StatusConflict, "das Netzwerk braucht zuerst einen Proxy"))
		return
	}
	_, enabled := p.networkFloodgateKey(project.ID)
	if enabled != body.Enabled {
		ctx, cancel := context.WithTimeout(r.Context(), 5*time.Minute)
		defer cancel()
		if body.Enabled {
			err = p.enableNetworkGeyser(ctx, project, *proxy)
		} else {
			err = p.disableNetworkGeyser(ctx, project, *proxy, rows)
		}
		if err != nil {
			writeError(w, err)
			return
		}
		if body.Enabled {
			// Floodgate auf den Servern dahinter laden dauert; danach wird der Schlüssel verteilt
			go func() {
				bg, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
				defer cancel()
				for _, row := range rows {
					if row.Role == RoleBackend {
						if err := p.installFloodgate(bg, row); err != nil {
							p.log.Warn("Floodgate nicht installiert", "server", row.Name, "error", err)
						}
					}
				}
				if _, err := p.syncNetwork(bg, project.ID); err != nil {
					p.log.Warn("Netzwerk-Abgleich fehlgeschlagen", "error", err)
				}
			}()
		}
		if _, err := p.syncNetwork(ctx, project.ID); err != nil {
			p.log.Warn("Netzwerk-Abgleich fehlgeschlagen", "error", err)
		}
	}
	list, err := p.projectBlockDTOs(r.Context(), project)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, list)
}

func (p *Panel) enableNetworkGeyser(ctx context.Context, project Project, proxy ServerRow) error {
	client, _, err := p.client(proxy.NodeID)
	if err != nil {
		return err
	}
	view, err := p.offlineView(ctx, client, proxy)
	if err != nil {
		return err
	}
	if err := p.enableServerBlock(ctx, client, proxy, view, BlockGeyser); err != nil {
		return err
	}
	key, err := newFloodgateKey()
	if err != nil {
		return err
	}
	sealed, err := p.seal(key)
	if err != nil {
		return err
	}
	return p.store.SetProjectBlock(ProjectBlock{ProjectID: project.ID, Block: BlockGeyser, Secret: sealed})
}

func (p *Panel) disableNetworkGeyser(ctx context.Context, project Project, proxy ServerRow, rows []ServerRow) error {
	client, _, err := p.client(proxy.NodeID)
	if err != nil {
		return err
	}
	view, err := p.offlineView(ctx, client, proxy)
	if err != nil {
		return err
	}
	blocks, err := p.store.ServerBlocks(proxy.ID)
	if err != nil {
		return err
	}
	if block, ok := blocks[BlockGeyser]; ok {
		if err := p.changePorts(ctx, client, proxy, view, block.Port, nil); err != nil {
			return err
		}
		p.removeFiles(ctx, client, proxy, block.Files)
		if err := p.store.DeleteServerBlock(proxy.ID, BlockGeyser); err != nil {
			return err
		}
	}
	for _, row := range rows {
		blocks, err := p.store.ServerBlocks(row.ID)
		if err != nil {
			continue
		}
		if block, ok := blocks[BlockFloodgate]; ok {
			if backend, _, err := p.client(row.NodeID); err == nil {
				p.removeFiles(ctx, backend, row, block.Files)
			}
			_ = p.store.DeleteServerBlock(row.ID, BlockFloodgate)
		}
	}
	return p.store.DeleteProjectBlock(project.ID, BlockGeyser)
}
