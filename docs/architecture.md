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
- **Verbindung Panel ↔ Node:** zwei Wege.
  - *Direkt:* Das Panel ruft die HTTP-Schnittstelle des Nodes auf (Bearer-Token); die Node-API gehört ins interne Netz
    oder hinter TLS/VPN.
  - *Node verbindet sich selbst* (`internal/nodelink`, empfohlen): Der Node baut einen Websocket zu
    `/api/node-link/connect` auf; yamux bündelt darin beliebig viele Datenströme. Das Panel öffnet für jede Anfrage einen
    Strom und spricht darüber gewöhnliches HTTP; der Node bedient ihn mit seinem unveränderten `http.Handler`
    (`internal/node/nodelink.go` setzt dabei sein eigenes Token ein). Im Panel bekommt der `NodeClient` eines solchen
    Nodes (in der Datenbank ohne Adresse) einen `http.Transport`, der statt TCP einen Strom wählt
    (`internal/panel/nodelink.go`); `Do`, `Raw`, `Proxy` (Down-/Uploads als Strom) und `DialWS` (Konsole) laufen so
    ohne Sonderfall. Je Node gilt höchstens eine Verbindung, eine neue ersetzt die alte. Der Node kann keine Ströme
    zum Panel öffnen (werden sofort geschlossen); Websocket-Nachrichten über 1 MiB beenden die Verbindung.
  - *Kopplung:* „Node hinzufügen“ legt den Node an und erzeugt ein Join-Token (`ldj_…`, 24 h, einmalig, nur als Hash in
    `node_join_tokens`). Der Node tauscht es an `/api/node-link/join` gegen ID und Geheimnis (`ldn_…`, im Panel nur als
    Hash in `nodes.link_secret_hash`, beim Node in `panel-link.json`). „Neu koppeln“ erzeugt ein neues Join-Token; wird
    es eingelöst, verliert das alte Geheimnis seine Gültigkeit. Beide Routen sind ohne Benutzeranmeldung erreichbar und
    je IP gedrosselt.
  - *Ausfall:* Der Node verbindet sich mit Backoff (1 s bis 60 s, bei Ablehnung 5 min) neu. Ohne Verbindung schlagen
    Anfragen sofort mit „nicht erreichbar“ fehl; der Wächter meldet das wie bei direkten Nodes als `node_offline`.

## Datenmodell

- **Projekt**: Einzelserver oder Netzwerk; hat Mitglieder mit Rollen pro Projekt.
- **Server** gehört zu einem Projekt und läuft auf einer Node. Er hat eine **Software** (Paper, Purpur, Folia, Fabric,
  NeoForge, Forge, Vanilla, Velocity), Version, Java-Version, Startzeile und Limits. Ein Server ist entweder
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
  - *Geyser + Floodgate* (umgesetzt, `internal/panel/inhalte_blocks.go`): Bedrock-Port ab 19132 mit gleichem Container- und
    Host-Port über TCP und UDP; im Netzwerk Geyser und Floodgate auf dem Proxy (`send-floodgate-data: true`), Floodgate auf den
    Servern dahinter, ein gemeinsamer `key.pem` (16 zufällige, druckbare Zeichen = AES-128-Schlüssel), mit `secret.key`
    verschlüsselt in `project_blocks` gespeichert und über die Autoconfig (`writes`) verteilt. Die Ansicht der Server an den
    Browser enthält den Inhalt geschriebener Dateien nicht (`redactView`).
  - *BlueMap* (umgesetzt): Plugin/Mod von Modrinth, `accept-download: true` in `core.conf` (Autoconfig-Format `hocon`, nur
    oberste Ebene), Port 8100 veröffentlicht, Link beim Server.
  - später: geteilte LuckPerms-Datenbank, ViaVersion

## Quellen für Plugins, Mods und Modpacks

