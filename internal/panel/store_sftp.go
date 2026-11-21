package panel

import (
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// schemaSFTP sind die Tabellen des SFTP-Zugangs: Passwort und Schlüssel je Benutzer, Einstellung je Server.
const schemaSFTP = `
CREATE TABLE IF NOT EXISTS sftp_credentials (
  user_id TEXT PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
  password_hash TEXT NOT NULL DEFAULT '',
  password_login INTEGER NOT NULL DEFAULT 1,
  updated_at INTEGER NOT NULL
);
CREATE TABLE IF NOT EXISTS sftp_keys (
  id TEXT PRIMARY KEY,
  user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  name TEXT NOT NULL,
  public_key TEXT NOT NULL,
  fingerprint TEXT NOT NULL,
  created_at INTEGER NOT NULL,
  last_used INTEGER NOT NULL DEFAULT 0,
  UNIQUE (user_id, fingerprint)
);
CREATE TABLE IF NOT EXISTS sftp_servers (
  server_id TEXT PRIMARY KEY REFERENCES servers(id) ON DELETE CASCADE,
  operators_read_only INTEGER NOT NULL DEFAULT 0
);
`

// SFTPKey ist ein öffentlicher SSH-Schlüssel eines Benutzers.
type SFTPKey struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	PublicKey   string `json:"publicKey"`
	Fingerprint string `json:"fingerprint"`
	CreatedAt   int64  `json:"createdAt"`
	LastUsed    int64  `json:"lastUsed"`
}

// SFTPCredential ist das SFTP-Passwort eines Benutzers (nur als Hash) und ob es gelten soll.
type SFTPCredential struct {
	PasswordHash  string
	PasswordLogin bool
}

// SFTPCredential liefert Passwort-Hash und Schalter; ohne Eintrag gibt es kein Passwort, Passwort-Login ist an.
func (s *Store) SFTPCredential(userID string) (SFTPCredential, error) {
	credential := SFTPCredential{PasswordLogin: true}
	var login int
	err := s.db.QueryRow("SELECT password_hash, password_login FROM sftp_credentials WHERE user_id = ?", userID).Scan(&credential.PasswordHash, &login)
	if errors.Is(err, sql.ErrNoRows) {
		return credential, nil
	}
	credential.PasswordLogin = login == 1
	return credential, err
}

// SetSFTPPassword setzt (oder mit "" löscht) den Hash des SFTP-Passworts.
func (s *Store) SetSFTPPassword(userID, hash string) error {
	_, err := s.db.Exec(`INSERT INTO sftp_credentials (user_id, password_hash, updated_at) VALUES (?, ?, ?)
		ON CONFLICT(user_id) DO UPDATE SET password_hash = excluded.password_hash, updated_at = excluded.updated_at`,
		userID, hash, time.Now().UnixMilli())
	return err
}

// SetSFTPPasswordLogin schaltet die Anmeldung mit Passwort für den Benutzer ein oder aus.
func (s *Store) SetSFTPPasswordLogin(userID string, on bool) error {
	_, err := s.db.Exec(`INSERT INTO sftp_credentials (user_id, password_login, updated_at) VALUES (?, ?, ?)
		ON CONFLICT(user_id) DO UPDATE SET password_login = excluded.password_login, updated_at = excluded.updated_at`,
		userID, boolInt(on), time.Now().UnixMilli())
	return err
}

const sftpKeyColumns = "id, name, public_key, fingerprint, created_at, last_used"

func scanSFTPKey(row interface{ Scan(...any) error }) (SFTPKey, error) {
	var key SFTPKey
	err := row.Scan(&key.ID, &key.Name, &key.PublicKey, &key.Fingerprint, &key.CreatedAt, &key.LastUsed)
	return key, err
}

