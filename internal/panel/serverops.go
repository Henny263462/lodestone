package panel

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/lodestone-mc/lodestone/internal/node"
)

// Abstände der Vorgänge; in Tests kürzer.
var (
	// opBeat ist der Takt, in dem ein Vorgang Fortschritt und Lebenszeichen schreibt und nach einem Abbruch fragt.
	opBeat = time.Second
	// opPoll ist der Takt, in dem auf das Ende eines Stopps gewartet wird.
	opPoll = 500 * time.Millisecond
	// opStopTimeout ist die Zeit, die ein Server zum Stoppen bekommt, bevor er beendet wird.
	opStopTimeout = 3 * time.Minute
	// storageEvery ist der Abstand, in dem das Panel die Speicherlimits prüft.
	storageEvery = 5 * time.Minute
)

const (
	opTimeout = 6 * time.Hour
	// moveReserve muss auf dem Ziel nach einem Umzug oder einer Kopie frei bleiben.
	moveReserve = int64(1) << 30
)

func init() {
	// Die Ereignisse von Speicherplatz, Kopie und Umzug gehören zur zentralen Liste in events.go.
	eventKinds = append(eventKinds, "storage", "transfer")
	defaultHookEvents = append(defaultHookEvents, "storage")
	titles["storage"] = "Speicher fast voll"
	titles["transfer"] = "Kopie und Umzug"
}

// humanBytes schreibt eine Größe deutsch (Dezimalkomma).
func humanBytes(n int64) string {
	switch {
	case n >= 1<<30:
		return strings.Replace(fmt.Sprintf("%.1f GB", float64(n)/(1<<30)), ".", ",", 1)
	case n >= 1<<20:
		return fmt.Sprintf("%d MB", n>>20)
	default:
		return fmt.Sprintf("%d KB", n>>10)
	}
}

// ---------------------------------------------------------------- Übertragungen zum Node

// longClient ist ein NodeClient ohne Gesamt-Zeitlimit für Kopien und Übertragungen; das Ende bestimmt der Kontext.
func (p *Panel) longClient(n Node) *NodeClient {
	base := p.nodeClient(n)
	return &NodeClient{node: base.node, http: &http.Client{Transport: base.http.Transport}}
}

// send schickt eine Anfrage mit beliebigem Körper (als Strom) und liefert die Antwort. Fehlerantworten werden zu NodeError.
func (c *NodeClient) send(ctx context.Context, method, path string, body io.Reader, header http.Header) (*http.Response, error) {
	request, err := http.NewRequestWithContext(ctx, method, c.url(path), body)
	if err != nil {
		return nil, err
	}
	for key, values := range header {
		request.Header[key] = values
	}
	request.Header.Set("Authorization", "Bearer "+c.node.Token)
	response, err := c.http.Do(request)
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, fmt.Errorf("%w (%s): %v", ErrNodeOffline, c.node.Name, err)
	}
	if response.StatusCode >= 300 {
		defer response.Body.Close()
		var failure struct {
			Error string `json:"error"`
		}
		_ = json.NewDecoder(io.LimitReader(response.Body, 1<<20)).Decode(&failure)
		if failure.Error == "" {
			failure.Error = fmt.Sprintf("Node antwortet mit %d", response.StatusCode)
		}
		return nil, &NodeError{Status: response.StatusCode, Message: failure.Error}
	}
	return response, nil
}

// countingReader zählt die gelesenen Bytes für die Fortschrittsanzeige.
type countingReader struct {
	r     io.Reader
	count *atomic.Int64
}

func (c *countingReader) Read(p []byte) (int, error) {
	n, err := c.r.Read(p)
	c.count.Add(int64(n))
	return n, err
}

func isNotFound(err error) bool {
	var nodeErr *NodeError
	return errors.As(err, &nodeErr) && nodeErr.Status == http.StatusNotFound
}

// ---------------------------------------------------------------- Ablauf eines Vorgangs

type opResult struct {
	Message  string
	Warnings []string
	ServerID string
}

