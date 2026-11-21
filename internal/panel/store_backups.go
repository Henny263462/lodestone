package panel

import (
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"time"
)

// schemaBackups enthält die Tabellen für externe Backup-Ziele, Backup-Einstellungen und den Zustand der Backups.
// Die Backups selbst liegen auf dem Node (lokal) bzw. im Bucket (extern); hier steht nur, was das Panel darüber weiß.
const schemaBackups = `
CREATE TABLE IF NOT EXISTS backup_targets (
  project_id TEXT PRIMARY KEY REFERENCES projects(id) ON DELETE CASCADE,
  endpoint TEXT NOT NULL,
  region TEXT NOT NULL,
  bucket TEXT NOT NULL,
  prefix TEXT NOT NULL DEFAULT '',
  access_key TEXT NOT NULL,
  secret_sealed TEXT NOT NULL,
  path_style INTEGER NOT NULL DEFAULT 1,
  updated_at INTEGER NOT NULL
);
CREATE TABLE IF NOT EXISTS backup_settings (
  server_id TEXT PRIMARY KEY REFERENCES servers(id) ON DELETE CASCADE,
  exclude TEXT NOT NULL DEFAULT '[]',
  remote_keep INTEGER NOT NULL DEFAULT 7,
  updated_at INTEGER NOT NULL
);
CREATE TABLE IF NOT EXISTS backup_records (
  server_id TEXT NOT NULL REFERENCES servers(id) ON DELETE CASCADE,
  name TEXT NOT NULL,
  project_id TEXT NOT NULL,
  label TEXT NOT NULL DEFAULT '',
  size INTEGER NOT NULL DEFAULT 0,
  created_at INTEGER NOT NULL,
  status TEXT NOT NULL,
  want_remote INTEGER NOT NULL DEFAULT 0,
  remote_state TEXT NOT NULL DEFAULT '',
  remote_key TEXT NOT NULL DEFAULT '',
  remote_target TEXT NOT NULL DEFAULT '',
  upload_id TEXT NOT NULL DEFAULT '',
  error TEXT NOT NULL DEFAULT '',
  locked INTEGER NOT NULL DEFAULT 0,
  PRIMARY KEY (server_id, name)
);
`

// BackupTarget ist das externe, S3-kompatible Ziel eines Projekts. Das Secret liegt nur verschlüsselt vor.
type BackupTarget struct {
	ProjectID    string
	Endpoint     string
	Region       string
	Bucket       string
	Prefix       string
	AccessKey    string
	SecretSealed string
	PathStyle    bool
	UpdatedAt    int64
}

// BackupSettings sind die Backup-Einstellungen eines Servers, die das Panel verwaltet.
type BackupSettings struct {
	ServerID   string
	Exclude    []string
	RemoteKeep int
}

// Zustände eines Backups (Erstellen bzw. Hochladen).
const (
	backupRunning = "running"
	backupDone    = "done"
	backupFailed  = "failed"
)

// BackupRecord ist, was das Panel über ein Backup weiß.
type BackupRecord struct {
	ServerID     string
	Name         string
	ProjectID    string
	Label        string
	Size         int64
	CreatedAt    int64
	Status       string // running, done, failed: das Erstellen des Archivs
	WantRemote   bool
	RemoteState  string // leer, running, done, failed: das Hochladen
	RemoteKey    string
	RemoteTarget string // Endpoint und Bucket, in dem das Objekt liegt
	UploadID     string
	Error        string
	Locked       bool
}

const defaultRemoteKeep = 7

func (s *Store) BackupTarget(projectID string) (BackupTarget, error) {
	var target BackupTarget
	var pathStyle int
	err := s.db.QueryRow("SELECT project_id, endpoint, region, bucket, prefix, access_key, secret_sealed, path_style, updated_at FROM backup_targets WHERE project_id = ?", projectID).
		Scan(&target.ProjectID, &target.Endpoint, &target.Region, &target.Bucket, &target.Prefix, &target.AccessKey, &target.SecretSealed, &pathStyle, &target.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return BackupTarget{}, ErrNotFound
	}
	target.PathStyle = pathStyle == 1
	return target, err
}

func (s *Store) SetBackupTarget(target BackupTarget) error {
	target.UpdatedAt = time.Now().UnixMilli()
	_, err := s.db.Exec(`INSERT INTO backup_targets (project_id, endpoint, region, bucket, prefix, access_key, secret_sealed, path_style, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(project_id) DO UPDATE SET endpoint = excluded.endpoint, region = excluded.region, bucket = excluded.bucket, prefix = excluded.prefix,
		access_key = excluded.access_key, secret_sealed = excluded.secret_sealed, path_style = excluded.path_style, updated_at = excluded.updated_at`,
		target.ProjectID, target.Endpoint, target.Region, target.Bucket, target.Prefix, target.AccessKey, target.SecretSealed, boolInt(target.PathStyle), target.UpdatedAt)
	return err
}

func (s *Store) DeleteBackupTarget(projectID string) error {
	_, err := s.db.Exec("DELETE FROM backup_targets WHERE project_id = ?", projectID)
	return err
}

