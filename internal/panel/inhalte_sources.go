package panel

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// Weitere Quellen für Plugins und Mods neben Modrinth: Hangar (PaperMC), Spigot (über die Spiget-API) und CurseForge
// (nur mit API-Schlüssel). Alle Quellen liefern dieselben, vereinfachten Typen, damit Oberfläche und Updates nicht je
// Quelle unterscheiden müssen.

// Kennungen der Quellen.
const (
	SourceModrinth   = "modrinth"
	SourceHangar     = "hangar"
	SourceSpigot     = "spigot"
	SourceCurseForge = "curseforge"
)

// SourceHit ist ein Treffer der Suche oder die Beschreibung eines Projekts.
type SourceHit struct {
	ID          string `json:"id"`
	Slug        string `json:"slug,omitempty"`
	Title       string `json:"title"`
	Description string `json:"description"`
	Author      string `json:"author,omitempty"`
	Downloads   int    `json:"downloads"`
	IconURL     string `json:"iconUrl,omitempty"`
	Link        string `json:"link,omitempty"`
	// Installable ist false, wenn sich das Projekt nicht über Lodestone laden lässt; Note sagt dann, warum.
	Installable bool   `json:"installable"`
	Note        string `json:"note,omitempty"`
}

// SourceVersion ist eine Version eines Projekts. Downloadable ist false für Dateien, die die Quelle nicht zum Laden
// freigibt (z. B. CurseForge ohne allowModDistribution); Reason sagt dann, warum.
type SourceVersion struct {
	ID           string   `json:"id"`
	Name         string   `json:"name"`
	Number       string   `json:"versionNumber"`
	Type         string   `json:"type"`
	Published    string   `json:"published"`
	GameVersions []string `json:"gameVersions"`
	Downloadable bool     `json:"downloadable"`
	Reason       string   `json:"reason,omitempty"`
}

// sourceFile ist die Datei einer Version, wie der Node sie laden soll.
type sourceFile struct {
	URL      string
	FileName string
	SHA1     string
	SHA256   string
}

// sourceTarget beschreibt den Server, für den gesucht wird.
type sourceTarget struct {
	Software    string
	GameVersion string
}

// contentSource ist eine Quelle für Plugins oder Mods.
type contentSource interface {
	Search(ctx context.Context, target sourceTarget, query string, offset int) ([]SourceHit, error)
	Project(ctx context.Context, target sourceTarget, id string) (SourceHit, []SourceVersion, error)
	// File liefert die Datei der Version versionID oder, wenn sie leer ist, der neuesten passenden Version.
	File(ctx context.Context, target sourceTarget, projectID, versionID string) (SourceHit, SourceVersion, sourceFile, error)
}

// SourceInfo sagt der Oberfläche, welche Quellen es für einen Server gibt.
type SourceInfo struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Available bool   `json:"available"`
	Reason    string `json:"reason,omitempty"`
}

// contentSources hält die Clients der Quellen. Die Adressen sind für Tests austauschbar.
type contentSources struct {
	hangar     *hangarSource
	spigot     *spigetSource
	curseforge *curseForgeSource
	geyser     *geyserDownloads
}

func newContentSources(curseForgeKey string) *contentSources {
	client := &http.Client{Timeout: 20 * time.Second}
	return &contentSources{
		hangar:     &hangarSource{base: "https://hangar.papermc.io/api/v1", http: client},
		spigot:     &spigetSource{base: "https://api.spiget.org/v2", http: client},
		curseforge: &curseForgeSource{base: "https://api.curseforge.com/v1", key: strings.TrimSpace(curseForgeKey), http: client},
		geyser:     &geyserDownloads{base: "https://download.geysermc.org/v2", http: client},
	}
}

