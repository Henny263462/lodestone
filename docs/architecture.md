# Architektur und Plan

Diese Datei hält die Entscheidungen fest, die beim Planen von Lodestone gefallen sind, damit sie nicht verloren gehen.

## Bausteine

```
Panel (Next.js)  ──HTTPS/WebSocket──►  Panel-Backend (Go) ── Datenbank (SQLite → Postgres)
                                              ▲
                                              │ Websocket, die Node verbindet nach außen
                                       Node-Daemon (Go) ──► Docker ──► Minecraft-Server (Torch optional)
```

- **Node-Daemon** (`cmd/lodestone-node`): läuft auf jeder Maschine, besitzt die Server-Prozesse. *Fertig.*
- **Panel-Backend** (`cmd/lodestone-panel`, `internal/panel`): Benutzer, Rollen, Projekte, Nodes, Netzwerk-Autoconfig,
  Modrinth, Proxy zu den Nodes, SQLite. *Fertig (v0.1).*
- **Panel-Oberfläche** (`web/`, Next.js als statischer Export, ins Panel-Binary eingebettet; Konsole mit ANSI-Farben,
  Datei-Editor mit CodeMirror 6, Plugins/Mods, Backups, Spieler). *Fertig (v0.1).* Das Panel liefert Text-Dateien gzip-
  komprimiert aus (`internal/panel/webui`, einmal gepackt und im Speicher gehalten); CodeMirror lädt erst beim Öffnen einer Datei.
- **Verbindung Panel ↔ Node (v0.1):** Das Panel ruft die HTTP-Schnittstelle des Nodes direkt auf (Bearer-Token); die
  Node-API gehört deshalb ins interne Netz oder hinter TLS/VPN. **Später:** Die Node verbindet **nach außen** zum Panel
  (eine Websocket-Verbindung, über die alles läuft). Dann braucht sie keine offenen Ports und funktioniert hinter NAT.
  Der Daemon-Handler ist ein normaler `http.Handler`, deshalb lässt sich die jetzige Schnittstelle unverändert durch
  diese Verbindung tunneln.

## Datenmodell

- **Projekt**: Einzelserver oder Netzwerk; hat Mitglieder mit Rollen pro Projekt.
- **Server** gehört zu einem Projekt und läuft auf einer Node. Er hat eine **Software** (Paper, Purpur, Folia, Fabric,
  NeoForge, Vanilla, Velocity, später Forge), Version, Java-Version, Startzeile und Limits. Ein Server ist entweder
  *statisch* (behält seine Daten) oder Instanz einer *Gruppe*.
- **Netzwerk** = ein oder mehrere Proxys + beliebig viele statische Server + optional Gruppen. Auch ein kleines
  Netzwerk nur aus statischen Servern ist möglich.
- **Gruppe**: Vorlage (Software, Version, Plugins, Welt, Configs) plus Regeln (min./max. Instanzen, Spieler pro
  Instanz, Benennung `name-1 …`, RAM/CPU, Nodes, Try-Liste). Instanzen sind dynamisch (aus der Vorlage kopiert,
  beim Stoppen gelöscht) oder statisch.

## Autoconfig für Netzwerke

Beim Hinzufügen eines Servers zu einem Netzwerk schreibt das Panel alles selbst:

- Velocity: Forwarding-Secret, Server-Liste und **Try-Liste** (pro Server wählbar, ob und an welcher Position, per
  Drag & Drop), Forced-Hosts
- Paper/Purpur/Folia: Velocity-Forwarding mit Secret, `online-mode=false`, Port
- Fabric: Proxy-Mod (z. B. FabricProxy-Lite) samt Config
- **Gruppen:** Instanzen melden sich im laufenden Proxy an und beim Stoppen wieder ab (Proxy-Plugin, ähnlich
  Comparator), ohne Neustart. Das Plugin kann auch die Lobby nach Auslastung verteilen, denn Velocity probiert die
  Try-Liste nur der Reihe nach.
