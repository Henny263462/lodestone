package panel

import (
	"context"
	"net/http"
	"net/url"
	"time"
)

// registerWorldRoutes hängt die Routen für Welten, server.properties, Datapacks und die eigene Ordnung der Projekte ein.
// Die Server-Routen sind spezifischer als der allgemeine Durchgriff /api/servers/{id}/{rest...} und gehen deshalb vor.
func (p *Panel) registerWorldRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/servers/{id}/properties", p.authenticate(p.getProperties))
	mux.HandleFunc("PUT /api/servers/{id}/properties", p.authenticate(p.putProperties))
	mux.HandleFunc("GET /api/servers/{id}/worlds", p.authenticate(p.listWorlds))
	mux.HandleFunc("POST /api/servers/{id}/worlds/{world}/reset", p.authenticate(p.resetWorld))
	mux.HandleFunc("GET /api/servers/{id}/worlds/{world}/download", p.authenticate(p.worldPassthrough(rankOperator, "/download")))
	mux.HandleFunc("POST /api/servers/{id}/worlds/{world}/upload", p.authenticate(p.worldPassthrough(rankAdmin, "/upload")))
	mux.HandleFunc("GET /api/servers/{id}/worlds/{world}/datapacks", p.authenticate(p.worldPassthrough(rankOperator, "/datapacks")))
	mux.HandleFunc("POST /api/servers/{id}/worlds/{world}/datapacks", p.authenticate(p.worldPassthrough(rankOperator, "/datapacks")))
	mux.HandleFunc("DELETE /api/servers/{id}/worlds/{world}/datapacks/{name}", p.authenticate(p.deleteDatapack))

	mux.HandleFunc("GET /api/projects/{id}/live-players", p.authenticate(p.projectLivePlayers))
	mux.HandleFunc("GET /api/me/project-prefs", p.authenticate(p.listProjectPrefs))
	mux.HandleFunc("PUT /api/me/project-prefs/{project}", p.authenticate(p.putProjectPref))
	mux.HandleFunc("PUT /api/me/project-order", p.authenticate(p.putProjectOrder))
}

// ---------------------------------------------------------------- Welten und server.properties

// requireGameServer ist requireServer für Minecraft-Server mit Welten; ein Velocity-Proxy hat keine.
func (p *Panel) requireGameServer(r *http.Request, minimum int) (ServerRow, *NodeClient, error) {
	row, _, _, err := p.requireServer(r, minimum)
	if err != nil {
		return ServerRow{}, nil, err
	}
	if row.Role == RoleProxy || row.Software == "velocity" {
		return ServerRow{}, nil, fail(http.StatusBadRequest, "Velocity hat keine Welten und keine server.properties; seine Einstellungen stehen in velocity.toml (unter Dateien)")
	}
	client, _, err := p.client(row.NodeID)
	if err != nil {
		return ServerRow{}, nil, err
	}
	return row, client, nil
}

// worldName liest {world} und prüft ihn wie der Node (ein einzelner Ordnername).
func worldName(r *http.Request) (string, error) {
	name := r.PathValue("world")
	if !worldNameRule.MatchString(name) {
		return "", fail(http.StatusBadRequest, "ungültiger Weltname (erlaubt sind Buchstaben, Ziffern, Leerzeichen und _ . + -)")
	}
	return name, nil
}

func (p *Panel) getProperties(w http.ResponseWriter, r *http.Request) {
	row, client, err := p.requireGameServer(r, rankOperator)
	if err != nil {
		writeError(w, err)
		return
	}
	var view struct {
		Exists  bool `json:"exists"`
		Entries []struct {
			Key   string `json:"key"`
			Value string `json:"value"`
		} `json:"entries"`
	}
	if err := client.Do(r.Context(), http.MethodGet, "/api/servers/"+row.ID+"/properties", nil, &view); err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"exists": view.Exists, "entries": view.Entries, "locked": lockedProperties(row)})
}

