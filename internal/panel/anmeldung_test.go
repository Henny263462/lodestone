package panel

import (
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"io"
	"log/slog"
	"math/big"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/go-webauthn/webauthn/protocol/webauthncbor"
	"github.com/go-webauthn/webauthn/protocol/webauthncose"
)

// ---------------------------------------------------------------- Test-Issuer (OIDC-Anbieter als Attrappe)

type issuedCode struct {
	nonce, challenge string
}

// fakeIssuer spielt einen OIDC-Anbieter: Discovery, JWKS, Anmeldeseite (leitet sofort zurück) und Token-Endpunkt.
type fakeIssuer struct {
	t   *testing.T
	srv *httptest.Server
	key *rsa.PrivateKey

	mu       sync.Mutex
	codes    map[string]issuedCode
	subject  string
	email    string
	username string
	groups   []string
	// Fehlerfälle
	wrongNonce    bool
	wrongAudience bool
	deny          bool
}

const testClientID = "lodestone-test"

func newFakeIssuer(t *testing.T) *fakeIssuer {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	issuer := &fakeIssuer{t: t, key: key, codes: map[string]issuedCode{}, subject: "sub-alex", email: "alex@example.org", username: "alex"}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /.well-known/openid-configuration", func(w http.ResponseWriter, _ *http.Request) {
		base := issuer.srv.URL
		writeJSON(w, 200, map[string]any{
			"issuer": base, "authorization_endpoint": base + "/authorize", "token_endpoint": base + "/token", "jwks_uri": base + "/jwks",
			"response_types_supported": []string{"code"}, "subject_types_supported": []string{"public"},
			"id_token_signing_alg_values_supported": []string{"RS256"},
		})
	})
	mux.HandleFunc("GET /jwks", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, 200, map[string]any{"keys": []map[string]string{{
			"kty": "RSA", "kid": "k1", "use": "sig", "alg": "RS256",
			"n": b64(key.N.Bytes()), "e": b64(big.NewInt(int64(key.E)).Bytes()),
		}}})
	})
	mux.HandleFunc("GET /authorize", func(w http.ResponseWriter, r *http.Request) {
		query := r.URL.Query()
		if query.Get("client_id") != testClientID || query.Get("response_type") != "code" {
			t.Errorf("Anmeldeseite: falsche Parameter %v", query)
		}
		if query.Get("code_challenge_method") != "S256" || query.Get("code_challenge") == "" || query.Get("nonce") == "" || query.Get("state") == "" {
			t.Errorf("PKCE, nonce oder state fehlen: %v", query)
		}
		target, _ := url.Parse(query.Get("redirect_uri"))
		back := url.Values{"state": {query.Get("state")}}
		issuer.mu.Lock()
		if issuer.deny {
			back.Set("error", "access_denied")
		} else {
			code := b64([]byte(newID() + newID()))
			issuer.codes[code] = issuedCode{nonce: query.Get("nonce"), challenge: query.Get("code_challenge")}
			back.Set("code", code)
		}
		issuer.mu.Unlock()
		target.RawQuery = back.Encode()
		http.Redirect(w, r, target.String(), http.StatusFound)
	})
	mux.HandleFunc("POST /token", func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		issuer.mu.Lock()
		issued, ok := issuer.codes[r.PostForm.Get("code")]
		delete(issuer.codes, r.PostForm.Get("code"))
		issuer.mu.Unlock()
		sum := sha256.Sum256([]byte(r.PostForm.Get("code_verifier")))
		if !ok || b64(sum[:]) != issued.challenge {
			writeJSON(w, 400, map[string]string{"error": "invalid_grant"})
			return
		}
		writeJSON(w, 200, map[string]any{"access_token": "zugang", "token_type": "Bearer", "expires_in": 300, "id_token": issuer.idToken(issued.nonce)})
	})
	issuer.srv = httptest.NewServer(mux)
	t.Cleanup(issuer.srv.Close)
	return issuer
}

func b64(data []byte) string { return base64.RawURLEncoding.EncodeToString(data) }

// idToken signiert ein ID-Token (RS256) mit den aktuellen Angaben.
func (f *fakeIssuer) idToken(nonce string) string {
	f.mu.Lock()
	defer f.mu.Unlock()
	audience := testClientID
	if f.wrongAudience {
		audience = "eine-andere-anwendung"
	}
	if f.wrongNonce {
		nonce = "falsche-nonce"
	}
	header, _ := json.Marshal(map[string]string{"alg": "RS256", "kid": "k1", "typ": "JWT"})
	claims, _ := json.Marshal(map[string]any{
		"iss": f.srv.URL, "sub": f.subject, "aud": audience, "iat": time.Now().Unix(), "exp": time.Now().Add(5 * time.Minute).Unix(),
		"nonce": nonce, "email": f.email, "preferred_username": f.username, "groups": f.groups,
	})
	signing := b64(header) + "." + b64(claims)
	digest := sha256.Sum256([]byte(signing))
	signature, err := rsa.SignPKCS1v15(rand.Reader, f.key, crypto.SHA256, digest[:])
	if err != nil {
		f.t.Fatal(err)
	}
	return signing + "." + b64(signature)
}

