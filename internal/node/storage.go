package node

import (
	"context"
	"fmt"
	"io/fs"
	"net/http"
	"os"
	"regexp"
	"strings"
	"sync"
	"time"
)

const (
	// diskMeasureEvery ist der Abstand, in dem der Daemon die Serverordner nachmisst. Das Durchlaufen großer Welten
	// kostet Zeit, deshalb geschieht es nie bei einer Anfrage, sondern im Hintergrund.
	diskMeasureEvery = 5 * time.Minute
	// maxDiskLimitMB ist das größte einstellbare Speicherlimit (16 TB).
	maxDiskLimitMB = 16 << 20
	// minDiskLimitMB ist das kleinste Limit; darunter passt kein Server mit Welt.
	minDiskLimitMB = 256
	// manualMeasurePause ist die Pause zwischen zwei Messungen auf Knopfdruck.
	manualMeasurePause    = 15 * time.Second
	defaultRemeasureDelay = 10 * time.Second
)

// serverOps ist der Zustand für Speicherplatz, Klonen und Umzug. Er hängt am Manager; der Nullwert ist nutzbar.
type serverOps struct {
	mu        sync.Mutex
	usage     map[string]DiskUsage
	measuring map[string]bool
	pending   map[string]bool
	// busy sind Server, die gerade umziehen; sie dürfen nicht starten.
	busy map[string]string
	// reserved sind IDs, die gerade durch Klonen oder Import entstehen.
	reserved map[string]bool
	// remeasureDelay ist die Wartezeit, bevor nach einem Upload nachgemessen wird (0 = Standard; in Tests kürzer).
	remeasureDelay time.Duration
}

func (o *serverOps) initLocked() {
	if o.usage == nil {
		o.usage, o.measuring, o.pending = map[string]DiskUsage{}, map[string]bool{}, map[string]bool{}
		o.busy, o.reserved = map[string]string{}, map[string]bool{}
	}
}

// DiskUsage ist die zuletzt gemessene Größe eines Servers.
type DiskUsage struct {
	// Bytes ist die Größe des Serverordners (Welten, Plugins, Configs); nur sie zählt für das Limit.
	Bytes int64 `json:"bytes"`
	Files int   `json:"files"`
	// Backups ist die Größe der Backups des Servers (liegen außerhalb des Serverordners).
	Backups    int64 `json:"backups"`
	MeasuredAt int64 `json:"measuredAt"`
}

// StorageInfo ist der Speicherplatz eines Servers, wie ihn die Schnittstelle zeigt.
type StorageInfo struct {
	DiskUsage
	// LimitMB ist das weiche Speicherlimit (0 = keins).
	LimitMB int `json:"limitMB"`
	// Blocked heißt: Das Limit ist erreicht, Start, Upload und Entpacken sind gesperrt.
	Blocked   bool  `json:"blocked"`
	Measuring bool  `json:"measuring"`
	Free      int64 `json:"free"`
	Total     int64 `json:"total"`
}

// ServerStorage ist ein Eintrag in der Speicherübersicht des Nodes.
type ServerStorage struct {
	ID string `json:"id"`
	DiskUsage
	LimitMB int `json:"limitMB"`
}

// NodeStorage ist der Speicherplatz der Maschine und aller Server darauf.
type NodeStorage struct {
	Free    int64           `json:"free"`
	Total   int64           `json:"total"`
	Servers []ServerStorage `json:"servers"`
}

// ---------------------------------------------------------------- Messen

