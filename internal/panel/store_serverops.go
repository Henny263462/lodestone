package panel

import (
	"database/sql"
	"encoding/json"
	"errors"
	"time"
)

// schemaServerOps ist die Tabelle der Kopier- und Umzugsvorgänge (OpenStore führt sie mit schemaMore aus).
const schemaServerOps = `
CREATE TABLE IF NOT EXISTS server_ops (
  id TEXT PRIMARY KEY,
  project_id TEXT NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
  server_id TEXT NOT NULL DEFAULT '',
  kind TEXT NOT NULL,
  state TEXT NOT NULL,
  label TEXT NOT NULL DEFAULT '',
  step TEXT NOT NULL DEFAULT '',
  done INTEGER NOT NULL DEFAULT 0,
  total INTEGER NOT NULL DEFAULT 0,
  message TEXT NOT NULL DEFAULT '',
  warnings TEXT NOT NULL DEFAULT '',
  source_node_id TEXT NOT NULL DEFAULT '',
  target_node_id TEXT NOT NULL DEFAULT '',
  result_server_id TEXT NOT NULL DEFAULT '',
  cancel INTEGER NOT NULL DEFAULT 0,
  user_id TEXT NOT NULL DEFAULT '',
  created_at INTEGER NOT NULL,
  updated_at INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS server_ops_project ON server_ops(project_id, created_at);
`

// Zustände eines Vorgangs.
const (
	OpRunning  = "running"
	OpDone     = "done"
	OpFailed   = "failed"
	OpCanceled = "canceled"
)

// opStale ist die Zeit ohne Lebenszeichen, nach der ein laufender Vorgang als abgebrochen gilt (das Panel wurde
// währenddessen neu gestartet).
const opStale = 2 * time.Minute

// ServerOp ist ein Kopier- (clone) oder Umzugsvorgang (migrate) mit Fortschritt und Ergebnis.
type ServerOp struct {
	ID        string `json:"id"`
	ProjectID string `json:"projectId"`
	// ServerID ist der kopierte Server; bei einem Umzug der erste umziehende.
	ServerID string `json:"serverId,omitempty"`
	Kind     string `json:"kind"`
	State    string `json:"state"`
	// Label beschreibt den Vorgang für Menschen („Survival → Survival-Kopie“).
	Label string `json:"label"`
	Step  string `json:"step"`
	// Done und Total sind übertragene und erwartete Bytes (Total 0 = unbekannt).
	Done           int64    `json:"done"`
	Total          int64    `json:"total"`
	Message        string   `json:"message"`
	Warnings       []string `json:"warnings"`
	SourceNodeID   string   `json:"sourceNodeId,omitempty"`
	TargetNodeID   string   `json:"targetNodeId,omitempty"`
	ResultServerID string   `json:"resultServerId,omitempty"`
	Cancel         bool     `json:"cancelRequested"`
	UserID         string   `json:"-"`
	CreatedAt      int64    `json:"createdAt"`
	UpdatedAt      int64    `json:"updatedAt"`
}

const opColumns = "id, project_id, server_id, kind, state, label, step, done, total, message, warnings, source_node_id, target_node_id, result_server_id, cancel, user_id, created_at, updated_at"

func scanOp(row interface{ Scan(...any) error }) (ServerOp, error) {
	var op ServerOp
	var warnings string
	var cancel int
	if err := row.Scan(&op.ID, &op.ProjectID, &op.ServerID, &op.Kind, &op.State, &op.Label, &op.Step, &op.Done, &op.Total, &op.Message, &warnings,
		&op.SourceNodeID, &op.TargetNodeID, &op.ResultServerID, &cancel, &op.UserID, &op.CreatedAt, &op.UpdatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ServerOp{}, ErrNotFound
		}
		return ServerOp{}, err
	}
	op.Cancel = cancel == 1
	op.Warnings = []string{}
	if warnings != "" {
		_ = json.Unmarshal([]byte(warnings), &op.Warnings)
	}
	return op, nil
}

// ErrOpRunning meldet, dass im Projekt schon ein Vorgang läuft.
var ErrOpRunning = errors.New("im Projekt läuft schon eine Kopie oder ein Umzug – warte, bis er fertig ist")

