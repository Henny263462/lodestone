package node

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
)

// Ein Schlüssel aus dem playit-Dashboard darf Tunnel nur lesen (NotAllowedWithReadOnly). Tunnel anlegen darf nur der Schlüssel
// eines „self-managed“ Agenten. Den bekommt man wie bei der playit-CLI (`playit claim`): Der Node denkt sich einen Code aus,
// der Benutzer bestätigt ihn auf playit.gg, danach tauscht der Node den Code gegen den Schlüssel. Der Schlüssel erscheint nie
// in einer Antwort; er wird direkt gespeichert und der Agent damit gestartet.

const (
	claimURL     = "https://playit.gg/claim/"
	claimVersion = "Lodestone"

	// Zustände einer Verbindung mit playit.
	ClaimNone     = "none"     // es läuft keine Verbindung
	ClaimWaiting  = "waiting"  // der Benutzer muss den Link auf playit.gg bestätigen
	ClaimAccepted = "accepted" // der Schlüssel ist gespeichert, der Agent läuft damit
	ClaimRejected = "rejected" // der Benutzer hat auf playit.gg abgelehnt
	ClaimExpired  = "expired"  // der Code ist abgelaufen
)

var (
	errClaimExpired  = errors.New("der Code ist abgelaufen")
	errClaimRejected = errors.New("die Verbindung wurde auf playit.gg abgelehnt")
	errClaimPending  = errors.New("der Code ist noch nicht bestätigt")
)

// PlayitClaim beschreibt den Stand einer Verbindung mit playit.
type PlayitClaim struct {
	State string `json:"state"`
	// URL ist der Link, den der Benutzer auf playit.gg bestätigt (nur solange State „waiting“ ist).
	URL string `json:"url,omitempty"`
}

// StartPlayitClaim beginnt eine Verbindung mit playit und gibt den Link zurück, den der Benutzer bestätigen muss.
// Eine noch offene frühere Verbindung wird dabei ersetzt.
func (m *Manager) StartPlayitClaim(ctx context.Context) (PlayitClaim, error) {
	buffer := make([]byte, 5)
	if _, err := rand.Read(buffer); err != nil {
		return PlayitClaim{}, err
	}
	code := hex.EncodeToString(buffer)

	m.playit.claimMu.Lock()
	defer m.playit.claimMu.Unlock()
	m.playit.claimCode = ""
	if _, err := m.claimSetup(ctx, code); err != nil {
		if errors.Is(err, errClaimExpired) {
			err = fmt.Errorf("%w: playit hat den Code nicht angenommen. Versuche es noch einmal", errUpstream)
		}
		return PlayitClaim{}, err
	}
	m.playit.claimCode = code
	return PlayitClaim{State: ClaimWaiting, URL: claimURL + code}, nil
}

// PollPlayitClaim fragt playit, ob der Benutzer bestätigt hat. Ist das geschehen, wird der Schlüssel getauscht, gespeichert
// und der Agent damit (neu) gestartet.
func (m *Manager) PollPlayitClaim(ctx context.Context) (PlayitClaim, error) {
	m.playit.claimMu.Lock()
	defer m.playit.claimMu.Unlock()
	code := m.playit.claimCode
	if code == "" {
		return PlayitClaim{State: ClaimNone}, nil
	}
	waiting := PlayitClaim{State: ClaimWaiting, URL: claimURL + code}
	finish := func(state string) PlayitClaim {
		m.playit.claimCode = ""
		return PlayitClaim{State: state}
	}

	state, err := m.claimSetup(ctx, code)
	switch {
	case errors.Is(err, errClaimExpired):
		return finish(ClaimExpired), nil
	case err != nil:
		return PlayitClaim{}, err
	}
	switch state {
	case "UserRejected":
		return finish(ClaimRejected), nil
	case "UserAccepted":
	default:
		return waiting, nil
	}

	data, err := m.playitCall(ctx, "", "/claim/exchange", map[string]string{"code": code})
	switch {
	case errors.Is(err, errClaimPending):
		return waiting, nil
	case errors.Is(err, errClaimExpired):
		return finish(ClaimExpired), nil
	case errors.Is(err, errClaimRejected):
		return finish(ClaimRejected), nil
	case err != nil:
		return PlayitClaim{}, err
	}
	var exchanged struct {
		SecretKey string `json:"secret_key"`
	}
	if json.Unmarshal(data, &exchanged) != nil || exchanged.SecretKey == "" {
		return PlayitClaim{}, fmt.Errorf("%w: der Schlüssel fehlt in der Antwort", errUpstream)
	}
	if err := m.ConfigurePlayit(ctx, exchanged.SecretKey); err != nil {
		return PlayitClaim{}, err
	}
	return finish(ClaimAccepted), nil
}

// claimSetup meldet den Code bei playit an und gibt den Stand zurück (WaitingForUserVisit, WaitingForUser,
// UserAccepted oder UserRejected).
func (m *Manager) claimSetup(ctx context.Context, code string) (string, error) {
	data, err := m.playitCall(ctx, "", "/claim/setup", map[string]string{
		"code": code, "agent_type": "self-managed", "version": claimVersion,
	})
	if err != nil {
		return "", err
	}
	var state string
	if json.Unmarshal(data, &state) != nil || state == "" {
		return "", fmt.Errorf("%w: der Stand der Verbindung fehlt in der Antwort", errUpstream)
	}
	return state, nil
}
