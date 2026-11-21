"use client";

import { ArrowDown, ArrowUp, Network, Server as ServerIcon, Star } from "lucide-react";
import { useEffect, useState } from "react";
import { Badge, Button, cx } from "@/components/ui/primitives";
import { Dialog, useToast } from "@/components/ui/feedback";
import { api, useApi } from "@/lib/api";
import { allTags, type ProjectFilter } from "@/lib/order";
import type { Project, ProjectPref } from "@/lib/types";
import { t } from "@/lib/i18n";

/** Die eigene Ordnung (Favoriten, Reihenfolge, Schlagwörter) des angemeldeten Benutzers. */
export function useProjectPrefs() {
  return useApi<ProjectPref[]>("/me/project-prefs");
}

export function prefOf(prefs: ProjectPref[] | undefined, projectId: string): ProjectPref | undefined {
  return prefs?.find((pref) => pref.projectId === projectId);
}

/** Stern zum Anheften. Sitzt oft in einem Link, deshalb wird der Klick dort nicht weitergereicht. */
export function FavoriteButton({ project, pref, onChanged }: { project: Project; pref?: ProjectPref; onChanged: () => void }) {
  const toast = useToast();
  const [busy, setBusy] = useState(false);
  const favorite = !!pref?.favorite;
  return (
    <button
      type="button"
      aria-label={favorite ? t("{name} vom Anheften lösen", { name: project.name }) : `${project.name} anheften`}
      aria-pressed={favorite}
      title={favorite ? t("Angeheftet – klicken zum Lösen") : t("Anheften (steht dann oben)")}
      disabled={busy}
      onClick={async (event) => {
        event.preventDefault();
        event.stopPropagation();
        setBusy(true);
        await toast.run(() => api.put(`/me/project-prefs/${project.id}`, { favorite: !favorite }), favorite ? t("Nicht mehr angeheftet") : t("Angeheftet"));
        setBusy(false);
        onChanged();
      }}
      className={cx(
        "grid h-8 w-8 shrink-0 place-items-center rounded-lg transition duration-100 hover:bg-white/[0.06] active:scale-90 disabled:opacity-50",
        favorite ? "text-accent" : "text-muted hover:text-ink",
      )}
    >
      <Star className={cx("h-4 w-4", favorite && "fill-current")} />
    </button>
  );
}

/** Filter der Übersicht als Chips: Alle, Angeheftet und jedes Schlagwort. */
export function ProjectFilterChips({ prefs, value, onChange }: { prefs?: ProjectPref[]; value: ProjectFilter; onChange: (filter: ProjectFilter) => void }) {
  const tags = allTags(prefs);
  const hasFavorites = prefs?.some((pref) => pref.favorite);
  if (!hasFavorites && tags.length === 0) return null;
  const chip = (label: string, selected: boolean, filter: ProjectFilter) => (
    <button
      key={label}
      type="button"
      aria-pressed={selected}
      onClick={() => onChange(filter)}
      className={cx(
        "rounded-full border px-2.5 py-1 text-xs transition duration-100 active:scale-95",
        selected ? "border-accent/60 bg-accent/10 text-ink" : "border-white/[0.1] text-muted hover:bg-white/[0.06] hover:text-ink",
      )}
    >
      {label}
    </button>
  );
  return (
    <div className="mb-4 flex flex-wrap items-center gap-1.5">
      <span className="mr-1 text-xs text-muted">{t("Zeigen:")}</span>
      {chip(t("Alle"), value.kind === "all", { kind: "all" })}
      {hasFavorites && chip(t("Angeheftet"), value.kind === "favorites", { kind: "favorites" })}
      {tags.map((tag) => chip(tag, value.kind === "tag" && value.tag.toLowerCase() === tag.toLowerCase(), { kind: "tag", tag }))}
    </div>
  );
}

/** Schlagwörter eines Projekts als kleine Abzeichen. */
export function ProjectTags({ pref }: { pref?: ProjectPref }) {
  if (!pref || pref.tags.length === 0) return null;
  return (
    <div className="mt-3 flex flex-wrap gap-1">
      {pref.tags.map((tag) => (
        <Badge key={tag}>{tag}</Badge>
      ))}
    </div>
  );
}

interface Row {
  project: Project;
  favorite: boolean;
  tags: string;
}

const splitTags = (text: string) =>
  text
    .split(",")
    .map((tag) => tag.trim())
    .filter(Boolean);

