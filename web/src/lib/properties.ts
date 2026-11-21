// Die wichtigsten Schlüssel der server.properties mit Typ, Standardwert und Erklärung. Alles andere zeigt das Formular als
// freien Text unter „Weitere Einstellungen“; die Prüfung macht das Panel (internal/panel/welten.go).

interface Base {
  key: string;
  label: string;
  help: string;
  /** Wert, den Minecraft nimmt, wenn der Schlüssel fehlt. */
  fallback: string;
}

export type PropertyField =
  | (Base & { type: "bool" })
  | (Base & { type: "int"; min: number; max: number })
  | (Base & { type: "enum"; options: { value: string; label: string }[] })
  | (Base & { type: "text"; maxLength: number; multiline?: boolean; placeholder?: string });

export interface PropertyGroup {
  title: string;
  description: string;
  fields: PropertyField[];
}

export const propertyGroups: PropertyGroup[] = [
  {
    title: "Allgemein",
    description: "Was Spieler in der Serverliste sehen und wie das Spiel grundsätzlich läuft.",
    fields: [
      { key: "motd", label: "Beschreibung (MOTD)", type: "text", maxLength: 512, multiline: true, fallback: "A Minecraft Server", help: "Text unter dem Servernamen in der Serverliste. Eine zweite Zeile beginnt nach einem Zeilenumbruch, Farben gehen mit §-Codes." },
      { key: "max-players", label: "Spieler höchstens", type: "int", min: 1, max: 100000, fallback: "20", help: "So viele Spieler können gleichzeitig online sein. Operatoren dürfen trotzdem beitreten." },
      {
        key: "difficulty",
        label: "Schwierigkeit",
        type: "enum",
        fallback: "easy",
        options: [
          { value: "peaceful", label: "Friedlich" },
          { value: "easy", label: "Einfach" },
          { value: "normal", label: "Normal" },
          { value: "hard", label: "Schwer" },
        ],
        help: "Friedlich: keine Monster, kein Hunger. Schwer: Monster richten mehr Schaden an, Hunger kann töten.",
      },
      {
        key: "gamemode",
        label: "Spielmodus",
        type: "enum",
        fallback: "survival",
        options: [
          { value: "survival", label: "Überleben" },
          { value: "creative", label: "Kreativ" },
          { value: "adventure", label: "Abenteuer" },
          { value: "spectator", label: "Zuschauer" },
        ],
        help: "Der Modus, in dem neue Spieler starten.",
      },
      { key: "force-gamemode", label: "Spielmodus erzwingen", type: "bool", fallback: "false", help: "Setzt jeden Spieler beim Beitreten auf den Spielmodus oben, auch wenn er vorher einen anderen hatte." },
      { key: "hardcore", label: "Hardcore", type: "bool", fallback: "false", help: "Schwer und nur ein Leben: Wer stirbt, wird zum Zuschauer." },
    ],
  },
  {
    title: "Spieler und Zugang",
    description: "Wer beitreten darf und was Spieler dürfen.",
    fields: [
      { key: "white-list", label: "Whitelist", type: "bool", fallback: "false", help: "Nur Spieler auf der Whitelist dürfen beitreten. Die Liste pflegst du unter Spieler." },
      { key: "enforce-whitelist", label: "Whitelist sofort durchsetzen", type: "bool", fallback: "false", help: "Wirft Spieler, die nicht (mehr) auf der Whitelist stehen, sofort vom Server, statt erst beim nächsten Beitritt." },
      { key: "online-mode", label: "Konten prüfen (online-mode)", type: "bool", fallback: "true", help: "Prüft bei Mojang, ob ein Spieler ein echtes Minecraft-Konto hat. Ohne Prüfung kann sich jeder als jeder ausgeben." },
      { key: "pvp", label: "PvP", type: "bool", fallback: "true", help: "Spieler können einander Schaden zufügen." },
      { key: "allow-flight", label: "Fliegen erlauben", type: "bool", fallback: "false", help: "Verhindert, dass Spieler im Überlebensmodus wegen Fliegens gekickt werden. Nötig für manche Plugins und Mods (z. B. Jetpacks)." },
      { key: "spawn-protection", label: "Spawn-Schutz (Blöcke)", type: "int", min: 0, max: 10000, fallback: "16", help: "Um den Spawn darf in diesem Umkreis nur ein Operator bauen. 0 schaltet den Schutz ab." },
      { key: "player-idle-timeout", label: "Untätige Spieler kicken (Minuten)", type: "int", min: 0, max: 100000, fallback: "0", help: "Wer so lange nichts tut, wird gekickt. 0 heißt: nie." },
      {
        key: "op-permission-level",
        label: "Rechte von Operatoren",
        type: "enum",
        fallback: "4",
        options: [
          { value: "1", label: "1 – Spawn-Schutz umgehen" },
          { value: "2", label: "2 – Spielbefehle (Cheats)" },
          { value: "3", label: "3 – Spieler verwalten" },
          { value: "4", label: "4 – alles, auch Server stoppen" },
        ],
        help: "Was ein Spieler mit /op darf.",
      },
    ],
  },
  {
    title: "Welt",
    description: "Welche Welt geladen wird und wie neue Welten entstehen. Seed und Struktur wirken nur auf neu erzeugte Welten.",
    fields: [
      { key: "level-name", label: "Weltordner", type: "text", maxLength: 64, fallback: "world", placeholder: "world", help: "Der Ordner, den der Server als Welt lädt. Gibt es ihn nicht, wird eine neue Welt erzeugt. Paper legt Nether und End daneben als <name>_nether und <name>_the_end an." },
      { key: "level-seed", label: "Seed", type: "text", maxLength: 128, fallback: "", placeholder: "leer = zufällig", help: "Bestimmt, wie eine neue Welt aussieht. Für eine bestehende Welt ändert er nichts – setze sie dafür unter Welten zurück." },
      { key: "generate-structures", label: "Strukturen erzeugen", type: "bool", fallback: "true", help: "Dörfer, Tempel, Festungen und Co. in neu erzeugten Gebieten." },
      { key: "allow-nether", label: "Nether erlauben", type: "bool", fallback: "true", help: "Ohne Nether funktionieren Netherportale nicht." },
      { key: "enable-command-block", label: "Befehlsblöcke", type: "bool", fallback: "false", help: "Befehlsblöcke führen ihre Befehle aus. Aus Sicherheitsgründen standardmäßig aus." },
    ],
  },
  {
    title: "Leistung",
    description: "Wie weit der Server um jeden Spieler Welt lädt und berechnet. Kleinere Werte sparen CPU und Arbeitsspeicher.",
    fields: [
      { key: "view-distance", label: "Sichtweite (Chunks)", type: "int", min: 2, max: 32, fallback: "10", help: "So weit schickt der Server Chunks an Spieler. Jeder Chunk mehr kostet spürbar Leistung; 8 bis 10 ist ein guter Wert." },
      { key: "simulation-distance", label: "Simulationsweite (Chunks)", type: "int", min: 2, max: 32, fallback: "10", help: "In diesem Umkreis wachsen Pflanzen, bewegen sich Tiere und laufen Redstone-Schaltungen. Darf kleiner als die Sichtweite sein." },
    ],
  },
];

export const knownPropertyKeys = new Set(propertyGroups.flatMap((group) => group.fields.map((field) => field.key)));

/** Prüft einen Wert im Browser vor dem Speichern (nur zur Hilfe, die echte Prüfung macht das Panel). */
export function propertyProblem(field: PropertyField, value: string): string | null {
  switch (field.type) {
    case "int": {
      if (!/^-?\d+$/.test(value.trim())) return "Bitte eine ganze Zahl eingeben.";
      const number = Number(value);
      if (number < field.min || number > field.max) return `Erlaubt sind ${field.min} bis ${field.max}.`;
      return null;
    }
    case "text":
      if (value.length > field.maxLength) return `Höchstens ${field.maxLength} Zeichen.`;
      if (field.key === "level-name" && !/^[A-Za-z0-9_][A-Za-z0-9 _.+-]{0,63}$/.test(value)) return "Nur Buchstaben, Ziffern, Leerzeichen und _ . + - (ohne Schrägstrich).";
      return null;
    default:
      return null;
  }
}