// CreateOp legt einen laufenden Vorgang an. Pro Projekt läuft höchstens einer gleichzeitig.
func (s *Store) CreateOp(op ServerOp) (ServerOp, error) {
	now := time.Now().UnixMilli()
	op.ID, op.State, op.CreatedAt, op.UpdatedAt = newID(), OpRunning, now, now
	tx, err := s.db.Begin()
	if err != nil {
		return ServerOp{}, err
	}
	defer tx.Rollback()
	if err := expireOps(tx, now); err != nil {
		return ServerOp{}, err
	}
	var running int
	if err := tx.QueryRow("SELECT COUNT(*) FROM server_ops WHERE project_id = ? AND state = ?", op.ProjectID, OpRunning).Scan(&running); err != nil {
		return ServerOp{}, err
	}
	if running > 0 {
		return ServerOp{}, ErrOpRunning
	}
	if _, err := tx.Exec("INSERT INTO server_ops ("+opColumns+") VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)",
		op.ID, op.ProjectID, op.ServerID, op.Kind, op.State, op.Label, op.Step, op.Done, op.Total, "", "", op.SourceNodeID, op.TargetNodeID, "", 0, op.UserID, op.CreatedAt, op.UpdatedAt); err != nil {
		return ServerOp{}, err
	}
	op.Warnings = []string{}
	return op, tx.Commit()
}

// expireOps markiert Vorgänge ohne Lebenszeichen als fehlgeschlagen.
func expireOps(exec interface {
	Exec(string, ...any) (sql.Result, error)
}, now int64) error {
	_, err := exec.Exec("UPDATE server_ops SET state = ?, message = ? WHERE state = ? AND updated_at < ?",
		OpFailed, "Abgebrochen: Das Panel wurde während des Vorgangs neu gestartet. Prüfe die Server; ein Quellserver wird nur nach einem erfolgreichen Umzug gelöscht.",
		OpRunning, now-opStale.Milliseconds())
	return err
}

// OpByID liefert einen Vorgang.
func (s *Store) OpByID(id string) (ServerOp, error) {
	if err := expireOps(s.db, time.Now().UnixMilli()); err != nil {
		return ServerOp{}, err
	}
	return scanOp(s.db.QueryRow("SELECT "+opColumns+" FROM server_ops WHERE id = ?", id))
}

// ListOps liefert die letzten Vorgänge eines Projekts, den neuesten zuerst.
func (s *Store) ListOps(projectID string, limit int) ([]ServerOp, error) {
	if err := expireOps(s.db, time.Now().UnixMilli()); err != nil {
		return nil, err
	}
	rows, err := s.db.Query("SELECT "+opColumns+" FROM server_ops WHERE project_id = ? ORDER BY created_at DESC LIMIT ?", projectID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	list := []ServerOp{}
	for rows.Next() {
		op, err := scanOp(rows)
		if err != nil {
			return nil, err
		}
		list = append(list, op)
	}
	return list, rows.Err()
}

// UpdateOpProgress schreibt den Fortschritt (gleichzeitig das Lebenszeichen) und sagt, ob abgebrochen werden soll.
func (s *Store) UpdateOpProgress(id, step string, done, total int64) (bool, error) {
	if _, err := s.db.Exec("UPDATE server_ops SET step = ?, done = ?, total = ?, updated_at = ? WHERE id = ? AND state = ?",
		step, done, total, time.Now().UnixMilli(), id, OpRunning); err != nil {
		return false, err
	}
	var cancel int
	err := s.db.QueryRow("SELECT cancel FROM server_ops WHERE id = ?", id).Scan(&cancel)
	return cancel == 1, err
}

// FinishOp schließt einen Vorgang ab.
func (s *Store) FinishOp(id, state, message string, warnings []string, resultServerID string, done, total int64) error {
	if warnings == nil {
		warnings = []string{}
	}
	data, _ := json.Marshal(warnings)
	_, err := s.db.Exec("UPDATE server_ops SET state = ?, message = ?, warnings = ?, result_server_id = ?, done = ?, total = ?, step = '', updated_at = ? WHERE id = ?",
		state, message, string(data), resultServerID, done, total, time.Now().UnixMilli(), id)
	return err
}

// RequestOpCancel bittet einen laufenden Vorgang, abzubrechen.
func (s *Store) RequestOpCancel(id string) error {
	_, err := s.db.Exec("UPDATE server_ops SET cancel = 1 WHERE id = ? AND state = ?", id, OpRunning)
	return err
}

// MoveServersToNode trägt die Server auf einem neuen Node ein (alle oder keiner). Die öffentlichen Adressen gehörten
// zur alten Maschine und werden geleert.
func (s *Store) MoveServersToNode(ids []string, nodeID string) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, id := range ids {
		if _, err := tx.Exec("UPDATE servers SET node_id = ?, public_address = '', public_voice_address = '' WHERE id = ?", nodeID, id); err != nil {
			return err
		}
	}
	return tx.Commit()
}
