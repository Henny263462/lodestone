# 01 – Visuelles Design der Weboberfläche

Quelle der Wahrheit: `web/src/app/globals.css` (Variablen), `web/tailwind.config.ts` (Farbnamen, Schatten, Schriften) und
`web/src/components/ui/*.tsx` (Bausteine). Dieses Dokument beschreibt sie und sagt, **wie** man sie benutzt.

## 1. Grundhaltung

Lodestone sieht aus wie der **Minecraft-Launcher**: **dunkel, blockig, klar, ein Akzent**. Flächen sind kantig (kleine Rundungen),
Knöpfe sind kleine Blöcke mit heller Oberkante und dunkler Unterkante, die beim Drücken einsinken. Überschriften, Knöpfe und
Bereichsleisten stehen in einer **Pixelschrift**, Fließtext in einer gut lesbaren Schrift. Minecraft kommt über das **Pixel-Logo**,
die **Blocksymbole** für Projekte und Server, die **Pixel-Avatare**, die **Ping-Balken** und die Pixelschrift vor (`components/ui/mc.tsx`) –
nie über kopierte Spieltexturen, Spielnamen oder Bilder aus dem Spiel. Farbe bedeutet immer etwas (Zustand, Aktion). Nichts blinkt.

Jeder Benutzer stellt unter **Konto → Darstellung** Schriftstil und Akzentfarbe selbst ein (`lib/appearance.ts`). Deshalb gilt:
**nie eine Schrift oder Akzentfarbe fest setzen**, immer `font-display`/`font-sans` und `accent` benutzen.

## 2. Farben

Variablen in `:root` (RGB-Tripel, werden mit Tailwind-Alpha benutzt, z. B. `bg-accent/10`):

| Name (Tailwind) | Wert | Wofür |
| --- | --- | --- |
| `bg` | 22 22 24 | Seitenhintergrund |
| `surface` | 32 32 34 | Karten, Dialoge |
| `raised` | 46 46 49 | Erhöhte Flächen (Menüs, Auswahllisten, sekundäre Knöpfe) |
| `line` | 58 58 62 | Standard-Rahmenfarbe (`*` hat sie) |
| `ink` | 236 236 236 | Haupttext |
| `muted` | 160 160 166 | Nebentext, Beschriftungen, Icons |
| `accent` | vom Benutzer gewählt, Standard Smaragd 60 160 50 | Primäre Aktion, Auswahl, Fokus, Links, Logo-Nadel |
| `accent-ink` | passend zum Akzent (weiß oder dunkel) | Text und Symbole **auf** einer Akzentfläche (`text-accent-ink`) |
| `good` | 85 205 90 | Läuft, Erfolg |
| `warn` | 255 170 0 | Wartet, Warnung, wenig Ressourcen |
| `bad` | 240 80 80 | Fehler, Absturz, Gefahr |
| `info` | 88 180 255 | Hinweis, Installation läuft |

Akzentfarben (`[data-accent=…]` in `globals.css`): `emerald` (Smaragd, Standard), `diamond` (Diamant), `lapis` (Lapislazuli),
`amethyst` (Amethyst), `copper` (Kupfer). Neue Akzente brauchen einen Eintrag in `globals.css`, `lib/appearance.ts` **und**
`public/appearance.js`, und `accent-ink` muss auf dem Akzent lesbar sein.

Tiefe entsteht über Überlagerungen und Kanten:

- Fläche: Eingaben und Vertiefungen `bg-black/25` (mit `shadow-sunken`), Hover `hover:bg-white/[0.04–0.06]`, aktiv `bg-white/[0.08]`.
- Linie: `border-white/[0.06]` (innen, zwischen Zeilen), `border-white/[0.07]` (Karten), `border-white/[0.08–0.1]` (Eingaben),
  `border-black/40–50` (Knöpfe, Menüs, Dialoge, Seitenleiste – die harte Kante eines Blocks).

**Regeln**
- Ein Zustand hat **immer** dieselbe Farbe: läuft = `good`, startet/installiert = `warn`/`info`, aus = `muted` (neutral), Fehler = `bad`.
  Benutze `Badge tone="…"`, `Dot tone="…"` oder `SignalBars`, nicht eigene Farbklassen.