// list nennt alle Quellen für eine Software, auch die, die gerade nicht gehen (mit Grund).
func (c *contentSources) list(softwareName string) []SourceInfo {
	result := []SourceInfo{{ID: SourceModrinth, Name: "Modrinth", Available: loadersFor(softwareName) != nil}}
	hangar := SourceInfo{ID: SourceHangar, Name: "Hangar"}
	if hangarPlatform(softwareName) != "" {
		hangar.Available = true
	} else {
		hangar.Reason = "Hangar hat nur Plugins für Paper, Purpur, Folia und Velocity."
	}
	spigot := SourceInfo{ID: SourceSpigot, Name: "Spigot"}
	// Leaf ist eine Abspaltung von Paper und lädt dieselben Plugins
	if softwareName == "paper" || softwareName == "purpur" || softwareName == "leaf" {
		spigot.Available = true
	} else {
		spigot.Reason = "Spigot-Plugins laufen nur auf Paper und Purpur."
	}
	curse := SourceInfo{ID: SourceCurseForge, Name: "CurseForge"}
	switch {
	case curseLoader(softwareName) == 0:
		curse.Reason = "Über CurseForge lädt Lodestone nur Mods für Fabric, Forge und NeoForge."
	case c.curseforge.key == "":
		curse.Reason = "Dafür braucht das Panel einen CurseForge-API-Schlüssel (Umgebungsvariable LODESTONE_CURSEFORGE_KEY)."
	default:
		curse.Available = true
	}
	return append(result, hangar, spigot, curse)
}

// get liefert die Quelle, wenn sie für die Software geht.
func (c *contentSources) get(id, softwareName string) (contentSource, error) {
	for _, info := range c.list(softwareName) {
		if info.ID != id || id == SourceModrinth {
			continue
		}
		if !info.Available {
			return nil, fail(http.StatusBadRequest, info.Reason)
		}
		switch id {
		case SourceHangar:
			return c.hangar, nil
		case SourceSpigot:
			return c.spigot, nil
		case SourceCurseForge:
			return c.curseforge, nil
		}
	}
	return nil, fail(http.StatusNotFound, "diese Quelle gibt es nicht")
}

// getSourceJSON fragt eine Quelle ab. Fehler sind deutsch und nennen die Quelle, nie Adressen mit Schlüsseln.
func getSourceJSON(ctx context.Context, client *http.Client, name, address string, header http.Header, into any) error {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, address, nil)
	if err != nil {
		return err
	}
	request.Header.Set("User-Agent", "lodestone-mc/lodestone (self-hosted panel)")
	request.Header.Set("Accept", "application/json")
	for key, values := range header {
		request.Header[key] = values
	}
	response, err := client.Do(request)
	if err != nil {
		return fail(http.StatusBadGateway, name+" ist nicht erreichbar. Versuche es gleich noch einmal.")
	}
	defer response.Body.Close()
	switch {
	case response.StatusCode == http.StatusNotFound:
		return ErrNotFound
	case response.StatusCode == http.StatusUnauthorized || response.StatusCode == http.StatusForbidden:
		return fail(http.StatusBadGateway, name+" lehnt die Anfrage ab ("+strconv.Itoa(response.StatusCode)+"). Prüfe den API-Schlüssel.")
	case response.StatusCode >= 300:
		return fail(http.StatusBadGateway, fmt.Sprintf("%s antwortet mit Status %d. Versuche es später noch einmal.", name, response.StatusCode))
	}
	if err := json.NewDecoder(io.LimitReader(response.Body, 16<<20)).Decode(into); err != nil {
		return fail(http.StatusBadGateway, name+" hat eine unerwartete Antwort geschickt.")
	}
	return nil
}

// minorVersion kürzt 1.21.4 auf 1.21 (26.3.1 auf 26.3).
func minorVersion(version string) string {
	parts := strings.Split(version, ".")
	if len(parts) > 2 {
		return strings.Join(parts[:2], ".")
	}
	return version
}

// matchesGame sagt, ob eine von der Quelle genannte Spielversion zum Server passt (1.21 passt zu 1.21.4).
func matchesGame(listed []string, gameVersion string) bool {
	if gameVersion == "" {
		return true
	}
	for _, version := range listed {
		version = strings.TrimSpace(version)
		if version == gameVersion || version == minorVersion(gameVersion) || strings.HasPrefix(gameVersion, version+".") {
			return true
		}
	}
	return false
}

var fileNameClean = regexp.MustCompile(`[^A-Za-z0-9._+-]+`)

