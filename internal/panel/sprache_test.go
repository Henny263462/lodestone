package panel

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type errorBody struct {
	Error string `json:"error"`
	Code  string `json:"code"`
}

func TestUserLanguage(t *testing.T) {
	p, web, _, _ := setupPanel(t)
	admin := newClient(t, web.URL)
	admin.expect(201, "POST", "/api/auth/setup", map[string]string{"username": "admin", "password": "ein-gutes-passwort"})

	var me struct {
		ID       string `json:"id"`
		Language *string
	}
	_ = json.Unmarshal(admin.expect(200, "GET", "/api/me", nil), &me)
	if me.Language == nil || *me.Language != "" {
		t.Fatalf("ohne Wahl muss die Sprache leer sein: %+v", me.Language)
	}

	admin.expect(200, "PUT", "/api/me/language", map[string]string{"language": "en"})
	_ = json.Unmarshal(admin.expect(200, "GET", "/api/me", nil), &me)
	if me.Language == nil || *me.Language != "en" {
		t.Fatalf("Sprache nach dem Speichern: %+v", me.Language)
	}
	if stored, _ := p.store.UserLanguage(me.ID); stored != "en" {
		t.Errorf("in der Datenbank: %q", stored)
	}
	var status struct{ Language string }
	_ = json.Unmarshal(admin.expect(200, "GET", "/api/auth/status", nil), &status)
	if status.Language != "en" {
		t.Errorf("Anmeldestatus nennt die Sprache nicht: %+v", status)
	}

	// Nur bekannte Sprachen, keine fremden Felder
	admin.expect(400, "PUT", "/api/me/language", map[string]string{"language": "fr"})
	admin.expect(400, "PUT", "/api/me/language", map[string]string{"language": "EN"})
	admin.expect(400, "PUT", "/api/me/language", map[string]any{"language": "de", "userId": "jemand"})

	// Leer heißt wieder: wie der Browser
	admin.expect(200, "PUT", "/api/me/language", map[string]string{"language": ""})
	if stored, _ := p.store.UserLanguage(me.ID); stored != "" {
		t.Errorf("zurückgesetzt: %q", stored)
	}

	// Ohne Anmeldung nicht, und mit API-Schlüssel auch nicht (Kontoeinstellung)
	newClient(t, web.URL).expect(401, "PUT", "/api/me/language", map[string]string{"language": "de"})
	var created struct{ Key string }
	_ = json.Unmarshal(admin.expect(201, "POST", "/api/me/keys", map[string]any{"name": "Skript"}), &created)
	if status, _ := bearer(t, web.URL, "PUT", "/api/me/language", created.Key, map[string]string{"language": "de"}); status != http.StatusForbidden {
		t.Errorf("mit API-Schlüssel: %d, erwartet 403", status)
	}

	// Die Wahl gilt pro Benutzer
	hash, err := HashPassword("ein-gutes-passwort")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := p.store.CreateUser("lea", hash, false); err != nil {
		t.Fatal(err)
	}
	lea := newClient(t, web.URL)
	lea.expect(200, "POST", "/api/auth/login", map[string]string{"username": "lea", "password": "ein-gutes-passwort"})
	admin.expect(200, "PUT", "/api/me/language", map[string]string{"language": "de"})
	_ = json.Unmarshal(lea.expect(200, "GET", "/api/me", nil), &me)
	if me.Language == nil || *me.Language != "" {
		t.Errorf("Sprache eines anderen Benutzers: %+v", me.Language)
	}
}

// Fehler tragen neben der deutschen Meldung eine stabile Kennung, wenn es eine gibt; die Oberfläche übersetzt damit.
func TestErrorCodes(t *testing.T) {
	_, web, _, _ := setupPanel(t)
	c := newClient(t, web.URL)
	read := func(method, path string, body any) (int, errorBody) {
		t.Helper()
		status, data := c.do(method, path, body)
		var parsed errorBody
		if err := json.Unmarshal(data, &parsed); err != nil {
			t.Fatalf("%s %s: %s", method, path, data)
		}
		return status, parsed
	}

	if status, body := read("GET", "/api/projects", nil); status != 401 || body.Code != "unauthorized" || body.Error != "nicht angemeldet" {
		t.Errorf("ohne Anmeldung: %d %+v", status, body)
	}
	c.expect(201, "POST", "/api/auth/setup", map[string]string{"username": "admin", "password": "ein-gutes-passwort"})
	if status, body := read("GET", "/api/projects/gibtsnicht", nil); status != 404 || body.Code != "not_found" {
		t.Errorf("fremdes Projekt: %d %+v", status, body)
	}
	if status, body := read("PUT", "/api/me/language", map[string]any{"sprache": "de"}); status != 400 || body.Code != "invalid_request" || !strings.HasPrefix(body.Error, "ungültige Anfrage: ") {
		t.Errorf("unbekanntes Feld: %d %+v", status, body)
	}
	// Eine gewöhnliche Meldung mit 400 hat keine Kennung, die Oberfläche übersetzt sie über den Text
	if status, body := read("PUT", "/api/me/language", map[string]string{"language": "fr"}); status != 400 || body.Code != "" || body.Error == "" {
		t.Errorf("unbekannte Sprache: %d %+v", status, body)
	}

	cases := []struct {
		err    error
		status int
		code   string
	}{
		{errForbidden, 403, "forbidden"},
		{fail(http.StatusTooManyRequests, "zu viele Anfragen"), 429, "too_many_requests"},
		{ErrNodeOffline, 502, "node_offline"},
		{ErrNotFound, 404, "not_found"},
		{&NodeError{Status: 409, Message: "der Server läuft nicht"}, 409, ""},
		{errors.New("kaputt"), 500, "internal"},
	}
	for _, tc := range cases {
		recorder := httptest.NewRecorder()
		writeError(recorder, tc.err)
		var body errorBody
		_ = json.Unmarshal(recorder.Body.Bytes(), &body)
		if recorder.Code != tc.status || body.Code != tc.code || body.Error == "" {
			t.Errorf("%v: %d %+v, erwartet %d %q", tc.err, recorder.Code, body, tc.status, tc.code)
		}
	}
}

func TestPermissionGroupIDs(t *testing.T) {
	known := map[string]bool{"console": true, "files": true, "backups": true, "server": true, "project": true}
	names := map[string]string{}
	for _, info := range permissionCatalog {
		if !known[info.GroupID] {
			t.Errorf("%s: unbekannte Gruppe %q", info.ID, info.GroupID)
		}
		if previous, ok := names[info.GroupID]; ok && previous != info.Group {
			t.Errorf("Gruppe %s hat zwei Namen: %q und %q", info.GroupID, previous, info.Group)
		}
		names[info.GroupID] = info.Group
	}
}
