package panel

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/lodestone-mc/lodestone/internal/nodelink"
)

// joinTokenTTL ist die Gültigkeit eines Join-Tokens.
const joinTokenTTL = 24 * time.Hour

var (
	linkNodeID     = regexp.MustCompile(`^[0-9a-f]{12}$`)
	linkVersion    = regexp.MustCompile(`^[0-9A-Za-z.+_-]{1,40}$`)
	linkPublicHost = regexp.MustCompile(`^[0-9A-Za-z.:\[\]-]{0,253}$`)
)

// registerNodeLinkRoutes hängt die Routen der Node-Verbindung ein. Koppeln und Verbinden sind die einzigen Routen, die
// ein Node am Panel aufrufen darf; sie laufen nicht über die Anmeldung der Benutzer, sondern prüfen Token bzw. Geheimnis.
func (p *Panel) registerNodeLinkRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/nodes/link", p.authenticate(p.listNodeLinks))
	mux.HandleFunc("POST /api/nodes/link", p.authenticate(p.sessionOnly(p.createLinkedNode)))
	mux.HandleFunc("POST /api/nodes/{id}/join-token", p.authenticate(p.sessionOnly(p.renewJoinToken)))
	mux.HandleFunc("POST "+nodelink.PathJoin, p.nodeJoin)
	mux.HandleFunc("GET "+nodelink.PathConnect, p.nodeConnect)
}

// nodeLinkDTO ist der Verbindungszustand eines Nodes, der sich selbst zum Panel verbindet.
type nodeLinkDTO struct {
	NodeID      string `json:"nodeId"`
	Connected   bool   `json:"connected"`
	ConnectedAt int64  `json:"connectedAt,omitempty"`
	Remote      string `json:"remote,omitempty"`
	Version     string `json:"version,omitempty"`
	// Insecure meldet eine unverschlüsselte Verbindung (ws:// statt wss://).
	Insecure bool  `json:"insecure"`
	JoinedAt int64 `json:"joinedAt,omitempty"`
	// JoinExpiresAt ist gesetzt, solange ein Join-Token offen ist.
	JoinExpiresAt int64 `json:"joinExpiresAt,omitempty"`
}

// joinTokenDTO wird nur einmal ausgeliefert: Das Token selbst speichert das Panel nur als Hash.
type joinTokenDTO struct {
	Node      nodeDTO `json:"node"`
	Token     string  `json:"token"`
	ExpiresAt int64   `json:"expiresAt"`
}

func newJoinToken() (token string, expiresAt int64, err error) {
	token, err = randomSecret(nodelink.JoinPrefix)
	return token, time.Now().Add(joinTokenTTL).UnixMilli(), err
}

func randomSecret(prefix string) (string, error) {
	buffer := make([]byte, 32)
	if _, err := rand.Read(buffer); err != nil {
		return "", err
	}
	return prefix + hex.EncodeToString(buffer), nil
}

func (p *Panel) listNodeLinks(w http.ResponseWriter, r *http.Request) {
	if err := requireAdmin(r); err != nil {
		writeError(w, err)
		return
	}
	rows, err := p.store.ListNodeLinks()
	if err != nil {
		writeError(w, err)
		return
	}
	now := time.Now().UnixMilli()
	result := make([]nodeLinkDTO, 0, len(rows))
	for _, row := range rows {
		entry := nodeLinkDTO{NodeID: row.NodeID, JoinedAt: row.JoinedAt}
		if row.JoinExpiresAt > now {
			entry.JoinExpiresAt = row.JoinExpiresAt
		}
		if link, ok := p.link.status(row.NodeID); ok {
			entry.Connected, entry.ConnectedAt, entry.Remote, entry.Version, entry.Insecure = true, link.since.UnixMilli(), link.remote, link.version, link.insecure
		}
		result = append(result, entry)
	}
	writeJSON(w, http.StatusOK, result)
}

func (p *Panel) createLinkedNode(w http.ResponseWriter, r *http.Request) {
	if err := requireAdmin(r); err != nil {
		writeError(w, err)
		return
	}
	var body struct {
		Name       string `json:"name"`
		PublicHost string `json:"publicHost"`
	}
	if err := decode(r, &body); err != nil {
		writeError(w, err)
		return
	}
	name, publicHost := strings.TrimSpace(body.Name), strings.TrimSpace(body.PublicHost)
	if name == "" || len(name) > 48 {
		writeError(w, fail(http.StatusBadRequest, "der Name braucht 1 bis 48 Zeichen"))
		return
	}
	if !linkPublicHost.MatchString(publicHost) {
		writeError(w, fail(http.StatusBadRequest, "die öffentliche Adresse ist ungültig, erwartet wird ein Hostname oder eine IP-Adresse"))
		return
	}
	token, expiresAt, err := newJoinToken()
	if err != nil {
		writeError(w, err)
		return
	}
	created, err := p.store.CreateLinkedNode(name, publicHost, hashToken(token), expiresAt)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, joinTokenDTO{Node: nodeDTO{ID: created.ID, Name: created.Name, PublicHost: created.PublicHost, Linked: true}, Token: token, ExpiresAt: expiresAt})
}