// jarName macht aus Projekt- und Versionsnamen einen sicheren Dateinamen.
func jarName(parts ...string) string {
	cleaned := []string{}
	for _, part := range parts {
		if part = strings.Trim(fileNameClean.ReplaceAllString(part, "_"), "._-"); part != "" {
			cleaned = append(cleaned, part)
		}
	}
	name := strings.Join(cleaned, "-")
	if len(name) > 96 {
		name = name[:96]
	}
	if name == "" {
		name = "plugin"
	}
	return name + ".jar"
}

// ---------------------------------------------------------------- Hangar

type hangarSource struct {
	base string
	http *http.Client
}

func hangarPlatform(softwareName string) string {
	switch softwareName {
	case "paper", "purpur", "folia", "leaf":
		return "PAPER"
	case "velocity":
		return "VELOCITY"
	}
	return ""
}

type hangarProject struct {
	ID        int    `json:"id"`
	Name      string `json:"name"`
	Namespace struct {
		Owner string `json:"owner"`
		Slug  string `json:"slug"`
	} `json:"namespace"`
	Stats struct {
		Downloads int `json:"downloads"`
	} `json:"stats"`
	Description string `json:"description"`
	AvatarURL   string `json:"avatarUrl"`
	Settings    struct {
		Tags []string `json:"tags"`
	} `json:"settings"`
}

func (p hangarProject) hit() SourceHit {
	return SourceHit{ID: strconv.Itoa(p.ID), Slug: p.Namespace.Slug, Title: p.Name, Description: p.Description, Author: p.Namespace.Owner,
		Downloads: p.Stats.Downloads, IconURL: p.AvatarURL, Link: "https://hangar.papermc.io/" + url.PathEscape(p.Namespace.Owner) + "/" + url.PathEscape(p.Namespace.Slug), Installable: true}
}

type hangarVersion struct {
	Name      string `json:"name"`
	CreatedAt string `json:"createdAt"`
	Channel   struct {
		Name string `json:"name"`
	} `json:"channel"`
	Downloads map[string]struct {
		FileInfo *struct {
			Name   string `json:"name"`
			SHA256 string `json:"sha256Hash"`
		} `json:"fileInfo"`
		ExternalURL string `json:"externalUrl"`
		DownloadURL string `json:"downloadUrl"`
	} `json:"downloads"`
	PlatformDependencies map[string][]string `json:"platformDependencies"`
}

func (v hangarVersion) brief(platform string) SourceVersion {
	kind := strings.ToLower(v.Channel.Name)
	if kind == "" {
		kind = "release"
	}
	out := SourceVersion{ID: v.Name, Name: v.Channel.Name, Number: v.Name, Type: kind, Published: v.CreatedAt, GameVersions: v.PlatformDependencies[platform]}
	if out.GameVersions == nil {
		out.GameVersions = []string{}
	}
	download, ok := v.Downloads[platform]
	switch {
	case !ok:
		out.Reason = "Diese Version gibt es nicht für die Software des Servers."
	case download.DownloadURL == "" || download.FileInfo == nil:
		out.Reason = "Diese Version liegt nicht auf Hangar, sondern auf einer anderen Seite. Lade sie dort herunter."
	default:
		out.Downloadable = true
	}
	return out
}

func (h *hangarSource) Search(ctx context.Context, target sourceTarget, query string, offset int) ([]SourceHit, error) {
	values := url.Values{"limit": {"20"}, "offset": {strconv.Itoa(offset)}, "platform": {hangarPlatform(target.Software)}}
	if query != "" {
		values.Set("q", query)
	} else {
		values.Set("sort", "-downloads")
	}
	if target.Software == "folia" {
		values.Set("tag", "SUPPORTS_FOLIA")
	}
	var result struct {
		Result []hangarProject `json:"result"`
	}
	if err := getSourceJSON(ctx, h.http, "Hangar", h.base+"/projects?"+values.Encode(), nil, &result); err != nil {
		return nil, err
	}
	hits := make([]SourceHit, 0, len(result.Result))
	for _, project := range result.Result {
		hits = append(hits, project.hit())
	}
	return hits, nil
}

