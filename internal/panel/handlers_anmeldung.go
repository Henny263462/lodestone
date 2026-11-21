package panel

import (
	"bytes"
	"context"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/coreos/go-oidc/v3/oidc"
	"github.com/go-webauthn/webauthn/protocol"
	"github.com/go-webauthn/webauthn/webauthn"
	"golang.org/x/oauth2"
)

// ---------------------------------------------------------------- OIDC: Anmeldung und Verknüpfung

// oidcIdentity ist das, was Lodestone aus dem geprüften ID-Token übernimmt.
type oidcIdentity struct {
	Subject  string
	Email    string
	Username string
	Name     string
	Groups   []string
}

// safeNext lässt nur Ziele auf dem Panel selbst zu (kein //fremd.example, keine Steuerzeichen).
func safeNext(next string) string {
	if next == "" || len(next) > 512 || !strings.HasPrefix(next, "/") || strings.HasPrefix(next, "//") || strings.HasPrefix(next, "/\\") {
		return "/"
	}
	for _, r := range next {
		if r < 0x20 || r == 0x7f {
			return "/"
		}
	}
	return next
}

// oidcRedirectError schickt den Browser mit einem Fehlercode zurück; die Oberfläche übersetzt ihn in einen Satz.
func oidcRedirectError(w http.ResponseWriter, r *http.Request, mode, code string) {
	target := "/login/?error=" + url.QueryEscape(code)
	if mode == "link" {
		target = "/account/?oidc=" + url.QueryEscape(code)
	}
	http.Redirect(w, r, target, http.StatusFound)
}

func (p *Panel) setFlowCookie(w http.ResponseWriter, value string, maxAge int) {
	http.SetCookie(w, &http.Cookie{
		// Lax, weil der Rücksprung vom Anbieter eine seitenübergreifende Navigation ist; Strict käme dort nicht an.
		Name: oidcFlowCookie, Value: value, Path: "/api/auth/oidc/", MaxAge: maxAge,
		HttpOnly: true, Secure: p.cfg.SecureCookies, SameSite: http.SameSiteLaxMode,
	})
}

// beginOIDC legt einen Ablauf an (state, nonce, PKCE, Flow-Cookie) und liefert die Adresse beim Anbieter.
func (p *Panel) beginOIDC(w http.ResponseWriter, r *http.Request, mode, userID, next string) (string, error) {
	provider, err := p.sso.oidcProvider(r.Context())
	if err != nil {
		p.log.Warn("OIDC-Anbieter nicht erreichbar", "issuer", p.sso.issuer, "error", err)
		return "", fail(http.StatusBadGateway, "der Anmeldedienst ist gerade nicht erreichbar – versuch es später noch einmal")
	}
	state, err1 := randomToken()
	nonce, err2 := randomToken()
	binding, err3 := randomToken()
	if err := errors.Join(err1, err2, err3); err != nil {
		return "", err
	}
	verifier := oauth2.GenerateVerifier()
	flow := oidcFlow{binding: hashToken(binding), verifier: verifier, nonce: nonce, mode: mode, userID: userID, next: next, ip: p.clientIP(r), expires: time.Now().Add(oidcFlowLife)}
	if err := p.sso.putOIDCFlow(state, flow); err != nil {
		return "", err
	}
	p.setFlowCookie(w, binding, int(oidcFlowLife.Seconds()))
	return p.sso.oauthConfig(provider).AuthCodeURL(state, oauth2.S256ChallengeOption(verifier), oauth2.SetAuthURLParam("nonce", nonce)), nil
}

// oidcStart leitet zum Anbieter weiter (Knopf „Mit … anmelden“).
func (p *Panel) oidcStart(w http.ResponseWriter, r *http.Request) {
	if p.requireOIDC() != nil {
		oidcRedirectError(w, r, "login", "oidc_off")
		return
	}
	if p.throttle.blocked(p.clientIP(r) + "|oidc") {
		oidcRedirectError(w, r, "login", "throttled")
		return
	}
	target, err := p.beginOIDC(w, r, "login", "", safeNext(r.URL.Query().Get("next")))
	if err != nil {
		oidcRedirectError(w, r, "login", "oidc_unavailable")
		return
	}
	http.Redirect(w, r, target, http.StatusFound)
}

