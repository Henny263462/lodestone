package panel

import (
	"context"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"math"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/lodestone-mc/lodestone/internal/node"
	"golang.org/x/crypto/ssh"
)

// SFTP: Das Panel ist die einzige Stelle, die entscheidet, wer per SFTP an welchen Server darf. Es rechnet die Zugänge
// aus Mitgliedern, Rollen und den SFTP-Daten der Benutzer aus und schiebt sie je Server zum Node. Der Node kann das
// Panel nicht fragen; deshalb gleicht eine Schleife regelmäßig ab (und sofort nach jeder Änderung hier).

const (
	sftpSyncEvery    = 5 * time.Second
	sftpPollEvery    = 15 * time.Second
	sftpStatusMaxAge = 30 * time.Second
	maxSFTPKeysUser  = 20
)

func init() {
	// SFTP-Anmeldungen erscheinen auch im Verlauf des Projekts (und auf Wunsch per Webhook), nicht nur im Protokoll.
	eventKinds = append(eventKinds, "sftp_login")
	titles["sftp_login"] = "SFTP-Anmeldung"
}

// sftpState ist der Stand des Abgleichs. Der Nullwert ist benutzbar (init legt die Maps an).
type sftpState struct {
	once   sync.Once
	wake   chan struct{}
	mu     sync.Mutex
	pushed map[string]string // Server → Hash der zuletzt geschickten Zugänge
	failed map[string]string // Server → letzter Fehler (nur bei Änderung ins Log)
	nodes  map[string]*sftpNodeState
}

type sftpNodeState struct {
	online      bool
	unsupported bool // älterer Node ohne SFTP
	status      node.SFTPStatus
	at          time.Time
	lastSeq     int64
}

func (s *sftpState) init() {
	s.once.Do(func() {
		s.wake = make(chan struct{}, 1)
		s.pushed, s.failed, s.nodes = map[string]string{}, map[string]string{}, map[string]*sftpNodeState{}
	})
}

// sftpWake stößt den Abgleich sofort an (blockiert nie).
func (p *Panel) sftpWake() {
	p.sftp.init()
	select {
	case p.sftp.wake <- struct{}{}:
	default:
	}
}

// registerSFTPRoutes trägt die Routen für SFTP ein.
func (p *Panel) registerSFTPRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/me/sftp", p.authenticate(p.sessionOnly(p.getMySFTP)))
	mux.HandleFunc("PUT /api/me/sftp/password", p.authenticate(p.sessionOnly(p.putMySFTPPassword)))
	mux.HandleFunc("DELETE /api/me/sftp/password", p.authenticate(p.sessionOnly(p.deleteMySFTPPassword)))
	mux.HandleFunc("PUT /api/me/sftp/password-login", p.authenticate(p.sessionOnly(p.putMySFTPPasswordLogin)))
	mux.HandleFunc("POST /api/me/sftp/keys", p.authenticate(p.sessionOnly(p.addMySFTPKey)))
	mux.HandleFunc("DELETE /api/me/sftp/keys/{id}", p.authenticate(p.sessionOnly(p.deleteMySFTPKey)))
	mux.HandleFunc("GET /api/servers/{id}/sftp", p.authenticate(p.getServerSFTP))
	mux.HandleFunc("PUT /api/servers/{id}/sftp", p.authenticate(p.putServerSFTP))
}

// ---------------------------------------------------------------- Konto

// SFTPAccountDTO ist, was der Benutzer unter Konto über seinen SFTP-Zugang sieht (nie der Hash).
type SFTPAccountDTO struct {
	PasswordSet   bool      `json:"passwordSet"`
	PasswordLogin bool      `json:"passwordLogin"`
	Keys          []SFTPKey `json:"keys"`
}

func (p *Panel) sftpAccount(userID string) (SFTPAccountDTO, error) {
	credential, err := p.store.SFTPCredential(userID)
	if err != nil {
		return SFTPAccountDTO{}, err
	}
	keys, err := p.store.ListSFTPKeys(userID)
	if err != nil {
		return SFTPAccountDTO{}, err
	}
	return SFTPAccountDTO{PasswordSet: credential.PasswordHash != "", PasswordLogin: credential.PasswordLogin, Keys: keys}, nil
}

func (p *Panel) writeSFTPAccount(w http.ResponseWriter, userID string) {
	account, err := p.sftpAccount(userID)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, account)
}

func (p *Panel) getMySFTP(w http.ResponseWriter, r *http.Request) {
	p.writeSFTPAccount(w, userFrom(r.Context()).ID)
}