- **Bausteine als Daten (YAML)** statt im Code, wie die „Configurators“ in SimpleCloud. Beispiele:
  - *Simple Voice Chat*: UDP-Port je Server, Config-Dateien (Paper/Fabric unterschiedlich), Port-Weiterleitung auf dem
    Proxy, Firewall-Hinweis. Die genauen Optionen vor dem Bau gegen die Doku prüfen.
  - später: Geyser/Floodgate, geteilte LuckPerms-Datenbank, ViaVersion, BlueMap

## Benutzer und Rechte

Von Anfang an: Konten, Teams, Rollen pro Projekt (Besitzer, Admin, Operator, Betrachter) mit feinen Rechten
(Konsole, Dateien, Backups, Plugins ...), API-Schlüssel, Audit-Log, möglichst früh Zwei-Faktor-Anmeldung.

So ist es umgesetzt (`internal/panel/rechte.go`):

- **Einzelrechte** (`Permission`, z. B. `console.read`, `files.write`, `backups.restore`, `members`) stehen in `permissionCatalog`,
  jedes mit deutschem Namen, Erklärung, Gruppe und Mindestrolle.
- **Rollen sind Vorlagen:** Eine Rolle ergibt genau die Rechte, deren Mindestrolle sie erreicht. Das entspricht den Stufen von
  vorher; `rechte_test.go` spielt jede Route mit jeder Rolle durch und beweist, dass sich nichts geändert hat.
- **Angepasste Rechte** liegen als JSON-Liste in `members.permissions` (leer = Vorlage). Eine neue Rolle ohne Liste setzt auf
  die Vorlage zurück; eine Liste gleich der Vorlage wird als Vorlage gespeichert. Eigentümer haben immer alles.
- **Eskalationsschutz:** Wer Mitglieder verwaltet (`members`), kann nur Rechte dazugeben, die er selbst hat, keine höhere Rolle
  vergeben und keine höher gestellten Mitglieder ändern. Wegnehmen geht immer. Zeitpläne prüfen zusätzlich die Rechte ihrer
  Schritte (Befehl, Starten/Stoppen, Backup).
- **An die Rolle gebunden** bleiben: Projekt löschen und Eigentum vergeben (Eigentümer), Benutzer, Nodes und Protokoll
  (globaler Administrator).
- Die API liefert `myPermissions` bei Projekt und Server, `permissions`/`customPermissions` bei jedem Mitglied und den Katalog
  unter `GET /api/permissions`; die Oberfläche blendet danach aus.

## Selbsthosten

Panel und Datenbank per `docker compose`; die Node als einzelnes Binary oder Container mit einem Einmal-Befehl, der
sie mit dem Panel verbindet. Datenbank hinter einer Schnittstelle, SQLite für kleine, Postgres für größere Setups.

## Reihenfolge

1. ✅ **Node-Daemon**: Server anlegen, starten, stoppen, Konsole live, Dateien, Limits, Wiederübernahme
2. ✅ Panel-Backend: Anmeldung, Benutzer, Datenbank, Nodes, Server-Verwaltung
3. ✅ Projekte (Einzelserver und Netzwerk), Next.js-Oberfläche
4. ✅ Datei-Editor mit Syntax-Highlight, Plugins/Mods (Modrinth, Updates), Logs, Backups, Spieler
5. ✅ Netzwerk-Autoconfig (Try-Reihenfolge, Forced Hosts, statische Server, Voicechat)
6. **Nächste Stufe:** Gruppen mit Scaling und Proxy-Plugin, Verbindung der Node nach außen, Netzwerke über mehrere
   Nodes, geplante Backups, Zwei-Faktor, Audit-Log
7. Forge/NeoForge, Geyser/Floodgate und weitere Bausteine, Spieler-Verwaltung über Rechte (Torch optional)

## Betrieb (Panel-Schleifen)

