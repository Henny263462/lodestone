package panel

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/lodestone-mc/lodestone/internal/software"
)

type ReleaseInfo struct {
	Version     string `json:"version"`
	Title       string `json:"title"`
	HTMLURL     string `json:"htmlUrl"`
	PublishedAt string `json:"publishedAt"`
	Body        string `json:"body"`
}

type UpdateStatus struct {
	PanelVersion    string             `json:"panelVersion"`
	LatestVersion   string             `json:"latestVersion"`
	UpdateAvailable bool               `json:"updateAvailable"`
	ReleaseURL      string             `json:"releaseUrl,omitempty"`
	ReleaseTitle    string             `json:"releaseTitle,omitempty"`
	ReleaseNotes    string             `json:"releaseNotes,omitempty"`
	PublishedAt     string             `json:"publishedAt,omitempty"`
	AutoUpdate      bool               `json:"autoUpdate"`
	LastChecked     int64              `json:"lastChecked"`
	Nodes           []NodeUpdateStatus `json:"nodes"`
}

type NodeUpdateStatus struct {
	ID              string `json:"id"`
	Name            string `json:"name"`
	Version         string `json:"version"`
	UpdateAvailable bool   `json:"updateAvailable"`
	Online          bool   `json:"online"`
}

type releaseCache struct {
	at      time.Time
	release ReleaseInfo
	err     error
}

var (
	releaseMu    sync.Mutex
	releaseEntry *releaseCache
)

func cleanVersion(v string) string {
	v = strings.TrimSpace(v)
	v = strings.TrimPrefix(v, "v")
	v = strings.TrimPrefix(v, "V")
	return v
}

func isNewer(latest, current string) bool {
	latest = cleanVersion(latest)
	current = cleanVersion(current)
	if latest == "" || current == "" {
		return false
	}
	if current == "dev" {
		return false
	}
	return software.CompareVersions(latest, current) > 0
}