// setupSSOPanel startet ein Panel mit Anmeldung über den Test-Issuer und Passkeys für https://panel.example.
func setupSSOPanel(t *testing.T, change func(*SSOConfig)) (*Panel, *httptest.Server) {
	store, err := OpenStore(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })
	var handler http.Handler
	web := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { handler.ServeHTTP(w, r) }))
	t.Cleanup(web.Close)
	cfg := SSOConfig{PublicURL: "https://panel.example", OIDCRedirectURL: web.URL + "/api/auth/oidc/callback", OIDCClientID: testClientID, OIDCClientSecret: "geheim", OIDCName: "Authentik"}
	change(&cfg)
	p := New(Config{Version: "test", SSO: cfg}, store, slog.New(slog.NewTextHandler(io.Discard, nil)), nil)
	handler = p.Handler()
	return p, web
}

// browse folgt Weiterleitungen wie ein Browser über Anbieter und Rücksprung und liefert das Ziel danach.
func browse(t *testing.T, c *client, address string) string {
	t.Helper()
	if strings.HasPrefix(address, "/") {
		address = c.base + address
	}
	browser := &http.Client{Jar: c.http.Jar, CheckRedirect: func(request *http.Request, _ []*http.Request) error {
		if strings.HasPrefix(request.URL.Path, "/api/auth/oidc/") || request.URL.Path == "/authorize" {
			return nil
		}
		return http.ErrUseLastResponse
	}}
	response, err := browser.Get(address)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusFound {
		t.Fatalf("%s: Status %d statt einer Weiterleitung", address, response.StatusCode)
	}
	return response.Header.Get("Location")
}

func TestOIDCLoginCreatesUserAndGrantsAdminGroup(t *testing.T) {
	issuer := newFakeIssuer(t)
	issuer.groups = []string{"spieler", "lodestone-admins"}
	p, web := setupSSOPanel(t, func(cfg *SSOConfig) {
		cfg.OIDCIssuer, cfg.OIDCAutoCreate, cfg.OIDCAdminGroup = issuer.srv.URL, true, "lodestone-admins"
	})

	var status struct {
		Oidc          struct{ Name string }
		PasswordLogin bool
		Passkeys      bool
	}
	_ = json.Unmarshal(newClient(t, web.URL).expect(200, "GET", "/api/auth/status", nil), &status)
	if status.Oidc.Name != "Authentik" || !status.PasswordLogin || !status.Passkeys {
		t.Fatalf("Anmeldewege: %+v", status)
	}

	c := newClient(t, web.URL)
	if target := browse(t, c, "/api/auth/oidc/start?next=/nodes/"); target != "/nodes/" {
		t.Fatalf("nach der Anmeldung: %s", target)
	}
	var me User
	_ = json.Unmarshal(c.expect(200, "GET", "/api/me", nil), &me)
	if me.Username != "alex" || !me.IsAdmin {
		t.Fatalf("angelegter Benutzer: %+v", me)
	}
	// Schreiben braucht weiter den Schutz-Header (dieselbe Sitzung wie bei der Passwort-Anmeldung)
	if code, _ := c.do("POST", "/api/projects", map[string]string{"name": "x", "kind": "server"}, csrfHeader, ""); code != 403 {
		t.Errorf("ohne CSRF-Header: %d", code)
	}
	// Ein fremdes Ziel nach der Anmeldung wird nicht angesprungen
	other := newClient(t, web.URL)
	if target := browse(t, other, "/api/auth/oidc/start?next=//boese.example/"); target != "/" {
		t.Fatalf("offene Weiterleitung: %s", target)
	}
	// Zweite Anmeldung desselben Kontos landet beim selben Benutzer
	if count, _ := p.store.CountUsers(); count != 1 {
		t.Fatalf("Benutzer: %d", count)
	}
	// Ohne Gruppe verliert ein über OIDC angelegtes Konto die Rechte nicht, solange es der letzte Administrator ist
	issuer.groups = nil
	browse(t, newClient(t, web.URL), "/api/auth/oidc/start")
	if user, _ := p.store.UserByName("alex"); !user.IsAdmin {
		t.Fatal("der letzte Administrator darf seine Rechte nicht verlieren")
	}
	entries, _ := p.store.ListAudit(50, 0, "")
	found := false
	for _, entry := range entries {
		found = found || entry.Action == "Anmeldung (OIDC)"
	}
	if !found {
		t.Error("die Anmeldung fehlt im Protokoll")
	}
}

