import { t } from "@/lib/i18n";
// Übersetzt die technische Aktion aus dem Protokoll (z. B. "POST /api/projects/{id}/servers") in lesbaren Text.

const rules = (): [RegExp, string][] => [
  [/^POST \/api\/projects$/, t("Projekt angelegt")],
  [/^PATCH \/api\/projects\/\{id\}$/, t("Projekt geändert")],
  [/^DELETE \/api\/projects\/\{id\}$/, t("Projekt gelöscht")],
  [/^PUT \/api\/projects\/\{id\}\/members/, t("Mitglied geändert")],
  [/^DELETE \/api\/projects\/\{id\}\/members/, t("Mitglied entfernt")],
  [/^POST \/api\/projects\/\{id\}\/servers/, t("Server angelegt")],
  [/^PUT \/api\/projects\/\{id\}\/network/, t("Netzwerk gespeichert")],
  [/^POST \/api\/projects\/\{id\}\/network\/sync/, t("Netzwerk abgeglichen")],
  [/^PUT \/api\/projects\/\{id\}\/voicechat/, t("Voicechat umgestellt")],
  [/^PUT \/api\/projects\/\{id\}\/motd/, t("MOTD geändert")],
  [/^PUT \/api\/projects\/\{id\}\/status/, t("Statusseite geändert")],
  [/^POST \/api\/projects\/\{id\}\/groups/, t("Gruppe angelegt")],
  [/^(PATCH|POST) \/api\/groups/, t("Gruppe geändert")],
  [/^DELETE \/api\/groups/, t("Gruppe gelöscht")],
  [/^POST \/api\/projects\/\{id\}\/schedules/, t("Zeitplan angelegt")],
  [/^PATCH \/api\/schedules/, t("Zeitplan geändert")],
  [/^DELETE \/api\/schedules/, t("Zeitplan gelöscht")],
  [/^POST \/api\/schedules\/\{id\}\/run/, t("Zeitplan ausgeführt")],
  [/^POST \/api\/projects\/\{id\}\/webhooks/, t("Webhook angelegt")],
  [/^(PATCH|POST) \/api\/webhooks/, t("Webhook geändert")],
  [/^DELETE \/api\/webhooks/, t("Webhook gelöscht")],
  [/^PATCH \/api\/servers\/\{id\}$/, t("Server-Einstellungen geändert")],
  [/^DELETE \/api\/servers\/\{id\}$/, t("Server gelöscht")],
  [/^PUT \/api\/servers\/\{id\}\/public-address/, t("Öffentliche Adresse geändert")],
  [/^POST \/api\/servers\/\{id\}\/tunnel/, t("Tunnel erstellt")],
  [/^DELETE \/api\/servers\/\{id\}\/tunnel/, t("Tunnel entfernt")],
  [/^PUT \/api\/servers\/\{id\}\/auto-restart/, t("Auto-Restart umgestellt")],
  [/^POST \/api\/servers\/\{id\}\/modrinth\/install/, t("Modrinth: installiert")],
  [/^POST \/api\/servers\/\{id\}\/addons/, t("Plugins/Mods aktualisiert")],
  [/^POST \/api\/servers\/\{id\}\/player-action/, t("Spieleraktion")],
  [/^(PUT|DELETE) \/api\/projects\/\{id\}\/backup-target$/, t("Backup-Ziel geändert")],
  [/^POST \/api\/projects\/\{id\}\/backup-target\/test/, t("Backup-Ziel getestet")],
  [/^PUT \/api\/servers\/\{id\}\/backup-settings/, t("Backup-Einstellungen geändert")],
  [/^POST \/api\/servers\/\{id\}\/backups$/, t("Backup erstellt")],
  [/^POST \/api\/servers\/\{id\}\/backups\/\{name\}\/lock/, t("Backup festgehalten oder entsperrt")],
  [/^POST \/api\/servers\/\{id\}\/backups\/\{name\}\/upload/, t("Backup hochgeladen")],
  [/^POST \/api\/servers\/\{id\}\/backups\/\{name\}\/restore-remote/, t("Backup wiederhergestellt")],
  [/^DELETE \/api\/servers\/\{id\}\/backups\/\{name\}$/, t("Backup gelöscht")],
  [/^PUT \/api\/servers\/\{id\}\/sftp$/, t("SFTP-Einstellung geändert")],
  [/^(PUT|POST|DELETE) \/api\/me\/sftp/, t("Eigenen SFTP-Zugang geändert")],
  [/^PUT \/api\/servers\/\{id\}\/properties/, t("Spieleinstellungen geändert")],
  [/^POST \/api\/servers\/\{id\}\/worlds\/\{world\}\/reset/, t("Welt zurückgesetzt")],
  [/^POST \/api\/servers\/\{id\}\/worlds\/\{world\}\/upload/, t("Welt hochgeladen")],
  [/^POST \/api\/servers\/\{id\}\/worlds\/\{world\}\/datapacks/, t("Datapack hinzugefügt")],
  [/^DELETE \/api\/servers\/\{id\}\/worlds\/\{world\}\/datapacks/, t("Datapack entfernt")],
  [/^POST \/api\/servers\/\{id\}\/clone/, t("Server kopiert")],
  [/^POST \/api\/projects\/\{id\}\/migrate/, t("Umzug gestartet")],
  [/^POST \/api\/server-ops\/\{id\}\/cancel/, t("Kopie oder Umzug abgebrochen")],
  [/^PUT \/api\/servers\/\{id\}\/storage$/, t("Speicherlimit geändert")],
  [/^POST \/api\/servers\/\{id\}\/storage\/measure/, t("Speicherplatz gemessen")],
  [/^PUT \/api\/servers\/\{id\}\/sleep/, t("Schlafmodus umgestellt")],
  [/^POST \/api\/projects\/\{id\}\/console-triggers/, t("Konsolen-Auslöser angelegt")],
  [/^PATCH \/api\/console-triggers/, t("Konsolen-Auslöser geändert")],
  [/^DELETE \/api\/console-triggers/, t("Konsolen-Auslöser gelöscht")],
  [/^(PUT|DELETE) \/api\/projects\/\{id\}\/discord$/, t("Discord-Bot geändert")],
  [/^POST \/api\/projects\/\{id\}\/discord\/register/, t("Discord-Befehle eingetragen")],
  [/^POST \/api\/servers\/\{id\}\/sources\/\{source\}\/install/, t("Plugin/Mod installiert")],
  [/^PUT \/api\/(servers|projects)\/\{id\}\/blocks/, t("Baustein umgestellt")],
  [/^PUT \/api\/me\/project-(prefs|order)/, t("Eigene Ordnung der Projekte geändert")],
  [/^DELETE \/api\/users\/\{id\}\/passkeys/, t("Passkeys eines Benutzers zurückgesetzt")],
  [/^POST \/api\/users$/, t("Benutzer angelegt")],
  [/^PATCH \/api\/users/, t("Benutzer geändert")],
  [/^DELETE \/api\/users\/\{id\}\/totp/, t("2FA eines Benutzers zurückgesetzt")],
  [/^DELETE \/api\/users/, t("Benutzer gelöscht")],
  [/^POST \/api\/users\/\{id\}\/password/, t("Passwort zurückgesetzt")],
  [/^POST \/api\/me\/password/, t("Eigenes Passwort geändert")],
  [/^POST \/api\/me\/keys/, t("API-Schlüssel angelegt")],
  [/^DELETE \/api\/me\/keys/, t("API-Schlüssel widerrufen")],
  [/^DELETE \/api\/me\/sessions/, t("Sitzung beendet")],
  [/^POST \/api\/nodes$/, t("Node hinzugefügt")],
  [/^PATCH \/api\/nodes/, t("Node geändert")],
  [/^DELETE \/api\/nodes\/\{id\}$/, t("Node entfernt")],
  [/^(PUT|DELETE) \/api\/nodes\/\{id\}\/playit/, t("playit geändert")],
  [/^POST \/api\/auth\/logout/, t("Abgemeldet")],
];

