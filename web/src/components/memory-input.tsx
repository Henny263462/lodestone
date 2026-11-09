"use client";

import { useEffect, useState } from "react";
import { cx } from "@/components/ui/primitives";
import { megabytes } from "@/lib/format";

/**
 * Arbeitsspeicher als Zahleneingabe mit Schieberegler. Beide ändern denselben Wert in MB;
 * freie Werte aus dem Feld bleiben erhalten, auch wenn sie nicht auf dem Raster des Reglers liegen.
 */
export function MemoryInput({
  value,
  onChange,
  min = 256,
  max = 65536,
  step = 256,
  disabled,
}: {
  value: number;
  onChange: (value: number) => void;
  min?: number;
  max?: number;
  step?: number;
  disabled?: boolean;
}) {
  const [text, setText] = useState(String(value));

  useEffect(() => setText(String(value)), [value]);

  function commit(raw: string) {
    const parsed = Number(raw.replace(",", "."));
    if (!Number.isFinite(parsed)) {
      setText(String(value));
      return;
    }
    const next = Math.min(max, Math.max(min, Math.round(parsed)));
    setText(String(next));
    if (next !== value) onChange(next);
  }

  return (
    <div className={cx("space-y-2", disabled && "opacity-50")}>
      <div className="flex items-center gap-2">
        <input
          type="number"
          className="field w-24 tabular-nums"
          value={text}
          min={min}
          max={max}
          step={step}
          disabled={disabled}
          aria-label="Arbeitsspeicher in MB"
          onChange={(event) => {
            const raw = event.target.value;
            setText(raw);
            const parsed = Number(raw);
            // Schon beim Tippen übernehmen, solange der Wert im erlaubten Bereich liegt; geklemmt wird erst beim Verlassen.
            if (Number.isFinite(parsed) && parsed >= min && parsed <= max && parsed !== value) onChange(Math.round(parsed));
          }}
          onBlur={(event) => commit(event.target.value)}
          onKeyDown={(event) => {
            if (event.key === "Enter") {
              event.preventDefault();
              commit(event.currentTarget.value);
            }
          }}
        />
        <span className="text-xs text-muted">MB</span>
        <span className="ml-auto text-xs tabular-nums text-muted">{megabytes(value)}</span>
      </div>
      <input
        type="range"
        className="h-1.5 w-full cursor-pointer appearance-none rounded-full bg-white/[0.09] outline-none transition focus-visible:ring-2 focus-visible:ring-accent/20 disabled:cursor-not-allowed [&::-moz-range-thumb]:h-3.5 [&::-moz-range-thumb]:w-3.5 [&::-moz-range-thumb]:rounded-full [&::-moz-range-thumb]:border-0 [&::-moz-range-thumb]:bg-accent [&::-webkit-slider-thumb]:h-3.5 [&::-webkit-slider-thumb]:w-3.5 [&::-webkit-slider-thumb]:appearance-none [&::-webkit-slider-thumb]:rounded-full [&::-webkit-slider-thumb]:bg-accent [&::-webkit-slider-thumb]:shadow-glow"
        value={Math.min(max, Math.max(min, value))}
        min={min}
        max={max}
        step={step}
        disabled={disabled}
        aria-label="Arbeitsspeicher"
        onChange={(event) => onChange(Number(event.target.value))}
      />
      <div className="flex justify-between text-[11px] text-muted">
        <span>{megabytes(min)}</span>
        <span>{megabytes(max)}</span>
      </div>
    </div>
  );
}