func TestOIDCLinkingWithoutAutoCreate(t *testing.T) {
	issuer := newFakeIssuer(t)
	p, web := setupSSOPanel(t, func(cfg *SSOConfig) { cfg.OIDCIssuer = issuer.srv.URL })
	admin := newClient(t, web.URL)
	admin.expect(201, "POST", "/api/auth/setup", map[string]string{"username": "admin", "password": "ein-gutes-passwort"})
	admin.expect(201, "POST", "/api/users", map[string]any{"username": "bob", "password": "ein-gutes-passwort"})

	// Unbekanntes Konto kommt ohne Auto-Create nicht herein
	stranger := newClient(t, web.URL)
	if target := browse(t, stranger, "/api/auth/oidc/start"); target != "/login/?error=oidc_unlinked" {
		t.Fatalf("unverknüpft: %s", target)
	}
	stranger.expect(401, "GET", "/api/me", nil)
	if count, _ := p.store.CountUsers(); count != 2 {
		t.Fatalf("ohne Auto-Create darf niemand angelegt werden: %d Benutzer", count)
	}

	// Der Administrator verknüpft sein Konto im Konto-Bereich
	var start struct{ URL string }
	_ = json.Unmarshal(admin.expect(200, "POST", "/api/me/oidc/link", nil), &start)
	if !strings.HasPrefix(start.URL, issuer.srv.URL+"/authorize?") {
		t.Fatalf("Adresse beim Anbieter: %s", start.URL)
	}
	if target := browse(t, admin, start.URL); target != "/account/?oidc=linked" {
		t.Fatalf("Verknüpfen: %s", target)
	}
	var account struct {
		Configured bool
		Name       string
		Link       *struct{ Email string }
	}
	_ = json.Unmarshal(admin.expect(200, "GET", "/api/me/oidc", nil), &account)
	if !account.Configured || account.Link == nil || account.Link.Email != "alex@example.org" {
		t.Fatalf("Verknüpfung: %+v", account)
	}

	// Jetzt kommt das Konto als „admin“ herein
	c := newClient(t, web.URL)
	if target := browse(t, c, "/api/auth/oidc/start"); target != "/" {
		t.Fatalf("verknüpft: %s", target)
	}
	var me User
	_ = json.Unmarshal(c.expect(200, "GET", "/api/me", nil), &me)
	if me.Username != "admin" {
		t.Fatalf("angemeldet als %+v", me)
	}

	// Dasselbe Konto beim Anbieter kann nicht zusätzlich zu bob gehören
	bob := newClient(t, web.URL)
	bob.expect(200, "POST", "/api/auth/login", map[string]string{"username": "bob", "password": "ein-gutes-passwort"})
	_ = json.Unmarshal(bob.expect(200, "POST", "/api/me/oidc/link", nil), &start)
	if target := browse(t, bob, start.URL); target != "/account/?oidc=oidc_taken" {
		t.Fatalf("doppelte Verknüpfung: %s", target)
	}

	// Lösen geht, weil admin ein Passwort hat; danach kommt das Konto nicht mehr herein
	admin.expect(204, "DELETE", "/api/me/oidc", nil)
	if target := browse(t, newClient(t, web.URL), "/api/auth/oidc/start"); target != "/login/?error=oidc_unlinked" {
		t.Fatalf("nach dem Lösen: %s", target)
	}
}