// oidcExchange tauscht den Code (mit PKCE) und prüft das ID-Token: Signatur, Aussteller, Empfänger, Ablauf, nonce.
func (p *Panel) oidcExchange(ctx context.Context, code string, flow oidcFlow) (oidcIdentity, error) {
	provider, err := p.sso.oidcProvider(ctx)
	if err != nil {
		return oidcIdentity{}, err
	}
	ctx, cancel := context.WithTimeout(p.sso.oidcContext(ctx), oidcTimeout)
	defer cancel()
	token, err := p.sso.oauthConfig(provider).Exchange(ctx, code, oauth2.VerifierOption(flow.verifier))
	if err != nil {
		return oidcIdentity{}, err
	}
	raw, ok := token.Extra("id_token").(string)
	if !ok || raw == "" {
		return oidcIdentity{}, errors.New("die Antwort enthält kein ID-Token")
	}
	idToken, err := provider.Verifier(&oidc.Config{ClientID: p.sso.cfg.OIDCClientID}).Verify(ctx, raw)
	if err != nil {
		return oidcIdentity{}, err
	}
	if subtle.ConstantTimeCompare([]byte(idToken.Nonce), []byte(flow.nonce)) != 1 {
		return oidcIdentity{}, errors.New("nonce stimmt nicht")
	}
	if idToken.AccessTokenHash != "" {
		if err := idToken.VerifyAccessToken(token.AccessToken); err != nil {
			return oidcIdentity{}, err
		}
	}
	var claims map[string]any
	if err := idToken.Claims(&claims); err != nil {
		return oidcIdentity{}, err
	}
	identity := oidcIdentity{Subject: idToken.Subject}
	identity.Email, _ = claims["email"].(string)
	identity.Username, _ = claims["preferred_username"].(string)
	identity.Name, _ = claims["name"].(string)
	claim := p.sso.cfg.OIDCGroupsClaim
	if claim == "" {
		claim = "groups"
	}
	switch groups := claims[claim].(type) {
	case []any:
		for _, group := range groups {
			if text, ok := group.(string); ok {
				identity.Groups = append(identity.Groups, text)
			}
		}
	case string:
		identity.Groups = strings.Fields(strings.ReplaceAll(groups, ",", " "))
	}
	if identity.Subject == "" || len(identity.Subject) > 255 {
		return oidcIdentity{}, errors.New("das ID-Token hat keinen gültigen sub-Claim")
	}
	return identity, nil
}

// oidcCallback ist der Rücksprung vom Anbieter.
func (p *Panel) oidcCallback(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query()
	cookieValue := ""
	if cookie, err := r.Cookie(oidcFlowCookie); err == nil {
		cookieValue = cookie.Value
	}
	p.setFlowCookie(w, "", -1)
	ip := p.clientIP(r)
	if p.requireOIDC() != nil {
		oidcRedirectError(w, r, "login", "oidc_off")
		return
	}
	if p.throttle.blocked(ip + "|oidc") {
		oidcRedirectError(w, r, "login", "throttled")
		return
	}
	state := query.Get("state")
	flow, ok := p.sso.takeOIDCFlow(state, cookieValue)
	if state == "" || cookieValue == "" || !ok {
		p.throttle.fail(ip + "|oidc")
		p.recordAudit("", "", "Anmeldung über OIDC abgelehnt: ungültiger state", "", http.StatusUnauthorized, ip)
		oidcRedirectError(w, r, "login", "oidc_state")
		return
	}
	if query.Get("error") != "" || query.Get("code") == "" {
		oidcRedirectError(w, r, flow.mode, "oidc_denied")
		return
	}
	identity, err := p.oidcExchange(r.Context(), query.Get("code"), flow)
	if err != nil {
		p.throttle.fail(ip + "|oidc")
		p.log.Warn("OIDC-Anmeldung abgelehnt", "error", err)
		p.recordAudit(flow.userID, "", "Anmeldung über OIDC abgelehnt: Token ungültig", "", http.StatusUnauthorized, ip)
		oidcRedirectError(w, r, flow.mode, "oidc_invalid")
		return
	}
	if flow.mode == "link" {
		p.finishOIDCLink(w, r, flow, identity)
		return
	}
	p.finishOIDCLogin(w, r, flow, identity)
}

