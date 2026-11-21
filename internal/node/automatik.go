package node

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"time"
)

// Automatik des Nodes: Schlafmodus, Beobachtung der Konsole (Spieler betritt/verlässt, eigene Muster) und
// Absturzanalyse. Das Panel holt die erkannten Ereignisse regelmäßig ab (POST /api/console-watch) und schickt dabei die
// Muster mit, die es beobachten soll.

const (
	automatikTick     = 2 * time.Second
	idleCheckEvery    = 30 * time.Second
	consoleEventLimit = 1000
	// maxPatternMatches begrenzt, wie oft ein Muster pro Minute und Server auslösen darf.
	maxPatternMatches = 5
	maxWatchPatterns  = 50
	maxPatternLength  = 200
)

var watchID = regexp.MustCompile(`^[a-z0-9]{1,32}$`)

// Erkennung von Beitritt und Verlassen. Vor dem Namen dürfen nur die Klammern des Log-Formats stehen
// („[12:00:00] [Server thread/INFO]: “, „[12:00:00 INFO]: “, bei Forge zusätzlich „[minecraft/MinecraftServer]“),
// damit ein Chat-Text wie „<Bob> Alex joined the game“ nicht als Beitritt zählt.
var (
	joinLeave     = regexp.MustCompile(`^(?:\[[^\]]*\]\s*){1,3}:\s*(\.?[A-Za-z0-9_]{1,16}) (joined|left) the game$`)
	proxyJoinLeft = regexp.MustCompile(`^(?:\[[^\]]*\]\s*){1,3}:\s*\[connected player\] (\.?[A-Za-z0-9_]{1,16}) \([^)]*\) has (connected|disconnected)$`)
)

// ConsoleEvent ist etwas, das der Node in der Konsole oder beim Schlafmodus erkannt hat.
type ConsoleEvent struct {
	Seq      int64  `json:"seq"`
	Time     int64  `json:"time"`
	ServerID string `json:"serverId"`
	// Kind: join, leave, match (eigenes Muster), wake (Schlafmodus: Beitritt startet), sleep (Leerlauf: gestoppt).
	Kind    string `json:"kind"`
	Player  string `json:"player,omitempty"`
	Pattern string `json:"pattern,omitempty"`
	Line    string `json:"line,omitempty"`
}

// WatchPattern ist ein Muster, auf das der Node in der Konsole eines Servers achten soll.
type WatchPattern struct {
	ID      string `json:"id"`
	Pattern string `json:"pattern"`
}

// WatchRequest holt Ereignisse ab und setzt die Muster je Server (die Liste ersetzt die vorige vollständig).
type WatchRequest struct {
	Boot     string                    `json:"boot"`
	After    int64                     `json:"after"`
	Patterns map[string][]WatchPattern `json:"patterns"`
}

// WatchResponse sind die Ereignisse nach After. Boot wechselt bei jedem Start des Daemons; passt er nicht zur Anfrage,
// kommen alle gemerkten Ereignisse.
type WatchResponse struct {
	Boot   string         `json:"boot"`
	Latest int64          `json:"latest"`
	Events []ConsoleEvent `json:"events"`
}

type compiledPattern struct {
	id string
	re *regexp.Regexp
}

type rateWindow struct {
	start time.Time
	count int
}

// autoState hängt am Manager. Der Nullwert ist benutzbar.
type autoState struct {
	sleepMu sync.Mutex
	sleep   map[string]*sleepEntry
	// players zählt die Spieler eines laufenden Servers (in Tests ersetzbar).
	players func(ctx context.Context, server *Server) (int, error)

	watchMu  sync.Mutex
	watched  map[string]func()
	patterns map[string][]compiledPattern
	limits   map[string]*rateWindow
	events   []ConsoleEvent
	seq      int64
	boot     string
}