// opRunner ist der laufende Vorgang aus Sicht der Arbeit: Kontext, Fortschritt und Schritt.
type opRunner struct {
	ctx      context.Context
	done     atomic.Int64
	total    atomic.Int64
	canceled atomic.Bool

	mu   sync.Mutex
	step string
}

func (r *opRunner) setStep(text string) {
	r.mu.Lock()
	r.step = text
	r.mu.Unlock()
}

func (r *opRunner) currentStep() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.step
}

// startOp legt den Vorgang an und führt work im Hintergrund aus.
func (p *Panel) startOp(op ServerOp, work func(*opRunner) (opResult, error)) (ServerOp, error) {
	created, err := p.store.CreateOp(op)
	if errors.Is(err, ErrOpRunning) {
		return ServerOp{}, fail(http.StatusConflict, "Im Projekt läuft schon eine Kopie oder ein Umzug. Warte, bis er fertig ist.")
	}
	if err != nil {
		return ServerOp{}, err
	}
	go p.runOp(created, work)
	return created, nil
}

func (p *Panel) runOp(op ServerOp, work func(*opRunner) (opResult, error)) {
	ctx, cancel := context.WithTimeout(context.Background(), opTimeout)
	defer cancel()
	runner := &opRunner{ctx: ctx, step: op.Step}
	runner.total.Store(op.Total)
	stop, stopped := make(chan struct{}), make(chan struct{})
	go func() {
		defer close(stopped)
		ticker := time.NewTicker(opBeat)
		defer ticker.Stop()
		for {
			select {
			case <-stop:
				return
			case <-ticker.C:
				wanted, err := p.store.UpdateOpProgress(op.ID, runner.currentStep(), runner.done.Load(), runner.total.Load())
				if err == nil && wanted && !runner.canceled.Swap(true) {
					cancel()
				}
			}
		}
	}()
	result, err := func() (result opResult, err error) {
		defer func() {
			if value := recover(); value != nil {
				err = fmt.Errorf("interner Fehler: %v", value)
			}
		}()
		return work(runner)
	}()
	close(stop)
	<-stopped

	state, message := OpDone, result.Message
	switch {
	case err != nil && runner.canceled.Load():
		state, message = OpCanceled, strings.TrimSpace("Abgebrochen. "+result.Message)
	case err != nil:
		state, message = OpFailed, strings.TrimSpace("Fehlgeschlagen: "+opErrorText(err)+". "+result.Message)
	}
	if err := p.store.FinishOp(op.ID, state, message, result.Warnings, result.ServerID, runner.done.Load(), runner.total.Load()); err != nil {
		p.log.Warn("Vorgang nicht abgeschlossen gespeichert", "op", op.ID, "error", err)
	}
	if state != OpDone {
		level := "error"
		if state == OpCanceled {
			level = "info"
		}
		p.emit(op.ProjectID, op.ServerID, "transfer", level, op.Label+": "+message)
	}
}

// opErrorText macht aus einem Fehler einen Satzteil für Menschen.
func opErrorText(err error) string {
	var httpErr *httpError
	var nodeErr *NodeError
	switch {
	case errors.As(err, &httpErr):
		return strings.TrimSuffix(httpErr.message, ".")
	case errors.As(err, &nodeErr):
		return strings.TrimSuffix(nodeErr.Message, ".")
	case errors.Is(err, context.Canceled):
		return "abgebrochen"
	case errors.Is(err, context.DeadlineExceeded):
		return "Zeitüberschreitung"
	}
	return strings.TrimSuffix(err.Error(), ".")
}

// ---------------------------------------------------------------- Klonen

type cloneBody struct {
	Name       string `json:"name"`
	WithWorlds bool   `json:"withWorlds"`
}

