package node

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/lodestone-mc/lodestone/internal/software"
)

// Schlafmodus: Ist ein Server aus, hält der Node seinen Port mit einem kleinen Minecraft-Listener. Die Serverliste im
// Spiel zeigt dann „Schläft – zum Starten beitreten“, ein Beitrittsversuch startet den echten Server. Läuft der Server
// eine Weile ohne Spieler, stoppt der Node ihn wieder sauber.

// SleepConfig ist die Einstellung des Schlafmodus eines Servers.
type SleepConfig struct {
	Enabled bool `json:"enabled"`
	// IdleMinutes ist die Zeit ohne Spieler, nach der der Server gestoppt wird.
	IdleMinutes int `json:"idleMinutes"`
}

// SleepStatus ist der Stand des Schlafmodus eines Servers.
type SleepStatus struct {
	SleepConfig
	// Supported ist false, wenn dieser Node die Ports der Maschine nicht selbst belegen kann (Node im Container).
	Supported bool `json:"supported"`
	// Listening heißt: Der Server schläft, der Node wartet auf seinem Port auf Spieler.
	Listening bool   `json:"listening"`
	Port      int    `json:"port,omitempty"`
	IdleSince int64  `json:"idleSince,omitempty"`
	LastWake  int64  `json:"lastWake,omitempty"`
	LastSleep int64  `json:"lastSleep,omitempty"`
	Problem   string `json:"problem,omitempty"`
}

const (
	sleepFile          = "sleep.json"
	defaultIdleMinutes = 10
	maxIdleMinutes     = 1440
	sleepMOTD          = "Schläft – zum Starten beitreten"
	sleepConnLimit     = 16
	sleepPacketLimit   = 4096
	sleepConnTimeout   = 5 * time.Second
)

var loginName = regexp.MustCompile(`^\.?[A-Za-z0-9_]{1,16}$`)

// sleepEntry ist der Laufzeitstand eines Servers im Schlafmodus.
type sleepEntry struct {
	config    SleepConfig
	sleeper   *sleeper
	idleSince time.Time
	lastWake  time.Time
	lastSleep time.Time
	problem   string
}

// sleeper ist ein laufender Listener auf dem Port eines schlafenden Servers.
type sleeper struct {
	listener net.Listener
	port     int
	done     chan struct{}
}

func (s *sleeper) close() {
	_ = s.listener.Close()
	<-s.done
}

// sleepSupported sagt, ob dieser Node Ports der Maschine selbst belegen kann. Läuft er in einem Docker-Container mit
// eigenem Netz, landet ein Listener dort und ist von außen nicht erreichbar.
func (m *Manager) sleepSupported() bool {
	switch strings.ToLower(m.cfg.SleepMode) {
	case "on", "true", "1":
		return true
	case "off", "false", "0":
		return false
	}
	_, err := os.Stat("/.dockerenv")
	return err != nil
}

// sleepPort ist der Port der Maschine, auf dem Spieler den Server erreichen (0 = keiner).
func sleepPort(spec Spec) int {
	info, ok := software.Lookup(spec.Software)
	if !ok {
		return 0
	}
	for _, mapping := range spec.Ports {
		if mapping.Container == info.DefaultPort && mapping.Protocol == "tcp" && mapping.Host > 0 {
			return mapping.Host
		}
	}
	return 0
}

// entryLocked liefert den Stand eines Servers und liest beim ersten Mal sleep.json. Braucht auto.sleepMu.
func (a *autoState) entryLocked(s *Server) *sleepEntry {
	if a.sleep == nil {
		a.sleep = map[string]*sleepEntry{}
	}
	entry, ok := a.sleep[s.ID()]
	if ok {
		return entry
	}
	entry = &sleepEntry{config: SleepConfig{IdleMinutes: defaultIdleMinutes}}
	if data, err := os.ReadFile(filepath.Join(s.dir, sleepFile)); err == nil {
		var config SleepConfig
		if json.Unmarshal(data, &config) == nil {
			if config.IdleMinutes <= 0 || config.IdleMinutes > maxIdleMinutes {
				config.IdleMinutes = defaultIdleMinutes
			}
			entry.config = config
		}
	}
	a.sleep[s.ID()] = entry
	return entry
}

// SleepStatus liefert den Stand des Schlafmodus.
func (m *Manager) SleepStatus(s *Server) SleepStatus {
	a := &m.auto
	a.sleepMu.Lock()
	defer a.sleepMu.Unlock()
	entry := a.entryLocked(s)
	status := SleepStatus{SleepConfig: entry.config, Supported: m.sleepSupported(), Port: sleepPort(s.snapshot()), Problem: entry.problem}
	if entry.sleeper != nil {
		status.Listening = true
		status.Port = entry.sleeper.port
	}
	status.IdleSince = unixMilli(entry.idleSince)
	status.LastWake = unixMilli(entry.lastWake)
	status.LastSleep = unixMilli(entry.lastSleep)
	return status
}