// RunAutomatik hält Schlafmodus und Konsolen-Beobachtung am Laufen, bis ctx endet.
func (m *Manager) RunAutomatik(ctx context.Context) {
	ticker := time.NewTicker(automatikTick)
	defer ticker.Stop()
	defer m.auto.closeSleepers()
	var lastIdle time.Time
	for {
		m.syncWatchers()
		m.syncSleep()
		if now := time.Now(); now.Sub(lastIdle) >= idleCheckEvery {
			lastIdle = now
			m.checkIdle(ctx, now)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

// ---------------------------------------------------------------- Ereignisse

func (a *autoState) bootID() string {
	if a.boot == "" {
		buffer := make([]byte, 8)
		_, _ = rand.Read(buffer)
		a.boot = hex.EncodeToString(buffer)
	}
	return a.boot
}

func (a *autoState) record(event ConsoleEvent) {
	a.watchMu.Lock()
	defer a.watchMu.Unlock()
	a.bootID()
	a.seq++
	event.Seq = a.seq
	if event.Time == 0 {
		event.Time = time.Now().UnixMilli()
	}
	a.events = append(a.events, event)
	if len(a.events) > consoleEventLimit {
		a.events = append([]ConsoleEvent(nil), a.events[len(a.events)-consoleEventLimit:]...)
	}
}

// watch setzt die Muster und liefert die Ereignisse seit after.
func (a *autoState) watch(request WatchRequest) WatchResponse {
	compiled := map[string][]compiledPattern{}
	for serverID, list := range request.Patterns {
		for _, entry := range list {
			if len(compiled[serverID]) >= maxWatchPatterns || !watchID.MatchString(entry.ID) || entry.Pattern == "" || len(entry.Pattern) > maxPatternLength {
				continue
			}
			re, err := regexp.Compile(entry.Pattern)
			if err != nil {
				continue
			}
			compiled[serverID] = append(compiled[serverID], compiledPattern{id: entry.ID, re: re})
		}
	}
	a.watchMu.Lock()
	defer a.watchMu.Unlock()
	a.patterns = compiled
	boot := a.bootID()
	response := WatchResponse{Boot: boot, Latest: a.seq, Events: []ConsoleEvent{}}
	for _, event := range a.events {
		if request.Boot != boot || event.Seq > request.After {
			response.Events = append(response.Events, event)
		}
	}
	return response
}

// syncWatchers beobachtet die Konsole jedes Servers. Wird ein Beobachter abgehängt (zu langsam), meldet er sich beim
// nächsten Durchlauf neu an; gelöschte Server werden abgemeldet.
func (m *Manager) syncWatchers() {
	a := &m.auto
	servers := m.List()
	alive := map[string]bool{}
	a.watchMu.Lock()
	defer a.watchMu.Unlock()
	if a.watched == nil {
		a.watched = map[string]func(){}
	}
	for _, server := range servers {
		id := server.ID()
		alive[id] = true
		if _, ok := a.watched[id]; ok {
			continue
		}
		events, cancel, _, _ := server.Subscribe(0)
		a.watched[id] = cancel
		go func() {
			for event := range events {
				if event.Type == "line" && event.Line != nil {
					a.observe(id, event.Line.Text)
				}
			}
			a.watchMu.Lock()
			delete(a.watched, id)
			a.watchMu.Unlock()
		}()
	}
	for id, cancel := range a.watched {
		if !alive[id] {
			cancel()
		}
	}
}

// observe prüft eine Konsolenzeile auf Beitritt, Verlassen und eigene Muster.
func (a *autoState) observe(serverID, raw string) {
	if strings.HasPrefix(raw, "[lodestone] ") {
		return // eigene Meldungen des Daemons
	}
	text := strings.TrimSpace(ansi.ReplaceAllString(raw, ""))
	if match := joinLeave.FindStringSubmatch(text); match != nil {
		kind := "join"
		if match[2] == "left" {
			kind = "leave"
		}
		a.record(ConsoleEvent{ServerID: serverID, Kind: kind, Player: match[1]})
	} else if match := proxyJoinLeft.FindStringSubmatch(text); match != nil {
		kind := "join"
		if match[2] == "disconnected" {
			kind = "leave"
		}
		a.record(ConsoleEvent{ServerID: serverID, Kind: kind, Player: match[1]})
	}
	a.watchMu.Lock()
	patterns := a.patterns[serverID]
	a.watchMu.Unlock()
	for _, pattern := range patterns {
		if !pattern.re.MatchString(text) || !a.allowMatch(serverID+"|"+pattern.id, time.Now()) {
			continue
		}
		line := text
		if len(line) > 300 {
			line = line[:300]
		}
		a.record(ConsoleEvent{ServerID: serverID, Kind: "match", Pattern: pattern.id, Line: line})
	}
}

// allowMatch lässt je Server und Muster höchstens maxPatternMatches Treffer pro Minute durch.
func (a *autoState) allowMatch(key string, now time.Time) bool {
	a.watchMu.Lock()
	defer a.watchMu.Unlock()
	if a.limits == nil {
		a.limits = map[string]*rateWindow{}
	}
	window := a.limits[key]
	if window == nil || now.Sub(window.start) > time.Minute {
		if len(a.limits) > 5000 {
			a.limits = map[string]*rateWindow{}
		}
		window = &rateWindow{start: now}
		a.limits[key] = window
	}
	window.count++
	return window.count <= maxPatternMatches
}

// ---------------------------------------------------------------- Schnittstelle

// registerAutomatikRoutes hängt die Routen von Schlafmodus, Konsolen-Beobachtung und Absturzanalyse ein.
func (a *API) registerAutomatikRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/servers/{id}/sleep", a.server(func(w http.ResponseWriter, _ *http.Request, server *Server) {
		writeJSON(w, http.StatusOK, a.manager.SleepStatus(server))
	}))
	mux.HandleFunc("PUT /api/servers/{id}/sleep", a.server(func(w http.ResponseWriter, r *http.Request, server *Server) {
		var config SleepConfig
		if err := decode(r, &config); err != nil {
			writeError(w, err)
			return
		}
		status, err := a.manager.SetSleep(server, config)
		if err != nil {
			writeError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, status)
	}))
	mux.HandleFunc("GET /api/servers/{id}/crash-analysis", a.server(func(w http.ResponseWriter, _ *http.Request, server *Server) {
		writeJSON(w, http.StatusOK, server.CrashAnalysis())
	}))
	mux.HandleFunc("POST /api/console-watch", func(w http.ResponseWriter, r *http.Request) {
		var request WatchRequest
		if err := decode(r, &request); err != nil {
			writeError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, a.manager.auto.watch(request))
	})
}