func TestOIDCRejectsBadStateNonceAndAudience(t *testing.T) {
	issuer := newFakeIssuer(t)
	_, web := setupSSOPanel(t, func(cfg *SSOConfig) { cfg.OIDCIssuer, cfg.OIDCAutoCreate = issuer.srv.URL, true })

	// Rücksprung ohne begonnene Anmeldung
	c := newClient(t, web.URL)
	if target := browse(t, c, "/api/auth/oidc/callback?state=ausgedacht&code=x"); target != "/login/?error=oidc_state" {
		t.Fatalf("ausgedachter state: %s", target)
	}

	// Rücksprung aus einem anderen Browser (ohne Flow-Cookie) und Wiederholung desselben Rücksprungs
	victim := newClient(t, web.URL)
	stop := &http.Client{Jar: victim.http.Jar, CheckRedirect: func(request *http.Request, _ []*http.Request) error {
		if request.URL.Path == "/authorize" {
			return nil
		}
		return http.ErrUseLastResponse
	}}
	response, err := stop.Get(web.URL + "/api/auth/oidc/start")
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	callback := response.Header.Get("Location")
	if !strings.Contains(callback, "/api/auth/oidc/callback?") {
		t.Fatalf("Rücksprung: %s", callback)
	}
	attacker := newClient(t, web.URL)
	if target := browse(t, attacker, callback); target != "/login/?error=oidc_state" {
		t.Fatalf("fremder Browser: %s", target)
	}
	attacker.expect(401, "GET", "/api/me", nil)
	if target := browse(t, victim, callback); target != "/login/?error=oidc_state" {
		t.Fatalf("ein state gilt nur einmal: %s", target)
	}

	for name, setup := range map[string]func(){
		"nonce":    func() { issuer.wrongNonce = true },
		"audience": func() { issuer.wrongAudience = true },
	} {
		issuer.wrongNonce, issuer.wrongAudience = false, false
		setup()
		c := newClient(t, web.URL)
		if target := browse(t, c, "/api/auth/oidc/start"); target != "/login/?error=oidc_invalid" {
			t.Fatalf("%s: %s", name, target)
		}
		c.expect(401, "GET", "/api/me", nil)
	}
	issuer.wrongNonce, issuer.wrongAudience, issuer.deny = false, false, true
	if target := browse(t, newClient(t, web.URL), "/api/auth/oidc/start"); target != "/login/?error=oidc_denied" {
		t.Fatalf("abgelehnt: %s", target)
	}
}

func TestOIDCOnlyTurnsOffPasswordLogin(t *testing.T) {
	issuer := newFakeIssuer(t)
	_, web := setupSSOPanel(t, func(cfg *SSOConfig) { cfg.OIDCIssuer, cfg.OIDCOnly = issuer.srv.URL, true })
	c := newClient(t, web.URL)
	// Die Einrichtung des ersten Administrators geht weiter (sonst käme niemand herein)
	c.expect(201, "POST", "/api/auth/setup", map[string]string{"username": "admin", "password": "ein-gutes-passwort"})
	other := newClient(t, web.URL)
	other.expect(403, "POST", "/api/auth/login", map[string]string{"username": "admin", "password": "ein-gutes-passwort"})
	var status struct{ PasswordLogin bool }
	_ = json.Unmarshal(other.expect(200, "GET", "/api/auth/status", nil), &status)
	if status.PasswordLogin {
		t.Fatal("die Oberfläche muss wissen, dass die Passwort-Anmeldung aus ist")
	}

	// Ohne eingerichtetes OIDC wird die Option ignoriert
	_, plain := setupSSOPanel(t, func(cfg *SSOConfig) { cfg.OIDCOnly, cfg.OIDCClientID = true, "" })
	c = newClient(t, plain.URL)
	c.expect(201, "POST", "/api/auth/setup", map[string]string{"username": "admin", "password": "ein-gutes-passwort"})
	newClient(t, plain.URL).expect(200, "POST", "/api/auth/login", map[string]string{"username": "admin", "password": "ein-gutes-passwort"})
}

func TestAPIKeysCannotChangeSignInMethods(t *testing.T) {
	issuer := newFakeIssuer(t)
	_, web := setupSSOPanel(t, func(cfg *SSOConfig) { cfg.OIDCIssuer = issuer.srv.URL })
	admin := newClient(t, web.URL)
	admin.expect(201, "POST", "/api/auth/setup", map[string]string{"username": "admin", "password": "ein-gutes-passwort"})
	var created struct{ Key string }
	_ = json.Unmarshal(admin.expect(201, "POST", "/api/me/keys", map[string]any{"name": "skript"}), &created)
	for _, route := range [][2]string{
		{"GET", "/api/me/passkeys"}, {"POST", "/api/me/passkeys/begin"}, {"POST", "/api/me/passkeys/finish"},
		{"PATCH", "/api/me/passkeys/x"}, {"DELETE", "/api/me/passkeys/x"},
		{"GET", "/api/me/oidc"}, {"POST", "/api/me/oidc/link"}, {"DELETE", "/api/me/oidc"}, {"DELETE", "/api/users/x/passkeys"},
	} {
		if status, _ := bearer(t, web.URL, route[0], route[1], created.Key, map[string]any{}); status != 403 {
			t.Errorf("%s %s mit API-Schlüssel: %d", route[0], route[1], status)
		}
	}
}

// ---------------------------------------------------------------- Software-Authenticator für WebAuthn

// softAuthenticator ist ein Passkey im Speicher: ES256, Bestätigung „none“, immer mit Benutzerprüfung.
type softAuthenticator struct {
	key        *ecdsa.PrivateKey
	id         []byte
	userHandle []byte
	rpID       string
	origin     string
	count      uint32
}

