package panel

import (
	"errors"
	"net/http"
	"strings"
)

// Permission ist ein Einzelrecht in einem Projekt, zum Beispiel "files.write".
//
// Die Rollen (viewer, operator, admin, owner) sind Vorlagen: Jede Rolle ergibt genau die Rechte, deren Mindestrolle
// sie erreicht (siehe permissionCatalog). Ein Mitglied kann statt der Vorlage eine angepasste Liste bekommen.
// Eigentümer-Dinge (Projekt löschen, Eigentum vergeben) hängen weiter an der Rolle, nicht an einem Recht.
type Permission string

// Die Einzelrechte. Neue Rechte kommen hier und in permissionCatalog dazu.
const (
	PermConsoleRead    Permission = "console.read"
	PermConsoleCommand Permission = "console.command"
	PermPower          Permission = "power"
	PermFilesRead      Permission = "files.read"
	PermFilesWrite     Permission = "files.write"
	PermSFTP           Permission = "sftp"
	PermBackupsRead    Permission = "backups.read"
	PermBackupsCreate  Permission = "backups.create"
	PermBackupsRestore Permission = "backups.restore"
	PermBackupsDelete  Permission = "backups.delete"
	PermAddons         Permission = "addons"
	PermPlayers        Permission = "players"
	PermSettings       Permission = "settings"
	PermServers        Permission = "servers"
	PermSchedules      Permission = "schedules"
	PermWebhooks       Permission = "webhooks"
	PermMembers        Permission = "members"
	PermNetwork        Permission = "network"
)

// PermissionInfo beschreibt ein Recht für die Oberfläche.
type PermissionInfo struct {
	ID          Permission `json:"id"`
	Group       string     `json:"group"`
	Label       string     `json:"label"`
	Description string     `json:"description"`
	// Role ist die kleinste Rolle, deren Vorlage das Recht enthält.
	Role string `json:"role"`
	rank int
}

// permissionCatalog ist die Liste aller Rechte in der Reihenfolge der Oberfläche. Die Mindestrolle entspricht genau
// der Stufe, die die Handler vor den Einzelrechten verlangt haben; rechte_test.go prüft das an jeder Route.
var permissionCatalog = []PermissionInfo{
	{ID: PermConsoleRead, Group: "Konsole", Label: "Konsole lesen", Description: "Sieht die Ausgabe der Konsole und das Protokoll des Servers.", rank: rankViewer},
	{ID: PermConsoleCommand, Group: "Konsole", Label: "Befehle senden", Description: "Schickt Befehle an die Konsole, zum Beispiel „op“ oder „stop“.", rank: rankOperator},
	{ID: PermPower, Group: "Konsole", Label: "Starten und Stoppen", Description: "Startet, stoppt und startet Server neu, auch Instanzen einer Gruppe.", rank: rankOperator},
	{ID: PermFilesRead, Group: "Dateien", Label: "Dateien ansehen", Description: "Öffnet und lädt Dateien des Servers herunter.", rank: rankOperator},
	{ID: PermFilesWrite, Group: "Dateien", Label: "Dateien ändern", Description: "Bearbeitet, lädt hoch, benennt um und löscht Dateien. Auch Plugins/Mods hochladen, abschalten und löschen läuft darüber.", rank: rankOperator},
	{ID: PermSFTP, Group: "Dateien", Label: "SFTP-Zugang", Description: "Darf per SFTP auf die Dateien zugreifen.", rank: rankOperator},
	{ID: PermBackupsRead, Group: "Backups", Label: "Backups ansehen", Description: "Sieht die Liste der Backups. Herunterladen braucht zusätzlich „Dateien ansehen“.", rank: rankViewer},
	{ID: PermBackupsCreate, Group: "Backups", Label: "Backups erstellen", Description: "Erstellt neue Backups.", rank: rankOperator},
	{ID: PermBackupsRestore, Group: "Backups", Label: "Backups wiederherstellen", Description: "Ersetzt alle Dateien des Servers durch ein Backup.", rank: rankOperator},
	{ID: PermBackupsDelete, Group: "Backups", Label: "Backups löschen", Description: "Löscht Backups unwiderruflich.", rank: rankOperator},
	{ID: PermAddons, Group: "Server", Label: "Plugins und Mods", Description: "Installiert, aktualisiert und wechselt Plugins/Mods über Modrinth.", rank: rankOperator},
	{ID: PermPlayers, Group: "Server", Label: "Spieler verwalten", Description: "Sieht Whitelist, Operatoren und Banns und kickt, bannt oder whitelistet Spieler.", rank: rankOperator},
	{ID: PermSettings, Group: "Server", Label: "Einstellungen", Description: "Ändert Name, Arbeitsspeicher, Startzeile, Version, Auto-Restart, öffentliche Adresse, Projektname und Statusseite.", rank: rankAdmin},
	{ID: PermServers, Group: "Server", Label: "Server anlegen und löschen", Description: "Legt neue Server an und löscht Server samt Welten und Backups.", rank: rankAdmin},
	{ID: PermSchedules, Group: "Projekt", Label: "Zeitpläne", Description: "Legt Zeitpläne an, ändert und löscht sie. Schritte gehen nur mit den passenden Rechten (Befehle, Starten, Backups).", rank: rankAdmin},
	{ID: PermWebhooks, Group: "Projekt", Label: "Webhooks", Description: "Verwaltet Webhooks, etwa für Discord.", rank: rankAdmin},
	{ID: PermMembers, Group: "Projekt", Label: "Mitglieder", Description: "Fügt Mitglieder hinzu, entfernt sie und vergibt Rollen und Rechte – nie mehr, als man selbst hat.", rank: rankAdmin},
	{ID: PermNetwork, Group: "Projekt", Label: "Netzwerk", Description: "Ändert Reihenfolge, Forced Hosts, MOTD, Voicechat, Tunnel und Gruppen (mit „Server anlegen und löschen“) des Netzwerks.", rank: rankAdmin},
}

