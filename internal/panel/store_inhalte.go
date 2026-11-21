package panel

import (
	"database/sql"
	"encoding/json"
	"errors"
	"time"
)

// schemaInhalte sind die Tabellen für weitere Quellen (Hangar, Spigot, CurseForge) und Bausteine (Geyser, BlueMap).
const schemaInhalte = `
CREATE TABLE IF NOT EXISTS addon_sources (
  server_id TEXT NOT NULL REFERENCES servers(id) ON DELETE CASCADE,
  path TEXT NOT NULL,
  source TEXT NOT NULL,
  project_id TEXT NOT NULL,
  project_name TEXT NOT NULL DEFAULT '',
  version_id TEXT NOT NULL DEFAULT '',
  version_name TEXT NOT NULL DEFAULT '',
  link TEXT NOT NULL DEFAULT '',
  icon_url TEXT NOT NULL DEFAULT '',
  installed_at INTEGER NOT NULL,
  PRIMARY KEY (server_id, path)
);
CREATE TABLE IF NOT EXISTS server_blocks (
  server_id TEXT NOT NULL REFERENCES servers(id) ON DELETE CASCADE,
  block TEXT NOT NULL,
  port INTEGER NOT NULL DEFAULT 0,
  files TEXT NOT NULL DEFAULT '[]',
  created_at INTEGER NOT NULL,
  PRIMARY KEY (server_id, block)
);
CREATE TABLE IF NOT EXISTS project_blocks (
  project_id TEXT NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
  block TEXT NOT NULL,
  secret TEXT NOT NULL DEFAULT '',
  created_at INTEGER NOT NULL,
  PRIMARY KEY (project_id, block)
);
`

// migrateInhalte legt die Tabellen an (nur additiv).
func (s *Store) migrateInhalte() error {
	_, err := s.db.Exec(schemaInhalte)
	return err
}

// AddonSource merkt sich, aus welcher Quelle eine Datei in plugins/ oder mods/ stammt. Modrinth erkennt Dateien an
// ihrer Prüfsumme; die anderen Quellen können das nicht, deshalb steht es hier.
type AddonSource struct {
	ServerID    string `json:"-"`
	Path        string `json:"path"`
	Source      string `json:"source"`
	ProjectID   string `json:"projectId"`
	ProjectName string `json:"projectName"`
	VersionID   string `json:"versionId"`
	VersionName string `json:"versionName"`
	Link        string `json:"link"`
	IconURL     string `json:"iconUrl"`
	InstalledAt int64  `json:"installedAt"`
}

// SetAddonSource legt die Herkunft einer Datei an oder ersetzt sie.
func (s *Store) SetAddonSource(entry AddonSource) error {
	if entry.InstalledAt == 0 {
		entry.InstalledAt = time.Now().UnixMilli()
	}
	_, err := s.db.Exec(`INSERT INTO addon_sources (server_id, path, source, project_id, project_name, version_id, version_name, link, icon_url, installed_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(server_id, path) DO UPDATE SET source = excluded.source, project_id = excluded.project_id, project_name = excluded.project_name,
  version_id = excluded.version_id, version_name = excluded.version_name, link = excluded.link, icon_url = excluded.icon_url, installed_at = excluded.installed_at`,
		entry.ServerID, entry.Path, entry.Source, entry.ProjectID, entry.ProjectName, entry.VersionID, entry.VersionName, entry.Link, entry.IconURL, entry.InstalledAt)
	return err
}

