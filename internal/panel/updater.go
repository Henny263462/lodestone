package panel

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/lodestone-mc/lodestone/internal/software"
)

// ReleaseInfo beschreibt das neueste veröffentlichte Release auf GitHub.
type ReleaseInfo struct {
	Version     string `json:"version"`
	Title       string `json:"title"`
	HTMLURL     string `json:"htmlUrl"`
	PublishedAt string `json:"publishedAt"`
	Body        string `json:"body"`
}

// UpdateStatus ist der Versionsstand von Panel und Nodes gegenüber dem neuesten Release.
type UpdateStatus struct {
	PanelVersion  string `json:"panelVersion"`
	LatestVersion string `json:"latestVersion"`
	// PanelUpdateAvailable gilt nur für das Panel; UpdateAvailable ist wahr, wenn Panel oder irgendein Node veraltet ist.
	PanelUpdateAvailable bool `json:"panelUpdateAvailable"`
	UpdateAvailable      bool `json:"updateAvailable"`
	// Channel ist der eingestellte Update-Kanal: "stable" (Releases) oder "dev" (neuester gebauter Stand von main).
	Channel string `json:"channel"`
	// CheckFailed ist wahr, wenn die neueste Version nicht ermittelt werden konnte; „kein Update“ heißt dann nur „unbekannt“.
	CheckFailed  bool               `json:"checkFailed"`
	ReleaseURL   string             `json:"releaseUrl,omitempty"`
	ReleaseTitle string             `json:"releaseTitle,omitempty"`
	ReleaseNotes string             `json:"releaseNotes,omitempty"`
	PublishedAt  string             `json:"publishedAt,omitempty"`
	AutoUpdate   bool               `json:"autoUpdate"`
	LastChecked  int64              `json:"lastChecked"`
	Nodes        []NodeUpdateStatus `json:"nodes"`
}

// NodeUpdateStatus ist der Versionsstand eines einzelnen Nodes.
type NodeUpdateStatus struct {
	ID              string `json:"id"`
	Name            string `json:"name"`
	Version         string `json:"version"`
	UpdateAvailable bool   `json:"updateAvailable"`
	Online          bool   `json:"online"`
}

// releaseCache merkt sich das zuletzt gefundene Release (eine Stunde lang).
type releaseCache struct {
	channel string    // der Kanal, für den die Abfrage gilt; nach einem Wechsel ist der Eintrag wertlos
	at      time.Time // letzte erfolgreiche Abfrage
	tried   time.Time // letzter Versuch, auch ein gescheiterter
	release ReleaseInfo
}

const (
	releaseCacheTTL = time.Hour
	// releaseRetryAfter ist die Pause nach einer gescheiterten Abfrage, damit ein hängendes GitHub nicht jede Anfrage aufhält.
	releaseRetryAfter = 5 * time.Minute
)

// cleanVersion entfernt Leerraum und das „v“ vor einer Versionsnummer („v0.3.0“ wird „0.3.0“).
func cleanVersion(v string) string {
	v = strings.TrimSpace(v)
	v = strings.TrimPrefix(v, "v")
	v = strings.TrimPrefix(v, "V")
	return v
}

// isNewer meldet, ob latest neuer als current ist. Entwicklungsstände („dev“) und unbekannte Versionen gelten nie als veraltet.
func isNewer(latest, current string) bool {
	latest = cleanVersion(latest)
	current = cleanVersion(current)
	if latest == "" || current == "" || current == "dev" {
		return false
	}
	return software.CompareVersions(latest, current) > 0
}

const (
	channelStable = "stable"
	channelDev    = "dev"
)

// isDevBuild meldet, ob version ein Entwicklungsstand ist: "dev" (selbst gebaut) oder "dev-<Commit>" (Build von main).
func isDevBuild(version string) bool {
	return version == "dev" || strings.HasPrefix(version, "dev-")
}