- Text ist `text-ink` (Standard) oder `text-muted`. Dazwischen `text-ink/80`. Nie reines Weiß für Fließtext, nie graue Hex-Werte.
- Der Akzent erscheint **sparsam**: ein primärer Knopf pro Ansicht, Auswahlzustände (Streifen links in der Seitenleiste, Strich unter
  dem aktiven Bereich), Fokusringe, Links (`text-accent hover:underline`).
- Gefahr = `bad` (rot) nur für Löschen/Zurücksetzen/Abstürze, nicht für gewöhnliche Hinweise.
- Verboten: Hex-Farben, `bg-blue-500`, `text-red-600` und ähnliche Tailwind-Paletten im normalen Code. Ausnahmen: die Pixelgrafiken
  in `lib/pixel-art.ts` (Texturen sind Bilder), die Diagrammfarben (`seriesColors` in `ui/chart.tsx`) und die MOTD-Farbcodes in `motd.tsx`.

## 3. Schrift

Zwei Rollen, beide über Variablen (Konto → Darstellung schaltet sie um, die Schriften bettet `next/font` beim Bauen ein):

| Klasse | Rolle | Standard („Minecraft“) |
| --- | --- | --- |
| `font-display` | Seitentitel, Kartentitel, Dialogtitel, Knöpfe, Bereichsleisten, Kennzahlen, Namen von Projekten/Servern | Pixelify Sans (Pixelschrift) |
| `font-sans` (Standard am `body`) | Fließtext, Beschreibungen, Eingaben, Tabellen | Inter |
| `font-mono` | Adressen, Befehle, Dateinamen, Logs | ui-monospace |

Schriftstile: `minecraft` (Pixel für Überschriften, Inter für Text), `pixel` (alles Pixel), `modern` (alles Inter), `system`.
Pixelschrift **nie fett** (`font-semibold`/`font-bold`) setzen: Sie verschmiert sonst; `font-medium` ist das Höchste. Unter
Pixel-Überschriften liegt der Schlagschatten aus der Klasse `text-pixel-shadow` (macht `PageHeader`).

Größen (feste Stufen, keine anderen benutzen):

| Zweck | Klassen |
| --- | --- |
| Seitentitel | `font-display text-2xl font-medium tracking-wide` (macht `PageHeader`) |
| Kartentitel | `font-display text-sm font-medium tracking-wide` (macht `CardHeader`) |
| Dialogtitel | `font-display text-base font-medium` (macht `Dialog`) |
| Fließtext, Eingaben | `text-sm` |
| Beschreibung unter Titeln | `text-[13px] text-muted` (Seite), `text-xs text-muted` (Karte, Dialog) |
| Beschriftung über Eingaben | Klasse `label` (`text-xs font-medium text-muted`) |
| Abzeichen | `text-[10.5px] font-medium` (macht `Badge`) |
| Kennzahlen | `font-display text-[28px] leading-none` (macht `StatCard`) |
| Adressen, Befehle, Dateinamen, Logs | `font-mono` (Konsole: Klasse `console-text`) |

Zahlen in Tabellen und Anzeigen: `tabular-nums`. Lange Texte kürzen mit `truncate` (und `min-w-0` am Flex-Kind).

## 4. Abstände, Rundung, Rahmen, Schatten

- Abstandsstufen (Tailwind): 1, 1.5, 2, 3, 4, 5, 6. Karteninnenabstand `px-5 py-4`, Raster `gap-4` (Karten) / `gap-5` (Bereiche),
  vertikale Gruppen `space-y-4` bzw. `space-y-5`. Keine Werte wie `p-[13px]`.
- Rundung: `globals.css` verkleinert Tailwinds Stufen (`rounded-xl` = 6 px, `-lg` = 4 px, `-md` = 3 px, `-sm` = 2 px). Karten/Dialoge
  `rounded-xl`, Eingaben/Zeilen `rounded-lg`, Knöpfe/Menüs/Kleinteile `rounded-md`, Abzeichen `rounded-sm`. **Kein `rounded-full`**
  außer für Diagrammpunkte: Im Blockstil gibt es keine Pillen.
