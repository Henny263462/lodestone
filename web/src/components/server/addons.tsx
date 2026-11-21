"use client";

import { ArrowUpCircle, CheckCircle2, Download, ExternalLink, Package, Plug, Power, RefreshCw, Repeat, Search, Trash2, Upload } from "lucide-react";
import { useEffect, useRef, useState, type ReactNode } from "react";
import { Badge, Button, Card, CardHeader, EmptyState, SearchInput, Tabs } from "@/components/ui/primitives";
import { ConfirmDialog, Dialog, useToast } from "@/components/ui/feedback";
import { ErrorState, Loading } from "@/components/ui/async";
import { api, upload, useApi } from "@/lib/api";
import { bytes, compact } from "@/lib/format";
import type { Addon, AddonDetail, ModrinthHit, UpdateEntry } from "@/lib/types";
import { SourceBrowse } from "@/components/server/sources";
import { locale, t, tp } from "@/lib/i18n";

type View = "installed" | "browse" | "blocks";

const sourceNames = { hangar: "Hangar", spigot: "Spigot", curseforge: "CurseForge" };

// canWrite erlaubt Modrinth (Installieren, Aktualisieren, Version wechseln); Hochladen, Abschalten und Löschen sind
// Dateiaktionen und brauchen zusätzlich canFiles.
export function AddonsTab({ serverId, canWrite, canFiles, noun, running, blocks }: { serverId: string; canWrite: boolean; canFiles: boolean; noun: "Plugins" | "Mods"; running: boolean; blocks?: ReactNode }) {
  const [view, setView] = useState<View>("installed");
  return (
    <div className="space-y-4">
      <Tabs
        value={view}
        onChange={setView}
        items={[
          { id: "installed", label: t("Installierte {noun}", { noun }) },
          ...(canWrite ? [{ id: "browse" as const, label: t("Durchsuchen") }] : []),
          ...(blocks ? [{ id: "blocks" as const, label: t("Bausteine") }] : []),
        ]}
      />
      {view === "installed" && <Installed serverId={serverId} canWrite={canWrite} canFiles={canFiles} noun={noun} running={running} />}
      {view === "browse" && <SourceBrowse serverId={serverId} noun={noun} onInstalled={() => setView("installed")} modrinth={<Browse serverId={serverId} noun={noun} onInstalled={() => setView("installed")} />} />}
      {view === "blocks" && blocks}
    </div>
  );
}

