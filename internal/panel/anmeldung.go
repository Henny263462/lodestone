package panel

import (
	"context"
	"crypto/subtle"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"github.com/go-webauthn/webauthn/protocol"
	"github.com/go-webauthn/webauthn/webauthn"
	"golang.org/x/oauth2"
)

// SSOConfig ist die Einstellung der Anmeldung über OIDC und Passkeys (Umgebungsvariablen LODESTONE_OIDC_*,
// LODESTONE_PUBLIC_URL und LODESTONE_WEBAUTHN_*).
type SSOConfig struct {
	// PublicURL ist die Adresse, unter der Benutzer das Panel im Browser öffnen (https://panel.example.org).
	// Daraus folgen die Rücksprungadresse für OIDC und RP-ID/Herkunft für Passkeys.
	PublicURL string

	OIDCIssuer       string
	OIDCClientID     string
	OIDCClientSecret string
	// OIDCRedirectURL ist die Rücksprungadresse; leer = PublicURL + /api/auth/oidc/callback.
	OIDCRedirectURL string
	// OIDCName steht auf dem Knopf („Mit Authentik anmelden“).
	OIDCName   string
	OIDCScopes []string
	// OIDCAutoCreate legt unbekannte Konten beim ersten Anmelden an; sonst kommen nur verknüpfte Konten herein.
	OIDCAutoCreate bool
	// OIDCAdminGroup macht Mitglieder dieser Gruppe (Claim OIDCGroupsClaim, Standard „groups“) zu Administratoren.
	OIDCAdminGroup  string
	OIDCGroupsClaim string
	// OIDCOnly schaltet die Anmeldung mit Passwort ab (Notfallzugang: Variable entfernen und neu starten).
	OIDCOnly bool

	// PasskeyRPID ist die RP-ID der Passkeys; leer = Hostname aus PublicURL.
	PasskeyRPID string
	// PasskeyOrigins sind weitere erlaubte Herkünfte neben der aus PublicURL.
	PasskeyOrigins []string
}

const (
	oidcFlowCookie  = "lodestone_oidc"
	oidcFlowLife    = 10 * time.Minute
	ceremonyLife    = 5 * time.Minute
	maxPendingFlows = 2000
	maxFlowsPerIP   = 20
	oidcTimeout     = 10 * time.Second
)

// ssoState hält alles, was die Anmeldung über OIDC und Passkeys zwischen zwei Anfragen braucht.
type ssoState struct {
	cfg SSOConfig
	log *slog.Logger

	// OIDC (leer, wenn nicht eingerichtet)
	oidcEnabled bool
	issuer      string
	redirectURL string
	name        string
	scopes      []string
	httpClient  *http.Client

	providerMu sync.Mutex
	provider   *oidc.Provider

	// Passkeys (nil, wenn nicht eingerichtet)
	webauthn *webauthn.WebAuthn

	mu         sync.Mutex
	oidcFlows  map[string]oidcFlow
	ceremonies map[string]ceremony
}

// oidcFlow ist eine begonnene Anmeldung beim Anbieter, erkennbar am state.
type oidcFlow struct {
	binding  string // Hash des Werts im Flow-Cookie: Der Rücksprung muss aus demselben Browser kommen
	verifier string // PKCE
	nonce    string
	mode     string // "login" oder "link"
	userID   string // bei "link": wer verknüpft
	next     string
	ip       string
	expires  time.Time
}

// ceremony ist eine begonnene WebAuthn-Zeremonie (Registrieren, Anmelden, zweiter Faktor).
type ceremony struct {
	kind    string
	userID  string
	session webauthn.SessionData
	ip      string
	expires time.Time
}

