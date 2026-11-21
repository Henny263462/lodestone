package panel

import (
	"net/http"
	"strings"
	"time"
)

// firstPasswordWindow ist die Zeit nach einer Anmeldung, in der ein Konto ohne Passwort (angelegt über OIDC) ein
// erstes festlegen darf. Ein aktuelles Passwort gibt es nicht; die frische Anmeldung über OIDC oder einen Passkey
// ersetzt die Rückfrage, damit niemand an einer vergessenen offenen Sitzung ein Passwort setzt.
const firstPasswordWindow = 10 * time.Minute

// firstPasswordUntil sagt, bis wann (Unix-Millisekunden) die laufende Sitzung ein erstes Passwort festlegen darf;
// 0 heißt: gar nicht (das Konto hat schon ein Passwort oder die Anmeldung liegt zu lange zurück).
func (p *Panel) firstPasswordUntil(r *http.Request, user User) int64 {
	if user.PasswordHash != "" {
		return 0
	}
	cookie, err := r.Cookie(cookieName)
	if err != nil {
		return 0
	}
	created, err := p.store.SessionCreatedAt(hashToken(cookie.Value))
	if err != nil {
		return 0
	}
	if until := created + firstPasswordWindow.Milliseconds(); until > time.Now().UnixMilli() {
		return until
	}
	return 0
}

type credentials struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

func (p *Panel) authStatus(w http.ResponseWriter, r *http.Request) {
	count, err := p.store.CountUsers()
	if err != nil {
		writeError(w, err)
		return
	}
	result := map[string]any{"setupRequired": count == 0, "version": p.cfg.Version, "timezone": p.location().String()}
	p.ssoStatus(result)
	if cookie, err := r.Cookie(cookieName); err == nil {
		if user, err := p.store.SessionUser(hashToken(cookie.Value)); err == nil {
			result["user"] = user
			// Die Oberfläche stellt damit gleich nach der Anmeldeprüfung die Sprache ein ("" = wie der Browser).
			result["language"], _ = p.store.UserLanguage(user.ID)
		}
	}
	writeJSON(w, http.StatusOK, result)
}

// setup legt den ersten Administrator an. Das geht nur, solange es noch keinen Benutzer gibt.
func (p *Panel) setup(w http.ResponseWriter, r *http.Request) {
	var body credentials
	if err := decode(r, &body); err != nil {
		writeError(w, err)
		return
	}
	body.Username = strings.TrimSpace(body.Username)
	if err := validateUsername(body.Username); err != nil {
		writeError(w, fail(http.StatusBadRequest, err.Error()))
		return
	}
	if err := validatePassword(body.Password); err != nil {
		writeError(w, fail(http.StatusBadRequest, err.Error()))
		return
	}
	count, err := p.store.CountUsers()
	if err != nil {
		writeError(w, err)
		return
	}
	if count > 0 {
		writeError(w, fail(http.StatusConflict, "die Einrichtung ist schon abgeschlossen"))
		return
	}
	hash, err := HashPassword(body.Password)
	if err != nil {
		writeError(w, err)
		return
	}
	user, err := p.store.CreateUser(body.Username, hash, true)
	if err != nil {
		writeError(w, err)
		return
	}
	if err := p.startSession(w, r, user); err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, user)
}