func (p *Panel) finishOIDCLink(w http.ResponseWriter, r *http.Request, flow oidcFlow, identity oidcIdentity) {
	ip := p.clientIP(r)
	user, err := p.store.UserByID(flow.userID)
	if err != nil || user.Disabled {
		oidcRedirectError(w, r, "link", "oidc_failed")
		return
	}
	err = p.store.LinkOIDC(OIDCLink{Issuer: p.sso.issuer, Subject: identity.Subject, UserID: user.ID, Email: identity.Email})
	if errors.Is(err, ErrExists) {
		p.recordAudit(user.ID, user.Username, "OIDC-Verknüpfung abgelehnt: Konto gehört jemand anderem", "", http.StatusConflict, ip)
		oidcRedirectError(w, r, "link", "oidc_taken")
		return
	}
	if err != nil {
		p.log.Warn("OIDC-Verknüpfung nicht gespeichert", "error", err)
		oidcRedirectError(w, r, "link", "oidc_failed")
		return
	}
	p.recordAudit(user.ID, user.Username, "OIDC-Konto verknüpft", identity.Email, http.StatusOK, ip)
	http.Redirect(w, r, "/account/?oidc=linked", http.StatusFound)
}

func (p *Panel) finishOIDCLogin(w http.ResponseWriter, r *http.Request, flow oidcFlow, identity oidcIdentity) {
	ip := p.clientIP(r)
	shown := identity.Email
	if shown == "" {
		shown = identity.Username
	}
	link, err := p.store.OIDCLinkBySubject(p.sso.issuer, identity.Subject)
	var user User
	switch {
	case err == nil:
		user, err = p.store.UserByID(link.UserID)
	case errors.Is(err, ErrNotFound) && p.sso.cfg.OIDCAutoCreate:
		user, link, err = p.createOIDCUser(identity, ip)
	case errors.Is(err, ErrNotFound):
		p.recordAudit("", shown, "Anmeldung über OIDC abgelehnt: Konto nicht verknüpft", "", http.StatusUnauthorized, ip)
		oidcRedirectError(w, r, "login", "oidc_unlinked")
		return
	}
	if err != nil {
		p.log.Warn("OIDC-Anmeldung fehlgeschlagen", "error", err)
		oidcRedirectError(w, r, "login", "oidc_failed")
		return
	}
	if user.Disabled {
		p.recordAudit(user.ID, user.Username, "Anmeldung über OIDC abgelehnt: Benutzer gesperrt", "", http.StatusUnauthorized, ip)
		oidcRedirectError(w, r, "login", "disabled")
		return
	}
	user = p.syncOIDCAdmin(user, link, identity)
	p.store.TouchOIDCLink(p.sso.issuer, identity.Subject, identity.Email)
	p.throttle.reset(ip + "|oidc")
	if err := p.startSession(w, r, user); err != nil {
		oidcRedirectError(w, r, "login", "oidc_failed")
		return
	}
	p.recordAudit(user.ID, user.Username, "Anmeldung (OIDC)", "", http.StatusOK, ip)
	http.Redirect(w, r, safeNext(flow.next), http.StatusFound)
}

// syncOIDCAdmin vergibt die Administratorrechte nach der Gruppe beim Anbieter. Entzogen werden sie nur Konten, die
// über OIDC angelegt wurden, und nie dem letzten Administrator.
func (p *Panel) syncOIDCAdmin(user User, link OIDCLink, identity oidcIdentity) User {
	group := p.sso.cfg.OIDCAdminGroup
	if group == "" {
		return user
	}
	member := containsString(identity.Groups, group)
	switch {
	case member && !user.IsAdmin:
		if err := p.store.UpdateUser(user.ID, true, user.Disabled); err == nil {
			user.IsAdmin = true
			p.recordAudit(user.ID, user.Username, "Administratorrechte über OIDC-Gruppe erhalten", group, http.StatusOK, "")
		}
	case !member && user.IsAdmin && link.AutoCreated:
		if admins, err := p.store.CountAdmins(); err == nil && admins > 1 {
			if err := p.store.UpdateUser(user.ID, false, user.Disabled); err == nil {
				user.IsAdmin = false
				p.recordAudit(user.ID, user.Username, "Administratorrechte über OIDC-Gruppe entzogen", group, http.StatusOK, "")
			}
		}
	}
	return user
}

