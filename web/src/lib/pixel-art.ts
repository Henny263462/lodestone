/**
 * Pixelgrafiken als Listen von Zellen (Logo, Blocksymbole, Avatare). Reine Logik ohne React; gezeichnet wird in
 * components/ui/mc.tsx. Die festen Farben hier sind Texturen (Bilder), keine Oberflächenfarben.
 */

export interface Cell {
  x: number;
  y: number;
  color: string;
  /** Abdunkeln (positiv) oder Aufhellen (negativ) über der Grundfarbe, 0–1 */
  shade?: number;
}

export function hash(text: string): number {
  let h = 2166136261;
  for (let i = 0; i < text.length; i++) h = Math.imul(h ^ text.charCodeAt(i), 16777619);
  return h >>> 0;
}

/** Zufallszahlen aus einem Startwert, damit dieselbe Eingabe immer dasselbe Bild ergibt. */
function random(seed: number): () => number {
  let state = seed || 1;
  return () => {
    state ^= state << 13;
    state ^= state >>> 17;
    state ^= state << 5;
    return (state >>> 0) / 4294967296;
  };
}

// ---------------------------------------------------------------- Logo

/** Platzhalter für die Akzentfarbe des Benutzers (CSS-Variable). */
export const ACCENT = "rgb(var(--accent))";

const LOGO = {
  outline: "#141416",
  light: "#a9a9af",
  stone: "#7d7d83",
  speck: "#6c6c72",
  shade: "#4c4c52",
  panel: "#222226",
  tick: "#45454c",
  white: "#f0f0f0",
};

// Ein paar dunklere Pixel im Steinrahmen, damit er nach Stein aussieht und nicht nach Kunststoff
const SPECKS = new Set(["5,2", "10,2", "2,7", "2,11", "13,5", "13,9", "6,13", "11,13"]);

/**
 * Ein Steinblock (16 × 16) mit eingelassenem Kompass. Rahmen: Umriss, Lichtkante oben links, Stein, Schatten unten
 * rechts; in der Vertiefung (8 × 8) eine zwei Pixel breite Nadel von unten links nach oben rechts. Die obere Hälfte hat
 * die Akzentfarbe des Benutzers, die untere ist weiß; die rechte Seite der Nadel liegt im Schatten.
 */
export function logoCells(): Cell[] {
  const cells: Cell[] = [];
  for (let y = 0; y < 16; y++) {
    for (let x = 0; x < 16; x++) {
      const outer = x === 0 || y === 0 || x === 15 || y === 15;
      if (outer && (x === 0 || x === 15) && (y === 0 || y === 15)) continue; // abgerundete Ecken
      let color = LOGO.stone;
      let shade = 0;
      const inRecess = x >= 3 && x <= 12 && y >= 3 && y <= 12;
      if (outer) color = LOGO.outline;
      else if (inRecess) {
        if (x === 3 || y === 3) color = LOGO.shade; // Vertiefung: oben links im Schatten
        else if (x === 12 || y === 12) color = LOGO.light; // unten rechts angeleuchtet
        else {
          const px = x - 4;
          const py = y - 4;
          color = LOGO.panel;
          // Markierungen in der Mitte jeder Seite, wie auf einer Kompassscheibe
          if (((px === 3 || px === 4) && (py === 0 || py === 7)) || ((py === 3 || py === 4) && (px === 0 || px === 7))) color = LOGO.tick;
          const diagonal = px + py;
          if (diagonal === 7 || diagonal === 8) {
            color = py <= 3 ? ACCENT : LOGO.white;
            if (diagonal === 8) shade = py <= 3 ? 0.32 : 0.22;
          }
        }
      } else if ((x === 1 || y === 1) && x !== 14 && y !== 14) color = LOGO.light;
      else if (x === 14 || y === 14) color = LOGO.shade;
      else if (SPECKS.has(`${x},${y}`)) color = LOGO.speck;
      cells.push({ x, y, color, shade });
    }
  }
  return cells;
}

// ---------------------------------------------------------------- Blöcke

const ORES: [string, string][] = [
  ["#5fe3dc", "#2fa9a3"], // Diamant
  ["#f5cf45", "#c29a1c"], // Gold
  ["#45d978", "#1f9e4b"], // Smaragd
  ["#e04848", "#a22626"], // Redstone
  ["#4a6fe0", "#2a47a8"], // Lapislazuli
  ["#b07ae8", "#7a4bb5"], // Amethyst
];

type Texture = (x: number, y: number, next: () => number) => string;

const pick = (list: string[], next: () => number) => list[Math.floor(next() * list.length)];

const STONE: Texture = (_x, _y, next) => (next() < 0.12 ? "#606066" : pick(["#8b8b90", "#7d7d82", "#717176"], next));