func (p *Panel) login(w http.ResponseWriter, r *http.Request) {
	var body credentials
	if err := decode(r, &body); err != nil {
		writeError(w, err)
		return
	}
	if p.sso.oidcOnly() {
		writeError(w, p.errPasswordLoginOff())
		return
	}
	key := p.clientIP(r) + "|" + strings.ToLower(body.Username)
	ipKey := p.clientIP(r)
	if p.throttle.blocked(key) || p.throttle.blocked(ipKey+"|*") {
		writeError(w, fail(http.StatusTooManyRequests, "zu viele Fehlversuche, bitte warte ein paar Minuten"))
		return
	}
	user, err := p.store.UserByName(strings.TrimSpace(body.Username))
	hash := dummyHash
	if err == nil {
		hash = user.PasswordHash
	}
	valid := CheckPassword(body.Password, hash) && err == nil && !user.Disabled
	if !valid {
		p.throttle.fail(key)
		p.throttle.fail(ipKey + "|*")
		p.recordAudit("", strings.TrimSpace(body.Username), "Anmeldung fehlgeschlagen", "", http.StatusUnauthorized, p.clientIP(r))
		writeError(w, fail(http.StatusUnauthorized, "Benutzername oder Passwort stimmt nicht"))
		return
	}
	p.throttle.reset(key)
	if methods := p.secondFactors(user.ID); len(methods) > 0 {
		// Zweiter Schritt (Code aus der App oder Passkey): Das Ticket gilt fünf Minuten und nur für diesen Benutzer
		ticket, err := p.makeTicket(user.ID)
		if err != nil {
			writeError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"needsTotp": true, "ticket": ticket, "secondFactor": methods})
		return
	}
	if err := p.startSession(w, r, user); err != nil {
		writeError(w, err)
		return
	}
	p.recordAudit(user.ID, user.Username, "Anmeldung", "", http.StatusOK, p.clientIP(r))
	writeJSON(w, http.StatusOK, user)
}

func (p *Panel) logout(w http.ResponseWriter, r *http.Request) {
	if cookie, err := r.Cookie(cookieName); err == nil {
		_ = p.store.DeleteSession(hashToken(cookie.Value))
	}
	p.clearCookie(w)
	w.WriteHeader(http.StatusNoContent)
}

func (p *Panel) me(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, userFrom(r.Context()))
}

func (p *Panel) changeOwnPassword(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Current string `json:"current"`
		Next    string `json:"next"`
	}
	if err := decode(r, &body); err != nil {
		writeError(w, err)
		return
	}
	user := userFrom(r.Context())
	switch {
	case user.PasswordHash == "" && p.firstPasswordUntil(r, user) == 0:
		writeError(w, fail(http.StatusForbidden, "dein Konto hat noch kein Passwort. Melde dich ab und neu über OIDC oder einen Passkey an; danach kannst du 10 Minuten lang ein erstes Passwort festlegen"))
		return
	case user.PasswordHash != "" && !CheckPassword(body.Current, user.PasswordHash):
		writeError(w, fail(http.StatusForbidden, "das aktuelle Passwort stimmt nicht"))
		return
	}
	if err := validatePassword(body.Next); err != nil {
		writeError(w, fail(http.StatusBadRequest, err.Error()))
		return
	}
	hash, err := HashPassword(body.Next)
	if err != nil {
		writeError(w, err)
		return
	}
	if err := p.store.SetPassword(user.ID, hash); err != nil {
		writeError(w, err)
		return
	}
	// Alle anderen Sitzungen enden, die eigene bleibt
	_ = p.store.DeleteUserSessions(user.ID)
	if err := p.startSession(w, r, user); err != nil {
		writeError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ---------------------------------------------------------------- Benutzerverwaltung (Administratoren)

// listUsers liefert alle Benutzer; Nicht-Administratoren bekommen nur Namen (zum Einladen in Projekte ist das nötig).
func (p *Panel) listUsers(w http.ResponseWriter, r *http.Request) {
	users, err := p.store.ListUsers()
	if err != nil {
		writeError(w, err)
		return
	}
	if !userFrom(r.Context()).IsAdmin {
		type brief struct {
			ID       string `json:"id"`
			Username string `json:"username"`
		}
		list := make([]brief, 0, len(users))
		for _, user := range users {
			if !user.Disabled {
				list = append(list, brief{user.ID, user.Username})
			}
		}
		writeJSON(w, http.StatusOK, list)
		return
	}
	writeJSON(w, http.StatusOK, users)
}

func (p *Panel) createUser(w http.ResponseWriter, r *http.Request) {
	if err := requireAdmin(r); err != nil {
		writeError(w, err)
		return
	}
	var body struct {
		credentials
		IsAdmin bool `json:"isAdmin"`
	}
	if err := decode(r, &body); err != nil {
		writeError(w, err)
		return
	}
	body.Username = strings.TrimSpace(body.Username)
	if err := validateUsername(body.Username); err != nil {
		writeError(w, fail(http.StatusBadRequest, err.Error()))
		return
	}
	if err := validatePassword(body.Password); err != nil {
		writeError(w, fail(http.StatusBadRequest, err.Error()))
		return
	}
	hash, err := HashPassword(body.Password)
	if err != nil {
		writeError(w, err)
		return
	}
	user, err := p.store.CreateUser(body.Username, hash, body.IsAdmin)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, user)
}

