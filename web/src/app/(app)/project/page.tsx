"use client";

import Link from "next/link";
import { useSearchParams } from "next/navigation";
import { AlertTriangle, Network, Plus, Server as ServerIcon } from "lucide-react";
import { Suspense, useState } from "react";
import { Badge, Button, Card, EmptyState, PageHeader, Tabs } from "@/components/ui/primitives";
import { ErrorState, Loading } from "@/components/ui/async";
import { AddServerDialog } from "@/components/add-server";
import { GroupsTab } from "@/components/groups";
import { SchedulesTab } from "@/components/schedules";
import { EventsTab } from "@/components/events";
import { NetworkTab } from "@/components/project-network";
import { MembersTab } from "@/components/project-members";
import { ProjectSettingsTab } from "@/components/project-settings";
import { ChartCard, RangeTabs } from "@/components/server/stats";
import { SeriesChart } from "@/components/ui/chart";
import { PowerButtons, StateBadge } from "@/components/status";
import { ServerSortSelect, useServerSort } from "@/components/server-sort";
import { sortServers } from "@/lib/order";
import { useApi } from "@/lib/api";
import { megabytes } from "@/lib/format";
import { usePoll } from "@/lib/live";
import { useUrlState } from "@/lib/url-state";
import { can, type Metrics, type Project, type Range } from "@/lib/types";
import { formatNumber, t } from "@/lib/i18n";

export default function ProjectPage() {
  return (
    <Suspense fallback={<Loading />}>
      <ProjectView />
    </Suspense>
  );
}

type Tab = "servers" | "network" | "groups" | "schedules" | "events" | "members" | "settings";

function ProjectView() {
  const id = useSearchParams().get("id");
  // Zustände und Änderungen kommen über den Live-Kanal; das Intervall hält CPU und RAM der Zeilen grob aktuell
  const { data: project, error, mutate } = useApi<Project>(id ? `/projects/${id}` : null, { refreshInterval: usePoll(30000, 4000) });
  const [tab, setTab] = useUrlState<Tab>("tab", "servers", ["section"]);
  const [adding, setAdding] = useState(false);

  if (!id) return <ErrorState error={new Error(t("Kein Projekt angegeben. Wähle eins in der Übersicht."))} />;
  if (error) return <ErrorState error={error} retry={() => mutate()} />;
  if (!project) return <Loading />;

  const canSettings = can(project.myPermissions, "settings");
  const network = project.kind === "network";
  const canAdd = can(project.myPermissions, "servers") && (network || project.servers.length === 0);

  const tabs: { id: Tab; label: string; count?: number }[] = [
    { id: "servers", label: "Server", count: project.servers.length },
    ...(network ? [{ id: "network" as const, label: t("Netzwerk") }, { id: "groups" as const, label: t("Gruppen"), count: project.groups.length }] : []),
    { id: "schedules", label: t("Zeitpläne") },
    { id: "events", label: t("Ereignisse") },
    { id: "members", label: t("Mitglieder"), count: project.members?.length },
    ...(canSettings ? [{ id: "settings" as const, label: t("Einstellungen") }] : []),
  ];
  const active = tabs.some((entry) => entry.id === tab) ? tab : "servers";

  return (
    <>
      <PageHeader
        title={project.name}
        description={project.description || (network ? t("Netzwerk mit Velocity-Proxy") : t("Einzelner Server"))}
        action={
          canAdd && (
            <Button variant="primary" onClick={() => setAdding(true)}>
              <Plus className="h-4 w-4" />
              {network && !project.servers.some((server) => server.role === "proxy") ? t("Proxy hinzufügen") : t("Server hinzufügen")}
            </Button>
          )
        }
      />

      {project.problems && project.problems.length > 0 && project.servers.length > 0 && (
        <div className="mb-5 flex gap-3 rounded-lg border border-warn/25 bg-warn/10 px-4 py-3 text-xs text-warn">
          <AlertTriangle className="mt-0.5 h-4 w-4 shrink-0" />
          <ul className="space-y-0.5">
            {project.problems.map((problem) => (
              <li key={problem}>{problem}</li>
            ))}
          </ul>
        </div>
      )}

      <div className="mb-5">
        <Tabs value={active} onChange={setTab} items={tabs} />
      </div>

      {/* Neuer Schlüssel pro Tab: Der Inhalt blendet beim Wechsel kurz ein */}
      <div key={active} className="animate-fade-in">
        {active === "servers" && <ServersTab project={project} refresh={() => mutate()} onAdd={canAdd ? () => setAdding(true) : undefined} />}
        {active === "network" && network && <NetworkTab project={project} canManage={can(project.myPermissions, "network")} refresh={() => mutate()} />}
        {active === "groups" && network && <GroupsTab project={project} refresh={() => mutate()} />}
        {active === "schedules" && <SchedulesTab project={project} />}
        {active === "events" && <EventsTab project={project} />}
        {active === "members" && <MembersTab project={project} refresh={() => mutate()} />}
        {active === "settings" && canSettings && <ProjectSettingsTab project={project} refresh={() => mutate()} />}
      </div>

      <AddServerDialog project={project} open={adding} onClose={() => setAdding(false)} onCreated={() => mutate()} />
    </>
  );
}