function Installed({ serverId, canWrite, canFiles, noun, running }: { serverId: string; canWrite: boolean; canFiles: boolean; noun: string; running: boolean }) {
  const toast = useToast();
  const { data, error, mutate } = useApi<Addon[]>(`/servers/${serverId}/addons`);
  const [updates, setUpdates] = useState<Record<string, UpdateEntry> | null>(null);
  const [checking, setChecking] = useState(false);
  const [remove, setRemove] = useState<Addon | null>(null);
  const [switching, setSwitching] = useState<AddonDetail | null>(null);
  const [progress, setProgress] = useState<number | null>(null);
  const picker = useRef<HTMLInputElement>(null);
  const { data: detailList, mutate: reloadDetails } = useApi<AddonDetail[]>(`/servers/${serverId}/addons/details`);
  const details = new Map((detailList ?? []).map((entry) => [`${entry.dir}/${entry.file}`, entry]));

  if (error) return <ErrorState error={error} retry={() => mutate()} />;
  if (!data) return <Loading />;

  const path = (addon: Addon) => `${addon.dir}/${addon.file}`;
  const pending = updates ? Object.values(updates).filter((entry) => entry.updateAvailable) : [];

  async function check() {
    setChecking(true);
    try {
      const list = await api.post<UpdateEntry[]>(`/servers/${serverId}/addons/check`);
      setUpdates(Object.fromEntries(list.map((entry) => [entry.file, entry])));
      const count = list.filter((entry) => entry.updateAvailable).length;
      toast.success(count ? tp("updatesAvailable", count) : t("Alles ist aktuell"));
    } catch (failure) {
      toast.error(failure instanceof Error ? failure.message : t("Prüfung fehlgeschlagen"));
    } finally {
      setChecking(false);
    }
  }

  async function updateAll(files: string[]) {
    const results = await api.post<{ file: string; updated: boolean; to?: string; error?: string }[]>(`/servers/${serverId}/addons/update`, { files });
    const ok = results.filter((entry) => entry.updated).length;
    const bad = results.filter((entry) => !entry.updated && entry.error && entry.error !== "schon aktuell");
    if (ok) toast.success(`${ok} aktualisiert${running ? t(" – Neustart nötig") : ""}`);
    if (bad.length) toast.error(bad.map((entry) => `${entry.file}: ${entry.error}`).join("\n"));
    setUpdates(null);
    mutate();
    reloadDetails();
  }

  async function toggle(addon: Addon) {
    const target = addon.enabled ? `${path(addon)}.disabled` : path(addon).replace(/\.disabled$/, "");
    await toast.run(() => api.post(`/servers/${serverId}/files/rename`, { from: path(addon), to: target }), addon.enabled ? t("Deaktiviert") : t("Aktiviert"));
    mutate();
  }

  return (
    <Card>
      <CardHeader
        title={t("Installierte {noun}", { noun })}
        description={running ? t("Änderungen werden nach einem Neustart des Servers wirksam.") : undefined}
        action={
          (canWrite || canFiles) && (
            <div className="flex gap-2">
              {canWrite && (
                <Button size="sm" onClick={check} loading={checking}>
                  <RefreshCw className="h-3.5 w-3.5" />
                  {t("Updates prüfen")}
                </Button>
              )}
              {canWrite && pending.length > 0 && (
                <Button size="sm" variant="primary" onClick={() => toast.run(() => updateAll(pending.map((entry) => entry.file)))}>
                  <ArrowUpCircle className="h-3.5 w-3.5" />
                  {t("Alle aktualisieren (")}{pending.length})
                </Button>
              )}
              {canFiles && (
                <Button size="sm" onClick={() => picker.current?.click()}>
                  <Upload className="h-3.5 w-3.5" />
                  {t("Hochladen")}
                </Button>
              )}
              <input
                ref={picker}
                type="file"
                accept=".jar"
                multiple
                className="hidden"
                onChange={async (event) => {
                  const files = Array.from(event.target.files ?? []);
                  event.target.value = "";
                  if (files.length === 0) return;
                  const folder = data[0]?.dir ?? (noun === "Mods" ? "mods" : "plugins");
                  setProgress(0);
                  await toast.run(() => upload(`/servers/${serverId}/files/upload?path=${folder}&overwrite=1`, files, setProgress), t("Hochgeladen"));
                  setProgress(null);
                  mutate();
                }}
              />
            </div>
          )
        }
      />
      {progress !== null && (
        <div className="h-1 bg-white/[0.06]">
          <div className="h-full bg-accent" style={{ width: `${Math.round(progress * 100)}%` }} />
        </div>
      )}
      {data.length === 0 ? (
        <EmptyState icon={<Package className="h-6 w-6" />} title={t("Keine {noun} installiert", { noun })} text={canWrite ? t("Durchsuche Modrinth und die anderen Quellen oder lade eine .jar-Datei hoch.") : undefined} />
      ) : (
        <div className="divide-y divide-white/[0.05]">
          {data.map((addon) => {
            const update = updates?.[addon.file];
            const detail = details.get(path(addon));
            return (
              <div key={`${addon.dir}/${addon.file}`} className="flex flex-wrap items-center gap-x-4 gap-y-2 px-5 py-3">
                {detail?.iconUrl ? (
                  // eslint-disable-next-line @next/next/no-img-element
                  <img src={detail.iconUrl} alt="" className="h-9 w-9 shrink-0 rounded-lg bg-raised object-cover" />
                ) : (
                  <Plug className={`h-4 w-4 shrink-0 ${addon.enabled ? "text-muted" : "text-muted/40"}`} />
                )}
                <div className="min-w-[180px] flex-1">
                  <p className={`truncate text-sm font-medium ${addon.enabled ? "" : "text-muted line-through"}`}>{detail?.title || addon.name || addon.file}</p>
                  <p className="truncate text-xs text-muted">
                    {detail?.versionLink ? (
                      <a className="text-accent hover:underline" href={detail.versionLink} target="_blank" rel="noreferrer" title={detail.versionName}>
                        {detail.versionNumber}
                      </a>
                    ) : (
                      addon.version
                    )}
                    {(detail?.versionNumber || addon.version) && " · "}
                    {addon.file} · {bytes(addon.size)}
                  </p>
                  {detail?.known && (
                    <p className="mt-0.5 flex flex-wrap items-center gap-x-3 text-[11px] text-muted">
                      {detail.link && (
                        <a className="inline-flex items-center gap-1 hover:text-ink hover:underline" href={detail.link} target="_blank" rel="noreferrer">
                          <ExternalLink className="h-3 w-3" />
                          {t("{source}-Seite", { source: detail.source ? sourceNames[detail.source] : "Modrinth" })}
                        </a>
                      )}
                      {detail.sourceUrl && (
                        <a className="hover:text-ink hover:underline" href={detail.sourceUrl} target="_blank" rel="noreferrer">
                          {t("Quellcode")}
                        </a>
                      )}
                      {detail.issuesUrl && (
                        <a className="hover:text-ink hover:underline" href={detail.issuesUrl} target="_blank" rel="noreferrer">
                          {t("Fehler melden")}
                        </a>
                      )}
                      {detail.license && <span>{detail.license}</span>}
                      {detail.versionType && detail.versionType !== "release" && <Badge tone="warn">{detail.versionType}</Badge>}
                    </p>
                  )}
                </div>
                {!addon.enabled && <Badge>{t("Deaktiviert")}</Badge>}
                {update?.updateAvailable && (
                  <Badge tone="accent">
                    <ArrowUpCircle className="h-3 w-3" />
                    {update.latest}
                  </Badge>
                )}
                {update && !update.updateAvailable && update.known && (
                  <Badge tone="good">
                    <CheckCircle2 className="h-3 w-3" />
                    {t("Aktuell")}
                  </Badge>
                )}
                {update && !update.known && <span title={t("Keiner Quelle zugeordnet – lade die Datei über „Durchsuchen“, damit Lodestone Updates findet")}><Badge>{t("Unbekannt")}</Badge></span>}
                {(canWrite || canFiles) && (
                  <div className="flex items-center gap-1">
                    {canWrite && update?.updateAvailable && (
                      <Button size="sm" onClick={() => toast.run(() => updateAll([addon.file]))}>
                        {t("Aktualisieren")}
                      </Button>
                    )}
                    {canWrite && detail?.known && detail.projectId && !detail.source && (
                      <Button variant="ghost" size="icon-sm" onClick={() => setSwitching(detail)} title={t("Version wechseln")} aria-label={t("Version wechseln")}>
                        <Repeat className="h-3.5 w-3.5" />
                      </Button>
                    )}
                    {canFiles && (
                      <Button variant="ghost" size="icon-sm" onClick={() => toggle(addon)} title={addon.enabled ? t("Deaktivieren") : t("Aktivieren")} aria-label={t("Umschalten")}>
                        <Power className="h-3.5 w-3.5" />
                      </Button>
                    )}
                    {canFiles && (
                      <Button variant="ghost" size="icon-sm" onClick={() => setRemove(addon)} title={t("Löschen")} aria-label={t("Löschen")}>
                        <Trash2 className="h-3.5 w-3.5" />
                      </Button>
                    )}
                  </div>
                )}
              </div>
            );
          })}
        </div>
      )}
      <SwitchDialog
        serverId={serverId}
        detail={switching}
        onClose={() => setSwitching(null)}
        onDone={() => {
          setSwitching(null);
          setUpdates(null);
          mutate();
          reloadDetails();
        }}
        running={running}
      />
      <ConfirmDialog
        open={!!remove}
        onClose={() => setRemove(null)}
        title={t("„{value}“ löschen?", { value: remove?.name || remove?.file })}
        text={t("Die Datei wird vom Server gelöscht. Die Konfiguration des Plugins bleibt erhalten.")}
        confirm={t("Löschen")}
        danger
        onConfirm={async () => {
          if (!remove) return;
          await toast.run(() => api.delete(`/servers/${serverId}/files?path=${encodeURIComponent(path(remove))}`), t("Gelöscht"));
          mutate();
        }}
      />
    </Card>
  );
}

