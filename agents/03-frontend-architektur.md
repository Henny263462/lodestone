# 03 – Frontend-Architektur (`web/`)

## 1. Technik (fest)

Next.js 16 (App Router) mit `output: "export"` + `trailingSlash: true` → **statische Dateien**, die das Go-Panel aus dem Binary ausliefert
(`internal/panel/webui/dist`). React 19, TypeScript 7 (strikt), Tailwind 4, SWR, lucide-react, CodeMirror 6, qrcode.
Globale CSS-Importe sind in `web/src/styles.d.ts` deklariert; die Stylesheets verarbeitet der Next.js-Bundler.
Tailwind läuft über `@tailwindcss/postcss`. `globals.css` lädt `tailwind.config.ts` ausdrücklich per `@config`;
Farben, Schriften und Animationen bleiben dort definiert. Die bisherigen Werte für `shadow-sm`, `backdrop-blur-sm`
bleiben in `globals.css` festgehalten. Unsichtbare Konturen verwenden `outline-hidden`, damit sie im Modus
für erzwungene Farben weiterhin erkennbar sind.
Das hat Folgen:

- Es gibt **keinen Next-Server**: keine API-Routen, keine Server Actions, keine Serverkomponenten mit Daten, kein `next/image`-Optimizer,
  keine Middleware, keine dynamischen Segmente (`[id]`). IDs reisen als **Suchparameter**: `/server/?id=…&tab=…`, `/project/?id=…`.
- Jede Seite beginnt mit `"use client"`. Seiten, die `useSearchParams` nutzen, stecken in `<Suspense>` (siehe `server/page.tsx`).
- Links zwischen Seiten: `next/link` mit abschließendem Schrägstrich (`/nodes/`, `/project/?id=…`).
- Entwicklung: Oberfläche auf Port 3100, Panel auf 8480; `apiBase()` in `lib/api.ts` kümmert sich darum. Nichts hartkodieren.

## 2. Ordner

```
web/src/
  app/
    layout.tsx, globals.css         Wurzel (Schriften über next/font, lädt /appearance.js), Design-Variablen
    login/page.tsx                  ohne Shell
    status/page.tsx                 öffentlich, ohne Shell
    (app)/                          alles mit Anmeldung + Shell (layout.tsx prüft die Anmeldung)
      page.tsx                      Übersicht
      project/page.tsx, server/page.tsx, nodes/, users/, audit/, account/, admin/ (System, Projekte, Packs, playit, Updates)
  components/
    ui/                             Grundbausteine (primitives, feedback, async, chart, mc)  ← hier KEINE Fachlogik
    layout/                         shell.tsx (Seitenleiste), palette.tsx (Strg+K), navigation.tsx (saubere Pfade)
    server/                         Bereiche eines Servers (console, files, addons, backups, players, settings, stats …)
    *.tsx                           Fachkomponenten auf Projektebene (add-server, groups, schedules, events, motd, status-settings, status,
                                    server-list = Serverzeile, appearance-settings = Konto → Darstellung)
  lib/
    api.ts                          request/api/useApi/upload/downloadUrl/fetchText/putText, wsBase
    live.ts                         Live-Kanal (useLiveUpdates in der Shell), affects (welche Schlüssel eine Meldung betrifft), usePoll
    url-state.ts                    useUrlState: Tab/Abschnitt in der Adresse
    types.ts                        Typen der Schnittstelle + roleRank
    format.ts                       bytes, megabytes, dateTime, ago, uptime, compact, stateLabel, stateTone … (sprachabhängig)
    i18n.ts                         Übersetzung: t, tp, tx, msg, tFor, formatNumber, locale, backendError, permissionText
    i18n-provider.tsx               Sprache im Browser: I18nProvider, LanguageRoot, useLanguage, useUserLanguage
    messages/                       en.ts (Englisch je deutschem Text), plurals.ts (Pluralformen de/en), backend-en.ts (Backend-Meldungen, Rechte)
    audit.ts, crash.ts, editor.ts   reine Hilfen (Beschriftungen, Fehlererkennung, Dateitypen des Editors)
    appearance.ts                   Darstellung des Benutzers (Schrift, Akzent) lesen, speichern, anwenden
    pixel-art.ts                    Pixelgrafiken (Logo, Blocksymbole, Avatare) als Zellenlisten
web/public/appearance.js            setzt data-font/data-accent vor dem ersten Zeichnen (dieselben Schlüssel wie lib/appearance.ts)
web/demo/server.mjs                 Schein-Backend für die Demo (Port 3100)
```

