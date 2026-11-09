package panel

import (
	"net/http"
)

// adminOverview liefert statistische Kennzahlen über das gesamte Panel.
func (p *Panel) adminOverview(w http.ResponseWriter, r *http.Request) {
	if err := requireAdmin(r); err != nil {
		writeError(w, err)
		return
	}
	overview, err := p.store.AdminOverview()
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, overview)
}

// adminListUsers liefert alle Benutzer mit Projekt- und Sitzungszahlen.
func (p *Panel) adminListUsers(w http.ResponseWriter, r *http.Request) {
	if err := requireAdmin(r); err != nil {
		writeError(w, err)
		return
	}
	users, err := p.store.AdminListUsers()
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, users)
}

// adminGetUser liefert die Details eines Benutzers inklusive seiner Projektzugehörigkeiten.
func (p *Panel) adminGetUser(w http.ResponseWriter, r *http.Request) {
	if err := requireAdmin(r); err != nil {
		writeError(w, err)
		return
	}
	detail, err := p.store.AdminUserByID(r.PathValue("id"))
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, detail)
}

// adminDeleteUserSessions beendet alle aktiven Sitzungen des angegebenen Benutzers.
func (p *Panel) adminDeleteUserSessions(w http.ResponseWriter, r *http.Request) {
	if err := requireAdmin(r); err != nil {
		writeError(w, err)
		return
	}
	id := r.PathValue("id")
	if _, err := p.store.UserByID(id); err != nil {
		writeError(w, err)
		return
	}
	// Es ist ausdrücklich erlaubt, die eigenen Sitzungen zu beenden; der Administrator wird dadurch abgemeldet.
	if err := p.store.DeleteUserSessions(id); err != nil {
		writeError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// adminListProjects liefert alle Projekte mit Eigentümer und Zählern für Mitglieder und Server.
func (p *Panel) adminListProjects(w http.ResponseWriter, r *http.Request) {
	if err := requireAdmin(r); err != nil {
		writeError(w, err)
		return
	}
	projects, err := p.store.AdminListProjects()
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, projects)
}