// runClone kopiert einen Server auf seinem Node und trägt die Kopie ein. In einem Netzwerk kommt sie als Backend
// außerhalb des Try-Ablaufs dazu; ein Server-Projekt bekommt für die Kopie ein eigenes Projekt.
func (p *Panel) runClone(r *opRunner, source ServerRow, project Project, body cloneBody, user User) (opResult, error) {
	n, err := p.store.NodeByID(source.NodeID)
	if err != nil {
		return opResult{}, err
	}
	client := p.longClient(n)
	id := newID()
	undoNode := func() {
		bg, cancel := context.WithTimeout(context.Background(), 90*time.Second)
		defer cancel()
		if err := client.Do(bg, http.MethodDelete, "/api/servers/"+id, nil, nil); err != nil && !isNotFound(err) {
			p.log.Warn("Halbe Kopie nicht entfernt", "server", id, "error", err)
		}
		p.invalidate(source.NodeID)
	}
	r.setStep("Dateien werden kopiert")
	var view node.View
	if err := client.Do(r.ctx, http.MethodPost, "/api/servers/"+source.ID+"/clone", node.CloneRequest{ID: id, Name: body.Name, WithWorlds: body.WithWorlds}, &view); err != nil {
		if r.ctx.Err() != nil {
			undoNode() // die Kopie kann im selben Moment fertig geworden sein
		}
		return opResult{Message: "Das Original ist unverändert."}, err
	}
	p.invalidate(source.NodeID)

	r.setStep("Kopie wird eingetragen")
	target, ownProject := project, false
	if project.Kind == "server" {
		secret, err := randomToken()
		if err != nil {
			undoNode()
			return opResult{}, err
		}
		target, err = p.store.CreateProject(Project{Name: body.Name, Kind: "server", Description: "Kopie von „" + source.Name + "“", Secret: secret[:24], OwnerID: user.ID})
		if err != nil {
			undoNode()
			return opResult{}, err
		}
		ownProject = true
	}
	existing, err := p.store.ListServers(target.ID)
	if err != nil {
		undoNode()
		return opResult{}, err
	}
	row := ServerRow{ID: id, ProjectID: target.ID, NodeID: source.NodeID, Name: body.Name, Slug: Slugify(body.Name), Role: source.Role, Software: source.Software,
		TryPosition: len(existing), Voicechat: source.Voicechat, Modpack: source.Modpack, AutoRestart: source.AutoRestart}
	if _, err := p.store.CreateServer(row); err != nil {
		undoNode()
		if ownProject {
			_ = p.store.DeleteProject(target.ID)
		}
		return opResult{Message: "Das Original ist unverändert."}, err
	}

	result := opResult{ServerID: id, Warnings: []string{}}
	switch {
	case project.Kind == "network":
		r.setStep("Netzwerk wird abgeglichen")
		bg, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		synced, err := p.syncNetwork(bg, project.ID)
		cancel()
		if err != nil {
			result.Warnings = append(result.Warnings, "Der Netzwerk-Abgleich ist fehlgeschlagen: "+opErrorText(err)+". Starte ihn im Tab „Netzwerk“ neu.")
		}
		for _, failed := range synced.Failed {
			result.Warnings = append(result.Warnings, "Abgleich fehlgeschlagen: "+failed)
		}
		result.Message = "„" + body.Name + "“ ist angelegt. Sie ist noch nicht im Try-Ablauf des Proxys: Schalte sie im Tab „Netzwerk“ ein, wenn sie fertig eingerichtet ist."
	case ownProject:
		result.Message = "„" + body.Name + "“ ist im neuen Projekt „" + target.Name + "“ angelegt, weil ein Server-Projekt genau einen Server hat. Mitglieder und Zeitpläne kommen nicht mit."
	default:
		result.Message = "„" + body.Name + "“ ist angelegt."
	}
	if !body.WithWorlds {
		result.Message += " Die Welten wurden nicht kopiert, sie entstehen beim ersten Start neu."
	}
	p.emit(project.ID, source.ID, "transfer", "info", fmt.Sprintf("„%s“ wurde als „%s“ kopiert.", source.Name, body.Name))
	return result, nil
}

// ---------------------------------------------------------------- Umzug

type migrationPlan struct {
	project        Project
	rows           []ServerRow
	source, target Node
	sizes          map[string]int64
	total          int64
}

