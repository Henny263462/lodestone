package panel

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/lodestone-mc/lodestone/internal/node"
)

// Automatik im Panel: holt erkannte Konsolen-Ereignisse von den Nodes ab (Spieler betritt/verlässt, eigene Muster,
// Schlafmodus), macht daraus Ereignisse und Webhooks, startet auf Wunsch Zeitpläne und lässt beim Proxy eines
// Netzwerks die Backend-Server mit einschlafen und aufwachen.

const (
	automatikEvery  = 5 * time.Second
	triggerCooldown = 30 * time.Second
)

// Neue Ereignisarten. Sie stehen hier statt in events.go, damit die Automatik an einer Stelle bleibt.
var automatikEventTitles = map[string]string{
	"player_join": "Spieler betritt", "player_leave": "Spieler verlässt", "console_match": "Konsolen-Auslöser",
	"sleep": "Server schläft ein", "wake": "Server wacht auf",
}

func init() {
	for _, kind := range []string{"player_join", "player_leave", "console_match", "sleep", "wake"} {
		eventKinds = append(eventKinds, kind)
		titles[kind] = automatikEventTitles[kind]
	}
}

// autoState hängt am Panel; der Nullwert ist benutzbar.
type autoState struct {
	mu       sync.Mutex
	cursors  map[string]watchCursor
	cooldown map[string]time.Time

	discordLimit limiter
	// discordBase und discordHTTP lassen sich in Tests auf eine Attrappe richten.
	discordBase string
	discordHTTP *http.Client
}

type watchCursor struct {
	known bool
	boot  string
	after int64
}

func (a *autoState) cursor(nodeID string) watchCursor {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.cursors[nodeID]
}

func (a *autoState) setCursor(nodeID, boot string, after int64) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.cursors == nil {
		a.cursors = map[string]watchCursor{}
	}
	a.cursors[nodeID] = watchCursor{known: true, boot: boot, after: after}
}

// cooled sagt, ob ein Auslöser für diesen Server wieder melden darf, und merkt sich den Zeitpunkt.
func (a *autoState) cooled(key string, now time.Time) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.cooldown == nil {
		a.cooldown = map[string]time.Time{}
	}
	if last, ok := a.cooldown[key]; ok && now.Sub(last) < triggerCooldown {
		return false
	}
	if len(a.cooldown) > 5000 {
		a.cooldown = map[string]time.Time{}
	}
	a.cooldown[key] = now
	return true
}

// automatikLoop fragt alle fünf Sekunden die Nodes ab, bis ctx endet.
func (p *Panel) automatikLoop(ctx context.Context) {
	ticker := time.NewTicker(automatikEvery)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			p.automatikTick(ctx, time.Now())
		}
	}
}

func (p *Panel) automatikTick(ctx context.Context, now time.Time) {
	rows, err := p.store.ListServers("")
	if err != nil || len(rows) == 0 {
		return
	}
	triggers, err := p.store.ListTriggers("")
	if err != nil {
		p.log.Warn("Konsolen-Auslöser nicht lesbar", "error", err)
		return
	}
	byNode := map[string][]ServerRow{}
	for _, row := range rows {
		byNode[row.NodeID] = append(byNode[row.NodeID], row)
	}
	for nodeID, list := range byNode {
		p.watchNode(ctx, nodeID, list, triggers, now)
	}
}

// watchNode schickt dem Node die Muster seiner Server und verarbeitet die neuen Ereignisse. Beim ersten Kontakt nach dem
// Start des Panels werden alte Ereignisse übersprungen, damit nichts doppelt gemeldet wird.
func (p *Panel) watchNode(ctx context.Context, nodeID string, rows []ServerRow, triggers []ConsoleTrigger, now time.Time) {
	servers := map[string]ServerRow{}
	patterns := map[string][]node.WatchPattern{}
	byID := map[string]ConsoleTrigger{}
	for _, trigger := range triggers {
		byID[trigger.ID] = trigger
	}
	for _, row := range rows {
		servers[row.ID] = row
		for _, trigger := range triggers {
			if trigger.Enabled && trigger.ProjectID == row.ProjectID && (trigger.ServerID == "" || trigger.ServerID == row.ID) {
				patterns[row.ID] = append(patterns[row.ID], node.WatchPattern{ID: trigger.ID, Pattern: trigger.Pattern})
			}
		}
	}
	client, _, err := p.client(nodeID)
	if err != nil {
		return
	}
	cursor := p.auto.cursor(nodeID)
	callCtx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	var response node.WatchResponse
	if err := client.Do(callCtx, http.MethodPost, "/api/console-watch", node.WatchRequest{Boot: cursor.boot, After: cursor.after, Patterns: patterns}, &response); err != nil {
		return // Node nicht erreichbar oder zu alt; das meldet schon watchLoop
	}
	p.auto.setCursor(nodeID, response.Boot, response.Latest)
	if !cursor.known {
		return
	}
	for _, event := range response.Events {
		row, ok := servers[event.ServerID]
		if !ok {
			continue
		}
		p.handleConsoleEvent(ctx, row, event, byID, now)
	}
}