Das Panel startet beim Hochfahren fünf Hintergrundschleifen (`maintain.go`): Messwerte (`sampleLoop`), Autoscaler der Gruppen
(`scaleLoop`), Wächter (`watchLoop`, alle 10 s: Abstürze über den Exit-Code des Nodes, Auto-Restart, Node offline/online, wenig
Arbeitsspeicher; Ergebnisse als Ereignisse und Webhooks), Zeitpläne (`scheduleLoop`, mit Nachholen bis zu einer Stunde) und den
SFTP-Abgleich (`sftpLoop`, siehe „SFTP“).
Webhooks nutzen einen eigenen HTTP-Client, der Verbindungen in interne Netze sperrt (auch bei Weiterleitungen).

Sicherheit (`security.go`, `auth.go`): TOTP-Geheimnisse liegen mit AES-GCM verschlüsselt in der Datenbank (Schlüssel
`data/secret.key`), API-Schlüssel (`lds_…`) nur als SHA-256-Hash und nur für Anfragen, nicht für Konto-Änderungen. Jede
verändernde Anfrage landet im Protokoll (Muster und Pfad, ohne Inhalt).

Der Node prüft vor jedem Start den freien Arbeitsspeicher (`/proc/meminfo`, Reserve `LODESTONE_NODE_MEMORY_RESERVE_MB`) und
meldet beim Beenden eines Containers den Exit-Code mit dem Merkmal „angefordert“; so unterscheidet das Panel Absturz und Stopp.

## SFTP

Jeder Node bietet SFTP an (`internal/node/sftp*.go`, `golang.org/x/crypto/ssh` + `github.com/pkg/sftp`, Port
`LODESTONE_NODE_SFTP_LISTEN`, Standard `:2022`). Der Node kann das Panel nicht anrufen, deshalb ist das **Panel die einzige
Autorität**: `handlers_sftp.go` rechnet aus Mitgliedern, Rollen (ab Operator, globale Administratoren überall), gesperrten
Benutzern und den SFTP-Daten aus dem Konto (`store_sftp.go`: Passwort-Hash, Passwort-Login an/aus, SSH-Schlüssel, „Operatoren
nur lesend“ je Server) für jeden Server die vollständige Liste der Zugänge und schickt sie mit `PUT /api/servers/{id}/sftp-access`
zum Node. Die Schleife `sftpLoop` vergleicht alle 5 s einen Hash je Server und schickt nur Änderungen; Änderungen über die
SFTP-Routen stoßen sie sofort an. Alle 15 s fragt sie `GET /api/sftp` ab: Ist ein Node wieder erreichbar oder hat er eine neue
Start-Kennung (`bootId`), bekommen alle seine Server die Zugänge neu; neue Anmeldungen landen im Protokoll.

Der Node speichert die Liste je Server in `sftp-access.json` neben `spec.json` (außerhalb der Serverdateien, geht mit dem
Server), prüft beim Anmelden Passwort (argon2id im Format des Panels) oder Schlüssel und **vor jeder Dateioperation erneut**, ob
der Zugang noch besteht und schreiben darf; widerrufene Zugänge trennen offene Verbindungen. Dateizugriff nur über das `os.Root`
des Servers. Benutzername: `<benutzer>.<erste 8 Zeichen der Server-ID>`.

## Grenzen in v0.1

- Ein Netzwerk liegt auf **einem** Node (die Server erreichen sich über das Docker-Netz `lodestone`).
- Das Panel greift per HTTP auf die Nodes zu (kein Tunnel nach außen).
- Backups liegen auf der Maschine des Nodes (begrenzte Aufbewahrung); Zeitpläne lösen sie aus, ein externes Ziel fehlt.
- Die Weboberfläche liefert kein TLS selbst – Reverse-Proxy davor setzen.

## Offene Entscheidungen

- Lizenz (MIT oder AGPL-3.0), bevor das Projekt veröffentlicht wird
- Unabhängige Sicherheitsprüfung vor dem Öffnen für fremde Benutzer