// ---------------------------------------------------------------- Server

function ServersTab({ project, refresh, onAdd }: { project: Project; refresh: () => void; onAdd?: () => void }) {
  const [sort, setSort] = useServerSort(project.id);
  const { data: players } = useApi<Record<string, number>>(sort === "players" ? `/projects/${project.id}/live-players` : null, { refreshInterval: usePoll(120000, 15000) });
  if (project.servers.length === 0) {
    return (
      <Card>
        <EmptyState
          icon={project.kind === "network" ? <Network className="h-6 w-6" /> : <ServerIcon className="h-6 w-6" />}
          title={t("Noch kein Server")}
          text={project.kind === "network" ? t("Beginne mit dem Velocity-Proxy, danach kommen die Server dahinter.") : t("Lege den Server dieses Projekts an.")}
        />
        {onAdd && (
          <div className="flex justify-center pb-10">
            <Button variant="primary" onClick={onAdd}>
              <Plus className="h-4 w-4" />
              {project.kind === "network" ? t("Proxy hinzufügen") : t("Server anlegen")}
            </Button>
          </div>
        )}
      </Card>
    );
  }
  const canOperate = can(project.myPermissions, "power");
  const groupName = (id?: string) => project.groups.find((group) => group.id === id)?.name;
  return (
    <div className="space-y-3">
      <ProjectStats project={project} />
      {project.servers.length > 1 && (
        <div className="flex justify-end">
          <ServerSortSelect value={sort} onChange={setSort} />
        </div>
      )}
      {sortServers(project.servers, sort, players).map((server) => (
        <Card key={server.id} className="p-4">
          <div className="flex flex-wrap items-center gap-x-6 gap-y-3">
            <Link href={`/server/?id=${server.id}`} className="flex min-w-0 flex-1 basis-56 items-center gap-3">
              <span className="grid h-9 w-9 shrink-0 place-items-center rounded-lg border border-white/[0.08] bg-white/[0.04] text-muted">
                <ServerIcon className="h-4 w-4" />
              </span>
              <div className="min-w-0">
                <p className="truncate text-sm font-semibold hover:underline">{server.name}</p>
                <p className="truncate text-xs text-muted">
                  {server.software} {server.view?.version} · {server.view ? megabytes(server.view.memoryMB) : "–"} · {server.nodeName}
                </p>
              </div>
            </Link>
            <div className="flex flex-wrap items-center gap-2">
              {server.role === "proxy" && <Badge tone="accent">Proxy</Badge>}
              {server.role === "backend" && <Badge>{server.inTry ? t("Beitritt #{value}", { value: server.tryPosition + 1 }) : "Backend"}</Badge>}
              {server.groupId && <Badge tone="info">{t("Gruppe")} {groupName(server.groupId)}</Badge>}
              {server.voicechat && <Badge tone="info">{t("Voicechat")}</Badge>}
              <StateBadge server={server} />
            </div>
            <div className="min-w-0 basis-36 text-xs">
              {server.address ? <span className="font-mono text-ink/80">{server.address}</span> : <span className="text-muted">{server.role === "backend" ? t("nur im Netzwerk") : "–"}</span>}
              {server.view?.stats && server.view.state === "running" && (
                <p className="mt-0.5 text-muted">
                  {server.view.stats.cpuPercent.toFixed(0)} {t("% CPU ·")} {Math.round(server.view.stats.memoryBytes / 1024 ** 2)} MB
                </p>
              )}
            </div>
            <PowerButtons server={server} onDone={refresh} enabled={canOperate} />
          </div>
          {server.view?.lastError && server.view.state === "offline" && <p className="mt-3 rounded-md border border-bad/25 bg-bad/10 px-3 py-2 text-xs text-bad">{server.view.lastError}</p>}
        </Card>
      ))}
    </div>
  );
}

// ---------------------------------------------------------------- Spieler im Netzwerk

function ProjectStats({ project }: { project: Project }) {
  const [range, setRange] = useState<Range>("24h");
  const { data } = useApi<Metrics>(`/projects/${project.id}/metrics?range=${range}`, { refreshInterval: 30000 });
  if (!data) return null;
  const series = data.series;
  return (
    <ChartCard
      title={project.kind === "network" ? t("Spieler im Netzwerk") : t("Spieler")}
      description={series.samples ? t("Jetzt {players} · Spitze {peakPlayers} · Ø {average}", { players: data.players, peakPlayers: series.peakPlayers, average: formatNumber(series.avgPlayers, 1) }) : undefined}
      empty={series.samples === 0}
      action={<RangeTabs value={range} onChange={setRange} />}
    >
      <SeriesChart times={series.times} series={[{ name: t("Spieler"), color: "#f59e0b", values: series.players }]} height={170} long={range === "24h" || range === "7d"} format={(value) => String(Math.round(value))} />
    </ChartCard>
  );
}