func (p *Panel) putMySFTPPassword(w http.ResponseWriter, r *http.Request) {
	user := userFrom(r.Context())
	var body struct {
		Password string `json:"password"`
	}
	if err := decode(r, &body); err != nil {
		writeError(w, err)
		return
	}
	if err := validatePassword(body.Password); err != nil {
		writeError(w, fail(http.StatusBadRequest, "Das SFTP-Passwort ist ungültig: "+err.Error()+"."))
		return
	}
	hash, err := HashPassword(body.Password)
	if err != nil {
		writeError(w, err)
		return
	}
	if err := p.store.SetSFTPPassword(user.ID, hash); err != nil {
		writeError(w, err)
		return
	}
	p.sftpWake()
	p.writeSFTPAccount(w, user.ID)
}

func (p *Panel) deleteMySFTPPassword(w http.ResponseWriter, r *http.Request) {
	user := userFrom(r.Context())
	if err := p.store.SetSFTPPassword(user.ID, ""); err != nil {
		writeError(w, err)
		return
	}
	p.sftpWake()
	p.writeSFTPAccount(w, user.ID)
}

func (p *Panel) putMySFTPPasswordLogin(w http.ResponseWriter, r *http.Request) {
	user := userFrom(r.Context())
	var body struct {
		Enabled bool `json:"enabled"`
	}
	if err := decode(r, &body); err != nil {
		writeError(w, err)
		return
	}
	if err := p.store.SetSFTPPasswordLogin(user.ID, body.Enabled); err != nil {
		writeError(w, err)
		return
	}
	p.sftpWake()
	p.writeSFTPAccount(w, user.ID)
}

// parseSFTPKey prüft einen öffentlichen Schlüssel (authorized_keys-Zeile) und liefert ihn ohne Kommentar.
func parseSFTPKey(line string) (normalized, fingerprint, comment string, err error) {
	line = strings.TrimSpace(line)
	if line == "" || len(line) > 16<<10 || strings.ContainsAny(line, "\r\n") {
		return "", "", "", fail(http.StatusBadRequest, "Füge genau eine Zeile ein, z. B. den Inhalt von ~/.ssh/id_ed25519.pub.")
	}
	key, comment, options, rest, parseErr := ssh.ParseAuthorizedKey([]byte(line))
	if parseErr != nil || len(rest) > 0 {
		return "", "", "", fail(http.StatusBadRequest, "Das ist kein öffentlicher SSH-Schlüssel. Füge den Inhalt der .pub-Datei ein (beginnt mit ssh-ed25519, ecdsa-sha2-… oder ssh-rsa).")
	}
	if len(options) > 0 {
		return "", "", "", fail(http.StatusBadRequest, "Optionen vor dem Schlüssel (wie command=…) sind nicht erlaubt.")
	}
	switch key.Type() {
	case ssh.KeyAlgoED25519, ssh.KeyAlgoECDSA256, ssh.KeyAlgoECDSA384, ssh.KeyAlgoECDSA521, ssh.KeyAlgoSKED25519, ssh.KeyAlgoSKECDSA256:
	case ssh.KeyAlgoRSA:
		crypto, ok := key.(ssh.CryptoPublicKey)
		if !ok {
			return "", "", "", fail(http.StatusBadRequest, "Der RSA-Schlüssel lässt sich nicht lesen.")
		}
		if public, ok := crypto.CryptoPublicKey().(*rsa.PublicKey); !ok || public.N.BitLen() < 2048 {
			return "", "", "", fail(http.StatusBadRequest, "RSA-Schlüssel brauchen mindestens 2048 Bit. Erzeuge besser einen neuen mit: ssh-keygen -t ed25519")
		}
	default:
		return "", "", "", fail(http.StatusBadRequest, "Dieser Schlüsseltyp wird nicht unterstützt. Erzeuge einen neuen mit: ssh-keygen -t ed25519")
	}
	normalized = strings.TrimSpace(string(ssh.MarshalAuthorizedKey(key)))
	return normalized, ssh.FingerprintSHA256(key), comment, nil
}