Neben Modrinth (`modrinth.go`, erkennt Dateien an der SHA-1) kennt das Panel Hangar, Spigot (Spiget-API) und CurseForge
(`inhalte_sources.go`, gemeinsame Schnittstelle `contentSource`). Diese Quellen können Dateien nicht an der Prüfsumme
wiedererkennen; das Panel merkt sich deshalb beim Installieren die Herkunft (`addon_sources`) und prüft Updates darüber.
Der CurseForge-Schlüssel (`LODESTONE_CURSEFORGE_KEY`) bleibt im Panel. Modrinth-Modpacks gehen für Fabric, NeoForge und
Forge; der Node stellt bei NeoForge/Forge die Loader-Version aus `modrinth.index.json` ein (`internal/node/inhalte.go`).
Forge kommt aus dem offiziellen Maven (`maven-metadata.xml`, `promotions_slim.json`, Installer mit `.sha256`).

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
  Schritte (Befehl, Starten/Stoppen, Backup), Konsolen-Auslöser die ihres Zeitplans, Discord-Regeln mit /start, /stop oder
  /restart verlangen „Starten und Stoppen“.
- **Alle Funktionen** (SFTP, Backups mit Ziel, Welten, Kopieren/Umziehen, Speicherplatz, Automatik, Bausteine) prüfen
  Einzelrechte. Der SFTP-Abgleich rechnet mit den wirksamen Rechten (`sftp` + `files.read`, schreiben mit `files.write`) und
  läuft nach jeder Änderung an Mitgliedern, Rechten oder Benutzern sofort an.
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
7. ✅ Forge/NeoForge, Geyser/Floodgate, BlueMap, weitere Quellen (Hangar, Spigot, CurseForge); offen: Spieler-Verwaltung über
   Rechte (Torch optional), CurseForge-Modpacks

## Betrieb (Panel-Schleifen)

Das Panel startet beim Hochfahren fünf Hintergrundschleifen (`maintain.go`): Messwerte (`sampleLoop`), Autoscaler der Gruppen
(`scaleLoop`), Wächter (`watchLoop`, alle 10 s: Abstürze über den Exit-Code des Nodes, Auto-Restart, Node offline/online, wenig
Arbeitsspeicher; Ergebnisse als Ereignisse und Webhooks), Zeitpläne (`scheduleLoop`, mit Nachholen bis zu einer Stunde) und den
SFTP-Abgleich (`sftpLoop`, siehe „SFTP“).
Webhooks nutzen einen eigenen HTTP-Client, der Verbindungen in interne Netze sperrt (auch bei Weiterleitungen).

Externe Backups (`handlers_backups.go`, `store_backups.go`, `s3.go`; Node: `backups_remote.go`): Das Backup-Ziel eines
Projekts (Endpoint, Region, Bucket, Präfix, Access Key, Secret) liegt in `backup_targets`, das Secret mit AES-GCM
verschlüsselt wie die TOTP-Geheimnisse. Das Panel signiert selbst (AWS Signature V4, mit den Testvektoren der AWS-Doku
geprüft) und gibt die Zugangsdaten nie weiter:

```
Panel ── CreateMultipartUpload (signiert) ──▶ S3
Panel ── vorsignierte PUT-Adresse je Teil ──▶ Node ── PUT Teil 1..n ──▶ S3      (ETags zurück ans Panel)
Panel ── CompleteMultipartUpload (signiert) ──▶ S3     · bei Fehler AbortMultipartUpload
Panel ── vorsignierte GET-Adresse ──▶ Node ── GET ──▶ S3 → Backup-Ordner → Einspielen (nur bei ausgeschaltetem Server)
```