// oidcUsername bildet aus den Claims einen gültigen Benutzernamen (Buchstaben, Ziffern, _ - .; 3 bis 32 Zeichen).
func oidcUsername(identity oidcIdentity) string {
	local, _, _ := strings.Cut(identity.Email, "@")
	for _, candidate := range []string{identity.Username, local, identity.Name} {
		var builder strings.Builder
		for _, r := range candidate {
			switch {
			case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '_', r == '-', r == '.':
				builder.WriteRune(r)
			case r == ' ':
				builder.WriteRune('.')
			}
		}
		name := strings.Trim(builder.String(), ".-_")
		if len(name) > 28 {
			name = name[:28]
		}
		if len(name) >= 3 {
			return name
		}
	}
	return "benutzer"
}

// createOIDCUser legt beim ersten Anmelden einen Benutzer an (nur mit LODESTONE_OIDC_AUTO_CREATE). Er hat kein
// Passwort; ein Administrator kann ihm eines setzen.
func (p *Panel) createOIDCUser(identity oidcIdentity, ip string) (User, OIDCLink, error) {
	base := oidcUsername(identity)
	admin := p.sso.cfg.OIDCAdminGroup != "" && containsString(identity.Groups, p.sso.cfg.OIDCAdminGroup)
	var user User
	var err error
	for attempt := 1; attempt <= 50; attempt++ {
		name := base
		if attempt > 1 {
			name = base + "-" + strconv.Itoa(attempt)
		}
		user, err = p.store.CreateUser(name, "", admin)
		if !errors.Is(err, ErrExists) {
			break
		}
	}
	if err != nil {
		return User{}, OIDCLink{}, err
	}
	link := OIDCLink{Issuer: p.sso.issuer, Subject: identity.Subject, UserID: user.ID, Email: identity.Email, AutoCreated: true}
	if err := p.store.LinkOIDC(link); err != nil {
		// Gleichzeitige erste Anmeldung desselben Kontos: den doppelten Benutzer wieder entfernen
		_ = p.store.DeleteUser(user.ID)
		return User{}, OIDCLink{}, err
	}
	p.recordAudit(user.ID, user.Username, "Benutzer über OIDC angelegt", identity.Email, http.StatusCreated, ip)
	return user, link, nil
}

// oidcAccount zeigt im Konto, ob und womit es verknüpft ist.
func (p *Panel) oidcAccount(w http.ResponseWriter, r *http.Request) {
	user := userFrom(r.Context())
	result := map[string]any{"configured": false, "hasPassword": user.PasswordHash != "", "passwordLogin": !p.sso.oidcOnly(),
		"firstPasswordUntil": p.firstPasswordUntil(r, user)}
	if p.requireOIDC() == nil {
		result["configured"], result["name"] = true, p.sso.name
		if link, err := p.store.OIDCLinkByUser(user.ID, p.sso.issuer); err == nil {
			result["link"] = link
		}
	}
	writeJSON(w, http.StatusOK, result)
}

// confirmBody ist die Bestätigung vor einem neuen Anmeldeweg.
type confirmBody struct {
	Password string `json:"password"`
}

// confirmIdentity verlangt vor einem neuen Anmeldeweg (Passkey, OIDC-Verknüpfung) eine frische Bestätigung: das
// aktuelle Passwort oder, bei Konten ohne Passwort, eine Anmeldung, die höchstens firstPasswordWindow zurückliegt.
// Sonst könnte sich jemand mit einer kurz geliehenen oder gestohlenen Sitzung einen eigenen Zugang anlegen, der den
// Passwortwechsel übersteht. Falsche Passwörter werden je Benutzer gedrosselt.
func (p *Panel) confirmIdentity(r *http.Request, password string) error {
	user := userFrom(r.Context())
	if user.PasswordHash == "" {
		if p.firstPasswordUntil(r, user) > 0 {
			return nil
		}
		return fail(http.StatusForbidden, "bestätige, dass du es bist: Melde dich ab und neu an, danach kannst du 10 Minuten lang einen Anmeldeweg hinzufügen")
	}
	key := "confirm|" + user.ID
	if p.throttle.blocked(key) {
		return fail(http.StatusTooManyRequests, "zu viele falsche Passwörter, versuche es in ein paar Minuten erneut")
	}
	if !CheckPassword(password, user.PasswordHash) {
		p.throttle.fail(key)
		return fail(http.StatusForbidden, "das aktuelle Passwort stimmt nicht")
	}
	p.throttle.reset(key)
	return nil
}