func (p *Panel) addMySFTPKey(w http.ResponseWriter, r *http.Request) {
	user := userFrom(r.Context())
	var body struct {
		Name      string `json:"name"`
		PublicKey string `json:"publicKey"`
	}
	if err := decode(r, &body); err != nil {
		writeError(w, err)
		return
	}
	normalized, fingerprint, comment, err := parseSFTPKey(body.PublicKey)
	if err != nil {
		writeError(w, err)
		return
	}
	name := strings.TrimSpace(body.Name)
	if name == "" {
		name = strings.TrimSpace(comment)
	}
	if name == "" {
		name = "SSH-Schlüssel"
	}
	if utf8.RuneCountInString(name) > 48 {
		writeError(w, fail(http.StatusBadRequest, "Der Name darf höchstens 48 Zeichen haben."))
		return
	}
	existing, err := p.store.ListSFTPKeys(user.ID)
	if err != nil {
		writeError(w, err)
		return
	}
	if len(existing) >= maxSFTPKeysUser {
		writeError(w, fail(http.StatusConflict, "Du hast schon 20 SSH-Schlüssel. Lösche zuerst einen, den du nicht mehr brauchst."))
		return
	}
	created, err := p.store.AddSFTPKey(user.ID, SFTPKey{Name: name, PublicKey: normalized, Fingerprint: fingerprint})
	if err != nil {
		writeError(w, err)
		return
	}
	p.sftpWake()
	writeJSON(w, http.StatusCreated, created)
}

func (p *Panel) deleteMySFTPKey(w http.ResponseWriter, r *http.Request) {
	if err := p.store.DeleteSFTPKey(userFrom(r.Context()).ID, r.PathValue("id")); err != nil {
		writeError(w, err)
		return
	}
	p.sftpWake()
	w.WriteHeader(http.StatusNoContent)
}

// ---------------------------------------------------------------- Server

// SFTPServerDTO ist die Karte „SFTP“ im Dateien-Tab eines Servers.
type SFTPServerDTO struct {
	// Enabled sagt, ob der Node SFTP anbietet.
	Enabled     bool   `json:"enabled"`
	Host        string `json:"host"`
	Port        int    `json:"port"`
	Username    string `json:"username"`
	Fingerprint string `json:"fingerprint"`
	// ReadOnly gilt für den angemeldeten Benutzer.
	ReadOnly bool `json:"readOnly"`
	// OperatorsReadOnly ist die Einstellung des Servers; CanConfigure sagt, ob der Benutzer sie ändern darf.
	OperatorsReadOnly bool `json:"operatorsReadOnly"`
	CanConfigure      bool `json:"canConfigure"`
	// PasswordReady: Ein SFTP-Passwort ist gesetzt und die Anmeldung damit eingeschaltet.
	PasswordReady bool `json:"passwordReady"`
	KeyCount      int  `json:"keyCount"`
}

// sftpLogin ist der Anmeldename eines Benutzers für einen Server: "<benutzer>.<kurz-id>".
func sftpLogin(username, serverID string) string {
	short := serverID
	if len(short) > 8 {
		short = short[:8]
	}
	return strings.ToLower(username) + "." + short
}

// sftpHost ist die Adresse, unter der man die Maschine erreicht.
func sftpHost(n Node) string {
	if n.PublicHost != "" {
		return n.PublicHost
	}
	if parsed, err := url.Parse(n.URL); err == nil && parsed.Hostname() != "" {
		return parsed.Hostname()
	}
	return n.URL
}

func (p *Panel) getServerSFTP(w http.ResponseWriter, r *http.Request) {
	// SFTP braucht das eigene Recht und „Dateien ansehen“; ohne „Dateien ändern“ ist der Zugang nur lesend
	row, _, access, err := p.requireServerPermission(r, PermSFTP, PermFilesRead)
	if err != nil {
		writeError(w, err)
		return
	}
	user := userFrom(r.Context())
	client, n, err := p.client(row.NodeID)
	if err != nil {
		writeError(w, err)
		return
	}
	status, err := p.sftpNodeStatus(r.Context(), client, row.NodeID)
	if err != nil {
		writeError(w, err)
		return
	}
	credential, err := p.store.SFTPCredential(user.ID)
	if err != nil {
		writeError(w, err)
		return
	}
	keys, err := p.store.ListSFTPKeys(user.ID)
	if err != nil {
		writeError(w, err)
		return
	}
	readOnly, err := p.store.SFTPOperatorsReadOnly(row.ID)
	if err != nil {
		writeError(w, err)
		return
	}
	p.sftpWake() // falls der Server neu ist, bekommt der Node seine Zugänge sofort
	writeJSON(w, http.StatusOK, SFTPServerDTO{
		Enabled: status.Enabled, Host: sftpHost(n), Port: status.Port, Username: sftpLogin(user.Username, row.ID), Fingerprint: status.Fingerprint,
		ReadOnly: sftpReadOnly(access, readOnly), OperatorsReadOnly: readOnly, CanConfigure: access.Has(PermSettings),
		PasswordReady: credential.PasswordHash != "" && credential.PasswordLogin, KeyCount: len(keys),
	})
}

