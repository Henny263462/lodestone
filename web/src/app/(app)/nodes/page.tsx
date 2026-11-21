"use client";

import Link from "next/link";
import { usePathname, useSearchParams } from "next/navigation";
import {
  Activity,
  ArrowUpCircle,
  ArrowUpRight,
  Cable,
  ChevronLeft,
  ChevronRight,
  Cpu,
  HardDrive,
  Layers,
  Pencil,
  Play,
  Plus,
  RefreshCw,
  RotateCw,
  Server as ServerIcon,
  Square,
  Trash2,
} from "lucide-react";
import { Suspense, useEffect, useState } from "react";
import { Badge, Button, Card, Dot, EmptyState, Meter, PageHeader, StatCard } from "@/components/ui/primitives";
import { ConfirmDialog, Dialog, useToast } from "@/components/ui/feedback";
import { ErrorState, Loading } from "@/components/ui/async";
import { useAppNavigation } from "@/components/layout/navigation";
import { useUser } from "@/components/layout/shell";
import { PlayitClaimDialog } from "@/components/playit-claim";
import { StateBadge } from "@/components/status";
import { softwareName } from "@/components/server-list";
import { NodeStorageCard } from "@/components/node-storage";
import { AddNodeDialog, NodeLinkCard } from "@/components/node-link";
import { api, useApi } from "@/lib/api";
import { bytes, megabytes } from "@/lib/format";
import { usePoll } from "@/lib/live";
import { can, type NodeDetail, type NodeInfo, type NodeLink, type PlayitStatus, type Project } from "@/lib/types";
import { formatNumber, t, tp } from "@/lib/i18n";

export default function NodesPage() {
  return (
    <Suspense fallback={<Loading label={t("Nodes werden geladen")} />}>
      <NodesRouter />
    </Suspense>
  );
}

function parseNodeRoute(pathname: string, searchParams: URLSearchParams): { id: string | null } {
  const segments = pathname.split("/").filter(Boolean);
  let id: string | null = null;
  if (segments[0] === "nodes" && segments[1]) {
    id = segments[1];
  }
  if (!id) {
    id = searchParams.get("id");
  }
  return { id };
}

function NodesRouter() {
  const pathname = usePathname() ?? "";
  const searchParams = useSearchParams();
  const { id } = parseNodeRoute(pathname, searchParams);

  if (id) {
    return <NodeDetailView nodeId={id} />;
  }

  return <NodesListView />;
}

/** Version des Daemons für Menschen: „–“, „dev“ oder „v1.2.3“. */
function versionLabel(version?: string): string {
  return !version ? "–" : version === "dev" ? "dev" : `v${version}`;
}

function pingTone(ping?: number): "neutral" | "good" | "warn" | "bad" {
  return ping === undefined ? "neutral" : ping < 60 ? "good" : ping < 150 ? "warn" : "bad";
}

/** Ein selbst verbundener Node, der gerade nicht steht: Dann erklärt die Verbindungskarte, was zu tun ist, nicht der Rohfehler. */
function waitingForLink(node: NodeInfo, links: NodeLink[] | undefined): boolean {
  return !!node.linked && !links?.find((link) => link.nodeId === node.id)?.connected;
}

// ================================================================
// Liste: alle Nodes
// ================================================================