// nodeStorage fragt die Speicherübersicht eines Nodes ab.
func (p *Panel) fetchNodeStorage(ctx context.Context, n Node) (node.NodeStorage, error) {
	ctx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	var storage node.NodeStorage
	err := p.nodeClient(n).Do(ctx, http.MethodGet, "/api/storage", nil, &storage)
	return storage, err
}

// planMigration prüft vorab, ob ein Umzug gehen kann, damit offensichtliche Fehler sofort sichtbar sind.
func (p *Panel) planMigration(ctx context.Context, project Project, targetID string) (migrationPlan, error) {
	plan := migrationPlan{project: project, sizes: map[string]int64{}}
	rows, err := p.store.ListServers(project.ID)
	if err != nil {
		return plan, err
	}
	if len(rows) == 0 {
		return plan, fail(http.StatusBadRequest, "Das Projekt hat noch keinen Server, der umziehen könnte.")
	}
	if groups, err := p.store.ListGroups(project.ID); err == nil && len(groups) > 0 {
		return plan, fail(http.StatusConflict, "Netzwerke mit Gruppen können noch nicht umziehen, weil der Autoscaler laufend Instanzen anlegt und löscht. Lösche die Gruppen vorher und lege sie danach neu an.")
	}
	for _, row := range rows {
		if row.NodeID != rows[0].NodeID {
			return plan, fail(http.StatusConflict, "Die Server des Projekts liegen auf verschiedenen Nodes. Ein Netzwerk muss auf einem Node liegen, damit die Server sich finden.")
		}
	}
	if targetID == rows[0].NodeID {
		return plan, fail(http.StatusBadRequest, "Das Projekt liegt schon auf diesem Node.")
	}
	if plan.target, err = p.store.NodeByID(targetID); err != nil {
		return plan, fail(http.StatusBadRequest, "Den Ziel-Node gibt es nicht.")
	}
	if plan.source, err = p.store.NodeByID(rows[0].NodeID); err != nil {
		return plan, err
	}
	from, err := p.fetchNodeStorage(ctx, plan.source)
	if err != nil {
		return plan, fail(http.StatusBadGateway, "Der Node „"+plan.source.Name+"“ ist nicht erreichbar. Ein Umzug braucht beide Nodes.")
	}
	to, err := p.fetchNodeStorage(ctx, plan.target)
	if err != nil {
		return plan, fail(http.StatusBadGateway, "Der Ziel-Node „"+plan.target.Name+"“ ist nicht erreichbar. Prüfe, ob er läuft.")
	}
	sizes := map[string]int64{}
	for _, entry := range from.Servers {
		sizes[entry.ID] = entry.Bytes
	}
	for _, entry := range to.Servers {
		for _, row := range rows {
			if entry.ID == row.ID {
				return plan, fail(http.StatusConflict, "Auf „"+plan.target.Name+"“ liegt noch eine alte Kopie von „"+row.Name+"“ aus einem früheren Umzug. Lösche sie dort zuerst (Ordner servers/"+row.ID+").")
			}
		}
	}
	for _, row := range rows {
		plan.sizes[row.ID] = sizes[row.ID]
		plan.total += sizes[row.ID]
	}
	if to.Free > 0 && plan.total > 0 && to.Free < plan.total+moveReserve {
		return plan, fail(http.StatusConflict, fmt.Sprintf("Auf „%s“ ist zu wenig Platz frei (frei %s, nötig etwa %s).", plan.target.Name, humanBytes(to.Free), humanBytes(plan.total+moveReserve)))
	}
	// Der Proxy startet zuletzt, damit er seine Server gleich findet
	sort.SliceStable(rows, func(i, j int) bool { return rows[i].Role != RoleProxy && rows[j].Role == RoleProxy })
	plan.rows = rows
	return plan, nil
}