func newSoftAuthenticator(t *testing.T) *softAuthenticator {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	id := make([]byte, 16)
	_, _ = rand.Read(id)
	return &softAuthenticator{key: key, id: id, rpID: "panel.example", origin: "https://panel.example"}
}

type creationOptions struct {
	PublicKey struct {
		Challenge string `json:"challenge"`
		RP        struct {
			ID string `json:"id"`
		} `json:"rp"`
		User struct {
			ID string `json:"id"`
		} `json:"user"`
		ExcludeCredentials []struct {
			ID string `json:"id"`
		} `json:"excludeCredentials"`
	} `json:"publicKey"`
}

type requestOptions struct {
	PublicKey struct {
		Challenge        string `json:"challenge"`
		RPID             string `json:"rpId"`
		UserVerification string `json:"userVerification"`
		AllowCredentials []struct {
			ID string `json:"id"`
		} `json:"allowCredentials"`
	} `json:"publicKey"`
}

func (a *softAuthenticator) clientData(kind, challenge string) []byte {
	data, _ := json.Marshal(map[string]any{"type": kind, "challenge": challenge, "origin": a.origin, "crossOrigin": false})
	return data
}

func (a *softAuthenticator) authData(flags byte, attested []byte) []byte {
	rpHash := sha256.Sum256([]byte(a.rpID))
	data := append(rpHash[:], flags)
	data = binary.BigEndian.AppendUint32(data, a.count)
	return append(data, attested...)
}

// create beantwortet navigator.credentials.create.
func (a *softAuthenticator) create(t *testing.T, raw json.RawMessage) json.RawMessage {
	t.Helper()
	var options creationOptions
	if err := json.Unmarshal(raw, &options); err != nil {
		t.Fatal(err)
	}
	if options.PublicKey.RP.ID != "panel.example" {
		t.Fatalf("RP-ID: %q", options.PublicKey.RP.ID)
	}
	a.userHandle, _ = base64.RawURLEncoding.DecodeString(options.PublicKey.User.ID)
	cose, err := webauthncbor.Marshal(webauthncose.EC2PublicKeyData{
		PublicKeyData: webauthncose.PublicKeyData{KeyType: int64(webauthncose.EllipticKey), Algorithm: int64(webauthncose.AlgES256)},
		Curve:         int64(webauthncose.P256), XCoord: a.key.X.FillBytes(make([]byte, 32)), YCoord: a.key.Y.FillBytes(make([]byte, 32)),
	})
	if err != nil {
		t.Fatal(err)
	}
	attested := make([]byte, 16) // AAGUID 0
	attested = binary.BigEndian.AppendUint16(attested, uint16(len(a.id)))
	attested = append(append(attested, a.id...), cose...)
	object, err := webauthncbor.Marshal(map[string]any{"fmt": "none", "attStmt": map[string]any{}, "authData": a.authData(0x45, attested)})
	if err != nil {
		t.Fatal(err)
	}
	answer, _ := json.Marshal(map[string]any{
		"id": b64(a.id), "rawId": b64(a.id), "type": "public-key", "clientExtensionResults": map[string]any{},
		"response": map[string]any{"clientDataJSON": b64(a.clientData("webauthn.create", options.PublicKey.Challenge)), "attestationObject": b64(object), "transports": []string{"internal"}},
	})
	return answer
}

// get beantwortet navigator.credentials.get.
func (a *softAuthenticator) get(t *testing.T, raw json.RawMessage) json.RawMessage {
	t.Helper()
	var options requestOptions
	if err := json.Unmarshal(raw, &options); err != nil {
		t.Fatal(err)
	}
	a.count++
	authData := a.authData(0x05, nil)
	clientData := a.clientData("webauthn.get", options.PublicKey.Challenge)
	clientHash := sha256.Sum256(clientData)
	digest := sha256.Sum256(append(append([]byte{}, authData...), clientHash[:]...))
	signature, err := ecdsa.SignASN1(rand.Reader, a.key, digest[:])
	if err != nil {
		t.Fatal(err)
	}
	answer, _ := json.Marshal(map[string]any{
		"id": b64(a.id), "rawId": b64(a.id), "type": "public-key", "clientExtensionResults": map[string]any{},
		"response": map[string]any{"clientDataJSON": b64(clientData), "authenticatorData": b64(authData), "signature": b64(signature), "userHandle": b64(a.userHandle)},
	})
	return answer
}

type ceremonyStart struct {
	Flow    string          `json:"flow"`
	Options json.RawMessage `json:"options"`
}

func registerPasskey(t *testing.T, c *client, device *softAuthenticator, name string) Passkey {
	t.Helper()
	var start ceremonyStart
	_ = json.Unmarshal(c.expect(200, "POST", "/api/me/passkeys/begin", nil), &start)
	var created Passkey
	_ = json.Unmarshal(c.expect(201, "POST", "/api/me/passkeys/finish", map[string]any{"flow": start.Flow, "name": name, "credential": device.create(t, start.Options)}), &created)
	return created
}