function NodesListView() {
  const user = useUser();
  // Ausfälle und Rückkehr meldet der Live-Kanal; die Intervalle sind der Rückfall
  const poll = usePoll(60000, 8000);
  const { data, error, mutate, isLoading } = useApi<NodeInfo[]>("/nodes", { refreshInterval: poll });
  const links = useApi<NodeLink[]>(user.isAdmin ? "/nodes/link" : null, { refreshInterval: poll });
  const params = useSearchParams();
  const [adding, setAdding] = useState(false);
  const [editing, setEditing] = useState<NodeInfo | "new" | null>(null);
  const [removing, setRemoving] = useState<NodeInfo | null>(null);
  const [updatingNode, setUpdatingNode] = useState<NodeInfo | null>(null);
  const toast = useToast();

  // Strg+K → „Node hinzufügen“ landet hier mit ?add=1
  useEffect(() => {
    if (params.get("add") !== "1") return;
    if (user.isAdmin) setAdding(true);
    const url = new URL(window.location.href);
    url.searchParams.delete("add");
    window.history.replaceState({ ...window.history.state }, "", url);
  }, [params, user.isAdmin]);

  if (error) return <ErrorState error={error} retry={() => mutate()} />;
  if (!data) return <Loading label={t("Nodes werden geladen")} />;

  const onlineCount = data.filter((n) => n.online).length;
  const totalServers = data.reduce((sum, n) => sum + (n.serverCount ?? 0), 0);
  const totalMemory = data.reduce((sum, n) => sum + (n.memoryTotal ?? n.system?.memoryBytes ?? 0), 0);
  const totalUsedMemory = data.reduce((sum, n) => {
    const total = n.memoryTotal ?? n.system?.memoryBytes ?? 0;
    const avail = n.memoryAvailable ?? 0;
    return sum + (n.memoryUsed ?? Math.max(0, total - avail));
  }, 0);

  return (
    <>
      <PageHeader
        crumbs={[{ label: t("Übersicht"), href: "/" }, { label: t("Nodes") }]}
        title={t("Nodes")}
        description={t("Maschinen mit Docker, auf denen die Server laufen. Auf jeder läuft der Lodestone-Node-Daemon.")}
        action={
          <>
            <Button variant="secondary" onClick={() => mutate()} loading={isLoading}>
              <RefreshCw className="h-4 w-4" />
              {t("Aktualisieren")}
            </Button>
            {user.isAdmin && (
              <Button variant="primary" onClick={() => setAdding(true)}>
                <Plus className="h-4 w-4" />
                {t("Node hinzufügen")}
              </Button>
            )}
          </>
        }
      />

      <div className="grid gap-4 sm:grid-cols-2 xl:grid-cols-4">
        <StatCard
          label={t("Nodes online")}
          value={`${onlineCount}/${data.length}`}
          icon={<Cpu className="h-4 w-4" />}
          tone={onlineCount === data.length && data.length > 0 ? "green" : onlineCount > 0 ? "amber" : "neutral"}
          hint={data.length === 0 ? t("Noch kein Node vorhanden") : tp("machinesReady", onlineCount)}
        />
        <StatCard label={t("Verwaltete Server")} value={totalServers} icon={<ServerIcon className="h-4 w-4" />} tone="blue" hint={t("Über alle Nodes verteilt")} />
        <StatCard
          label={t("Arbeitsspeicher (RAM)")}
          value={bytes(totalUsedMemory)}
          icon={<HardDrive className="h-4 w-4" />}
          tone="violet"
          hint={t("von {total} belegt", { total: bytes(totalMemory) })}
        />
        <StatCard
          label={t("playit.gg-Tunnel")}
          value={user.isAdmin ? t("Verwalten") : t("Aktiv")}
          icon={<Cable className="h-4 w-4" />}
          tone="neutral"
          hint={
            user.isAdmin ? (
              <Link href="/admin/playit/" className="text-accent hover:underline">
                {t("Zur playit-Seite →")}
              </Link>
            ) : (
              t("Ports ohne Portfreigabe")
            )
          }
        />
      </div>

      <div className="mt-8">
        {data.length === 0 ? (
          <Card>
            <EmptyState
              icon={<Cpu className="h-6 w-6" />}
              title={t("Noch kein Node eingerichtet")}
              text={user.isAdmin ? t("Füge einen Node hinzu: Du bekommst einen Startbefehl, mit dem sich der Node selbst zum Panel verbindet.") : t("Ein Administrator muss einen Node einrichten.")}
              action={
                user.isAdmin && (
                  <Button variant="primary" onClick={() => setAdding(true)}>
                    <Plus className="h-4 w-4" />
                    {t("Node hinzufügen")}
                  </Button>
                )
              }
            />
          </Card>
        ) : (
          <div className="grid gap-4 lg:grid-cols-2">
            {data.map((node) => {
              const totalMem = node.memoryTotal ?? node.system?.memoryBytes ?? 0;
              const availMem = node.memoryAvailable ?? 0;
              const usedMem = node.memoryUsed ?? Math.max(0, totalMem - availMem);
              const committedMem = node.memoryCommitted ?? 0;
              const usedPercent = totalMem > 0 ? (usedMem / totalMem) * 100 : 0;
              const waiting = user.isAdmin && waitingForLink(node, links.data);

              return (
                <Card key={node.id} className="flex min-w-0 flex-col justify-between p-5">
                  <div>
                    <div className="flex flex-wrap items-start justify-between gap-3">
                      <div className="flex min-w-0 items-center gap-3">
                        <span className="grid h-9 w-9 shrink-0 place-items-center rounded-md border border-white/[0.08] bg-black/20 text-muted shadow-sunken">
                          <Cpu className="h-4 w-4" />
                        </span>
                        <div className="min-w-0">
                          <p className="truncate font-display text-[15px] tracking-wide">{node.name}</p>
                          <p className="truncate text-xs text-muted">{node.publicHost || t("Keine öffentliche Adresse")}</p>
                        </div>
                      </div>
                      <div className="flex items-center gap-2">
                        {node.online && node.pingMs !== undefined && (
                          <Badge tone={pingTone(node.pingMs)}>
                            <Activity className="h-3 w-3" />
                            {node.pingMs} ms
                          </Badge>
                        )}
                        <Badge tone={node.online ? "good" : "bad"}>
                          <Dot tone={node.online ? "good" : "bad"} />
                          {node.online ? "Online" : "Offline"}
                        </Badge>
                      </div>
                    </div>

                    {node.online && node.system ? (
                      <>
                        <dl className="mt-4 grid grid-cols-2 gap-x-4 gap-y-2 text-xs">
                          <dt className="text-muted">{t("Version")}</dt>
                          <dd className="flex flex-wrap items-center gap-1.5">
                            <span>{versionLabel(node.version)}</span>
                            {node.updateAvailable && <Badge tone="warn">{t("Update verfügbar")}</Badge>}
                          </dd>
                          <dt className="text-muted">Docker</dt>
                          <dd className="truncate">{node.system.dockerVersion}</dd>
                          <dt className="text-muted">{t("System")}</dt>
                          <dd className="truncate">
                            {node.system.os} · {node.system.architecture}
                          </dd>
                          <dt className="text-muted">{t("Kerne / RAM")}</dt>
                          <dd>
                            {tp("cores", node.system.cpus)} · {bytes(totalMem)}
                          </dd>
                          <dt className="text-muted">{t("Server auf diesem Node")}</dt>
                          <dd className="font-medium text-ink">{node.serverCount}</dd>
                        </dl>

                        <div className="mt-4 space-y-2.5 border-t border-white/[0.06] pt-3">
                          <div>
                            <div className="mb-1 flex justify-between gap-2 text-[11px] text-muted">
                              <span>{t("Arbeitsspeicher (belegt)")}</span>
                              <span className="tabular-nums text-ink">
                                {t("{used} von {total} ({percent} %)", { used: bytes(usedMem), total: bytes(totalMem), percent: formatNumber(usedPercent, 0) })}
                              </span>
                            </div>
                            <Meter value={usedPercent} max={100} tone={usedPercent > 90 ? "bad" : usedPercent > 75 ? "warn" : "good"} />
                          </div>
                          {committedMem > 0 && (
                            <div className="flex justify-between gap-2 text-[11px] text-muted">
                              <span>{t("Zugesagt durch Server-Limits:")}</span>
                              <span className="tabular-nums text-ink">{bytes(committedMem)}</span>
                            </div>
                          )}
                        </div>
                      </>
                    ) : waiting ? (
                      <p className="mt-4 rounded-md border border-warn/25 bg-warn/10 px-3 py-2 text-xs text-warn">
                        {t("Der Node verbindet sich selbst zum Panel, steht aber gerade nicht. Startbefehl und Zustand der Verbindung findest du in den Details.")}
                      </p>
                    ) : (
                      node.error && <p className="mt-4 rounded-md border border-bad/25 bg-bad/10 px-3 py-2 text-xs text-bad">{node.error}</p>
                    )}

                    {user.isAdmin && node.online && node.updateAvailable && (
                      <div className="mt-3 flex flex-wrap items-center justify-between gap-2 rounded-lg border border-warn/25 bg-warn/10 px-3 py-2 text-xs">
                        <span className="text-warn">{t("Ein Update ist verfügbar.")}</span>
                        <Button variant="secondary" size="sm" onClick={() => setUpdatingNode(node)}>
                          <ArrowUpCircle className="h-3.5 w-3.5" />
                          {t("Aktualisieren")}
                        </Button>
                      </div>
                    )}
                  </div>

                  <div className="mt-5 flex items-center justify-between gap-2 border-t border-white/[0.06] pt-3">
                    <div className="min-w-0">
                      <Link href={`/nodes/${node.id}/`} className="inline-flex items-center gap-1.5 text-xs font-medium text-accent transition hover:underline">
                        {t("Details & Server ansehen")}
                        <ChevronRight className="h-3.5 w-3.5" />
                      </Link>
                      {user.isAdmin && <p className="mt-0.5 truncate font-mono text-[11px] text-muted">{node.linked ? t("verbindet sich selbst zum Panel") : node.url}</p>}
                    </div>

                    {user.isAdmin && (
                      <div className="flex shrink-0 gap-1">
                        <Button variant="ghost" size="icon-sm" onClick={() => setEditing(node)} aria-label={t("Bearbeiten")}>
                          <Pencil className="h-3.5 w-3.5" />
                        </Button>
                        <Button variant="ghost" size="icon-sm" onClick={() => setRemoving(node)} aria-label={t("Entfernen")}>
                          <Trash2 className="h-3.5 w-3.5" />
                        </Button>
                      </div>
                    )}
                  </div>
                </Card>
              );
            })}
          </div>
        )}
      </div>

      <NodeDialog node={editing} onClose={() => setEditing(null)} onDone={() => mutate()} />
      <AddNodeDialog
        open={adding}
        onClose={() => setAdding(false)}
        onDirect={() => {
          setAdding(false);
          setEditing("new");
        }}
        onDone={() => {
          mutate();
          links.mutate();
        }}
      />
      <ConfirmDialog
        open={!!updatingNode}
        onClose={() => setUpdatingNode(null)}
        title={t("Node „{name}“ aktualisieren?", { name: updatingNode?.name })}
        text={t("Der Node-Daemon wird auf das Image der neuen Version gehoben und neu gestartet. Laufende Minecraft-Server auf diesem Node laufen ungestört weiter.")}
        confirm={t("Aktualisieren")}
        onConfirm={async () => {
          if (!updatingNode) return;
          await toast.run(() => api.post(`/nodes/${updatingNode.id}/update`), t("Node-Update gestartet"));
          mutate();
        }}
      />
      <ConfirmDialog
        open={!!removing}
        onClose={() => setRemoving(null)}
        title={t("„{name}“ entfernen?", { name: removing?.name })}
        text={t("Der Node wird aus dem Panel entfernt. Das geht nur, wenn kein Server mehr auf ihm liegt. Der Daemon selbst wird nicht angehalten.")}
        confirm={t("Entfernen")}
        danger
        onConfirm={async () => {
          if (!removing) return;
          await toast.run(() => api.delete(`/nodes/${removing.id}`), t("Node entfernt"));
          mutate();
        }}
      />
    </>
  );
}