func (h *hangarSource) project(ctx context.Context, target sourceTarget, id string) (SourceHit, error) {
	if !digitsOnly(id) {
		return SourceHit{}, fail(http.StatusBadRequest, "ungültige Projekt-ID")
	}
	var project hangarProject
	if err := getSourceJSON(ctx, h.http, "Hangar", h.base+"/projects/"+id, nil, &project); err != nil {
		return SourceHit{}, err
	}
	hit := project.hit()
	if target.Software == "folia" && !containsString(project.Settings.Tags, "SUPPORTS_FOLIA") {
		hit.Installable, hit.Note = false, "Das Plugin unterstützt Folia laut Hangar nicht."
	}
	return hit, nil
}

// versions holt die Versionen für Plattform und Spielversion. Viele Plugins nennen nur 1.21 statt 1.21.4, deshalb
// wird ohne Treffer mit der kürzeren Version noch einmal gesucht.
func (h *hangarSource) versions(ctx context.Context, target sourceTarget, id string) ([]hangarVersion, error) {
	platform := hangarPlatform(target.Software)
	tries := []string{""}
	if target.Software != "velocity" && target.GameVersion != "" {
		tries = []string{target.GameVersion}
		if minor := minorVersion(target.GameVersion); minor != target.GameVersion {
			tries = append(tries, minor)
		}
	}
	for _, game := range tries {
		values := url.Values{"limit": {"25"}, "offset": {"0"}, "platform": {platform}}
		if game != "" {
			values.Set("platformVersion", game)
		}
		var result struct {
			Result []hangarVersion `json:"result"`
		}
		if err := getSourceJSON(ctx, h.http, "Hangar", h.base+"/projects/"+id+"/versions?"+values.Encode(), nil, &result); err != nil {
			return nil, err
		}
		if len(result.Result) > 0 {
			return result.Result, nil
		}
	}
	return []hangarVersion{}, nil
}

func (h *hangarSource) Project(ctx context.Context, target sourceTarget, id string) (SourceHit, []SourceVersion, error) {
	hit, err := h.project(ctx, target, id)
	if err != nil {
		return hit, nil, err
	}
	list, err := h.versions(ctx, target, id)
	if err != nil {
		return hit, nil, err
	}
	platform := hangarPlatform(target.Software)
	versions := make([]SourceVersion, 0, len(list))
	for _, version := range list {
		brief := version.brief(platform)
		if !hit.Installable {
			brief.Downloadable, brief.Reason = false, hit.Note
		}
		versions = append(versions, brief)
	}
	return hit, versions, nil
}

func (h *hangarSource) File(ctx context.Context, target sourceTarget, projectID, versionID string) (SourceHit, SourceVersion, sourceFile, error) {
	hit, err := h.project(ctx, target, projectID)
	if err != nil {
		return hit, SourceVersion{}, sourceFile{}, err
	}
	if !hit.Installable {
		return hit, SourceVersion{}, sourceFile{}, fail(http.StatusBadRequest, hit.Note)
	}
	platform := hangarPlatform(target.Software)
	list, err := h.versions(ctx, target, projectID)
	if err != nil {
		return hit, SourceVersion{}, sourceFile{}, err
	}
	var chosen *hangarVersion
	for i := range list {
		brief := list[i].brief(platform)
		switch {
		case versionID != "" && list[i].Name == versionID:
			chosen = &list[i]
		case versionID == "" && brief.Downloadable && strings.EqualFold(list[i].Channel.Name, "release") && chosen == nil:
			chosen = &list[i]
		}
	}
	if chosen == nil && versionID == "" {
		for i := range list {
			if list[i].brief(platform).Downloadable {
				chosen = &list[i]
				break
			}
		}
	}
	if chosen == nil {
		return hit, SourceVersion{}, sourceFile{}, errNoVersion
	}
	brief := chosen.brief(platform)
	if !brief.Downloadable {
		return hit, brief, sourceFile{}, fail(http.StatusConflict, brief.Reason)
	}
	download := chosen.Downloads[platform]
	return hit, brief, sourceFile{URL: download.DownloadURL, FileName: download.FileInfo.Name, SHA256: download.FileInfo.SHA256}, nil
}

