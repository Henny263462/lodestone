"use client";

import { Download, ExternalLink, Package, Search } from "lucide-react";
import { useEffect, useState, type ReactNode } from "react";
import { Badge, Button, Card, EmptyState, SearchInput, Tabs } from "@/components/ui/primitives";
import { Dialog, useToast } from "@/components/ui/feedback";
import { ErrorState, Loading } from "@/components/ui/async";
import { api, useApi } from "@/lib/api";
import { compact } from "@/lib/format";
import type { SourceHit, SourceId, SourceInfo, SourceVersion } from "@/lib/types";

/** Suche über alle Quellen eines Servers. Modrinth bringt seine eigene Ansicht mit (modrinth), die anderen Quellen
 *  (Hangar, Spigot, CurseForge) teilen sich diese hier. */
export function SourceBrowse({ serverId, noun, onInstalled, modrinth }: { serverId: string; noun: string; onInstalled: () => void; modrinth: ReactNode }) {
  const { data, error, mutate } = useApi<SourceInfo[]>(`/servers/${serverId}/sources`);
  const [source, setSource] = useState<SourceId>("modrinth");

  if (error) return <ErrorState error={error} retry={() => mutate()} />;
  if (!data) return <Loading />;

  const available = data.filter((entry) => entry.available);
  const missing = data.filter((entry) => !entry.available && entry.reason);
  const active = available.some((entry) => entry.id === source) ? source : (available[0]?.id ?? "modrinth");

  return (
    <div className="space-y-4">
      {available.length > 1 && (
        <div className="flex flex-wrap items-center gap-3">
          <span className="text-xs text-muted">Quelle:</span>
          <Tabs value={active} onChange={setSource} items={available.map((entry) => ({ id: entry.id, label: entry.name }))} />
        </div>
      )}
      <div key={active} className="animate-fade-in">
        {active === "modrinth" ? modrinth : <ExternalBrowse serverId={serverId} source={active} name={data.find((entry) => entry.id === active)?.name ?? active} noun={noun} onInstalled={onInstalled} />}
      </div>
      {missing.length > 0 && (
        <div className="space-y-1 text-xs text-muted">
          {missing.map((entry) => (
            <p key={entry.id}>
              <span className="text-ink/80">{entry.name}:</span> {entry.reason}
            </p>
          ))}
        </div>
      )}
    </div>
  );
}

function ExternalBrowse({ serverId, source, name, noun, onInstalled }: { serverId: string; source: SourceId; name: string; noun: string; onInstalled: () => void }) {
  const toast = useToast();
  const [query, setQuery] = useState("");
  const [debounced, setDebounced] = useState("");
  const [detail, setDetail] = useState<SourceHit | null>(null);
  const [installing, setInstalling] = useState<string | null>(null);

  useEffect(() => {
    const timer = setTimeout(() => setDebounced(query.trim()), 350);
    return () => clearTimeout(timer);
  }, [query]);

  const { data, error, isLoading, mutate } = useApi<{ hits: SourceHit[] }>(`/servers/${serverId}/sources/${source}/search?q=${encodeURIComponent(debounced)}`);

  async function install(project: string, versionId?: string) {
    setInstalling(project);
    try {
      const result = await api.post<{ installed: string[] }>(`/servers/${serverId}/sources/${source}/install`, { project, versionId: versionId ?? "" });
      toast.success(`${result.installed.join(", ")} installiert`);
      setDetail(null);
      onInstalled();
    } catch (failure) {
      toast.error(failure instanceof Error ? failure.message : "Die Installation ist fehlgeschlagen.");
    } finally {
      setInstalling(null);
    }
  }

  return (
    <div className="space-y-4">
      <SearchInput value={query} onChange={(event) => setQuery(event.target.value)} placeholder={`${noun} auf ${name} suchen – nur passende zu Software und Version`} />
      {error ? (
        <ErrorState error={error} retry={() => mutate()} />
      ) : isLoading && !data ? (
        <Loading label="Suche läuft" />
      ) : data && data.hits.length === 0 ? (
        <Card>
          <EmptyState icon={<Search className="h-6 w-6" />} title="Nichts gefunden" text={`${name} hat dazu nichts Passendes. Probiere einen anderen Suchbegriff.`} />
        </Card>
      ) : (
        <div className="grid gap-3 lg:grid-cols-2">
          {data?.hits.map((hit) => (
            <Card key={hit.id} className="flex items-start gap-3 p-4">
              {/* eslint-disable-next-line @next/next/no-img-element */}
              {hit.iconUrl ? <img src={hit.iconUrl} alt="" className="h-12 w-12 shrink-0 rounded-lg bg-raised object-cover" /> : <span className="grid h-12 w-12 shrink-0 place-items-center rounded-lg bg-raised text-muted"><Package className="h-5 w-5" /></span>}
              <div className="min-w-0 flex-1">
                <button className="truncate text-left text-sm font-semibold hover:underline" onClick={() => setDetail(hit)}>
                  {hit.title}
                </button>
                <p className="text-xs text-muted">
                  {hit.author ? `von ${hit.author} · ` : ""}
                  {compact(hit.downloads)} Downloads
                </p>
                <p className="mt-1.5 line-clamp-2 text-xs text-ink/70">{hit.description}</p>
                {!hit.installable && hit.note && <p className="mt-1.5 text-xs text-warn">{hit.note}</p>}
              </div>
              {hit.installable ? (
                <Button size="sm" variant="primary" loading={installing === hit.id} disabled={!!installing} onClick={() => install(hit.id)}>
                  <Download className="h-3.5 w-3.5" />
                  Installieren
                </Button>
              ) : (
                hit.link && (
                  <a className="inline-flex shrink-0 items-center gap-1 text-xs text-accent hover:underline" href={hit.link} target="_blank" rel="noreferrer">
                    <ExternalLink className="h-3 w-3" />
                    Seite
                  </a>
                )
              )}
            </Card>
          ))}
        </div>
      )}
      <SourceDetailDialog serverId={serverId} source={source} name={name} hit={detail} installing={!!installing} onClose={() => setDetail(null)} onInstall={install} />
    </div>
  );
}

