package nodelink

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/hashicorp/yamux"
)

func TestEndpoints(t *testing.T) {
	cases := []struct {
		in, join, connect string
		secure, ok        bool
	}{
		{"https://panel.example.org", "https://panel.example.org/api/node-link/join", "wss://panel.example.org/api/node-link/connect", true, true},
		{"wss://panel.example.org/", "https://panel.example.org/api/node-link/join", "wss://panel.example.org/api/node-link/connect", true, true},
		{"http://10.0.0.2:8480", "http://10.0.0.2:8480/api/node-link/join", "ws://10.0.0.2:8480/api/node-link/connect", false, true},
		{"https://example.org/lodestone/", "https://example.org/lodestone/api/node-link/join", "wss://example.org/lodestone/api/node-link/connect", true, true},
		{"ftp://example.org", "", "", false, false},
		{"https://user:pass@example.org", "", "", false, false},
		{"panel.example.org", "", "", false, false},
		{"https://example.org/?x=1", "", "", false, false},
	}
	for _, c := range cases {
		join, connect, secure, err := Endpoints(c.in)
		if (err == nil) != c.ok {
			t.Errorf("%q: Fehler %v, erwartet ok=%v", c.in, err, c.ok)
			continue
		}
		if join != c.join || connect != c.connect || secure != c.secure {
			t.Errorf("%q: %q %q %v", c.in, join, connect, secure)
		}
	}
}

// pair baut eine Verbindung wie im Betrieb: panel ist die Seite des Panels, node die des Nodes.
func pair(t *testing.T) (panel, node *yamux.Session) {
	t.Helper()
	accepted := make(chan *yamux.Session, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer ldn_richtig" {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = io.WriteString(w, `{"error":"falsches Geheimnis"}`)
			return
		}
		session, err := Accept(w, r)
		if err != nil {
			return
		}
		accepted <- session
		<-session.CloseChan()
	}))
	t.Cleanup(server.Close)
	_, connect, _, _ := Endpoints(server.URL)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	node, err := Dial(ctx, connect, "0123456789ab", "ldn_richtig", "test", &http.Client{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { node.Close() })
	select {
	case panel = <-accepted:
	case <-time.After(5 * time.Second):
		t.Fatal("das Panel hat die Verbindung nicht angenommen")
	}
	t.Cleanup(func() { panel.Close() })
	return panel, node
}

func TestTunnelCarriesHTTP(t *testing.T) {
	panel, node := pair(t)
	server := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, "hallo "+r.URL.Path)
	})}
	go server.Serve(node)
	t.Cleanup(func() { server.Close() })

	client := &http.Client{Transport: &http.Transport{DialContext: func(context.Context, string, string) (net.Conn, error) { return panel.OpenStream() }}}
	for range 3 { // mehrere Anfragen, auch über wiederverwendete Ströme
		response, err := client.Get("http://node.link/api/system")
		if err != nil {
			t.Fatal(err)
		}
		data, _ := io.ReadAll(response.Body)
		response.Body.Close()
		if string(data) != "hallo /api/system" {
			t.Fatalf("Antwort %q", data)
		}
	}
}

func TestNodeCannotOpenStreams(t *testing.T) {
	_, node := pair(t)
	stream, err := node.OpenStream()
	if err != nil {
		t.Fatal(err)
	}
	_ = stream.SetReadDeadline(time.Now().Add(5 * time.Second))
	_, _ = stream.Write([]byte("GET /api/nodes HTTP/1.1\r\nHost: panel\r\n\r\n"))
	buffer := make([]byte, 16)
	if n, err := stream.Read(buffer); err == nil || n > 0 {
		t.Fatalf("der Node bekam eine Antwort vom Panel: %q %v", buffer[:n], err)
	}
	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		t.Fatal("der Strom wurde nicht geschlossen")
	}
}

func TestDialRejected(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = io.WriteString(w, `{"error":"unbekannter Node"}`)
	}))
	defer server.Close()
	_, connect, _, _ := Endpoints(server.URL)
	_, err := Dial(context.Background(), connect, "0123456789ab", "ldn_falsch", "test", &http.Client{})
	var rejected *RejectedError
	if !errors.As(err, &rejected) || rejected.Status != http.StatusUnauthorized || rejected.Message != "unbekannter Node" {
		t.Fatalf("erwartet abgelehnt, bekam %v", err)
	}
}

func TestOversizedMessageClosesTunnel(t *testing.T) {
	accepted := make(chan *yamux.Session, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		session, err := Accept(w, r)
		if err != nil {
			return
		}
		accepted <- session
		<-session.CloseChan()
	}))
	defer server.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	conn, _, err := websocket.Dial(ctx, server.URL, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.CloseNow()
	session := <-accepted
	_ = conn.Write(ctx, websocket.MessageBinary, make([]byte, ReadLimit+1))
	select {
	case <-session.CloseChan():
	case <-time.After(5 * time.Second):
		t.Fatal("eine zu große Nachricht hat die Verbindung nicht beendet")
	}
}
