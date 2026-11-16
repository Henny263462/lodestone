"use client";

import { Check } from "lucide-react";
import { useEffect, useState } from "react";
import { Card, CardHeader, cx } from "@/components/ui/primitives";
import { LodestoneLogo } from "@/components/ui/mc";
import { accentColors, defaultAppearance, fontStyles, onAppearanceChange, readAppearance, saveAppearance, type Appearance } from "@/lib/appearance";

/** Schriftstil und Akzentfarbe; gilt sofort und für diesen Browser. */
export function AppearanceCard() {
  const [appearance, setAppearance] = useState<Appearance>(defaultAppearance);

  useEffect(() => {
    setAppearance(readAppearance());
    return onAppearanceChange(() => setAppearance(readAppearance()));
  }, []);

  function update(patch: Partial<Appearance>) {
    const next = { ...appearance, ...patch };
    setAppearance(next);
    saveAppearance(next);
  }

  return (
    <Card id="darstellung" className="scroll-mt-6">
      <CardHeader title="Darstellung" description="Schrift und Akzentfarbe der Oberfläche. Wirkt sofort und gilt für diesen Browser." />
      <div className="space-y-5 px-5 py-4">
        <div>
          <p className="label">Schrift</p>
          <div role="radiogroup" aria-label="Schrift" className="grid gap-2 sm:grid-cols-2">
            {fontStyles.map((style) => {
              const selected = appearance.font === style.id;
              return (
                <button
                  key={style.id}
                  type="button"
                  role="radio"
                  aria-checked={selected}
                  onClick={() => update({ font: style.id })}
                  className={cx(
                    "relative rounded-lg border p-3 text-left transition active:translate-y-px",
                    selected ? "border-accent/70 bg-accent/10" : "border-white/[0.08] bg-black/20 hover:border-white/[0.16]",
                  )}
                >
                  {selected && <Check className="absolute right-3 top-3 h-3.5 w-3.5 text-accent" />}
                  {/* data-font: Die Vorschau zeigt die Schrift mit ihren echten Werten (globals.css) */}
                  <span data-font={style.id} className="block">
                    <span className="block font-display text-base tracking-wide">{style.label}</span>
                    <span className="mt-1 block font-sans text-xs text-muted">{style.text}</span>
                  </span>
                </button>
              );
            })}
          </div>
        </div>

        <div>
          <p className="label">Akzentfarbe</p>
          <div role="radiogroup" aria-label="Akzentfarbe" className="flex flex-wrap gap-2">
            {accentColors.map((color) => {
              const selected = appearance.accent === color.id;
              return (
                <button
                  key={color.id}
                  type="button"
                  role="radio"
                  aria-checked={selected}
                  onClick={() => update({ accent: color.id })}
                  className={cx(
                    "flex items-center gap-2 rounded-md border px-3 py-2 text-[13px] transition active:translate-y-px",
                    selected ? "border-white/[0.3] bg-white/[0.08] text-ink" : "border-white/[0.08] bg-black/20 text-muted hover:border-white/[0.16] hover:text-ink",
                  )}
                >
                  <span aria-hidden data-accent={color.id} className="h-4 w-4 rounded-sm bg-accent shadow-bevel" />
                  {color.label}
                  {selected && <Check className="h-3.5 w-3.5 text-ink" />}
                </button>
              );
            })}
          </div>
          <p className="mt-2 flex items-center gap-2 text-xs text-muted">
            <LodestoneLogo size={16} />
            Die Farbe gilt für Knöpfe, Auswahl, Links und die Kompassnadel im Logo.
          </p>
        </div>
      </div>
    </Card>
  );
}