// KeepMeasuring misst den Platz aller Server sofort und danach regelmäßig, bis ctx endet.
func (m *Manager) KeepMeasuring(ctx context.Context) {
	ticker := time.NewTicker(diskMeasureEvery)
	defer ticker.Stop()
	for {
		m.MeasureAll(ctx)
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

// MeasureAll misst alle Server nacheinander und vergisst Messungen gelöschter Server.
func (m *Manager) MeasureAll(ctx context.Context) {
	servers := m.List()
	known := make(map[string]bool, len(servers))
	for _, server := range servers {
		known[server.ID()] = true
		if ctx.Err() != nil {
			return
		}
		m.measure(ctx, server)
	}
	m.ops.mu.Lock()
	defer m.ops.mu.Unlock()
	for id := range m.ops.usage {
		if !known[id] {
			delete(m.ops.usage, id)
		}
	}
}

// measure misst einen Server. Läuft schon eine Messung, gilt deren Ergebnis.
func (m *Manager) measure(ctx context.Context, server *Server) DiskUsage {
	id := server.ID()
	m.ops.mu.Lock()
	m.ops.initLocked()
	if m.ops.measuring[id] {
		usage := m.ops.usage[id]
		m.ops.mu.Unlock()
		return usage
	}
	m.ops.measuring[id] = true
	m.ops.mu.Unlock()

	usage := DiskUsage{}
	usage.Bytes, usage.Files = treeSize(ctx, server.fs.root.FS())
	usage.Backups, _ = treeSize(ctx, os.DirFS(server.backupDir()))
	usage.MeasuredAt = time.Now().UnixMilli()

	m.ops.mu.Lock()
	defer m.ops.mu.Unlock()
	delete(m.ops.measuring, id)
	if ctx.Err() == nil {
		m.ops.usage[id] = usage
	}
	return m.ops.usage[id]
}

// treeSize zählt die Größe aller regulären Dateien. Symlinks werden nicht verfolgt, Unlesbares wird übersprungen.
func treeSize(ctx context.Context, root fs.FS) (int64, int) {
	var total int64
	files := 0
	_ = fs.WalkDir(root, ".", func(_ string, entry fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if files%1000 == 0 && ctx.Err() != nil {
			return ctx.Err()
		}
		if !entry.Type().IsRegular() {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return nil
		}
		total += info.Size()
		files++
		return nil
	})
	return total, files
}

// usageOf liefert die letzte Messung (MeasuredAt 0 = noch nicht gemessen).
func (m *Manager) usageOf(id string) (DiskUsage, bool) {
	m.ops.mu.Lock()
	defer m.ops.mu.Unlock()
	m.ops.initLocked()
	return m.ops.usage[id], m.ops.measuring[id]
}

// remeasureSoon misst einen Server kurz nach einer Änderung (Upload, Entpacken) neu; mehrere Änderungen in kurzer
// Zeit ergeben eine Messung.
func (m *Manager) remeasureSoon(server *Server) {
	id := server.ID()
	m.ops.mu.Lock()
	m.ops.initLocked()
	if m.ops.pending[id] {
		m.ops.mu.Unlock()
		return
	}
	m.ops.pending[id] = true
	delay := m.ops.remeasureDelay
	m.ops.mu.Unlock()
	if delay <= 0 {
		delay = defaultRemeasureDelay
	}
	go func() {
		time.Sleep(delay)
		m.ops.mu.Lock()
		delete(m.ops.pending, id)
		m.ops.mu.Unlock()
		if _, ok := m.Get(id); ok {
			m.measure(context.Background(), server)
		}
	}()
}

// ---------------------------------------------------------------- Limit

// diskLimitErr meldet, ob ein Server sein Speicherlimit erreicht hat. Ohne Limit oder Messung gilt es als frei.
func (m *Manager) diskLimitErr(id string, limitMB int) error {
	if limitMB <= 0 {
		return nil
	}
	usage, _ := m.usageOf(id)
	limit := int64(limitMB) << 20
	if usage.MeasuredAt == 0 || usage.Bytes < limit {
		return nil
	}
	return fmt.Errorf("%w: Das Speicherlimit des Servers ist erreicht (belegt %s von %s). Starten, Hochladen und Entpacken sind gesperrt, bis du Dateien löschst oder ein Administrator das Limit erhöht",
		ErrConflict, formatBytes(usage.Bytes), formatBytes(limit))
}

// startBlocked sagt, warum ein Server gerade nicht starten darf (nil = er darf). Der Aufrufer hält s.mu.
func (m *Manager) startBlocked(id string, limitMB int) error {
	m.ops.mu.Lock()
	m.ops.initLocked()
	reason := m.ops.busy[id]
	m.ops.mu.Unlock()
	if reason != "" {
		return fmt.Errorf("%w: %s", ErrBadState, reason)
	}
	return m.diskLimitErr(id, limitMB)
}

// SetDiskLimit setzt das weiche Speicherlimit (0 = keins). Es gilt sofort, auch für einen laufenden Server: Er läuft
// weiter, aber Start, Upload und Entpacken sind gesperrt, sobald das Limit erreicht ist.
func (s *Server) SetDiskLimit(limitMB int) error {
	if limitMB != 0 && (limitMB < minDiskLimitMB || limitMB > maxDiskLimitMB) {
		return badRequest("das Speicherlimit muss 0 (keins) oder zwischen %d MB und %d MB liegen", minDiskLimitMB, maxDiskLimitMB)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.spec.DiskLimitMB = limitMB
	return s.saveLocked()
}

func (m *Manager) storageOf(server *Server) StorageInfo {
	usage, measuring := m.usageOf(server.ID())
	limit := server.snapshot().DiskLimitMB
	free, total := diskSpace(m.cfg.DataDir)
	return StorageInfo{DiskUsage: usage, LimitMB: limit, Blocked: m.diskLimitErr(server.ID(), limit) != nil, Measuring: measuring, Free: free, Total: total}
}

// formatBytes schreibt eine Größe deutsch (Dezimalkomma).
func formatBytes(n int64) string {
	switch {
	case n >= 1<<30:
		return strings.Replace(fmt.Sprintf("%.1f GB", float64(n)/(1<<30)), ".", ",", 1)
	case n >= 1<<20:
		return fmt.Sprintf("%d MB", n>>20)
	default:
		return fmt.Sprintf("%d KB", n>>10)
	}
}

// ---------------------------------------------------------------- Schnittstelle

// guardedPath sind die Aktionen, die einen Server wachsen lassen und beim erreichten Limit gesperrt sind.
var guardedPath = regexp.MustCompile(`^/api/servers/([a-z0-9][a-z0-9-]{0,62})/(files/upload|files/extract|files/pull|modpack)$`)

// storageGuard sperrt Uploads, Entpacken, Downloads per Link und Modpacks, wenn das Limit erreicht ist oder der
// Server gerade umzieht, und misst nach einer erfolgreichen Änderung nach.
func (a *API) storageGuard(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		match := guardedPath.FindStringSubmatch(r.URL.Path)
		if r.Method != http.MethodPost || match == nil {
			next.ServeHTTP(w, r)
			return
		}
		server, ok := a.manager.Get(match[1])
		if !ok {
			next.ServeHTTP(w, r)
			return
		}
		if err := a.manager.startBlocked(server.ID(), server.snapshot().DiskLimitMB); err != nil {
			writeError(w, err)
			return
		}
		next.ServeHTTP(w, r)
		a.manager.remeasureSoon(server)
	})
}

func (a *API) nodeStorage(w http.ResponseWriter, _ *http.Request) {
	free, total := diskSpace(a.cfg.DataDir)
	result := NodeStorage{Free: free, Total: total, Servers: []ServerStorage{}}
	for _, server := range a.manager.List() {
		usage, _ := a.manager.usageOf(server.ID())
		result.Servers = append(result.Servers, ServerStorage{ID: server.ID(), DiskUsage: usage, LimitMB: server.snapshot().DiskLimitMB})
	}
	writeJSON(w, http.StatusOK, result)
}

func (a *API) serverStorage(w http.ResponseWriter, _ *http.Request, server *Server) {
	writeJSON(w, http.StatusOK, a.manager.storageOf(server))
}

func (a *API) putStorageLimit(w http.ResponseWriter, r *http.Request, server *Server) {
	var body struct {
		LimitMB int `json:"limitMB"`
	}
	if err := decode(r, &body); err != nil {
		writeError(w, err)
		return
	}
	if err := server.SetDiskLimit(body.LimitMB); err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, a.manager.storageOf(server))
}

// measureStorage misst auf Wunsch neu (im Hintergrund, höchstens alle paar Sekunden).
func (a *API) measureStorage(w http.ResponseWriter, _ *http.Request, server *Server) {
	info := a.manager.storageOf(server)
	if !info.Measuring && time.Since(time.UnixMilli(info.MeasuredAt)) > manualMeasurePause {
		go a.manager.measure(context.Background(), server)
		info.Measuring = true
	}
	writeJSON(w, http.StatusAccepted, info)
}