func unixMilli(at time.Time) int64 {
	if at.IsZero() {
		return 0
	}
	return at.UnixMilli()
}

// SetSleep speichert die Einstellung und gleicht den Listener sofort ab.
func (m *Manager) SetSleep(s *Server, config SleepConfig) (SleepStatus, error) {
	if config.IdleMinutes == 0 {
		config.IdleMinutes = defaultIdleMinutes
	}
	if config.IdleMinutes < 1 || config.IdleMinutes > maxIdleMinutes {
		return SleepStatus{}, badRequest("die Leerlaufzeit muss zwischen 1 und %d Minuten liegen", maxIdleMinutes)
	}
	if config.Enabled && !m.sleepSupported() {
		return SleepStatus{}, fmt.Errorf("%w: Der Schlafmodus ist auf diesem Node abgeschaltet. Der Node läuft in einem Docker-Container mit eigenem Netz und kann die Ports der Maschine nicht selbst belegen. Starte ihn direkt auf der Maschine oder mit network_mode: host und setze LODESTONE_NODE_SLEEP_MODE=on", ErrConflict)
	}
	if config.Enabled && sleepPort(s.snapshot()) == 0 {
		return SleepStatus{}, badRequest("der Server hat keinen Port auf der Maschine (er ist nur im Docker-Netzwerk erreichbar). Der Schlafmodus braucht einen veröffentlichten Port")
	}
	data, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return SleepStatus{}, err
	}
	path := filepath.Join(s.dir, sleepFile)
	if err := os.WriteFile(path+".tmp", data, 0o600); err != nil {
		return SleepStatus{}, err
	}
	if err := os.Rename(path+".tmp", path); err != nil {
		return SleepStatus{}, err
	}
	a := &m.auto
	a.sleepMu.Lock()
	entry := a.entryLocked(s)
	entry.config = config
	if !config.Enabled {
		entry.idleSince = time.Time{}
	}
	a.sleepMu.Unlock()
	m.syncSleep()
	return m.SleepStatus(s), nil
}

// releaseSleep schließt den Listener eines Servers, damit der echte Server den Port bekommt. doLaunch ruft es vor dem
// Anlegen des Containers auf; der Zustand steht dann schon auf „starting“, syncSleep öffnet ihn also nicht wieder.
func (a *autoState) releaseSleep(id string) {
	a.sleepMu.Lock()
	defer a.sleepMu.Unlock()
	if entry := a.sleep[id]; entry != nil && entry.sleeper != nil {
		entry.sleeper.close()
		entry.sleeper = nil
	}
}

// sleepWanted sagt, ob der Server gerade schlafen soll, und auf welchem Port. Braucht auto.sleepMu.
func (m *Manager) sleepWanted(server *Server, entry *sleepEntry, supported bool) (bool, int) {
	view := server.View()
	port := sleepPort(view.Spec)
	return entry.config.Enabled && supported && view.State == StateOffline && view.Installed && port > 0 && !server.modpackBusy.Load(), port
}

// syncSleep öffnet die Listener aller schlafenden Server und schließt die, die nicht mehr gebraucht werden.
//
// Vor dem Öffnen fragt es Docker, ob der Container doch läuft: Direkt nach dem Start des Daemons stehen alle Server auf
// „offline“, bis Reconcile laufende Container übernommen hat. Ein Beitritt in diesem Fenster würde sonst einen Start
// auslösen, der den laufenden Container ersetzt. Die Docker-Abfrage läuft ohne Sperre; danach werden Zustand und Port
// unter derselben Sperre wie in releaseSleep noch einmal geprüft, damit nie ein Listener den Port eines startenden
// Servers hält.
func (m *Manager) syncSleep() {
	a := &m.auto
	supported := m.sleepSupported()
	servers := m.List()
	var candidates []*Server

	a.sleepMu.Lock()
	alive := map[string]bool{}
	for _, server := range servers {
		alive[server.ID()] = true
		entry := a.entryLocked(server)
		want, port := m.sleepWanted(server, entry, supported)
		if entry.sleeper != nil && (!want || entry.sleeper.port != port) {
			entry.sleeper.close()
			entry.sleeper = nil
		}
		if !want && !entry.config.Enabled {
			entry.problem = ""
		}
		if want && entry.sleeper == nil {
			candidates = append(candidates, server)
		}
	}
	for id, entry := range a.sleep {
		if !alive[id] {
			if entry.sleeper != nil {
				entry.sleeper.close()
			}
			delete(a.sleep, id)
		}
	}
	a.sleepMu.Unlock()

	for _, server := range candidates {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		state, err := m.rt.Inspect(ctx, "lodestone-"+server.ID())
		cancel()
		if err != nil || state.Running {
			continue // Docker nicht erreichbar oder der Container läuft noch: lieber nicht schlafen
		}
		m.armSleep(server, supported)
	}
}