var permissionByID = func() map[Permission]PermissionInfo {
	result := map[Permission]PermissionInfo{}
	for _, info := range permissionCatalog {
		info.Role = rankName(info.rank)
		result[info.ID] = info
	}
	return result
}()

// permissionLabel ist der deutsche Name eines Rechts (für Fehlermeldungen).
func permissionLabel(perm Permission) string {
	if info, ok := permissionByID[perm]; ok {
		return info.Label
	}
	return string(perm)
}

// Access sind die wirksamen Rechte eines Benutzers in einem Projekt.
type Access struct {
	// Rank ist die Stufe der Rolle; sie zählt weiter für Eigentümer-Dinge und die Rangordnung unter Mitgliedern.
	Rank int
	// Custom ist wahr, wenn das Mitglied eine angepasste Rechteliste statt der Vorlage seiner Rolle hat.
	Custom bool
	perms  map[Permission]bool
}

// Has sagt, ob alle genannten Rechte vorhanden sind.
func (a Access) Has(needed ...Permission) bool {
	return len(a.missing(needed)) == 0
}

func (a Access) missing(needed []Permission) []Permission {
	var result []Permission
	for _, perm := range needed {
		if !a.perms[perm] {
			result = append(result, perm)
		}
	}
	return result
}

// List liefert die Rechte in der Reihenfolge des Katalogs (nie nil).
func (a Access) List() []Permission {
	result := []Permission{}
	for _, info := range permissionCatalog {
		if a.perms[info.ID] {
			result = append(result, info.ID)
		}
	}
	return result
}

// roleTemplate sind die Rechte, die eine Rolle ohne Anpassung hat.
func roleTemplate(rank int) map[Permission]bool {
	result := map[Permission]bool{}
	for _, info := range permissionCatalog {
		if rank >= info.rank {
			result[info.ID] = true
		}
	}
	return result
}

// accessFor baut die wirksamen Rechte aus Rolle und (optional) angepasster Liste. Eigentümer haben immer alle Rechte.
func accessFor(rank int, custom []Permission) Access {
	if custom == nil || rank >= rankOwner || rank == rankNone {
		return Access{Rank: rank, perms: roleTemplate(rank)}
	}
	perms := map[Permission]bool{}
	for _, perm := range custom {
		if _, ok := permissionByID[perm]; ok {
			perms[perm] = true
		}
	}
	return Access{Rank: rank, Custom: true, perms: perms}
}

// normalizePermissions prüft eine Liste aus einer Anfrage, entfernt Doppelte und sortiert nach dem Katalog. Ist sie
// gleich der Vorlage der Rolle, ist das Ergebnis nil (das Mitglied folgt dann wieder der Vorlage).
func normalizePermissions(rank int, list []Permission) ([]Permission, error) {
	wanted := map[Permission]bool{}
	for _, perm := range list {
		if _, ok := permissionByID[perm]; !ok {
			return nil, fail(http.StatusBadRequest, "unbekanntes Recht „"+string(perm)+"“")
		}
		wanted[perm] = true
	}
	template := roleTemplate(rank)
	same := len(template) == len(wanted)
	result := []Permission{}
	for _, info := range permissionCatalog {
		if wanted[info.ID] {
			result = append(result, info.ID)
		}
		if wanted[info.ID] != template[info.ID] {
			same = false
		}
	}
	if same {
		return nil, nil
	}
	return result, nil
}

