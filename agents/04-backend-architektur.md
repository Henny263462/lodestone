# 04 – Backend-Architektur (Go)

Gesamtbild und Begründungen: [`../docs/architecture.md`](../docs/architecture.md). Hier stehen die **Regeln für den Code**.

## 1. Bausteine

```
Browser ──HTTP──▶ Panel (cmd/lodestone-panel, internal/panel) ──HTTP+Bearer-Token──▶ Node (cmd/lodestone-node, internal/node) ──▶ Docker
                       │ SQLite (Benutzer, Projekte, Server-Zeilen, Zeitpläne …)            │ Dateien der Server, Backups, Container
                       └ liefert die Oberfläche (internal/panel/webui, eingebettet)         └ internal/software (Paper, Velocity …: Versionen, Downloads, Java)
```

| Paket | Aufgabe | Darf nicht |
| --- | --- | --- |
| `internal/panel` | Benutzer, Rechte, Projekte, Netzwerke, Gruppen, Zeitpläne, Ereignisse, Weboberfläche; spricht **nur über `NodeClient`** mit Nodes | Docker oder Server-Dateien direkt anfassen |
| `internal/node` | Docker-Container, Server-Dateien (`os.Root`), Backups, Modpacks, playit, Speicherprüfung; kennt weder Benutzer noch Projekte | auf die Panel-Datenbank zugreifen, Rechte entscheiden |
| `internal/software` | Software-Katalog, Versionslisten, Prüfsummen, Java-Wahl | von `panel` oder `node` importieren |
| `internal/nodelink` | Protokoll der Verbindung, die ein Node selbst zum Panel aufbaut (Websocket + yamux, Kopfzeilen, Limits) | Benutzer, Docker oder Fachlogik kennen; von `panel` oder `node` importieren |
| `cmd/*` | Start, Umgebungsvariablen, Verdrahtung | Fachlogik |

Abhängigkeiten laufen nur in eine Richtung: `cmd` → `panel`/`node` → `software`/`nodelink`. `panel` importiert aus `node` nur **Datentypen** der Schnittstelle
(`node.View`, `node.PlayitStatus` …), nie Logik.

## 2. Dateien im Panel (wo gehört was hin)

| Datei | Inhalt |
| --- | --- |
| `panel.go` | `Config`, `Panel`, **alle Routen** (`routes()`), Middleware, `writeJSON`/`writeError`/`fail`/`decode`, Rollen (`rank*`, `requireProject/Server/Admin`) |
| `rechte.go`, `handlers_rechte.go`, `store_rechte.go` | Einzelrechte: Katalog, Vorlagen der Rollen, `requireProjectPermission`/`requireServerPermission`, `proxyPermissions`, Mitglieder-DTO, Eskalationsschutz |
| `auth.go`, `security.go`, `handlers_users.go` | Anmeldung, Sitzungen, Zwei-Faktor, API-Schlüssel, Benutzer |
| `anmeldung.go`, `handlers_anmeldung.go`, `store_anmeldung.go` | Anmeldung über OIDC und Passkeys (Einstellung, Abläufe im Speicher, Handler, Tabellen `oidc_links`/`passkeys`, Routen in `registerAnmeldungRoutes`) |
| `handlers_projects.go`, `handlers_servers.go`, `handlers_nodes.go`, `handlers_addons.go`, `handlers_extra.go`, `handlers_more.go` | HTTP-Handler nach Thema |
| `store.go` (Kern + Schema), `store_ext.go`, `store_more.go`, `store_backups.go` | Datenbank: Tabellen, Abfragen. **Nur hier steht SQL.** |
| `handlers_backups.go`, `s3.go` | Backups mit externem Ziel: Handler und Abläufe bzw. S3-Client (Signatur V4, vorsignierte Adressen) |
| `nodeclient.go` | HTTP-Client zum Node (`Do`, `Raw`, `Proxy`, `DialWS`) |
| `nodelink.go`, `handlers_nodelink.go`, `store_nodelink.go` | Nodes, die sich selbst verbinden: Tunnel-Transport je Node, Koppeln/Verbinden, Join-Tokens. Ein Node ohne Adresse (`url = ''`) läuft immer über den Tunnel; neue Node-Funktionen brauchen dafür nichts Eigenes |
| `network.go` | Autoconfig-Erzeugung für Velocity/Paper/Fabric (reine Funktionen, gut testbar) |
| `groups.go`, `scheduler.go`, `events.go`, `metrics.go`, `maintain.go` | Hintergrundschleifen und ihre Logik |
| `modrinth.go` | Modrinth-Client (mit Cache) |
| `automatik.go`, `handlers_automatik.go`, `store_automatik.go`, `discord.go`, `prometheus.go` | Automatik: Konsolen-Ereignisse und -Auslöser, Schlafmodus, Absturzanalyse, Discord-Bot, `/metrics` (Node-Seite: `internal/node/automatik.go`, `sleep.go`, `crash_analysis.go`) |
| `*_test.go` | Tests, siehe Abschnitt 8 |