// BackupSettingsFor liefert die Einstellungen eines Servers; ohne Eintrag die Standardwerte.
func (s *Store) BackupSettingsFor(serverID string) (BackupSettings, error) {
	settings := BackupSettings{ServerID: serverID, Exclude: []string{}, RemoteKeep: defaultRemoteKeep}
	var exclude string
	err := s.db.QueryRow("SELECT exclude, remote_keep FROM backup_settings WHERE server_id = ?", serverID).Scan(&exclude, &settings.RemoteKeep)
	if errors.Is(err, sql.ErrNoRows) {
		return settings, nil
	}
	if err != nil {
		return settings, err
	}
	if json.Unmarshal([]byte(exclude), &settings.Exclude) != nil || settings.Exclude == nil {
		settings.Exclude = []string{}
	}
	return settings, nil
}

func (s *Store) SetBackupSettings(settings BackupSettings) error {
	exclude, _ := json.Marshal(settings.Exclude)
	_, err := s.db.Exec(`INSERT INTO backup_settings (server_id, exclude, remote_keep, updated_at) VALUES (?, ?, ?, ?)
		ON CONFLICT(server_id) DO UPDATE SET exclude = excluded.exclude, remote_keep = excluded.remote_keep, updated_at = excluded.updated_at`,
		settings.ServerID, string(exclude), settings.RemoteKeep, time.Now().UnixMilli())
	return err
}

const backupRecordColumns = "server_id, name, project_id, label, size, created_at, status, want_remote, remote_state, remote_key, remote_target, upload_id, error, locked"

func scanBackupRecord(row interface{ Scan(...any) error }) (BackupRecord, error) {
	var record BackupRecord
	var wantRemote, locked int
	err := row.Scan(&record.ServerID, &record.Name, &record.ProjectID, &record.Label, &record.Size, &record.CreatedAt, &record.Status, &wantRemote,
		&record.RemoteState, &record.RemoteKey, &record.RemoteTarget, &record.UploadID, &record.Error, &locked)
	if errors.Is(err, sql.ErrNoRows) {
		return BackupRecord{}, ErrNotFound
	}
	record.WantRemote, record.Locked = wantRemote == 1, locked == 1
	return record, err
}

// AddBackupRecord legt einen Eintrag an; gibt es ihn schon, bleibt er, wie er ist.
func (s *Store) AddBackupRecord(record BackupRecord) error {
	_, err := s.db.Exec("INSERT OR IGNORE INTO backup_records ("+backupRecordColumns+") VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)",
		record.ServerID, record.Name, record.ProjectID, record.Label, record.Size, record.CreatedAt, record.Status, boolInt(record.WantRemote),
		record.RemoteState, record.RemoteKey, record.RemoteTarget, record.UploadID, shortError(record.Error), boolInt(record.Locked))
	return err
}

func (s *Store) BackupRecord(serverID, name string) (BackupRecord, error) {
	return scanBackupRecord(s.db.QueryRow("SELECT "+backupRecordColumns+" FROM backup_records WHERE server_id = ? AND name = ?", serverID, name))
}

// ListBackupRecords liefert die Einträge eines Servers, die neuesten zuerst.
func (s *Store) ListBackupRecords(serverID string) ([]BackupRecord, error) {
	rows, err := s.db.Query("SELECT "+backupRecordColumns+" FROM backup_records WHERE server_id = ? ORDER BY created_at DESC, name DESC", serverID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	list := []BackupRecord{}
	for rows.Next() {
		record, err := scanBackupRecord(rows)
		if err != nil {
			return nil, err
		}
		list = append(list, record)
	}
	return list, rows.Err()
}

// SetBackupStatus hält das Ergebnis des Erstellens fest.
func (s *Store) SetBackupStatus(serverID, name, status string, size int64, message string) error {
	_, err := s.db.Exec("UPDATE backup_records SET status = ?, size = CASE WHEN ? > 0 THEN ? ELSE size END, error = ? WHERE server_id = ? AND name = ?",
		status, size, size, shortError(message), serverID, name)
	return err
}

// SetBackupRemote hält den Stand des Hochladens fest.
func (s *Store) SetBackupRemote(record BackupRecord) error {
	_, err := s.db.Exec("UPDATE backup_records SET want_remote = ?, remote_state = ?, remote_key = ?, remote_target = ?, upload_id = ?, error = ? WHERE server_id = ? AND name = ?",
		boolInt(record.WantRemote), record.RemoteState, record.RemoteKey, record.RemoteTarget, record.UploadID, shortError(record.Error), record.ServerID, record.Name)
	return err
}

func (s *Store) SetBackupLocked(serverID, name string, locked bool) error {
	_, err := s.db.Exec("UPDATE backup_records SET locked = ? WHERE server_id = ? AND name = ?", boolInt(locked), serverID, name)
	return err
}

func (s *Store) DeleteBackupRecord(serverID, name string) error {
	_, err := s.db.Exec("DELETE FROM backup_records WHERE server_id = ? AND name = ?", serverID, name)
	return err
}

func shortError(message string) string {
	if len(message) > 300 {
		return strings.ToValidUTF8(message[:300], "")
	}
	return message
}