Teile sind mindestens 64 MiB groß und werden so gewählt, dass es höchstens 1000 sind (Größengrenze der Node-Schnittstelle);
ein Backup darf damit bis knapp 5 TB groß sein. Der Node lädt jeden Teil bis zu dreimal. Was das Panel über ein Backup weiß
(Status, extern ja/nein, Objektschlüssel, festgehalten, Fehler), steht in `backup_records`; die Liste setzt sich aus dem
Backup-Ordner des Nodes und diesen Einträgen zusammen. Läuft beim Neustart des Panels ein Upload, gilt er danach als
unterbrochen (Teilupload wird verworfen). Ausschlüsse (`backup_settings`) gehen bei jedem Backup an den Node und stehen als
`.lodestone-backup.json` am Anfang des Archivs, damit das Einspielen ausgeschlossene Pfade stehen lässt. Festhalten ist auf
dem Node eine Markierungsdatei `<name>.locked` neben dem Archiv und im Panel ein Feld; beide Aufbewahrungen überspringen
festgehaltene Backups. Ziele im internen Netz sind gesperrt, außer `LODESTONE_BACKUP_ALLOW_INTERNAL=true`. Das gilt auch
für den Node: Er prüft beim Hoch- und Herunterladen die IP, mit der er wirklich verbindet (`transferClient`, gegen
DNS-Rebinding), die Freigabe schickt das Panel als `allowInternal` mit. Beim Einspielen darf ein Backup entpackt höchstens
den freien Platz des Nodes abzüglich Reserve belegen (`restoreBudget`); ein vom Ziel geladenes Backup, das sich nicht
einspielen lässt, wird wieder gelöscht.

Dazu kommt die **Automatik** (`automatik.go`, `automatikLoop`, alle 5 s): Das Panel schickt jedem Node per
`POST /api/console-watch` die Muster seiner Konsolen-Auslöser und holt dabei die neuen Ereignisse ab (Cursor aus `boot` + `seq`;
nach einem Neustart des Panels werden alte übersprungen). Der Node erkennt in der Konsole Beitritt/Verlassen und die Muster
(Go-`regexp`, also linear, höchstens 5 Treffer pro Minute und Muster) und meldet Schlafmodus-Ereignisse. Das Panel macht daraus
Ereignisse (`player_join`, `player_leave`, `console_match`, `sleep`, `wake`), startet bei einem Treffer auf Wunsch einen Zeitplan
und lässt beim Einschlafen/Aufwachen eines Proxys die Backend-Server mitgehen. Konsolen-Auslöser und Discord-Anbindung liegen in
eigenen Tabellen (`store_automatik.go`, `console_triggers`, `discord_bots`).

**Schlafmodus** (`internal/node/sleep.go`): Ist ein Server mit Schlafmodus aus, öffnet der Node auf dessen Host-Port einen
kleinen Minecraft-Listener (Handshake, Status mit eigener MOTD, Ping/Pong; Login → Server starten und mit Meldung trennen).
`doLaunch` schließt ihn vor dem Anlegen des Containers unter derselben Sperre, unter der er geöffnet wird, damit Docker den Port
sicher bekommt. Der Leerlauf (Spielerabfrage alle 30 s) stoppt den Server sauber (zählt nicht als Absturz). Die Einstellung liegt
in `sleep.json` im Serverordner (außerhalb von `data/`). Läuft der Node in einem Container mit eigenem Netz, ist der Schlafmodus
aus (`LODESTONE_NODE_SLEEP_MODE`), weil ein Listener dort von außen nicht erreichbar wäre.

**Absturzanalyse** (`internal/node/crash_analysis.go`): reine Auswertung des neuesten `crash-reports/*.txt` (nur, wenn er zum
letzten Lauf passt) und der letzten 400 Konsolenzeilen. `handleCrash` hängt die Zusammenfassung an das Absturz-Ereignis, die
Konsole zeigt sie über `GET /api/servers/{id}/crash-analysis`.

**Discord-Bot** (`discord.go`): öffentlicher Endpunkt `POST /api/public/discord/{projekt}/interactions`, ed25519-Signaturprüfung
aus der Standardbibliothek (mit Zeitfenster von 5 Minuten), ratenbegrenzt; Rechte je Discord-Rolle/-Benutzer pro Befehl, Bot-Token
AES-GCM-verschlüsselt (wie TOTP). Aktionen über Discord stehen im Protokoll (`discord:<Name>`). **Prometheus** (`prometheus.go`):
`GET /metrics`, nur mit `LODESTONE_METRICS_TOKEN`, Textformat ohne zusätzliche Bibliothek.