Neue Datei nur, wenn ein neues **Thema** entsteht; sonst in die passende vorhandene Datei. Handler und Store eines Themas liegen in
verschiedenen Dateien (Handler `handlers_*.go`/Themendatei, SQL `store*.go`).

## 3. Das Handler-Muster (immer so)

Vorbild: `putAutoRestart` in `events.go`.

```go
func (p *Panel) putAutoRestart(w http.ResponseWriter, r *http.Request) {
    row, _, _, err := p.requireServerPermission(r, PermSettings)  // 1. Rechte (immer zuerst!)
    if err != nil { writeError(w, err); return }
    var body struct{ Enabled bool `json:"enabled"` }       // 2. Eingabe, strikt (unbekannte Felder = 400)
    if err := decode(r, &body); err != nil { writeError(w, err); return }
    // 3. prüfen: Länge, Muster, Bereiche → fail(http.StatusBadRequest, "deutsche Meldung")
    if err := p.store.SetAutoRestart(row.ID, body.Enabled); err != nil { writeError(w, err); return }  // 4. handeln
    writeJSON(w, http.StatusOK, …)                          // 5. Antwort (DTO, nie die Datenbankzeile mit Geheimnissen)
}
```

Route in `panel.go`: `mux.HandleFunc("PUT /api/servers/{id}/auto-restart", p.authenticate(p.putAutoRestart))`.

Regeln:
- **Jede** Route außer `healthz`, `auth/status|setup|login|totp`, `auth/oidc/*`, `auth/passkey/*` und `public/status/*` läuft durch `p.authenticate(...)`. Routen, die Konto
  oder Zugangsdaten betreffen, zusätzlich durch `p.sessionOnly(...)` (API-Schlüssel dürfen dort nicht hin).
- **Rechte prüfen** (`rechte.go`): Projekt-Routen mit `p.requireProjectPermission(r, projectID, PermXyz…)`, Server-Routen mit
  `p.requireServerPermission(r, PermXyz…)`. Ohne Recht genügt die Mitgliedschaft (Lesen, wie früher `rankViewer`). Mehrere
  Rechte heißen „alle nötig“. Zurück kommt `Access` (`Rank`, `Has(…)`, `List()`) für Entscheidungen im Handler
  (Beispiel: Befehle im Konsolen-Websocket nur mit `access.Has(PermConsoleCommand)`).
- Welches Recht? Das, dessen Erklärung im Katalog die Tat beschreibt: Lesen von Konsole/Protokoll `PermConsoleRead`, Befehle
  `PermConsoleCommand`, Start/Stopp `PermPower`, Dateien `PermFilesRead`/`PermFilesWrite`, Backups `PermBackups*`, Plugins/Mods
  `PermAddons`, Spieler `PermPlayers`, Einstellungen `PermSettings`, Server anlegen/löschen `PermServers`, Zeitpläne, Webhooks,
  Mitglieder, Netzwerk. Passt keines, ein **neues Recht** in `permissionCatalog` anlegen (ID-Konstante, Gruppe, deutscher Name,
  Erklärung, Mindestrolle = die Stufe, die du sonst gewählt hättest), den Typ `Permission` in `web/src/lib/types.ts` und den
  Katalog in `web/demo/server.mjs` ergänzen. Im Zweifel die strengere Wahl.
- **Rang statt Recht** nur für Eigentümer-Dinge (`requireProject(r, id, rankOwner)`: Projekt löschen) und Systemweites
  (`requireAdmin(r)`). `requireProject`/`requireServer` mit Mindeststufe gibt es weiter; neuer Code nimmt die Einzelrechte.
- Kann ein Handler etwas anstoßen, das weitere Rechte braucht (ein Zeitplan führt Befehle aus, eine Gruppe legt Server an,
  ein Konsolen-Auslöser startet einen Zeitplan, eine Discord-Regel erlaubt /stop), verlangt er auch diese (`stepPermissions`,
  `checkTriggerSchedule`, `discordPermissions`). Wer anderen Rechte gibt, prüft mit `checkGrant`, dass nichts dazukommt, was er
  selbst nicht hat.
