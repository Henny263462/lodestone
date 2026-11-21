package panel

import (
	"context"
	"errors"
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/hashicorp/yamux"
)

// linkBaseURL ist die Adresse, mit der der NodeClient einen verbundenen Node anspricht. Sie wird nie aufgelöst: Der
// Transport wählt statt einer TCP-Verbindung einen neuen Datenstrom in der Verbindung des Nodes.
const linkBaseURL = "http://node.link"

// errLinkDown meldet, dass ein Node, der sich selbst verbindet, gerade keine Verbindung zum Panel hat.
var errLinkDown = errors.New("der Node ist gerade nicht mit dem Panel verbunden")

// linked meldet, ob sich der Node selbst zum Panel verbindet (dann hat er keine Adresse).
func (n Node) linked() bool { return n.URL == "" }

// linkConn ist die aktive Verbindung eines Nodes.
type linkConn struct {
	session  *yamux.Session
	since    time.Time
	remote   string
	version  string
	insecure bool
}

// linkHub hält je Node höchstens eine aktive Verbindung und einen HTTP-Transport, der über sie wählt.
type linkHub struct {
	mu         sync.Mutex
	links      map[string]*linkConn
	transports map[string]*http.Transport
}

func newLinkHub() *linkHub {
	return &linkHub{links: map[string]*linkConn{}, transports: map[string]*http.Transport{}}
}

// initNodeLink richtet die Verbindungen ein: Nodes ohne Adresse bekommen einen NodeClient, der über ihren Tunnel läuft.
func (p *Panel) initNodeLink() {
	p.link = newLinkHub()
	direct := p.nodeClient
	p.nodeClient = func(n Node) *NodeClient {
		if n.linked() {
			return p.link.client(n)
		}
		return direct(n)
	}
}

// client baut den NodeClient für einen verbundenen Node.
func (h *linkHub) client(n Node) *NodeClient {
	n.URL = linkBaseURL
	transport := h.transport(n.ID)
	return &NodeClient{node: n, http: &http.Client{Timeout: 30 * time.Second, Transport: transport}, transport: transport}
}

func (h *linkHub) transport(nodeID string) *http.Transport {
	h.mu.Lock()
	defer h.mu.Unlock()
	if transport, ok := h.transports[nodeID]; ok {
		return transport
	}
	transport := &http.Transport{
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			return h.open(ctx, nodeID)
		},
		MaxIdleConnsPerHost:    16,
		IdleConnTimeout:        90 * time.Second,
		MaxResponseHeaderBytes: 64 << 10,
		ExpectContinueTimeout:  time.Second,
	}
	h.transports[nodeID] = transport
	return transport
}

// open öffnet einen neuen Datenstrom zum Node.
func (h *linkHub) open(ctx context.Context, nodeID string) (net.Conn, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	h.mu.Lock()
	link := h.links[nodeID]
	h.mu.Unlock()
	if link == nil || link.session.IsClosed() {
		return nil, errLinkDown
	}
	stream, err := link.session.OpenStream()
	if err != nil {
		return nil, errLinkDown
	}
	return stream, nil
}

// attach macht link zur aktiven Verbindung des Nodes. Eine ältere Verbindung desselben Nodes wird getrennt.
func (h *linkHub) attach(nodeID string, link *linkConn) {
	h.mu.Lock()
	old := h.links[nodeID]
	h.links[nodeID] = link
	if transport, ok := h.transports[nodeID]; ok {
		transport.CloseIdleConnections() // gehören zur alten Verbindung
	}
	h.mu.Unlock()
	if old != nil {
		old.session.Close()
	}
}

// detach entfernt session, wenn sie noch die aktive Verbindung des Nodes ist.
func (h *linkHub) detach(nodeID string, session *yamux.Session) {
	h.mu.Lock()
	if link := h.links[nodeID]; link != nil && link.session == session {
		delete(h.links, nodeID)
	}
	if transport, ok := h.transports[nodeID]; ok {
		transport.CloseIdleConnections()
	}
	h.mu.Unlock()
	session.Close()
}

// drop trennt einen Node (gelöscht oder neu gekoppelt).
func (h *linkHub) drop(nodeID string) {
	h.mu.Lock()
	link := h.links[nodeID]
	delete(h.links, nodeID)
	if transport, ok := h.transports[nodeID]; ok {
		transport.CloseIdleConnections()
		delete(h.transports, nodeID)
	}
	h.mu.Unlock()
	if link != nil {
		link.session.Close()
	}
}

// status liefert eine Kopie der aktiven Verbindung eines Nodes.
func (h *linkHub) status(nodeID string) (linkConn, bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	link := h.links[nodeID]
	if link == nil || link.session.IsClosed() {
		return linkConn{}, false
	}
	return *link, true
}