function SourceDetailDialog({
  serverId,
  source,
  name,
  hit,
  installing,
  onClose,
  onInstall,
}: {
  serverId: string;
  source: SourceId;
  name: string;
  hit: SourceHit | null;
  installing: boolean;
  onClose: () => void;
  onInstall: (project: string, versionId?: string) => void;
}) {
  const { data, error, mutate } = useApi<{ project: SourceHit; versions: SourceVersion[] }>(hit ? `/servers/${serverId}/sources/${source}/project/${encodeURIComponent(hit.id)}` : null);
  return (
    <Dialog open={!!hit} onClose={onClose} title={data?.project.title ?? hit?.title ?? "Wird geladen"} description={data?.project.description ?? hit?.description} wide>
      {data?.project.link && (
        <a className="inline-flex items-center gap-1 text-xs text-accent hover:underline" href={data.project.link} target="_blank" rel="noreferrer">
          <ExternalLink className="h-3 w-3" />
          Auf {name} ansehen
        </a>
      )}
      {error ? (
        <ErrorState error={error} retry={() => mutate()} />
      ) : !data ? (
        <Loading />
      ) : data.versions.length === 0 ? (
        <p className="text-sm text-muted">Es gibt keine Version, die zu dieser Software und Spielversion passt.</p>
      ) : (
        <div className="max-h-[50vh] divide-y divide-white/[0.05] overflow-y-auto rounded-lg border border-white/[0.07]">
          {data.versions.slice(0, 40).map((version) => (
            <div key={version.id} className="flex items-center gap-3 px-3 py-2.5">
              <div className="min-w-0 flex-1">
                <p className="truncate text-sm">{version.versionNumber}</p>
                <p className="truncate text-xs text-muted">
                  {version.name && version.name !== version.versionNumber ? `${version.name} · ` : ""}
                  {version.published ? new Date(version.published).toLocaleDateString("de-DE") : ""}
                  {version.gameVersions.length > 0 && ` · MC ${version.gameVersions.slice(0, 4).join(", ")}${version.gameVersions.length > 4 ? " …" : ""}`}
                </p>
                {!version.downloadable && version.reason && <p className="mt-0.5 text-xs text-warn">{version.reason}</p>}
              </div>
              <Badge tone={version.type === "release" ? "good" : "warn"}>{version.type}</Badge>
              {version.downloadable ? (
                <Button size="sm" disabled={installing} onClick={() => onInstall(data.project.id, version.id)}>
                  Installieren
                </Button>
              ) : (
                <Badge>Nicht ladbar</Badge>
              )}
            </div>
          ))}
        </div>
      )}
    </Dialog>
  );
}
