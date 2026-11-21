package panel

import (
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// schemaAnmeldung sind die Tabellen für die Anmeldung über OIDC und Passkeys.
const schemaAnmeldung = `
CREATE TABLE IF NOT EXISTS oidc_links (
  issuer TEXT NOT NULL,
  subject TEXT NOT NULL,
  user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  email TEXT NOT NULL DEFAULT '',
  auto_created INTEGER NOT NULL DEFAULT 0,
  created_at INTEGER NOT NULL,
  last_login INTEGER NOT NULL DEFAULT 0,
  PRIMARY KEY (issuer, subject)
);
CREATE UNIQUE INDEX IF NOT EXISTS oidc_links_user ON oidc_links(user_id, issuer);
CREATE TABLE IF NOT EXISTS passkeys (
  id TEXT PRIMARY KEY,
  user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  credential_id TEXT NOT NULL UNIQUE,
  name TEXT NOT NULL,
  data TEXT NOT NULL,
  created_at INTEGER NOT NULL,
  last_used INTEGER NOT NULL DEFAULT 0
);
CREATE INDEX IF NOT EXISTS passkeys_user ON passkeys(user_id);
`

// ---------------------------------------------------------------- OIDC-Verknüpfungen

// OIDCLink verknüpft ein Konto beim Anbieter (issuer + sub) mit einem Benutzer.
type OIDCLink struct {
	Issuer      string `json:"issuer"`
	Subject     string `json:"-"`
	UserID      string `json:"-"`
	Email       string `json:"email"`
	AutoCreated bool   `json:"autoCreated"`
	CreatedAt   int64  `json:"createdAt"`
	LastLogin   int64  `json:"lastLogin"`
}

const oidcLinkColumns = "issuer, subject, user_id, email, auto_created, created_at, last_login"

func scanOIDCLink(row interface{ Scan(...any) error }) (OIDCLink, error) {
	var link OIDCLink
	var auto int
	if err := row.Scan(&link.Issuer, &link.Subject, &link.UserID, &link.Email, &auto, &link.CreatedAt, &link.LastLogin); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return OIDCLink{}, ErrNotFound
		}
		return OIDCLink{}, err
	}
	link.AutoCreated = auto == 1
	return link, nil
}

// OIDCLinkBySubject findet die Verknüpfung eines Kontos beim Anbieter.
func (s *Store) OIDCLinkBySubject(issuer, subject string) (OIDCLink, error) {
	return scanOIDCLink(s.db.QueryRow("SELECT "+oidcLinkColumns+" FROM oidc_links WHERE issuer = ? AND subject = ?", issuer, subject))
}

// OIDCLinkByUser findet die Verknüpfung eines Benutzers mit einem Anbieter.
func (s *Store) OIDCLinkByUser(userID, issuer string) (OIDCLink, error) {
	return scanOIDCLink(s.db.QueryRow("SELECT "+oidcLinkColumns+" FROM oidc_links WHERE user_id = ? AND issuer = ?", userID, issuer))
}

// LinkOIDC verknüpft einen Benutzer mit einem Konto beim Anbieter. Eine ältere Verknüpfung desselben Benutzers mit
// demselben Anbieter wird ersetzt; ein Konto, das schon zu jemand anderem gehört, ergibt ErrExists.
func (s *Store) LinkOIDC(link OIDCLink) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var owner string
	err = tx.QueryRow("SELECT user_id FROM oidc_links WHERE issuer = ? AND subject = ?", link.Issuer, link.Subject).Scan(&owner)
	switch {
	case err == nil && owner != link.UserID:
		return fmt.Errorf("%w: dieses Konto ist schon mit einem anderen Benutzer verknüpft", ErrExists)
	case err != nil && !errors.Is(err, sql.ErrNoRows):
		return err
	}
	if _, err := tx.Exec("DELETE FROM oidc_links WHERE user_id = ? AND issuer = ?", link.UserID, link.Issuer); err != nil {
		return err
	}
	if _, err := tx.Exec("INSERT INTO oidc_links ("+oidcLinkColumns+") VALUES (?, ?, ?, ?, ?, ?, ?)",
		link.Issuer, link.Subject, link.UserID, clip(link.Email, 200), boolInt(link.AutoCreated), time.Now().UnixMilli(), link.LastLogin); err != nil {
		return err
	}
	return tx.Commit()
}

