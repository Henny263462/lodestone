package panel

import (
	"database/sql"
	"errors"
)

// AdminOverview fasst die Zähler aller Kernbereiche für die Übersicht des Administrators zusammen.
type AdminOverview struct {
	Users         int `json:"users"`
	Admins        int `json:"admins"`
	DisabledUsers int `json:"disabledUsers"`
	Projects      int `json:"projects"`
	Servers       int `json:"servers"`
	Nodes         int `json:"nodes"`
	Members       int `json:"members"`
	Sessions      int `json:"sessions"`
}

// AdminUser ist die erweiterte Benutzeransicht für Administratoren mit Projekt- und Sitzungszahlen.
type AdminUser struct {
	ID           string `json:"id"`
	Username     string `json:"username"`
	IsAdmin      bool   `json:"isAdmin"`
	Disabled     bool   `json:"disabled"`
	CreatedAt    int64  `json:"createdAt"`
	TotpEnabled  bool   `json:"totpEnabled"`
	ProjectCount int    `json:"projectCount"`
	SessionCount int    `json:"sessionCount"`
}

// AdminUserProject beschreibt die Mitgliedschaft eines Benutzers in einem Projekt.
type AdminUserProject struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Kind string `json:"kind"`
	Role string `json:"role"`
}

// AdminUserDetail enthält die Daten des Benutzers sowie seine Projektmitgliedschaften.
type AdminUserDetail struct {
	User     AdminUser          `json:"user"`
	Projects []AdminUserProject `json:"projects"`
}

// AdminProject liefert alle für Administratoren relevanten Projektdetails inklusive Eigentümer und Zähler.
type AdminProject struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Kind        string `json:"kind"`
	Description string `json:"description"`
	CreatedAt   int64  `json:"createdAt"`
	OwnerID     string `json:"ownerId"`
	OwnerName   string `json:"ownerName"`
	MemberCount int    `json:"memberCount"`
	ServerCount int    `json:"serverCount"`
}

// AdminOverview liefert die Zähler für Benutzer, Administratoren, deaktivierte Konten, Projekte, Server, Nodes, Mitglieder und Sitzungen.
func (s *Store) AdminOverview() (AdminOverview, error) {
	var overview AdminOverview
	query := `
SELECT
  (SELECT COUNT(*) FROM users),
  (SELECT COUNT(*) FROM users WHERE is_admin = 1),
  (SELECT COUNT(*) FROM users WHERE disabled = 1),
  (SELECT COUNT(*) FROM projects),
  (SELECT COUNT(*) FROM servers),
  (SELECT COUNT(*) FROM nodes),
  (SELECT COUNT(*) FROM members),
  (SELECT COUNT(*) FROM sessions)
`
	err := s.db.QueryRow(query).Scan(
		&overview.Users,
		&overview.Admins,
		&overview.DisabledUsers,
		&overview.Projects,
		&overview.Servers,
		&overview.Nodes,
		&overview.Members,
		&overview.Sessions,
	)
	return overview, err
}

// AdminListUsers liefert alle Benutzer mit Projekt- und Sitzungszählern in einer einzelnen Abfrage, sortiert nach Benutzername.
func (s *Store) AdminListUsers() ([]AdminUser, error) {
	query := `
SELECT
  u.id,
  u.username,
  u.is_admin,
  u.disabled,
  u.created_at,
  u.totp_enabled,
  (SELECT COUNT(*) FROM members m WHERE m.user_id = u.id) AS project_count,
  (SELECT COUNT(*) FROM sessions s WHERE s.user_id = u.id) AS session_count
FROM users u
ORDER BY u.username COLLATE NOCASE
`
	rows, err := s.db.Query(query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	users := []AdminUser{}
	for rows.Next() {
		var u AdminUser
		var admin, disabled, totp int
		if err := rows.Scan(&u.ID, &u.Username, &admin, &disabled, &u.CreatedAt, &totp, &u.ProjectCount, &u.SessionCount); err != nil {
			return nil, err
		}
		u.IsAdmin = admin == 1
		u.Disabled = disabled == 1
		u.TotpEnabled = totp == 1
		users = append(users, u)
	}
	return users, rows.Err()
}

// AdminUserByID liefert einen Benutzer mit seinen Zählern und Projektmitgliedschaften.
func (s *Store) AdminUserByID(id string) (AdminUserDetail, error) {
	userQuery := `
SELECT
  u.id,
  u.username,
  u.is_admin,
  u.disabled,
  u.created_at,
  u.totp_enabled,
  (SELECT COUNT(*) FROM members m WHERE m.user_id = u.id) AS project_count,
  (SELECT COUNT(*) FROM sessions s WHERE s.user_id = u.id) AS session_count
FROM users u
WHERE u.id = ?
`
	var detail AdminUserDetail
	detail.Projects = []AdminUserProject{}

	var admin, disabled, totp int
	err := s.db.QueryRow(userQuery, id).Scan(
		&detail.User.ID,
		&detail.User.Username,
		&admin,
		&disabled,
		&detail.User.CreatedAt,
		&totp,
		&detail.User.ProjectCount,
		&detail.User.SessionCount,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return detail, ErrNotFound
	}
	if err != nil {
		return detail, err
	}
	detail.User.IsAdmin = admin == 1
	detail.User.Disabled = disabled == 1
	detail.User.TotpEnabled = totp == 1

	projectsQuery := `
SELECT p.id, p.name, p.kind, m.role
FROM projects p
JOIN members m ON m.project_id = p.id
WHERE m.user_id = ?
ORDER BY p.name COLLATE NOCASE
`
	rows, err := s.db.Query(projectsQuery, id)
	if err != nil {
		return detail, err
	}
	defer rows.Close()

	for rows.Next() {
		var p AdminUserProject
		if err := rows.Scan(&p.ID, &p.Name, &p.Kind, &p.Role); err != nil {
			return detail, err
		}
		detail.Projects = append(detail.Projects, p)
	}
	return detail, rows.Err()
}

// AdminListProjects liefert alle Projekte mit Eigentümernamen sowie Mitglieder- und Serverzählern, sortiert nach Name.
func (s *Store) AdminListProjects() ([]AdminProject, error) {
	query := `
SELECT
  p.id,
  p.name,
  p.kind,
  p.description,
  p.created_at,
  p.owner_id,
  COALESCE(u.username, '') AS owner_name,
  (SELECT COUNT(*) FROM members m WHERE m.project_id = p.id) AS member_count,
  (SELECT COUNT(*) FROM servers s WHERE s.project_id = p.id) AS server_count
FROM projects p
LEFT JOIN users u ON u.id = p.owner_id
ORDER BY p.name COLLATE NOCASE
`
	rows, err := s.db.Query(query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	projects := []AdminProject{}
	for rows.Next() {
		var p AdminProject
		if err := rows.Scan(
			&p.ID,
			&p.Name,
			&p.Kind,
			&p.Description,
			&p.CreatedAt,
			&p.OwnerID,
			&p.OwnerName,
			&p.MemberCount,
			&p.ServerCount,
		); err != nil {
			return nil, err
		}
		projects = append(projects, p)
	}
	return projects, rows.Err()
}