// ---------------------------------------------------------------- Spigot (Spiget)

type spigetSource struct {
	base string
	http *http.Client
}

const spigetFields = "id,name,tag,external,premium,file,testedVersions,downloads,icon,version,updateDate"

type spigetResource struct {
	ID       int    `json:"id"`
	Name     string `json:"name"`
	Tag      string `json:"tag"`
	External bool   `json:"external"`
	Premium  bool   `json:"premium"`
	File     struct {
		Type string `json:"type"`
	} `json:"file"`
	TestedVersions []string `json:"testedVersions"`
	Downloads      int      `json:"downloads"`
	Icon           struct {
		URL string `json:"url"`
	} `json:"icon"`
	Version struct {
		ID int `json:"id"`
	} `json:"version"`
	UpdateDate int64 `json:"updateDate"`
}

func (s spigetResource) link() string {
	return "https://www.spigotmc.org/resources/" + strconv.Itoa(s.ID) + "/"
}

// problem sagt, warum sich die Ressource nicht laden lässt (oder "").
func (s spigetResource) problem(gameVersion string) string {
	switch {
	case s.Premium:
		return "Das ist ein Bezahl-Plugin. Kaufe und lade es auf spigotmc.org."
	case s.External || (s.File.Type != "" && s.File.Type != ".jar"):
		return "Das Plugin liegt nicht auf Spigot, sondern auf einer anderen Seite. Lade es dort herunter."
	case len(s.TestedVersions) > 0 && !matchesGame(s.TestedVersions, gameVersion):
		return "Laut Spigot nicht für Minecraft " + gameVersion + " getestet (getestet: " + strings.Join(s.TestedVersions, ", ") + ")."
	}
	return ""
}

func (s spigetResource) hit(gameVersion string) SourceHit {
	hit := SourceHit{ID: strconv.Itoa(s.ID), Title: s.Name, Description: s.Tag, Downloads: s.Downloads, Link: s.link(), Installable: true}
	if s.Icon.URL != "" {
		hit.IconURL = "https://www.spigotmc.org/" + strings.TrimPrefix(s.Icon.URL, "/")
	}
	if note := s.problem(gameVersion); note != "" {
		hit.Installable, hit.Note = false, note
	}
	return hit
}

func (s *spigetSource) Search(ctx context.Context, target sourceTarget, query string, offset int) ([]SourceHit, error) {
	values := url.Values{"size": {"20"}, "page": {strconv.Itoa(offset/20 + 1)}, "sort": {"-downloads"}, "fields": {spigetFields}}
	address := s.base + "/resources/free?" + values.Encode()
	if query != "" {
		address = s.base + "/search/resources/" + url.PathEscape(query) + "?" + values.Encode()
	}
	var list []spigetResource
	err := getSourceJSON(ctx, s.http, "Spigot", address, nil, &list)
	if errors.Is(err, ErrNotFound) {
		return []SourceHit{}, nil // Spiget antwortet ohne Treffer mit 404
	}
	if err != nil {
		return nil, err
	}
	hits := make([]SourceHit, 0, len(list))
	for _, resource := range list {
		hits = append(hits, resource.hit(target.GameVersion))
	}
	return hits, nil
}

func (s *spigetSource) resource(ctx context.Context, id string) (spigetResource, error) {
	if !digitsOnly(id) {
		return spigetResource{}, fail(http.StatusBadRequest, "ungültige Projekt-ID")
	}
	var resource spigetResource
	err := getSourceJSON(ctx, s.http, "Spigot", s.base+"/resources/"+id+"?fields="+url.QueryEscape(spigetFields), nil, &resource)
	return resource, err
}