Regeln für Orte:
- Eine **Seite** (`app/**/page.tsx`) ist dünn: Daten holen, Zustände (`Loading`/`ErrorState`), Aufbau, Fachkomponenten einsetzen.
  Wird sie länger als ~250 Zeilen, wandert ein Block in eine Komponente unter `components/`.
- **Fachkomponenten** liegen bei dem, was sie betreffen: Serverbereich → `components/server/`, Projekt → `components/`.
- **`components/ui/`** enthält nur Fachfreies. Nichts importiert dort aus `lib/types` Fachtypen (außer Tonnamen).
- **Reine Logik ohne React** (Parsen, Erkennen, Beschriften) → `lib/`.
- Pfadalias `@/` zeigt auf `web/src/`. Immer so importieren, nie `../../..`. (Innerhalb eines Ordners ist `./` ok.)

## 3. Daten laden und senden

```tsx
import { api, useApi } from "@/lib/api";
import type { Schedule } from "@/lib/types";

const { data, error, mutate } = useApi<Schedule[]>(`/projects/${project.id}/schedules`, { refreshInterval: 15000 });
// Pfad ohne "/api"-Präfix. Mit `null` als Pfad wird nichts geladen (bedingte Abfrage).

const toast = useToast();
await toast.run(() => api.post(`/projects/${project.id}/schedules`, body), "Zeitplan angelegt");
mutate();
```

- `useApi` = SWR mit `credentials: include`; `api.get/post/put/patch/delete` für Aktionen; `ApiError` trägt `status` und die deutsche Meldung.
- 401 leitet automatisch zur Anmeldung. 204 liefert `undefined`.
- Dateien: `upload()` (mit Fortschritt), `downloadUrl(pfad)` für Download-Links, `fetchText/putText` für Textdateien.
- Konsole: Websocket über `wsBase()`; nur dort.
- Live-Kanal: Die Shell hält genau eine `EventSource` auf `/api/live` offen (`useLiveUpdates`). Kommt eine Meldung, lädt `affects`
  die passenden SWR-Schlüssel neu (Pfad ohne `/api`). Neue Abfragen unter `/servers/{id}/…` oder `/projects/{id}/…` sind damit
  automatisch abgedeckt; eine neue Art Meldung braucht einen Fall in `affects` **und** im Backend (`updates.go`).
  Intervalle als Rückfall mit `usePoll(lang, kurz)`, nie mehr feste 3–15 s.
- Tab/Abschnitt in der Adresse: `const [tab, setTab] = useUrlState<Tab>("tab", "console", ["section"])` (der dritte Wert sind
  Parameter, die beim Wechsel wegfallen). Ungültige Werte auf den ersten erlaubten Eintrag zurückfallen lassen.
- `prefetch(pfad)` startet eine Abfrage vorab (z. B. die Projektliste parallel zur Anmeldeprüfung); `useApi` übernimmt sie dann.
- Große Bibliotheken (CodeMirror) nur über `next/dynamic` laden, damit sie nicht im Paket jeder Seite landen. Reine Hilfen dazu
  gehören nach `lib/` (Vorbild: `lib/editor.ts` und `components/server/files.tsx`).
- `prefetch(pfad)` startet eine Abfrage vorab (z. B. die Projektliste parallel zur Anmeldeprüfung); `useApi` übernimmt sie dann.
- Große Bibliotheken (CodeMirror) nur über `next/dynamic` laden, damit sie nicht im Paket jeder Seite landen. Reine Hilfen dazu
  gehören nach `lib/` (Vorbild: `lib/editor.ts` und `components/server/files.tsx`).
- Kein globaler Store (kein Redux/Zustand). Serverdaten = SWR-Cache, Formularzustand = `useState` in der Komponente, URL = Tabs/IDs.
- Eigene Hooks nur, wenn sie mindestens zweimal gebraucht werden (Beispiel `usePlayerActions` in `components/server/player-admin.tsx`).

## 4. Typen

