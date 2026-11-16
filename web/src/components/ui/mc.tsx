"use client";

import { useMemo } from "react";
import { avatarCells, blockCells, logoCells, type Cell } from "@/lib/pixel-art";
import { cx } from "./primitives";

// Pixelgrafiken aus Rechtecken, ohne Weichzeichnen, wie Texturen im Spiel. Die Bilder selbst entstehen in lib/pixel-art.ts.

function Pixels({ cells, grid, size, label, className }: { cells: Cell[]; grid: number; size: number; label?: string; className?: string }) {
  return (
    <svg
      viewBox={`0 0 ${grid} ${grid}`}
      width={size}
      height={size}
      shapeRendering="crispEdges"
      className={cx("shrink-0", className)}
      role={label ? "img" : undefined}
      aria-label={label}
      aria-hidden={label ? undefined : true}
    >
      {cells.map((cell) => (
        <g key={`${cell.x}-${cell.y}`}>
          <rect x={cell.x} y={cell.y} width={1} height={1} fill={cell.color} />
          {cell.shade ? <rect x={cell.x} y={cell.y} width={1} height={1} fill={cell.shade > 0 ? "black" : "white"} opacity={Math.abs(cell.shade)} /> : null}
        </g>
      ))}
    </svg>
  );
}

/** Das Logo: ein Steinblock mit Kompassnadel in der Akzentfarbe. */
export function LodestoneLogo({ size = 28, className }: { size?: number; className?: string }) {
  const cells = useMemo(logoCells, []);
  return <Pixels cells={cells} grid={16} size={size} label="Lodestone" className={className} />;
}

/** Schriftzug in Pixelschrift mit dem Schlagschatten, den Minecraft unter Texten zeichnet. */
export function Wordmark({ className }: { className?: string }) {
  return <span className={cx("font-display font-semibold uppercase tracking-[0.12em] text-pixel-shadow", className)}>Lodestone</span>;
}

/** Blocksymbol für Projekte und Server; derselbe Name ergibt immer denselben Block. */
export function BlockIcon({ name, size = 32, className }: { name: string; size?: number; className?: string }) {
  const cells = useMemo(() => blockCells(name), [name]);
  return <Pixels cells={cells} grid={8} size={size} className={cx("rounded-[2px] ring-1 ring-black/50", className)} />;
}

/** Kopf im Stil eines Skins; derselbe Name ergibt immer dasselbe Gesicht. */
export function PixelAvatar({ name, size = 28, className }: { name: string; size?: number; className?: string }) {
  const cells = useMemo(() => avatarCells(name), [name]);
  return <Pixels cells={cells} grid={8} size={size} label={name} className={cx("rounded-[2px] ring-1 ring-black/50", className)} />;
}

/**
 * Fünf Balken wie die Ping-Anzeige in der Serverliste. `level` 0–5 sagt, wie viele leuchten; die Farbe kommt aus
 * `className` (z. B. text-good). Nur Zierde: Der Zustand steht immer zusätzlich als Text daneben.
 */
export function SignalBars({ level, className }: { level: number; className?: string }) {
  const heights = [2, 3, 5, 6, 8];
  return (
    <svg viewBox="0 0 9 8" width={14} height={12} shapeRendering="crispEdges" className={cx("shrink-0", className)} aria-hidden>
      {heights.map((height, index) => (
        <rect key={height} x={index * 2} y={8 - height} width={1} height={height} fill="currentColor" opacity={index < level ? 1 : 0.22} />
      ))}
    </svg>
  );
}