func newSSOState(cfg SSOConfig, logger *slog.Logger) *ssoState {
	state := &ssoState{
		cfg: cfg, log: logger, oidcFlows: map[string]oidcFlow{}, ceremonies: map[string]ceremony{},
		httpClient: &http.Client{Timeout: oidcTimeout},
	}
	public := strings.TrimRight(cfg.PublicURL, "/")
	var publicURL *url.URL
	if public != "" {
		parsed, err := url.Parse(public)
		if err != nil || (parsed.Scheme != "https" && parsed.Scheme != "http") || parsed.Host == "" {
			logger.Warn("LODESTONE_PUBLIC_URL ist keine gültige Adresse (z. B. https://panel.example.org) – OIDC-Rücksprung und Passkeys bleiben aus", "value", cfg.PublicURL)
		} else {
			publicURL = parsed
		}
	}

	if cfg.OIDCIssuer != "" || cfg.OIDCClientID != "" {
		state.redirectURL = cfg.OIDCRedirectURL
		if state.redirectURL == "" && publicURL != nil {
			state.redirectURL = publicURL.Scheme + "://" + publicURL.Host + "/api/auth/oidc/callback"
		}
		switch {
		case cfg.OIDCIssuer == "" || cfg.OIDCClientID == "":
			logger.Warn("OIDC ist unvollständig eingerichtet: LODESTONE_OIDC_ISSUER und LODESTONE_OIDC_CLIENT_ID werden gebraucht")
		case state.redirectURL == "":
			logger.Warn("OIDC braucht LODESTONE_PUBLIC_URL oder LODESTONE_OIDC_REDIRECT_URL für den Rücksprung")
		default:
			state.oidcEnabled = true
			state.issuer = cfg.OIDCIssuer
			state.name = cfg.OIDCName
			if state.name == "" {
				state.name = "SSO"
			}
			state.scopes = cfg.OIDCScopes
			if len(state.scopes) == 0 {
				state.scopes = []string{"profile", "email"}
			}
			if !containsString(state.scopes, oidc.ScopeOpenID) {
				state.scopes = append([]string{oidc.ScopeOpenID}, state.scopes...)
			}
		}
	}
	if cfg.OIDCOnly && !state.oidcEnabled {
		logger.Warn("LODESTONE_OIDC_ONLY wird ignoriert, weil OIDC nicht eingerichtet ist – sonst käme niemand mehr herein")
	}

	if publicURL != nil {
		rpID := cfg.PasskeyRPID
		if rpID == "" {
			rpID = publicURL.Hostname()
		}
		origins := append([]string{publicURL.Scheme + "://" + publicURL.Host}, cfg.PasskeyOrigins...)
		instance, err := webauthn.New(&webauthn.Config{
			RPID: rpID, RPDisplayName: "Lodestone", RPOrigins: origins,
			AttestationPreference: protocol.PreferNoAttestation,
			Timeouts: webauthn.TimeoutsConfig{
				Login:        webauthn.TimeoutConfig{Enforce: true, Timeout: ceremonyLife, TimeoutUVD: ceremonyLife},
				Registration: webauthn.TimeoutConfig{Enforce: true, Timeout: ceremonyLife, TimeoutUVD: ceremonyLife},
			},
		})
		if err != nil {
			logger.Warn("Passkeys bleiben aus: RP-ID oder Herkunft ist ungültig", "rpId", rpID, "error", err)
		} else {
			state.webauthn = instance
		}
	}
	return state
}

// oidcOnly sagt, ob die Anmeldung mit Passwort abgeschaltet ist.
func (s *ssoState) oidcOnly() bool { return s != nil && s.cfg.OIDCOnly && s.oidcEnabled }

func (s *ssoState) passkeysEnabled() bool { return s != nil && s.webauthn != nil }

// oidcContext hängt den HTTP-Client mit Zeitlimit an, den go-oidc und oauth2 benutzen.
func (s *ssoState) oidcContext(ctx context.Context) context.Context {
	return oidc.ClientContext(ctx, s.httpClient)
}

// oidcProvider liest die Discovery des Anbieters beim ersten Bedarf. Ein Fehler wird nicht gemerkt, damit das Panel
// auch startet, wenn der Anbieter gerade nicht erreichbar ist.
func (s *ssoState) oidcProvider(ctx context.Context) (*oidc.Provider, error) {
	s.providerMu.Lock()
	defer s.providerMu.Unlock()
	if s.provider != nil {
		return s.provider, nil
	}
	ctx, cancel := context.WithTimeout(ctx, oidcTimeout)
	defer cancel()
	provider, err := oidc.NewProvider(s.oidcContext(ctx), s.issuer)
	if err != nil {
		return nil, err
	}
	s.provider = provider
	return provider, nil
}

func (s *ssoState) oauthConfig(provider *oidc.Provider) *oauth2.Config {
	return &oauth2.Config{
		ClientID: s.cfg.OIDCClientID, ClientSecret: s.cfg.OIDCClientSecret, RedirectURL: s.redirectURL,
		Endpoint: provider.Endpoint(), Scopes: s.scopes,
	}
}