// renewJoinToken erzeugt ein neues Join-Token, z. B. wenn der Node neu aufgesetzt wurde und sein Geheimnis verloren hat.
// Die bestehende Verbindung bleibt, bis sich ein Node mit dem neuen Token koppelt.
func (p *Panel) renewJoinToken(w http.ResponseWriter, r *http.Request) {
	if err := requireAdmin(r); err != nil {
		writeError(w, err)
		return
	}
	n, err := p.store.NodeByID(r.PathValue("id"))
	if err != nil {
		writeError(w, err)
		return
	}
	if !n.linked() {
		writeError(w, fail(http.StatusBadRequest, "dieser Node wird direkt über seine Adresse angesprochen und braucht kein Join-Token"))
		return
	}
	token, expiresAt, err := newJoinToken()
	if err != nil {
		writeError(w, err)
		return
	}
	if err := p.store.SetJoinToken(n.ID, hashToken(token), expiresAt); err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, joinTokenDTO{Node: nodeDTO{ID: n.ID, Name: n.Name, PublicHost: n.PublicHost, Linked: true}, Token: token, ExpiresAt: expiresAt})
}

// linkRejected beantwortet einen Fehlversuch beim Koppeln oder Verbinden. Gedrosselt werden nur Fehlversuche: Ein
// gültiges Token oder Geheimnis (256 Bit, nicht zu erraten) kommt immer durch, damit niemand ohne Konto über viele
// falsche Versuche von derselben Adresse (etwa der des Reverse-Proxys) das Wiederverbinden echter Nodes sperren kann.
func (p *Panel) linkRejected(w http.ResponseWriter, key string, rejected error) {
	if p.throttle.blocked(key) {
		writeError(w, fail(http.StatusTooManyRequests, "zu viele Fehlversuche, versuche es in ein paar Minuten erneut"))
		return
	}
	p.throttle.fail(key)
	writeError(w, rejected)
}

// nodeJoin tauscht ein einmaliges Join-Token gegen die ID und das dauerhafte Geheimnis des Nodes. Fehlversuche werden
// je IP gedrosselt wie die Anmeldung (siehe linkRejected).
func (p *Panel) nodeJoin(w http.ResponseWriter, r *http.Request) {
	key := "nodelink-join|" + p.clientIP(r)
	r.Body = http.MaxBytesReader(w, r.Body, 4<<10)
	var body nodelink.JoinRequest
	if err := decode(r, &body); err != nil {
		writeError(w, err)
		return
	}
	token := strings.TrimSpace(body.Token)
	rejected := fail(http.StatusUnauthorized, "das Join-Token ist ungültig, abgelaufen oder schon benutzt – erzeuge im Panel unter Nodes ein neues")
	if !strings.HasPrefix(token, nodelink.JoinPrefix) || len(token) > 128 {
		p.linkRejected(w, key, rejected)
		return
	}
	secret, err := randomSecret(nodelink.SecretPrefix)
	if err != nil {
		writeError(w, err)
		return
	}
	n, err := p.store.RedeemJoinToken(hashToken(token), hashToken(secret), time.Now())
	if errors.Is(err, ErrNotFound) {
		p.linkRejected(w, key, rejected)
		return
	}
	if err != nil {
		writeError(w, err)
		return
	}
	// Eine noch offene Verbindung mit dem alten Geheimnis gilt ab jetzt nicht mehr
	p.link.drop(n.ID)
	p.invalidate(n.ID)
	p.log.Info("Node gekoppelt", "node", n.Name, "ip", p.clientIP(r))
	writeJSON(w, http.StatusOK, nodelink.JoinResponse{NodeID: n.ID, Secret: secret})
}

// nodeConnect nimmt die dauerhafte Verbindung eines gekoppelten Nodes an und hält sie, bis sie abbricht. Über sie
// erreicht das Panel den Node; der Node selbst kann darüber nichts am Panel aufrufen.
func (p *Panel) nodeConnect(w http.ResponseWriter, r *http.Request) {
	ip := p.clientIP(r)
	key := "nodelink-connect|" + ip
	nodeID := r.Header.Get(nodelink.HeaderNode)
	secret := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
	rejected := fail(http.StatusUnauthorized, "unbekannter Node oder falsches Geheimnis – kopple den Node im Panel unter Nodes neu")
	if !linkNodeID.MatchString(nodeID) || !strings.HasPrefix(secret, nodelink.SecretPrefix) || len(secret) > 128 {
		p.linkRejected(w, key, rejected)
		return
	}
	expected, err := p.store.NodeLinkSecretHash(nodeID)
	if err != nil && !errors.Is(err, ErrNotFound) {
		writeError(w, err)
		return
	}
	if err != nil || subtle.ConstantTimeCompare([]byte(expected), []byte(hashToken(secret))) != 1 {
		p.linkRejected(w, key, rejected)
		return
	}
	session, err := nodelink.Accept(w, r)
	if err != nil {
		return // Accept hat schon geantwortet
	}
	version := r.Header.Get(nodelink.HeaderVersion)
	if !linkVersion.MatchString(version) {
		version = ""
	}
	insecure := r.TLS == nil && !strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https")
	p.link.attach(nodeID, &linkConn{session: session, since: time.Now(), remote: ip, version: version, insecure: insecure})
	p.invalidate(nodeID)
	p.log.Info("Node verbunden", "node", nodeID, "ip", ip, "encrypted", !insecure)
	<-session.CloseChan()
	p.link.detach(nodeID, session)
	p.invalidate(nodeID)
	p.log.Info("Node getrennt", "node", nodeID)
}