// ListSFTPKeys liefert die Schlüssel eines Benutzers, älteste zuerst.
func (s *Store) ListSFTPKeys(userID string) ([]SFTPKey, error) {
	rows, err := s.db.Query("SELECT "+sftpKeyColumns+" FROM sftp_keys WHERE user_id = ? ORDER BY created_at", userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	keys := []SFTPKey{}
	for rows.Next() {
		key, err := scanSFTPKey(rows)
		if err != nil {
			return nil, err
		}
		keys = append(keys, key)
	}
	return keys, rows.Err()
}

// AddSFTPKey speichert einen Schlüssel; denselben Schlüssel zweimal gibt es nicht.
func (s *Store) AddSFTPKey(userID string, key SFTPKey) (SFTPKey, error) {
	key.ID, key.CreatedAt = newID(), time.Now().UnixMilli()
	_, err := s.db.Exec("INSERT INTO sftp_keys (id, user_id, name, public_key, fingerprint, created_at) VALUES (?, ?, ?, ?, ?, ?)",
		key.ID, userID, key.Name, key.PublicKey, key.Fingerprint, key.CreatedAt)
	if isUnique(err) {
		return SFTPKey{}, fmt.Errorf("%w: diesen Schlüssel hast du schon hinzugefügt", ErrExists)
	}
	return key, err
}

// DeleteSFTPKey löscht einen Schlüssel des Benutzers.
func (s *Store) DeleteSFTPKey(userID, id string) error {
	result, err := s.db.Exec("DELETE FROM sftp_keys WHERE id = ? AND user_id = ?", id, userID)
	if err != nil {
		return err
	}
	if count, _ := result.RowsAffected(); count == 0 {
		return ErrNotFound
	}
	return nil
}

// TouchSFTPKey merkt sich die letzte Benutzung eines Schlüssels.
func (s *Store) TouchSFTPKey(userID, fingerprint string, at int64) error {
	_, err := s.db.Exec("UPDATE sftp_keys SET last_used = ? WHERE user_id = ? AND fingerprint = ? AND last_used < ?", at, userID, fingerprint, at)
	return err
}

// SFTPOperatorsReadOnly sagt, ob Operatoren eines Servers über SFTP nur lesen dürfen.
func (s *Store) SFTPOperatorsReadOnly(serverID string) (bool, error) {
	var on int
	err := s.db.QueryRow("SELECT operators_read_only FROM sftp_servers WHERE server_id = ?", serverID).Scan(&on)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	return on == 1, err
}

// SetSFTPOperatorsReadOnly stellt ein, ob Operatoren eines Servers über SFTP nur lesen dürfen.
func (s *Store) SetSFTPOperatorsReadOnly(serverID string, on bool) error {
	_, err := s.db.Exec(`INSERT INTO sftp_servers (server_id, operators_read_only) VALUES (?, ?)
		ON CONFLICT(server_id) DO UPDATE SET operators_read_only = excluded.operators_read_only`, serverID, boolInt(on))
	return err
}

// LatestSFTPAudit ist die Zeit des neuesten SFTP-Eintrags im Protokoll (0, wenn es keinen gibt).
func (s *Store) LatestSFTPAudit() int64 {
	var at int64
	_ = s.db.QueryRow("SELECT COALESCE(MAX(ts), 0) FROM audit WHERE action LIKE 'SFTP-%'").Scan(&at)
	return at
}

// sftpSnapshot ist alles, was der Abgleich der Zugänge braucht, in wenigen Abfragen gelesen.
type sftpSnapshot struct {
	users       []User
	members     map[string]map[string]string       // Projekt → Benutzer → Rolle
	custom      map[string]map[string][]Permission // Projekt → Benutzer → angepasste Rechte (fehlt = Vorlage der Rolle)
	credentials map[string]SFTPCredential          // Benutzer → Passwort
	keys        map[string][]string                // Benutzer → öffentliche Schlüssel
	readOnly    map[string]bool                    // Server → Operatoren nur lesend
	servers     []ServerRow
}

func (s *Store) sftpSnapshot() (sftpSnapshot, error) {
	snapshot := sftpSnapshot{members: map[string]map[string]string{}, custom: map[string]map[string][]Permission{}, credentials: map[string]SFTPCredential{}, keys: map[string][]string{}, readOnly: map[string]bool{}}
	var err error
	if snapshot.users, err = s.ListUsers(); err != nil {
		return snapshot, err
	}
	if snapshot.servers, err = s.ListServers(""); err != nil {
		return snapshot, err
	}
	rows, err := s.db.Query("SELECT project_id, user_id, role, permissions FROM members")
	if err != nil {
		return snapshot, err
	}
	for rows.Next() {
		var project, user, role, perms string
		if err := rows.Scan(&project, &user, &role, &perms); err != nil {
			rows.Close()
			return snapshot, err
		}
		if snapshot.members[project] == nil {
			snapshot.members[project] = map[string]string{}
		}
		snapshot.members[project][user] = role
		if custom := decodePermissions(perms); custom != nil {
			if snapshot.custom[project] == nil {
				snapshot.custom[project] = map[string][]Permission{}
			}
			snapshot.custom[project][user] = custom
		}
	}
	rows.Close()
	rows, err = s.db.Query("SELECT user_id, password_hash, password_login FROM sftp_credentials")
	if err != nil {
		return snapshot, err
	}
	for rows.Next() {
		var user string
		var credential SFTPCredential
		var login int
		if err := rows.Scan(&user, &credential.PasswordHash, &login); err != nil {
			rows.Close()
			return snapshot, err
		}
		credential.PasswordLogin = login == 1
		snapshot.credentials[user] = credential
	}
	rows.Close()
	rows, err = s.db.Query("SELECT user_id, public_key FROM sftp_keys ORDER BY created_at")
	if err != nil {
		return snapshot, err
	}
	for rows.Next() {
		var user, key string
		if err := rows.Scan(&user, &key); err != nil {
			rows.Close()
			return snapshot, err
		}
		snapshot.keys[user] = append(snapshot.keys[user], key)
	}
	rows.Close()
	rows, err = s.db.Query("SELECT server_id FROM sftp_servers WHERE operators_read_only = 1")
	if err != nil {
		return snapshot, err
	}
	defer rows.Close()
	for rows.Next() {
		var server string
		if err := rows.Scan(&server); err != nil {
			return snapshot, err
		}
		snapshot.readOnly[server] = true
	}
	return snapshot, rows.Err()
}