// Aktionen an einem Server laufen über eine gemeinsame Route; der Pfad sagt, was gemeint war.
const serverParts = (): [RegExp, string][] => [
  [/\/power$/, t("Server gesteuert (Start/Stopp)")],
  [/\/command$/, t("Konsolenbefehl gesendet")],
  [/\/files\/upload/, t("Datei hochgeladen")],
  [/\/files\/(content|mkdir|rename|move|delete|archive|extract|pull)/, t("Dateien geändert")],
  [/\/files/, t("Dateien geändert")],
  [/\/backups\/[^/]+\/restore/, t("Backup wiederhergestellt")],
  [/\/backups\/[^/]+$/, t("Backup gelöscht")],
  [/\/backups/, t("Backup erstellt")],
  [/\/reinstall/, t("Version gewechselt")],
];

export function auditLabel(action: string, target?: string): string {
  const base = action.replace(" (API-Schlüssel)", "");
  const viaKey = action.includes("(API-Schlüssel)") ? t(" · API-Schlüssel") : "";
  for (const [pattern, label] of rules()) {
    if (pattern.test(base)) return label + viaKey;
  }
  if (base.startsWith("POST /api/servers/{id}/") || base.startsWith("DELETE /api/servers/{id}/")) {
    for (const [pattern, label] of serverParts()) {
      if (pattern.test(target ?? "")) return label + viaKey;
    }
  }
  return base + viaKey;
}