// runMigration zieht die Server um: stoppen, übertragen (als Strom, ohne Zwischenspeicher), umtragen, alte löschen,
// wieder starten. Bis zum Umtragen lässt jeder Fehler und jeder Abbruch die Quellserver unverändert.
func (p *Panel) runMigration(r *opRunner, plan migrationPlan) (opResult, error) {
	src, dst := p.longClient(plan.source), p.longClient(plan.target)
	wasRunning := map[string]bool{}
	rollback := func() string {
		bg, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
		defer cancel()
		var leftovers []string
		for _, row := range plan.rows {
			if err := dst.Do(bg, http.MethodDelete, "/api/servers/"+row.ID, nil, nil); err != nil && !isNotFound(err) {
				leftovers = append(leftovers, row.Name)
			}
		}
		restarted := false
		for _, row := range plan.rows {
			if wasRunning[row.ID] {
				restarted = true
				if err := src.Do(bg, http.MethodPost, "/api/servers/"+row.ID+"/power", map[string]string{"action": "start"}, nil); err != nil {
					p.log.Warn("Server nach dem abgebrochenen Umzug nicht gestartet", "server", row.Name, "error", err)
				}
			}
		}
		p.invalidate(plan.source.ID)
		p.invalidate(plan.target.ID)
		text := "Alle Server sind unverändert auf „" + plan.source.Name + "“"
		if restarted {
			text += " und werden wieder gestartet"
		}
		text += "."
		if len(leftovers) > 0 {
			text += " Auf „" + plan.target.Name + "“ ließen sich Reste nicht löschen: " + strings.Join(leftovers, ", ") + "."
		}
		return text
	}

	r.setStep("Server werden gestoppt")
	for _, row := range plan.rows {
		running, err := p.stopForMove(r.ctx, src, row)
		wasRunning[row.ID] = running
		if err != nil {
			return opResult{Message: rollback()}, err
		}
	}
	for i, row := range plan.rows {
		r.setStep(fmt.Sprintf("„%s“ wird übertragen (%d von %d)", row.Name, i+1, len(plan.rows)))
		if err := p.transferServer(r, src, dst, row, plan.sizes[row.ID]); err != nil {
			return opResult{Message: rollback()}, err
		}
	}
	if err := r.ctx.Err(); err != nil {
		return opResult{Message: rollback()}, err
	}
	ids := make([]string, len(plan.rows))
	for i, row := range plan.rows {
		ids[i] = row.ID
	}
	if err := p.store.MoveServersToNode(ids, plan.target.ID); err != nil {
		return opResult{Message: rollback()}, err
	}

	// Ab hier gilt der neue Node. Was jetzt schiefgeht, ist ein Hinweis, kein Fehlschlag mehr.
	r.setStep("Alte Server werden gelöscht")
	result := opResult{ServerID: plan.rows[0].ID, Warnings: []string{}}
	bg, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	for _, row := range plan.rows {
		if row.PublicAddress != "" || row.PublicVoiceAddress != "" {
			address := strings.TrimSpace(row.PublicAddress + " " + row.PublicVoiceAddress)
			result.Warnings = append(result.Warnings, "„"+row.Name+"“ hatte die öffentliche Adresse „"+address+"“. Sie gehörte zur alten Maschine und wurde entfernt. Lege auf „"+plan.target.Name+"“ einen neuen playit-Tunnel an (Server → Einstellungen) und lösche den alten auf „"+plan.source.Name+"“ (Nodes → playit).")
		}
		if err := src.Do(bg, http.MethodDelete, "/api/servers/"+row.ID, nil, nil); err != nil && !isNotFound(err) {
			result.Warnings = append(result.Warnings, "Auf „"+plan.source.Name+"“ ließ sich „"+row.Name+"“ nicht löschen ("+opErrorText(err)+"). Die alten Dateien belegen dort noch Platz.")
		}
	}
	p.invalidate(plan.source.ID)
	p.invalidate(plan.target.ID)
	if plan.project.Kind == "network" {
		r.setStep("Netzwerk wird abgeglichen")
		synced, err := p.syncNetwork(bg, plan.project.ID)
		if err != nil {
			result.Warnings = append(result.Warnings, "Der Netzwerk-Abgleich ist fehlgeschlagen: "+opErrorText(err)+". Starte ihn im Tab „Netzwerk“ neu.")
		}
		for _, failed := range synced.Failed {
			result.Warnings = append(result.Warnings, "Abgleich fehlgeschlagen: "+failed)
		}
	}
	r.setStep("Server werden gestartet")
	for _, row := range plan.rows {
		if !wasRunning[row.ID] {
			continue
		}
		if err := dst.Do(bg, http.MethodPost, "/api/servers/"+row.ID+"/power", map[string]string{"action": "start"}, nil); err != nil {
			result.Warnings = append(result.Warnings, "„"+row.Name+"“ ließ sich auf „"+plan.target.Name+"“ nicht starten: "+opErrorText(err)+".")
		}
	}
	p.invalidate(plan.target.ID)

	result.Message = "Umzug auf „" + plan.target.Name + "“ abgeschlossen."
	if moved, err := p.store.ListServers(plan.project.ID); err == nil {
		for _, dto := range p.serverDTOs(bg, moved) {
			if dto.Address != "" && dto.Role != RoleBackend {
				result.Message += " „" + dto.Name + "“ ist jetzt unter " + dto.Address + " erreichbar."
			}
		}
	}
	p.emit(plan.project.ID, "", "transfer", "info", fmt.Sprintf("„%s“ ist vom Node „%s“ auf „%s“ umgezogen.", plan.project.Name, plan.source.Name, plan.target.Name))
	return result, nil
}