// ================================================================
// Detailseite eines Nodes (/nodes/<id>/)
// ================================================================

function NodeDetailView({ nodeId }: { nodeId: string }) {
  const user = useUser();
  const toast = useToast();
  const navigation = useAppNavigation();
  const poll = usePoll(30000, 5000);
  const { data: node, error, mutate, isLoading } = useApi<NodeDetail>(`/nodes/${nodeId}`, { refreshInterval: poll });
  const links = useApi<NodeLink[]>(user.isAdmin ? "/nodes/link" : null, { refreshInterval: poll });
  const [editing, setEditing] = useState<NodeInfo | null>(null);
  const [removing, setRemoving] = useState<NodeInfo | null>(null);
  const [updatingNode, setUpdatingNode] = useState<NodeInfo | null>(null);
  const [serverBusy, setServerBusy] = useState<string | null>(null);
  // Start/Stopp nur zeigen, wenn das Recht „power“ im Projekt des Servers besteht (die Prüfung macht das Backend)
  const { data: projects } = useApi<Project[]>("/projects");
  const canPower = (projectId: string) => can(projects?.find((project) => project.id === projectId)?.myPermissions, "power");

  if (error) {
    return (
      <>
        <div className="mb-4">
          <Link href="/nodes/" className="inline-flex items-center gap-1.5 text-xs text-muted transition hover:text-ink">
            <ChevronLeft className="h-4 w-4" />
            {t("Zurück zu allen Nodes")}
          </Link>
        </div>
        <ErrorState error={error} retry={() => mutate()} />
      </>
    );
  }

  if (!node) return <Loading label={t("Node-Details werden geladen")} />;

  const totalMem = node.memoryTotal ?? node.system?.memoryBytes ?? 0;
  const availMem = node.memoryAvailable ?? 0;
  const usedMem = node.memoryUsed ?? Math.max(0, totalMem - availMem);
  const committedMem = node.memoryCommitted ?? 0;
  const usedPercent = totalMem > 0 ? (usedMem / totalMem) * 100 : 0;
  const committedPercent = totalMem > 0 ? (committedMem / totalMem) * 100 : 0;
  const servers = node.servers ?? [];
  const runningServers = servers.filter((s) => s.view?.state === "running").length;
  const link = links.data?.find((entry) => entry.nodeId === node.id);
  const waiting = user.isAdmin && waitingForLink(node, links.data);

  async function handleServerPower(serverId: string, action: "start" | "restart" | "stop") {
    setServerBusy(serverId);
    const label = action === "start" ? t("Server startet") : action === "restart" ? t("Server startet neu") : t("Server wird gestoppt");
    await toast.run(() => api.post(`/servers/${serverId}/power`, { action }), label);
    setServerBusy(null);
    mutate();
  }

  return (
    <>
      <PageHeader
        crumbs={[{ label: t("Übersicht"), href: "/" }, { label: t("Nodes"), href: "/nodes/" }, { label: node.name }]}
        title={node.name}
        description={
          node.linked && !node.publicHost
            ? t("Node-Daemon, der sich selbst zum Panel verbindet. Systemanalyse und Server auf diesem Node.")
            : t("Node-Daemon auf {host}. Systemanalyse und Server auf diesem Node.", { host: node.publicHost || node.url || "–" })
        }
        meta={
          <>
            <Badge tone={node.online ? "good" : "bad"}>
              <Dot tone={node.online ? "good" : "bad"} />
              {node.online ? t("Online und erreichbar") : "Offline"}
            </Badge>
            {node.online && node.pingMs !== undefined && (
              <Badge tone={pingTone(node.pingMs)}>
                <Activity className="h-3 w-3" />
                {t("Latenz: {ms} ms", { ms: node.pingMs })}
              </Badge>
            )}
            <Badge>{t("Version: {version}", { version: versionLabel(node.version) })}</Badge>
            {node.updateAvailable && <Badge tone="warn">{t("Update auf neues Image verfügbar")}</Badge>}
            {node.system?.dockerVersion && <Badge>Docker {node.system.dockerVersion}</Badge>}
          </>
        }
        action={
          <>
            <Button variant="secondary" onClick={() => mutate()} loading={isLoading}>
              <RefreshCw className="h-4 w-4" />
              {t("Aktualisieren")}
            </Button>
            {user.isAdmin && (
              <>
                <Button variant="secondary" onClick={() => setEditing(node)}>
                  <Pencil className="h-4 w-4" />
                  {t("Bearbeiten")}
                </Button>
                {node.updateAvailable && (
                  <Button variant="secondary" onClick={() => setUpdatingNode(node)}>
                    <ArrowUpCircle className="h-4 w-4" />
                    {t("Update")}
                  </Button>
                )}
                <Button variant="ghost" onClick={() => setRemoving(node)} aria-label={t("Node entfernen")}>
                  <Trash2 className="h-4 w-4" />
                  {t("Entfernen")}
                </Button>
              </>
            )}
          </>
        }
      />

      {node.error && !waiting && (
        <div className="mb-6 rounded-lg border border-bad/25 bg-bad/10 px-4 py-3 text-xs text-bad">
          <p className="font-medium">{t("Fehler bei der Verbindung:")}</p>
          <p className="mt-1 break-words">{node.error}</p>
        </div>
      )}

      <div className="grid gap-4 sm:grid-cols-2 xl:grid-cols-4">
        <StatCard
          label={t("Belegter Arbeitsspeicher")}
          value={bytes(usedMem)}
          icon={<HardDrive className="h-4 w-4" />}
          tone={usedPercent > 90 ? "red" : usedPercent > 75 ? "amber" : "violet"}
          hint={t("{free} noch frei (von {total})", { free: bytes(availMem), total: bytes(totalMem) })}
        />
        <StatCard
          label={t("Zugesagte Limits (RAM)")}
          value={bytes(committedMem)}
          icon={<Layers className="h-4 w-4" />}
          tone={committedPercent > 100 ? "amber" : "blue"}
          hint={t("{percent} % der physischen Kapazität", { percent: formatNumber(committedPercent, 0) })}
        />
        <StatCard
          label={t("Ping / Latenz")}
          value={node.pingMs !== undefined ? `${node.pingMs} ms` : "–"}
          icon={<Activity className="h-4 w-4" />}
          tone={node.pingMs === undefined ? "neutral" : node.pingMs < 60 ? "green" : node.pingMs < 150 ? "amber" : "red"}
          hint={t("Antwortzeit des Node-Daemons")}
        />
        <StatCard
          label={t("Laufende Server")}
          value={`${runningServers}/${servers.length}`}
          icon={<ServerIcon className="h-4 w-4" />}
          tone={runningServers > 0 ? "green" : "neutral"}
          hint={tp("nodeServersTotal", servers.length)}
        />
      </div>

      <div className="mt-8 grid gap-4 lg:grid-cols-3">
        <Card className="min-w-0 p-5 lg:col-span-2">
          <div className="flex flex-wrap items-start justify-between gap-2">
            <div className="min-w-0">
              <h2 className="font-display text-sm font-medium tracking-wide">{t("Arbeitsspeicher-Analyse (RAM)")}</h2>
              <p className="mt-0.5 text-xs text-muted">{t("Vergleich von tatsächlicher Auslastung und zugesagten Server-Limits.")}</p>
            </div>
            <span className="font-mono text-xs text-ink">{t("{total} gesamt", { total: bytes(totalMem) })}</span>
          </div>

          <div className="mt-6 space-y-5">
            <div>
              <div className="mb-1.5 flex justify-between gap-2 text-xs">
                <span className="text-muted">{t("Tatsächlich belegter Speicher")}</span>
                <span className="tabular-nums font-medium text-ink">
                  {bytes(usedMem)} ({formatNumber(usedPercent, 1)} %)
                </span>
              </div>
              <Meter value={usedPercent} max={100} tone={usedPercent > 90 ? "bad" : usedPercent > 75 ? "warn" : "good"} />
              <p className="mt-1 text-[11px] text-muted">
                {t("Frei auf der Maschine:")} <span className="font-medium text-ink">{bytes(availMem)}</span>
              </p>
            </div>

            <div>
              <div className="mb-1.5 flex justify-between gap-2 text-xs">
                <span className="text-muted">{t("Zugesagte Limits laufender Server")}</span>
                <span className="tabular-nums font-medium text-ink">
                  {bytes(committedMem)} ({formatNumber(committedPercent, 1)} %)
                </span>
              </div>
              <Meter value={committedPercent} max={100} tone={committedPercent > 100 ? "bad" : committedPercent > 85 ? "warn" : "good"} />
              <p className="mt-1 text-[11px] text-muted">
                {committedPercent > 100 ? (
                  <span className="font-medium text-warn">{t("Die Limits überschreiten den Speicher des Nodes. Das geht gut, solange nicht alle Server voll ausgelastet sind.")}</span>
                ) : (
                  <span>{t("Die Server-Limits passen vollständig in den physischen Arbeitsspeicher.")}</span>
                )}
              </p>
            </div>

            <div className="grid grid-cols-2 gap-3 rounded-lg border border-white/[0.06] bg-black/20 p-3 text-xs sm:grid-cols-4">
              <div>
                <p className="text-[11px] text-muted">{t("Gesamter RAM")}</p>
                <p className="mt-0.5 font-medium text-ink">{bytes(totalMem)}</p>
              </div>
              <div>
                <p className="text-[11px] text-muted">{t("Tatsächlich belegt")}</p>
                <p className="mt-0.5 font-medium text-good">{bytes(usedMem)}</p>
              </div>
              <div>
                <p className="text-[11px] text-muted">{t("Frei / Verfügbar")}</p>
                <p className="mt-0.5 font-medium text-ink">{bytes(availMem)}</p>
              </div>
              <div>
                <p className="text-[11px] text-muted">{t("Zugesagte Limits")}</p>
                <p className="mt-0.5 font-medium text-accent">{bytes(committedMem)}</p>
              </div>
            </div>
          </div>
        </Card>

        <Card className="flex min-w-0 flex-col justify-between p-5">
          <div>
            <h2 className="font-display text-sm font-medium tracking-wide">{t("System-Informationen")}</h2>
            <p className="mt-0.5 text-xs text-muted">{t("Hardware und Docker-Umgebung dieser Maschine.")}</p>

            <dl className="mt-4 space-y-3 text-xs">
              <div className="flex justify-between gap-3 border-b border-white/[0.04] pb-2">
                <dt className="text-muted">{t("Prozessor (CPUs)")}</dt>
                <dd className="font-medium text-ink">{node.system?.cpus !== undefined ? tp("cores", node.system.cpus) : "–"}</dd>
              </div>
              <div className="flex justify-between gap-3 border-b border-white/[0.04] pb-2">
                <dt className="text-muted">{t("Betriebssystem")}</dt>
                <dd className="font-medium text-ink">{node.system?.os ?? "–"}</dd>
              </div>
              <div className="flex justify-between gap-3 border-b border-white/[0.04] pb-2">
                <dt className="text-muted">{t("Architektur")}</dt>
                <dd className="font-medium text-ink">{node.system?.architecture ?? "–"}</dd>
              </div>
              <div className="flex justify-between gap-3 border-b border-white/[0.04] pb-2">
                <dt className="text-muted">{t("Docker-Version")}</dt>
                <dd className="font-medium text-ink">{node.system?.dockerVersion ?? "–"}</dd>
              </div>
              <div className="flex justify-between gap-3 border-b border-white/[0.04] pb-2">
                <dt className="text-muted">{t("Daemon-Version")}</dt>
                <dd className="font-medium text-ink">{node.version || "dev"}</dd>
              </div>
              <div className="flex justify-between gap-3">
                <dt className="text-muted">{t("Öffentlicher Host")}</dt>
                <dd className="truncate font-mono text-[11px] text-ink">{node.publicHost || "–"}</dd>
              </div>
            </dl>
          </div>

          {user.isAdmin && (
            <div className="mt-4 border-t border-white/[0.06] pt-3">
              <p className="text-[11px] text-muted">{t("Adresse des Daemons:")}</p>
              <p className="mt-0.5 truncate font-mono text-[11px] text-ink/70">{node.linked ? t("verbindet sich selbst zum Panel") : node.url || "–"}</p>
            </div>
          )}
        </Card>
      </div>

      {/* Verbindung (selbst verbundene Nodes) und Festplatte; die Karten bringen eine eigene Trennlinie mit, die hier entfällt */}
      {((user.isAdmin && node.linked) || node.online) && (
        <div className="mt-4 grid gap-4 lg:grid-cols-2">
          {user.isAdmin && node.linked && (
            <Card className="min-w-0 p-5 [&>div]:mt-0 [&>div]:border-t-0 [&>div]:pt-0">
              <NodeLinkCard node={node} link={link} error={links.error} retry={() => links.mutate()} onChanged={() => links.mutate()} />
            </Card>
          )}
          {node.online && (
            <Card className="min-w-0 p-5 [&>div]:mt-0 [&>div]:border-t-0 [&>div]:pt-0">
              <NodeStorageCard node={node} />
            </Card>
          )}
        </div>
      )}

      <div className="mt-10">
        <div className="mb-4 flex flex-wrap items-center justify-between gap-2">
          <div className="min-w-0">
            <h2 className="font-display text-base tracking-wide text-pixel-shadow">{t("Server auf „{name}“", { name: node.name })}</h2>
            <p className="text-xs text-muted">{t("Alle Minecraft- und Proxy-Server, die auf dieser Maschine laufen oder eingerichtet sind.")}</p>
          </div>
          <Badge tone={runningServers > 0 ? "good" : "neutral"}>{t("{running} von {total} aktiv", { running: runningServers, total: servers.length })}</Badge>
        </div>

        {servers.length === 0 ? (
          <Card>
            <EmptyState
              icon={<ServerIcon className="h-6 w-6" />}
              title={t("Keine Server auf diesem Node")}
              text={t("Aktuell ist kein Server auf dieser Maschine. Beim Anlegen eines Servers in einem Projekt kannst du diesen Node auswählen.")}
            />
          </Card>
        ) : (
          <div className="grid gap-4 md:grid-cols-2 xl:grid-cols-3">
            {servers.map((server) => {
              const state = server.view?.state;
              const isRunning = state === "running";
              const isOffline = state === "offline" || !state;
              const isBusy = serverBusy === server.id;
              const projectName = node.projects?.[server.projectId] ?? t("Projekt");

              return (
                <Card key={server.id} className="flex min-w-0 flex-col justify-between p-5">
                  <div>
                    <div className="flex items-start justify-between gap-2">
                      <div className="min-w-0">
                        <Link href={`/server/${server.id}/`} className="block truncate font-display text-sm tracking-wide text-ink hover:text-accent hover:underline">
                          {server.name}
                        </Link>
                        <p className="mt-0.5 truncate text-xs text-muted">
                          {t("Projekt:")}{" "}
                          <Link href={`/project/${server.projectId}/`} className="text-ink/80 hover:underline">
                            {projectName}
                          </Link>
                        </p>
                      </div>
                      <StateBadge server={server} />
                    </div>

                    <dl className="mt-4 grid grid-cols-2 gap-x-3 gap-y-2 text-xs">
                      <dt className="text-muted">{t("Software")}</dt>
                      <dd className="font-medium text-ink">
                        {softwareName(server.software)} {server.view?.version ? `(${server.view.version})` : ""}
                      </dd>
                      <dt className="text-muted">{t("RAM-Limit")}</dt>
                      <dd className="font-medium text-ink">{server.view?.memoryMB ? megabytes(server.view.memoryMB) : "–"}</dd>
                      <dt className="text-muted">{t("Adresse")}</dt>
                      <dd className="truncate font-mono text-[11px] text-ink">{server.address || server.publicAddress || "–"}</dd>
                      {server.voiceAddress && (
                        <>
                          <dt className="text-muted">Voicechat</dt>
                          <dd className="truncate font-mono text-[11px] text-accent">{server.voiceAddress}</dd>
                        </>
                      )}
                    </dl>
                  </div>

                  <div className="mt-5 flex flex-wrap items-center justify-between gap-2 border-t border-white/[0.06] pt-3">
                    <div className="flex items-center gap-1">
                      {canPower(server.projectId) && isOffline && (
                        <Button size="sm" variant="secondary" loading={isBusy} onClick={() => handleServerPower(server.id, "start")} title={t("Server starten")}>
                          <Play className="h-3 w-3 text-good" />
                          {t("Start")}
                        </Button>
                      )}
                      {canPower(server.projectId) && isRunning && (
                        <>
                          <Button size="sm" variant="secondary" loading={isBusy} onClick={() => handleServerPower(server.id, "restart")} title={t("Server neu starten")}>
                            <RotateCw className="h-3 w-3" />
                            {t("Neustart")}
                          </Button>
                          <Button size="sm" variant="secondary" loading={isBusy} onClick={() => handleServerPower(server.id, "stop")} title={t("Server stoppen")}>
                            <Square className="h-3 w-3 text-bad" />
                            {t("Stopp")}
                          </Button>
                        </>
                      )}
                    </div>

                    <Link href={`/server/${server.id}/`} className="inline-flex items-center gap-1 text-xs font-medium text-accent hover:underline">
                      {t("Konsole und Verwaltung")}
                      <ArrowUpRight className="h-3.5 w-3.5" />
                    </Link>
                  </div>
                </Card>
              );
            })}
          </div>
        )}
      </div>

      {user.isAdmin && (
        <div className="mt-10">
          <div className="mb-4 flex flex-wrap items-center justify-between gap-2">
            <div className="min-w-0">
              <h2 className="font-display text-base tracking-wide text-pixel-shadow">{t("playit.gg auf dieser Maschine")}</h2>
              <p className="text-xs text-muted">{t("Der playit-Agent macht Server auf diesem Node ohne Portfreigabe im Router erreichbar.")}</p>
            </div>
            <Link href="/admin/playit/" className="text-xs font-medium text-accent hover:underline">
              {t("Zur vollständigen playit-Seite →")}
            </Link>
          </div>

          <Card className="p-5">
            <PlayitNodeCard node={node} onUpdated={() => mutate()} />
          </Card>
        </div>
      )}

      <NodeDialog node={editing} onClose={() => setEditing(null)} onDone={() => mutate()} />
      <ConfirmDialog
        open={!!updatingNode}
        onClose={() => setUpdatingNode(null)}
        title={t("Node „{name}“ aktualisieren?", { name: updatingNode?.name })}
        text={t("Der Node-Daemon wird auf das Image der neuen Version gehoben und neu gestartet. Laufende Minecraft-Server auf diesem Node laufen ungestört weiter.")}
        confirm={t("Aktualisieren")}
        onConfirm={async () => {
          if (!updatingNode) return;
          await toast.run(() => api.post(`/nodes/${updatingNode.id}/update`), t("Node-Update gestartet"));
          mutate();
        }}
      />
      <ConfirmDialog
        open={!!removing}
        onClose={() => setRemoving(null)}
        title={t("„{name}“ entfernen?", { name: removing?.name })}
        text={t("Der Node wird aus dem Panel entfernt. Das geht nur, wenn kein Server mehr auf ihm liegt. Der Daemon selbst wird nicht angehalten.")}
        confirm={t("Entfernen")}
        danger
        onConfirm={async () => {
          if (!removing) return;
          const ok = await toast.run(() => api.delete(`/nodes/${removing.id}`), t("Node entfernt"));
          if (ok) navigation.push("/nodes/");
        }}
      />
    </>
  );
}