func passkeyLogin(t *testing.T, base string, device *softAuthenticator) (*client, int) {
	t.Helper()
	c := newClient(t, base)
	var start ceremonyStart
	_ = json.Unmarshal(c.expect(200, "POST", "/api/auth/passkey/begin", nil), &start)
	var options requestOptions
	_ = json.Unmarshal(start.Options, &options)
	if len(options.PublicKey.AllowCredentials) != 0 || options.PublicKey.UserVerification != "required" {
		t.Fatalf("Anmeldung ohne Benutzernamen braucht eine leere Liste und Benutzerprüfung: %s", start.Options)
	}
	status, _ := c.do("POST", "/api/auth/passkey/finish", map[string]any{"flow": start.Flow, "credential": device.get(t, start.Options)})
	return c, status
}

func TestPasskeyRegistrationAndPasswordlessLogin(t *testing.T) {
	p, web := setupSSOPanel(t, func(*SSOConfig) {})
	admin := newClient(t, web.URL)
	admin.expect(201, "POST", "/api/auth/setup", map[string]string{"username": "admin", "password": "ein-gutes-passwort"})

	device := newSoftAuthenticator(t)
	created := registerPasskey(t, admin, device, "Laptop")
	if created.ID == "" || created.Name != "Laptop" {
		t.Fatalf("Passkey: %+v", created)
	}
	admin.expect(400, "PATCH", "/api/me/passkeys/"+created.ID, map[string]string{"name": " "})
	admin.expect(204, "PATCH", "/api/me/passkeys/"+created.ID, map[string]string{"name": "Arbeitslaptop"})
	admin.expect(404, "PATCH", "/api/me/passkeys/gibtsnicht", map[string]string{"name": "x"})
	var list []map[string]any
	_ = json.Unmarshal(admin.expect(200, "GET", "/api/me/passkeys", nil), &list)
	if len(list) != 1 || list[0]["name"] != "Arbeitslaptop" || list[0]["data"] != nil || list[0]["credentialId"] != nil {
		t.Fatalf("Liste (ohne Schlüsseldaten): %v", list)
	}

	// Derselbe Passkey lässt sich nicht zweimal registrieren
	var again ceremonyStart
	_ = json.Unmarshal(admin.expect(200, "POST", "/api/me/passkeys/begin", nil), &again)
	var exclusions creationOptions
	_ = json.Unmarshal(again.Options, &exclusions)
	if len(exclusions.PublicKey.ExcludeCredentials) != 1 {
		t.Fatalf("vorhandene Passkeys müssen ausgeschlossen werden: %s", again.Options)
	}
	admin.expect(409, "POST", "/api/me/passkeys/finish", map[string]any{"flow": again.Flow, "name": "doppelt", "credential": device.create(t, again.Options)})

	// Anmelden ohne Passwort
	c, status := passkeyLogin(t, web.URL, device)
	if status != 200 {
		t.Fatalf("Anmeldung mit Passkey: %d", status)
	}
	var me User
	_ = json.Unmarshal(c.expect(200, "GET", "/api/me", nil), &me)
	if me.Username != "admin" {
		t.Fatalf("angemeldet als %+v", me)
	}
	if keys, _ := p.store.ListPasskeys(me.ID); keys[0].LastUsed == 0 {
		t.Error("die letzte Nutzung wird nicht gespeichert")
	}

	// Wiederholte Antwort, falsche Herkunft und unbekannter Passkey werden abgelehnt
	var start ceremonyStart
	_ = json.Unmarshal(c.expect(200, "POST", "/api/auth/passkey/begin", nil), &start)
	answer := device.get(t, start.Options)
	c.expect(200, "POST", "/api/auth/passkey/finish", map[string]any{"flow": start.Flow, "credential": answer})
	c.expect(401, "POST", "/api/auth/passkey/finish", map[string]any{"flow": start.Flow, "credential": answer})
	device.origin = "https://boese.example"
	if _, status := passkeyLogin(t, web.URL, device); status != 401 {
		t.Fatalf("falsche Herkunft: %d", status)
	}
	device.origin = "https://panel.example"
	if _, status := passkeyLogin(t, web.URL, newSoftAuthenticator(t)); status != 401 {
		t.Fatalf("unbekannter Passkey: %d", status)
	}

	// Gesperrte Benutzer kommen auch mit Passkey nicht herein
	_ = p.store.UpdateUser(me.ID, true, true)
	if _, status := passkeyLogin(t, web.URL, device); status != 401 {
		t.Fatalf("gesperrt: %d", status)
	}
}

