package panel

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/lodestone-mc/lodestone/internal/node"
)

// registerInhalteRoutes hängt die Routen für weitere Quellen und Bausteine ein.
func (p *Panel) registerInhalteRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/servers/{id}/sources", p.authenticate(p.listSources))
	mux.HandleFunc("GET /api/servers/{id}/sources/{source}/search", p.authenticate(p.sourceSearch))
	mux.HandleFunc("GET /api/servers/{id}/sources/{source}/project/{project}", p.authenticate(p.sourceProject))
	mux.HandleFunc("POST /api/servers/{id}/sources/{source}/install", p.authenticate(p.sourceInstall))
	mux.HandleFunc("GET /api/servers/{id}/blocks", p.authenticate(p.listServerBlocks))
	mux.HandleFunc("PUT /api/servers/{id}/blocks/{block}", p.authenticate(p.putServerBlock))
	mux.HandleFunc("GET /api/projects/{id}/blocks", p.authenticate(p.listProjectBlocks))
	mux.HandleFunc("PUT /api/projects/{id}/blocks/{block}", p.authenticate(p.putProjectBlock))
}

// ---------------------------------------------------------------- Quellen

func (p *Panel) sourceContext(r *http.Request, needed ...Permission) (ServerRow, sourceTarget, contentSource, error) {
	row, gameVersion, err := p.modrinthContext(r, needed...)
	if err != nil {
		return row, sourceTarget{}, nil, err
	}
	source, err := p.content.get(r.PathValue("source"), row.Software)
	return row, sourceTarget{Software: row.Software, GameVersion: gameVersion}, source, err
}

func (p *Panel) listSources(w http.ResponseWriter, r *http.Request) {
	row, _, _, err := p.requireServer(r, rankViewer)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, p.content.list(row.Software))
}

func (p *Panel) sourceSearch(w http.ResponseWriter, r *http.Request) {
	_, target, source, err := p.sourceContext(r)
	if err != nil {
		writeError(w, err)
		return
	}
	offset, _ := strconv.Atoi(r.URL.Query().Get("offset"))
	query := strings.TrimSpace(r.URL.Query().Get("q"))
	if len(query) > 100 {
		writeError(w, fail(http.StatusBadRequest, "der Suchbegriff ist zu lang"))
		return
	}
	hits, err := source.Search(r.Context(), target, query, min(max(offset, 0), 5000))
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"hits": hits})
}

func (p *Panel) sourceProject(w http.ResponseWriter, r *http.Request) {
	_, target, source, err := p.sourceContext(r)
	if err != nil {
		writeError(w, err)
		return
	}
	hit, versions, err := source.Project(r.Context(), target, r.PathValue("project"))
	if errors.Is(err, ErrNotFound) {
		writeError(w, fail(http.StatusNotFound, "das Projekt gibt es nicht (mehr)"))
		return
	}
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"project": hit, "versions": versions})
}

func (p *Panel) sourceInstall(w http.ResponseWriter, r *http.Request) {
	row, target, source, err := p.sourceContext(r, PermAddons)
	if err != nil {
		writeError(w, err)
		return
	}
	var body struct {
		Project   string `json:"project"`
		VersionID string `json:"versionId"`
	}
	if err := decode(r, &body); err != nil {
		writeError(w, err)
		return
	}
	if body.Project == "" || len(body.Project) > 64 || len(body.VersionID) > 128 {
		writeError(w, fail(http.StatusBadRequest, "kein Projekt angegeben"))
		return
	}
	sourceID := r.PathValue("source")
	known, err := p.store.AddonSources(row.ID)
	if err != nil {
		writeError(w, err)
		return
	}
	for _, entry := range known {
		if entry.Source == sourceID && entry.ProjectID == body.Project {
			writeError(w, fail(http.StatusConflict, "das ist schon installiert – nutze „Updates prüfen“"))
			return
		}
	}
	name, err := p.installFromSource(r.Context(), row, target, sourceID, source, body.Project, body.VersionID, "")
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"installed": []string{name}})
}

// installFromSource lädt eine Version auf den Server und merkt sich ihre Herkunft. replace ist eine Datei, die dabei
// ersetzt wird (Update), sonst leer.
func (p *Panel) installFromSource(ctx context.Context, row ServerRow, target sourceTarget, sourceID string, source contentSource, project, versionID, replace string) (string, error) {
	hit, version, file, err := source.File(ctx, target, project, versionID)
	switch {
	case errors.Is(err, ErrNotFound):
		return "", fail(http.StatusNotFound, "das Projekt gibt es nicht (mehr)")
	case errors.Is(err, errNoVersion):
		return "", fail(http.StatusNotFound, errNoVersion.Error())
	case err != nil:
		return "", err
	}
	name, safe := safeFilename(file.FileName)
	if !safe {
		return "", fail(http.StatusBadGateway, "die Version enthält keine passende .jar-Datei")
	}
	client, _, err := p.client(row.NodeID)
	if err != nil {
		return "", err
	}
	dir := addonDir(row.Software)
	path := dir + "/" + name
	request := node.PullRequest{URL: file.URL, Path: path, SHA1: file.SHA1, SHA256: file.SHA256, Overwrite: replace == path}
	if err := p.pull(ctx, client, row.ID, request); err != nil {
		if errors.Is(err, errAlreadyInstalled) {
			return "", fail(http.StatusConflict, "eine Datei mit dem Namen "+name+" liegt schon auf dem Server")
		}
		return "", err
	}
	if replace != "" && replace != path {
		query := url.Values{"path": {replace}}
		if err := client.Do(ctx, http.MethodDelete, "/api/servers/"+row.ID+"/files?"+query.Encode(), nil, nil); err != nil {
			p.log.Warn("alte Datei nach dem Update nicht gelöscht", "server", row.Name, "file", replace, "error", err)
		}
		_ = p.store.DeleteAddonSource(row.ID, replace)
	}
	entry := AddonSource{ServerID: row.ID, Path: path, Source: sourceID, ProjectID: project, ProjectName: hit.Title, VersionID: version.ID,
		VersionName: version.Number, Link: hit.Link, IconURL: hit.IconURL}
	if err := p.store.SetAddonSource(entry); err != nil {
		return name, err
	}
	return name, nil
}