// Tauscht eine installierte Datei gegen eine andere Version desselben Projekts (auch ältere).
function SwitchDialog({ serverId, detail, onClose, onDone, running }: { serverId: string; detail: AddonDetail | null; onClose: () => void; onDone: () => void; running: boolean }) {
  const toast = useToast();
  const [busy, setBusy] = useState<string | null>(null);
  const { data, error } = useApi<ProjectDetail>(detail?.projectId ? `/servers/${serverId}/modrinth/project/${encodeURIComponent(detail.projectId)}` : null);
  return (
    <Dialog open={!!detail} onClose={onClose} title={t("{value}: Version wechseln", { value: detail?.title ?? "" })} description={t("Installiert: {value}{value2}", { value: detail?.versionNumber ?? "?", value2: running ? t(" · Neustart nötig") : "" })} wide>
      {error ? (
        <p className="text-sm text-bad">{error.message}</p>
      ) : !data ? (
        <Loading />
      ) : (
        <div className="max-h-[50vh] divide-y divide-white/[0.05] overflow-y-auto rounded-lg border border-white/[0.07]">
          {data.versions.slice(0, 60).map((version) => (
            <div key={version.id} className="flex items-center gap-3 px-3 py-2.5">
              <div className="min-w-0 flex-1">
                <p className="truncate text-sm">{version.versionNumber}</p>
                <p className="truncate text-xs text-muted">
                  {version.name} · {new Date(version.published).toLocaleDateString(locale())}
                </p>
              </div>
              <Badge tone={version.type === "release" ? "good" : "warn"}>{version.type}</Badge>
              {version.id === detail?.versionId ? (
                <Badge tone="accent">{t("Installiert")}</Badge>
              ) : (
                <Button
                  size="sm"
                  loading={busy === version.id}
                  disabled={!!busy}
                  onClick={async () => {
                    setBusy(version.id);
                    const ok = await toast.run(() => api.post(`/servers/${serverId}/addons/switch`, { file: detail?.file, versionId: version.id }), t("Auf {versionNumber} gewechselt", { versionNumber: version.versionNumber }));
                    setBusy(null);
                    if (ok) onDone();
                  }}
                >
                  {t("Wechseln")}
                </Button>
              )}
            </div>
          ))}
        </div>
      )}
    </Dialog>
  );
}