// errMissing ist die Ablehnung mit den Namen der fehlenden Rechte.
func errMissing(missing []Permission) error {
	labels := make([]string, len(missing))
	for i, perm := range missing {
		labels[i] = "„" + permissionLabel(perm) + "“"
	}
	if len(labels) == 1 {
		return fail(http.StatusForbidden, "dafür fehlt dir das Recht "+labels[0])
	}
	return fail(http.StatusForbidden, "dafür fehlen dir die Rechte "+strings.Join(labels, ", "))
}

// projectAccess liefert die Rechte des Benutzers im Projekt. Globale Administratoren sind überall Eigentümer.
// Projekte, in denen man nicht Mitglied ist, gibt es nicht (404).
func (p *Panel) projectAccess(user User, projectID string) (Access, error) {
	if user.IsAdmin {
		if _, err := p.store.ProjectByID(projectID); err != nil {
			return Access{}, err
		}
		return accessFor(rankOwner, nil), nil
	}
	role, custom, err := p.store.MemberAccess(projectID, user.ID)
	if errors.Is(err, ErrNotFound) {
		return Access{}, errNotFound
	}
	if err != nil {
		return Access{}, err
	}
	return accessFor(roleRanks[role], custom), nil
}

// requireProjectPermission prüft, dass der Benutzer Mitglied ist und alle genannten Rechte hat, und liefert das
// Projekt. Ohne Rechte genügt die Mitgliedschaft (Lesen).
func (p *Panel) requireProjectPermission(r *http.Request, projectID string, needed ...Permission) (Project, Access, error) {
	access, err := p.projectAccess(userFrom(r.Context()), projectID)
	if err != nil {
		return Project{}, Access{}, err
	}
	if missing := access.missing(needed); len(missing) > 0 {
		return Project{}, Access{}, errMissing(missing)
	}
	project, err := p.store.ProjectByID(projectID)
	return project, access, err
}

// requireServerPermission löst {id} auf und prüft die Rechte im Projekt des Servers.
func (p *Panel) requireServerPermission(r *http.Request, needed ...Permission) (ServerRow, Project, Access, error) {
	row, err := p.store.ServerByID(r.PathValue("id"))
	if err != nil {
		return ServerRow{}, Project{}, Access{}, errNotFound
	}
	project, access, err := p.requireProjectPermission(r, row.ProjectID, needed...)
	return row, project, access, err
}

// proxyPermissions sagt, welche Rechte eine zum Node durchgereichte Anfrage braucht. requiredRank bleibt die Liste
// der erlaubten Routen; kennt proxyPermissions eine Route nicht (ok = false), gilt dort weiter die Stufe aus requiredRank.
func proxyPermissions(method, rest string) (needed []Permission, ok bool) {
	parts := strings.Split(strings.Trim(rest, "/"), "/")
	switch parts[0] {
	case "players", "addons", "modpack":
		return nil, true
	case "logs":
		return []Permission{PermConsoleRead}, true
	case "power":
		return []Permission{PermPower}, true
	case "command":
		return []Permission{PermConsoleCommand}, true
	case "reinstall":
		return []Permission{PermSettings}, true
	case "files":
		if method == http.MethodGet {
			return []Permission{PermFilesRead}, true
		}
		return []Permission{PermFilesWrite}, true
	case "backups":
		switch {
		case len(parts) == 1 && method == http.MethodGet:
			return []Permission{PermBackupsRead}, true
		case len(parts) == 1 && method == http.MethodPost:
			return []Permission{PermBackupsCreate}, true
		case len(parts) == 2 && method == http.MethodDelete:
			return []Permission{PermBackupsDelete}, true
		case len(parts) == 3 && parts[2] == "download":
			// Ein Backup enthält alle Dateien des Servers
			return []Permission{PermBackupsRead, PermFilesRead}, true
		case len(parts) == 3 && parts[2] == "restore":
			return []Permission{PermBackupsRestore}, true
		}
	}
	return nil, false
}

// stepPermissions sind die Rechte, die die Schritte eines Zeitplans brauchen. Wer einen Zeitplan anlegt, ändert oder
// von Hand startet, braucht sie selbst – sonst könnte er über den Zeitplan mehr tun, als er darf.
func stepPermissions(steps []Step) []Permission {
	seen := map[Permission]bool{}
	var result []Permission
	add := func(perm Permission) {
		if !seen[perm] {
			seen[perm] = true
			result = append(result, perm)
		}
	}
	for _, step := range steps {
		switch step.Action {
		case "command":
			add(PermConsoleCommand)
		case "start", "stop", "restart":
			add(PermPower)
		case "backup":
			add(PermBackupsCreate)
		}
	}
	return result
}
