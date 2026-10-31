"use client";

import Link from "next/link";
import { usePathname, useRouter, useSearchParams } from "next/navigation";
import {
  Activity,
  ArrowUpCircle,
  ArrowUpRight,
  Cable,
  ChevronLeft,
  ChevronRight,
  Cpu,
  ExternalLink,
  HardDrive,
  Layers,
  Pencil,
  Play,
  Plus,
  Radio,
  RefreshCw,
  RotateCw,
  Server as ServerIcon,
  Square,
  Terminal,
  Trash2,
} from "lucide-react";
import { Suspense, useState } from "react";
import { Badge, Button, Card, Dot, EmptyState, Meter, PageHeader, StatCard } from "@/components/ui/primitives";
import { ConfirmDialog, Dialog, useToast } from "@/components/ui/feedback";
import { ErrorState, Loading } from "@/components/ui/async";
import { useUser } from "@/components/layout/shell";
import { PlayitClaimDialog } from "@/components/playit-claim";
import { StateBadge } from "@/components/status";
import { api, useApi } from "@/lib/api";
import { bytes, megabytes } from "@/lib/format";
import type { NodeDetail, NodeInfo, PlayitStatus, Server } from "@/lib/types";
import { AdminSidebar } from "@/components/admin-sidebar";

export default function NodesPage() {
  return (
    <Suspense fallback={<Loading label="Nodes werden geladen" />}>
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

// ================================================================
// LIST VIEW: Übersicht aller Nodes
// ================================================================

function NodesListView() {
  const user = useUser();
  const { data, error, mutate, isLoading } = useApi<NodeInfo[]>("/nodes", { refreshInterval: 8000 });
  const [editing, setEditing] = useState<NodeInfo | "new" | null>(null);
  const [removing, setRemoving] = useState<NodeInfo | null>(null);
  const [updatingNode, setUpdatingNode] = useState<NodeInfo | null>(null);
  const toast = useToast();

  if (error) return <ErrorState error={error} retry={() => mutate()} />;
  if (!data) return <Loading label="Nodes werden geladen" />;

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
      {user.isAdmin && <AdminSidebar />}
      <PageHeader
        title="Nodes"
        description="Maschinen mit Docker, auf denen die Server laufen. Auf jeder läuft der Lodestone-Node-Daemon."
        action={
          <div className="flex gap-2">
            <Button variant="secondary" onClick={() => mutate()} loading={isLoading}>
              <RefreshCw className="h-4 w-4" />
              Aktualisieren
            </Button>
            {user.isAdmin && (
              <Button variant="primary" onClick={() => setEditing("new")}>
                <Plus className="h-4 w-4" />
                Node hinzufügen
              </Button>
            )}
          </div>
        }
      />

      <div className="grid gap-4 sm:grid-cols-2 xl:grid-cols-4">
        <StatCard
          label="Nodes online"
          value={`${onlineCount}/${data.length}`}
          icon={<Cpu className="h-4 w-4" />}
          tone={onlineCount === data.length && data.length > 0 ? "green" : onlineCount > 0 ? "amber" : "neutral"}
          hint={data.length === 0 ? "Noch kein Node vorhanden" : `${onlineCount} Maschinen bereit`}
        />
        <StatCard
          label="Verwaltete Server"
          value={totalServers}
          icon={<ServerIcon className="h-4 w-4" />}
          tone="blue"
          hint="Über alle Nodes verteilt"
        />
        <StatCard
          label="Arbeitsspeicher (RAM)"
          value={bytes(totalUsedMemory)}
          icon={<HardDrive className="h-4 w-4" />}
          tone="violet"
          hint={`von ${bytes(totalMemory)} belegt`}
        />
        <StatCard
          label="playit.gg-Tunnel"
          value={user.isAdmin ? "Verwalten" : "Aktiv"}
          icon={<Cable className="h-4 w-4" />}
          tone="neutral"
          hint={
            user.isAdmin ? (
              <Link href="/admin/playit/" className="text-accent hover:underline">
                Zur playit-Seite →
              </Link>
            ) : (
              "Ports ohne Portfreigabe"
            )
          }
        />
      </div>

      <div className="mt-8">
        {data.length === 0 ? (
          <Card>
            <EmptyState
              icon={<Cpu className="h-6 w-6" />}
              title="Noch kein Node eingerichtet"
              text={
                user.isAdmin
                  ? "Starte den Node-Daemon auf einer Maschine und trage hier Adresse und Token ein."
                  : "Ein Administrator muss mindestens einen Node einrichten."
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
              const pingTone =
                node.pingMs === undefined ? "neutral" : node.pingMs < 60 ? "good" : node.pingMs < 150 ? "warn" : "bad";

              return (
                <Card key={node.id} className="flex flex-col justify-between p-5">
                  <div>
                    <div className="flex items-start justify-between gap-3">
                      <div className="flex items-center gap-3">
                        <span className="grid h-9 w-9 place-items-center rounded-lg border border-white/[0.08] bg-white/[0.04] text-muted">
                          <Cpu className="h-4 w-4" />
                        </span>
                        <div>
                          <p className="text-sm font-semibold">{node.name}</p>
                          <p className="text-xs text-muted">{node.publicHost || "Keine öffentliche Adresse"}</p>
                        </div>
                      </div>
                      <div className="flex items-center gap-2">
                        {node.online && node.pingMs !== undefined && (
                          <Badge tone={pingTone}>
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
                          <dt className="text-muted">Version</dt>
                          <dd className="flex items-center gap-1.5">
                            <span>{!node.version ? "–" : node.version === "dev" ? "dev" : `v${node.version}`}</span>
                            {node.updateAvailable && <Badge tone="warn">Update verfügbar</Badge>}
                          </dd>
                          <dt className="text-muted">Docker</dt>
                          <dd className="truncate">{node.system.dockerVersion}</dd>
                          <dt className="text-muted">System</dt>
                          <dd className="truncate">
                            {node.system.os} · {node.system.architecture}
                          </dd>
                          <dt className="text-muted">Kerne / RAM</dt>
                          <dd>
                            {node.system.cpus} Kerne · {bytes(totalMem)}
                          </dd>
                          <dt className="text-muted">Server auf diesem Node</dt>
                          <dd className="font-semibold text-ink">
                            {node.serverCount} {node.serverCount === 1 ? "Server" : "Server"}
                          </dd>
                        </dl>

                        {/* Speicher-Übersicht */}
                        <div className="mt-4 space-y-2.5 border-t border-white/[0.06] pt-3">
                          <div>
                            <div className="mb-1 flex justify-between text-[11px] text-muted">
                              <span>Arbeitsspeicher (belegt)</span>
                              <span className="tabular-nums text-ink">
                                {bytes(usedMem)} von {bytes(totalMem)} ({usedPercent.toFixed(0)}%)
                              </span>
                            </div>
                            <Meter
                              value={usedPercent}
                              max={100}
                              tone={usedPercent > 90 ? "bad" : usedPercent > 75 ? "warn" : "good"}
                            />
                          </div>
                          {committedMem > 0 && (
                            <div className="flex justify-between text-[11px] text-muted">
                              <span>Zugesagt durch Server-Limits:</span>
                              <span className="tabular-nums text-ink">{bytes(committedMem)}</span>
                            </div>
                          )}
                        </div>
                      </>
                    ) : (
                      node.error && (
                        <p className="mt-4 rounded-md border border-bad/25 bg-bad/10 px-3 py-2 text-xs text-bad">
                          {node.error}
                        </p>
                      )
                    )}

                    {user.isAdmin && node.online && node.updateAvailable && (
                      <div className="mt-3 flex items-center justify-between gap-2 rounded-lg border border-warn/25 bg-warn/10 px-3 py-2 text-xs">
                        <span className="text-warn">Ein Update ist verfügbar.</span>
                        <Button variant="secondary" size="sm" onClick={() => setUpdatingNode(node)}>
                          <ArrowUpCircle className="h-3.5 w-3.5" />
                          Aktualisieren
                        </Button>
                      </div>
                    )}
                  </div>

                  <div className="mt-5 flex items-center justify-between gap-2 border-t border-white/[0.06] pt-3">
                    <Link
                      href={`/nodes/${node.id}/`}
                      className="inline-flex items-center gap-1.5 text-xs font-medium text-accent transition hover:underline"
                    >
                      Details & Server ansehen
                      <ChevronRight className="h-3.5 w-3.5" />
                    </Link>

                    {user.isAdmin && (
                      <div className="flex gap-1">
                        <Button variant="ghost" size="icon-sm" onClick={() => setEditing(node)} aria-label="Bearbeiten">
                          <Pencil className="h-3.5 w-3.5" />
                        </Button>
                        <Button variant="ghost" size="icon-sm" onClick={() => setRemoving(node)} aria-label="Entfernen">
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
      <ConfirmDialog
        open={!!updatingNode}
        onClose={() => setUpdatingNode(null)}
        title={`Node „${updatingNode?.name}“ aktualisieren?`}
        text="Der Node-Daemon wird auf das Image der neuen Version gehoben und neu gestartet. Laufende Minecraft-Server auf diesem Node laufen ungestört weiter."
        confirm="Aktualisieren"
        onConfirm={async () => {
          if (!updatingNode) return;
          await toast.run(() => api.post(`/nodes/${updatingNode.id}/update`), "Node-Update gestartet");
          mutate();
        }}
      />
      <ConfirmDialog
        open={!!removing}
        onClose={() => setRemoving(null)}
        title={`„${removing?.name}“ entfernen?`}
        text="Der Node wird aus dem Panel entfernt. Das geht nur, wenn kein Server mehr auf ihm liegt. Der Daemon selbst wird nicht angehalten."
        confirm="Entfernen"
        danger
        onConfirm={async () => {
          if (!removing) return;
          await toast.run(() => api.delete(`/nodes/${removing.id}`), "Node entfernt");
          mutate();
        }}
      />
    </>
  );
}

// ================================================================
// DETAIL VIEW: Eigene Seite für einen spezifischen Node
// ================================================================

function NodeDetailView({ nodeId }: { nodeId: string }) {
  const user = useUser();
  const toast = useToast();
  const router = useRouter();
  const { data: node, error, mutate, isLoading } = useApi<NodeDetail>(`/nodes/${nodeId}`, { refreshInterval: 5000 });
  const [editing, setEditing] = useState<NodeInfo | null>(null);
  const [removing, setRemoving] = useState<NodeInfo | null>(null);
  const [updatingNode, setUpdatingNode] = useState<NodeInfo | null>(null);
  const [serverBusy, setServerBusy] = useState<string | null>(null);

  if (error) {
    return (
      <>
        {user.isAdmin && <AdminSidebar />}
        <div className="mb-4">
          <Link href="/nodes/" className="inline-flex items-center gap-1.5 text-xs text-muted transition hover:text-ink">
            <ChevronLeft className="h-4 w-4" />
            Zurück zu allen Nodes
          </Link>
        </div>
        <ErrorState error={error} retry={() => mutate()} />
      </>
    );
  }

  if (!node) {
    return (
      <>
        {user.isAdmin && <AdminSidebar />}
        <Loading label="Node-Details werden geladen" />
      </>
    );
  }

  const totalMem = node.memoryTotal ?? node.system?.memoryBytes ?? 0;
  const availMem = node.memoryAvailable ?? 0;
  const usedMem = node.memoryUsed ?? Math.max(0, totalMem - availMem);
  const committedMem = node.memoryCommitted ?? 0;
  const usedPercent = totalMem > 0 ? (usedMem / totalMem) * 100 : 0;
  const committedPercent = totalMem > 0 ? (committedMem / totalMem) * 100 : 0;
  const pingTone = node.pingMs === undefined ? "neutral" : node.pingMs < 60 ? "good" : node.pingMs < 150 ? "warn" : "bad";
  const servers = node.servers ?? [];
  const runningServers = servers.filter((s) => s.view?.state === "running").length;

  async function handleServerPower(serverId: string, action: "start" | "restart" | "stop") {
    setServerBusy(serverId);
    const label = action === "start" ? "Server startet" : action === "restart" ? "Server startet neu" : "Server stoppt";
    await toast.run(() => api.post(`/servers/${serverId}/power`, { action }), label);
    setServerBusy(null);
    mutate();
  }

  return (
    <>
      {user.isAdmin && <AdminSidebar />}
      <div>
        <Link
          href="/nodes/"
          className="mb-3 inline-flex items-center gap-1.5 text-xs text-muted transition hover:text-ink"
        >
          <ChevronLeft className="h-4 w-4" />
          Zurück zu allen Nodes
        </Link>
      </div>

      <PageHeader
        title={node.name}
        description={`Node-Daemon auf ${node.publicHost || node.url || "verbunden"}. Detaillierte Systemanalyse und Server auf diesem Node.`}
        action={
          <div className="flex flex-wrap items-center gap-2">
            <Button variant="secondary" onClick={() => mutate()} loading={isLoading}>
              <RefreshCw className="h-4 w-4" />
              Aktualisieren
            </Button>
            {user.isAdmin && (
              <>
                <Button variant="secondary" onClick={() => setEditing(node)}>
                  <Pencil className="h-4 w-4" />
                  Bearbeiten
                </Button>
                {node.updateAvailable && (
                  <Button variant="secondary" onClick={() => setUpdatingNode(node)}>
                    <ArrowUpCircle className="h-4 w-4" />
                    Aktualisieren
                  </Button>
                )}
                <Button variant="ghost" onClick={() => setRemoving(node)} aria-label="Node entfernen">
                  <Trash2 className="h-4 w-4" />
                  Löschen
                </Button>
              </>
            )}
          </div>
        }
      />

      {/* Badges / Quick Health Bar */}
      <div className="mb-6 flex flex-wrap items-center gap-2">
        <Badge tone={node.online ? "good" : "bad"}>
          <Dot tone={node.online ? "good" : "bad"} />
          {node.online ? "Online & Erreichbar" : "Offline"}
        </Badge>
        {node.online && node.pingMs !== undefined && (
          <Badge tone={pingTone}>
            <Activity className="h-3 w-3" />
            Latenz: {node.pingMs} ms
          </Badge>
        )}
        <Badge tone="neutral">Version: {!node.version ? "–" : node.version === "dev" ? "dev" : `v${node.version}`}</Badge>
        {node.updateAvailable && <Badge tone="warn">Update auf neues Image verfügbar</Badge>}
        {node.system?.dockerVersion && <Badge tone="neutral">Docker: {node.system.dockerVersion}</Badge>}
      </div>

      {node.error && (
        <div className="mb-6 rounded-lg border border-bad/30 bg-bad/10 p-4 text-xs text-bad">
          <p className="font-semibold">Fehler bei Verbindung:</p>
          <p className="mt-1">{node.error}</p>
        </div>
      )}

      {/* Analytics KPI StatCards */}
      <div className="grid gap-4 sm:grid-cols-2 xl:grid-cols-4">
        <StatCard
          label="Belegter Arbeitsspeicher"
          value={bytes(usedMem)}
          icon={<HardDrive className="h-4 w-4" />}
          tone={usedPercent > 90 ? "red" : usedPercent > 75 ? "amber" : "violet"}
          hint={`${bytes(availMem)} noch frei (von ${bytes(totalMem)})`}
        />
        <StatCard
          label="Zugesagte Limits (RAM)"
          value={bytes(committedMem)}
          icon={<Layers className="h-4 w-4" />}
          tone={committedPercent > 100 ? "amber" : "blue"}
          hint={`${committedPercent.toFixed(0)}% der physischen Kapazität`}
        />
        <StatCard
          label="Ping / Latenz"
          value={node.pingMs !== undefined ? `${node.pingMs} ms` : "–"}
          icon={<Activity className="h-4 w-4" />}
          tone={node.pingMs === undefined ? "neutral" : node.pingMs < 60 ? "green" : node.pingMs < 150 ? "amber" : "red"}
          hint="Roundtrip-Zeit zum Node-Daemon"
        />
        <StatCard
          label="Laufende Server"
          value={`${runningServers}/${servers.length}`}
          icon={<ServerIcon className="h-4 w-4" />}
          tone={runningServers > 0 ? "green" : "neutral"}
          hint={`${servers.length} Server insgesamt auf diesem Node`}
        />
      </div>

      {/* Detaillierte Speicher- und Systemanalyse */}
      <div className="mt-8 grid gap-6 lg:grid-cols-3">
        {/* Speicheranalyse Card */}
        <Card className="p-5 lg:col-span-2">
          <div className="flex items-center justify-between">
            <div>
              <h2 className="text-sm font-semibold">Arbeitsspeicher-Analyse (RAM)</h2>
              <p className="text-xs text-muted">Vergleich von tatsächlicher Auslastung und zugesagten Server-Limits.</p>
            </div>
            <span className="font-mono text-xs font-semibold text-ink">{bytes(totalMem)} Gesamt</span>
          </div>

          <div className="mt-6 space-y-5">
            {/* Tatsächliche Auslastung */}
            <div>
              <div className="mb-1.5 flex justify-between text-xs">
                <span className="text-muted">Tatsächlich belegter Speicher</span>
                <span className="tabular-nums font-medium text-ink">
                  {bytes(usedMem)} ({usedPercent.toFixed(1)}%)
                </span>
              </div>
              <Meter
                value={usedPercent}
                max={100}
                tone={usedPercent > 90 ? "bad" : usedPercent > 75 ? "warn" : "good"}
              />
              <p className="mt-1 text-[11px] text-muted">
                Frei auf der Maschine: <strong className="text-ink">{bytes(availMem)}</strong>
              </p>
            </div>

            {/* Zugesagter Speicher */}
            <div>
              <div className="mb-1.5 flex justify-between text-xs">
                <span className="text-muted">Zugesagte Limits laufender Server</span>
                <span className="tabular-nums font-medium text-ink">
                  {bytes(committedMem)} ({committedPercent.toFixed(1)}%)
                </span>
              </div>
              <Meter
                value={committedPercent}
                max={100}
                tone={committedPercent > 100 ? "bad" : committedPercent > 85 ? "warn" : "good"}
              />
              <p className="mt-1 text-[11px] text-muted">
                {committedPercent > 100 ? (
                  <span className="text-warn font-medium">
                    Achtung: Die Server-Limits überschreiten den physischen RAM des Nodes (Overcommit).
                  </span>
                ) : (
                  <span>Die Server-Limits passen vollständig in den physischen Arbeitsspeicher.</span>
                )}
              </p>
            </div>

            {/* Quick Metrics Breakdown Table */}
            <div className="grid grid-cols-2 gap-3 rounded-lg border border-white/[0.06] bg-white/[0.02] p-3 text-xs sm:grid-cols-4">
              <div>
                <p className="text-muted text-[11px]">Gesamter RAM</p>
                <p className="mt-0.5 font-semibold text-ink">{bytes(totalMem)}</p>
              </div>
              <div>
                <p className="text-muted text-[11px]">Tatsächlich belegt</p>
                <p className="mt-0.5 font-semibold text-good">{bytes(usedMem)}</p>
              </div>
              <div>
                <p className="text-muted text-[11px]">Frei / Verfügbar</p>
                <p className="mt-0.5 font-semibold text-ink">{bytes(availMem)}</p>
              </div>
              <div>
                <p className="text-muted text-[11px]">Zugesagte Limits</p>
                <p className="mt-0.5 font-semibold text-accent">{bytes(committedMem)}</p>
              </div>
            </div>
          </div>
        </Card>

        {/* System & Hardware Card */}
        <Card className="flex flex-col justify-between p-5">
          <div>
            <h2 className="text-sm font-semibold">System-Informationen</h2>
            <p className="text-xs text-muted">Hardware und Docker-Umgebung dieser Maschine.</p>

            <dl className="mt-4 space-y-3 text-xs">
              <div className="flex justify-between border-b border-white/[0.04] pb-2">
                <dt className="text-muted">Prozessor (CPUs)</dt>
                <dd className="font-semibold text-ink">{node.system?.cpus ?? "–"} Kerne</dd>
              </div>
              <div className="flex justify-between border-b border-white/[0.04] pb-2">
                <dt className="text-muted">Betriebssystem</dt>
                <dd className="font-semibold text-ink">{node.system?.os ?? "–"}</dd>
              </div>
              <div className="flex justify-between border-b border-white/[0.04] pb-2">
                <dt className="text-muted">Architektur</dt>
                <dd className="font-semibold text-ink">{node.system?.architecture ?? "–"}</dd>
              </div>
              <div className="flex justify-between border-b border-white/[0.04] pb-2">
                <dt className="text-muted">Docker-Version</dt>
                <dd className="font-semibold text-ink">{node.system?.dockerVersion ?? "–"}</dd>
              </div>
              <div className="flex justify-between border-b border-white/[0.04] pb-2">
                <dt className="text-muted">Daemon-Version</dt>
                <dd className="font-semibold text-ink">{node.version || "dev"}</dd>
              </div>
              <div className="flex justify-between">
                <dt className="text-muted">Öffentlicher Host</dt>
                <dd className="font-mono text-[11px] text-ink">{node.publicHost || "–"}</dd>
              </div>
            </dl>
          </div>

          {user.isAdmin && (
            <div className="mt-4 border-t border-white/[0.06] pt-3">
              <p className="text-[11px] text-muted">Daemon-Adresse:</p>
              <p className="mt-0.5 truncate font-mono text-[11px] text-ink/70">{node.url || "–"}</p>
            </div>
          )}
        </Card>
      </div>

      {/* Server auf diesem Node */}
      <div className="mt-10">
        <div className="mb-4 flex items-center justify-between">
          <div>
            <h2 className="text-base font-semibold">Server auf „{node.name}“</h2>
            <p className="text-xs text-muted">
              Alle Minecraft- und Proxy-Server, die auf dieser Maschine laufen oder eingerichtet sind.
            </p>
          </div>
          <Badge tone={runningServers > 0 ? "good" : "neutral"}>
            {runningServers} von {servers.length} aktiv
          </Badge>
        </div>

        {servers.length === 0 ? (
          <Card>
            <EmptyState
              icon={<ServerIcon className="h-6 w-6" />}
              title="Keine Server auf diesem Node"
              text="Aktuell ist kein Server auf dieser Maschine zugewiesen. Beim Erstellen eines Servers in einem Projekt kannst du diesen Node auswählen."
            />
          </Card>
        ) : (
          <div className="grid gap-4 md:grid-cols-2 xl:grid-cols-3">
            {servers.map((server) => {
              const state = server.view?.state;
              const isRunning = state === "running";
              const isOffline = state === "offline" || !state;
              const isBusy = serverBusy === server.id;
              const projectName = node.projects?.[server.projectId] ?? "Projekt";

              return (
                <Card key={server.id} className="flex flex-col justify-between p-5">
                  <div>
                    <div className="flex items-start justify-between gap-2">
                      <div className="min-w-0">
                        <Link href={`/server/${server.id}/`} className="text-sm font-semibold text-ink hover:text-accent hover:underline">
                          {server.name}
                        </Link>
                        <p className="mt-0.5 text-xs text-muted">
                          Projekt:{" "}
                          <Link href={`/project/${server.projectId}/`} className="text-ink/80 hover:underline">
                            {projectName}
                          </Link>
                        </p>
                      </div>
                      <StateBadge server={server} />
                    </div>

                    <dl className="mt-4 grid grid-cols-2 gap-x-3 gap-y-2 text-xs">
                      <dt className="text-muted">Software</dt>
                      <dd className="font-medium text-ink capitalize">
                        {server.software} {server.view?.version ? `(${server.view.version})` : ""}
                      </dd>
                      <dt className="text-muted">RAM-Limit</dt>
                      <dd className="font-semibold text-ink">
                        {server.view?.memoryMB ? megabytes(server.view.memoryMB) : "–"}
                      </dd>
                      <dt className="text-muted">Adresse</dt>
                      <dd className="truncate font-mono text-[11px] text-ink">
                        {server.address || server.publicAddress || "–"}
                      </dd>
                      {server.voiceAddress && (
                        <>
                          <dt className="text-muted">Voicechat</dt>
                          <dd className="truncate font-mono text-[11px] text-accent">{server.voiceAddress}</dd>
                        </>
                      )}
                    </dl>
                  </div>

                  <div className="mt-5 flex items-center justify-between border-t border-white/[0.06] pt-3">
                    <div className="flex items-center gap-1">
                      {isOffline && (
                        <Button
                          size="sm"
                          variant="secondary"
                          loading={isBusy}
                          onClick={() => handleServerPower(server.id, "start")}
                          title="Server starten"
                        >
                          <Play className="h-3 w-3 text-good" />
                          Start
                        </Button>
                      )}
                      {isRunning && (
                        <>
                          <Button
                            size="sm"
                            variant="secondary"
                            loading={isBusy}
                            onClick={() => handleServerPower(server.id, "restart")}
                            title="Server neu starten"
                          >
                            <RotateCw className="h-3 w-3" />
                            Neustart
                          </Button>
                          <Button
                            size="sm"
                            variant="secondary"
                            loading={isBusy}
                            onClick={() => handleServerPower(server.id, "stop")}
                            title="Server stoppen"
                          >
                            <Square className="h-3 w-3 text-bad" />
                            Stopp
                          </Button>
                        </>
                      )}
                    </div>

                    <Link
                      href={`/server/${server.id}/`}
                      className="inline-flex items-center gap-1 text-xs font-medium text-accent hover:underline"
                    >
                      Konsole & Verwaltung
                      <ArrowUpRight className="h-3.5 w-3.5" />
                    </Link>
                  </div>
                </Card>
              );
            })}
          </div>
        )}
      </div>

      {/* playit.gg Integration auf diesem Node */}
      {user.isAdmin && (
        <div className="mt-10">
          <div className="mb-4 flex items-center justify-between">
            <div>
              <h2 className="text-base font-semibold">playit.gg auf dieser Maschine</h2>
              <p className="text-xs text-muted">
                Der playit-Agent macht Server auf diesem Node ohne Router-Portfreigabe erreichbar.
              </p>
            </div>
            <Link href="/admin/playit/" className="text-xs font-medium text-accent hover:underline">
              Zur vollständigen playit-Seite →
            </Link>
          </div>

          <Card className="p-5">
            <PlayitNodeCard node={node} onUpdated={() => mutate()} />
          </Card>
        </div>
      )}

      {/* Edit and Update dialogs */}
      <NodeDialog
        node={editing}
        onClose={() => setEditing(null)}
        onDone={() => mutate()}
      />
      <ConfirmDialog
        open={!!updatingNode}
        onClose={() => setUpdatingNode(null)}
        title={`Node „${updatingNode?.name}“ aktualisieren?`}
        text="Der Node-Daemon wird auf das Image der neuen Version gehoben und neu gestartet. Laufende Minecraft-Server laufen ungestört weiter."
        confirm="Aktualisieren"
        onConfirm={async () => {
          if (!updatingNode) return;
          await toast.run(() => api.post(`/nodes/${updatingNode.id}/update`), "Node-Update gestartet");
          mutate();
        }}
      />
      <ConfirmDialog
        open={!!removing}
        onClose={() => setRemoving(null)}
        title={`„${removing?.name}“ entfernen?`}
        text="Der Node wird aus dem Panel entfernt. Das geht nur, wenn kein Server mehr auf ihm liegt."
        confirm="Entfernen"
        danger
        onConfirm={async () => {
          if (!removing) return;
          const ok = await toast.run(() => api.delete(`/nodes/${removing.id}`), "Node entfernt");
          if (ok) router.push("/nodes/");
        }}
      />
    </>
  );
}

// ================================================================
// PLAYIT CARD AUF DEM NODE
// ================================================================

function PlayitNodeCard({ node, onUpdated }: { node: NodeInfo; onUpdated: () => void }) {
  const toast = useToast();
  const { data, mutate } = useApi<PlayitStatus>(`/nodes/${node.id}/playit`, { refreshInterval: 6000 });
  const [open, setOpen] = useState(false);
  const [secret, setSecret] = useState("");
  const [busy, setBusy] = useState(false);
  const [removing, setRemoving] = useState(false);
  const [claiming, setClaiming] = useState(false);

  return (
    <div>
      <div className="flex flex-wrap items-center justify-between gap-3">
        <div className="flex items-center gap-3">
          <span className="grid h-9 w-9 place-items-center rounded-lg border border-white/[0.08] bg-white/[0.04] text-muted">
            <Cable className="h-4 w-4" />
          </span>
          <div>
            <p className="text-sm font-semibold">playit.gg Agent</p>
            <p className="text-xs text-muted">
              {data?.configured
                ? data.running
                  ? "Agent läuft als Docker-Container"
                  : "Agent ist gestoppt"
                : "Noch kein playit-Schlüssel hinterlegt"}
            </p>
          </div>
        </div>

        <div className="flex flex-wrap items-center gap-2">
          <Badge tone={data?.configured ? (data.running ? "good" : "warn") : "neutral"}>
            <Dot tone={data?.configured ? (data.running ? "good" : "warn") : "neutral"} />
            {data?.configured ? (data.running ? "Verbunden" : "Gestoppt") : "Nicht eingerichtet"}
          </Badge>

          <Button size="sm" variant="secondary" onClick={() => setClaiming(true)}>
            {data?.configured ? "Neu verbinden" : "Mit playit verbinden"}
          </Button>
          <Button size="sm" variant="ghost" onClick={() => setOpen(true)}>
            Schlüssel eintragen
          </Button>
          {data?.configured && (
            <Button size="sm" variant="ghost" onClick={() => setRemoving(true)}>
              Entfernen
            </Button>
          )}
        </div>
      </div>

      {data?.error && <p className="mt-3 text-xs text-bad">{data.error}</p>}

      {data?.configured && data.logs && data.logs.length > 0 && (
        <div className="mt-4">
          <p className="mb-1 text-[11px] text-muted">Agent-Logs:</p>
          <pre className="max-h-24 overflow-auto rounded-md bg-black/40 p-2 font-mono text-[10px] leading-relaxed text-muted">
            {data.logs.slice(-8).join("\n")}
          </pre>
        </div>
      )}

      <PlayitClaimDialog
        nodeId={node.id}
        open={claiming}
        onClose={() => setClaiming(false)}
        onDone={() => {
          setClaiming(false);
          toast.success("Mit playit verbunden");
          mutate();
          onUpdated();
        }}
      />

      <Dialog
        open={open}
        onClose={() => setOpen(false)}
        title="playit-Schlüssel eintragen"
        description="Ein Schlüssel aus dem playit-Dashboard. Nimm alternativ „Mit playit verbinden“, um den Node automatisch zu autorisieren."
        footer={
          <>
            <Button variant="ghost" onClick={() => setOpen(false)}>
              Abbrechen
            </Button>
            <Button
              variant="primary"
              loading={busy}
              disabled={secret.trim().length < 16}
              onClick={async () => {
                setBusy(true);
                const ok = await toast.run(
                  () => api.put(`/nodes/${node.id}/playit`, { secret: secret.trim() }),
                  "playit-Agent gestartet",
                );
                setBusy(false);
                if (ok) {
                  setOpen(false);
                  setSecret("");
                  mutate();
                  onUpdated();
                }
              }}
            >
              Speichern und starten
            </Button>
          </>
        }
      >
        <ol className="list-decimal space-y-1 pl-5 text-xs text-muted">
          <li>
            Melde dich auf{" "}
            <a className="text-accent hover:underline" href="https://playit.gg/account/agents" target="_blank" rel="noreferrer">
              playit.gg/account/agents
            </a>{" "}
            an und lege einen neuen Agenten an.
          </li>
          <li>Kopiere den geheimen Schlüssel (SECRET_KEY) hierher.</li>
        </ol>
        <div>
          <label className="label">Schlüssel</label>
          <input
            className="field font-mono text-xs"
            type="password"
            value={secret}
            onChange={(e) => setSecret(e.target.value)}
            placeholder="playit secret key..."
            autoComplete="off"
          />
        </div>
      </Dialog>

      <ConfirmDialog
        open={removing}
        onClose={() => setRemoving(false)}
        title="playit-Agent entfernen?"
        text="Der Agent wird gestoppt und der Schlüssel gelöscht. Tunnel auf diesem Node funktionieren dann nicht mehr."
        confirm="Entfernen"
        danger
        onConfirm={async () => {
          await toast.run(() => api.delete(`/nodes/${node.id}/playit`), "Agent entfernt");
          mutate();
          onUpdated();
        }}
      />
    </div>
  );
}

// ================================================================
// NODE DIALOG (Erstellen / Bearbeiten)
// ================================================================

function NodeDialog({
  node,
  onClose,
  onDone,
}: {
  node: NodeInfo | "new" | null;
  onClose: () => void;
  onDone: () => void;
}) {
  const toast = useToast();
  const existing = node && node !== "new" ? node : null;
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

  const set = (key: keyof typeof form) => (event: React.ChangeEvent<HTMLInputElement>) =>
    setForm((current) => ({ ...current, [key]: event.target.value }));

  return (
    <Dialog
      open={!!node}
      onClose={onClose}
      title={existing ? "Node bearbeiten" : "Node hinzufügen"}
      footer={
        <>
          <Button variant="ghost" onClick={onClose}>
            Abbrechen
          </Button>
          <Button
            variant="primary"
            loading={busy}
            disabled={!form.name.trim() || !form.url.trim() || (!existing && !form.token.trim())}
            onClick={async () => {
              setBusy(true);
              const ok = await toast.run(
                () => (existing ? api.patch(`/nodes/${existing.id}`, form) : api.post("/nodes", form)),
                existing ? "Gespeichert" : "Node hinzugefügt",
              );
              setBusy(false);
              if (ok) {
                onClose();
                onDone();
              }
            }}
          >
            {existing ? "Speichern" : "Hinzufügen"}
          </Button>
        </>
      }
    >
      <div>
        <label className="label">Name</label>
        <input className="field" value={form.name} onChange={set("name")} placeholder="home" maxLength={48} autoFocus />
      </div>
      <div>
        <label className="label">Adresse des Daemons</label>
        <input className="field font-mono text-xs" value={form.url} onChange={set("url")} placeholder="http://node.example.org:8480" />
      </div>
      <div>
        <label className="label">Token {existing && "(leer lassen, um es zu behalten)"}</label>
        <input className="field font-mono text-xs" type="password" value={form.token} onChange={set("token")} autoComplete="off" />
      </div>
      <div>
        <label className="label">Öffentliche Adresse (für Spieler)</label>
        <input className="field" value={form.publicHost} onChange={set("publicHost")} placeholder="mc.example.org" />
        <p className="mt-1 text-xs text-muted">Wird für die Beitritts-Adresse und die Voicechat-Konfiguration verwendet.</p>
      </div>
    </Dialog>
  );
}
