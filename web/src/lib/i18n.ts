import type { en } from "@/lib/messages/en";
import { pluralsDe, pluralsEn, type PluralKey } from "@/lib/messages/plurals";
import type { PermissionInfo } from "@/lib/types";

// Übersetzung der Oberfläche ohne Bibliothek, ohne React (Provider und Hooks: lib/i18n-provider.tsx). Deutsch ist die Quelle: Ein Text wird deutsch geschrieben und in t("…")
// gepackt; der deutsche Text ist zugleich der Schlüssel (wie bei gettext). Das englische Wörterbuch (`messages/en.ts`)
// muss jeden dieser Schlüssel enthalten – fehlt einer, meldet `tsc` den Aufruf von t() als Fehler.

/** Language sind die Sprachen der Oberfläche. Deutsch ist Standard. */
export type Language = "de" | "en";

/** LanguageChoice ist die Wahl im Konto: "" heißt „wie der Browser“. */
export type LanguageChoice = Language | "";

/** MessageKey ist jeder deutsche Text, den die Oberfläche über t() zeigt. */
export type MessageKey = keyof typeof en;

export type Vars = Record<string, string | number | null | undefined>;

/** languageNames werden immer in der eigenen Sprache gezeigt, damit man seine Sprache auch findet, wenn man die andere nicht liest. */
export const languageNames: Record<Language, string> = { de: "Deutsch", en: "English" };

const storageKey = "lodestone.language";

// Die Sprache liegt außerhalb von React, damit reine Hilfen (lib/format, lib/audit …) sie lesen können. Sie ändert sich
// nur über den Provider (lib/i18n-provider.tsx), und der baut bei einem Wechsel den Baum neu auf (LanguageRoot).
let current: Language = "de";

/** setActiveLanguage setzt die gültige Sprache; nur der Provider ruft das auf (für "en" erst nach loadLanguage). */
export function setActiveLanguage(value: Language) {
  current = value;
}

// Das englische Wörterbuch wird erst geladen, wenn jemand Englisch wählt; wer Deutsch nutzt, lädt es nie.
type English = { en: typeof en } & typeof import("@/lib/messages/backend-en");
let english: English | null = null;

/** loadLanguage lädt das Wörterbuch einer Sprache (Deutsch steckt im Code und braucht nichts). */
export async function loadLanguage(target: Language): Promise<void> {
  if (target !== "en" || english) return;
  const [messages, backend] = await Promise.all([import("@/lib/messages/en"), import("@/lib/messages/backend-en")]);
  english = { en: messages.en, ...backend };
}

function englishText(key: MessageKey): string {
  return english?.en[key] ?? key;
}

/** language ist die gerade gültige Sprache. */
export function language(): Language {
  return current;
}

/** locale ist die Sprachkennung für Intl (Datum, Zahlen). */
export function locale(): string {
  return current === "en" ? "en-US" : "de-DE";
}

function fill(text: string, vars?: Vars): string {
  if (!vars) return text;
  return text.replace(/\{(\w+)\}/g, (match, name: string) => (name in vars ? String(vars[name] ?? "") : match));
}

/**
 * t übersetzt einen deutschen Text in die gültige Sprache und setzt Platzhalter ein: t("„{name}“ löschen?", { name }).
 * Nur beim Rendern oder in Ereignissen aufrufen, nie auf Modulebene (dort steht die Sprache noch nicht fest – nimm `msg`).
 */
export function t(key: MessageKey, vars?: Vars): string {
  return fill(current === "en" ? englishText(key) : key, vars);
}

/** tFor übersetzt in eine bestimmte Sprache, etwa für die Meldung nach einem Sprachwechsel. */
export function tFor(target: Language, key: MessageKey, vars?: Vars): string {
  return fill(target === "en" ? englishText(key) : key, vars);
}

/** msg markiert einen Text auf Modulebene (Listen, Tabellen) für die Prüfung; übersetzt wird er beim Rendern mit t(). */
export function msg<K extends MessageKey>(key: K): K {
  return key;
}

/**
 * tx übersetzt Text, der erst zur Laufzeit feststeht (Meldungen und Gründe vom Backend, Beschriftungen aus Daten).
 * Unbekannte Texte bleiben unverändert, also deutsch.
 */
export function tx(text: string, vars?: Vars): string {
  if (current === "en" && english) {
    const known = (english.en as Record<string, string>)[text] ?? english.backendEn[text];
    if (known) return fill(known, vars);
  }
  return fill(text, vars);
}