// needsUpdate sagt, ob current auf target gehoben werden soll. Im Dev-Kanal zählt jeder andere Stand, im stabilen Kanal ein
// neueres Release oder das Zurückwechseln von einem Dev-Build. Ein selbst gebautes "dev" gilt nur dann als wechselbar, wenn
// der Kanal ausdrücklich gewählt wurde; sonst bleibt es unberührt (so war es vor den Kanälen).
func needsUpdate(channel string, explicit bool, target, current string) bool {
	target, current = cleanVersion(target), cleanVersion(current)
	if target == "" || current == "" {
		return false
	}
	if current == "dev" && !explicit {
		return false
	}
	if channel == channelDev {
		return current != target
	}
	if isDevBuild(current) {
		return true
	}
	return isNewer(target, current)
}

// updateChannel liefert den eingestellten Kanal und ob er ausdrücklich gewählt wurde. Ohne Wahl folgt er der laufenden
// Version: Wer einen Dev-Build betreibt, bleibt im Dev-Kanal.
func (p *Panel) updateChannel() (channel string, explicit bool) {
	switch value, _ := p.store.Setting("update_channel", ""); value {
	case channelStable, channelDev:
		return value, true
	}
	if isDevBuild(p.cfg.Version) {
		return channelDev, false
	}
	return channelStable, false
}

// needsUpdate sagt, ob current auf target gehoben werden soll, gemessen am eingestellten Kanal.
func (p *Panel) needsUpdate(target, current string) bool {
	channel, explicit := p.updateChannel()
	return needsUpdate(channel, explicit, target, current)
}

// checkRelease liefert die neueste Version des eingestellten Kanals. Der Netzaufruf läuft ohne Sperre, damit er andere
// Anfragen nicht aufhält; scheitert er, gilt der letzte bekannte Stand desselben Kanals.
func (p *Panel) checkRelease(ctx context.Context, force bool) (ReleaseInfo, error) {
	channel, _ := p.updateChannel()
	p.releaseMu.Lock()
	cached := p.releaseEntry
	p.releaseMu.Unlock()
	if cached != nil && cached.channel != channel {
		cached = nil
	}
	if !force && cached != nil && (time.Since(cached.at) < releaseCacheTTL || time.Since(cached.tried) < releaseRetryAfter) {
		return cached.release, nil
	}
	info, err := p.fetchRelease(ctx, channel)
	if err != nil {
		if cached != nil {
			p.releaseMu.Lock()
			p.releaseEntry = &releaseCache{channel: channel, at: cached.at, tried: time.Now(), release: cached.release}
			p.releaseMu.Unlock()
			return cached.release, nil
		}
		return ReleaseInfo{}, err
	}
	now := time.Now()
	p.releaseMu.Lock()
	p.releaseEntry = &releaseCache{channel: channel, at: now, tried: now, release: info}
	p.releaseMu.Unlock()
	return info, nil
}

// githubGet fragt die GitHub-Schnittstelle ab; der Aufrufer schließt die Antwort.
func githubGet(ctx context.Context, endpoint string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "Lodestone-Panel")
	req.Header.Set("Accept", "application/vnd.github.v3+json")
	client := &http.Client{Timeout: 6 * time.Second}
	return client.Do(req)
}

// fetchRelease fragt GitHub nach der neuesten Version des Kanals.
func (p *Panel) fetchRelease(ctx context.Context, channel string) (ReleaseInfo, error) {
	if channel == channelDev {
		return p.fetchDevBuild(ctx)
	}
	return p.fetchStableRelease(ctx)
}

// devBuildEndpoint fragt den letzten erfolgreichen Lauf des Workflows „Docker Autobuild & Release“ auf main ab. Erfolgreich
// heißt: Panel- und Node-Image mit dem Tag dev-<Commit> liegen in der Registry.
const devBuildEndpoint = "https://api.github.com/repos/Henny263462/lodestone/actions/workflows/release.yml/runs?branch=main&event=push&status=success&per_page=1"