Sicherheit (`security.go`, `auth.go`): TOTP-Geheimnisse liegen mit AES-GCM verschlüsselt in der Datenbank (Schlüssel
`data/secret.key`), API-Schlüssel (`lds_…`) nur als SHA-256-Hash und nur für Anfragen, nicht für Konto-Änderungen. Jede
verändernde Anfrage landet im Protokoll (Muster und Pfad, ohne Inhalt).

Anmeldung über OIDC und Passkeys (`anmeldung.go` Einstellung und Abläufe, `handlers_anmeldung.go` Handler,
`store_anmeldung.go` Tabellen `oidc_links` und `passkeys`): OIDC läuft als Authorization Code Flow mit PKCE (`golang.org/x/oauth2`),
das ID-Token prüft `github.com/coreos/go-oidc/v3` (Signatur über JWKS, Aussteller, Empfänger, Ablauf) und das Panel die `nonce`.
`state`, PKCE-Verifier und `nonce` liegen nur im Speicher des Panels (zehn Minuten, einmal gültig) und sind an ein
`SameSite=Lax`-Cookie gebunden, damit der Rücksprung aus demselben Browser kommen muss. Konten werden über `issuer` + `sub`
verknüpft. Passkeys nutzen `github.com/go-webauthn/webauthn`; die Zeremonien (Herausforderungen) liegen ebenfalls nur im
Speicher (fünf Minuten, einmal gültig), gespeichert werden nur öffentlicher Schlüssel, Zähler und Merkmale. RP-ID und
Herkunft folgen aus `LODESTONE_PUBLIC_URL`. Nach jeder Anmeldung entsteht dieselbe Sitzung wie bei der Passwort-Anmeldung.
Einen neuen Anmeldeweg (Passkey registrieren, OIDC verknüpfen) gibt es nur nach `confirmIdentity`: aktuelles Passwort
(gedrosselt je Benutzer) oder, ohne Passwort, eine Sitzung, die höchstens `firstPasswordWindow` alt ist.

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

## Kopie, Umzug und Speicherplatz

- **Speicherplatz** (`internal/node/storage.go`): `KeepMeasuring` misst alle 5 Minuten jeden Serverordner (und die Backups)
  im Hintergrund, nach Uploads und Entpacken wird kurz danach nachgemessen; Anfragen lesen nur die Messung. Der freie Platz
  kommt aus `statfs` bzw. `GetDiskFreeSpaceExW` (`diskfree_*.go`). Das weiche Limit steht als `diskLimitMB` in `spec.json`;
  ist es erreicht, lehnen `Start` und die Middleware `storageGuard` (Upload, Entpacken, Download per Link, Modpack) ab.
  Das Panel prüft die Limits in `storageLoop` (alle 5 Minuten) und meldet `storage` bei 90 % und 100 %.
- **Kopie** (`internal/node/serverops.go`, `Clone`): kopiert den Ordner (Symlinks und Hilfsdateien nie, Welten = Ordner mit
  `level.dat` wahlweise) in einen Hilfsordner `servers/.clone-<id>`, vergibt neue Host-Ports und schiebt ihn erst am Ende an
  seinen Platz. Das Panel (`serverops.go`, `runClone`) trägt die Kopie ein und gleicht ein Netzwerk ab (`inTry` aus).
