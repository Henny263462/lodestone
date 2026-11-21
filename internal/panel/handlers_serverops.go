package panel

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/lodestone-mc/lodestone/internal/node"
)

// registerServerOpsRoutes hängt Klonen, Umzug und Speicherplatz ein (Aufruf in panel.go).
func (p *Panel) registerServerOpsRoutes(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/servers/{id}/clone", p.authenticate(p.cloneServer))
	mux.HandleFunc("POST /api/projects/{id}/migrate", p.authenticate(p.migrateProject))
	mux.HandleFunc("GET /api/projects/{id}/server-ops", p.authenticate(p.listServerOps))
	mux.HandleFunc("GET /api/server-ops/{id}", p.authenticate(p.getServerOp))
	mux.HandleFunc("POST /api/server-ops/{id}/cancel", p.authenticate(p.cancelServerOp))
	mux.HandleFunc("GET /api/servers/{id}/storage", p.authenticate(p.serverStorage))
	mux.HandleFunc("PUT /api/servers/{id}/storage", p.authenticate(p.putServerStorage))
	mux.HandleFunc("POST /api/servers/{id}/storage/measure", p.authenticate(p.measureServerStorage))
	mux.HandleFunc("GET /api/nodes/{id}/storage", p.authenticate(p.nodeStorage))
}

// ---------------------------------------------------------------- Klonen und Umzug

// cloneServer legt eine Kopie an: einen neuen Server mit allen Dateien des alten.
func (p *Panel) cloneServer(w http.ResponseWriter, r *http.Request) {
	row, project, _, err := p.requireServerPermission(r, PermServers, PermFilesRead)
	if err != nil {
		writeError(w, err)
		return
	}
	var body cloneBody
	if err := decode(r, &body); err != nil {
		writeError(w, err)
		return
	}
	body.Name = strings.TrimSpace(body.Name)
	if body.Name == "" || len(body.Name) > 48 {
		writeError(w, fail(http.StatusBadRequest, "Der Name braucht 1 bis 48 Zeichen."))
		return
	}
	if Slugify(body.Name) == "" {
		writeError(w, fail(http.StatusBadRequest, "Der Name braucht mindestens einen Buchstaben oder eine Ziffer."))
		return
	}
	switch {
	case row.GroupID != "":
		writeError(w, fail(http.StatusBadRequest, "Instanzen einer Gruppe legt der Autoscaler selbst an. Ändere stattdessen die Gruppe."))
		return
	case row.Role == RoleProxy:
		writeError(w, fail(http.StatusConflict, "Ein Netzwerk hat genau einen Proxy, er lässt sich deshalb nicht kopieren."))
		return
	}
	if project.Kind == "network" {
		existing, err := p.store.ListServers(project.ID)
		if err != nil {
			writeError(w, err)
			return
		}
		for _, other := range existing {
			if other.Slug == Slugify(body.Name) {
				writeError(w, fail(http.StatusConflict, "Einen Server mit diesem Namen gibt es im Projekt schon."))
				return
			}
		}
	}
	client, _, err := p.client(row.NodeID)
	if err != nil {
		writeError(w, err)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 8*time.Second)
	defer cancel()
	var storage node.StorageInfo
	if err := client.Do(ctx, http.MethodGet, "/api/servers/"+row.ID+"/storage", nil, &storage); err != nil {
		writeError(w, err)
		return
	}
	if storage.Free > 0 && storage.Bytes > 0 && storage.Free < storage.Bytes+moveReserve {
		writeError(w, fail(http.StatusConflict, "Auf dem Node ist zu wenig Platz für die Kopie frei (frei "+humanBytes(storage.Free)+", nötig etwa "+humanBytes(storage.Bytes+moveReserve)+")."))
		return
	}
	user := userFrom(r.Context())
	op := ServerOp{ProjectID: project.ID, ServerID: row.ID, Kind: "clone", Label: "„" + row.Name + "“ → „" + body.Name + "“", Step: "Wird vorbereitet",
		Total: storage.Bytes, SourceNodeID: row.NodeID, TargetNodeID: row.NodeID, UserID: user.ID}
	created, err := p.startOp(op, func(runner *opRunner) (opResult, error) { return p.runClone(runner, row, project, body, user) })
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusAccepted, created)
}

// migrateProject zieht die Server um; dafür werden sie gestoppt und auf dem neuen Node wieder gestartet.
func (p *Panel) migrateProject(w http.ResponseWriter, r *http.Request) {
	project, _, err := p.requireProjectPermission(r, r.PathValue("id"), PermServers, PermPower)
	if err != nil {
		writeError(w, err)
		return
	}
	var body struct {
		NodeID string `json:"nodeId"`
	}
	if err := decode(r, &body); err != nil {
		writeError(w, err)
		return
	}
	plan, err := p.planMigration(r.Context(), project, strings.TrimSpace(body.NodeID))
	if err != nil {
		writeError(w, err)
		return
	}
	op := ServerOp{ProjectID: project.ID, ServerID: plan.rows[0].ID, Kind: "migrate", Label: "„" + project.Name + "“: „" + plan.source.Name + "“ → „" + plan.target.Name + "“",
		Step: "Wird vorbereitet", Total: plan.total, SourceNodeID: plan.source.ID, TargetNodeID: plan.target.ID, UserID: userFrom(r.Context()).ID}
	created, err := p.startOp(op, func(runner *opRunner) (opResult, error) { return p.runMigration(runner, plan) })
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusAccepted, created)
}

func (p *Panel) listServerOps(w http.ResponseWriter, r *http.Request) {
	project, _, err := p.requireProjectPermission(r, r.PathValue("id"))
	if err != nil {
		writeError(w, err)
		return
	}
	ops, err := p.store.ListOps(project.ID, 10)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, ops)
}