func (p *Panel) putServerSFTP(w http.ResponseWriter, r *http.Request) {
	row, _, _, err := p.requireServerPermission(r, PermSettings)
	if err != nil {
		writeError(w, err)
		return
	}
	var body struct {
		OperatorsReadOnly bool `json:"operatorsReadOnly"`
	}
	if err := decode(r, &body); err != nil {
		writeError(w, err)
		return
	}
	if err := p.store.SetSFTPOperatorsReadOnly(row.ID, body.OperatorsReadOnly); err != nil {
		writeError(w, err)
		return
	}
	p.sftpWake()
	writeJSON(w, http.StatusOK, map[string]bool{"operatorsReadOnly": body.OperatorsReadOnly})
}

// sftpNodeStatus liefert Port und Fingerabdruck des Nodes (aus dem Abgleich, sonst frisch gefragt, ohne Ereignisse).
func (p *Panel) sftpNodeStatus(ctx context.Context, client *NodeClient, nodeID string) (node.SFTPStatus, error) {
	p.sftp.init()
	p.sftp.mu.Lock()
	state := p.sftp.nodes[nodeID]
	if state != nil && state.online && time.Since(state.at) < sftpStatusMaxAge {
		status := state.status
		p.sftp.mu.Unlock()
		return status, nil
	}
	p.sftp.mu.Unlock()
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	var status node.SFTPStatus
	err := client.Do(ctx, http.MethodGet, "/api/sftp?after="+strconv.FormatInt(math.MaxInt64, 10), nil, &status)
	var nodeErr *NodeError
	if errors.As(err, &nodeErr) && nodeErr.Status == http.StatusNotFound {
		return node.SFTPStatus{}, nil // älterer Node: kein SFTP
	}
	status.Events = nil
	return status, err
}

// ---------------------------------------------------------------- Abgleich

// sftpReadOnly sagt, ob ein Zugang nur lesen darf: ohne „Dateien ändern“ immer, und wenn der Server „Operatoren nur
// lesend“ eingestellt hat, für alle, die diese Einstellung nicht selbst ändern dürfen (in den Vorlagen: Operatoren).
func sftpReadOnly(access Access, operatorsReadOnly bool) bool {
	return !access.Has(PermFilesWrite) || (operatorsReadOnly && !access.Has(PermSettings))
}

// sftpTargets rechnet aus, welche Zugänge jeder Server haben soll. Hinein darf, wer im Projekt die Rechte „SFTP-Zugang“
// und „Dateien ansehen“ hat (auch angepasste Rechte zählen), globale Administratoren überall; gesperrte Benutzer und
// solche ohne Passwort und Schlüssel bekommen keinen Zugang.
func sftpTargets(snapshot sftpSnapshot) map[string]node.SFTPAccess {
	users := map[string]User{}
	for _, user := range snapshot.users {
		users[user.ID] = user
	}
	targets := make(map[string]node.SFTPAccess, len(snapshot.servers))
	for _, server := range snapshot.servers {
		accesses := map[string]Access{}
		for userID, role := range snapshot.members[server.ProjectID] {
			accesses[userID] = accessFor(roleRanks[role], snapshot.custom[server.ProjectID][userID])
		}
		for _, user := range snapshot.users {
			if user.IsAdmin {
				accesses[user.ID] = accessFor(rankOwner, nil)
			}
		}
		grants := []node.SFTPGrant{}
		for userID, access := range accesses {
			user, ok := users[userID]
			if !ok || user.Disabled || !access.Has(PermSFTP, PermFilesRead) {
				continue
			}
			credential := snapshot.credentials[userID]
			hash := ""
			if credential.PasswordLogin && credential.PasswordHash != "" {
				hash = credential.PasswordHash
			}
			keys := snapshot.keys[userID]
			if hash == "" && len(keys) == 0 {
				continue
			}
			grants = append(grants, node.SFTPGrant{
				Username: sftpLogin(user.Username, server.ID), PasswordHash: hash, Keys: keys,
				ReadOnly: sftpReadOnly(access, snapshot.readOnly[server.ID]),
			})
		}
		sort.Slice(grants, func(i, j int) bool { return grants[i].Username < grants[j].Username })
		targets[server.ID] = node.SFTPAccess{Grants: grants}
	}
	return targets
}