// armSleep öffnet den Listener eines Servers, wenn er nach erneuter Prüfung noch schlafen soll.
func (m *Manager) armSleep(server *Server, supported bool) {
	a := &m.auto
	a.sleepMu.Lock()
	defer a.sleepMu.Unlock()
	if _, ok := m.Get(server.ID()); !ok {
		return // inzwischen gelöscht
	}
	entry := a.entryLocked(server)
	want, port := m.sleepWanted(server, entry, supported)
	if !want || entry.sleeper != nil {
		return
	}
	listener, err := net.Listen("tcp", fmt.Sprintf(":%d", port))
	if err != nil {
		problem := fmt.Sprintf("Der Port %d ist gerade belegt, der Schlafmodus wartet darauf, dass er frei wird.", port)
		if entry.problem != problem {
			server.logf("Schlafmodus: Port %d ist belegt (%v)", port, err)
		}
		entry.problem = problem
		return
	}
	entry.problem = ""
	entry.sleeper = &sleeper{listener: listener, port: port, done: make(chan struct{})}
	go m.serveSleep(server, entry.sleeper)
	server.logf("Schlafmodus: Der Server schläft. Port %d wartet auf Spieler, ein Beitritt startet ihn", port)
}

// closeSleepers schließt alle Listener (beim Beenden des Daemons).
func (a *autoState) closeSleepers() {
	a.sleepMu.Lock()
	defer a.sleepMu.Unlock()
	for _, entry := range a.sleep {
		if entry.sleeper != nil {
			entry.sleeper.close()
			entry.sleeper = nil
		}
	}
}

func (m *Manager) serveSleep(server *Server, sl *sleeper) {
	defer close(sl.done)
	slots := make(chan struct{}, sleepConnLimit)
	for {
		conn, err := sl.listener.Accept()
		if err != nil {
			if errors.Is(err, net.ErrClosed) {
				return
			}
			time.Sleep(50 * time.Millisecond)
			continue
		}
		select {
		case slots <- struct{}{}:
			go func() {
				defer func() { <-slots }()
				m.sleepConn(server, conn)
			}()
		default:
			conn.Close() // zu viele gleichzeitige Verbindungen
		}
	}
}

// ---------------------------------------------------------------- Minecraft-Protokoll (nur Handshake, Status, Login)

type handshake struct {
	protocol int
	address  string
	next     int
}

func readFrame(r *bufio.Reader) (int, *byteReader, error) {
	length, err := readVarInt(r)
	if err != nil {
		return 0, nil, err
	}
	if length <= 0 || length > sleepPacketLimit {
		return 0, nil, errors.New("ungültige Paketlänge")
	}
	frame := make([]byte, length)
	if _, err := io.ReadFull(r, frame); err != nil {
		return 0, nil, err
	}
	body := &byteReader{data: frame}
	id, err := readVarInt(body)
	if err != nil {
		return 0, nil, err
	}
	return id, body, nil
}

func readString(r *byteReader, limit int) (string, error) {
	size, err := readVarInt(r)
	if err != nil {
		return "", err
	}
	if size < 0 || size > limit || size > len(r.data)-r.pos {
		return "", errors.New("ungültige Zeichenkette")
	}
	text := string(r.data[r.pos : r.pos+size])
	r.pos += size
	return text, nil
}

func parseHandshake(body *byteReader) (handshake, error) {
	var result handshake
	var err error
	if result.protocol, err = readVarInt(body); err != nil {
		return result, err
	}
	if result.address, err = readString(body, 1024); err != nil {
		return result, err
	}
	if len(body.data)-body.pos < 2 {
		return result, errors.New("Handshake zu kurz")
	}
	body.pos += 2 // Port
	if result.next, err = readVarInt(body); err != nil {
		return result, err
	}
	return result, nil
}

func writeString(w *bytesWriter, text string) {
	writeVarInt(w, len(text))
	w.data = append(w.data, text...)
}

// sleepStatusJSON ist die Antwort auf die Statusabfrage der Serverliste.
func sleepStatusJSON(protocol int) string {
	data, _ := json.Marshal(map[string]any{
		"version":     map[string]any{"name": "Lodestone", "protocol": protocol},
		"players":     map[string]any{"max": 0, "online": 0},
		"description": map[string]any{"text": sleepMOTD},
	})
	return string(data)
}

