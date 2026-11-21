package panel

import (
	"database/sql"
	"encoding/json"
	"errors"
)

// schemaWelten sind die Tabellen für die eigene Ordnung der Projekte (Favoriten, Reihenfolge, Schlagwörter). Sie gehören
// dem Benutzer: Jeder ordnet seine Projekte für sich, niemand sonst sieht das.
const schemaWelten = `
CREATE TABLE IF NOT EXISTS project_prefs (
  user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  project_id TEXT NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
  favorite INTEGER NOT NULL DEFAULT 0,
  position INTEGER NOT NULL DEFAULT 0,
  tags TEXT NOT NULL DEFAULT '[]',
  PRIMARY KEY (user_id, project_id)
);
`

// ProjectPref ist die Ordnung eines Projekts für einen Benutzer. Position 0 heißt: keine eigene Reihenfolge.
type ProjectPref struct {
	ProjectID string   `json:"projectId"`
	Favorite  bool     `json:"favorite"`
	Position  int      `json:"position"`
	Tags      []string `json:"tags"`
}

func scanProjectPref(row interface{ Scan(...any) error }) (ProjectPref, error) {
	var pref ProjectPref
	var favorite int
	var tags string
	if err := row.Scan(&pref.ProjectID, &favorite, &pref.Position, &tags); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ProjectPref{}, ErrNotFound
		}
		return ProjectPref{}, err
	}
	pref.Favorite = favorite == 1
	if err := json.Unmarshal([]byte(tags), &pref.Tags); err != nil || pref.Tags == nil {
		pref.Tags = []string{}
	}
	return pref, nil
}

// ProjectPrefs liefert die Ordnung aller Projekte eines Benutzers.
func (s *Store) ProjectPrefs(userID string) ([]ProjectPref, error) {
	rows, err := s.db.Query("SELECT project_id, favorite, position, tags FROM project_prefs WHERE user_id = ? ORDER BY position, project_id", userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	prefs := []ProjectPref{}
	for rows.Next() {
		pref, err := scanProjectPref(rows)
		if err != nil {
			return nil, err
		}
		prefs = append(prefs, pref)
	}
	return prefs, rows.Err()
}

// ProjectPref liefert die Ordnung eines Projekts; gibt es noch keine, die leere.
func (s *Store) ProjectPref(userID, projectID string) (ProjectPref, error) {
	pref, err := scanProjectPref(s.db.QueryRow("SELECT project_id, favorite, position, tags FROM project_prefs WHERE user_id = ? AND project_id = ?", userID, projectID))
	if errors.Is(err, ErrNotFound) {
		return ProjectPref{ProjectID: projectID, Tags: []string{}}, nil
	}
	return pref, err
}

// SetProjectPref speichert Favorit und Schlagwörter eines Projekts (die Position bleibt).
func (s *Store) SetProjectPref(userID string, pref ProjectPref) error {
	if pref.Tags == nil {
		pref.Tags = []string{}
	}
	tags, err := json.Marshal(pref.Tags)
	if err != nil {
		return err
	}
	_, err = s.db.Exec(`INSERT INTO project_prefs (user_id, project_id, favorite, position, tags) VALUES (?, ?, ?, ?, ?)
		ON CONFLICT (user_id, project_id) DO UPDATE SET favorite = excluded.favorite, tags = excluded.tags`,
		userID, pref.ProjectID, boolInt(pref.Favorite), pref.Position, string(tags))
	return err
}

// SetProjectOrder legt die Reihenfolge fest: Das erste Projekt der Liste bekommt Position 1. Projekte, die nicht in der
// Liste stehen, verlieren ihre Position und rücken ans Ende.
func (s *Store) SetProjectOrder(userID string, projectIDs []string) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec("UPDATE project_prefs SET position = 0 WHERE user_id = ?", userID); err != nil {
		return err
	}
	for i, projectID := range projectIDs {
		if _, err := tx.Exec(`INSERT INTO project_prefs (user_id, project_id, position) VALUES (?, ?, ?)
			ON CONFLICT (user_id, project_id) DO UPDATE SET position = excluded.position`, userID, projectID, i+1); err != nil {
			return err
		}
	}
	return tx.Commit()
}
