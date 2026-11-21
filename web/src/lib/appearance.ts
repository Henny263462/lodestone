/**
 * Darstellung, die jeder Benutzer selbst wählt (Konto → Darstellung): Schriftstil und Akzentfarbe.
 * Sie gilt pro Browser und liegt im localStorage. public/appearance.js liest dieselben Schlüssel schon vor dem
 * ersten Zeichnen, damit die Seite nicht kurz in der Standarddarstellung aufblitzt. Ändert sich hier ein Schlüssel
 * oder Wert, muss die Datei mitgeändert werden.
 */
import { t } from "@/lib/i18n";

export type FontStyle = "minecraft" | "pixel" | "modern" | "system";
export type AccentColor = "emerald" | "diamond" | "lapis" | "amethyst" | "copper";

export interface Appearance {
  font: FontStyle;
  accent: AccentColor;
}

export const defaultAppearance: Appearance = { font: "minecraft", accent: "emerald" };

// Listen mit Texten sind Funktionen, weil die Sprache erst beim Rendern feststeht
export const fontStyles = (): { id: FontStyle; label: string; text: string }[] => [
  { id: "minecraft", label: "Minecraft", text: t("Pixelschrift für Überschriften und Knöpfe, gut lesbare Schrift für Text.") },
  { id: "pixel", label: t("Pixel überall"), text: t("Alles in Pixelschrift, so nah am Spiel wie möglich.") },
  { id: "modern", label: t("Modern"), text: t("Eine klare, moderne Schrift für alles.") },
  { id: "system", label: t("System"), text: t("Die Schrift deines Betriebssystems.") },
];

// Die RGB-Werte stehen in globals.css; hier nur Name und Vorschaufarbe für die Auswahl
export const accentColors = (): { id: AccentColor; label: string }[] => [
  { id: "emerald", label: t("Smaragd") },
  { id: "diamond", label: t("Diamant") },
  { id: "lapis", label: t("Lapislazuli") },
  { id: "amethyst", label: t("Amethyst") },
  { id: "copper", label: t("Kupfer") },
];

const FONT_KEY = "lodestone.font";
const ACCENT_KEY = "lodestone.accent";
const EVENT = "lodestone:appearance";

function isFont(value: unknown): value is FontStyle {
  return fontStyles().some((style) => style.id === value);
}

function isAccent(value: unknown): value is AccentColor {
  return accentColors().some((color) => color.id === value);
}

/** Liest die gespeicherte Darstellung. Nur im Browser aufrufen (Effekte, Ereignisse). */
export function readAppearance(): Appearance {
  try {
    const font = window.localStorage.getItem(FONT_KEY);
    const accent = window.localStorage.getItem(ACCENT_KEY);
    return {
      font: isFont(font) ? font : defaultAppearance.font,
      accent: isAccent(accent) ? accent : defaultAppearance.accent,
    };
  } catch {
    // Gesperrter Speicher (privates Fenster, Richtlinie): dann gilt einfach der Standard
    return defaultAppearance;
  }
}

function apply(appearance: Appearance) {
  const root = document.documentElement;
  root.dataset.font = appearance.font;
  root.dataset.accent = appearance.accent;
}

/** Speichert und übernimmt die Darstellung sofort für die ganze Oberfläche. */
export function saveAppearance(next: Appearance) {
  try {
    window.localStorage.setItem(FONT_KEY, next.font);
    window.localStorage.setItem(ACCENT_KEY, next.accent);
  } catch {
    // Ohne Speicher gilt die Wahl nur bis zum Neuladen
  }
  apply(next);
  window.dispatchEvent(new CustomEvent(EVENT));
}

/** Meldet Änderungen der Darstellung, auch aus anderen Tabs. Gibt die Abmeldung zurück. */
export function onAppearanceChange(listener: () => void): () => void {
  const storage = (event: StorageEvent) => {
    if (event.key !== FONT_KEY && event.key !== ACCENT_KEY) return;
    apply(readAppearance());
    listener();
  };
  window.addEventListener(EVENT, listener);
  window.addEventListener("storage", storage);
  return () => {
    window.removeEventListener(EVENT, listener);
    window.removeEventListener("storage", storage);
  };
}
