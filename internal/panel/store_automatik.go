package panel

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

// schemaAutomatik sind die Tabellen für Konsolen-Auslöser und den Discord-Bot.
const schemaAutomatik = `
CREATE TABLE IF NOT EXISTS console_triggers (
  id TEXT PRIMARY KEY,
  project_id TEXT NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
  server_id TEXT NOT NULL DEFAULT '',
  name TEXT NOT NULL,
  pattern TEXT NOT NULL,
  level TEXT NOT NULL DEFAULT 'info',
  schedule_id TEXT NOT NULL DEFAULT '',
  enabled INTEGER NOT NULL DEFAULT 1,
  last_match INTEGER NOT NULL DEFAULT 0,
  last_line TEXT NOT NULL DEFAULT '',
  created_at INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS console_triggers_project ON console_triggers(project_id);
CREATE TABLE IF NOT EXISTS discord_bots (
  project_id TEXT PRIMARY KEY REFERENCES projects(id) ON DELETE CASCADE,
  application_id TEXT NOT NULL DEFAULT '',
  public_key TEXT NOT NULL DEFAULT '',
  bot_token TEXT NOT NULL DEFAULT '',
  rules TEXT NOT NULL DEFAULT '[]',
  enabled INTEGER NOT NULL DEFAULT 1,
  registered_at INTEGER NOT NULL DEFAULT 0,
  updated_at INTEGER NOT NULL
);
`

// migrateAutomatik legt die Tabellen an (OpenStore ruft es auf).
func (s *Store) migrateAutomatik() error {
	if _, err := s.db.Exec(schemaAutomatik); err != nil {
		return fmt.Errorf("Schema anlegen: %w", err)
	}
	return nil
}

// ---------------------------------------------------------------- Konsolen-Auslöser

// ConsoleTrigger ist ein Muster, das in der Konsole eines Servers ein Ereignis (und auf Wunsch einen Zeitplan) auslöst.
type ConsoleTrigger struct {
	ID        string `json:"id"`
	ProjectID string `json:"projectId"`
	// ServerID leer heißt: alle Server des Projekts.
	ServerID string `json:"serverId"`
	Name     string `json:"name"`
	Pattern  string `json:"pattern"`
	// Level: info, warn oder error.
	Level string `json:"level"`
	// ScheduleID ist ein Zeitplan, der beim Treffer läuft (leer = keiner).
	ScheduleID string `json:"scheduleId"`
	Enabled    bool   `json:"enabled"`
	LastMatch  int64  `json:"lastMatch"`
	LastLine   string `json:"lastLine"`
	CreatedAt  int64  `json:"createdAt"`
}

const triggerColumns = "id, project_id, server_id, name, pattern, level, schedule_id, enabled, last_match, last_line, created_at"

func scanTrigger(row interface{ Scan(...any) error }) (ConsoleTrigger, error) {
	var trigger ConsoleTrigger
	var enabled int
	if err := row.Scan(&trigger.ID, &trigger.ProjectID, &trigger.ServerID, &trigger.Name, &trigger.Pattern, &trigger.Level, &trigger.ScheduleID,
		&enabled, &trigger.LastMatch, &trigger.LastLine, &trigger.CreatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ConsoleTrigger{}, ErrNotFound
		}
		return ConsoleTrigger{}, err
	}
	trigger.Enabled = enabled == 1
	return trigger, nil
}

// CreateTrigger legt einen Konsolen-Auslöser an.
func (s *Store) CreateTrigger(trigger ConsoleTrigger) (ConsoleTrigger, error) {
	trigger.ID, trigger.CreatedAt = newID(), time.Now().UnixMilli()
	_, err := s.db.Exec("INSERT INTO console_triggers ("+triggerColumns+") VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)",
		trigger.ID, trigger.ProjectID, trigger.ServerID, trigger.Name, trigger.Pattern, trigger.Level, trigger.ScheduleID, boolInt(trigger.Enabled), 0, "", trigger.CreatedAt)
	return trigger, err
}

// TriggerByID liefert einen Konsolen-Auslöser.
func (s *Store) TriggerByID(id string) (ConsoleTrigger, error) {
	return scanTrigger(s.db.QueryRow("SELECT "+triggerColumns+" FROM console_triggers WHERE id = ?", id))
}