/** Dialog zum Ordnen: Reihenfolge mit Pfeilen, Anheften und Schlagwörter. Gilt nur für dich. */
export function OrganizeDialog({ open, onClose, projects, prefs, onSaved }: { open: boolean; onClose: () => void; projects: Project[]; prefs?: ProjectPref[]; onSaved: () => void }) {
  const toast = useToast();
  const [rows, setRows] = useState<Row[]>([]);
  const [busy, setBusy] = useState(false);

  // Beim Öffnen vom aktuellen Stand ausgehen (projects ist schon geordnet)
  useEffect(() => {
    if (!open) return;
    setRows(projects.map((project) => ({ project, favorite: !!prefOf(prefs, project.id)?.favorite, tags: (prefOf(prefs, project.id)?.tags ?? []).join(", ") })));
    // Nur beim Öffnen übernehmen, nicht bei jeder Aktualisierung im Hintergrund
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [open]);

  function move(index: number, by: number) {
    setRows((current) => {
      const target = index + by;
      if (target < 0 || target >= current.length) return current;
      const next = [...current];
      [next[index], next[target]] = [next[target], next[index]];
      return next;
    });
  }

  const tooManyTags = rows.some((row) => splitTags(row.tags).length > 8 || splitTags(row.tags).some((tag) => tag.length > 24));

  async function save() {
    setBusy(true);
    const ok = await toast.run(async () => {
      // Angeheftete stehen ohnehin oben; die Reihenfolge der Liste gilt innerhalb beider Teile
      await api.put("/me/project-order", { projectIds: rows.map((row) => row.project.id) });
      for (const row of rows) {
        const before = prefOf(prefs, row.project.id);
        const tags = splitTags(row.tags);
        if (row.favorite !== !!before?.favorite || JSON.stringify(tags) !== JSON.stringify(before?.tags ?? [])) {
          await api.put(`/me/project-prefs/${row.project.id}`, { favorite: row.favorite, tags });
        }
      }
    }, t("Ordnung gespeichert"));
    setBusy(false);
    onSaved();
    if (ok) onClose();
  }

  return (
    <Dialog
      open={open}
      onClose={onClose}
      wide
      title={t("Projekte ordnen")}
      description={t("Reihenfolge in Seitenleiste und Übersicht, angeheftete Projekte und Schlagwörter zum Filtern. Das gilt nur für dich.")}
      footer={
        <>
          <Button variant="ghost" onClick={onClose}>
            {t("Abbrechen")}
          </Button>
          <Button variant="primary" loading={busy} disabled={tooManyTags} onClick={save}>
            {t("Speichern")}
          </Button>
        </>
      }
    >
      <div className="divide-y divide-white/[0.05] rounded-lg border border-white/[0.07]">
        {rows.map((row, index) => (
          <div key={row.project.id} className="flex flex-wrap items-center gap-x-3 gap-y-2 px-3 py-2.5">
            <div className="flex items-center gap-0.5">
              <Button variant="ghost" size="icon-sm" aria-label={t("{name} nach oben", { name: row.project.name })} disabled={index === 0} onClick={() => move(index, -1)}>
                <ArrowUp className="h-3.5 w-3.5" />
              </Button>
              <Button variant="ghost" size="icon-sm" aria-label={t("{name} nach unten", { name: row.project.name })} disabled={index === rows.length - 1} onClick={() => move(index, 1)}>
                <ArrowDown className="h-3.5 w-3.5" />
              </Button>
            </div>
            <span className="text-muted">{row.project.kind === "network" ? <Network className="h-4 w-4" /> : <ServerIcon className="h-4 w-4" />}</span>
            <p className="min-w-[120px] flex-1 truncate text-sm font-medium">{row.project.name}</p>
            <input
              className="field max-w-[240px] py-1.5 text-xs"
              value={row.tags}
              aria-label={t("Schlagwörter für {name}", { name: row.project.name })}
              placeholder={t("Schlagwörter, mit Komma")}
              maxLength={220}
              onChange={(event) => setRows((current) => current.map((entry, i) => (i === index ? { ...entry, tags: event.target.value } : entry)))}
            />
            <button
              type="button"
              aria-pressed={row.favorite}
              aria-label={row.favorite ? t("{name} vom Anheften lösen", { name: row.project.name }) : `${row.project.name} anheften`}
              onClick={() => setRows((current) => current.map((entry, i) => (i === index ? { ...entry, favorite: !entry.favorite } : entry)))}
              className={cx("grid h-8 w-8 place-items-center rounded-lg transition duration-100 hover:bg-white/[0.06] active:scale-90", row.favorite ? "text-accent" : "text-muted hover:text-ink")}
            >
              <Star className={cx("h-4 w-4", row.favorite && "fill-current")} />
            </button>
          </div>
        ))}
      </div>
      <p className="text-xs text-muted">{tooManyTags ? t("Höchstens 8 Schlagwörter pro Projekt, jedes bis 24 Zeichen.") : t("Angeheftete Projekte stehen immer oben. Schlagwörter erscheinen in der Übersicht als Filter.")}</p>
    </Dialog>
  );
}