// latest beschreibt die neueste Version. Ältere Versionen gibt Spigot nur über die Webseite heraus.
func (s *spigetSource) latest(ctx context.Context, resource spigetResource, gameVersion string) (SourceVersion, error) {
	var version struct {
		ID          int    `json:"id"`
		Name        string `json:"name"`
		ReleaseDate int64  `json:"releaseDate"`
	}
	if err := getSourceJSON(ctx, s.http, "Spigot", s.base+"/resources/"+strconv.Itoa(resource.ID)+"/versions/latest", nil, &version); err != nil {
		return SourceVersion{}, err
	}
	brief := SourceVersion{ID: strconv.Itoa(version.ID), Name: version.Name, Number: version.Name, Type: "release",
		Published: time.Unix(version.ReleaseDate, 0).UTC().Format(time.RFC3339), GameVersions: resource.TestedVersions, Downloadable: true}
	if brief.GameVersions == nil {
		brief.GameVersions = []string{}
	}
	if problem := resource.problem(gameVersion); problem != "" {
		brief.Downloadable, brief.Reason = false, problem
	}
	return brief, nil
}

func (s *spigetSource) Project(ctx context.Context, target sourceTarget, id string) (SourceHit, []SourceVersion, error) {
	resource, err := s.resource(ctx, id)
	if err != nil {
		return SourceHit{}, nil, err
	}
	latest, err := s.latest(ctx, resource, target.GameVersion)
	if err != nil {
		return resource.hit(target.GameVersion), nil, err
	}
	return resource.hit(target.GameVersion), []SourceVersion{latest}, nil
}

func (s *spigetSource) File(ctx context.Context, target sourceTarget, projectID, versionID string) (SourceHit, SourceVersion, sourceFile, error) {
	resource, err := s.resource(ctx, projectID)
	if err != nil {
		return SourceHit{}, SourceVersion{}, sourceFile{}, err
	}
	hit := resource.hit(target.GameVersion)
	latest, err := s.latest(ctx, resource, target.GameVersion)
	if err != nil {
		return hit, SourceVersion{}, sourceFile{}, err
	}
	if versionID != "" && versionID != latest.ID {
		return hit, latest, sourceFile{}, fail(http.StatusConflict, "Über Spigot lässt sich nur die neueste Version laden. Ältere Versionen gibt es auf spigotmc.org.")
	}
	if !latest.Downloadable {
		return hit, latest, sourceFile{}, fail(http.StatusConflict, latest.Reason)
	}
	// Spiget nennt keine Prüfsumme; geladen wird nur über https von api.spiget.org und dessen CDN (Host-Liste des Nodes)
	return hit, latest, sourceFile{URL: s.base + "/resources/" + strconv.Itoa(resource.ID) + "/download", FileName: jarName(resource.Name, latest.Number)}, nil
}

// ---------------------------------------------------------------- CurseForge

type curseForgeSource struct {
	base string
	key  string
	http *http.Client
}

const (
	curseGameMinecraft = 432
	curseClassMods     = 6
)

// curseLoader ist der modLoaderType von CurseForge (1 Forge, 4 Fabric, 6 NeoForge).
func curseLoader(softwareName string) int {
	switch softwareName {
	case "forge":
		return 1
	case "fabric":
		return 4
	case "neoforge":
		return 6
	}
	return 0
}

type curseMod struct {
	ID                   int     `json:"id"`
	Name                 string  `json:"name"`
	Slug                 string  `json:"slug"`
	Summary              string  `json:"summary"`
	DownloadCount        float64 `json:"downloadCount"`
	ClassID              int     `json:"classId"`
	AllowModDistribution *bool   `json:"allowModDistribution"`
	Links                struct {
		WebsiteURL string `json:"websiteUrl"`
	} `json:"links"`
	Logo *struct {
		ThumbnailURL string `json:"thumbnailUrl"`
	} `json:"logo"`
	Authors []struct {
		Name string `json:"name"`
	} `json:"authors"`
}

// distributable ist false, wenn der Autor Downloads über andere Programme verbietet.
func (m curseMod) distributable() bool {
	return m.AllowModDistribution == nil || *m.AllowModDistribution
}

const curseBlocked = "Der Autor erlaubt keine Downloads über andere Programme. Lade die Datei auf curseforge.com herunter und dann hier hoch."

func (m curseMod) hit() SourceHit {
	hit := SourceHit{ID: strconv.Itoa(m.ID), Slug: m.Slug, Title: m.Name, Description: m.Summary, Downloads: int(m.DownloadCount), Link: m.Links.WebsiteURL, Installable: true}
	if m.Logo != nil {
		hit.IconURL = m.Logo.ThumbnailURL
	}
	if len(m.Authors) > 0 {
		hit.Author = m.Authors[0].Name
	}
	if !m.distributable() {
		hit.Installable, hit.Note = false, curseBlocked
	}
	return hit
}

