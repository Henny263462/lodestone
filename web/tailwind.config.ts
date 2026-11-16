import type { Config } from "tailwindcss";

const withAlpha = (variable: string) => `rgb(var(${variable}) / <alpha-value>)`;

const config: Config = {
  content: ["./src/**/*.{ts,tsx}"],
  theme: {
    extend: {
      colors: {
        bg: withAlpha("--bg"),
        surface: withAlpha("--surface"),
        raised: withAlpha("--raised"),
        line: withAlpha("--line"),
        muted: withAlpha("--muted"),
        ink: withAlpha("--ink"),
        accent: withAlpha("--accent"),
        // Textfarbe auf einer Fläche in Akzentfarbe (hell auf dunklen Akzenten, dunkel auf hellen)
        "accent-ink": withAlpha("--accent-ink"),
        good: withAlpha("--good"),
        warn: withAlpha("--warn"),
        bad: withAlpha("--bad"),
        info: withAlpha("--info"),
      },
      // Die Schriften kommen aus Variablen, damit jeder Benutzer sie unter Konto → Darstellung umstellen kann (globals.css)
      fontFamily: {
        sans: ["var(--font-body)"],
        display: ["var(--font-display)"],
        mono: ["ui-monospace", "SFMono-Regular", "Menlo", "Consolas", "monospace"],
      },
      // Blockiger Stil wie in Minecraft: Kanten statt weicher Schatten
      boxShadow: {
        // Knopf: helle Oberkante, dunkle Unterkante (wie ein Block, der leicht hervorsteht)
        bevel: "inset 0 1px 0 0 rgb(255 255 255 / 0.16), inset 0 -3px 0 0 rgb(0 0 0 / 0.32)",
        // Gedrückter Knopf: die Unterkante wird schmaler, der Knopf wirkt eingedrückt
        "bevel-pressed": "inset 0 1px 0 0 rgb(255 255 255 / 0.08), inset 0 -1px 0 0 rgb(0 0 0 / 0.32)",
        // Karte: feine Lichtkante oben, harte Schattenkante unten
        panel: "inset 0 1px 0 0 rgb(255 255 255 / 0.04), 0 2px 0 0 rgb(0 0 0 / 0.35)",
        // Eingabefeld: leicht vertieft
        sunken: "inset 0 2px 0 0 rgb(0 0 0 / 0.25)",
      },
      // Alle Bewegungen sind kurz (120–220 ms) und benutzen dieselbe Kurve: schnell los, weich aus
      transitionTimingFunction: {
        snappy: "cubic-bezier(0.16, 1, 0.3, 1)",
      },
      keyframes: {
        "fade-in": {
          from: { opacity: "0", transform: "translateY(4px)" },
          to: { opacity: "1", transform: "translateY(0)" },
        },
        "overlay-in": { from: { opacity: "0" }, to: { opacity: "1" } },
        "overlay-out": { from: { opacity: "1" }, to: { opacity: "0" } },
        "pop-in": {
          from: { opacity: "0", transform: "translateY(6px) scale(0.97)" },
          to: { opacity: "1", transform: "translateY(0) scale(1)" },
        },
        "pop-out": {
          from: { opacity: "1", transform: "scale(1)" },
          to: { opacity: "0", transform: "scale(0.97)" },
        },
        "drawer-in": { from: { transform: "translateX(-100%)" }, to: { transform: "translateX(0)" } },
        "drawer-out": { from: { transform: "translateX(0)" }, to: { transform: "translateX(-100%)" } },
        "toast-in": {
          from: { opacity: "0", transform: "translateY(10px) scale(0.97)" },
          to: { opacity: "1", transform: "translateY(0) scale(1)" },
        },
        "toast-out": {
          from: { opacity: "1", transform: "translateX(0)" },
          to: { opacity: "0", transform: "translateX(16px)" },
        },
      },
      animation: {
        "fade-in": "fade-in 0.2s cubic-bezier(0.16, 1, 0.3, 1) backwards",
        // Erst nach kurzer Wartezeit sichtbar, damit schnelle Antworten keinen Lade-Kringel aufblitzen lassen
        "fade-in-late": "fade-in 0.2s cubic-bezier(0.16, 1, 0.3, 1) 0.18s backwards",
        "overlay-in": "overlay-in 0.16s ease-out backwards",
        "overlay-out": "overlay-out 0.12s ease-in forwards",
        "pop-in": "pop-in 0.18s cubic-bezier(0.16, 1, 0.3, 1) backwards",
        "pop-out": "pop-out 0.12s ease-in forwards",
        "drawer-in": "drawer-in 0.22s cubic-bezier(0.16, 1, 0.3, 1) backwards",
        "drawer-out": "drawer-out 0.16s ease-in forwards",
        "toast-in": "toast-in 0.22s cubic-bezier(0.16, 1, 0.3, 1) backwards",
        "toast-out": "toast-out 0.16s ease-in forwards",
      },
    },
  },
  plugins: [],
};

export default config;