- Rahmen: immer 1 px (siehe Farben). Farbige Rahmen nur bei Zustandskarten (`border-bad/25` mit `bg-bad/10` usw.).
- Schatten nur diese (in `tailwind.config.ts`): `shadow-bevel` (Knopf, Schaltknauf), `shadow-bevel-pressed` (gedrückter Knopf),
  `shadow-panel` (Karte, Dialog, Menü: Lichtkante oben, harte Schattenkante unten), `shadow-sunken` (Eingabe, Vertiefung). Keine
  weichen Schatten (`shadow-lg`, `shadow-2xl`), keine Verläufe, kein Leuchten.
- Seitenbreite: Inhalt höchstens `max-w-[1400px]`, Seitenrand `px-4 lg:px-10` (macht die Shell – nicht selbst setzen).

## 5. Bausteine (alle in `web/src/components/ui/`) und ihr Einsatz

| Baustein | Einsatz | Wichtig |
| --- | --- | --- |
| `Button` (`variant`: primary, secondary, ghost, danger; `size`: sm, md, lg, icon, icon-sm; `loading`) | Jede Aktion | Primär = höchstens einer pro Ansicht. Aus Tabellenzeilen: `size="sm"` oder `ghost`. Nur-Icon-Knöpfe **brauchen** `aria-label`. |
| `Card`, `CardHeader` (`title`, `description`, `action`) | Jeder Inhaltsblock | Kartenkopf immer mit `CardHeader`, Inhalt darunter mit `px-5 py-4`. |
| `PageHeader` (`title`, `description`, `action`, `crumbs`, `icon`, `meta`) | Oben auf jeder Seite | Hauptknopf in `action`. `crumbs` zeigt den Pfad (Übersicht → Projekt → Server, Verwaltung → Benutzer), `icon` ein `BlockIcon`/`PixelAvatar`, `meta` eine Zeile Abzeichen (Zustand, Software, Adresse). |
| `PageTabs` (`items` mit `id`, `label`, `href`, `icon`, `count`; `value`) | Bereiche eines Projekts oder Servers | Links, der Bereich steht in der Adresse. Direkt unter dem `PageHeader`. |
| `Tabs` | Umschalten **innerhalb** einer Karte oder eines Dialogs (Zeitraum, Ansicht) | Nicht für Seitenbereiche, dafür gibt es `PageTabs`. |
| `StatCard` (`tone`: green, blue, violet, amber, red, neutral) | Kennzahlen im Raster `grid gap-4 sm:grid-cols-2 xl:grid-cols-4` | Ton nach Bedeutung (violet = Akzent). Farbe tragen nur Streifen und Symbol. |
| `Badge`, `Dot` | Zustände, Rollen, kleine Etiketten | Tonnamen: neutral, good, warn, bad, info, accent. `Dot` ist ein kleines Quadrat. |
| `Switch` | Ein/Aus mit sofortiger Wirkung | Mit Beschriftung daneben (oder `label`), in `rounded-lg border border-white/[0.07] bg-white/[0.02] px-3 py-2.5`. |
| `Checkbox`, `SearchInput` | Auswahl, Suche | Suche immer mit `SearchInput`. |
| `RowMenu` | „⋯“-Menü für Zeilenaktionen | Gefährliches mit `danger: true`, durch `"separator"` abgetrennt. |
| `Select` (`value`, `onChange`, `options`, `size`) | Jede Auswahl: Software, Version, Node, Rollen, Zeiten | Eigene Liste im Panel-Stil. Kein natives `<select>`: dessen aufgeklappte Liste zeichnet der Browser im Systemstil. |
| `Meter`, `loadTone` | Auslastung als Balken | Farbe aus `loadTone(prozent)` (ab 75 % gelb, ab 90 % rot). |
| `EmptyState` (`icon`, `title`, `text`, `action`) | Leere Liste/Karte | Icon + kurzer Titel + ein Satz, was zu tun ist; der Knopf zum Anlegen kommt in `action`. |
| `Loading`, `ErrorState` (`ui/async.tsx`) | Laden, Fehler | `ErrorState` bekommt `retry`. |
| `Dialog`, `ConfirmDialog`, `useToast` (`ui/feedback.tsx`) | Eingaben im Dialog, Rückfragen, Rückmeldungen | `wide` nur für große Inhalte. |
| `SeriesChart`, `ChartLegend` (`ui/chart.tsx`) | Zeitreihen | Farben aus `seriesColors`, nicht eigene. |
| `LodestoneLogo`, `Wordmark` (`ui/mc.tsx`) | Marke | Die Nadel im Logo hat die Akzentfarbe des Benutzers. |
| `BlockIcon` (`name`, `size`) | Bild für Projekte und Server (Seitenleiste, Übersicht, Seitentitel) | Deterministisch aus dem Namen (Gras, Stein, Holz, Erz …). Bilder entstehen in `lib/pixel-art.ts`. |
| `PixelAvatar` (`name`, `size`) | Benutzer | Deterministisch aus dem Namen. |
| `SignalBars` (`level` 0–5, `className` für die Farbe) | Zustand eines Servers in Listen, wie die Ping-Anzeige im Spiel | Nur Zierde: immer zusammen mit `StateBadge` oder Text. |
| Klassen `field`, `label`, `panel`, `row-hover`, `text-pixel-shadow` (`globals.css`) | Eingabefelder, Beschriftungen, Zeilen-Hover, Schatten unter Pixeltext | `<input className="field">`, `<label className="label">`. |