type curseFile struct {
	ID           int      `json:"id"`
	DisplayName  string   `json:"displayName"`
	FileName     string   `json:"fileName"`
	ReleaseType  int      `json:"releaseType"`
	FileDate     string   `json:"fileDate"`
	DownloadURL  string   `json:"downloadUrl"`
	GameVersions []string `json:"gameVersions"`
	IsAvailable  bool     `json:"isAvailable"`
	IsServerPack bool     `json:"isServerPack"`
	Hashes       []struct {
		Value string `json:"value"`
		Algo  int    `json:"algo"`
	} `json:"hashes"`
}

func (f curseFile) sha1() string {
	for _, hash := range f.Hashes {
		if hash.Algo == 1 {
			return hash.Value
		}
	}
	return ""
}

func (f curseFile) brief(mod curseMod) SourceVersion {
	kind := map[int]string{1: "release", 2: "beta", 3: "alpha"}[f.ReleaseType]
	if kind == "" {
		kind = "release"
	}
	out := SourceVersion{ID: strconv.Itoa(f.ID), Name: f.FileName, Number: f.DisplayName, Type: kind, Published: f.FileDate, GameVersions: f.GameVersions, Downloadable: true}
	if out.GameVersions == nil {
		out.GameVersions = []string{}
	}
	switch {
	case !mod.distributable() || f.DownloadURL == "":
		out.Downloadable, out.Reason = false, curseBlocked
	case !f.IsAvailable:
		out.Downloadable, out.Reason = false, "CurseForge stellt diese Datei gerade nicht bereit."
	}
	return out
}

func (c *curseForgeSource) get(ctx context.Context, address string, into any) error {
	return getSourceJSON(ctx, c.http, "CurseForge", c.base+address, http.Header{"X-Api-Key": {c.key}}, into)
}

func (c *curseForgeSource) search(ctx context.Context, class, loader int, gameVersion, query string, offset int) ([]SourceHit, error) {
	values := url.Values{"gameId": {strconv.Itoa(curseGameMinecraft)}, "classId": {strconv.Itoa(class)}, "sortField": {"2"}, "sortOrder": {"desc"},
		"index": {strconv.Itoa(offset)}, "pageSize": {"20"}}
	if query != "" {
		values.Set("searchFilter", query)
	}
	if gameVersion != "" {
		values.Set("gameVersion", gameVersion)
	}
	if loader != 0 {
		values.Set("modLoaderType", strconv.Itoa(loader))
	}
	var result struct {
		Data []curseMod `json:"data"`
	}
	if err := c.get(ctx, "/mods/search?"+values.Encode(), &result); err != nil {
		return nil, err
	}
	hits := make([]SourceHit, 0, len(result.Data))
	for _, mod := range result.Data {
		hits = append(hits, mod.hit())
	}
	return hits, nil
}

func (c *curseForgeSource) Search(ctx context.Context, target sourceTarget, query string, offset int) ([]SourceHit, error) {
	return c.search(ctx, curseClassMods, curseLoader(target.Software), target.GameVersion, query, offset)
}

func (c *curseForgeSource) mod(ctx context.Context, id string, class int) (curseMod, error) {
	if !digitsOnly(id) {
		return curseMod{}, fail(http.StatusBadRequest, "ungültige Projekt-ID")
	}
	var result struct {
		Data curseMod `json:"data"`
	}
	if err := c.get(ctx, "/mods/"+id, &result); err != nil {
		return curseMod{}, err
	}
	if result.Data.ClassID != class {
		return curseMod{}, fail(http.StatusBadRequest, "das ist kein passendes CurseForge-Projekt")
	}
	return result.Data, nil
}

