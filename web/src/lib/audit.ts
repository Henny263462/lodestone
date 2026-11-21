// Übersetzt die technische Aktion aus dem Protokoll (z. B. "POST /api/projects/{id}/servers") in lesbaren Text.

const rules: [RegExp, string][] = [
  [/^POST \/api\/projects$/, "Projekt angelegt"],
  [/^PATCH \/api\/projects\/\{id\}$/, "Projekt geändert"],
  [/^DELETE \/api\/projects\/\{id\}$/, "Projekt gelöscht"],
  [/^PUT \/api\/projects\/\{id\}\/members/, "Mitglied geändert"],
  [/^DELETE \/api\/projects\/\{id\}\/members/, "Mitglied entfernt"],
  [/^POST \/api\/projects\/\{id\}\/servers/, "Server angelegt"],
  [/^PUT \/api\/projects\/\{id\}\/network/, "Netzwerk gespeichert"],
  [/^POST \/api\/projects\/\{id\}\/network\/sync/, "Netzwerk abgeglichen"],
  [/^PUT \/api\/projects\/\{id\}\/voicechat/, "Voicechat umgestellt"],
  [/^PUT \/api\/projects\/\{id\}\/motd/, "MOTD geändert"],
  [/^PUT \/api\/projects\/\{id\}\/status/, "Statusseite geändert"],
  [/^POST \/api\/projects\/\{id\}\/groups/, "Gruppe angelegt"],
  [/^(PATCH|POST) \/api\/groups/, "Gruppe geändert"],
  [/^DELETE \/api\/groups/, "Gruppe gelöscht"],
  [/^POST \/api\/projects\/\{id\}\/schedules/, "Zeitplan angelegt"],
  [/^PATCH \/api\/schedules/, "Zeitplan geändert"],
  [/^DELETE \/api\/schedules/, "Zeitplan gelöscht"],
  [/^POST \/api\/schedules\/\{id\}\/run/, "Zeitplan ausgeführt"],
  [/^POST \/api\/projects\/\{id\}\/webhooks/, "Webhook angelegt"],
  [/^(PATCH|POST) \/api\/webhooks/, "Webhook geändert"],
  [/^DELETE \/api\/webhooks/, "Webhook gelöscht"],
  [/^PATCH \/api\/servers\/\{id\}$/, "Server-Einstellungen geändert"],
  [/^DELETE \/api\/servers\/\{id\}$/, "Server gelöscht"],
  [/^PUT \/api\/servers\/\{id\}\/public-address/, "Öffentliche Adresse geändert"],
  [/^POST \/api\/servers\/\{id\}\/tunnel/, "Tunnel erstellt"],
  [/^DELETE \/api\/servers\/\{id\}\/tunnel/, "Tunnel entfernt"],
  [/^PUT \/api\/servers\/\{id\}\/auto-restart/, "Auto-Restart umgestellt"],
  [/^POST \/api\/servers\/\{id\}\/modrinth\/install/, "Modrinth: installiert"],
  [/^POST \/api\/servers\/\{id\}\/addons/, "Plugins/Mods aktualisiert"],
  [/^POST \/api\/servers\/\{id\}\/player-action/, "Spieleraktion"],
  [/^(PUT|DELETE) \/api\/projects\/\{id\}\/backup-target$/, "Backup-Ziel geändert"],
  [/^POST \/api\/projects\/\{id\}\/backup-target\/test/, "Backup-Ziel getestet"],
  [/^PUT \/api\/servers\/\{id\}\/backup-settings/, "Backup-Einstellungen geändert"],
  [/^POST \/api\/servers\/\{id\}\/backups$/, "Backup erstellt"],
  [/^POST \/api\/servers\/\{id\}\/backups\/\{name\}\/lock/, "Backup festgehalten oder entsperrt"],
  [/^POST \/api\/servers\/\{id\}\/backups\/\{name\}\/upload/, "Backup hochgeladen"],
  [/^POST \/api\/servers\/\{id\}\/backups\/\{name\}\/restore-remote/, "Backup wiederhergestellt"],
  [/^DELETE \/api\/servers\/\{id\}\/backups\/\{name\}$/, "Backup gelöscht"],
  [/^PUT \/api\/servers\/\{id\}\/sftp$/, "SFTP-Einstellung geändert"],
  [/^(PUT|POST|DELETE) \/api\/me\/sftp/, "Eigenen SFTP-Zugang geändert"],
  [/^PUT \/api\/servers\/\{id\}\/properties/, "Spieleinstellungen geändert"],
  [/^POST \/api\/servers\/\{id\}\/worlds\/\{world\}\/reset/, "Welt zurückgesetzt"],
  [/^POST \/api\/servers\/\{id\}\/worlds\/\{world\}\/upload/, "Welt hochgeladen"],
  [/^POST \/api\/servers\/\{id\}\/worlds\/\{world\}\/datapacks/, "Datapack hinzugefügt"],
  [/^DELETE \/api\/servers\/\{id\}\/worlds\/\{world\}\/datapacks/, "Datapack entfernt"],
  [/^POST \/api\/servers\/\{id\}\/clone/, "Server kopiert"],
  [/^POST \/api\/projects\/\{id\}\/migrate/, "Umzug gestartet"],
  [/^POST \/api\/server-ops\/\{id\}\/cancel/, "Kopie oder Umzug abgebrochen"],
  [/^PUT \/api\/servers\/\{id\}\/storage$/, "Speicherlimit geändert"],
  [/^POST \/api\/servers\/\{id\}\/storage\/measure/, "Speicherplatz gemessen"],
  [/^PUT \/api\/servers\/\{id\}\/sleep/, "Schlafmodus umgestellt"],
  [/^POST \/api\/projects\/\{id\}\/console-triggers/, "Konsolen-Auslöser angelegt"],
  [/^PATCH \/api\/console-triggers/, "Konsolen-Auslöser geändert"],
  [/^DELETE \/api\/console-triggers/, "Konsolen-Auslöser gelöscht"],
  [/^(PUT|DELETE) \/api\/projects\/\{id\}\/discord$/, "Discord-Bot geändert"],
  [/^POST \/api\/projects\/\{id\}\/discord\/register/, "Discord-Befehle eingetragen"],
  [/^POST \/api\/servers\/\{id\}\/sources\/\{source\}\/install/, "Plugin/Mod installiert"],
  [/^PUT \/api\/(servers|projects)\/\{id\}\/blocks/, "Baustein umgestellt"],
  [/^PUT \/api\/me\/project-(prefs|order)/, "Eigene Ordnung der Projekte geändert"],
  [/^DELETE \/api\/users\/\{id\}\/passkeys/, "Passkeys eines Benutzers zurückgesetzt"],
  [/^POST \/api\/users$/, "Benutzer angelegt"],
  [/^PATCH \/api\/users/, "Benutzer geändert"],
  [/^DELETE \/api\/users\/\{id\}\/totp/, "2FA eines Benutzers zurückgesetzt"],
  [/^DELETE \/api\/users/, "Benutzer gelöscht"],
  [/^POST \/api\/users\/\{id\}\/password/, "Passwort zurückgesetzt"],
  [/^POST \/api\/me\/password/, "Eigenes Passwort geändert"],
  [/^POST \/api\/me\/keys/, "API-Schlüssel angelegt"],
  [/^DELETE \/api\/me\/keys/, "API-Schlüssel widerrufen"],
  [/^DELETE \/api\/me\/sessions/, "Sitzung beendet"],
  [/^POST \/api\/nodes$/, "Node hinzugefügt"],
  [/^PATCH \/api\/nodes/, "Node geändert"],
  [/^DELETE \/api\/nodes\/\{id\}$/, "Node entfernt"],
  [/^(PUT|DELETE) \/api\/nodes\/\{id\}\/playit/, "playit geändert"],
  [/^POST \/api\/auth\/logout/, "Abgemeldet"],
];

