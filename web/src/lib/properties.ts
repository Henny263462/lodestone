import { t } from "@/lib/i18n";
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

export const propertyGroups = (): PropertyGroup[] => [
  {
    title: t("Allgemein"),
    description: t("Was Spieler in der Serverliste sehen und wie das Spiel grundsätzlich läuft."),
    fields: [
      { key: "motd", label: t("Beschreibung (MOTD)"), type: "text", maxLength: 512, multiline: true, fallback: "A Minecraft Server", help: t("Text unter dem Servernamen in der Serverliste. Eine zweite Zeile beginnt nach einem Zeilenumbruch, Farben gehen mit §-Codes.") },
      { key: "max-players", label: t("Spieler höchstens"), type: "int", min: 1, max: 100000, fallback: "20", help: t("So viele Spieler können gleichzeitig online sein. Operatoren dürfen trotzdem beitreten.") },
      {
        key: "difficulty",
        label: t("Schwierigkeit"),
        type: "enum",
        fallback: "easy",
        options: [
          { value: "peaceful", label: t("Friedlich") },
          { value: "easy", label: t("Einfach") },
          { value: "normal", label: t("Normal") },
          { value: "hard", label: t("Schwer") },
        ],
        help: t("Friedlich: keine Monster, kein Hunger. Schwer: Monster richten mehr Schaden an, Hunger kann töten."),
      },
      {
        key: "gamemode",
        label: t("Spielmodus"),
        type: "enum",
        fallback: "survival",
        options: [
          { value: "survival", label: t("Überleben") },
          { value: "creative", label: t("Kreativ") },
          { value: "adventure", label: t("Abenteuer") },
          { value: "spectator", label: t("Zuschauer") },
        ],
        help: t("Der Modus, in dem neue Spieler starten."),
      },
      { key: "force-gamemode", label: t("Spielmodus erzwingen"), type: "bool", fallback: "false", help: t("Setzt jeden Spieler beim Beitreten auf den Spielmodus oben, auch wenn er vorher einen anderen hatte.") },
      { key: "hardcore", label: "Hardcore", type: "bool", fallback: "false", help: t("Schwer und nur ein Leben: Wer stirbt, wird zum Zuschauer.") },
    ],
  },
  {
    title: t("Spieler und Zugang"),
    description: t("Wer beitreten darf und was Spieler dürfen."),
    fields: [
      { key: "white-list", label: t("Whitelist"), type: "bool", fallback: "false", help: t("Nur Spieler auf der Whitelist dürfen beitreten. Die Liste pflegst du unter Spieler.") },
      { key: "enforce-whitelist", label: t("Whitelist sofort durchsetzen"), type: "bool", fallback: "false", help: t("Wirft Spieler, die nicht (mehr) auf der Whitelist stehen, sofort vom Server, statt erst beim nächsten Beitritt.") },
      { key: "online-mode", label: t("Konten prüfen (online-mode)"), type: "bool", fallback: "true", help: t("Prüft bei Mojang, ob ein Spieler ein echtes Minecraft-Konto hat. Ohne Prüfung kann sich jeder als jeder ausgeben.") },
      { key: "pvp", label: "PvP", type: "bool", fallback: "true", help: t("Spieler können einander Schaden zufügen.") },
      { key: "allow-flight", label: t("Fliegen erlauben"), type: "bool", fallback: "false", help: t("Verhindert, dass Spieler im Überlebensmodus wegen Fliegens gekickt werden. Nötig für manche Plugins und Mods (z. B. Jetpacks).") },
      { key: "spawn-protection", label: t("Spawn-Schutz (Blöcke)"), type: "int", min: 0, max: 10000, fallback: "16", help: t("Um den Spawn darf in diesem Umkreis nur ein Operator bauen. 0 schaltet den Schutz ab.") },
      { key: "player-idle-timeout", label: t("Untätige Spieler kicken (Minuten)"), type: "int", min: 0, max: 100000, fallback: "0", help: t("Wer so lange nichts tut, wird gekickt. 0 heißt: nie.") },
      {
        key: "op-permission-level",
        label: t("Rechte von Operatoren"),
        type: "enum",
        fallback: "4",
        options: [
          { value: "1", label: t("1 – Spawn-Schutz umgehen") },
          { value: "2", label: t("2 – Spielbefehle (Cheats)") },
          { value: "3", label: t("3 – Spieler verwalten") },
          { value: "4", label: t("4 – alles, auch Server stoppen") },
        ],
        help: t("Was ein Spieler mit /op darf."),
      },
    ],
  },
  {
    title: t("Welt"),
    description: t("Welche Welt geladen wird und wie neue Welten entstehen. Seed und Struktur wirken nur auf neu erzeugte Welten."),
    fields: [
      { key: "level-name", label: t("Weltordner"), type: "text", maxLength: 64, fallback: "world", placeholder: "world", help: t("Der Ordner, den der Server als Welt lädt. Gibt es ihn nicht, wird eine neue Welt erzeugt. Paper legt Nether und End daneben als <name>_nether und <name>_the_end an.") },
      { key: "level-seed", label: "Seed", type: "text", maxLength: 128, fallback: "", placeholder: t("leer = zufällig"), help: t("Bestimmt, wie eine neue Welt aussieht. Für eine bestehende Welt ändert er nichts – setze sie dafür unter Welten zurück.") },
      { key: "generate-structures", label: t("Strukturen erzeugen"), type: "bool", fallback: "true", help: t("Dörfer, Tempel, Festungen und Co. in neu erzeugten Gebieten.") },
      { key: "allow-nether", label: t("Nether erlauben"), type: "bool", fallback: "true", help: t("Ohne Nether funktionieren Netherportale nicht.") },
      { key: "enable-command-block", label: t("Befehlsblöcke"), type: "bool", fallback: "false", help: t("Befehlsblöcke führen ihre Befehle aus. Aus Sicherheitsgründen standardmäßig aus.") },
    ],
  },
  {
    title: t("Leistung"),
    description: t("Wie weit der Server um jeden Spieler Welt lädt und berechnet. Kleinere Werte sparen CPU und Arbeitsspeicher."),
    fields: [
      { key: "view-distance", label: t("Sichtweite (Chunks)"), type: "int", min: 2, max: 32, fallback: "10", help: t("So weit schickt der Server Chunks an Spieler. Jeder Chunk mehr kostet spürbar Leistung; 8 bis 10 ist ein guter Wert.") },
      { key: "simulation-distance", label: t("Simulationsweite (Chunks)"), type: "int", min: 2, max: 32, fallback: "10", help: t("In diesem Umkreis wachsen Pflanzen, bewegen sich Tiere und laufen Redstone-Schaltungen. Darf kleiner als die Sichtweite sein.") },
    ],
  },
];

export const knownPropertyKeys = new Set(propertyGroups().flatMap((group) => group.fields.map((field) => field.key)));

/** Prüft einen Wert im Browser vor dem Speichern (nur zur Hilfe, die echte Prüfung macht das Panel). */
export function propertyProblem(field: PropertyField, value: string): string | null {
  switch (field.type) {
    case "int": {
      if (!/^-?\d+$/.test(value.trim())) return t("Bitte eine ganze Zahl eingeben.");
      const number = Number(value);
      if (number < field.min || number > field.max) return t("Erlaubt sind {min} bis {max}.", { min: field.min, max: field.max });
      return null;
    }
    case "text":
      if (value.length > field.maxLength) return t("Höchstens {maxLength} Zeichen.", { maxLength: field.maxLength });
      if (field.key === "level-name" && !/^[A-Za-z0-9_][A-Za-z0-9 _.+-]{0,63}$/.test(value)) return t("Nur Buchstaben, Ziffern, Leerzeichen und _ . + - (ohne Schrägstrich).");
      return null;
    default:
      return null;
  }
}