const TEXTURES: Texture[] = [
  // Gras: grüne Oberseite, die etwas in die Erde hängt
  (_x, y, next) => (y < 2 || (y === 2 && next() < 0.55) ? pick(["#62a83c", "#559632", "#70b946"], next) : pick(["#8c5b37", "#7b4e2f", "#9a6941"], next)),
  STONE,
  // Holzbretter mit Fugen
  (x, y, next) => (y === 3 || y === 7 ? "#6b4f2e" : x === (y < 4 ? 2 : 6) ? "#7d5d36" : pick(["#b4884f", "#a77c46"], next)),
  // Sand
  (_x, _y, next) => pick(["#dccf9b", "#d2c58d", "#e5daa8"], next),
  // Tiefenschiefer
  (_x, y, next) => (y % 3 === 0 && next() < 0.5 ? "#38383e" : pick(["#4c4c53", "#424248", "#55555c"], next)),
  // Ziegel
  (x, y) => (y % 3 === 2 || x === (Math.floor(y / 3) % 2 === 0 ? 3 : 7) ? "#8f8a85" : "#9c4b3a"),
];

/**
 * Ein Block (8 × 8) für Projekte und Server, wie die Symbole in der Serverliste des Spiels. Aus dem Namen ergibt sich
 * immer derselbe Block; jeder vierte ist ein Erz mit farbigen Einschlüssen.
 */
export function blockCells(name: string): Cell[] {
  const seed = hash(name.toLowerCase());
  const next = random(seed);
  const ore = seed % 4 === 0;
  const texture = ore ? STONE : TEXTURES[(seed >>> 2) % TEXTURES.length];
  const oreColors = ORES[(seed >>> 5) % ORES.length];
  const orePixels = new Set<string>();
  if (ore) {
    for (let cluster = 0; cluster < 3; cluster++) {
      const ox = 1 + Math.floor(next() * 5);
      const oy = 1 + Math.floor(next() * 5);
      orePixels.add(`${ox},${oy}`);
      orePixels.add(`${ox + 1},${oy}`);
      if (next() < 0.6) orePixels.add(`${ox},${oy + 1}`);
    }
  }
  const cells: Cell[] = [];
  for (let y = 0; y < 8; y++) {
    for (let x = 0; x < 8; x++) {
      let color = texture(x, y, next);
      if (orePixels.has(`${x},${y}`)) color = next() < 0.65 ? oreColors[0] : oreColors[1];
      // Lichtkante oben links, Schatten unten rechts: der Block wirkt plastisch
      const shade = x === 0 || y === 0 ? -0.12 : x === 7 || y === 7 ? 0.22 : 0;
      cells.push({ x, y, color, shade });
    }
  }
  return cells;
}

// ---------------------------------------------------------------- Avatar

const SKIN = ["#f1c9a5", "#e2b08a", "#c68b61", "#9a643d", "#fbdcc0"];
const HAIR = ["#3a2a1a", "#6b4423", "#c9a24a", "#1d1d1d", "#8a3b1c", "#d8d8d8", "#3a3f9e"];
const EYES = ["#3b5bdb", "#2f9e44", "#5c3a21", "#1c7ed6", "#6741d9"];
const SHIRT = ["#00a8a8", "#b02e2e", "#2f9e44", "#7048e8", "#e8890c", "#3b5bdb"];

/** 8 × 8 Pixel, wie der Kopf eines Minecraft-Skins; Haut, Haare, Augen und Hemd ergeben sich aus dem Namen. */
export function avatarCells(name: string): Cell[] {
  const next = random(hash(name.toLowerCase()));
  const choose = (list: string[]) => list[Math.floor(next() * list.length)];
  const skin = choose(SKIN);
  const hair = choose(HAIR);
  const eyes = choose(EYES);
  const shirt = choose(SHIRT);
  const fringe = next() < 0.5 ? 1 : 2; // Haarlänge an der Stirn
  const sideburns = next() < 0.6;
  const mouth = darken(skin, 0.28);
  const cells: Cell[] = [];
  for (let y = 0; y < 8; y++) {
    for (let x = 0; x < 8; x++) {
      let color = skin;
      if (y < fringe + 1) color = hair; // Haar oben
      else if (y < 5 && (x === 0 || x === 7) && sideburns) color = hair;
      if (y === 4 && (x === 1 || x === 2 || x === 5 || x === 6)) color = x === 2 || x === 5 ? eyes : "#ffffff";
      if (y === 6 && x >= 2 && x <= 5) color = mouth;
      if (y === 7) color = shirt; // Kragen
      cells.push({ x, y, color });
    }
  }
  return cells;
}

function darken(hex: string, amount: number): string {
  const value = parseInt(hex.slice(1), 16);
  const channel = (shift: number) => Math.max(0, Math.min(255, Math.round(((value >> shift) & 255) * (1 - amount))));
  return `#${[16, 8, 0].map((shift) => channel(shift).toString(16).padStart(2, "0")).join("")}`;
}