func accessHash(access node.SFTPAccess) string {
	data, _ := json.Marshal(access)
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// sftpLoop gleicht die Zugänge ab, bis ctx endet: alle 5 Sekunden, sofort nach Änderungen hier, beim Start des
// Panels und wenn ein Node wieder erreichbar ist oder neu gestartet wurde.
func (p *Panel) sftpLoop(ctx context.Context) {
	p.sftp.init()
	ticker := time.NewTicker(sftpSyncEvery)
	defer ticker.Stop()
	var lastPoll time.Time
	for {
		poll := time.Since(lastPoll) >= sftpPollEvery
		if poll {
			lastPoll = time.Now()
		}
		p.sftpSync(ctx, poll)
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		case <-p.sftp.wake:
		}
	}
}

// sftpSync schickt jedem Server, dessen Zugänge sich geändert haben, die neue Liste. Mit poll werden vorher die Nodes
// nach ihrem Zustand und neuen Anmeldungen gefragt.
func (p *Panel) sftpSync(ctx context.Context, poll bool) {
	p.sftp.init()
	nodes, err := p.store.ListNodes()
	if err != nil {
		p.log.Warn("SFTP-Abgleich: Nodes nicht lesbar", "error", err)
		return
	}
	byID := map[string]Node{}
	for _, n := range nodes {
		byID[n.ID] = n
		if poll {
			p.sftpPollNode(ctx, n)
		}
	}
	snapshot, err := p.store.sftpSnapshot()
	if err != nil {
		p.log.Warn("SFTP-Abgleich: Datenbank nicht lesbar", "error", err)
		return
	}
	targets := sftpTargets(snapshot)
	keep := map[string]bool{}
	for _, row := range snapshot.servers {
		keep[row.ID] = true
		if n, ok := byID[row.NodeID]; ok {
			p.sftpPush(ctx, n, row.ID, targets[row.ID])
		}
	}
	// Server, die der Node noch hat, das Panel aber nicht mehr (z. B. mit force gelöscht, als der Node fehlte), verlieren
	// alle Zugänge.
	for _, n := range nodes {
		if !p.sftpReachable(n.ID) {
			continue
		}
		views, err := p.nodeViews(ctx, n.ID)
		if err != nil {
			continue
		}
		for id := range views {
			if !keep[id] {
				keep[id] = true
				p.sftpPush(ctx, n, id, node.SFTPAccess{Grants: []node.SFTPGrant{}})
			}
		}
	}
	p.sftp.mu.Lock()
	for id := range p.sftp.pushed {
		if !keep[id] {
			delete(p.sftp.pushed, id)
		}
	}
	p.sftp.mu.Unlock()
}

// sftpReachable sagt, ob der Node beim letzten Abfragen erreichbar war und SFTP kennt.
func (p *Panel) sftpReachable(nodeID string) bool {
	p.sftp.mu.Lock()
	defer p.sftp.mu.Unlock()
	state := p.sftp.nodes[nodeID]
	return state != nil && state.online && !state.unsupported
}

// sftpPush schickt die Zugänge eines Servers, wenn sie sich seit dem letzten Mal geändert haben.
func (p *Panel) sftpPush(ctx context.Context, n Node, serverID string, access node.SFTPAccess) {
	hash := accessHash(access)
	p.sftp.mu.Lock()
	unchanged := p.sftp.pushed[serverID] == hash
	p.sftp.mu.Unlock()
	if unchanged || !p.sftpReachable(n.ID) {
		return
	}
	pushCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	err := p.nodeClient(n).Do(pushCtx, http.MethodPut, "/api/servers/"+serverID+"/sftp-access", access, nil)
	cancel()
	p.sftp.mu.Lock()
	defer p.sftp.mu.Unlock()
	if err != nil {
		if p.sftp.failed[serverID] != err.Error() {
			p.log.Warn("SFTP-Zugänge nicht zum Node geschickt, neuer Versuch folgt", "server", serverID, "node", n.Name, "error", err)
		}
		p.sftp.failed[serverID] = err.Error()
		return
	}
	p.sftp.pushed[serverID] = hash
	delete(p.sftp.failed, serverID)
}

// sftpForget sorgt dafür, dass alle Server eines Nodes beim nächsten Abgleich ihre Zugänge neu bekommen.
func (p *Panel) sftpForget(nodeID string) {
	rows, err := p.store.ListServers("")
	if err != nil {
		return
	}
	p.sftp.mu.Lock()
	defer p.sftp.mu.Unlock()
	for _, row := range rows {
		if row.NodeID == nodeID {
			delete(p.sftp.pushed, row.ID)
		}
	}
}