// oidcLinkStart beginnt die Verknüpfung des angemeldeten Benutzers; die Oberfläche öffnet danach die Adresse.
func (p *Panel) oidcLinkStart(w http.ResponseWriter, r *http.Request) {
	if err := p.requireOIDC(); err != nil {
		writeError(w, err)
		return
	}
	var body confirmBody
	if err := decodeOptional(r, &body); err != nil {
		writeError(w, err)
		return
	}
	if err := p.confirmIdentity(r, body.Password); err != nil {
		writeError(w, err)
		return
	}
	target, err := p.beginOIDC(w, r, "link", userFrom(r.Context()).ID, "/account/")
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"url": target})
}

// canLogInWithout prüft, ob ein Benutzer ohne den genannten Weg („oidc“ oder „passkey“) noch hereinkommt.
func (p *Panel) canLogInWithout(user User, way string) bool {
	if user.PasswordHash != "" && !p.sso.oidcOnly() {
		return true
	}
	passkeys := 0
	if p.sso.passkeysEnabled() {
		passkeys, _ = p.store.CountPasskeys(user.ID)
	}
	linked := false
	if p.requireOIDC() == nil {
		_, err := p.store.OIDCLinkByUser(user.ID, p.sso.issuer)
		linked = err == nil
	}
	switch way {
	case "oidc":
		return passkeys > 0
	case "passkey":
		return passkeys > 1 || linked
	}
	return false
}

func (p *Panel) oidcUnlink(w http.ResponseWriter, r *http.Request) {
	if err := p.requireOIDC(); err != nil {
		writeError(w, err)
		return
	}
	user := userFrom(r.Context())
	if !p.canLogInWithout(user, "oidc") {
		writeError(w, fail(http.StatusConflict, "ohne die Verknüpfung kämst du nicht mehr herein – lege zuerst einen Passkey an oder lass dir von einem Administrator ein Passwort setzen"))
		return
	}
	if err := p.store.UnlinkOIDC(user.ID, p.sso.issuer); err != nil {
		writeError(w, err)
		return
	}
	p.audit(r, "OIDC-Verknüpfung gelöst", "")
	w.WriteHeader(http.StatusNoContent)
}

// ---------------------------------------------------------------- Passkeys: Anmelden ohne Passwort

// credentialBody ist die Antwort des Browsers (navigator.credentials) samt Bezug auf die Zeremonie.
type credentialBody struct {
	Flow       string          `json:"flow"`
	Ticket     string          `json:"ticket,omitempty"`
	Name       string          `json:"name,omitempty"`
	Credential json.RawMessage `json:"credential"`
}

// decodeEmpty erlaubt einen leeren Body oder {} (Zeremonien beginnen ohne Eingabe).
func decodeEmpty(r *http.Request) error {
	return decodeOptional(r, &struct{}{})
}