func (p *Panel) checkRelease(ctx context.Context, force bool) (ReleaseInfo, error) {
	releaseMu.Lock()
	defer releaseMu.Unlock()
	if !force && releaseEntry != nil && time.Since(releaseEntry.at) < 1*time.Hour {
		return releaseEntry.release, releaseEntry.err
	}

	url := os.Getenv("LODESTONE_UPDATE_URL")
	if url == "" {
		url = "https://api.github.com/repos/Henny263462/lodestone/releases/latest"
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return ReleaseInfo{}, err
	}
	req.Header.Set("User-Agent", "Lodestone-Panel")
	req.Header.Set("Accept", "application/vnd.github.v3+json")

	client := &http.Client{Timeout: 6 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		if releaseEntry != nil {
			return releaseEntry.release, nil
		}
		return ReleaseInfo{}, err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		info := ReleaseInfo{Version: cleanVersion(p.cfg.Version)}
		releaseEntry = &releaseCache{at: time.Now(), release: info}
		return info, nil
	}
	if resp.StatusCode >= 300 {
		if releaseEntry != nil {
			return releaseEntry.release, nil
		}
		return ReleaseInfo{}, fmt.Errorf("GitHub antwortet mit %d", resp.StatusCode)
	}

	var gh struct {
		TagName     string `json:"tag_name"`
		Name        string `json:"name"`
		HTMLURL     string `json:"html_url"`
		PublishedAt string `json:"published_at"`
		Body        string `json:"body"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&gh); err != nil {
		return ReleaseInfo{}, err
	}

	info := ReleaseInfo{
		Version:     cleanVersion(gh.TagName),
		Title:       gh.Name,
		HTMLURL:     gh.HTMLURL,
		PublishedAt: gh.PublishedAt,
		Body:        gh.Body,
	}
	releaseEntry = &releaseCache{at: time.Now(), release: info}
	return info, nil
}

func (p *Panel) getUpdateStatus(ctx context.Context, force bool) (UpdateStatus, error) {
	rel, err := p.checkRelease(ctx, force)
	var latestVersion string
	var releaseURL, releaseTitle, releaseNotes, publishedAt string
	if err == nil {
		latestVersion = rel.Version
		releaseURL = rel.HTMLURL
		releaseTitle = rel.Title
		releaseNotes = rel.Body
		publishedAt = rel.PublishedAt
	}

	autoUpdateVal, _ := p.store.Setting("auto_update", "false")
	autoUpdate := autoUpdateVal == "true"

	panelVersion := p.cfg.Version
	panelUpdate := isNewer(latestVersion, panelVersion)

	nodes, _ := p.store.ListNodes()
	nodeStatuses := make([]NodeUpdateStatus, 0, len(nodes))
	for _, n := range nodes {
		callCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
		var system struct {
			Version string `json:"version"`
		}
		online := p.nodeClient(n).Do(callCtx, http.MethodGet, "/api/system", nil, &system) == nil
		cancel()

		nodeVersion := system.Version
		nodeUpdate := online && isNewer(latestVersion, nodeVersion)
		nodeStatuses = append(nodeStatuses, NodeUpdateStatus{
			ID:              n.ID,
			Name:            n.Name,
			Version:         nodeVersion,
			UpdateAvailable: nodeUpdate,
			Online:          online,
		})
	}

	hasAnyUpdate := panelUpdate
	for _, nu := range nodeStatuses {
		if nu.UpdateAvailable {
			hasAnyUpdate = true
			break
		}
	}

	var lastChecked int64
	releaseMu.Lock()
	if releaseEntry != nil {
		lastChecked = releaseEntry.at.UnixMilli()
	}
	releaseMu.Unlock()

	return UpdateStatus{
		PanelVersion:    panelVersion,
		LatestVersion:   latestVersion,
		UpdateAvailable: hasAnyUpdate,
		ReleaseURL:      releaseURL,
		ReleaseTitle:    releaseTitle,
		ReleaseNotes:    releaseNotes,
		PublishedAt:     publishedAt,
		AutoUpdate:      autoUpdate,
		LastChecked:     lastChecked,
		Nodes:           nodeStatuses,
	}, nil
}

func (p *Panel) updatePanel(ctx context.Context) error {
	nodes, err := p.store.ListNodes()
	if err != nil {
		return err
	}
	if len(nodes) == 0 {
		return fail(http.StatusBadGateway, "kein Node eingerichtet, der das Panel aktualisieren könnte")
	}

	var targetNode *Node
	for _, n := range nodes {
		if strings.Contains(n.URL, "node:8480") || strings.Contains(n.URL, "127.0.0.1") || strings.Contains(n.URL, "localhost") {
			targetNode = &n
			break
		}
	}
	if targetNode == nil && len(nodes) > 0 {
		targetNode = &nodes[0]
	}

	var resp struct {
		OK      bool   `json:"ok"`
		Message string `json:"message"`
	}
	if err := p.nodeClient(*targetNode).Do(ctx, http.MethodPost, "/api/system/update-panel", nil, &resp); err != nil {
		return fail(http.StatusBadGateway, "der Node konnte das Panel nicht aktualisieren: "+err.Error())
	}
	return nil
}

func (p *Panel) performNodeUpdate(ctx context.Context, nodeID string) error {
	node, err := p.store.NodeByID(nodeID)
	if err != nil {
		return err
	}
	var resp struct {
		OK      bool   `json:"ok"`
		Message string `json:"message"`
	}
	if err := p.nodeClient(node).Do(ctx, http.MethodPost, "/api/system/update", nil, &resp); err != nil {
		return fail(http.StatusBadGateway, "der Node konnte nicht aktualisiert werden: "+err.Error())
	}
	return nil
}

func (p *Panel) updateAllNodes(ctx context.Context) error {
	nodes, err := p.store.ListNodes()
	if err != nil {
		return err
	}
	var errs []string
	for _, n := range nodes {
		var resp struct {
			OK bool `json:"ok"`
		}
		if err := p.nodeClient(n).Do(ctx, http.MethodPost, "/api/system/update", nil, &resp); err != nil {
			errs = append(errs, n.Name+": "+err.Error())
		}
	}
	if len(errs) > 0 && len(errs) == len(nodes) {
		return fail(http.StatusBadGateway, "Update aller Nodes fehlgeschlagen: "+strings.Join(errs, ", "))
	}
	return nil
}

func (p *Panel) updateLoop(ctx context.Context) {
	select {
	case <-ctx.Done():
		return
	case <-time.After(30 * time.Second):
		p.checkAndAutoUpdate(ctx)
	}

	ticker := time.NewTicker(6 * time.Hour)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			p.checkAndAutoUpdate(ctx)
		}
	}
}

func (p *Panel) checkAndAutoUpdate(ctx context.Context) {
	status, err := p.getUpdateStatus(ctx, false)
	if err != nil || !status.UpdateAvailable {
		return
	}

	lastNotified, _ := p.store.Setting("update_notified_version", "")
	if lastNotified != status.LatestVersion && status.LatestVersion != "" {
		_ = p.store.SetSetting("update_notified_version", status.LatestVersion)
		if projects, err := p.store.ListProjects("", true); err == nil {
			for _, prj := range projects {
				p.emit(prj.ID, "", "update_available", "info",
					fmt.Sprintf("Eine neue Version von Lodestone ist verfügbar: %s (installiert: %s).", status.LatestVersion, p.cfg.Version))
			}
		}
	}

	if status.AutoUpdate && status.LatestVersion != "" {
		p.log.Info("Automatisches Update wird ausgeführt", "neueVersion", status.LatestVersion)
		if err := p.updateAllNodes(ctx); err != nil {
			p.log.Warn("Automatisches Update der Nodes fehlgeschlagen", "error", err)
		}
		if err := p.updatePanel(ctx); err != nil {
			p.log.Warn("Automatisches Update des Panels fehlgeschlagen", "error", err)
		}
		if projects, err := p.store.ListProjects("", true); err == nil {
			for _, prj := range projects {
				p.emit(prj.ID, "", "update_installed", "info",
					fmt.Sprintf("Lodestone wurde automatisch auf Version %s aktualisiert.", status.LatestVersion))
			}
		}
	}
}

func (p *Panel) getUpdates(w http.ResponseWriter, r *http.Request) {
	if err := requireAdmin(r); err != nil {
		writeError(w, err)
		return
	}
	status, err := p.getUpdateStatus(r.Context(), false)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, status)
}

func (p *Panel) checkUpdatesNow(w http.ResponseWriter, r *http.Request) {
	if err := requireAdmin(r); err != nil {
		writeError(w, err)
		return
	}
	status, err := p.getUpdateStatus(r.Context(), true)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, status)
}

func (p *Panel) putUpdateSettings(w http.ResponseWriter, r *http.Request) {
	if err := requireAdmin(r); err != nil {
		writeError(w, err)
		return
	}
	var body struct {
		AutoUpdate bool `json:"autoUpdate"`
	}
	if err := decode(r, &body); err != nil {
		writeError(w, err)
		return
	}
	val := "false"
	if body.AutoUpdate {
		val = "true"
	}
	if err := p.store.SetSetting("auto_update", val); err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"autoUpdate": body.AutoUpdate})
}

func (p *Panel) triggerPanelUpdate(w http.ResponseWriter, r *http.Request) {
	if err := requireAdmin(r); err != nil {
		writeError(w, err)
		return
	}
	if err := p.updatePanel(r.Context()); err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "message": "Panel-Update wurde gestartet. Das Panel startet neu."})
}

func (p *Panel) triggerNodeUpdate(w http.ResponseWriter, r *http.Request) {
	if err := requireAdmin(r); err != nil {
		writeError(w, err)
		return
	}
	nodeID := r.PathValue("id")
	if err := p.performNodeUpdate(r.Context(), nodeID); err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "message": "Node-Update wurde gestartet."})
}

func (p *Panel) triggerAllNodesUpdate(w http.ResponseWriter, r *http.Request) {
	if err := requireAdmin(r); err != nil {
		writeError(w, err)
		return
	}
	if err := p.updateAllNodes(r.Context()); err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "message": "Update aller Nodes wurde gestartet."})
}