Fachliche Bausteine, die mehrere Seiten teilen: `ServerRow`, `RoleBadges`, `softwareName` (`components/server-list.tsx`: Serverzeile im
Stil der Mehrspieler-Liste), `StateBadge`, `PowerButtons`, `CopyAddress` (`components/status.tsx`), `AppearanceCard`
(`components/appearance-settings.tsx`).

Fehlt dir ein Baustein: erst prüfen, ob sich der Bedarf mit den vorhandenen lösen lässt (meist ja). Wenn wirklich nötig, ergänze ihn in
`ui/primitives.tsx` im selben Stil (gleiche Tonnamen, `cx`, Tastaturbedienung), nicht in der Seite, und trage ihn hier ein.

## 6. Zustände und Interaktion

- Jedes klickbare Element hat Hover (`hover:bg-white/[0.04–0.06]` bzw. bei Knöpfen `hover:brightness-110–125`). Knöpfe sinken beim
  Drücken ein (`active:translate-y-px` und `active:shadow-bevel-pressed`, macht `Button`).
- Fokus: Eingaben zeigen `focus:border-accent/70 focus:ring-2 focus:ring-accent/20` (Klasse `field`). Nie `outline-none` ohne Ersatz.
- Deaktiviert: `disabled:opacity-50` (macht `Button`), plus kurzer Grund als Text oder `title`, wenn nicht offensichtlich.
- Übergänge: `transition duration-100` bis `200`, für gleitende Bewegungen `ease-snappy` (schnell los, weich aus).
- Animationen (alle in `tailwind.config.ts`, alle 120–220 ms, keine anderen erfinden):
  - `animate-fade-in`: Seiten (Shell, Schlüssel = Pfad) und Bereichsinhalte (`<div key={tab} className="animate-fade-in">`).
  - `animate-pop-in` / `animate-pop-out`: Dialoge, Schnellsuche, `RowMenu`; `animate-overlay-in/-out`: der abgedunkelte Hintergrund dazu.
  - `animate-drawer-in/-out`: das Handy-Menü. `animate-toast-in/-out`: Meldungen.
  - `animate-fade-in-late`: `Loading` erscheint erst nach 180 ms, damit schnelle Antworten keinen Kringel aufblitzen lassen.
- Ausblenden: `usePresence(open)` aus `ui/primitives` hält ein Element nach dem Schließen kurz im Baum (`leaving` → `*-out`-Klasse,
  `pointer-events-none`). `Dialog` zeigt dabei den letzten Inhalt weiter, die Seite darf ihre Daten also sofort leeren.
- `Tabs` haben einen gleitenden Block unter dem aktiven Tab; `Meter`-Balken gleiten (500 ms) statt zu springen.
- Bei „Bewegung reduzieren“ im System schaltet `globals.css` alle Animationen ab (außer dem Lade-Kringel).
- Tastaturfokus: einheitlicher Rahmen über `:focus-visible` in `globals.css` (Akzent, 2 px); Eingaben behalten ihren Ring aus `field`.
- Mindest-Klickfläche: Icon-Knöpfe `h-8 w-8` (`icon-sm`) oder `h-10 w-10` (`icon`).

## 7. Layout-Muster