func TestPasskeyAsSecondFactor(t *testing.T) {
	p, web := setupSSOPanel(t, func(*SSOConfig) {})
	admin := newClient(t, web.URL)
	admin.expect(201, "POST", "/api/auth/setup", map[string]string{"username": "admin", "password": "ein-gutes-passwort"})
	device := newSoftAuthenticator(t)
	created := registerPasskey(t, admin, device, "Schlüssel")

	c := newClient(t, web.URL)
	var step struct {
		NeedsTotp    bool
		Ticket       string
		SecondFactor []string
	}
	_ = json.Unmarshal(c.expect(200, "POST", "/api/auth/login", map[string]string{"username": "admin", "password": "ein-gutes-passwort"}), &step)
	if !step.NeedsTotp || step.Ticket == "" || len(step.SecondFactor) != 1 || step.SecondFactor[0] != "passkey" {
		t.Fatalf("mit Passkey braucht die Passwort-Anmeldung einen zweiten Schritt: %+v", step)
	}
	c.expect(401, "GET", "/api/me", nil)
	c.expect(401, "POST", "/api/auth/passkey/second/begin", map[string]string{"ticket": step.Ticket + "x"})

	var start ceremonyStart
	_ = json.Unmarshal(c.expect(200, "POST", "/api/auth/passkey/second/begin", map[string]string{"ticket": step.Ticket}), &start)
	var options requestOptions
	_ = json.Unmarshal(start.Options, &options)
	if len(options.PublicKey.AllowCredentials) != 1 {
		t.Fatalf("der zweite Faktor nennt die Passkeys des Benutzers: %s", start.Options)
	}
	// Ein Passkey eines anderen Geräts passt nicht
	stranger := newSoftAuthenticator(t)
	stranger.userHandle = device.userHandle
	c.expect(401, "POST", "/api/auth/passkey/second/finish", map[string]any{"ticket": step.Ticket, "flow": start.Flow, "credential": stranger.get(t, start.Options)})
	_ = json.Unmarshal(c.expect(200, "POST", "/api/auth/passkey/second/begin", map[string]string{"ticket": step.Ticket}), &start)
	c.expect(200, "POST", "/api/auth/passkey/second/finish", map[string]any{"ticket": step.Ticket, "flow": start.Flow, "credential": device.get(t, start.Options)})
	c.expect(200, "GET", "/api/me", nil)

	// Fehlversuche zählen wie falsche TOTP-Codes
	for i := 0; i < throttleLimit; i++ {
		_ = json.Unmarshal(c.expect(200, "POST", "/api/auth/passkey/second/begin", map[string]string{"ticket": step.Ticket}), &start)
		c.expect(401, "POST", "/api/auth/passkey/second/finish", map[string]any{"ticket": step.Ticket, "flow": start.Flow, "credential": stranger.get(t, start.Options)})
	}
	c.expect(429, "POST", "/api/auth/passkey/second/begin", map[string]string{"ticket": step.Ticket})
	c.expect(429, "POST", "/api/auth/totp", map[string]string{"ticket": step.Ticket, "code": "000000"})

	// Entfernen geht, weil das Konto ein Passwort hat; danach reicht das Passwort wieder
	var me User
	_ = json.Unmarshal(admin.expect(200, "GET", "/api/me", nil), &me)
	p.throttle.reset("totp|" + me.ID)
	admin.expect(204, "DELETE", "/api/me/passkeys/"+created.ID, nil)
	admin.expect(404, "DELETE", "/api/me/passkeys/"+created.ID, nil)
	plain := newClient(t, web.URL)
	var direct map[string]any
	_ = json.Unmarshal(plain.expect(200, "POST", "/api/auth/login", map[string]string{"username": "admin", "password": "ein-gutes-passwort"}), &direct)
	if direct["needsTotp"] != nil {
		t.Fatalf("ohne Passkey kein zweiter Schritt: %v", direct)
	}
}

