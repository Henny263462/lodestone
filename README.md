# Lodestone

[![CI](https://github.com/Henny263462/lodestone/actions/workflows/ci.yml/badge.svg)](https://github.com/Henny263462/lodestone/actions/workflows/ci.yml)
[![Lizenz: MIT](https://img.shields.io/badge/Lizenz-MIT-blue.svg)](LICENSE)

Ein Hosting-Panel für Minecraft, zum Selbsthosten. Es verwaltet Server und ganze Netzwerke als **Projekte** auf einer
oder mehreren Maschinen (Nodes), mit Docker als Isolation, mehreren Benutzern und Rollen pro Projekt.

> **Stand: Version 0.1.** Fertig sind Node-Daemon, Panel-Backend, Weboberfläche und die Netzwerk-Autoconfig. Gruppen mit
> Autoscaling und die Verbindung der Node „nach außen“ kommen später, siehe [docs/architecture.md](docs/architecture.md).

```
Browser ──►  Panel (Go, SQLite, eingebettete Next.js-Oberfläche)  ──HTTP + Token──►  Node-Daemon (Go)  ──►  Docker  ──►  Server
```

## Was geht

- **Projekte:** ein einzelner Server oder ein **Netzwerk** (Velocity + Server dahinter). Mitglieder mit Rollen
  *Betrachter, Operator, Admin, Eigentümer* pro Projekt; globale Administratoren sehen alles
- **Feine Rechte pro Mitglied:** Die Rolle ist eine Vorlage, unter „Mitglieder → Rechte“ lassen sich 18 Einzelrechte
  (Konsole lesen, Befehle, Starten/Stoppen, Dateien ansehen/ändern, SFTP, Backups ansehen/erstellen/wiederherstellen/löschen,
  Plugins/Mods, Spieler, Einstellungen, Server anlegen/löschen, Zeitpläne, Webhooks, Mitglieder, Netzwerk) dazugeben oder
  wegnehmen – niemand kann mehr vergeben, als er selbst hat
- **Server-Arten:** Paper, Purpur, Folia, Fabric, NeoForge, Vanilla, Velocity – Version, Download mit Prüfsumme und Java-Version
  werden automatisch bestimmt
- **Konsole live** (Websocket, mit Verlauf und ANSI-Farben), Start/Stopp/Neustart/Kill, CPU-/RAM-Anzeige
- **Dateien** mit Editor und Syntax-Highlight (JSON, YAML, TOML, XML, Properties, Markdown …), Upload per Drag & Drop,
  Verschieben, Archivieren (`.zip`, `.tar.gz`), Entpacken und Mehrfachauswahl (Verschieben, Archivieren, Löschen)
- **SFTP-Zugang pro Server** (FileZilla, WinSCP, `sftp`): ab der Rolle Operator, mit eigenem SFTP-Passwort und/oder
  **SSH-Schlüsseln** unter *Konto*, Passwort-Anmeldung abschaltbar. Benutzername `<benutzer>.<server-kurz-id>`, Host, Port und
  Fingerabdruck stehen mit Kopierknöpfen im Dateien-Tab. Admins können Operatoren eines Servers auf „nur lesen“ stellen.
  Das Panel entscheidet allein und schiebt die Zugänge zum Node (Änderungen wirken nach wenigen Sekunden, auch auf offene
  Verbindungen); Anmeldungen stehen im Protokoll und in der Konsole
- **Plugins und Mods:** installierte anzeigen (Name, Version), von **Modrinth** suchen und installieren (nur passende
  zu Software und Version, inklusive benötigter Abhängigkeiten), Updates prüfen und einspielen, Jar hochladen,
  aktivieren/deaktivieren. Zu jeder installierten Datei zeigt das Panel **Modrinth-Seite, Version, Lizenz und
  Quellcode** und lässt die **Version wechseln**
- **Modpacks:** Beim Anlegen eines Servers ein **Modrinth-Modpack** (Fabric, `.mrpack`) suchen und Version wählen. Mods
  und Konfiguration werden mit Prüfsummen geladen, der Fortschritt steht im Server
- **playit.gg:** Der Node startet den playit-Agenten (Schlüssel unter **Nodes** eintragen), die Tunnel-Adresse
  speichert man beim Server unter *Einstellungen → Öffentliche Adresse*. Keine Portfreigabe im Router nötig
- **Backups** (tar.gz, bei laufendem Server mit `save-off`), Wiederherstellen (sicher, nur bei ausgeschaltetem Server),
  Aufbewahrungsanzahl, Download. **Ausschlüsse** pro Server (Pfadmuster wie `dynmap/**`, `logs/**`, `*.log`; beim
  Wiederherstellen bleiben ausgeschlossene Ordner unangetastet), **Festhalten** (festgehaltene Backups löscht weder die
  Aufbewahrung noch ein Klick, erst nach dem Entsperren)
- **Externes Backup-Ziel** pro Projekt (S3-kompatibel: AWS, MinIO, Backblaze B2, Hetzner, Cloudflare R2; eigene
  Signatur V4, kein SDK) mit Verbindungstest. Backups lokal, extern oder beides, getrennte Aufbewahrung, Hochladen
  auch nachträglich und per Zeitplan („auch extern hochladen“), Wiederherstellen direkt aus dem Ziel. Die
  Zugangsdaten bleiben im Panel (Secret mit AES-GCM verschlüsselt): Der Node bekommt nur vorsignierte Adressen und lädt
  damit selbst hoch (Multipart, Teile ab 64 MiB, höchstens 1000 Teile) bzw. herunter. Externe Backups bleiben beim
  Löschen eines Servers im Bucket
- **Spieler** online (Serverlisten-Abfrage), Server-Einstellungen, Versionswechsel ohne Datenverlust
- **Netzwerk-Autoconfig:** Beim Hinzufügen schreibt das Panel alles selbst – Velocity *modern forwarding* samt
  Secret, Serverliste, **Beitrittsreihenfolge (Try) mit Reihenfolge und Schalter pro Server**, **Forced Hosts**,
  `online-mode=false` und Forwarding bei Paper/Purpur/Folia, FabricProxy-Lite bei Fabric (wird installiert)
- **Simple Voice Chat** pro Server einschaltbar: Plugin bzw. Mod wird installiert, der UDP-Port veröffentlicht und
  `voice_host` gesetzt
- Server hinter dem Proxy sind **nicht** nach außen veröffentlicht, nur der Proxy (und der Voicechat-UDP-Port)
- **Zeitpläne:** Ketten aus Backup, Befehl, Neustart, Start, Stopp und Warten – alle N Minuten, täglich oder an
  gewählten Wochentagen, mit Vorwarnung im Chat und Nachholen nach einem Panel-Neustart (Zeitzone über `LODESTONE_TZ`)
- **Ereignisse und Webhooks:** Abstürze, automatische Neustarts, Node offline/online, wenig Arbeitsspeicher und
  Zeitpläne landen im Projektverlauf und gehen auf Wunsch an **Discord** oder eine beliebige Adresse (Schutz vor
  Zugriffen auf das interne Netz). **Auto-Restart** startet abgestürzte Server neu (höchstens 3-mal in 15 Minuten)
- **Spielerverwaltung:** Kicken, Sperren, Operator, Whitelist und IP-Sperren per Klick (Listen aus den Dateien des
  Servers); **MOTD** des Netzwerks mit MiniMessage-Vorschau
- **Öffentliche Statusseite** pro Projekt (`/status/?p=<Kürzel>`, mit `&embed=1` zum Einbetten) und JSON unter
  `/api/public/status/<Kürzel>`; zeigt nur Online-Status, Spielerzahl, Version und Adresse
- **Konsole:** Suche, Filter nach Stufe, Log-Download und Hinweise zu bekannten Fehlern (zu wenig RAM, EULA, Port belegt,
  falsche Java-Version, Plugin- und Mod-Konflikte)
- **Arbeitsspeicher-Planer:** Der Node prüft vor dem Start, ob genug RAM frei ist (Reserve einstellbar), und die
  Nodes-Seite zeigt zugesagt gegen tatsächlich belegt
- **Konto-Sicherheit:** Zwei-Faktor-Anmeldung (TOTP mit QR-Code und Wiederherstellungscodes), **API-Schlüssel** für
  Skripte (`Authorization: Bearer lds_…`), Sitzungsliste mit Abmelden und ein **Protokoll** aller Änderungen
  (90 Tage, nur für Administratoren)
- **Anmeldung über OIDC und Passkeys:** Knopf „Mit Authentik anmelden“ für jeden OIDC-Anbieter (Authentik, Keycloak,
  Authelia, Google …), Konten werden verknüpft oder auf Wunsch automatisch angelegt, Administratoren optional über eine
  Gruppe; **Passkeys** (Fingerabdruck, Gesichtserkennung, Sicherheitsschlüssel) zum Anmelden ohne Passwort oder als
  zweiter Faktor statt des Codes aus der App
- **Anlegen:** Vorlagen (Survival, Lobby, Creative, Mods), **ZIP-Import** einer bestehenden Welt oder eines Servers
  und Strg+K als Schnellsuche

## Schnellstart (1-Klick Installer für Linux)

Auf einem Linux-Server (Ubuntu, Debian, Fedora, Arch etc.) als Root oder mit `sudo` ausführen:

```bash
curl -fsSL https://henny263462.github.io/lodestone/install.sh | sudo bash
```

Das Skript prüft/installiert Docker und Docker Compose, ermittelt die `DOCKER_GID`, generiert sichere Tokens & Passwörter, erstellt das Datenverzeichnis `/srv/lodestone` (1000:1000) und startet den Stack.

## Updates & Automatisierung

Lodestone-Images werden bei jedem Push auf GitHub automatisch per GitHub Actions gebaut und auf der **GitHub Container Registry (ghcr.io)** bereitgestellt:
- `ghcr.io/henny263462/lodestone-panel:latest`
- `ghcr.io/henny263462/lodestone-node:latest`

### Automatische Updates (optional)
In der Release-Compose-Datei (`docker-compose.release.yml`) liegt **Watchtower** bereit, ist aber standardmäßig **aus**
(`--profile autoupdate` schaltet es ein). Es prüft stündlich auf neue Images. Watchtower von containrrr ist archiviert und
läuft mit neuen Docker-Versionen (29+) nicht zuverlässig; das manuelle Update unten ist die sichere Wahl.

### Manuelles Update (1-Klick)
```bash
curl -fsSL https://henny263462.github.io/lodestone/update.sh | sudo bash
```
Oder direkt auf dem Server:
```bash
cd /opt/lodestone && sudo ./update.sh
```

## Schnellstart mit fertigen Images

```bash
curl -LO https://github.com/Henny263462/lodestone/releases/latest/download/docker-compose.release.yml
curl -L  https://github.com/Henny263462/lodestone/releases/latest/download/default.env.example -o .env
# .env ausfüllen (siehe unten), Datenordner anlegen: mkdir -p /srv/lodestone && chown 1000:1000 /srv/lodestone
docker compose -f docker-compose.release.yml up -d
```

## Schnellstart (Docker Compose, selbst bauen)

Voraussetzung: Docker mit Compose. Panel und ein Node laufen auf derselben Maschine.

```bash
cp .env.example .env
# .env ausfüllen: LODESTONE_ADMIN_PASSWORD, LODESTONE_NODE_TOKEN (openssl rand -hex 32), LODESTONE_PUBLIC_HOST,
# DOCKER_GID (stat -c %g /var/run/docker.sock) und LODESTONE_DATA (Ordner für die Serverdaten, Besitzer 1000:1000)
mkdir -p /srv/lodestone && chown 1000:1000 /srv/lodestone
docker compose up -d --build
```

Dann `http://<Maschine>:8480` öffnen und mit dem Administrator aus der `.env` anmelden. Die Weboberfläche hat kein
eigenes TLS: Stelle bei Zugriff aus dem Internet einen Reverse-Proxy mit HTTPS davor und setze
`LODESTONE_SECURE_COOKIES=true` und `LODESTONE_TRUST_PROXY=true`.

Wichtig bei Compose: Der Datenordner wird **mit demselben Pfad** im Host und im Node-Container eingebunden, weil Docker
die Serverordner über den Pfad des Hosts in die Minecraft-Container bindet. Die Node-API (`:8480` im Compose-Netz) wird
nicht veröffentlicht. Die Minecraft-Ports kommen aus `LODESTONE_PORT_RANGE` – wähle sie so, dass sie keine anderen
Dienste stören, und öffne sie in der Firewall.

Weitere Maschinen: Node-Container (`Dockerfile.node`) dort starten, die API mit TLS oder über VPN erreichbar machen und
im Panel unter **Nodes** mit Adresse und Token eintragen. Alle Server eines Netzwerks liegen auf *einem* Node.

### Ohne Docker bauen

```bash
sh scripts/build-web.sh                          # Oberfläche bauen und einbetten (Node.js 22)
go build -o bin/lodestone-panel ./cmd/lodestone-panel
go build -o bin/lodestone-node  ./cmd/lodestone-node
```

Für schlanke Maschinen: `sh scripts/package.sh` baut Linux-Binaries nach `dist/`, daraus bauen die
`deploy/Dockerfile.*.prebuilt` kleine Images.

## Konfiguration des Panels (Umgebung)

| Variable | Bedeutung |
| --- | --- |
| `LODESTONE_LISTEN` | Adresse, Standard `:8480` |
| `LODESTONE_DATA_DIR` | Ordner der Datenbank (`lodestone.db`), Standard `data` |
| `LODESTONE_ADMIN_USER` / `_PASSWORD` | legt beim ersten Start den Administrator an (ohne Passwort wird eins erzeugt und einmalig ins Log geschrieben). Ohne diese Variablen fragt die Oberfläche beim ersten Aufruf nach dem Administrator |
| `LODESTONE_NODE_URL` / `_TOKEN` / `_NAME` / `_PUBLIC_HOST` | trägt beim Start einen Node ein bzw. aktualisiert ihn |
| `LODESTONE_SECURE_COOKIES`, `LODESTONE_TRUST_PROXY` | hinter HTTPS-Proxy auf `true` |
| `LODESTONE_TZ` | Zeitzone für Zeitpläne, z. B. `Europe/Berlin` (Standard: Zeitzone des Panels, sonst UTC) |
| `LODESTONE_WEBHOOK_ALLOW_INTERNAL` | `true` erlaubt Webhooks an Adressen im internen Netz (Standard: gesperrt) |
| `LODESTONE_BACKUP_ALLOW_INTERNAL` | `true` erlaubt Backup-Ziele im internen Netz, z. B. MinIO im LAN (Standard: gesperrt) |
| `LODESTONE_PUBLIC_URL` | Adresse, unter der du das Panel im Browser öffnest, z. B. `https://panel.example.org`. Nötig für Passkeys und den OIDC-Rücksprung |
| `LODESTONE_OIDC_ISSUER` / `_CLIENT_ID` / `_CLIENT_SECRET` | schaltet die Anmeldung über einen OIDC-Anbieter ein (Issuer-Adresse wie beim Anbieter angegeben) |
| `LODESTONE_OIDC_NAME` | Name auf dem Knopf („Mit Authentik anmelden“), Standard `SSO` |
| `LODESTONE_OIDC_REDIRECT_URL` | Rücksprungadresse, Standard `<LODESTONE_PUBLIC_URL>/api/auth/oidc/callback` |
| `LODESTONE_OIDC_SCOPES` | Scopes, Standard `openid profile email` (für Gruppen bei Authentik/Authelia: `openid profile email groups`) |
| `LODESTONE_OIDC_AUTO_CREATE` | `true` legt unbekannte Konten beim ersten Anmelden an; sonst kommen nur verknüpfte Konten herein |
| `LODESTONE_OIDC_ADMIN_GROUP` / `_GROUPS_CLAIM` | Mitglieder dieser Gruppe werden Administratoren (Claim Standard `groups`) |
| `LODESTONE_OIDC_ONLY` | `true` schaltet die Anmeldung mit Passwort ab (Notfallzugang siehe unten) |
| `LODESTONE_WEBAUTHN_RP_ID` / `_ORIGINS` | RP-ID der Passkeys (Standard: Hostname aus `LODESTONE_PUBLIC_URL`) und weitere erlaubte Herkünfte (kommagetrennt) |

### Anmeldung über OIDC und Passkeys

**OIDC** (Authorization Code Flow mit PKCE, `state` und `nonce`; das ID-Token wird mit den Schlüsseln des Anbieters geprüft):

1. Lege beim Anbieter eine Anwendung (vertraulicher Client) an und trage als Rücksprungadresse
   `https://panel.example.org/api/auth/oidc/callback` ein.
2. Setze `LODESTONE_PUBLIC_URL`, `LODESTONE_OIDC_ISSUER`, `_CLIENT_ID`, `_CLIENT_SECRET` und `_NAME` und starte das Panel neu.
   Die Anmeldeseite zeigt dann „Mit <Name> anmelden“. Das Panel fragt die Discovery des Anbieters erst beim ersten
   Anmelden ab; ist er gerade nicht erreichbar, startet das Panel trotzdem.
3. Bestehende Benutzer verknüpfen ihr Konto unter **Konto → Anmeldung über …**. Verknüpft wird über Aussteller und
   `sub`, nie über die E-Mail-Adresse. Ohne `LODESTONE_OIDC_AUTO_CREATE=true` kommen nur verknüpfte Konten herein;
   mit der Option bekommen neue Konten einen Benutzer ohne Passwort (Name aus `preferred_username` bzw. der E-Mail).
4. Mit `LODESTONE_OIDC_ADMIN_GROUP` werden Gruppenmitglieder Administratoren. Entzogen werden die Rechte nur Konten,
   die über OIDC angelegt wurden, und nie dem letzten Administrator.

Nach der Anmeldung gilt dieselbe Sitzung wie bei der Passwort-Anmeldung (Cookie, Schutz-Header). Die Zwei-Faktor-Abfrage
übernimmt bei OIDC der Anbieter. GitHub oder Discord sprechen kein OIDC; binde sie über einen Anbieter wie Authentik oder
Keycloak an.

**Nur OIDC:** `LODESTONE_OIDC_ONLY=true` schaltet die Anmeldung mit Passwort ab (Passkeys bleiben erlaubt; die Einrichtung
des ersten Administrators geht weiter). Ist OIDC nicht vollständig eingerichtet, wird die Option ignoriert.
**Notfallzugang**, falls der Anbieter ausfällt: Entferne `LODESTONE_OIDC_ONLY` (oder setze sie auf `false`), starte das
Panel neu und melde dich mit Benutzername und Passwort an. Benutzer ohne Passwort bekommen eines von einem Administrator
(Benutzer → Passwort zurücksetzen).

**Passkeys** brauchen `LODESTONE_PUBLIC_URL` und HTTPS (oder `http://localhost`). RP-ID ist der Hostname dieser Adresse,
erlaubte Herkunft ist `Schema://Host[:Port]` daraus. Ein Passkey gilt nur für genau diese RP-ID: Wechselst du die Adresse,
müssen alle ihre Passkeys neu anlegen. Passkeys legst du unter **Konto → Passkeys** an. Die Anmeldung nur mit Passkey verlangt die Prüfung am Gerät
(PIN, Fingerabdruck) und ersetzt Passwort und zweiten Faktor. Wer einen Passkey hat, braucht nach
dem Passwort einen zweiten Schritt (Passkey oder Code aus der App). Hat jemand sein Gerät verloren, entfernt ein
Administrator seine Passkeys mit `DELETE /api/users/<id>/passkeys`.

Node-Daemon: `LODESTONE_NODE_TOKEN`, `_LISTEN`, `_DATA_DIR`, `_PUBLIC_HOST`, `_PORT_RANGE` (z. B. `28000-28099`),
`_CONTAINER_USER`, `_NETWORK`, `_MEMORY_RESERVE_MB` (RAM, der für das System frei bleibt, Standard 1024), `_PLAYIT_IMAGE`,
`_SFTP_LISTEN` (Adresse des SFTP-Zugangs, Standard `:2022`; leer oder `off` schaltet SFTP ab)
– oder `config.json` (siehe unten). Der Host-Schlüssel für SFTP wird beim ersten Start im Datenordner des Nodes angelegt
(`sftp_host_ed25519_key`) und danach nie geändert. Im Compose-Setup ist der Port über `LODESTONE_SFTP_PORT` (Standard 2022)
veröffentlicht; zum Abschalten `LODESTONE_NODE_SFTP_LISTEN: ""` setzen und den `ports`-Eintrag des Nodes entfernen. Lässt sich
SFTP nicht starten (Port belegt, Schlüssel nicht schreibbar), läuft der Node ohne SFTP weiter und schreibt den Grund ins Log.

## Sicherheit

- Anmeldung mit **argon2id**-Passwörtern, Sitzungen als HttpOnly-/SameSite=Strict-Cookie (nur der Hash liegt in der
  Datenbank), zusätzlicher Header gegen CSRF, Herkunftsprüfung beim Websocket, Drosselung nach Fehlversuchen
- Rechte werden bei **jeder** Anfrage serverseitig geprüft (Betrachter lesen Konsole/Spieler, Operatoren steuern
  Server und Dateien, Admins ändern Einstellungen, Eigentümer löschen Projekte; angepasste Einzelrechte gelten genauso);
  Projekte, in denen man nicht Mitglied ist, „gibt es nicht“ (404)
- Optional **Zwei-Faktor** (TOTP, TOTP-Geheimnisse mit AES-GCM verschlüsselt abgelegt), API-Schlüssel nur als Hash
  gespeichert und ohne Zugriff auf Konto-Einstellungen; Änderungen werden im **Protokoll** festgehalten
- OIDC mit PKCE, `state` (an ein kurzlebiges Cookie des Browsers gebunden, gilt einmal) und `nonce`; Passkeys mit
  Benutzerprüfung beim Anmelden ohne Passwort. Client-Secret und Schlüsseldaten verlassen das Panel nie, Fehlversuche
  werden gedrosselt und protokolliert, API-Schlüssel können an Anmeldewegen nichts ändern
- Das Token des Nodes verlässt das Panel nie; die Node-API gehört nur ins interne Netz oder hinter TLS/VPN
- SFTP: nur das Subsystem `sftp` (keine Shell, keine Befehle, keine Weiterleitungen), Zugriff strikt im Serverordner über
  `os.Root` (auch Symlinks), keine neuen Symlinks, Upload-Grenze wie im Panel, Passwörter nur als argon2id-Hash, Drosselung
  nach 8 Fehlversuchen je Adresse in 10 Minuten
- Container laufen ohne Root-Rechte des Hosts, mit `no-new-privileges`, abgeschalteten Capabilities, Prozess-,
  Speicher- und CPU-Limit; Dateizugriffe bleiben im Serverordner (`os.Root`, auch über Symlinks hinweg)
- **Java2Shell/Java4Shell (CVE-2026-114938):** Jeder Server bekommt `JAVA_TOOL_OPTIONS=-XX:-UseBASE64Intrinsics`
  (gilt auch für eigene Startzeilen), und der Node lädt die Java-Images höchstens alle 6 Stunden beim Start neu, damit
  gepatchte Java-Versionen ankommen
- Ein Daemon mit Zugriff auf `docker.sock` hat auf der Maschine weitreichende Rechte. Vor dem Öffnen für fremde
  Benutzer: unabhängige Sicherheitsprüfung (siehe docs/architecture.md)

## Der Node-Daemon allein

Der Daemon lässt sich auch ohne Panel nutzen (eigene HTTP-Schnittstelle, Token).

### Voraussetzungen

- [Go](https://go.dev/dl/) 1.26 oder neuer zum Bauen
- [Docker](https://docs.docker.com/get-docker/) (Linux oder Docker Desktop mit WSL)

### Bauen und starten

```bash
go build -o bin/lodestone-node ./cmd/lodestone-node
./bin/lodestone-node -config config.json
```

Beim ersten Start wird `config.json` mit einem zufälligen Token angelegt und ausgegeben. Die Schnittstelle lauscht
standardmäßig nur auf `127.0.0.1:8480`.

```json
{
  "listen": "127.0.0.1:8480",
  "token": "…",
  "dataDir": "data",
  "containerUser": "",
  "portRangeStart": 25565,
  "portRangeEnd": 25999,
  "maxUploadMB": 512,
  "sftpListen": ":2022"
}
```

Läuft Docker nicht, startet der Daemon trotzdem und versucht es alle 10 Sekunden erneut.

### Schnittstelle ausprobieren

Alle Aufrufe brauchen `Authorization: Bearer <token>`.

```bash
T=…   # Token aus config.json
H="Authorization: Bearer $T"

# Server anlegen (die Minecraft-EULA bestätigst du mit "eula": true)
curl -H "$H" -X POST localhost:8480/api/servers -d '{"name":"Lobby","software":"paper","version":"1.21.8","memoryMB":2048,"eula":true}'

curl -H "$H" localhost:8480/api/servers                                   # Liste
curl -H "$H" -X POST localhost:8480/api/servers/<id>/power -d '{"action":"start"}'
curl -H "$H" -X POST localhost:8480/api/servers/<id>/command -d '{"command":"say hallo"}'
curl -H "$H" "localhost:8480/api/servers/<id>/logs?lines=100"
curl -H "$H" "localhost:8480/api/servers/<id>/files?path=plugins"
curl -H "$H" "localhost:8480/api/software/paper/versions"
```

| Methode | Pfad | Zweck |
| --- | --- | --- |
| GET | `/api/system` | Daemon-Version, Docker-Zustand |
| GET | `/api/software`, `/api/software/{id}/versions` | Server-Arten und Versionen |
| GET, POST | `/api/servers` | Liste, Server anlegen |
| GET, PATCH, DELETE | `/api/servers/{id}` | ansehen, ändern (nur offline), löschen |
| POST | `/api/servers/{id}/reinstall` | Software oder Version wechseln |
| POST | `/api/servers/{id}/power` | `start`, `stop`, `restart`, `kill` |
| POST | `/api/servers/{id}/command` | Konsolenbefehl |
| GET | `/api/servers/{id}/logs`, `/ws` | Verlauf, Live-Konsole (Websocket, Token auch als `?token=`) |
| GET, PUT, DELETE | `/api/servers/{id}/files`, `/files/content` | Dateien listen, lesen, schreiben, löschen |
| POST | `/api/servers/{id}/files/mkdir`, `/rename`, `/upload` | Ordner, Umbenennen, Upload |
| GET | `/api/servers/{id}/files/download` | Herunterladen |
| GET | `/api/sftp?after=<seq>` | SFTP an/aus, Port, Fingerabdruck, Start-Kennung, Anmeldungen seit `seq` |
| PUT | `/api/servers/{id}/sftp-access` | vollständige Liste der SFTP-Zugänge des Servers (setzt nur das Panel) |

Websocket: Die erste Nachricht ist `hello` mit Zustand und Verlauf, danach `line`, `state` und `stats`. Zum Senden
schickt der Client `{"type":"command","data":"say hi"}`.

## Tests

```bash
go test ./...                              # ohne Docker (Docker wird durch eine Attrappe ersetzt)
go test -tags docker ./internal/node -run Docker -v   # gegen ein echtes Docker, überspringt sich ohne
```

## Lizenz

[MIT](LICENSE). Beiträge sind willkommen, siehe [CONTRIBUTING.md](CONTRIBUTING.md). Sicherheitslücken bitte privat
melden ([SECURITY.md](SECURITY.md)).