/** tp wählt die Pluralform passend zur Zahl: tp("files", 3) → „3 Dateien“ / „3 files“. */
export function tp(key: PluralKey, count: number, vars?: Vars): string {
  const forms = current === "en" ? pluralsEn[key] : pluralsDe[key];
  const form = new Intl.PluralRules(locale()).select(count) === "one" ? forms.one : forms.other;
  return fill(form, { count: formatNumber(count), ...vars });
}

/** formatNumber schreibt eine Zahl mit Tausender- und Dezimalzeichen der Sprache. */
export function formatNumber(value: number, fractionDigits?: number): string {
  return new Intl.NumberFormat(locale(), fractionDigits === undefined ? undefined : { minimumFractionDigits: fractionDigits, maximumFractionDigits: fractionDigits }).format(value);
}

/**
 * backendError übersetzt eine Fehlermeldung des Panels. Zuerst zählt die genaue Meldung (Liste in `messages/backend-en.ts`),
 * dann Meldungen mit bekanntem Anfang („ungültige Anfrage: …“), dann der stabile Fehlercode (`code` im JSON). Was nichts davon
 * trifft, bleibt deutsch.
 */
export function backendError(message: string, code?: string): string {
  if (current !== "en") return message;
  const known = english?.backendEn[message];
  if (known) return known;
  for (const [prefix, english] of errorPrefixes) {
    if (message.startsWith(prefix)) return english + message.slice(prefix.length);
  }
  if (code && errorCodes[code]) return errorCodes[code];
  return message;
}

const errorPrefixes: [string, string][] = [
  ["ungültige Anfrage: ", "Invalid request: "],
  ["interner Fehler: ", "Internal error: "],
  ["Node nicht erreichbar: ", "Node unreachable: "],
];

const errorCodes: Record<string, string> = {
  forbidden: "You don't have permission to do that.",
  not_found: "Not found.",
  unauthorized: "You're not signed in.",
  invalid_request: "Invalid request.",
  too_many_requests: "Too many attempts. Wait a few minutes and try again.",
  node_offline: "The node can't be reached. Check that it's running.",
  internal: "Internal error.",
};

/**
 * permissionText liefert Gruppe, Name und Erklärung eines Rechts. Das Backend schickt sie deutsch und mit stabilen IDs;
 * für Englisch zählt die ID (`messages/backend-en.ts`), fehlt sie dort, bleibt das Deutsch des Backends.
 */
export function permissionText(info: PermissionInfo): { group: string; label: string; description: string } {
  if (current !== "en") return { group: info.group, label: info.label, description: info.description };
  const text = english?.permissionsEn[info.id];
  return { group: english?.permissionGroupsEn[info.groupId] ?? info.group, label: text?.label ?? info.label, description: text?.description ?? info.description };
}

/** isLanguage prüft, ob ein Wert eine Sprache der Oberfläche ist. */
export function isLanguage(value: unknown): value is Language {
  return value === "de" || value === "en";
}

function stored(): Language | null {
  try {
    const value = window.localStorage.getItem(storageKey);
    return isLanguage(value) ? value : null;
  } catch {
    return null;
  }
}

/** rememberLanguage merkt sich die Wahl auf diesem Gerät ("" vergisst sie). */
export function rememberLanguage(choice: LanguageChoice) {
  try {
    if (choice) window.localStorage.setItem(storageKey, choice);
    else window.localStorage.removeItem(storageKey);
  } catch {}
}

/** browserLanguage ist die erste Sprache des Browsers, die Lodestone kann; sonst Deutsch. */
export function browserLanguage(): Language {
  if (typeof navigator === "undefined") return "de";
  const wanted = navigator.languages?.length ? navigator.languages : [navigator.language];
  for (const entry of wanted) {
    const base = (entry ?? "").toLowerCase().split("-")[0];
    if (isLanguage(base)) return base;
  }
  return "de";
}

/**
 * detectLanguage ist die Sprache beim Laden: die auf diesem Gerät gewählte, sonst die des Browsers. Die öffentliche
 * Statusseite folgt immer dem Browser, nicht einer Wahl, die jemand auf diesem Gerät im Panel getroffen hat.
 */
export function detectLanguage(): Language {
  if (window.location.pathname.startsWith("/status")) return browserLanguage();
  return stored() ?? browserLanguage();
}