// pruneLocked entfernt abgelaufene Abläufe; s.mu muss gehalten sein.
func (s *ssoState) pruneLocked(now time.Time) {
	for key, flow := range s.oidcFlows {
		if now.After(flow.expires) {
			delete(s.oidcFlows, key)
		}
	}
	for key, entry := range s.ceremonies {
		if now.After(entry.expires) {
			delete(s.ceremonies, key)
		}
	}
}

var errTooManyFlows = fail(http.StatusTooManyRequests, "gerade laufen zu viele Anmeldungen – versuch es gleich noch einmal")

func (s *ssoState) putOIDCFlow(state string, flow oidcFlow) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.pruneLocked(time.Now())
	perIP := 0
	for _, other := range s.oidcFlows {
		if other.ip == flow.ip {
			perIP++
		}
	}
	if len(s.oidcFlows) >= maxPendingFlows || perIP >= maxFlowsPerIP {
		return errTooManyFlows
	}
	s.oidcFlows[state] = flow
	return nil
}

// takeOIDCFlow holt einen Ablauf und entfernt ihn (jeder state gilt genau einmal).
func (s *ssoState) takeOIDCFlow(state, cookieValue string) (oidcFlow, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	flow, ok := s.oidcFlows[state]
	if !ok {
		return oidcFlow{}, false
	}
	delete(s.oidcFlows, state)
	if time.Now().After(flow.expires) || subtle.ConstantTimeCompare([]byte(flow.binding), []byte(hashToken(cookieValue))) != 1 {
		return oidcFlow{}, false
	}
	return flow, true
}

func (s *ssoState) putCeremony(entry ceremony) (string, error) {
	id, err := randomToken()
	if err != nil {
		return "", err
	}
	entry.expires = time.Now().Add(ceremonyLife)
	s.mu.Lock()
	defer s.mu.Unlock()
	s.pruneLocked(time.Now())
	perIP := 0
	for _, other := range s.ceremonies {
		if other.ip == entry.ip {
			perIP++
		}
	}
	// Eine Adresse allein kann den Speicher nicht füllen und so die Anmeldung für alle blockieren
	if len(s.ceremonies) >= maxPendingFlows || perIP >= maxFlowsPerIP {
		return "", errTooManyFlows
	}
	s.ceremonies[id] = entry
	return id, nil
}

// takeCeremony holt eine Zeremonie der passenden Art und entfernt sie (jede Herausforderung gilt einmal).
func (s *ssoState) takeCeremony(id, kind, userID string) (ceremony, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	entry, ok := s.ceremonies[id]
	if !ok {
		return ceremony{}, false
	}
	delete(s.ceremonies, id)
	if entry.kind != kind || entry.userID != userID || time.Now().After(entry.expires) {
		return ceremony{}, false
	}
	return entry, true
}

// ---------------------------------------------------------------- Benutzer für die WebAuthn-Bibliothek

// passkeyUser verbindet einen Benutzer mit seinen gespeicherten Passkeys.
type passkeyUser struct {
	user        User
	credentials []webauthn.Credential
}

func (u passkeyUser) WebAuthnID() []byte                         { return []byte(u.user.ID) }
func (u passkeyUser) WebAuthnName() string                       { return u.user.Username }
func (u passkeyUser) WebAuthnDisplayName() string                { return u.user.Username }
func (u passkeyUser) WebAuthnCredentials() []webauthn.Credential { return u.credentials }

// loadPasskeyUser lädt einen Benutzer samt seiner Passkeys.
func (p *Panel) loadPasskeyUser(user User) (passkeyUser, error) {
	keys, err := p.store.ListPasskeys(user.ID)
	if err != nil {
		return passkeyUser{}, err
	}
	result := passkeyUser{user: user}
	for _, key := range keys {
		var credential webauthn.Credential
		if err := credential.UnmarshalJSON([]byte(key.Data)); err != nil {
			p.log.Warn("Passkey nicht lesbar", "passkey", key.ID, "error", err)
			continue
		}
		result.credentials = append(result.credentials, credential)
	}
	return result, nil
}