func (p *Panel) updateUser(w http.ResponseWriter, r *http.Request) {
	if err := requireAdmin(r); err != nil {
		writeError(w, err)
		return
	}
	var body struct {
		IsAdmin  *bool `json:"isAdmin"`
		Disabled *bool `json:"disabled"`
	}
	if err := decode(r, &body); err != nil {
		writeError(w, err)
		return
	}
	user, err := p.store.UserByID(r.PathValue("id"))
	if err != nil {
		writeError(w, err)
		return
	}
	admin, disabled := user.IsAdmin, user.Disabled
	if body.IsAdmin != nil {
		admin = *body.IsAdmin
	}
	if body.Disabled != nil {
		disabled = *body.Disabled
	}
	self := userFrom(r.Context())
	if user.ID == self.ID && (!admin || disabled) {
		writeError(w, fail(http.StatusConflict, "du kannst dir die Administratorrechte nicht selbst nehmen oder dich sperren"))
		return
	}
	if err := p.store.UpdateUser(user.ID, admin, disabled); err != nil {
		writeError(w, err)
		return
	}
	if disabled {
		_ = p.store.DeleteUserSessions(user.ID)
	}
	p.sftpWake() // Sperren und Administratorrechte gelten für SFTP sofort
	user.IsAdmin, user.Disabled = admin, disabled
	writeJSON(w, http.StatusOK, user)
}

func (p *Panel) deleteUser(w http.ResponseWriter, r *http.Request) {
	if err := requireAdmin(r); err != nil {
		writeError(w, err)
		return
	}
	id := r.PathValue("id")
	if id == userFrom(r.Context()).ID {
		writeError(w, fail(http.StatusConflict, "du kannst dich nicht selbst löschen"))
		return
	}
	// Projekte, deren einziger Eigentümer er ist, würden verwaisen
	projects, err := p.store.ListProjects(id, false)
	if err != nil {
		writeError(w, err)
		return
	}
	for _, project := range projects {
		if role, _ := p.store.MemberRole(project.ID, id); role == "owner" {
			if owners, _ := p.store.CountOwners(project.ID); owners <= 1 {
				writeError(w, fail(http.StatusConflict, "er ist der einzige Eigentümer von „"+project.Name+"“ – übertrage das Projekt zuerst"))
				return
			}
		}
	}
	if err := p.store.DeleteUser(id); err != nil {
		writeError(w, err)
		return
	}
	p.sftpWake()
	w.WriteHeader(http.StatusNoContent)
}

func (p *Panel) resetPassword(w http.ResponseWriter, r *http.Request) {
	if err := requireAdmin(r); err != nil {
		writeError(w, err)
		return
	}
	var body struct {
		Password string `json:"password"`
	}
	if err := decode(r, &body); err != nil {
		writeError(w, err)
		return
	}
	if err := validatePassword(body.Password); err != nil {
		writeError(w, fail(http.StatusBadRequest, err.Error()))
		return
	}
	user, err := p.store.UserByID(r.PathValue("id"))
	if err != nil {
		writeError(w, err)
		return
	}
	hash, err := HashPassword(body.Password)
	if err != nil {
		writeError(w, err)
		return
	}
	if err := p.store.SetPassword(user.ID, hash); err != nil {
		writeError(w, err)
		return
	}
	if user.ID != userFrom(r.Context()).ID {
		_ = p.store.DeleteUserSessions(user.ID)
	}
	w.WriteHeader(http.StatusNoContent)
}