// decodeOptional liest einen JSON-Körper streng wie decode, ein leerer Körper ist aber erlaubt (into bleibt dann leer).
func decodeOptional(r *http.Request, into any) error {
	data, err := io.ReadAll(io.LimitReader(r.Body, 1<<16))
	if err != nil {
		return fail(http.StatusBadRequest, "ungültige Anfrage")
	}
	if len(bytes.TrimSpace(data)) == 0 {
		return nil
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(into); err != nil {
		return fail(http.StatusBadRequest, "ungültige Anfrage: "+err.Error())
	}
	return nil
}

func encodeCredentialID(id []byte) string { return base64.RawURLEncoding.EncodeToString(id) }

// savePasskeyUse merkt sich Zähler und Merkmale nach einer erfolgreichen Prüfung.
func (p *Panel) savePasskeyUse(userID string, credential *webauthn.Credential) {
	data, err := json.Marshal(credential)
	if err == nil {
		err = p.store.UpdatePasskeyUse(userID, encodeCredentialID(credential.ID), string(data))
	}
	if err != nil {
		p.log.Warn("Passkey-Nutzung nicht gespeichert", "error", err)
	}
}

func (p *Panel) passkeyLoginBegin(w http.ResponseWriter, r *http.Request) {
	if err := p.requirePasskeys(); err != nil {
		writeError(w, err)
		return
	}
	if err := decodeEmpty(r); err != nil {
		writeError(w, err)
		return
	}
	if p.throttle.blocked(p.clientIP(r) + "|passkey") {
		writeError(w, fail(http.StatusTooManyRequests, "zu viele Fehlversuche, bitte warte ein paar Minuten"))
		return
	}
	// Ohne Passwort muss das Gerät den Benutzer prüfen (PIN, Fingerabdruck …)
	assertion, session, err := p.sso.webauthn.BeginDiscoverableLogin(webauthn.WithUserVerification(protocol.VerificationRequired))
	if err != nil {
		writeError(w, err)
		return
	}
	flow, err := p.sso.putCeremony(ceremony{kind: "login", session: *session, ip: p.clientIP(r)})
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"flow": flow, "options": assertion})
}

var errPasskeyRejected = fail(http.StatusUnauthorized, "der Passkey wurde nicht erkannt – prüfe, ob er für dieses Panel registriert ist")

func (p *Panel) passkeyLoginFinish(w http.ResponseWriter, r *http.Request) {
	if err := p.requirePasskeys(); err != nil {
		writeError(w, err)
		return
	}
	var body credentialBody
	if err := decode(r, &body); err != nil {
		writeError(w, err)
		return
	}
	ip := p.clientIP(r)
	key := ip + "|passkey"
	if p.throttle.blocked(key) {
		writeError(w, fail(http.StatusTooManyRequests, "zu viele Fehlversuche, bitte warte ein paar Minuten"))
		return
	}
	entry, ok := p.sso.takeCeremony(body.Flow, "login", "")
	if !ok {
		writeError(w, fail(http.StatusUnauthorized, "die Anmeldung ist abgelaufen – versuch es noch einmal"))
		return
	}
	reject := func(reason error) {
		p.throttle.fail(key)
		p.log.Info("Passkey-Anmeldung abgelehnt", "error", reason)
		p.recordAudit("", "", "Anmeldung mit Passkey fehlgeschlagen", "", http.StatusUnauthorized, ip)
		writeError(w, errPasskeyRejected)
	}
	parsed, err := protocol.ParseCredentialRequestResponseBytes(body.Credential)
	if err != nil {
		reject(err)
		return
	}
	var found passkeyUser
	handler := func(_, userHandle []byte) (webauthn.User, error) {
		user, err := p.store.UserByID(string(userHandle))
		if err != nil {
			return nil, err
		}
		if user.Disabled {
			return nil, errors.New("Benutzer gesperrt")
		}
		found, err = p.loadPasskeyUser(user)
		return found, err
	}
	_, credential, err := p.sso.webauthn.ValidatePasskeyLogin(handler, entry.session, parsed)
	if err != nil {
		reject(err)
		return
	}
	if credential.Authenticator.CloneWarning {
		reject(errors.New("Zähler des Passkeys ist zurückgesprungen (möglicher Klon)"))
		return
	}
	p.savePasskeyUse(found.user.ID, credential)
	p.throttle.reset(key)
	if err := p.startSession(w, r, found.user); err != nil {
		writeError(w, err)
		return
	}
	p.recordAudit(found.user.ID, found.user.Username, "Anmeldung (Passkey)", "", http.StatusOK, ip)
	writeJSON(w, http.StatusOK, found.user)
}

// ---------------------------------------------------------------- Passkeys: zweiter Faktor nach dem Passwort