func (c *curseForgeSource) files(ctx context.Context, id string, loader int, gameVersion string) ([]curseFile, error) {
	values := url.Values{"pageSize": {"50"}}
	if gameVersion != "" {
		values.Set("gameVersion", gameVersion)
	}
	if loader != 0 {
		values.Set("modLoaderType", strconv.Itoa(loader))
	}
	var result struct {
		Data []curseFile `json:"data"`
	}
	if err := c.get(ctx, "/mods/"+id+"/files?"+values.Encode(), &result); err != nil {
		return nil, err
	}
	files := result.Data[:0]
	for _, file := range result.Data {
		if !file.IsServerPack {
			files = append(files, file)
		}
	}
	return files, nil
}

func (c *curseForgeSource) Project(ctx context.Context, target sourceTarget, id string) (SourceHit, []SourceVersion, error) {
	mod, err := c.mod(ctx, id, curseClassMods)
	if err != nil {
		return SourceHit{}, nil, err
	}
	files, err := c.files(ctx, id, curseLoader(target.Software), target.GameVersion)
	if err != nil {
		return mod.hit(), nil, err
	}
	versions := make([]SourceVersion, 0, len(files))
	for _, file := range files {
		versions = append(versions, file.brief(mod))
	}
	return mod.hit(), versions, nil
}

func (c *curseForgeSource) File(ctx context.Context, target sourceTarget, projectID, versionID string) (SourceHit, SourceVersion, sourceFile, error) {
	mod, err := c.mod(ctx, projectID, curseClassMods)
	if err != nil {
		return SourceHit{}, SourceVersion{}, sourceFile{}, err
	}
	files, err := c.files(ctx, projectID, curseLoader(target.Software), target.GameVersion)
	if err != nil {
		return mod.hit(), SourceVersion{}, sourceFile{}, err
	}
	var chosen *curseFile
	for i := range files {
		if versionID != "" && strconv.Itoa(files[i].ID) == versionID {
			chosen = &files[i]
		}
		if versionID == "" && chosen == nil && files[i].ReleaseType == 1 {
			chosen = &files[i]
		}
	}
	if chosen == nil && versionID == "" && len(files) > 0 {
		chosen = &files[0]
	}
	if chosen == nil {
		return mod.hit(), SourceVersion{}, sourceFile{}, errNoVersion
	}
	brief := chosen.brief(mod)
	if !brief.Downloadable {
		return mod.hit(), brief, sourceFile{}, fail(http.StatusConflict, brief.Reason)
	}
	return mod.hit(), brief, sourceFile{URL: chosen.DownloadURL, FileName: chosen.FileName, SHA1: chosen.sha1()}, nil
}

// ---------------------------------------------------------------- GeyserMC

// geyserDownloads fragt die Download-API von GeyserMC (Geyser und Floodgate, mit SHA-256).
type geyserDownloads struct {
	base string
	http *http.Client
}

// latest liefert die neueste Datei eines Projekts (geyser, floodgate) für eine Plattform (spigot, velocity …).
func (g *geyserDownloads) latest(ctx context.Context, project, platform string) (sourceFile, string, error) {
	var build struct {
		Version   string `json:"version"`
		Build     int    `json:"build"`
		Downloads map[string]struct {
			Name   string `json:"name"`
			SHA256 string `json:"sha256"`
		} `json:"downloads"`
	}
	address := fmt.Sprintf("%s/projects/%s/versions/latest/builds/latest", g.base, url.PathEscape(project))
	if err := getSourceJSON(ctx, g.http, "GeyserMC", address, nil, &build); err != nil {
		return sourceFile{}, "", err
	}
	download, ok := build.Downloads[platform]
	if !ok || download.Name == "" || download.SHA256 == "" {
		return sourceFile{}, "", fail(http.StatusBadGateway, "GeyserMC bietet "+project+" nicht für "+platform+" an.")
	}
	file := sourceFile{
		URL:      fmt.Sprintf("%s/projects/%s/versions/%s/builds/%d/downloads/%s", g.base, url.PathEscape(project), url.PathEscape(build.Version), build.Build, url.PathEscape(platform)),
		FileName: download.Name, SHA256: download.SHA256,
	}
	return file, fmt.Sprintf("%s-%d", build.Version, build.Build), nil
}

func digitsOnly(value string) bool {
	if value == "" || len(value) > 20 {
		return false
	}
	for _, r := range value {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}