- **Shell** (`components/layout/shell.tsx`): eine Seitenleiste links (248 px, mit Strg+B einklappbar auf 68 px), dunkler als der Inhalt
  und durch eine harte Kante getrennt. Sie enthält **nur die globale Navigation**: Übersicht, Nodes, die Projekte (das aktive klappt
  seine Server auf), für Administratoren den Abschnitt **Verwaltung** und unten das Konto. Bereiche eines Projekts oder Servers stehen
  **nicht** in der Leiste, sondern als `PageTabs` auf der Seite. Die Leiste scrollt nie mit; der Inhalt scrollt mit dem Fenster.
- **Raster**: `grid gap-4 lg:grid-cols-2` für Karten nebeneinander, `grid gap-4 sm:grid-cols-2 xl:grid-cols-4` für Kennzahlen.
  Mobil immer eine Spalte, nichts darf seitlich überlaufen.
- **Listenzeile**: `flex items-center gap-3 px-5 py-2.5`, Trenner `divide-y divide-white/[0.05]`, Name `min-w-0 flex-1 truncate`,
  Aktionen rechts. Server immer als `ServerRow`.
- **Formular im Dialog**: `Dialog` mit `<div><label className="label">…</label><input className="field" /></div>` je Feld, zwei Felder
  nebeneinander mit `grid gap-3 sm:grid-cols-2`, Hilfetext darunter `text-xs text-muted`.
- **Hinweis-/Fehlerkasten**: `rounded-lg border border-bad/25 bg-bad/10 px-4 py-3 text-xs text-bad` (analog `warn`, `info`, `good`).
- **Auswahlkacheln** (Projektart, Schrift): `rounded-lg border p-3`, gewählt `border-accent/70 bg-accent/10`, sonst
  `border-white/[0.08] bg-black/20 hover:border-white/[0.16]`.

## 8. Barrierefreiheit und Mobil

- Kontrast: Text auf Flächen nur `ink` oder `muted`, auf Akzentflächen `accent-ink`. Zustand nie **nur** durch Farbe: immer Text oder
  Icon dazu (`SignalBars` und `Dot` sind nur Zierde).
- Alle Icon-Knöpfe `aria-label` (deutsch), Dialoge schließen mit Esc, Menüs ebenso.
- Unter `lg` klappt die Seitenleiste in ein Menü; `PageTabs` scrollen seitlich. Prüfe jede neue Seite gedanklich bei 360 px Breite
  (Tabellen: `overflow-x-auto`, Knopfgruppen: `flex-wrap`, lange Beschriftungen auf kleinen Bildschirmen mit `hidden sm:inline`).

## 9. Typische Fehler schwacher Modelle (und die Lösung)

| Falsch | Richtig |
| --- | --- |
| `className="bg-blue-600 text-white rounded px-4 py-2"` | `<Button variant="primary">` |
| `<div className="bg-gray-800 rounded p-4 shadow-lg">` | `<Card>` mit `CardHeader` |
| `<span className="text-green-500">läuft</span>` | `<Badge tone="good">läuft</Badge>` |
| `style={{ fontFamily: "Minecraft" }}`, `font-['Pixelify_Sans']` | `font-display` (folgt der Einstellung des Benutzers) |
| `font-display font-bold` | `font-display font-medium` (Pixelschrift nie fett) |
| `rounded-full` für Etiketten oder Knöpfe | `Badge` (`rounded-sm`), `Button` (`rounded-md`) |
| Eigene Tab-Leiste oder zweite Seitenleiste für Bereiche | `PageTabs` unter dem `PageHeader` |
| `alert("Gespeichert")`, `confirm("Sicher?")` | `toast.run(...)`, `ConfirmDialog` |
| Eigener Lade-Kringel oder „Loading…“ | `<Loading />` |
| Leere Fläche, wenn es nichts gibt | `<EmptyState icon title text action />` |
| Englische Texte („Save“, „Delete“, „Error“) | „Speichern“, „Löschen“, Meldung mit Ursache und Lösung |
| Neues Icon-Set, Emoji | `lucide-react` |
| `style={{ color: "#3b82f6" }}` | `text-accent` |
| Heller Hintergrund, Dark/Light-Umschalter | Es gibt nur das dunkle Design; einstellbar sind Schrift und Akzent |