// TouchOIDCLink merkt sich die letzte Anmeldung und die aktuelle E-Mail-Adresse.
func (s *Store) TouchOIDCLink(issuer, subject, email string) {
	_, _ = s.db.Exec("UPDATE oidc_links SET last_login = ?, email = ? WHERE issuer = ? AND subject = ?", time.Now().UnixMilli(), clip(email, 200), issuer, subject)
}

// UnlinkOIDC löst die Verknüpfung eines Benutzers mit einem Anbieter.
func (s *Store) UnlinkOIDC(userID, issuer string) error {
	result, err := s.db.Exec("DELETE FROM oidc_links WHERE user_id = ? AND issuer = ?", userID, issuer)
	if err != nil {
		return err
	}
	if n, _ := result.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// ---------------------------------------------------------------- Passkeys

// Passkey ist ein registrierter WebAuthn-Schlüssel. Data enthält den öffentlichen Schlüssel, Zähler und Merkmale
// (JSON von webauthn.Credential) – nichts Geheimes, verlässt das Panel aber trotzdem nicht.
type Passkey struct {
	ID           string `json:"id"`
	UserID       string `json:"-"`
	CredentialID string `json:"-"`
	Name         string `json:"name"`
	Data         string `json:"-"`
	CreatedAt    int64  `json:"createdAt"`
	LastUsed     int64  `json:"lastUsed"`
}

const passkeyColumns = "id, user_id, credential_id, name, data, created_at, last_used"

func scanPasskey(row interface{ Scan(...any) error }) (Passkey, error) {
	var key Passkey
	if err := row.Scan(&key.ID, &key.UserID, &key.CredentialID, &key.Name, &key.Data, &key.CreatedAt, &key.LastUsed); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Passkey{}, ErrNotFound
		}
		return Passkey{}, err
	}
	return key, nil
}

// CreatePasskey speichert einen neuen Passkey.
func (s *Store) CreatePasskey(key Passkey) (Passkey, error) {
	key.ID, key.CreatedAt = newID(), time.Now().UnixMilli()
	_, err := s.db.Exec("INSERT INTO passkeys ("+passkeyColumns+") VALUES (?, ?, ?, ?, ?, ?, ?)",
		key.ID, key.UserID, key.CredentialID, key.Name, key.Data, key.CreatedAt, 0)
	if isUnique(err) {
		return Passkey{}, fmt.Errorf("%w: dieser Passkey ist schon registriert", ErrExists)
	}
	return key, err
}

// ListPasskeys liefert die Passkeys eines Benutzers, neueste zuerst.
func (s *Store) ListPasskeys(userID string) ([]Passkey, error) {
	rows, err := s.db.Query("SELECT "+passkeyColumns+" FROM passkeys WHERE user_id = ? ORDER BY created_at DESC", userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	list := []Passkey{}
	for rows.Next() {
		key, err := scanPasskey(rows)
		if err != nil {
			return nil, err
		}
		list = append(list, key)
	}
	return list, rows.Err()
}

// CountPasskeys zählt die Passkeys eines Benutzers.
func (s *Store) CountPasskeys(userID string) (int, error) {
	var count int
	err := s.db.QueryRow("SELECT COUNT(*) FROM passkeys WHERE user_id = ?", userID).Scan(&count)
	return count, err
}

// RenamePasskey benennt einen Passkey des Benutzers um.
func (s *Store) RenamePasskey(userID, id, name string) error {
	result, err := s.db.Exec("UPDATE passkeys SET name = ? WHERE id = ? AND user_id = ?", name, id, userID)
	if err != nil {
		return err
	}
	if n, _ := result.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// DeletePasskey entfernt einen Passkey des Benutzers.
func (s *Store) DeletePasskey(userID, id string) error {
	result, err := s.db.Exec("DELETE FROM passkeys WHERE id = ? AND user_id = ?", id, userID)
	if err != nil {
		return err
	}
	if n, _ := result.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// DeleteUserPasskeys entfernt alle Passkeys eines Benutzers (Administrator setzt zurück).
func (s *Store) DeleteUserPasskeys(userID string) error {
	_, err := s.db.Exec("DELETE FROM passkeys WHERE user_id = ?", userID)
	return err
}

// UpdatePasskeyUse speichert Zähler und Merkmale nach einer Anmeldung.
func (s *Store) UpdatePasskeyUse(userID, credentialID, data string) error {
	_, err := s.db.Exec("UPDATE passkeys SET data = ?, last_used = ? WHERE user_id = ? AND credential_id = ?", data, time.Now().UnixMilli(), userID, credentialID)
	return err
}