func (p *Panel) putProperties(w http.ResponseWriter, r *http.Request) {
	row, client, err := p.requireGameServer(r, rankAdmin)
	if err != nil {
		writeError(w, err)
		return
	}
	var body struct {
		Values map[string]string `json:"values"`
	}
	if err := decode(r, &body); err != nil {
		writeError(w, err)
		return
	}
	if len(body.Values) == 0 {
		writeError(w, fail(http.StatusBadRequest, "es wurde nichts geändert"))
		return
	}
	if len(body.Values) > 256 {
		writeError(w, fail(http.StatusBadRequest, "zu viele Werte auf einmal"))
		return
	}
	locked := lockedProperties(row)
	for key, value := range body.Values {
		if reason, ok := locked[key]; ok {
			writeError(w, fail(http.StatusBadRequest, key+" lässt sich hier nicht ändern. "+reason))
			return
		}
		if err := validateProperty(key, value); err != nil {
			writeError(w, err)
			return
		}
	}
	var view map[string]any
	if err := client.Do(r.Context(), http.MethodPut, "/api/servers/"+row.ID+"/properties", map[string]any{"set": body.Values}, &view); err != nil {
		writeError(w, err)
		return
	}
	if view == nil {
		view = map[string]any{}
	}
	view["locked"] = locked
	writeJSON(w, http.StatusOK, view)
}

func (p *Panel) listWorlds(w http.ResponseWriter, r *http.Request) {
	row, client, err := p.requireGameServer(r, rankOperator)
	if err != nil {
		writeError(w, err)
		return
	}
	var list map[string]any
	if err := client.Do(r.Context(), http.MethodGet, "/api/servers/"+row.ID+"/worlds", nil, &list); err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, list)
}

func (p *Panel) resetWorld(w http.ResponseWriter, r *http.Request) {
	row, client, err := p.requireGameServer(r, rankAdmin)
	if err != nil {
		writeError(w, err)
		return
	}
	world, err := worldName(r)
	if err != nil {
		writeError(w, err)
		return
	}
	var body struct {
		Backup     bool    `json:"backup"`
		Seed       *string `json:"seed"`
		Dimensions bool    `json:"dimensions"`
	}
	if err := decode(r, &body); err != nil {
		writeError(w, err)
		return
	}
	if body.Seed != nil {
		if err := validateProperty("level-seed", *body.Seed); err != nil {
			writeError(w, err)
			return
		}
	}
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	var view map[string]any
	if err := client.Do(ctx, http.MethodPost, "/api/servers/"+row.ID+"/worlds/"+url.PathEscape(world)+"/reset", body, &view); err != nil {
		writeError(w, err)
		return
	}
	p.invalidate(row.NodeID)
	writeJSON(w, http.StatusAccepted, view)
}

// worldPassthrough reicht Downloads, Uploads und die Datapack-Liste als Strom an den Node durch (nach Rechteprüfung).
func (p *Panel) worldPassthrough(minimum int, suffix string) func(http.ResponseWriter, *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		row, client, err := p.requireGameServer(r, minimum)
		if err != nil {
			writeError(w, err)
			return
		}
		world, err := worldName(r)
		if err != nil {
			writeError(w, err)
			return
		}
		client.Proxy(w, r, "/api/servers/"+row.ID+"/worlds/"+world+suffix)
		if r.Method != http.MethodGet {
			p.invalidate(row.NodeID)
		}
	}
}