// ticketUser prüft das Ticket aus dem ersten Schritt und lädt den Benutzer mit seinen Passkeys.
func (p *Panel) ticketUser(ticket string) (passkeyUser, error) {
	userID, ok := p.readTicket(ticket)
	if !ok {
		return passkeyUser{}, fail(http.StatusUnauthorized, "die Anmeldung ist abgelaufen – melde dich noch einmal an")
	}
	if p.throttle.blocked("totp|" + userID) {
		return passkeyUser{}, fail(http.StatusTooManyRequests, "zu viele Fehlversuche, bitte warte ein paar Minuten")
	}
	user, err := p.store.UserByID(userID)
	if err != nil || user.Disabled {
		return passkeyUser{}, fail(http.StatusUnauthorized, "nicht angemeldet")
	}
	return p.loadPasskeyUser(user)
}

func (p *Panel) passkeySecondBegin(w http.ResponseWriter, r *http.Request) {
	if err := p.requirePasskeys(); err != nil {
		writeError(w, err)
		return
	}
	var body struct {
		Ticket string `json:"ticket"`
	}
	if err := decode(r, &body); err != nil {
		writeError(w, err)
		return
	}
	account, err := p.ticketUser(body.Ticket)
	if err != nil {
		writeError(w, err)
		return
	}
	if len(account.credentials) == 0 {
		writeError(w, fail(http.StatusConflict, "für dieses Konto ist kein Passkey registriert – nimm den Code aus deiner App"))
		return
	}
	assertion, session, err := p.sso.webauthn.BeginLogin(account, webauthn.WithUserVerification(protocol.VerificationPreferred))
	if err != nil {
		writeError(w, err)
		return
	}
	flow, err := p.sso.putCeremony(ceremony{kind: "second", userID: account.user.ID, session: *session, ip: p.clientIP(r)})
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"flow": flow, "options": assertion})
}

func (p *Panel) passkeySecondFinish(w http.ResponseWriter, r *http.Request) {
	if err := p.requirePasskeys(); err != nil {
		writeError(w, err)
		return
	}
	var body credentialBody
	if err := decode(r, &body); err != nil {
		writeError(w, err)
		return
	}
	account, err := p.ticketUser(body.Ticket)
	if err != nil {
		writeError(w, err)
		return
	}
	user, ip, key := account.user, p.clientIP(r), "totp|"+account.user.ID
	entry, ok := p.sso.takeCeremony(body.Flow, "second", user.ID)
	if !ok {
		writeError(w, fail(http.StatusUnauthorized, "die Anmeldung ist abgelaufen – versuch es noch einmal"))
		return
	}
	parsed, err := protocol.ParseCredentialRequestResponseBytes(body.Credential)
	var credential *webauthn.Credential
	if err == nil {
		credential, err = p.sso.webauthn.ValidateLogin(account, entry.session, parsed)
	}
	if err == nil && credential.Authenticator.CloneWarning {
		err = errors.New("Zähler des Passkeys ist zurückgesprungen (möglicher Klon)")
	}
	if err != nil {
		p.throttle.fail(key)
		p.log.Info("Passkey als zweiter Faktor abgelehnt", "error", err)
		p.recordAudit(user.ID, user.Username, "Anmeldung: Passkey abgelehnt", "", http.StatusUnauthorized, ip)
		writeError(w, errPasskeyRejected)
		return
	}
	p.savePasskeyUse(user.ID, credential)
	p.throttle.reset(key)
	if err := p.startSession(w, r, user); err != nil {
		writeError(w, err)
		return
	}
	p.recordAudit(user.ID, user.Username, "Anmeldung", "", http.StatusOK, ip)
	writeJSON(w, http.StatusOK, user)
}

// ---------------------------------------------------------------- Passkeys im Konto

const maxPasskeys = 20

func validPasskeyName(name string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" || utf8.RuneCountInString(name) > 48 {
		return "", fail(http.StatusBadRequest, "der Name braucht 1 bis 48 Zeichen")
	}
	return name, nil
}

func (p *Panel) listPasskeys(w http.ResponseWriter, r *http.Request) {
	keys, err := p.store.ListPasskeys(userFrom(r.Context()).ID)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, keys)
}