// Aktionen an einem Server laufen über eine gemeinsame Route; der Pfad sagt, was gemeint war.
const serverParts: [RegExp, string][] = [
  [/\/power$/, "Server gesteuert (Start/Stopp)"],
  [/\/command$/, "Konsolenbefehl gesendet"],
  [/\/files\/upload/, "Datei hochgeladen"],
  [/\/files\/(content|mkdir|rename|move|delete|archive|extract|pull)/, "Dateien geändert"],
  [/\/files/, "Dateien geändert"],
  [/\/backups\/[^/]+\/restore/, "Backup wiederhergestellt"],
  [/\/backups\/[^/]+$/, "Backup gelöscht"],
  [/\/backups/, "Backup erstellt"],
  [/\/reinstall/, "Version gewechselt"],
];

export function auditLabel(action: string, target?: string): string {
  const base = action.replace(" (API-Schlüssel)", "");
  const viaKey = action.includes("(API-Schlüssel)") ? " · API-Schlüssel" : "";
  for (const [pattern, label] of rules) {
    if (pattern.test(base)) return label + viaKey;
  }
  if (base.startsWith("POST /api/servers/{id}/") || base.startsWith("DELETE /api/servers/{id}/")) {
    for (const [pattern, label] of serverParts) {
      if (pattern.test(target ?? "")) return label + viaKey;
    }
  }
  return base + viaKey;
}