// ---------------------------------------------------------------- Modrinth

interface ProjectDetail {
  project: { id: string; title: string; description: string; icon_url: string; downloads: number; slug: string };
  versions: { id: string; name: string; versionNumber: string; type: string; published: string; gameVersions: string[] }[];
}

function Browse({ serverId, noun, onInstalled }: { serverId: string; noun: string; onInstalled: () => void }) {
  const toast = useToast();
  const [query, setQuery] = useState("");
  const [debounced, setDebounced] = useState("");
  const [detail, setDetail] = useState<string | null>(null);
  const [installing, setInstalling] = useState<string | null>(null);

  useEffect(() => {
    const timer = setTimeout(() => setDebounced(query.trim()), 350);
    return () => clearTimeout(timer);
  }, [query]);

  const { data, error, isLoading } = useApi<{ hits: ModrinthHit[]; total_hits: number }>(`/servers/${serverId}/modrinth/search?q=${encodeURIComponent(debounced)}`);

  async function install(project: string, versionId?: string) {
    setInstalling(project);
    try {
      const result = await api.post<{ installed: string[] }>(`/servers/${serverId}/modrinth/install`, { project, versionId: versionId ?? "" });
      toast.success(`${result.installed.join(", ")} installiert`);
      setDetail(null);
      onInstalled();
    } catch (failure) {
      toast.error(failure instanceof Error ? failure.message : t("Installation fehlgeschlagen"));
    } finally {
      setInstalling(null);
    }
  }

  return (
    <div className="space-y-4">
      <SearchInput value={query} onChange={(event) => setQuery(event.target.value)} placeholder={t("{noun} auf Modrinth suchen – nur passende zu Software und Version", { noun })} />
      {error ? (
        <ErrorState error={error} />
      ) : isLoading && !data ? (
        <Loading label={t("Suche läuft")} />
      ) : data && data.hits.length === 0 ? (
        <Card>
          <EmptyState icon={<Search className="h-6 w-6" />} title={t("Nichts gefunden")} text={t("Probiere einen anderen Suchbegriff.")} />
        </Card>
      ) : (
        <div className="grid gap-3 lg:grid-cols-2">
          {data?.hits.map((hit) => (
            <Card key={hit.project_id} className="flex items-start gap-3 p-4">
              {/* eslint-disable-next-line @next/next/no-img-element */}
              {hit.icon_url ? <img src={hit.icon_url} alt="" className="h-12 w-12 shrink-0 rounded-lg bg-raised object-cover" /> : <span className="grid h-12 w-12 shrink-0 place-items-center rounded-lg bg-raised text-muted"><Package className="h-5 w-5" /></span>}
              <div className="min-w-0 flex-1">
                <button className="truncate text-left text-sm font-semibold hover:underline" onClick={() => setDetail(hit.project_id)}>
                  {hit.title}
                </button>
                <p className="text-xs text-muted">
                  {t("von")} {hit.author} · {compact(hit.downloads)} {t("Downloads")}
                </p>
                <p className="mt-1.5 line-clamp-2 text-xs text-ink/70">{hit.description}</p>
              </div>
              <Button size="sm" variant="primary" loading={installing === hit.project_id} onClick={() => install(hit.project_id)}>
                <Download className="h-3.5 w-3.5" />
                {t("Installieren")}
              </Button>
            </Card>
          ))}
        </div>
      )}
      <DetailDialog serverId={serverId} project={detail} installing={!!installing} onClose={() => setDetail(null)} onInstall={install} />
    </div>
  );
}