// ListTriggers liefert die Auslöser eines Projekts (projectID leer = alle).
func (s *Store) ListTriggers(projectID string) ([]ConsoleTrigger, error) {
	query, args := "SELECT "+triggerColumns+" FROM console_triggers ORDER BY created_at", []any{}
	if projectID != "" {
		query, args = "SELECT "+triggerColumns+" FROM console_triggers WHERE project_id = ? ORDER BY created_at", []any{projectID}
	}
	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	list := []ConsoleTrigger{}
	for rows.Next() {
		trigger, err := scanTrigger(rows)
		if err != nil {
			return nil, err
		}
		list = append(list, trigger)
	}
	return list, rows.Err()
}

// UpdateTrigger speichert die Einstellungen eines Auslösers.
func (s *Store) UpdateTrigger(trigger ConsoleTrigger) error {
	_, err := s.db.Exec("UPDATE console_triggers SET server_id = ?, name = ?, pattern = ?, level = ?, schedule_id = ?, enabled = ? WHERE id = ?",
		trigger.ServerID, trigger.Name, trigger.Pattern, trigger.Level, trigger.ScheduleID, boolInt(trigger.Enabled), trigger.ID)
	return err
}

// RecordTriggerMatch merkt sich den letzten Treffer.
func (s *Store) RecordTriggerMatch(id string, at int64, line string) error {
	_, err := s.db.Exec("UPDATE console_triggers SET last_match = ?, last_line = ? WHERE id = ?", at, clip(line, 300), id)
	return err
}

// DeleteTrigger löscht einen Auslöser.
func (s *Store) DeleteTrigger(id string) error {
	_, err := s.db.Exec("DELETE FROM console_triggers WHERE id = ?", id)
	return err
}

// ---------------------------------------------------------------- Discord-Bot

// DiscordRule erlaubt einer Discord-Rolle oder einem Discord-Benutzer bestimmte Befehle.
type DiscordRule struct {
	// Kind: role oder user.
	Kind  string `json:"kind"`
	ID    string `json:"id"`
	Label string `json:"label,omitempty"`
	// Actions: status, players, start, stop, restart.
	Actions []string `json:"actions"`
}

// DiscordBot ist die Anbindung eines Projekts an eine Discord-Anwendung. BotToken ist mit dem Schlüssel des Panels
// verschlüsselt und verlässt das Panel nie.
type DiscordBot struct {
	ProjectID     string
	ApplicationID string
	PublicKey     string
	BotToken      string
	Rules         []DiscordRule
	Enabled       bool
	RegisteredAt  int64
	UpdatedAt     int64
}

// DiscordBot liefert die Discord-Anbindung eines Projekts (ErrNotFound, wenn es keine gibt).
func (s *Store) DiscordBot(projectID string) (DiscordBot, error) {
	var bot DiscordBot
	var rules string
	var enabled int
	err := s.db.QueryRow("SELECT project_id, application_id, public_key, bot_token, rules, enabled, registered_at, updated_at FROM discord_bots WHERE project_id = ?", projectID).
		Scan(&bot.ProjectID, &bot.ApplicationID, &bot.PublicKey, &bot.BotToken, &rules, &enabled, &bot.RegisteredAt, &bot.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return DiscordBot{}, ErrNotFound
	}
	if err != nil {
		return DiscordBot{}, err
	}
	bot.Enabled = enabled == 1
	if json.Unmarshal([]byte(rules), &bot.Rules) != nil || bot.Rules == nil {
		bot.Rules = []DiscordRule{}
	}
	return bot, nil
}

// SaveDiscordBot legt die Discord-Anbindung an oder ersetzt sie.
func (s *Store) SaveDiscordBot(bot DiscordBot) error {
	rules, err := json.Marshal(bot.Rules)
	if err != nil {
		return err
	}
	_, err = s.db.Exec(`INSERT INTO discord_bots (project_id, application_id, public_key, bot_token, rules, enabled, registered_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(project_id) DO UPDATE SET application_id = excluded.application_id, public_key = excluded.public_key, bot_token = excluded.bot_token,
		rules = excluded.rules, enabled = excluded.enabled, registered_at = excluded.registered_at, updated_at = excluded.updated_at`,
		bot.ProjectID, bot.ApplicationID, bot.PublicKey, bot.BotToken, string(rules), boolInt(bot.Enabled), bot.RegisteredAt, time.Now().UnixMilli())
	return err
}

// DeleteDiscordBot entfernt die Discord-Anbindung.
func (s *Store) DeleteDiscordBot(projectID string) error {
	_, err := s.db.Exec("DELETE FROM discord_bots WHERE project_id = ?", projectID)
	return err
}