func (p *Panel) handleConsoleEvent(ctx context.Context, row ServerRow, event node.ConsoleEvent, triggers map[string]ConsoleTrigger, now time.Time) {
	switch event.Kind {
	case "join":
		p.emit(row.ProjectID, row.ID, "player_join", "info", fmt.Sprintf("%s hat %s betreten.", event.Player, row.Name))
	case "leave":
		p.emit(row.ProjectID, row.ID, "player_leave", "info", fmt.Sprintf("%s hat %s verlassen.", event.Player, row.Name))
	case "wake":
		p.emit(row.ProjectID, row.ID, "wake", "info", fmt.Sprintf("%s wacht auf, weil %s beitreten möchte.", row.Name, event.Player))
		if row.Role == "proxy" {
			go p.followProxy(context.WithoutCancel(ctx), row, "start")
		}
	case "sleep":
		p.emit(row.ProjectID, row.ID, "sleep", "info", fmt.Sprintf("%s schläft ein, weil niemand online war.", row.Name))
		if row.Role == "proxy" {
			go p.followProxy(context.WithoutCancel(ctx), row, "stop")
		}
	case "match":
		trigger, ok := triggers[event.Pattern]
		if !ok || !trigger.Enabled || trigger.ProjectID != row.ProjectID || !p.auto.cooled(trigger.ID+"|"+row.ID, now) {
			return
		}
		// Die Zeile kann Chat von Spielern enthalten: „@everyone“ darf in Discord-Webhooks niemanden anpingen
		line := strings.ReplaceAll(event.Line, "@", "@​")
		_ = p.store.RecordTriggerMatch(trigger.ID, now.UnixMilli(), line)
		p.emit(row.ProjectID, row.ID, "console_match", trigger.Level, fmt.Sprintf("Konsolen-Auslöser „%s“ auf %s: %s", trigger.Name, row.Name, line))
		if trigger.ScheduleID == "" {
			return
		}
		schedule, err := p.store.ScheduleByID(trigger.ScheduleID)
		if err != nil || schedule.ProjectID != trigger.ProjectID || !p.beginRun(schedule.ID) {
			return
		}
		go func() {
			defer p.endRun(schedule.ID)
			p.runSchedule(context.WithoutCancel(ctx), schedule)
		}()
	}
}

// followProxy startet oder stoppt die Backend-Server eines Netzwerks, wenn sein Proxy aufwacht oder einschläft.
// Instanzen von Gruppen bleiben dem Autoscaler überlassen.
func (p *Panel) followProxy(ctx context.Context, proxy ServerRow, action string) {
	rows, err := p.store.ListServers(proxy.ProjectID)
	if err != nil {
		return
	}
	for _, row := range rows {
		if row.Role != "backend" || row.GroupID != "" {
			continue
		}
		views, err := p.nodeViews(ctx, row.NodeID)
		if err != nil {
			continue
		}
		view, ok := views[row.ID]
		if !ok || action == "start" && view.State != node.StateOffline || action == "stop" && view.State != node.StateRunning && view.State != node.StateStarting {
			continue
		}
		client, _, err := p.client(row.NodeID)
		if err != nil {
			continue
		}
		callCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
		err = client.Do(callCtx, http.MethodPost, "/api/servers/"+row.ID+"/power", map[string]string{"action": action}, nil)
		cancel()
		p.invalidate(row.NodeID)
		if err != nil {
			p.log.Warn("Backend-Server folgt dem Proxy nicht", "server", row.Name, "action", action, "error", err)
		}
	}
}

// crashHint fragt den Node nach der Absturzanalyse und liefert den Verdacht als Satz (leer, wenn es keinen gibt).
// handleCrash hängt ihn an die Meldung des Absturz-Ereignisses.
func (p *Panel) crashHint(ctx context.Context, row ServerRow) string {
	client, _, err := p.client(row.NodeID)
	if err != nil {
		return ""
	}
	callCtx, cancel := context.WithTimeout(ctx, 4*time.Second)
	defer cancel()
	var analysis node.CrashAnalysis
	if err := client.Do(callCtx, http.MethodGet, "/api/servers/"+row.ID+"/crash-analysis", nil, &analysis); err != nil || !analysis.Found || analysis.Summary == "" {
		return ""
	}
	return " " + analysis.Summary
}
