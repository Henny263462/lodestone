package panel

import (
	"database/sql"
	"encoding/json"
	"errors"
)

// Die angepassten Rechte eines Mitglieds stehen als JSON-Liste in members.permissions (Spalte in moreColumns).
// Leer heißt: Das Mitglied hat genau die Vorlage seiner Rolle.

func encodePermissions(list []Permission) (string, error) {
	if list == nil {
		return "", nil
	}
	data, err := json.Marshal(list)
	return string(data), err
}

func decodePermissions(text string) []Permission {
	if text == "" {
		return nil
	}
	list := []Permission{}
	if err := json.Unmarshal([]byte(text), &list); err != nil {
		return []Permission{} // kaputte Liste: lieber keine Rechte als zu viele
	}
	return list
}

// MemberAccess liefert Rolle und angepasste Rechte (nil = Vorlage der Rolle) eines Mitglieds.
func (s *Store) MemberAccess(projectID, userID string) (string, []Permission, error) {
	var role, perms string
	err := s.db.QueryRow("SELECT role, permissions FROM members WHERE project_id = ? AND user_id = ?", projectID, userID).Scan(&role, &perms)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil, ErrNotFound
	}
	if err != nil {
		return "", nil, err
	}
	return role, decodePermissions(perms), nil
}

// SetMemberAccess legt ein Mitglied an oder ändert Rolle und Rechte zusammen. perms nil heißt: Vorlage der Rolle.
func (s *Store) SetMemberAccess(projectID, userID, role string, perms []Permission) error {
	text, err := encodePermissions(perms)
	if err != nil {
		return err
	}
	_, err = s.db.Exec("INSERT INTO members (project_id, user_id, role, permissions) VALUES (?, ?, ?, ?) ON CONFLICT(project_id, user_id) DO UPDATE SET role = excluded.role, permissions = excluded.permissions",
		projectID, userID, role, text)
	return err
}

// MemberPermissions liefert die angepassten Rechte aller Mitglieder eines Projekts (nur die mit Anpassung).
func (s *Store) MemberPermissions(projectID string) (map[string][]Permission, error) {
	rows, err := s.db.Query("SELECT user_id, permissions FROM members WHERE project_id = ? AND permissions != ''", projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := map[string][]Permission{}
	for rows.Next() {
		var userID, perms string
		if err := rows.Scan(&userID, &perms); err != nil {
			return nil, err
		}
		result[userID] = decodePermissions(perms)
	}
	return result, rows.Err()
}