// AddonSources liefert die Herkunft aller gemerkten Dateien eines Servers, Schlüssel ist der Pfad.
func (s *Store) AddonSources(serverID string) (map[string]AddonSource, error) {
	rows, err := s.db.Query(`SELECT server_id, path, source, project_id, project_name, version_id, version_name, link, icon_url, installed_at
FROM addon_sources WHERE server_id = ?`, serverID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := map[string]AddonSource{}
	for rows.Next() {
		var entry AddonSource
		if err := rows.Scan(&entry.ServerID, &entry.Path, &entry.Source, &entry.ProjectID, &entry.ProjectName, &entry.VersionID, &entry.VersionName, &entry.Link, &entry.IconURL, &entry.InstalledAt); err != nil {
			return nil, err
		}
		result[entry.Path] = entry
	}
	return result, rows.Err()
}

// DeleteAddonSource vergisst die Herkunft einer Datei.
func (s *Store) DeleteAddonSource(serverID, path string) error {
	_, err := s.db.Exec("DELETE FROM addon_sources WHERE server_id = ? AND path = ?", serverID, path)
	return err
}

// ServerBlock ist ein eingeschalteter Baustein eines Servers (Geyser, Floodgate, BlueMap). Files sind die Dateien, die
// Lodestone dafür geladen hat (zum Entfernen beim Ausschalten), Port ist der veröffentlichte Port (0 = keiner).
type ServerBlock struct {
	ServerID  string
	Block     string
	Port      int
	Files     []string
	CreatedAt int64
}

// SetServerBlock legt einen Baustein an oder ersetzt ihn.
func (s *Store) SetServerBlock(block ServerBlock) error {
	if block.CreatedAt == 0 {
		block.CreatedAt = time.Now().UnixMilli()
	}
	if block.Files == nil {
		block.Files = []string{}
	}
	files, _ := json.Marshal(block.Files)
	_, err := s.db.Exec(`INSERT INTO server_blocks (server_id, block, port, files, created_at) VALUES (?, ?, ?, ?, ?)
ON CONFLICT(server_id, block) DO UPDATE SET port = excluded.port, files = excluded.files`, block.ServerID, block.Block, block.Port, string(files), block.CreatedAt)
	return err
}

// ServerBlocks liefert die Bausteine eines Servers, Schlüssel ist der Name des Bausteins.
func (s *Store) ServerBlocks(serverID string) (map[string]ServerBlock, error) {
	rows, err := s.db.Query("SELECT server_id, block, port, files, created_at FROM server_blocks WHERE server_id = ?", serverID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := map[string]ServerBlock{}
	for rows.Next() {
		var block ServerBlock
		var files string
		if err := rows.Scan(&block.ServerID, &block.Block, &block.Port, &files, &block.CreatedAt); err != nil {
			return nil, err
		}
		_ = json.Unmarshal([]byte(files), &block.Files)
		result[block.Block] = block
	}
	return result, rows.Err()
}

// DeleteServerBlock schaltet einen Baustein eines Servers aus.
func (s *Store) DeleteServerBlock(serverID, block string) error {
	_, err := s.db.Exec("DELETE FROM server_blocks WHERE server_id = ? AND block = ?", serverID, block)
	return err
}

// ProjectBlock ist ein Baustein, der für ein ganzes Netzwerk gilt. Secret ist mit dem Schlüssel des Panels
// verschlüsselt (Floodgate-Schlüssel).
type ProjectBlock struct {
	ProjectID string
	Block     string
	Secret    string
	CreatedAt int64
}

// SetProjectBlock legt einen Netzwerk-Baustein an oder ersetzt ihn.
func (s *Store) SetProjectBlock(block ProjectBlock) error {
	if block.CreatedAt == 0 {
		block.CreatedAt = time.Now().UnixMilli()
	}
	_, err := s.db.Exec(`INSERT INTO project_blocks (project_id, block, secret, created_at) VALUES (?, ?, ?, ?)
ON CONFLICT(project_id, block) DO UPDATE SET secret = excluded.secret`, block.ProjectID, block.Block, block.Secret, block.CreatedAt)
	return err
}

// ProjectBlockByName liefert einen Netzwerk-Baustein oder ErrNotFound.
func (s *Store) ProjectBlockByName(projectID, name string) (ProjectBlock, error) {
	var block ProjectBlock
	err := s.db.QueryRow("SELECT project_id, block, secret, created_at FROM project_blocks WHERE project_id = ? AND block = ?", projectID, name).
		Scan(&block.ProjectID, &block.Block, &block.Secret, &block.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return block, ErrNotFound
	}
	return block, err
}

// DeleteProjectBlock schaltet einen Netzwerk-Baustein aus.
func (s *Store) DeleteProjectBlock(projectID, name string) error {
	_, err := s.db.Exec("DELETE FROM project_blocks WHERE project_id = ? AND block = ?", projectID, name)
	return err
}