function DetailDialog({ serverId, project, installing, onClose, onInstall }: { serverId: string; project: string | null; installing: boolean; onClose: () => void; onInstall: (project: string, versionId?: string) => void }) {
  const { data, error } = useApi<ProjectDetail>(project ? `/servers/${serverId}/modrinth/project/${encodeURIComponent(project)}` : null);
  return (
    <Dialog open={!!project} onClose={onClose} title={data?.project.title ?? t("Wird geladen")} description={data?.project.description} wide>
      {error ? (
        <p className="text-sm text-bad">{error.message}</p>
      ) : !data ? (
        <Loading />
      ) : data.versions.length === 0 ? (
        <p className="text-sm text-muted">{t("Es gibt keine Version, die zu dieser Software und Spielversion passt.")}</p>
      ) : (
        <div className="max-h-[50vh] divide-y divide-white/[0.05] overflow-y-auto rounded-lg border border-white/[0.07]">
          {data.versions.slice(0, 40).map((version) => (
            <div key={version.id} className="flex items-center gap-3 px-3 py-2.5">
              <div className="min-w-0 flex-1">
                <p className="truncate text-sm">{version.versionNumber}</p>
                <p className="truncate text-xs text-muted">
                  {version.name} · {new Date(version.published).toLocaleDateString(locale())}
                </p>
              </div>
              <Badge tone={version.type === "release" ? "good" : "warn"}>{version.type}</Badge>
              <Button size="sm" disabled={installing} onClick={() => onInstall(data.project.id, version.id)}>
                {t("Installieren")}
              </Button>
            </div>
          ))}
        </div>
      )}
    </Dialog>
  );
}