// ---------------------------------------------------------------- Anschluss an Modrinth-Liste und Updates

// trackedPath ist der Pfad, unter dem die Herkunft gemerkt ist (deaktivierte Dateien tragen .disabled).
func trackedPath(dir, file string) string {
	return dir + "/" + strings.TrimSuffix(file, ".disabled")
}

// applySourceDetails ergänzt die Liste der installierten Dateien um die Herkunft aus Hangar, Spigot und CurseForge
// und vergisst Herkünfte von Dateien, die es nicht mehr gibt.
func (p *Panel) applySourceDetails(row ServerRow, details []addonDetail) {
	known, err := p.store.AddonSources(row.ID)
	if err != nil || len(known) == 0 {
		return
	}
	present := map[string]bool{}
	for i := range details {
		path := trackedPath(details[i].Dir, details[i].File)
		present[path] = true
		entry, ok := known[path]
		if !ok {
			continue
		}
		detail := &details[i]
		*detail = addonDetail{File: detail.File, Dir: detail.Dir, Known: true, Source: entry.Source, ProjectID: entry.ProjectID, Title: entry.ProjectName,
			IconURL: entry.IconURL, Link: entry.Link, VersionID: entry.VersionID, VersionNumber: entry.VersionName, VersionType: "release", VersionLink: entry.Link}
	}
	for path := range known {
		if !present[path] {
			_ = p.store.DeleteAddonSource(row.ID, path)
		}
	}
}

// applySourceChecks prüft für Dateien aus anderen Quellen, ob es eine neuere Version gibt.
func (p *Panel) applySourceChecks(ctx context.Context, row ServerRow, gameVersion string, entries []updateEntry) {
	known, err := p.store.AddonSources(row.ID)
	if err != nil || len(known) == 0 {
		return
	}
	target := sourceTarget{Software: row.Software, GameVersion: gameVersion}
	for i := range entries {
		entry, ok := known[trackedPath(entries[i].Dir, entries[i].File)]
		if !ok || strings.HasSuffix(entries[i].File, ".disabled") {
			continue
		}
		check := &entries[i]
		check.Known, check.UpdateAvailable, check.Latest, check.LatestFile, check.Source = true, false, "", "", entry.Source
		check.Current = entry.VersionName
		source, err := p.content.get(entry.Source, row.Software)
		if err != nil {
			continue
		}
		_, version, file, err := source.File(ctx, target, entry.ProjectID, "")
		if err != nil || version.ID == entry.VersionID {
			continue
		}
		check.UpdateAvailable, check.Latest, check.LatestFile = true, version.Number, file.FileName
	}
}

// sourceOutcome hat dieselben Felder wie das Ergebnis von addonsUpdate.
type sourceOutcome struct {
	File    string `json:"file"`
	Updated bool   `json:"updated"`
	To      string `json:"to,omitempty"`
	Error   string `json:"error,omitempty"`
}

// splitTracked trennt die Dateien aus anderen Quellen von denen, die über Modrinth aktualisiert werden.
func (p *Panel) splitTracked(row ServerRow, addons []node.Addon) (modrinth, tracked []node.Addon) {
	known, err := p.store.AddonSources(row.ID)
	if err != nil {
		return addons, nil
	}
	modrinth = []node.Addon{}
	for _, addon := range addons {
		if _, ok := known[addon.Dir+"/"+addon.File]; ok {
			tracked = append(tracked, addon)
		} else {
			modrinth = append(modrinth, addon)
		}
	}
	return modrinth, tracked
}

// updateTracked aktualisiert Dateien aus Hangar, Spigot und CurseForge auf die neueste passende Version.
func (p *Panel) updateTracked(ctx context.Context, row ServerRow, gameVersion string, addons []node.Addon) []sourceOutcome {
	results := []sourceOutcome{}
	known, err := p.store.AddonSources(row.ID)
	if err != nil {
		return results
	}
	target := sourceTarget{Software: row.Software, GameVersion: gameVersion}
	for _, addon := range addons {
		path := addon.Dir + "/" + addon.File
		entry := known[path]
		out := sourceOutcome{File: addon.File}
		source, err := p.content.get(entry.Source, row.Software)
		if err != nil {
			out.Error = err.Error()
			results = append(results, out)
			continue
		}
		_, version, _, err := source.File(ctx, target, entry.ProjectID, "")
		switch {
		case err != nil:
			out.Error = err.Error()
		case version.ID == entry.VersionID:
			out.Error = "schon aktuell"
		default:
			if _, err := p.installFromSource(ctx, row, target, entry.Source, source, entry.ProjectID, version.ID, path); err != nil {
				out.Error = err.Error()
			} else {
				out.Updated, out.To = true, version.Number
			}
		}
		results = append(results, out)
	}
	return results
}
