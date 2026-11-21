package panel

import (
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// schemaNodeLink enthält die Tabelle der einmaligen Join-Tokens. Ein Node, der sich selbst zum Panel verbindet, hat in
// nodes eine leere Adresse und nach dem Koppeln den Hash seines Geheimnisses in nodes.link_secret_hash.
const schemaNodeLink = `
CREATE TABLE IF NOT EXISTS node_join_tokens (
  node_id TEXT PRIMARY KEY REFERENCES nodes(id) ON DELETE CASCADE,
  token_hash TEXT NOT NULL UNIQUE,
  created_at INTEGER NOT NULL,
  expires_at INTEGER NOT NULL
);
`

// nodeLinkRow ist der gespeicherte Kopplungszustand eines verbundenen Nodes (ohne Geheimnisse).
type nodeLinkRow struct {
	NodeID string
	// JoinedAt ist der Zeitpunkt der letzten erfolgreichen Kopplung (0 = noch nie gekoppelt).
	JoinedAt int64
	// JoinExpiresAt ist das Ablaufdatum eines offenen Join-Tokens (0 = keins offen).
	JoinExpiresAt int64
}

// initNodeLink legt die Tabellen der Node-Verbindung an.
func (s *Store) initNodeLink() error {
	if _, err := s.db.Exec(schemaNodeLink); err != nil {
		return fmt.Errorf("Schema der Node-Verbindung anlegen: %w", err)
	}
	return nil
}

// CreateLinkedNode legt einen Node an, der sich selbst zum Panel verbindet, samt seinem ersten Join-Token.
func (s *Store) CreateLinkedNode(name, publicHost, joinHash string, expiresAt int64) (Node, error) {
	node := Node{ID: newID(), Name: name, PublicHost: publicHost, CreatedAt: time.Now().UnixMilli()}
	tx, err := s.db.Begin()
	if err != nil {
		return Node{}, err
	}
	defer tx.Rollback()
	_, err = tx.Exec("INSERT INTO nodes ("+nodeColumns+") VALUES (?, ?, '', '', ?, ?)", node.ID, node.Name, node.PublicHost, node.CreatedAt)
	if isUnique(err) {
		return Node{}, fmt.Errorf("%w: der Name ist vergeben", ErrExists)
	}
	if err != nil {
		return Node{}, err
	}
	if _, err := tx.Exec("INSERT INTO node_join_tokens (node_id, token_hash, created_at, expires_at) VALUES (?, ?, ?, ?)", node.ID, joinHash, node.CreatedAt, expiresAt); err != nil {
		return Node{}, err
	}
	return node, tx.Commit()
}

// SetJoinToken ersetzt das offene Join-Token eines verbundenen Nodes.
func (s *Store) SetJoinToken(nodeID, joinHash string, expiresAt int64) error {
	_, err := s.db.Exec(`INSERT INTO node_join_tokens (node_id, token_hash, created_at, expires_at) VALUES (?, ?, ?, ?)
		ON CONFLICT(node_id) DO UPDATE SET token_hash = excluded.token_hash, created_at = excluded.created_at, expires_at = excluded.expires_at`,
		nodeID, joinHash, time.Now().UnixMilli(), expiresAt)
	return err
}

// RedeemJoinToken löst ein gültiges Join-Token ein: Der Node bekommt den Hash seines neuen Geheimnisses, das Token ist
// danach verbraucht. Unbekannte oder abgelaufene Tokens liefern ErrNotFound.
func (s *Store) RedeemJoinToken(joinHash, secretHash string, now time.Time) (Node, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return Node{}, err
	}
	defer tx.Rollback()
	var nodeID string
	err = tx.QueryRow("SELECT node_id FROM node_join_tokens WHERE token_hash = ? AND expires_at > ?", joinHash, now.UnixMilli()).Scan(&nodeID)
	if errors.Is(err, sql.ErrNoRows) {
		return Node{}, ErrNotFound
	}
	if err != nil {
		return Node{}, err
	}
	result, err := tx.Exec("UPDATE nodes SET link_secret_hash = ?, link_joined_at = ? WHERE id = ? AND url = ''", secretHash, now.UnixMilli(), nodeID)
	if err != nil {
		return Node{}, err
	}
	if changed, _ := result.RowsAffected(); changed != 1 {
		return Node{}, ErrNotFound
	}
	if _, err := tx.Exec("DELETE FROM node_join_tokens WHERE node_id = ?", nodeID); err != nil {
		return Node{}, err
	}
	node, err := scanNode(tx.QueryRow("SELECT "+nodeColumns+" FROM nodes WHERE id = ?", nodeID))
	if err != nil {
		return Node{}, err
	}
	return node, tx.Commit()
}

// NodeLinkSecretHash liefert den Hash des Geheimnisses eines gekoppelten Nodes. Nodes mit Adresse (direkter Zugriff)
// und noch nicht gekoppelte Nodes liefern ErrNotFound.
func (s *Store) NodeLinkSecretHash(nodeID string) (string, error) {
	var hash string
	err := s.db.QueryRow("SELECT link_secret_hash FROM nodes WHERE id = ? AND url = ''", nodeID).Scan(&hash)
	if errors.Is(err, sql.ErrNoRows) || (err == nil && hash == "") {
		return "", ErrNotFound
	}
	return hash, err
}

// ListNodeLinks liefert den Kopplungszustand aller Nodes, die sich selbst verbinden.
func (s *Store) ListNodeLinks() ([]nodeLinkRow, error) {
	rows, err := s.db.Query(`SELECT n.id, n.link_joined_at, COALESCE(j.expires_at, 0) FROM nodes n
		LEFT JOIN node_join_tokens j ON j.node_id = n.id WHERE n.url = '' ORDER BY n.name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	list := []nodeLinkRow{}
	for rows.Next() {
		var row nodeLinkRow
		if err := rows.Scan(&row.NodeID, &row.JoinedAt, &row.JoinExpiresAt); err != nil {
			return nil, err
		}
		list = append(list, row)
	}
	return list, rows.Err()
}