- Fremde Projekte „gibt es nicht“: `errNotFound` (404), nicht 403 (macht `requireProject` schon).
- Zustand, den das Panel aus den Rechten ableitet und zum Node schiebt (SFTP-Zugänge), rechnet mit den **wirksamen** Rechten
  (`accessFor` mit angepasster Liste, nicht nur der Rolle). Jede Änderung an Mitgliedern, Rechten oder Benutzern ruft
  `p.sftpWake()` auf, damit ein Entzug sofort gilt.
- Fehler immer mit `fail(status, "deutscher Satz mit Ursache")` oder einem vorhandenen Fehler (`ErrNotFound`, `ErrExists`, `errForbidden`).
  Fehler vom Node reichen `writeError`/`NodeError` durch. Keine rohen `err.Error()` aus Bibliotheken an Benutzer, wenn sie Pfade oder
  Geheimnisse enthalten könnten.
- Antworten sind JSON mit camelCase-Tags. Listen als `[]` (nie `null`: leere Slices anlegen).
- Jede **verändernde** Route erscheint automatisch im Protokoll (`authenticate` schreibt es). Nichts Persönliches in den Pfad legen.
- Durchgereichte Node-Routen (`/api/servers/{id}/…`) müssen in `requiredRank` (handlers_servers.go) mit Stufe und erlaubter Methode eingetragen
  sein, sonst sind sie gesperrt, **und** in `proxyPermissions` (rechte.go) ihr Recht bekommen (fehlt es dort, gilt die Stufe).
  Neue Node-Funktion → beides eintragen, die Route in `TestProxyPermissionsMatchRanks` aufnehmen **und** `fakeNode` im Test erweitern.

## 4. Datenbank (SQLite, `modernc.org/sqlite`)

- Schema steht als `schema`/`schemaMore` (Strings mit `CREATE TABLE IF NOT EXISTS`). **Neue Tabelle** → in `schemaMore`, oder bei einem
  eigenen Thema als eigene Konstante in dessen Store-Datei, die `OpenStore` mit einer Zeile ausführt (Vorbild `schemaBackups`
  in `store_backups.go`).
- **Neue Spalte** an einer bestehenden Tabelle → Eintrag in `moreColumns` (`{table, name, definition}`), sie wird beim Start per `ensureColumn`
  nachgezogen. Die Definition braucht einen Standardwert (`INTEGER NOT NULL DEFAULT 0`).
- Nie Spalten löschen, umbenennen oder Typen ändern; nie Daten verwerfen. Ältere Datenbanken müssen weiter starten.
- IDs: `newID()` (Zufall, Hex). Zeit: Unix-Millisekunden (`time.Now().UnixMilli()`), Spaltenname `…_at`/`time`.
- SQL nur mit Platzhaltern (`?`), nie mit `fmt.Sprintf` aus Eingaben. Fehler `sql.ErrNoRows` → `ErrNotFound` übersetzen (Muster in `store.go`).
- Geheimnisse (Passwort, Sitzung, API-Schlüssel, Node-Token, TOTP): Passwörter argon2id, Sitzungs-/Schlüssel-Token nur als SHA-256-Hash,
  TOTP-Geheimnis mit AES-GCM (`secret.key`). Nie im Klartext speichern, nie in Antworten oder Logs.

## 5. Node

- Schnittstelle in `internal/node/api.go` und `api_extra.go`; jede Route prüft das Bearer-Token (konstante Zeit).
- **Dateien**: ausschließlich über `FS` (`files.go`, basiert auf `os.Root`). Nie `filepath.Join(serverDir, nutzerpfad)` mit direktem `os.*`.
  Entpacken verwirft Symlinks, Pfade nach außen und überschreibt nichts (`archive.go`).
- **Docker**: nur über das `Runtime`-Interface (`runtime.go`, Umsetzung `docker.go`), damit Tests eine Attrappe (`fake_test.go`) nehmen können.
  Container laufen als Nicht-Root, ohne Capabilities, mit `no-new-privileges`, Speicher-/CPU-/Prozess-Limit.
- **Externe Downloads** (Modrinth, Modpack-Dateien, Software): nur von erlaubten Hosts, mit Größenlimit, Prüfsumme (sha1/sha256/sha512),
  Zeitlimit. Siehe `modpack.go` (`safeModpackPath`, Host-Liste).
- **Arbeitsspeicher**: Start eines Servers geht nur nach `checkMemory` (Reserve `LODESTONE_NODE_MEMORY_RESERVE_MB`). Neue speicherhungrige
  Funktionen müssen diese Prüfung benutzen.