- `lib/types.ts` spiegelt die JSON-Antworten **Feld für Feld** (camelCase wie im Go-`json:"…"`-Tag). Neue Antwort im Backend → derselbe
  Commit ergänzt den Typ. Optionale Felder (`omitempty` im Go) sind `?:` im Typ.
- Kein `any`. Kein `as`-Raten bei Antworten; Typ an `useApi<T>`/`api.get<T>` übergeben.
- Zustandsnamen sind Vereinigungstypen (`"running" | "offline" | …`), keine freien Strings.

## 5. Komponenten schreiben

- Funktionskomponenten, benannte Exporte (Seiten: `export default`). Props als Objekt mit Typ direkt an der Funktion.
- Klassen mit `cx(...)` aus `ui/primitives` zusammensetzen, keine Klassen-Strings mit Template-Verkettung aus Nutzereingaben.
- Kein `dangerouslySetInnerHTML`. Nutzer- und Servertexte werden als Text gerendert. MOTD-Vorschau parst MiniMessage selbst (`motd.tsx`).
- Ein Effekt pro Zweck, Abhängigkeiten vollständig. Aufräumen (Timer, Websocket, Listener) im Rückgabewert.
- Zugriff auf `window`/`document`/`localStorage` nur in Effekten oder Ereignissen (die Seite wird beim Bauen einmal vorgerendert).
- Listen mit stabilem `key` (ID, nicht Index, wenn sich die Liste ändert).
- Kommentare auf Deutsch, nur für das **Warum** („Der Kontext hängt an globalThis, weil …“). Kein Kommentar, der den Code wiederholt.

## 6. Rezept: neue Seite

1. Datei `web/src/app/(app)/<name>/page.tsx`, `"use client"`.
2. Typen in `lib/types.ts`, Daten mit `useApi`.
3. Aufbau nach `02` (PageHeader → Zustände → Karten). Rechte: `const user = useUser();` (aus `components/layout/shell`) für `isAdmin`.
4. Link in der Seitenleiste (`shell.tsx`: `mainEntries` für alle, `adminEntries` für Administratoren, mit lucide-Icon) **und** Eintrag
   in `palette.tsx`. Verwaltungsseiten bekommen `crumbs={[{ label: "Verwaltung" }, { label: "…" }]}`.
5. Demo: Endpunkte in `web/demo/server.mjs` ergänzen, damit die Seite dort funktioniert.
6. Prüfen: `npx tsc --noEmit`, `npx next build`.

## 7. Rezept: neuer Bereich im Server oder neuer Tab im Projekt

Vorbild `components/server/backups.tsx` (einfach) oder `players.tsx` (mit Diagrammen). Komponente `XyzTab({ server | project, canWrite })`,
Eintrag im `entries`-Array von `server/page.tsx` (Recht beachten: `allowed("…")`) bzw. im `tabs`-Array (`PageTab[]` mit `href`) von
`project/page.tsx`, Anzeige per `{active === "xyz" && <XyzTab … />}`, Ziel in `serverTargets`/`projectTargets` von `palette.tsx`. Die
Seitenleiste bleibt unverändert: Bereiche stehen nur in den `PageTabs`.

Neuer **Abschnitt** in Einstellungen (Server: `components/server/settings.tsx`, Projekt: `components/project-settings.tsx`, Konto:
`account/page.tsx`): Eintrag im `sections`-Array (ID, Label, lucide-Icon, Recht), Anzeige per `{active === "xyz" && <XyzCard … />}`.

## 8. Nicht tun

- Keine neue Bibliothek (UI-Kits, Formularbibliotheken, Icon-Sets, Datumsbibliotheken, Zustandsverwaltung, CSS-in-JS).
- Kein `fetch` außerhalb von `lib/api.ts`. Keine API-Adressen mit festem Host.
- Kein Inline-`style`, außer für berechnete Werte (Breite eines Balkens, Pixelgrößen von Avataren).
- Keine Server-Komponenten, `generateStaticParams`, `useRouter().push` auf dynamische Pfade.
- Keine Schrift über `@import` oder `<link>` von fremden Servern laden: Schriften kommen über `next/font` (wird beim Bauen eingebettet).
- Kein Umbenennen oder Verschieben bestehender Basisrouten (Lesezeichen und bestehende Adressen wie `/status/?p=` müssen als Fallback weiter funktionieren).
- Keine englischen Texte im Code (Englisch steht nur in `lib/messages/`), kein sichtbarer Text ohne `t()`, keine
  Zahlenformatierung von Hand (`lib/format`, `formatNumber`).
