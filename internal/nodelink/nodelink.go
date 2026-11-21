// Package nodelink ist die Verbindung, die ein Node selbst zum Panel aufbaut. Sie besteht aus einem einzigen Websocket,
// über den yamux beliebig viele Datenströme bündelt. Das Panel öffnet auf jedem Strom eine gewöhnliche HTTP-Verbindung
// zum Node; der Node bedient sie mit seinem unveränderten http.Handler. So laufen alle Anfragen, Datei-Downloads und
// -Uploads (als Strom) und die Websocket-Konsole über diese eine Verbindung, und der Node braucht keine offenen Ports.
//
// Das Paket kennt weder Benutzer noch Docker; es enthält nur das Protokoll, das Panel und Node gemeinsam sprechen.
package nodelink

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/coder/websocket"
	"github.com/hashicorp/yamux"
)

const (
	// PathJoin ist die Adresse im Panel, an der ein Node sein einmaliges Join-Token gegen ein Geheimnis tauscht.
	PathJoin = "/api/node-link/join"
	// PathConnect ist die Adresse im Panel, an der ein Node die dauerhafte Verbindung aufbaut.
	PathConnect = "/api/node-link/connect"
	// HeaderNode trägt beim Verbinden die ID des Nodes; das Geheimnis steht in Authorization: Bearer.
	HeaderNode = "X-Lodestone-Node"
	// HeaderVersion trägt die Version des Daemons (nur zur Anzeige).
	HeaderVersion = "X-Lodestone-Node-Version"
	// JoinPrefix steht vor jedem Join-Token, SecretPrefix vor jedem Node-Geheimnis.
	JoinPrefix   = "ldj_"
	SecretPrefix = "ldn_"
	// ReadLimit ist die größte Websocket-Nachricht, die eine Seite annimmt. yamux schickt höchstens ein Stream-Fenster
	// (256 KiB) am Stück, alles darüber ist ein Angriff oder ein Fehler und beendet die Verbindung.
	ReadLimit = 1 << 20
)

// JoinRequest schickt der Node beim Koppeln an PathJoin.
type JoinRequest struct {
	Token   string `json:"token"`
	Version string `json:"version"`
}

// JoinResponse ist die Antwort des Panels: die ID des Nodes und sein dauerhaftes Geheimnis (nur jetzt im Klartext).
type JoinResponse struct {
	NodeID string `json:"nodeId"`
	Secret string `json:"secret"`
}

// RejectedError meldet, dass das Panel die Anmeldung abgelehnt hat (falsches Token oder Geheimnis, Node gelöscht).
type RejectedError struct {
	Status  int
	Message string
}

func (e *RejectedError) Error() string {
	if e.Message != "" {
		return fmt.Sprintf("das Panel lehnt die Anmeldung ab (%d): %s", e.Status, e.Message)
	}
	return fmt.Sprintf("das Panel lehnt die Anmeldung ab (%d)", e.Status)
}

// muxConfig ist die gemeinsame yamux-Einstellung beider Seiten.
func muxConfig() *yamux.Config {
	config := yamux.DefaultConfig()
	config.AcceptBacklog = 128 // Ströme, die auf Annahme warten; mehr werden abgewiesen
	config.KeepAliveInterval = 20 * time.Second
	config.ConnectionWriteTimeout = 15 * time.Second
	config.StreamOpenTimeout = 30 * time.Second
	config.LogOutput = io.Discard
	return config
}

// Endpoints leitet aus der Adresse des Panels (http, https, ws oder wss) die Adressen zum Koppeln und Verbinden ab.
// secure meldet, ob die Verbindung verschlüsselt ist.
func Endpoints(panelURL string) (join, connect string, secure bool, err error) {
	parsed, err := url.Parse(strings.TrimSpace(panelURL))
	if err != nil || parsed.Host == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return "", "", false, errors.New("die Adresse des Panels ist ungültig, erwartet wird z. B. https://panel.example.org")
	}
	var httpScheme, wsScheme string
	switch strings.ToLower(parsed.Scheme) {
	case "https", "wss":
		httpScheme, wsScheme, secure = "https", "wss", true
	case "http", "ws":
		httpScheme, wsScheme = "http", "ws"
	default:
		return "", "", false, errors.New("die Adresse des Panels muss mit https:// (empfohlen) oder http:// beginnen")
	}
	base := strings.TrimRight(parsed.Host+parsed.EscapedPath(), "/")
	return httpScheme + "://" + base + PathJoin, wsScheme + "://" + base + PathConnect, secure, nil
}

// Accept nimmt auf Seite des Panels die Verbindung eines Nodes an. Der Aufrufer hat den Node vorher angemeldet. Der
// Node darf über die Verbindung selbst keine Anfragen stellen: Datenströme, die er öffnet, werden sofort geschlossen.
func Accept(w http.ResponseWriter, r *http.Request) (*yamux.Session, error) {
	conn, err := websocket.Accept(w, r, nil)
	if err != nil {
		return nil, err
	}
	// Der Kontext begrenzt die Lebensdauer der Verbindung; sie endet mit Close der Sitzung, nicht mit der Anfrage
	session, err := yamux.Client(wrap(conn), muxConfig())
	if err != nil {
		conn.CloseNow()
		return nil, err
	}
	go func() {
		for {
			stream, err := session.AcceptStream()
			if err != nil {
				return
			}
			stream.Close()
		}
	}()
	return session, nil
}

// Dial baut auf Seite des Nodes die Verbindung zum Panel auf. ctx begrenzt nur den Aufbau. Lehnt das Panel die
// Anmeldung ab, ist der Fehler ein *RejectedError.
func Dial(ctx context.Context, connectURL, nodeID, secret, version string, client *http.Client) (*yamux.Session, error) {
	header := http.Header{}
	header.Set("Authorization", "Bearer "+secret)
	header.Set(HeaderNode, nodeID)
	header.Set(HeaderVersion, version)
	conn, response, err := websocket.Dial(ctx, connectURL, &websocket.DialOptions{HTTPHeader: header, HTTPClient: client})
	if err != nil {
		if response != nil && response.StatusCode >= 400 && response.StatusCode < 500 && response.StatusCode != http.StatusTooManyRequests {
			return nil, &RejectedError{Status: response.StatusCode, Message: readError(response)}
		}
		return nil, err
	}
	session, err := yamux.Server(wrap(conn), muxConfig())
	if err != nil {
		conn.CloseNow()
		return nil, err
	}
	return session, nil
}

// wrap macht aus dem Websocket eine net.Conn für yamux. NetConn schaltet das Leselimit ab; es wird deshalb danach
// gesetzt und gilt für jede einzelne Nachricht.
func wrap(conn *websocket.Conn) net.Conn {
	netConn := websocket.NetConn(context.Background(), conn, websocket.MessageBinary)
	conn.SetReadLimit(ReadLimit)
	return netConn
}

// readError liest die Fehlermeldung einer JSON-Antwort des Panels (kurz und begrenzt).
func readError(response *http.Response) string {
	if response.Body == nil {
		return ""
	}
	var failure struct {
		Error string `json:"error"`
	}
	_ = json.NewDecoder(io.LimitReader(response.Body, 4<<10)).Decode(&failure)
	return strings.TrimSpace(failure.Error)
}