- Konfiguration über Umgebungsvariablen `LODESTONE_NODE_*` (`config.go`); jede neue Variable dort **und** in README/`.env.example` eintragen.
  Ausnahme: Die Verbindung zum Panel (`LODESTONE_NODE_PANEL_URL`, `_JOIN_TOKEN`) liest `LinkConfigFromEnv` in `nodelink.go`.

## 6. Hintergrundschleifen (Panel)

`maintain.go` startet: `sampleLoop` (Messwerte), `scaleLoop` (Gruppen), `watchLoop` (Abstürze, Auto-Restart, Node-Zustand, wenig RAM),
`scheduleLoop` (Zeitpläne). Regeln: jede Schleife endet mit dem `ctx`, fängt Fehler und loggt sie (`p.log.Warn`) statt abzustürzen,
hält Sperren (`sync.Mutex`) kurz und nie über Netzwerkaufrufe. Neue Hinweise an Benutzer laufen über `p.emit(projectID, serverID, kind, level, message)`
(→ Verlauf + Webhooks).

## 7. Sicherheit (nicht verhandelbar)

1. Rechte bei **jeder** Anfrage im Backend (Abschnitt 3). Die Oberfläche versteckt nur.
2. Eingaben, die in Befehle, Dateinamen, Konfigurationsdateien oder Adressen wandern, werden **streng** geprüft (Whitelist-Regex, wie `playerName`,
   `ipAddress`, `Slugify`) und nie in eine Shell eingesetzt. Konsolenbefehle: Zeilenumbrüche entfernen.
3. Ausgehende Anfragen auf Wunsch von Benutzern (Webhooks) nur über `newHookClient` (sperrt interne Adressen, keine Weiterleitungen),
   zu Backup-Zielen über `newS3HTTPClient` (ebenso, Freigabe mit `LODESTONE_BACKUP_ALLOW_INTERNAL`). Zugangsdaten externer Dienste
   bleiben im Panel (mit `seal` verschlüsselt); der Node bekommt höchstens vorsignierte, ablaufende Adressen.
4. Schreibende Browser-Anfragen brauchen den Header `X-Lodestone: 1` (CSRF); der Websocket prüft die Herkunft. Cookies `HttpOnly`, `SameSite=Strict`.
5. Antworten setzen `Cache-Control: no-store`; nichts Geheimes in URLs.
6. Anmeldeversuche sind gedrosselt; Registrierung gibt es nicht (nur Einrichtung beim ersten Start oder Admin legt an).
7. Öffentliche Routen (`/api/public/*`) liefern nur Unkritisches, sind ratenbegrenzt und gecacht.

## 8. Tests

- Jede neue Funktion bekommt Tests in der passenden `*_test.go`. Fehler- und Rechte-Fälle gehören dazu (z. B. „Betrachter bekommt 403“,
  „fremdes Projekt 404“, „ungültige Eingabe 400“). Neue Routen gehören zusätzlich mit ihrer Mindeststufe in die Liste von
  `TestRolesKeepTheirAccess` (`rechte_test.go`), die jede Route mit jeder Rolle prüft.
- Panel-Tests: `setupPanel(t)` startet Panel + `fakeNode` (HTTP-Attrappe des Nodes in `panel_test.go`); `newClient(t, url)` mit
  `expect(status, method, path, body)`. Neuer Node-Endpunkt → `fakeNode` in `panel_test.go` ergänzen.
- Node-Tests: `fake_test.go` ersetzt Docker. Echte Docker-Tests liegen getrennt (`docker_integration_test.go`) und laufen nicht standardmäßig.
- Externe Dienste (Modrinth, Mojang, Paper-API) werden mit `httptest`-Servern gespielt, nie im Test über das Internet angesprochen.
- Zeit-abhängiges (Zeitpläne, Fenster) mit festen Zeitstempeln testen, nicht mit `Sleep`.
- Läufe: `go test -count=1 ./...`. Auf Windows kann der **erste** Lauf wegen Loopback-Zeitüberschreitungen scheitern; Wiederholen. Schlägt es
  danach noch fehl, ist es ein echter Fehler.

## 9. Stil

- `gofmt` (Pflicht), `go vet ./...` sauber. Kommentare auf Deutsch, an jedem exportierten Namen ein Satz, der mit dem Namen beginnt
  („Schedule ist …“). Bezeichner Englisch, Fehlermeldungen Deutsch.
- Fehler mit `fmt.Errorf("…: %w", err)` weitergeben; Kontext (`context.Context`) an Netzwerk-/Docker-Aufrufe durchreichen.
- Keine globalen veränderlichen Variablen; Zustand hängt am `Panel`/`Manager`.
- Neue Abhängigkeiten (`go.mod`) nur auf Wunsch des Besitzers. Standardbibliothek zuerst.