// requireOp lädt einen Vorgang und prüft die Rechte im Projekt (fremde Vorgänge gibt es nicht).
func (p *Panel) requireOp(r *http.Request, needed ...Permission) (ServerOp, error) {
	op, err := p.store.OpByID(r.PathValue("id"))
	if err != nil {
		return ServerOp{}, errNotFound
	}
	if _, _, err := p.requireProjectPermission(r, op.ProjectID, needed...); err != nil {
		return ServerOp{}, err
	}
	return op, nil
}

func (p *Panel) getServerOp(w http.ResponseWriter, r *http.Request) {
	op, err := p.requireOp(r)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, op)
}

func (p *Panel) cancelServerOp(w http.ResponseWriter, r *http.Request) {
	op, err := p.requireOp(r, PermServers)
	if err != nil {
		writeError(w, err)
		return
	}
	if op.State != OpRunning {
		writeError(w, fail(http.StatusConflict, "Der Vorgang ist schon beendet."))
		return
	}
	if err := p.store.RequestOpCancel(op.ID); err != nil {
		writeError(w, err)
		return
	}
	op.Cancel = true
	writeJSON(w, http.StatusAccepted, op)
}

// ---------------------------------------------------------------- Speicherplatz

func (p *Panel) serverStorage(w http.ResponseWriter, r *http.Request) {
	row, _, _, err := p.requireServerPermission(r)
	if err != nil {
		writeError(w, err)
		return
	}
	p.storageCall(w, r, row, http.MethodGet, "", nil, http.StatusOK)
}

func (p *Panel) putServerStorage(w http.ResponseWriter, r *http.Request) {
	row, _, _, err := p.requireServerPermission(r, PermSettings)
	if err != nil {
		writeError(w, err)
		return
	}
	var body struct {
		LimitMB int `json:"limitMB"`
	}
	if err := decode(r, &body); err != nil {
		writeError(w, err)
		return
	}
	if body.LimitMB != 0 && (body.LimitMB < 256 || body.LimitMB > 16<<20) {
		writeError(w, fail(http.StatusBadRequest, "Das Speicherlimit muss 0 (keins) oder zwischen 256 MB und 16 TB liegen."))
		return
	}
	p.storageCall(w, r, row, http.MethodPut, "", body, http.StatusOK)
}

func (p *Panel) measureServerStorage(w http.ResponseWriter, r *http.Request) {
	row, _, _, err := p.requireServerPermission(r, PermFilesRead)
	if err != nil {
		writeError(w, err)
		return
	}
	p.storageCall(w, r, row, http.MethodPost, "/measure", nil, http.StatusAccepted)
}

func (p *Panel) storageCall(w http.ResponseWriter, r *http.Request, row ServerRow, method, suffix string, body any, status int) {
	client, _, err := p.client(row.NodeID)
	if err != nil {
		writeError(w, err)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	var info node.StorageInfo
	if err := client.Do(ctx, method, "/api/servers/"+row.ID+"/storage"+suffix, body, &info); err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, status, info)
}

type nodeStorageServer struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	ProjectID   string `json:"projectId"`
	ProjectName string `json:"projectName"`
	Bytes       int64  `json:"bytes"`
	Backups     int64  `json:"backups"`
	LimitMB     int    `json:"limitMB"`
	MeasuredAt  int64  `json:"measuredAt"`
}

type nodeStorageDTO struct {
	Online bool   `json:"online"`
	Error  string `json:"error,omitempty"`
	Free   int64  `json:"free"`
	Total  int64  `json:"total"`
	// Used ist, was die Server des Panels belegen (Serverordner und Backups).
	Used int64 `json:"used"`
	// Servers stehen nur für Administratoren drin, sie nennen Server aller Projekte.
	Servers []nodeStorageServer `json:"servers,omitempty"`
}

// nodeStorage zeigt den Speicherplatz eines Nodes. Jeder Angemeldete sieht die Summen (wie die Node-Liste),
// die Aufteilung nach Servern nur ein Administrator.
func (p *Panel) nodeStorage(w http.ResponseWriter, r *http.Request) {
	n, err := p.store.NodeByID(r.PathValue("id"))
	if err != nil {
		writeError(w, errNotFound)
		return
	}
	storage, err := p.fetchNodeStorage(r.Context(), n)
	if err != nil {
		writeJSON(w, http.StatusOK, nodeStorageDTO{Error: opErrorText(err)})
		return
	}
	result := nodeStorageDTO{Online: true, Free: storage.Free, Total: storage.Total}
	rows, err := p.store.ListServers("")
	if err != nil {
		writeError(w, err)
		return
	}
	known := map[string]ServerRow{}
	for _, row := range rows {
		if row.NodeID == n.ID {
			known[row.ID] = row
		}
	}
	admin := userFrom(r.Context()).IsAdmin
	projects := map[string]string{}
	if admin {
		result.Servers = []nodeStorageServer{}
		if list, err := p.store.ListProjects("", true); err == nil {
			for _, project := range list {
				projects[project.ID] = project.Name
			}
		}
	}
	for _, entry := range storage.Servers {
		row, ok := known[entry.ID]
		if !ok {
			continue // Reste auf dem Node, die das Panel nicht kennt
		}
		result.Used += entry.Bytes + entry.Backups
		if admin {
			result.Servers = append(result.Servers, nodeStorageServer{ID: row.ID, Name: row.Name, ProjectID: row.ProjectID, ProjectName: projects[row.ProjectID],
				Bytes: entry.Bytes, Backups: entry.Backups, LimitMB: entry.LimitMB, MeasuredAt: entry.MeasuredAt})
		}
	}
	writeJSON(w, http.StatusOK, result)
}
