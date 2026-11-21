package node

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"log/slog"
	"math/rand/v2"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/lodestone-mc/lodestone/internal/nodelink"
)

// LinkConfig ist die Einstellung der Verbindung, die der Node selbst zum Panel aufbaut.
type LinkConfig struct {
	// PanelURL ist die Adresse des Panels (LODESTONE_NODE_PANEL_URL), z. B. https://panel.example.org.
	PanelURL string
	// JoinToken ist das einmalige Token aus dem Panel (LODESTONE_NODE_JOIN_TOKEN). Es wird nur gebraucht, solange der
	// Node noch kein Geheimnis hat.
	JoinToken string
	// StatePath ist die Datei, in der ID und Geheimnis des Nodes liegen (im Datenordner, nur für den Besitzer lesbar).
	StatePath string
}

// LinkConfigFromEnv liest die Verbindung zum Panel aus der Umgebung. ok ist false, wenn keine Adresse gesetzt ist.
func LinkConfigFromEnv(cfg Config) (link LinkConfig, ok bool) {
	address := strings.TrimRight(strings.TrimSpace(os.Getenv("LODESTONE_NODE_PANEL_URL")), "/")
	if address == "" {
		return LinkConfig{}, false
	}
	return LinkConfig{
		PanelURL:  address,
		JoinToken: strings.TrimSpace(os.Getenv("LODESTONE_NODE_JOIN_TOKEN")),
		StatePath: filepath.Join(cfg.DataDir, "panel-link.json"),
	}, true
}

// linkState ist der Inhalt von StatePath.
type linkState struct {
	PanelURL string `json:"panelUrl"`
	NodeID   string `json:"nodeId"`
	Secret   string `json:"secret"`
}

var linkStateID = regexp.MustCompile(`^[0-9a-f]{12}$`)

// Link hält die Verbindung zum Panel und bedient darüber die Schnittstelle des Daemons.
type Link struct {
	cfg        LinkConfig
	join       string
	connect    string
	secure     bool
	handler    http.Handler
	log        *slog.Logger
	version    string
	httpClient *http.Client
	// minBackoff und maxBackoff begrenzen die Wartezeit zwischen Verbindungsversuchen, rejectedWait gilt, wenn das
	// Panel die Anmeldung ablehnt (lässt sich in Tests verkürzen).
	minBackoff, maxBackoff, rejectedWait time.Duration
}

// NewLink baut die Verbindung. api ist die Schnittstelle des Daemons, token ihr Schlüssel: Anfragen über die
// Verbindung gelten als angemeldet, weil sich das Panel über die Verbindung selbst ausweist.
func NewLink(cfg LinkConfig, token string, api http.Handler, logger *slog.Logger, version string) (*Link, error) {
	join, connect, secure, err := nodelink.Endpoints(cfg.PanelURL)
	if err != nil {
		return nil, fmt.Errorf("LODESTONE_NODE_PANEL_URL: %w", err)
	}
	if version == "" {
		version = "dev"
	}
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.Header.Set("Authorization", "Bearer "+token)
		api.ServeHTTP(w, r)
	})
	return &Link{
		cfg: cfg, join: join, connect: connect, secure: secure, handler: handler, log: logger, version: version,
		httpClient: &http.Client{Timeout: 20 * time.Second},
		minBackoff: time.Second, maxBackoff: time.Minute, rejectedWait: 5 * time.Minute,
	}, nil
}

// Run hält die Verbindung, bis ctx endet. Bricht sie ab, verbindet sich der Node mit wachsendem Abstand neu.
func (l *Link) Run(ctx context.Context) {
	if !l.secure {
		l.log.Warn("Die Verbindung zum Panel ist unverschlüsselt (http/ws). Nutze https://, sonst können Dritte das Geheimnis des Nodes mitlesen.")
	}
	backoff := l.minBackoff
	for {
		started := time.Now()
		err := l.runOnce(ctx)
		if ctx.Err() != nil {
			return
		}
		if time.Since(started) > time.Minute {
			backoff = l.minBackoff // die Verbindung stand eine Weile, also neu anfangen
		}
		wait := backoff/2 + rand.N(backoff/2+1)
		var rejected *nodelink.RejectedError
		if errors.As(err, &rejected) {
			wait = l.rejectedWait
			l.log.Error("Das Panel lehnt den Node ab", "error", err, "retryIn", wait.String())
		} else {
			l.log.Warn("Keine Verbindung zum Panel", "error", err, "retryIn", wait.Round(time.Second).String())
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(wait):
		}
		backoff = min(backoff*2, l.maxBackoff)
	}
}