- `t()` nie auf Modulebene aufrufen (siehe §9).

## 9. Zwei Sprachen (`lib/i18n.ts`)

Eigene kleine Lösung ohne Bibliothek, passend zum statischen Export (keine Pfade wie `/en/`, die Sprache lebt im Browser).

- **Deutsch ist Quelle und Schlüssel:** `t("Backup erstellen")`, mit Platzhaltern `t("„{name}“ löschen?", { name })`. Der Typ
  des Schlüssels kommt aus `lib/messages/en.ts`; steht ein Text dort nicht, meldet `npx tsc --noEmit` den Aufruf. Neue Texte
  trägst du in `en.ts` unter der Datei ein, in der sie zuerst vorkommen. Ändert sich ein deutscher Text, ändert sich der
  Schlüssel – tsc zeigt dir dann die Stelle in `en.ts`.
- **Mehrzahl:** `tp("filesUploaded", anzahl, { … })` mit Formen `one`/`other` je Sprache in `lib/messages/plurals.ts`
  (`pluralsEn` muss jeden Schlüssel von `pluralsDe` haben, sonst Typfehler). `{count}` ist die Zahl.
- **Zahlen und Datum:** `formatNumber(wert, nachkommastellen)`, `locale()` für `toLocaleString`; die Hilfen in `lib/format.ts`
  nutzen das schon.
- **Modulebene:** Dort steht die Sprache noch nicht fest. Listen mit Texten (Abschnitte, Beschriftungen, Regeln) sind deshalb
  Funktionen, die beim Rendern übersetzen: `const sections = (): Section<Id>[] => [{ id: "x", label: t("…") }]`, Aufruf
  `sections()`. `msg("…")` markiert einen Text nur für die Prüfung, wenn er wirklich als Konstante bleiben muss.
- **Text, der erst zur Laufzeit feststeht** (Gründe und Beschreibungen vom Backend): `tx(text)` – bekannte Texte werden übersetzt,
  der Rest bleibt deutsch.
- **Fehlermeldungen des Backends** sind deutsche Sätze. `lib/api.ts` reicht sie durch `backendError(meldung, code)`: zuerst die
  genaue Meldung aus `lib/messages/backend-en.ts`, dann bekannte Anfänge („ungültige Anfrage: …“), dann die stabile Kennung
  `code` aus dem JSON (`forbidden`, `not_found`, `unauthorized`, `invalid_request`, `too_many_requests`, `node_offline`,
  `internal`). Unbekanntes bleibt deutsch. Neue häufige Meldung → Eintrag in `backendEn`.
- **Rechte-Katalog:** kommt deutsch vom Backend, mit stabiler `id` und `groupId`. `permissionText(info)` nimmt für Englisch
  `permissionsEn`/`permissionGroupsEn` (`backend-en.ts`), sonst das Deutsch des Backends. Ein neues Recht braucht dort einen
  Eintrag (Typfehler sonst). Ereignis-Titel übersetzt die Oberfläche über die `kind` (`eventLabels()` in `components/events.tsx`).
- **Wechsel:** `I18nProvider`/`LanguageRoot` (in `app/layout.tsx`) starten auf Deutsch wie das vorgerenderte HTML, laden dann
  die gespeicherte Wahl bzw. die Browsersprache und bauen den Baum bei einem Wechsel neu auf. Das englische Wörterbuch wird
  nur geladen, wenn jemand Englisch nutzt. Die Wahl im Konto (`PUT /api/me/language`, `language` in `/api/me` und
  `/api/auth/status`) übernimmt die Shell mit `useUserLanguage`.
- **Prüfen:** `node scripts/i18n-check.mjs` findet ungenutzte oder fehlende Einträge, abweichende Platzhalter und sichtbaren
  JSX-Text ohne `t()`. Es liest den Code mit dem SWC-Parser, den Next.js mitbringt (TypeScript 7 hat keine JavaScript-Schnittstelle zum Parsen mehr). Werte, die in jeder Sprache gleich sind (Adressen, Befehle, Produktnamen), stehen dort in `neutral`.