func (p *Panel) deleteDatapack(w http.ResponseWriter, r *http.Request) {
	row, client, err := p.requireGameServer(r, rankOperator)
	if err != nil {
		writeError(w, err)
		return
	}
	world, err := worldName(r)
	if err != nil {
		writeError(w, err)
		return
	}
	name := r.PathValue("name")
	if name == "" || len(name) > 128 {
		writeError(w, fail(http.StatusBadRequest, "ungültiger Name des Datapacks"))
		return
	}
	if err := client.Do(r.Context(), http.MethodDelete, "/api/servers/"+row.ID+"/worlds/"+url.PathEscape(world)+"/datapacks/"+url.PathEscape(name), nil, nil); err != nil {
		writeError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// projectLivePlayers liefert die Spielerzahl jedes Servers aus der letzten Messung (zum Sortieren der Serverliste).
func (p *Panel) projectLivePlayers(w http.ResponseWriter, r *http.Request) {
	project, _, err := p.requireProject(r, r.PathValue("id"), rankViewer)
	if err != nil {
		writeError(w, err)
		return
	}
	rows, err := p.store.ListServers(project.ID)
	if err != nil {
		writeError(w, err)
		return
	}
	players := map[string]int{}
	for _, row := range rows {
		live := p.liveOf(row.ID)
		if live.Running && time.Since(live.At) < 3*time.Minute {
			players[row.ID] = live.Players
		} else {
			players[row.ID] = 0
		}
	}
	writeJSON(w, http.StatusOK, players)
}

// ---------------------------------------------------------------- Eigene Ordnung der Projekte

// visiblePrefs filtert die Ordnung auf Projekte, die der Benutzer (noch) sieht.
func (p *Panel) visiblePrefs(user User) ([]ProjectPref, error) {
	prefs, err := p.store.ProjectPrefs(user.ID)
	if err != nil {
		return nil, err
	}
	visible := []ProjectPref{}
	for _, pref := range prefs {
		if rank, err := p.projectRank(user, pref.ProjectID); err == nil && rank >= rankViewer {
			visible = append(visible, pref)
		}
	}
	return visible, nil
}

func (p *Panel) listProjectPrefs(w http.ResponseWriter, r *http.Request) {
	prefs, err := p.visiblePrefs(userFrom(r.Context()))
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, prefs)
}

func (p *Panel) putProjectPref(w http.ResponseWriter, r *http.Request) {
	project, _, err := p.requireProject(r, r.PathValue("project"), rankViewer)
	if err != nil {
		writeError(w, err)
		return
	}
	var body struct {
		Favorite *bool     `json:"favorite"`
		Tags     *[]string `json:"tags"`
	}
	if err := decode(r, &body); err != nil {
		writeError(w, err)
		return
	}
	user := userFrom(r.Context())
	pref, err := p.store.ProjectPref(user.ID, project.ID)
	if err != nil {
		writeError(w, err)
		return
	}
	if body.Favorite != nil {
		pref.Favorite = *body.Favorite
	}
	if body.Tags != nil {
		if pref.Tags, err = cleanTags(*body.Tags); err != nil {
			writeError(w, err)
			return
		}
	}
	if err := p.store.SetProjectPref(user.ID, pref); err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, pref)
}

func (p *Panel) putProjectOrder(w http.ResponseWriter, r *http.Request) {
	var body struct {
		ProjectIDs []string `json:"projectIds"`
	}
	if err := decode(r, &body); err != nil {
		writeError(w, err)
		return
	}
	if len(body.ProjectIDs) > maxOrderedIDs {
		writeError(w, fail(http.StatusBadRequest, "zu viele Projekte in der Liste"))
		return
	}
	user := userFrom(r.Context())
	seen := map[string]bool{}
	for _, id := range body.ProjectIDs {
		if seen[id] {
			writeError(w, fail(http.StatusBadRequest, "ein Projekt steht doppelt in der Liste"))
			return
		}
		seen[id] = true
		if _, err := p.projectRank(user, id); err != nil {
			writeError(w, fail(http.StatusBadRequest, "unbekanntes Projekt in der Liste"))
			return
		}
	}
	if err := p.store.SetProjectOrder(user.ID, body.ProjectIDs); err != nil {
		writeError(w, err)
		return
	}
	prefs, err := p.visiblePrefs(user)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, prefs)
}