// stopForMove stoppt einen Server und wartet, bis er aus ist (notfalls mit Kill). wasRunning sagt, ob er lief.
func (p *Panel) stopForMove(ctx context.Context, client *NodeClient, row ServerRow) (bool, error) {
	view, err := p.viewOf(ctx, client, row.ID)
	if err != nil {
		return false, err
	}
	switch view.State {
	case node.StateOffline:
		return false, nil
	case node.StateInstalling, node.StateRestoring:
		return false, fail(http.StatusConflict, "„"+row.Name+"“ wird gerade installiert oder wiederhergestellt. Warte, bis das fertig ist")
	}
	running := view.State == node.StateRunning || view.State == node.StateStarting
	if running {
		if err := client.Do(ctx, http.MethodPost, "/api/servers/"+row.ID+"/power", map[string]string{"action": "stop"}, nil); err != nil {
			_ = client.Do(ctx, http.MethodPost, "/api/servers/"+row.ID+"/power", map[string]string{"action": "kill"}, nil)
		}
	}
	deadline, killed := time.Now().Add(opStopTimeout), false
	for {
		select {
		case <-ctx.Done():
			return running, ctx.Err()
		case <-time.After(opPoll):
		}
		view, err := p.viewOf(ctx, client, row.ID)
		if err != nil {
			return running, err
		}
		if view.State == node.StateOffline {
			return running, nil
		}
		if time.Now().After(deadline) {
			if killed {
				return running, fail(http.StatusConflict, "„"+row.Name+"“ lässt sich nicht stoppen")
			}
			_ = client.Do(ctx, http.MethodPost, "/api/servers/"+row.ID+"/power", map[string]string{"action": "kill"}, nil)
			killed, deadline = true, time.Now().Add(30*time.Second)
		}
	}
}

func (p *Panel) viewOf(ctx context.Context, client *NodeClient, id string) (node.View, error) {
	callCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	var view node.View
	err := client.Do(callCtx, http.MethodGet, "/api/servers/"+id, nil, &view)
	return view, err
}

