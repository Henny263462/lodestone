package panel

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/lodestone-mc/lodestone/internal/node"
)

// ---------------------------------------------------------------- Live-Aktualisierung Panel → Browser
//
// Der Browser hält eine Verbindung zu GET /api/live (Server-Sent Events). Darüber kommt nur, *dass* sich etwas
// geändert hat (Thema und IDs), nie die Daten selbst: Die Oberfläche lädt danach die passenden Abfragen über die
// normalen Routen neu, die ihre Rechte wie immer prüfen. Jede Meldung wird zusätzlich vor dem Senden gegen die
// Rechte des Empfängers geprüft, damit niemand erfährt, dass sich in einem fremden Projekt etwas tut.

// Update ist eine Änderungsmeldung an die Oberfläche.
type Update struct {
	// Topic ist die Art der Änderung: server, players, backups, events, ops, project, node, resync.
	Topic     string `json:"topic"`
	ProjectID string `json:"projectId,omitempty"`
	ServerID  string `json:"serverId,omitempty"`
	NodeID    string `json:"nodeId,omitempty"`
	// Area ist der Bereich unter /servers/{id}/… bzw. /projects/{id}/…, in dem sich etwas geändert hat (z. B. backups).
	Area string `json:"area,omitempty"`
}

const (
	updateBuffer   = 64
	updatePerUser  = 8
	updateMaxTotal = 512
)

// Zeiten des Kanals; in Tests kürzer.
var (
	updateBeat    = 25 * time.Second // Lebenszeichen, damit Proxys die Verbindung nicht schließen
	updateRecheck = 30 * time.Second // so oft wird geprüft, ob die Anmeldung noch gilt
)

type updateSub struct {
	userID string
	ch     chan Update
	lost   atomic.Bool // Meldungen sind verloren gegangen (Puffer voll): Der Browser soll alles neu laden
}

// updateHub verteilt Meldungen an alle offenen Verbindungen und merkt sich den letzten Zustand der Server, um
// Änderungen zu erkennen, die auf dem Node passieren (Start fertig, Absturz).
type updateHub struct {
	mu      sync.Mutex
	subs    map[*updateSub]struct{}
	perUser map[string]int

	stateMu sync.Mutex
	states  map[string]string // Node-ID/Server-ID → Zustand aus nodeViews
	nodeUp  map[string]bool
}

func newUpdateHub() *updateHub {
	return &updateHub{subs: map[*updateSub]struct{}{}, perUser: map[string]int{}, states: map[string]string{}, nodeUp: map[string]bool{}}
}

func (h *updateHub) subscribe(userID string) (*updateSub, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if len(h.subs) >= updateMaxTotal || h.perUser[userID] >= updatePerUser {
		return nil, fail(http.StatusTooManyRequests, "Zu viele offene Live-Verbindungen. Schließe ein paar Tabs, die Seiten laden dann regelmäßig selbst neu.")
	}
	sub := &updateSub{userID: userID, ch: make(chan Update, updateBuffer)}
	h.subs[sub] = struct{}{}
	h.perUser[userID]++
	return sub, nil
}

func (h *updateHub) unsubscribe(sub *updateSub) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if _, ok := h.subs[sub]; !ok {
		return
	}
	delete(h.subs, sub)
	if h.perUser[sub.userID]--; h.perUser[sub.userID] <= 0 {
		delete(h.perUser, sub.userID)
	}
}

// send verteilt ohne zu warten: Ist der Puffer einer Verbindung voll, merkt sie sich das und schickt später „resync“.
func (h *updateHub) send(update Update) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for sub := range h.subs {
		select {
		case sub.ch <- update:
		default:
			sub.lost.Store(true)
		}
	}
}

func (h *updateHub) count() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.subs)
}

// publish meldet eine Änderung. Fehlt das Projekt, wird es aus dem Server gelesen; ohne Projekt geht nur „node“ raus.
func (p *Panel) publish(update Update) {
	if p.updates == nil || p.updates.count() == 0 {
		return
	}
	if update.ProjectID == "" && update.ServerID != "" {
		row, err := p.store.ServerByID(update.ServerID)
		if err != nil {
			return
		}
		update.ProjectID = row.ProjectID
	}
	if update.ProjectID == "" && update.Topic != "node" {
		return
	}
	p.updates.send(update)
}

// observeViews vergleicht frisch gelesene Zustände mit den zuletzt gesehenen und meldet Wechsel (aus nodeViews).
func (p *Panel) observeViews(nodeID string, views map[string]node.View, err error) {
	if p.updates == nil {
		return
	}
	h := p.updates
	var changed []string
	up := err == nil
	h.stateMu.Lock()
	was, known := h.nodeUp[nodeID]
	h.nodeUp[nodeID] = up
	if up {
		for id, view := range views {
			state := fmt.Sprintf("%s|%s|%s|%t|%t", view.State, view.LastError, view.Version, view.Installed, view.AutoconfigPending)
			key := nodeID + "/" + id
			if before, seen := h.states[key]; seen && before != state {
				changed = append(changed, id)
			}
			h.states[key] = state
		}
		// Gelöschte oder umgezogene Server vergessen
		for key := range h.states {
			if id, ok := strings.CutPrefix(key, nodeID+"/"); ok {
				if _, still := views[id]; !still {
					delete(h.states, key)
				}
			}
		}
	}
	h.stateMu.Unlock()
	if known && was != up {
		p.publish(Update{Topic: "node", NodeID: nodeID})
	}
	for _, id := range changed {
		p.publish(Update{Topic: "server", ServerID: id})
	}
}