func (p *Panel) passkeyRegisterBegin(w http.ResponseWriter, r *http.Request) {
	if err := p.requirePasskeys(); err != nil {
		writeError(w, err)
		return
	}
	var body confirmBody
	if err := decodeOptional(r, &body); err != nil {
		writeError(w, err)
		return
	}
	if err := p.confirmIdentity(r, body.Password); err != nil {
		writeError(w, err)
		return
	}
	account, err := p.loadPasskeyUser(userFrom(r.Context()))
	if err != nil {
		writeError(w, err)
		return
	}
	if len(account.credentials) >= maxPasskeys {
		writeError(w, fail(http.StatusConflict, "höchstens 20 Passkeys pro Benutzer"))
		return
	}
	creation, session, err := p.sso.webauthn.BeginRegistration(account,
		webauthn.WithResidentKeyRequirement(protocol.ResidentKeyRequirementPreferred),
		webauthn.WithExclusions(webauthn.Credentials(account.credentials).CredentialDescriptors()))
	if err != nil {
		writeError(w, err)
		return
	}
	flow, err := p.sso.putCeremony(ceremony{kind: "register", userID: account.user.ID, session: *session, ip: p.clientIP(r)})
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"flow": flow, "options": creation})
}

func (p *Panel) passkeyRegisterFinish(w http.ResponseWriter, r *http.Request) {
	if err := p.requirePasskeys(); err != nil {
		writeError(w, err)
		return
	}
	var body credentialBody
	if err := decode(r, &body); err != nil {
		writeError(w, err)
		return
	}
	name, err := validPasskeyName(body.Name)
	if err != nil {
		writeError(w, err)
		return
	}
	account, err := p.loadPasskeyUser(userFrom(r.Context()))
	if err != nil {
		writeError(w, err)
		return
	}
	entry, ok := p.sso.takeCeremony(body.Flow, "register", account.user.ID)
	if !ok {
		writeError(w, fail(http.StatusConflict, "die Einrichtung ist abgelaufen – starte sie noch einmal"))
		return
	}
	parsed, err := protocol.ParseCredentialCreationResponseBytes(body.Credential)
	var credential *webauthn.Credential
	if err == nil {
		credential, err = p.sso.webauthn.CreateCredential(account, entry.session, parsed)
	}
	if err != nil {
		p.log.Info("Passkey-Registrierung abgelehnt", "error", err)
		writeError(w, fail(http.StatusBadRequest, "der Passkey konnte nicht geprüft werden – versuch es noch einmal"))
		return
	}
	data, err := json.Marshal(credential)
	if err != nil {
		writeError(w, err)
		return
	}
	created, err := p.store.CreatePasskey(Passkey{UserID: account.user.ID, CredentialID: encodeCredentialID(credential.ID), Name: name, Data: string(data)})
	if err != nil {
		writeError(w, err)
		return
	}
	p.audit(r, "Passkey hinzugefügt", name)
	writeJSON(w, http.StatusCreated, created)
}

func (p *Panel) renamePasskey(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Name string `json:"name"`
	}
	if err := decode(r, &body); err != nil {
		writeError(w, err)
		return
	}
	name, err := validPasskeyName(body.Name)
	if err != nil {
		writeError(w, err)
		return
	}
	if err := p.store.RenamePasskey(userFrom(r.Context()).ID, r.PathValue("id"), name); err != nil {
		writeError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (p *Panel) deletePasskey(w http.ResponseWriter, r *http.Request) {
	user := userFrom(r.Context())
	if !p.canLogInWithout(user, "passkey") {
		writeError(w, fail(http.StatusConflict, "das ist dein letzter Anmeldeweg – lege zuerst einen anderen Passkey an oder verknüpfe dein Konto"))
		return
	}
	if err := p.store.DeletePasskey(user.ID, r.PathValue("id")); err != nil {
		writeError(w, err)
		return
	}
	p.audit(r, "Passkey entfernt", "")
	w.WriteHeader(http.StatusNoContent)
}

// resetPasskeys entfernt alle Passkeys eines Benutzers (Administrator, z. B. nach Verlust des Geräts).
func (p *Panel) resetPasskeys(w http.ResponseWriter, r *http.Request) {
	if err := requireAdmin(r); err != nil {
		writeError(w, err)
		return
	}
	user, err := p.store.UserByID(r.PathValue("id"))
	if err != nil {
		writeError(w, err)
		return
	}
	if err := p.store.DeleteUserPasskeys(user.ID); err != nil {
		writeError(w, err)
		return
	}
	p.audit(r, "Passkeys zurückgesetzt", user.Username)
	w.WriteHeader(http.StatusNoContent)
}