// ================================================================
// playit.gg auf einem Node
// ================================================================

function PlayitNodeCard({ node, onUpdated }: { node: NodeInfo; onUpdated: () => void }) {
  const toast = useToast();
  const { data, error, mutate } = useApi<PlayitStatus>(`/nodes/${node.id}/playit`, { refreshInterval: usePoll(30000, 6000) });
  const [open, setOpen] = useState(false);
  const [secret, setSecret] = useState("");
  const [busy, setBusy] = useState(false);
  const [removing, setRemoving] = useState(false);
  const [claiming, setClaiming] = useState(false);

  if (error) return <ErrorState inline error={error} retry={() => mutate()} />;
  if (!data) return <Loading />;

  return (
    <div>
      <div className="flex flex-wrap items-center justify-between gap-3">
        <div className="flex min-w-0 items-center gap-3">
          <span className="grid h-9 w-9 shrink-0 place-items-center rounded-md border border-white/[0.08] bg-black/20 text-muted shadow-sunken">
            <Cable className="h-4 w-4" />
          </span>
          <div className="min-w-0">
            <p className="font-display text-sm tracking-wide">{t("playit.gg-Agent")}</p>
            <p className="text-xs text-muted">
              {data.configured ? (data.running ? t("Agent läuft als Docker-Container") : t("Agent ist gestoppt")) : t("Noch kein playit-Schlüssel hinterlegt")}
            </p>
          </div>
        </div>

        <div className="flex flex-wrap items-center gap-2">
          <Badge tone={data.configured ? (data.running ? "good" : "warn") : "neutral"}>
            <Dot tone={data.configured ? (data.running ? "good" : "warn") : "neutral"} />
            {data.configured ? (data.running ? t("Verbunden") : t("Gestoppt")) : t("Nicht eingerichtet")}
          </Badge>

          <Button size="sm" variant="secondary" onClick={() => setClaiming(true)}>
            {data.configured ? t("Neu verbinden") : t("Mit playit verbinden")}
          </Button>
          <Button size="sm" variant="ghost" onClick={() => setOpen(true)}>
            {t("Schlüssel eintragen")}
          </Button>
          {data.configured && (
            <Button size="sm" variant="ghost" onClick={() => setRemoving(true)}>
              {t("Entfernen")}
            </Button>
          )}
        </div>
      </div>

      {data.error && <p className="mt-3 text-xs text-bad">{data.error}</p>}

      {data.configured && data.logs && data.logs.length > 0 && (
        <div className="mt-4">
          <p className="mb-1 text-[11px] text-muted">{t("Protokoll des Agenten:")}</p>
          <pre className="max-h-24 overflow-auto rounded-md border border-white/[0.06] bg-black/40 p-2 font-mono text-[10px] leading-relaxed text-muted">{data.logs.slice(-8).join("\n")}</pre>
        </div>
      )}

      <PlayitClaimDialog
        nodeId={node.id}
        open={claiming}
        onClose={() => setClaiming(false)}
        onDone={() => {
          setClaiming(false);
          toast.success(t("Mit playit verbunden"));
          mutate();
          onUpdated();
        }}
      />

      <Dialog
        open={open}
        onClose={() => setOpen(false)}
        title={t("playit-Schlüssel eintragen")}
        description={t("Ein Schlüssel aus dem playit-Dashboard. Nimm alternativ „Mit playit verbinden“, um den Node automatisch zu autorisieren.")}
        footer={
          <>
            <Button variant="ghost" onClick={() => setOpen(false)}>
              {t("Abbrechen")}
            </Button>
            <Button
              variant="primary"
              loading={busy}
              disabled={secret.trim().length < 16}
              onClick={async () => {
                setBusy(true);
                const ok = await toast.run(() => api.put(`/nodes/${node.id}/playit`, { secret: secret.trim() }), t("playit-Agent gestartet"));
                setBusy(false);
                if (ok) {
                  setOpen(false);
                  setSecret("");
                  mutate();
                  onUpdated();
                }
              }}
            >
              {t("Speichern und starten")}
            </Button>
          </>
        }
      >
        <ol className="list-decimal space-y-1 pl-5 text-xs text-muted">
          <li>
            {t("Melde dich auf")}{" "}
            <a className="text-accent hover:underline" href="https://playit.gg/account/agents" target="_blank" rel="noreferrer">
              playit.gg/account/agents
            </a>{" "}
            {t("an und lege einen neuen Agenten an.")}
          </li>
          <li>{t("Kopiere den Geheimschlüssel (SECRET_KEY) hierher.")}</li>
        </ol>
        <div>
          <label className="label">{t("Schlüssel")}</label>
          <input className="field font-mono text-xs" type="password" value={secret} onChange={(e) => setSecret(e.target.value)} autoComplete="off" />
        </div>
      </Dialog>

      <ConfirmDialog
        open={removing}
        onClose={() => setRemoving(false)}
        title={t("playit-Agent entfernen?")}
        text={t("Der Agent wird gestoppt und der Schlüssel gelöscht. Tunnel auf diesem Node funktionieren dann nicht mehr.")}
        confirm={t("Entfernen")}
        danger
        onConfirm={async () => {
          await toast.run(() => api.delete(`/nodes/${node.id}/playit`), t("Agent entfernt"));
          mutate();
          onUpdated();
        }}
      />
    </div>
  );
}