// sftpPollNode fragt einen Node nach seinem SFTP-Zustand und neuen Anmeldungen und trägt diese ins Protokoll ein.
func (p *Panel) sftpPollNode(ctx context.Context, n Node) {
	p.sftp.mu.Lock()
	state := p.sftp.nodes[n.ID]
	if state == nil {
		state = &sftpNodeState{}
		p.sftp.nodes[n.ID] = state
	}
	after, wasOnline, oldBoot := state.lastSeq, state.online, state.status.BootID
	p.sftp.mu.Unlock()

	fetch := func(after int64) (node.SFTPStatus, error) {
		fetchCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		defer cancel()
		var status node.SFTPStatus
		err := p.nodeClient(n).Do(fetchCtx, http.MethodGet, "/api/sftp?after="+strconv.FormatInt(after, 10), nil, &status)
		return status, err
	}
	status, err := fetch(after)
	if err == nil && oldBoot != "" && status.BootID != oldBoot && after > 0 {
		// Neu gestartet: die Folgenummern beginnen von vorn
		status, err = fetch(0)
	}
	var nodeErr *NodeError
	if errors.As(err, &nodeErr) && nodeErr.Status == http.StatusNotFound {
		p.sftp.mu.Lock()
		state.online, state.unsupported, state.at = true, true, time.Now()
		state.status = node.SFTPStatus{}
		p.sftp.mu.Unlock()
		return
	}
	if err != nil {
		p.sftp.mu.Lock()
		state.online = false
		p.sftp.mu.Unlock()
		return
	}
	if !wasOnline || status.BootID != oldBoot {
		// Wieder erreichbar oder neu gestartet: alles neu schicken
		p.sftpForget(n.ID)
	}
	maxSeq := int64(0)
	if status.BootID == oldBoot {
		maxSeq = after
	}
	for _, event := range status.Events {
		maxSeq = max(maxSeq, event.Seq)
	}
	events := status.Events
	if oldBoot == "" {
		// Erster Kontakt seit dem Start des Panels: nur Anmeldungen übernehmen, die noch nicht im Protokoll stehen
		since := p.store.LatestSFTPAudit()
		events = nil
		for _, event := range status.Events {
			if event.At > since {
				events = append(events, event)
			}
		}
	}
	status.Events = nil
	p.sftp.mu.Lock()
	state.online, state.unsupported, state.status, state.at, state.lastSeq = true, false, status, time.Now(), maxSeq
	p.sftp.mu.Unlock()
	for _, event := range events {
		p.recordSFTPEvent(event)
	}
}

// recordSFTPEvent trägt eine Anmeldung ins Protokoll ein (bei Schlüsseln auch „zuletzt benutzt“).
func (p *Panel) recordSFTPEvent(event node.SFTPEvent) {
	name := event.Username
	if cut := strings.LastIndex(name, "."); cut > 0 {
		name = name[:cut]
	}
	user, err := p.store.UserByName(name)
	if err != nil {
		user = User{Username: name}
	}
	action, status := "SFTP-Anmeldung (Passwort)", http.StatusOK
	if event.Method == "publickey" {
		action = "SFTP-Anmeldung (SSH-Schlüssel)"
		if user.ID != "" && event.KeyFingerprint != "" {
			_ = p.store.TouchSFTPKey(user.ID, event.KeyFingerprint, event.At)
		}
	}
	if !event.Success {
		action, status = "SFTP-Anmeldung abgelehnt (falsches Passwort)", http.StatusUnauthorized
	}
	p.recordAudit(user.ID, user.Username, action, "/api/servers/"+event.ServerID, status, event.IP)
	if row, err := p.store.ServerByID(event.ServerID); err == nil {
		// Die IP-Adresse steht nur im Protokoll der Administratoren, nicht im Verlauf, den alle Mitglieder sehen
		message, level := user.Username+" hat sich per SFTP an „"+row.Name+"“ angemeldet (Passwort).", "info"
		switch {
		case !event.Success:
			message, level = "Abgelehnte SFTP-Anmeldung als "+user.Username+" an „"+row.Name+"“ (falsches Passwort).", "warn"
		case event.Method == "publickey":
			message = user.Username + " hat sich per SFTP an „" + row.Name + "“ angemeldet (SSH-Schlüssel)."
		}
		p.emit(row.ProjectID, row.ID, "sftp_login", level, message)
	}
}