- **Umzug** (`runMigration`): stoppen → `GET /api/servers/{id}/export` am Quell-Node (tar.gz: `spec.json`, `data/…`,
  Abschlussvermerk) wird ohne Zwischenspeicher als Körper von `POST /api/servers/import` an den Ziel-Node gereicht →
  Ziel entpackt in `servers/.import-<id>` und übernimmt den Server erst, wenn der Vermerk und die gzip-Prüfsumme stimmen →
  `node_id` in einer Transaktion umtragen → alte Server löschen → Netzwerk abgleichen → wieder starten. Die Server-ID bleibt
  gleich (Docker-Namen `lodestone-<id>` im Netzwerk passen weiter). Bis zum Umtragen macht jeder Fehler und jeder Abbruch
  alles rückgängig.
- **Vorgänge** stehen in der Tabelle `server_ops` (Schritt, Bytes, Ergebnis, Hinweise). Der laufende Vorgang schreibt jede
  Sekunde ein Lebenszeichen und liest dabei den Abbruchwunsch; ohne Lebenszeichen seit 2 Minuten (Panel neu gestartet) gilt
  er als fehlgeschlagen. Pro Projekt läuft höchstens ein Vorgang.

## Welten, server.properties und eigene Ordnung

- **Node** (`internal/node/welten.go`, `properties.go`): Die `server.properties` wird mit einem eigenen Parser im Format von
  `java.util.Properties` gelesen (Kommentare, Fortsetzungszeilen, `\uXXXX`-Escapes). Beim Speichern ändert er nur die Zeilen
  der gesetzten Schlüssel, alles andere bleibt Byte für Byte; Nicht-ASCII schreibt er als `\uXXXX`, damit alte und neue
  Server die Datei gleich lesen. Welten sind Ordner direkt im Serverordner mit `level.dat`; Namen prüft eine strenge
  Regex, Zugriffe laufen über `FS` (`os.Root`). Zurücksetzen und Ersetzen gehen nur bei ausgeschaltetem Server; der
  Server steht dabei auf `restoring`, damit ihn niemand startet. Ein Welt-Upload wird erst vollständig in einen
  Hilfsordner entpackt (`Extract`: keine Symlinks, keine Pfade nach außen, Größenlimit) und auf `level.dat` geprüft,
  bevor die alte Welt weicht. Datapacks müssen ein Zip mit `pack.mcmeta` sein.
- **Panel** (`handlers_welten.go`, `welten.go`): eigene Handler mit Rechteprüfung (Lesen ab Operator, Zurücksetzen,
  Ersetzen und Formular-Speichern ab Admin, Datapacks ab Operator) und Prüfung der bekannten Schlüssel (Zahlbereiche,
  Auswahlwerte). `server-port` und im Netzwerk `online-mode` sind gesperrt, weil Lodestone sie selbst setzt.
  Downloads und Uploads reicht das Panel als Strom durch (`NodeClient.Proxy`).
- **Ordnung** (`store_welten.go`, Tabelle `project_prefs`): Favorit, Position und Schlagwörter je Benutzer und Projekt.
  Die Projektliste selbst bleibt unverändert; die Oberfläche ordnet sie mit `/api/me/project-prefs` (`lib/order.ts`).

## Grenzen in v0.1

- Ein Netzwerk liegt auf **einem** Node (die Server erreichen sich über das Docker-Netz `lodestone`); es zieht nur als Ganzes
  um, Netzwerke mit Gruppen (noch) gar nicht. Backups ziehen nicht mit um.
- Netzwerke über mehrere Nodes gibt es noch nicht: Die Verbindung nach außen trägt nur Anfragen des Panels an den Node,
  keinen Verkehr zwischen Servern auf verschiedenen Nodes.
- Backups liegen auf der Maschine des Nodes (begrenzte Aufbewahrung) und auf Wunsch in einem S3-kompatiblen Ziel; es
  gibt nur Vollsicherungen (keine inkrementellen), kein SFTP/restic.
- Die Weboberfläche liefert kein TLS selbst – Reverse-Proxy davor setzen.

## Offene Entscheidungen

- Lizenz (MIT oder AGPL-3.0), bevor das Projekt veröffentlicht wird
- Unabhängige Sicherheitsprüfung vor dem Öffnen für fremde Benutzer