func TestPasskeyLimitsAndAdminReset(t *testing.T) {
	p, web := setupSSOPanel(t, func(*SSOConfig) {})
	admin := newClient(t, web.URL)
	admin.expect(201, "POST", "/api/auth/setup", map[string]string{"username": "admin", "password": "ein-gutes-passwort"})
	admin.expect(201, "POST", "/api/users", map[string]any{"username": "bob", "password": "ein-gutes-passwort"})
	bob := newClient(t, web.URL)
	bob.expect(200, "POST", "/api/auth/login", map[string]string{"username": "bob", "password": "ein-gutes-passwort"})
	device := newSoftAuthenticator(t)
	key := registerPasskey(t, bob, device, "Handy")
	var bobUser User
	_ = json.Unmarshal(bob.expect(200, "GET", "/api/me", nil), &bobUser)
	key.UserID = bobUser.ID

	// Fremde Passkeys gibt es für andere nicht, und nur Administratoren setzen zurück
	admin.expect(404, "DELETE", "/api/me/passkeys/"+key.ID, nil)
	bob.expect(403, "DELETE", "/api/users/"+key.UserID+"/passkeys", nil)
	admin.expect(404, "DELETE", "/api/users/gibtsnicht/passkeys", nil)
	admin.expect(204, "DELETE", "/api/users/"+key.UserID+"/passkeys", nil)
	if count, _ := p.store.CountPasskeys(key.UserID); count != 0 {
		t.Fatalf("Passkeys nach dem Zurücksetzen: %d", count)
	}

	// Ungültige Antwort, abgelaufene Zeremonie, Name fehlt
	var start ceremonyStart
	_ = json.Unmarshal(bob.expect(200, "POST", "/api/me/passkeys/begin", nil), &start)
	bob.expect(400, "POST", "/api/me/passkeys/finish", map[string]any{"flow": start.Flow, "name": "", "credential": device.create(t, start.Options)})
	bob.expect(400, "POST", "/api/me/passkeys/finish", map[string]any{"flow": start.Flow, "name": "Handy", "credential": json.RawMessage(`{"id":"x"}`)})
	bob.expect(409, "POST", "/api/me/passkeys/finish", map[string]any{"flow": start.Flow, "name": "Handy", "credential": device.create(t, start.Options)})
	bob.expect(400, "POST", "/api/me/passkeys/begin", map[string]any{"unbekannt": true})

	// Gedrosselt nach zu vielen Fehlversuchen ohne Passwort
	for i := 0; i < throttleLimit; i++ {
		if _, status := passkeyLogin(t, web.URL, newSoftAuthenticator(t)); status != 401 {
			t.Fatalf("Versuch %d: %d", i, status)
		}
	}
	newClient(t, web.URL).expect(429, "POST", "/api/auth/passkey/begin", nil)
}

func TestPasskeysNeedPublicURL(t *testing.T) {
	_, web := setupSSOPanel(t, func(cfg *SSOConfig) { cfg.PublicURL, cfg.OIDCClientID = "", "" })
	c := newClient(t, web.URL)
	var status map[string]any
	_ = json.Unmarshal(c.expect(200, "GET", "/api/auth/status", nil), &status)
	if status["passkeys"] != false || status["oidc"] != nil || status["passwordLogin"] != true {
		t.Fatalf("ohne Einstellung: %v", status)
	}
	c.expect(404, "POST", "/api/auth/passkey/begin", nil)
	c.expect(201, "POST", "/api/auth/setup", map[string]string{"username": "admin", "password": "ein-gutes-passwort"})
	c.expect(404, "POST", "/api/me/passkeys/begin", nil)
	c.expect(404, "POST", "/api/me/oidc/link", nil)
	if target := browse(t, c, "/api/auth/oidc/start"); target != "/login/?error=oidc_off" {
		t.Fatalf("OIDC aus: %s", target)
	}
}

func TestPendingCeremoniesAreCappedPerAddress(t *testing.T) {
	_, web := setupSSOPanel(t, func(*SSOConfig) {})
	c := newClient(t, web.URL)
	for i := 0; i < maxFlowsPerIP; i++ {
		c.expect(200, "POST", "/api/auth/passkey/begin", nil)
	}
	c.expect(429, "POST", "/api/auth/passkey/begin", nil)
}

func TestOIDCUsernameAndNext(t *testing.T) {
	for _, entry := range []struct {
		identity oidcIdentity
		want     string
	}{
		{oidcIdentity{Username: "Alex"}, "Alex"},
		{oidcIdentity{Email: "max.muster@example.org"}, "max.muster"},
		{oidcIdentity{Username: "ä", Name: "Anna Bauer"}, "Anna.Bauer"},
		{oidcIdentity{Username: "x"}, "benutzer"},
		{oidcIdentity{Username: strings.Repeat("a", 40)}, strings.Repeat("a", 28)},
		{oidcIdentity{Username: "--weird--name!", Email: "a@b"}, "weird--name"},
	} {
		got := oidcUsername(entry.identity)
		if got != entry.want || validateUsername(got) != nil {
			t.Errorf("%+v: %q, erwartet %q", entry.identity, got, entry.want)
		}
	}
	for next, want := range map[string]string{"/nodes/": "/nodes/", "//boese.example": "/", "/\\boese": "/", "https://boese.example": "/", "": "/", "/a\nb": "/"} {
		if got := safeNext(next); got != want {
			t.Errorf("safeNext(%q) = %q", next, got)
		}
	}
}