// secondFactors nennt die zweiten Faktoren, die ein Benutzer nach dem Passwort vorweisen kann.
func (p *Panel) secondFactors(userID string) []string {
	methods := []string{}
	if _, enabled, _, _ := p.store.TotpState(userID); enabled {
		methods = append(methods, "totp")
	}
	if p.sso.passkeysEnabled() {
		if count, _ := p.store.CountPasskeys(userID); count > 0 {
			methods = append(methods, "passkey")
		}
	}
	return methods
}

// ssoStatus ergänzt /api/auth/status um die Anmeldewege (ohne Geheimnisse).
func (p *Panel) ssoStatus(result map[string]any) {
	result["passwordLogin"] = !p.sso.oidcOnly()
	result["passkeys"] = p.sso.passkeysEnabled()
	if p.sso != nil && p.sso.oidcEnabled {
		result["oidc"] = map[string]string{"name": p.sso.name}
	}
}

// errPasswordLoginOff kommt, wenn LODESTONE_OIDC_ONLY gesetzt ist.
func (p *Panel) errPasswordLoginOff() error {
	return fail(http.StatusForbidden, fmt.Sprintf("die Anmeldung mit Passwort ist abgeschaltet – melde dich über %s an", p.sso.name))
}

func containsString(list []string, value string) bool {
	for _, entry := range list {
		if entry == value {
			return true
		}
	}
	return false
}

var errPasskeysOff = fail(http.StatusNotFound, "Passkeys sind nicht eingerichtet – der Administrator muss LODESTONE_PUBLIC_URL setzen")
var errOIDCOff = fail(http.StatusNotFound, "die Anmeldung über OIDC ist nicht eingerichtet")

// requirePasskeys prüft, dass Passkeys eingerichtet sind.
func (p *Panel) requirePasskeys() error {
	if !p.sso.passkeysEnabled() {
		return errPasskeysOff
	}
	return nil
}

func (p *Panel) requireOIDC() error {
	if p.sso == nil || !p.sso.oidcEnabled {
		return errOIDCOff
	}
	return nil
}

// registerAnmeldungRoutes hängt die Routen für OIDC und Passkeys ein. Konto-Routen laufen über sessionOnly:
// API-Schlüssel dürfen an Anmeldewegen nichts ändern.
func (p *Panel) registerAnmeldungRoutes(mux *http.ServeMux) {
	if p.sso == nil {
		p.sso = newSSOState(p.cfg.SSO, p.log)
	}
	// öffentlich (ohne Anmeldung), gedrosselt
	mux.HandleFunc("GET /api/auth/oidc/start", p.oidcStart)
	mux.HandleFunc("GET /api/auth/oidc/callback", p.oidcCallback)
	mux.HandleFunc("POST /api/auth/passkey/begin", p.passkeyLoginBegin)
	mux.HandleFunc("POST /api/auth/passkey/finish", p.passkeyLoginFinish)
	mux.HandleFunc("POST /api/auth/passkey/second/begin", p.passkeySecondBegin)
	mux.HandleFunc("POST /api/auth/passkey/second/finish", p.passkeySecondFinish)

	// eigenes Konto
	mux.HandleFunc("GET /api/me/passkeys", p.authenticate(p.sessionOnly(p.listPasskeys)))
	mux.HandleFunc("POST /api/me/passkeys/begin", p.authenticate(p.sessionOnly(p.passkeyRegisterBegin)))
	mux.HandleFunc("POST /api/me/passkeys/finish", p.authenticate(p.sessionOnly(p.passkeyRegisterFinish)))
	mux.HandleFunc("PATCH /api/me/passkeys/{id}", p.authenticate(p.sessionOnly(p.renamePasskey)))
	mux.HandleFunc("DELETE /api/me/passkeys/{id}", p.authenticate(p.sessionOnly(p.deletePasskey)))
	mux.HandleFunc("GET /api/me/oidc", p.authenticate(p.sessionOnly(p.oidcAccount)))
	mux.HandleFunc("POST /api/me/oidc/link", p.authenticate(p.sessionOnly(p.oidcLinkStart)))
	mux.HandleFunc("DELETE /api/me/oidc", p.authenticate(p.sessionOnly(p.oidcUnlink)))

	// Administrator: Passkeys eines Benutzers zurücksetzen (z. B. Gerät verloren)
	mux.HandleFunc("DELETE /api/users/{id}/passkeys", p.authenticate(p.sessionOnly(p.resetPasskeys)))
}