// fetchDevBuild liefert den neuesten gebauten Stand von main (Adresse überschreibbar mit LODESTONE_DEV_UPDATE_URL).
func (p *Panel) fetchDevBuild(ctx context.Context) (ReleaseInfo, error) {
	endpoint := os.Getenv("LODESTONE_DEV_UPDATE_URL")
	if endpoint == "" {
		endpoint = devBuildEndpoint
	}
	resp, err := githubGet(ctx, endpoint)
	if err != nil {
		return ReleaseInfo{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return ReleaseInfo{}, fmt.Errorf("GitHub antwortet mit %d", resp.StatusCode)
	}
	var runs struct {
		WorkflowRuns []struct {
			HeadSHA    string `json:"head_sha"`
			UpdatedAt  string `json:"updated_at"`
			HeadCommit struct {
				Message string `json:"message"`
			} `json:"head_commit"`
		} `json:"workflow_runs"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&runs); err != nil {
		return ReleaseInfo{}, err
	}
	if len(runs.WorkflowRuns) == 0 {
		return ReleaseInfo{}, nil // noch kein gebauter Stand: es gibt nichts, wohin man wechseln könnte
	}
	run := runs.WorkflowRuns[0]
	if !isCommitHash(run.HeadSHA) {
		return ReleaseInfo{}, fmt.Errorf("GitHub nennt keinen gültigen Commit: %q", run.HeadSHA)
	}
	message := strings.TrimSpace(run.HeadCommit.Message)
	title, _, _ := strings.Cut(message, "\n")
	return ReleaseInfo{
		Version:     "dev-" + run.HeadSHA[:7],
		Title:       title,
		HTMLURL:     "https://github.com/Henny263462/lodestone/commit/" + run.HeadSHA,
		PublishedAt: run.UpdatedAt,
		Body:        message,
	}, nil
}

// isCommitHash prüft, ob s ein vollständiger Git-Hash aus Kleinbuchstaben und Ziffern ist; er landet im Tag eines Images.
func isCommitHash(s string) bool {
	if len(s) != 40 {
		return false
	}
	for _, c := range s {
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return false
		}
	}
	return true
}

// fetchStableRelease fragt GitHub nach dem neuesten Release (Adresse überschreibbar mit LODESTONE_UPDATE_URL).
func (p *Panel) fetchStableRelease(ctx context.Context) (ReleaseInfo, error) {
	endpoint := os.Getenv("LODESTONE_UPDATE_URL")
	if endpoint == "" {
		endpoint = "https://api.github.com/repos/Henny263462/lodestone/releases/latest"
	}
	resp, err := githubGet(ctx, endpoint)
	if err != nil {
		return ReleaseInfo{}, err
	}
	defer resp.Body.Close()

	// Noch kein Release veröffentlicht: Es gibt nichts, wohin man aktualisieren könnte.
	if resp.StatusCode == http.StatusNotFound {
		return ReleaseInfo{Version: cleanVersion(p.cfg.Version)}, nil
	}
	if resp.StatusCode >= 300 {
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
	return ReleaseInfo{
		Version:     cleanVersion(gh.TagName),
		Title:       gh.Name,
		HTMLURL:     gh.HTMLURL,
		PublishedAt: gh.PublishedAt,
		Body:        gh.Body,
	}, nil
}

// latestVersion ist die zuletzt bekannte neueste Version (leer, wenn noch nie geprüft wurde).
func (p *Panel) latestVersion() string {
	channel, _ := p.updateChannel()
	p.releaseMu.Lock()
	defer p.releaseMu.Unlock()
	if p.releaseEntry == nil || p.releaseEntry.channel != channel {
		return ""
	}
	return p.releaseEntry.release.Version
}

// nodeVersion fragt die Version eines Nodes ab; online ist falsch, wenn er nicht antwortet.
func (p *Panel) nodeVersion(ctx context.Context, n Node) (version string, online bool) {
	callCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	var system struct {
		Version string `json:"version"`
	}
	if err := p.nodeClient(n).Do(callCtx, http.MethodGet, "/api/system", nil, &system); err != nil {
		return "", false
	}
	return system.Version, true
}

// getUpdateStatus stellt den Versionsstand von Panel und Nodes zusammen; force fragt GitHub auch bei frischem Cache neu.
func (p *Panel) getUpdateStatus(ctx context.Context, force bool) UpdateStatus {
	status := UpdateStatus{PanelVersion: p.cfg.Version, Nodes: []NodeUpdateStatus{}}
	rel, err := p.checkRelease(ctx, force)
	if err != nil {
		status.CheckFailed = true
	} else {
		status.LatestVersion = rel.Version
		status.ReleaseURL = rel.HTMLURL
		status.ReleaseTitle = rel.Title
		status.ReleaseNotes = rel.Body
		status.PublishedAt = rel.PublishedAt
	}
	status.AutoUpdate = p.autoUpdateEnabled()
	channel, explicit := p.updateChannel()
	status.Channel = channel
	status.PanelUpdateAvailable = needsUpdate(channel, explicit, status.LatestVersion, p.cfg.Version)
	status.UpdateAvailable = status.PanelUpdateAvailable

	nodes, _ := p.store.ListNodes()
	for _, n := range nodes {
		version, online := p.nodeVersion(ctx, n)
		outdated := online && needsUpdate(channel, explicit, status.LatestVersion, version)
		status.Nodes = append(status.Nodes, NodeUpdateStatus{ID: n.ID, Name: n.Name, Version: version, UpdateAvailable: outdated, Online: online})
		if outdated {
			status.UpdateAvailable = true
		}
	}

	p.releaseMu.Lock()
	if p.releaseEntry != nil {
		status.LastChecked = p.releaseEntry.at.UnixMilli()
	}
	p.releaseMu.Unlock()
	return status
}

// autoUpdateEnabled sagt, ob der Administrator das automatische Update eingeschaltet hat (Standard: aus).
func (p *Panel) autoUpdateEnabled() bool {
	value, _ := p.store.Setting("auto_update", "false")
	return value == "true"
}

// beginUpdate sperrt Updates gegen sich selbst: ein zweiter Klick oder die Hintergrundschleife darf nicht dazwischenfunken.
func (p *Panel) beginUpdate() (end func(), err error) {
	if !p.updateMu.TryLock() {
		return nil, fail(http.StatusConflict, "Es läuft schon ein Update. Warte, bis es fertig ist.")
	}
	return p.updateMu.Unlock, nil
}

// releaseToInstall liefert die Version, auf die aktualisiert werden soll, oder einen Fehler für den Benutzer.
func (p *Panel) releaseToInstall(ctx context.Context) (string, error) {
	release, err := p.checkRelease(ctx, false)
	if err != nil || release.Version == "" {
		return "", fail(http.StatusBadGateway, "Die neueste Version konnte nicht ermittelt werden. Prüfe, ob das Panel github.com erreicht.")
	}
	return release.Version, nil
}

// hostsPanel schätzt, ob ein Node auf derselben Maschine wie das Panel läuft (Compose-Dienst „node“ oder Loopback).
func hostsPanel(n Node) bool {
	u, err := url.Parse(n.URL)
	if err != nil {
		return false
	}
	switch u.Hostname() {
	case "node", "localhost", "127.0.0.1", "::1":
		return true
	}
	return false
}

// updatePanel lässt einen Node das Panel-Image laden und den Panel-Container ersetzen. Das Panel liegt in Docker auf
// einer Node-Maschine; welche, weiß nur der Node, darum werden die Nodes (zuerst die lokalen) der Reihe nach gefragt.
func (p *Panel) updatePanel(ctx context.Context, version string) error {
	nodes, err := p.store.ListNodes()
	if err != nil {
		return err
	}
	if len(nodes) == 0 {
		return fail(http.StatusBadGateway, "Es ist kein Node eingerichtet, der das Panel aktualisieren könnte.")
	}
	ordered := make([]Node, 0, len(nodes))
	for _, n := range nodes {
		if hostsPanel(n) {
			ordered = append(ordered, n)
		}
	}
	for _, n := range nodes {
		if !hostsPanel(n) {
			ordered = append(ordered, n)
		}
	}
	var last error
	for _, n := range ordered {
		last = p.nodeClient(n).DoSlow(ctx, http.MethodPost, "/api/system/update-panel", map[string]string{"version": version}, nil)
		if last == nil {
			return nil
		}
	}
	return fail(http.StatusBadGateway, "Kein Node konnte das Panel aktualisieren. Das Panel muss als Docker-Container auf einer Node-Maschine laufen. Letzte Meldung: "+last.Error())
}

// performNodeUpdate lässt den Node n auf die Version version heben.
func (p *Panel) performNodeUpdate(ctx context.Context, n Node, version string) error {
	if err := p.nodeClient(n).DoSlow(ctx, http.MethodPost, "/api/system/update", map[string]string{"version": version}, nil); err != nil {
		return fail(http.StatusBadGateway, "Der Node „"+n.Name+"“ konnte nicht aktualisiert werden: "+err.Error())
	}
	return nil
}

// updateNodes aktualisiert alle Nodes, die laut status veraltet sind, und meldet, wie viele gestartet wurden.
func (p *Panel) updateNodes(ctx context.Context, status UpdateStatus) (int, error) {
	started := 0
	var failures []string
	for _, entry := range status.Nodes {
		if !entry.UpdateAvailable {
			continue
		}
		n, err := p.store.NodeByID(entry.ID)
		if err == nil {
			err = p.performNodeUpdate(ctx, n, status.LatestVersion)
		}
		if err != nil {
			failures = append(failures, err.Error())
			continue
		}
		started++
	}
	if len(failures) > 0 {
		return started, fail(http.StatusBadGateway, "Nicht alle Nodes konnten aktualisiert werden. "+strings.Join(failures, " "))
	}
	return started, nil
}

// updateLoop prüft regelmäßig auf neue Releases, meldet sie einmal pro Version und installiert sie, wenn Auto-Update an ist.
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

// checkAndAutoUpdate prüft auf ein neues Release, meldet es einmal je Version und installiert es bei eingeschaltetem Auto-Update.
func (p *Panel) checkAndAutoUpdate(ctx context.Context) {
	status := p.getUpdateStatus(ctx, false)
	if !status.UpdateAvailable {
		return
	}

	if lastNotified, _ := p.store.Setting("update_notified_version", ""); lastNotified != status.LatestVersion {
		_ = p.store.SetSetting("update_notified_version", status.LatestVersion)
		p.emitToAllProjects("update_available", fmt.Sprintf("Eine neue Version von Lodestone ist verfügbar: %s (installiert: %s).", status.LatestVersion, p.cfg.Version))
	}
	if !status.AutoUpdate {
		return
	}

	end, err := p.beginUpdate()
	if err != nil {
		return
	}
	defer end()
	p.log.Info("Automatisches Update wird ausgeführt", "neueVersion", status.LatestVersion)
	// Erst die Nodes, zuletzt das Panel: Dessen Austausch beendet diesen Prozess.
	if _, err := p.updateNodes(ctx, status); err != nil {
		p.log.Warn("Automatisches Update der Nodes fehlgeschlagen", "error", err)
		return
	}
	if status.PanelUpdateAvailable {
		if err := p.updatePanel(ctx, status.LatestVersion); err != nil {
			p.log.Warn("Automatisches Update des Panels fehlgeschlagen", "error", err)
			return
		}
	}
	p.emitToAllProjects("update_started", fmt.Sprintf("Das automatische Update auf Version %s wurde gestartet.", status.LatestVersion))
}

// emitToAllProjects meldet ein Ereignis, das nicht an einem Projekt hängt, in jedem Projekt (Verlauf und Webhooks).
func (p *Panel) emitToAllProjects(kind, message string) {
	projects, err := p.store.ListProjects("", true)
	if err != nil {
		return
	}
	for _, project := range projects {
		p.emit(project.ID, "", kind, "info", message)
	}
}

// getUpdates liefert den Versionsstand aus dem Cache (nur Administratoren).
func (p *Panel) getUpdates(w http.ResponseWriter, r *http.Request) {
	if err := requireAdmin(r); err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, p.getUpdateStatus(r.Context(), false))
}

// checkUpdatesNow fragt GitHub sofort nach dem neuesten Release (nur Administratoren).
func (p *Panel) checkUpdatesNow(w http.ResponseWriter, r *http.Request) {
	if err := requireAdmin(r); err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, p.getUpdateStatus(r.Context(), true))
}

// putUpdateSettings ändert das automatische Update und den Update-Kanal; jedes Feld ist optional (nur Administratoren).
func (p *Panel) putUpdateSettings(w http.ResponseWriter, r *http.Request) {
	if err := requireAdmin(r); err != nil {
		writeError(w, err)
		return
	}
	var body struct {
		AutoUpdate *bool   `json:"autoUpdate"`
		Channel    *string `json:"channel"`
	}
	if err := decode(r, &body); err != nil {
		writeError(w, err)
		return
	}
	if body.AutoUpdate == nil && body.Channel == nil {
		writeError(w, fail(http.StatusBadRequest, "Gib autoUpdate oder channel an."))
		return
	}
	if body.Channel != nil && *body.Channel != channelStable && *body.Channel != channelDev {
		writeError(w, fail(http.StatusBadRequest, "Der Update-Kanal muss „stable“ oder „dev“ sein."))
		return
	}
	if body.AutoUpdate != nil {
		value := "false"
		if *body.AutoUpdate {
			value = "true"
		}
		if err := p.store.SetSetting("auto_update", value); err != nil {
			writeError(w, err)
			return
		}
	}
	if body.Channel != nil {
		if err := p.store.SetSetting("update_channel", *body.Channel); err != nil {
			writeError(w, err)
			return
		}
		// Die gemerkte Version gehört zum alten Kanal
		p.releaseMu.Lock()
		p.releaseEntry = nil
		p.releaseMu.Unlock()
	}
	channel, _ := p.updateChannel()
	writeJSON(w, http.StatusOK, map[string]any{"autoUpdate": p.autoUpdateEnabled(), "channel": channel})
}

// triggerPanelUpdate startet das Panel-Update, wenn ein neueres Release vorliegt (nur Administratoren).
func (p *Panel) triggerPanelUpdate(w http.ResponseWriter, r *http.Request) {
	if err := requireAdmin(r); err != nil {
		writeError(w, err)
		return
	}
	version, err := p.releaseToInstall(r.Context())
	if err != nil {
		writeError(w, err)
		return
	}
	if !p.needsUpdate(version, p.cfg.Version) {
		writeError(w, fail(http.StatusConflict, "Das Panel ist schon auf dem neuesten Stand."))
		return
	}
	end, err := p.beginUpdate()
	if err != nil {
		writeError(w, err)
		return
	}
	defer end()
	if err := p.updatePanel(r.Context(), version); err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "message": "Das Panel-Update wurde gestartet. Das Panel startet gleich neu."})
}

// triggerNodeUpdate startet das Update eines Nodes, wenn er erreichbar und veraltet ist (nur Administratoren).
func (p *Panel) triggerNodeUpdate(w http.ResponseWriter, r *http.Request) {
	if err := requireAdmin(r); err != nil {
		writeError(w, err)
		return
	}
	n, err := p.store.NodeByID(r.PathValue("id"))
	if err != nil {
		writeError(w, err)
		return
	}
	version, err := p.releaseToInstall(r.Context())
	if err != nil {
		writeError(w, err)
		return
	}
	current, online := p.nodeVersion(r.Context(), n)
	if !online {
		writeError(w, fail(http.StatusBadGateway, "Der Node ist nicht erreichbar. Prüfe, ob er läuft."))
		return
	}
	if !p.needsUpdate(version, current) {
		writeError(w, fail(http.StatusConflict, "Der Node ist schon auf dem neuesten Stand."))
		return
	}
	end, err := p.beginUpdate()
	if err != nil {
		writeError(w, err)
		return
	}
	defer end()
	if err := p.performNodeUpdate(r.Context(), n, version); err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "message": "Das Node-Update wurde gestartet. Der Daemon startet gleich neu."})
}

// triggerAllNodesUpdate startet das Update aller erreichbaren, veralteten Nodes (nur Administratoren).
func (p *Panel) triggerAllNodesUpdate(w http.ResponseWriter, r *http.Request) {
	if err := requireAdmin(r); err != nil {
		writeError(w, err)
		return
	}
	if _, err := p.releaseToInstall(r.Context()); err != nil {
		writeError(w, err)
		return
	}
	end, err := p.beginUpdate()
	if err != nil {
		writeError(w, err)
		return
	}
	defer end()
	status := p.getUpdateStatus(r.Context(), false)
	started, err := p.updateNodes(r.Context(), status)
	if err != nil {
		writeError(w, err)
		return
	}
	if started == 0 {
		writeError(w, fail(http.StatusConflict, "Alle erreichbaren Nodes sind schon auf dem neuesten Stand."))
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "message": "Das Update der Nodes wurde gestartet."})
}