// ================================================================
// Dialog: Node anlegen (direkt mit Adresse und Token) oder bearbeiten
// ================================================================

function NodeDialog({ node, onClose, onDone }: { node: NodeInfo | "new" | null; onClose: () => void; onDone: () => void }) {
  const toast = useToast();
  const existing = node && node !== "new" ? node : null;
  const linked = !!existing?.linked; // verbindet sich selbst: keine Adresse, kein Token
  const [form, setForm] = useState({ name: "", url: "", token: "", publicHost: "" });
  const [busy, setBusy] = useState(false);
  const [seen, setSeen] = useState<NodeInfo | "new" | null>(null);

  if (node !== seen) {
    setSeen(node);
    setForm({
      name: existing?.name ?? "",
      url: existing?.url ?? "",
      token: "",
      publicHost: existing?.publicHost ?? "",
    });
  }

  const set = (key: keyof typeof form) => (event: React.ChangeEvent<HTMLInputElement>) => setForm((current) => ({ ...current, [key]: event.target.value }));

  return (
    <Dialog
      open={!!node}
      onClose={onClose}
      title={existing ? t("Node bearbeiten") : t("Node hinzufügen")}
      footer={
        <>
          <Button variant="ghost" onClick={onClose}>
            {t("Abbrechen")}
          </Button>
          <Button
            variant="primary"
            loading={busy}
            disabled={!form.name.trim() || (!linked && !form.url.trim()) || (!existing && !form.token.trim())}
            onClick={async () => {
              setBusy(true);
              const ok = await toast.run(() => (existing ? api.patch(`/nodes/${existing.id}`, form) : api.post("/nodes", form)), existing ? t("Gespeichert") : t("Node hinzugefügt"));
              setBusy(false);
              if (ok) {
                onClose();
                onDone();
              }
            }}
          >
            {existing ? t("Speichern") : t("Hinzufügen")}
          </Button>
        </>
      }
    >
      <div>
        <label className="label">{t("Name")}</label>
        <input className="field" value={form.name} onChange={set("name")} placeholder="home" maxLength={48} autoFocus />
      </div>
      {!linked && (
        <>
          <div>
            <label className="label">{t("Adresse des Daemons")}</label>
            <input className="field font-mono text-xs" value={form.url} onChange={set("url")} placeholder="http://node.example.org:8480" />
          </div>
          <div>
            <label className="label">Token {existing && t("(leer lassen, um es zu behalten)")}</label>
            <input className="field font-mono text-xs" type="password" value={form.token} onChange={set("token")} autoComplete="off" />
          </div>
        </>
      )}
      <div>
        <label className="label">{t("Öffentliche Adresse (für Spieler)")}</label>
        <input className="field" value={form.publicHost} onChange={set("publicHost")} placeholder="mc.example.org" />
        <p className="mt-1 text-xs text-muted">{t("Wird für die Beitritts-Adresse und die Voicechat-Konfiguration verwendet.")}</p>
      </div>
    </Dialog>
  );
}
