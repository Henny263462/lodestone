package panel

import (
	"context"
	"crypto/subtle"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/lodestone-mc/lodestone/internal/node"
)

// prometheus liefert GET /metrics im Textformat von Prometheus. Die Route gibt es nur, wenn LODESTONE_METRICS_TOKEN
// gesetzt ist; der Abfrager schickt das Token als „Authorization: Bearer …“.
func (p *Panel) prometheus(token string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if token == "" {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "Messwerte für Prometheus sind abgeschaltet. Setze LODESTONE_METRICS_TOKEN, um sie einzuschalten."})
			return
		}
		given := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		if subtle.ConstantTimeCompare([]byte(given), []byte(token)) != 1 {
			w.Header().Set("WWW-Authenticate", "Bearer")
			writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "falsches oder fehlendes Token"})
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 8*time.Second)
		defer cancel()
		w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		_, _ = w.Write([]byte(p.metricsText(ctx)))
	}
}

// metric ist eine Kennzahl mit Hilfetext und ihren Werten.
type metric struct {
	name, help, kind string
	samples          []string
}

func (m *metric) add(labels string, value float64) {
	m.samples = append(m.samples, fmt.Sprintf("%s{%s} %s", m.name, labels, formatFloat(value)))
}

func formatFloat(value float64) string {
	if value == float64(int64(value)) {
		return fmt.Sprintf("%d", int64(value))
	}
	return fmt.Sprintf("%g", value)
}

// promLabels baut die Label-Liste; Werte werden nach den Regeln des Textformats maskiert.
func promLabels(pairs ...string) string {
	escape := strings.NewReplacer(`\`, `\\`, `"`, `\"`, "\n", `\n`)
	parts := make([]string, 0, len(pairs)/2)
	for i := 0; i+1 < len(pairs); i += 2 {
		parts = append(parts, pairs[i]+`="`+escape.Replace(pairs[i+1])+`"`)
	}
	return strings.Join(parts, ",")
}

func (p *Panel) metricsText(ctx context.Context) string {
	nodeUp := &metric{name: "lodestone_node_up", help: "1, wenn der Node erreichbar ist.", kind: "gauge"}
	nodeMem := &metric{name: "lodestone_node_memory_available_bytes", help: "Freier Arbeitsspeicher der Maschine des Nodes.", kind: "gauge"}
	nodeServers := &metric{name: "lodestone_node_servers", help: "Zahl der Server auf dem Node.", kind: "gauge"}
	serverUp := &metric{name: "lodestone_server_up", help: "1, wenn der Server läuft.", kind: "gauge"}
	serverState := &metric{name: "lodestone_server_state", help: "Zustand des Servers (das Label state ist der aktuelle Zustand).", kind: "gauge"}
	players := &metric{name: "lodestone_server_players", help: "Spieler online (letzte Messung, alle 30 Sekunden).", kind: "gauge"}
	maxPlayers := &metric{name: "lodestone_server_players_max", help: "Höchstzahl der Spieler laut Server.", kind: "gauge"}
	cpu := &metric{name: "lodestone_server_cpu_percent", help: "CPU-Last des Servers in Prozent eines Kerns.", kind: "gauge"}
	memory := &metric{name: "lodestone_server_memory_bytes", help: "Belegter Arbeitsspeicher des Servers.", kind: "gauge"}
	memoryLimit := &metric{name: "lodestone_server_memory_limit_bytes", help: "Arbeitsspeicher-Limit des Servers.", kind: "gauge"}
	uptime := &metric{name: "lodestone_server_uptime_seconds", help: "Laufzeit seit dem Start.", kind: "gauge"}

	nodes, _ := p.store.ListNodes()
	rows, _ := p.store.ListServers("")
	projects, _ := p.store.ListProjects("", true)
	projectNames := map[string]string{}
	for _, project := range projects {
		projectNames[project.ID] = project.Name
	}
	perNode := map[string]int{}
	for _, row := range rows {
		perNode[row.NodeID]++
	}

	type nodeResult struct {
		views     map[string]node.View
		err       error
		available int64
	}
	results := make([]nodeResult, len(nodes))
	var group sync.WaitGroup
	for i, n := range nodes {
		group.Add(1)
		go func() {
			defer group.Done()
			views, err := p.nodeViews(ctx, n.ID)
			result := nodeResult{views: views, err: err}
			if err == nil {
				var system struct {
					MemoryAvailable int64 `json:"memoryAvailable"`
				}
				callCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
				if p.nodeClient(n).Do(callCtx, http.MethodGet, "/api/system", nil, &system) == nil {
					result.available = system.MemoryAvailable
				}
				cancel()
			}
			results[i] = result
		}()
	}
	group.Wait()

	now := time.Now()
	nodeNames := map[string]string{}
	views := map[string]node.View{}
	reachable := map[string]bool{}
	for i, n := range nodes {
		nodeNames[n.ID] = n.Name
		labels := promLabels("node", n.Name, "node_id", n.ID)
		up := results[i].err == nil
		reachable[n.ID] = up
		nodeUp.add(labels, boolFloat(up))
		nodeServers.add(labels, float64(perNode[n.ID]))
		if results[i].available > 0 {
			nodeMem.add(labels, float64(results[i].available))
		}
		for id, view := range results[i].views {
			views[id] = view
		}
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].ID < rows[j].ID })
	for _, row := range rows {
		view, ok := views[row.ID]
		if !reachable[row.NodeID] || !ok {
			continue
		}
		labels := promLabels("server", row.Name, "server_id", row.ID, "project", projectNames[row.ProjectID], "project_id", row.ProjectID,
			"node", nodeNames[row.NodeID], "software", row.Software, "role", row.Role)
		running := view.State == node.StateRunning
		serverUp.add(labels, boolFloat(running))
		serverState.add(labels+","+promLabels("state", string(view.State)), 1)
		live := p.liveOf(row.ID)
		if running {
			players.add(labels, float64(live.Players))
			if live.MaxPlayers > 0 {
				maxPlayers.add(labels, float64(live.MaxPlayers))
			}
		}
		if view.Stats != nil && view.State != node.StateOffline {
			cpu.add(labels, view.Stats.CPUPercent)
			memory.add(labels, float64(view.Stats.MemoryBytes))
			memoryLimit.add(labels, float64(view.Stats.MemoryLimit))
		}
		if view.StartedAt > 0 {
			uptime.add(labels, float64(now.Sub(time.UnixMilli(view.StartedAt))/time.Second))
		}
	}

	var out strings.Builder
	out.WriteString("# HELP lodestone_up 1, solange das Panel antwortet.\n# TYPE lodestone_up gauge\nlodestone_up 1\n")
	for _, m := range []*metric{nodeUp, nodeMem, nodeServers, serverUp, serverState, players, maxPlayers, cpu, memory, memoryLimit, uptime} {
		fmt.Fprintf(&out, "# HELP %s %s\n# TYPE %s %s\n", m.name, m.help, m.name, m.kind)
		for _, sample := range m.samples {
			out.WriteString(sample)
			out.WriteByte('\n')
		}
	}
	return out.String()
}

func boolFloat(value bool) float64 {
	if value {
		return 1
	}
	return 0
}