// transferServer streamt den Export der Quelle direkt in den Import des Ziels. Das Panel hält dabei nie mehr als
// einen Puffer im Speicher.
func (p *Panel) transferServer(r *opRunner, src, dst *NodeClient, row ServerRow, expected int64) error {
	export, err := src.send(r.ctx, http.MethodGet, "/api/servers/"+row.ID+"/export", nil, nil)
	if err != nil {
		return err
	}
	defer export.Body.Close()
	header := http.Header{}
	header.Set("Content-Type", "application/gzip")
	if expected > 0 {
		header.Set(node.SizeHeader, strconv.FormatInt(expected, 10))
	}
	answer, err := dst.send(r.ctx, http.MethodPost, "/api/servers/import", &countingReader{r: export.Body, count: &r.done}, header)
	if err != nil {
		return err
	}
	_, _ = io.Copy(io.Discard, io.LimitReader(answer.Body, 1<<20))
	return answer.Body.Close()
}

// ---------------------------------------------------------------- Speicherlimits beobachten

// storageLoop meldet Server, die ihrem Speicherlimit nahe kommen, und Nodes mit wenig freiem Platz.
func (p *Panel) storageLoop(ctx context.Context) {
	levels := map[string]int{}
	ticker := time.NewTicker(storageEvery)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			p.checkStorage(ctx, levels)
		}
	}
}

// Stufen der Speicherwarnung je Server bzw. Node.
const (
	storageOK = iota
	storageWarn
	storageFull
)

// checkStorage vergleicht die Messungen der Nodes mit den Limits. levels merkt sich die gemeldete Stufe, damit jede
// Stufe nur einmal gemeldet wird; erst unter 85 % gilt ein Server wieder als frei.
func (p *Panel) checkStorage(ctx context.Context, levels map[string]int) {
	rows, err := p.store.ListServers("")
	if err != nil {
		return
	}
	byNode := map[string][]ServerRow{}
	for _, row := range rows {
		byNode[row.NodeID] = append(byNode[row.NodeID], row)
	}
	for nodeID, list := range byNode {
		n, err := p.store.NodeByID(nodeID)
		if err != nil {
			continue
		}
		storage, err := p.fetchNodeStorage(ctx, n)
		if err != nil {
			continue
		}
		usage := map[string]node.ServerStorage{}
		for _, entry := range storage.Servers {
			usage[entry.ID] = entry
		}
		for _, row := range list {
			entry, ok := usage[row.ID]
			if !ok || entry.LimitMB <= 0 || entry.MeasuredAt == 0 {
				delete(levels, row.ID)
				continue
			}
			limit := int64(entry.LimitMB) << 20
			percent := entry.Bytes * 100 / limit
			level := storageOK
			switch {
			case percent >= 100:
				level = storageFull
			case percent >= 90:
				level = storageWarn
			case percent >= 85 && levels[row.ID] > storageOK:
				level = storageWarn
			}
			if level > levels[row.ID] {
				if level == storageFull {
					p.emit(row.ProjectID, row.ID, "storage", "error", fmt.Sprintf("„%s“ hat sein Speicherlimit erreicht (%s von %s). Starten, Hochladen und Entpacken sind gesperrt, bis Dateien gelöscht werden oder das Limit steigt.",
						row.Name, humanBytes(entry.Bytes), humanBytes(limit)))
				} else {
					p.emit(row.ProjectID, row.ID, "storage", "warn", fmt.Sprintf("Speicher fast voll: „%s“ belegt %s von %s (%d %%). Bei 100 %% startet der Server nicht mehr und nimmt keine Uploads an.",
						row.Name, humanBytes(entry.Bytes), humanBytes(limit), percent))
				}
			}
			levels[row.ID] = level
		}
		key := "node:" + nodeID
		low := storage.Total > 0 && storage.Free*100 < storage.Total*5
		if low && levels[key] == storageOK {
			for _, projectID := range distinctProjects(list) {
				p.emit(projectID, "", "storage", "warn", fmt.Sprintf("Auf dem Node „%s“ sind nur noch %s von %s frei. Lösche alte Backups oder ziehe Server auf einen anderen Node um.",
					n.Name, humanBytes(storage.Free), humanBytes(storage.Total)))
			}
		}
		if low {
			levels[key] = storageWarn
		} else if storage.Total > 0 && storage.Free*100 >= storage.Total*8 {
			delete(levels, key)
		}
	}
}