// changeOf leitet aus einer erfolgreichen, verändernden Anfrage die Meldung ab: alles unter /api/servers/{id},
// /api/projects/{id} und /api/nodes/{id} (Einstellungen, playit, Updates). Das Projekt eines Servers wird vor dem Handler gelesen (danach ist er vielleicht gelöscht).
func (p *Panel) changeOf(r *http.Request) (Update, bool) {
	if p.updates == nil || p.updates.count() == 0 {
		return Update{}, false // niemand hört zu: keine Datenbankabfrage
	}
	parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/") // api, servers, <id>, <area> …
	if len(parts) < 3 || parts[0] != "api" {
		return Update{}, false
	}
	area := ""
	if len(parts) > 3 {
		area = parts[3]
	}
	switch parts[1] {
	case "servers":
		row, err := p.store.ServerByID(parts[2])
		if err != nil {
			return Update{}, false
		}
		return Update{Topic: "server", ProjectID: row.ProjectID, ServerID: row.ID, Area: area}, true
	case "projects":
		return Update{Topic: "project", ProjectID: parts[2], Area: area}, true
	case "nodes":
		nodeID := parts[2]
		if nodeID == "update-all" {
			nodeID = ""
		}
		return Update{Topic: "node", NodeID: nodeID, Area: area}, true
	}
	return Update{}, false
}

// updatePermission ist das Recht, das eine Meldung über die Mitgliedschaft hinaus braucht (wie die Route dahinter).
func updatePermission(update Update) Permission {
	switch {
	case update.Topic == "backups" || update.Area == "backups" || update.Area == "backup-settings":
		return PermBackupsRead
	case update.Area == "files" || update.Area == "worlds" || update.Area == "properties" || update.Area == "sftp":
		return PermFilesRead
	case update.Area == "logs" || update.Area == "command":
		return PermConsoleRead
	case update.Area == "webhooks" || update.Area == "console-triggers":
		return PermWebhooks
	case update.Area == "backup-target" || update.Area == "discord":
		return PermSettings
	}
	return ""
}

// updateVisible sagt, ob der Benutzer die Meldung sehen darf. Nodes sieht jeder Angemeldete (wie GET /api/nodes).
func (p *Panel) updateVisible(user User, update Update) bool {
	if update.Topic == "resync" {
		return true
	}
	if update.ProjectID == "" {
		return update.Topic == "node"
	}
	access, err := p.projectAccess(user, update.ProjectID)
	if err != nil {
		return false
	}
	if need := updatePermission(update); need != "" && !access.Has(need) {
		return false
	}
	return true
}

// currentUser liest die Anmeldung der Anfrage erneut (Sitzung oder API-Schlüssel), damit eine lange offene
// Verbindung endet, sobald die Sitzung abläuft, der Benutzer gesperrt wird oder sich abmeldet.
func (p *Panel) currentUser(r *http.Request) (User, bool) {
	if header := r.Header.Get("Authorization"); strings.HasPrefix(header, "Bearer "+keyPrefix) {
		user, err := p.store.APIKeyUser(hashToken(strings.TrimPrefix(header, "Bearer ")))
		return user, err == nil
	}
	cookie, err := r.Cookie(cookieName)
	if err != nil || cookie.Value == "" {
		return User{}, false
	}
	user, err := p.store.SessionUser(hashToken(cookie.Value))
	return user, err == nil
}

// liveUpdates ist GET /api/live: ein Strom von Server-Sent Events mit Änderungsmeldungen.
func (p *Panel) liveUpdates(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, fail(http.StatusInternalServerError, "Live-Aktualisierung wird hier nicht unterstützt"))
		return
	}
	user := userFrom(r.Context())
	sub, err := p.updates.subscribe(user.ID)
	if err != nil {
		writeError(w, err)
		return
	}
	defer p.updates.unsubscribe(sub)

	// Die Verbindung bleibt offen; eine Schreibfrist des Servers darf sie nicht beenden
	_ = http.NewResponseController(w).SetWriteDeadline(time.Time{})
	header := w.Header()
	header.Set("Content-Type", "text/event-stream")
	header.Set("Cache-Control", "no-store")
	header.Set("X-Accel-Buffering", "no") // nginx & Co. sollen nicht puffern
	w.WriteHeader(http.StatusOK)
	write := func(update Update) bool {
		data, _ := json.Marshal(update)
		if _, err := fmt.Fprintf(w, "data: %s\n\n", data); err != nil {
			return false
		}
		flusher.Flush()
		return true
	}
	if _, err := fmt.Fprint(w, "retry: 5000\n\n"); err != nil {
		return
	}
	flusher.Flush()

	beat := time.NewTicker(updateBeat)
	defer beat.Stop()
	recheck := time.NewTicker(updateRecheck)
	defer recheck.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case <-beat.C:
			if _, err := fmt.Fprint(w, ": da\n\n"); err != nil {
				return
			}
			flusher.Flush()
		case <-recheck.C:
			fresh, ok := p.currentUser(r)
			if !ok {
				_, _ = fmt.Fprint(w, "event: end\ndata: {}\n\n")
				flusher.Flush()
				return
			}
			user = fresh // z. B. Administrator-Recht entzogen
		case update := <-sub.ch:
			if p.updateVisible(user, update) && !write(update) {
				return
			}
			if len(sub.ch) == 0 && sub.lost.Swap(false) && !write(Update{Topic: "resync"}) {
				return
			}
		}
	}
}