// runOnce koppelt den Node bei Bedarf, baut die Verbindung auf und bedient sie, bis sie endet.
func (l *Link) runOnce(ctx context.Context) error {
	state, err := l.credentials(ctx)
	if err != nil {
		return err
	}
	dialCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
	session, err := nodelink.Dial(dialCtx, l.connect, state.NodeID, state.Secret, l.version, &http.Client{})
	cancel()
	var rejected *nodelink.RejectedError
	if errors.As(err, &rejected) && rejected.Status == http.StatusUnauthorized && l.cfg.JoinToken != "" {
		// Das Geheimnis gilt nicht mehr (Node im Panel gelöscht oder neu gekoppelt): mit dem Join-Token neu koppeln
		_ = os.Remove(l.cfg.StatePath)
		return err
	}
	if err != nil {
		return err
	}
	l.log.Info("Mit dem Panel verbunden", "panel", l.cfg.PanelURL)
	server := &http.Server{
		Handler:           l.handler,
		ReadHeaderTimeout: 15 * time.Second,
		MaxHeaderBytes:    64 << 10,
		ErrorLog:          log.New(io.Discard, "", 0),
	}
	stop := context.AfterFunc(ctx, func() { session.Close() })
	defer stop()
	err = server.Serve(session)
	session.Close()
	server.Close()
	if ctx.Err() != nil {
		return ctx.Err()
	}
	return fmt.Errorf("Verbindung zum Panel getrennt: %w", err)
}

// credentials liefert ID und Geheimnis. Fehlen sie, koppelt sich der Node mit dem Join-Token und speichert sie.
func (l *Link) credentials(ctx context.Context) (linkState, error) {
	if data, err := os.ReadFile(l.cfg.StatePath); err == nil {
		var state linkState
		if json.Unmarshal(data, &state) == nil && state.PanelURL == l.cfg.PanelURL && linkStateID.MatchString(state.NodeID) && strings.HasPrefix(state.Secret, nodelink.SecretPrefix) {
			return state, nil
		}
	}
	if l.cfg.JoinToken == "" {
		return linkState{}, &nodelink.RejectedError{Status: http.StatusUnauthorized, Message: "der Node ist noch nicht gekoppelt – setze LODESTONE_NODE_JOIN_TOKEN (Panel → Nodes → Node hinzufügen)"}
	}
	body, _ := json.Marshal(nodelink.JoinRequest{Token: l.cfg.JoinToken, Version: l.version})
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, l.join, bytes.NewReader(body))
	if err != nil {
		return linkState{}, err
	}
	request.Header.Set("Content-Type", "application/json")
	response, err := l.httpClient.Do(request)
	if err != nil {
		return linkState{}, fmt.Errorf("Panel nicht erreichbar: %w", err)
	}
	defer response.Body.Close()
	data, _ := io.ReadAll(io.LimitReader(response.Body, 16<<10))
	if response.StatusCode != http.StatusOK {
		var failure struct {
			Error string `json:"error"`
		}
		_ = json.Unmarshal(data, &failure)
		if response.StatusCode >= 400 && response.StatusCode < 500 && response.StatusCode != http.StatusTooManyRequests {
			return linkState{}, &nodelink.RejectedError{Status: response.StatusCode, Message: failure.Error}
		}
		return linkState{}, fmt.Errorf("Panel antwortet beim Koppeln mit %d: %s", response.StatusCode, failure.Error)
	}
	var answer nodelink.JoinResponse
	if err := json.Unmarshal(data, &answer); err != nil || !linkStateID.MatchString(answer.NodeID) || !strings.HasPrefix(answer.Secret, nodelink.SecretPrefix) {
		return linkState{}, errors.New("unlesbare Antwort des Panels beim Koppeln")
	}
	state := linkState{PanelURL: l.cfg.PanelURL, NodeID: answer.NodeID, Secret: answer.Secret}
	encoded, _ := json.MarshalIndent(state, "", "  ")
	if err := os.MkdirAll(filepath.Dir(l.cfg.StatePath), 0o700); err != nil {
		return linkState{}, err
	}
	if err := os.WriteFile(l.cfg.StatePath, append(encoded, '\n'), 0o600); err != nil {
		return linkState{}, fmt.Errorf("Geheimnis des Nodes speichern: %w", err)
	}
	l.log.Info("Node mit dem Panel gekoppelt", "nodeId", answer.NodeID)
	return state, nil
}