func (m *Manager) sleepConn(server *Server, conn net.Conn) {
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(sleepConnTimeout))
	reader := bufio.NewReaderSize(conn, 1024)
	if first, err := reader.Peek(1); err != nil || first[0] == 0xFE {
		return // alte Statusabfrage (vor 1.7) wird nicht beantwortet
	}
	id, body, err := readFrame(reader)
	if err != nil || id != 0x00 {
		return
	}
	hello, err := parseHandshake(body)
	if err != nil {
		return
	}
	switch hello.next {
	case 1: // Status
		if id, _, err := readFrame(reader); err != nil || id != 0x00 {
			return
		}
		response := &bytesWriter{}
		writeString(response, sleepStatusJSON(hello.protocol))
		if _, err := conn.Write(packet(0x00, response.data)); err != nil {
			return
		}
		id, body, err := readFrame(reader)
		if err != nil || id != 0x01 || len(body.data)-body.pos != 8 {
			return
		}
		_, _ = conn.Write(packet(0x01, body.data[body.pos:]))
	case 2, 3: // Login oder Transfer
		id, body, err := readFrame(reader)
		if err != nil || id != 0x00 {
			return
		}
		name, err := readString(body, 64)
		if err != nil || !loginName.MatchString(name) {
			name = "Ein Spieler"
		}
		message := m.wake(server, name)
		text, _ := json.Marshal(map[string]string{"text": message})
		response := &bytesWriter{}
		writeString(response, string(text))
		_, _ = conn.Write(packet(0x00, response.data))
	}
}

// wake startet einen schlafenden Server, weil jemand beitreten möchte, und liefert die Meldung für den Spieler.
func (m *Manager) wake(server *Server, player string) string {
	err := server.Start()
	switch {
	case err == nil:
		a := &m.auto
		a.sleepMu.Lock()
		entry := a.entryLocked(server)
		entry.lastWake = time.Now()
		entry.idleSince = time.Time{}
		a.sleepMu.Unlock()
		server.logf("Schlafmodus: %s möchte beitreten, der Server wird gestartet", player)
		a.record(ConsoleEvent{ServerID: server.ID(), Kind: "wake", Player: player})
		return "Der Server wacht gerade auf. Versuch es in etwa einer Minute noch einmal."
	case errors.Is(err, ErrBadState):
		return "Der Server startet schon. Versuch es gleich noch einmal."
	default:
		server.logf("Schlafmodus: Der Start für %s ist fehlgeschlagen: %v", player, err)
		return "Der Server kann gerade nicht starten. Sag dem Betreiber Bescheid."
	}
}

// ---------------------------------------------------------------- Leerlauf

// checkIdle stoppt Server im Schlafmodus, auf denen seit IdleMinutes niemand mehr online ist. now kommt von außen,
// damit Tests mit festen Zeiten arbeiten können.
func (m *Manager) checkIdle(ctx context.Context, now time.Time) {
	a := &m.auto
	type candidate struct {
		server *Server
		idle   time.Duration
	}
	var candidates []candidate
	a.sleepMu.Lock()
	for _, server := range m.List() {
		entry := a.entryLocked(server)
		if !entry.config.Enabled || server.View().State != StateRunning {
			entry.idleSince = time.Time{}
			continue
		}
		candidates = append(candidates, candidate{server, time.Duration(entry.config.IdleMinutes) * time.Minute})
	}
	a.sleepMu.Unlock()

	for _, c := range candidates {
		online, err := a.countPlayers(ctx, c.server)
		if err != nil {
			continue // keine Antwort zählt weder als leer noch als besetzt
		}
		a.sleepMu.Lock()
		entry := a.entryLocked(c.server)
		stop := false
		switch {
		case online > 0:
			entry.idleSince = time.Time{}
		case entry.idleSince.IsZero():
			entry.idleSince = now
		case now.Sub(entry.idleSince) >= c.idle:
			stop = true
			entry.idleSince = time.Time{}
			entry.lastSleep = now
		}
		a.sleepMu.Unlock()
		if !stop {
			continue
		}
		c.server.logf("Schlafmodus: Seit %d Minuten ist niemand online, der Server schläft ein", int(c.idle/time.Minute))
		if err := c.server.Stop(); err != nil {
			c.server.logf("Schlafmodus: Stoppen fehlgeschlagen: %v", err)
			continue
		}
		a.record(ConsoleEvent{ServerID: c.server.ID(), Kind: "sleep"})
	}
}

func (a *autoState) countPlayers(ctx context.Context, server *Server) (int, error) {
	if a.players != nil {
		return a.players(ctx, server)
	}
	callCtx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	info, err := server.Players(callCtx)
	return info.Online, err
}
