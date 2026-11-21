"use client";

import { usePathname, useSearchParams } from "next/navigation";
import { Activity, AlertTriangle, Boxes, CalendarClock, Network, Plus, Server as ServerIcon, Settings, Users } from "lucide-react";
import { Suspense, useState } from "react";
import { Badge, Button, Card, EmptyState, PageHeader, PageTabs, type PageTab } from "@/components/ui/primitives";
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
import { PowerButtons } from "@/components/status";
import { BlockIcon } from "@/components/ui/mc";
import { RoleBadges, ServerRow } from "@/components/server-list";
import { ServerSortSelect, useServerSort } from "@/components/server-sort";
import { sortServers } from "@/lib/order";
import { useApi } from "@/lib/api";
import { usePoll } from "@/lib/live";
import { can, roleLabel, type Metrics, type Project, type Range } from "@/lib/types";
import { formatNumber, t, tp } from "@/lib/i18n";

export default function ProjectPage() {
  return (
    <Suspense fallback={<Loading />}>
      <ProjectView />
    </Suspense>
  );
}

type Tab = "servers" | "network" | "groups" | "schedules" | "events" | "members" | "settings";

/** Projekt-ID und Bereich aus dem sauberen Pfad (/project/<id>/<bereich>/) oder aus alten Suchparametern (?id=…&tab=…). */
function parseProjectRoute(pathname: string, searchParams: URLSearchParams): { id: string | null; tab: string | null } {
  const segments = pathname.split("/").filter(Boolean);
  let id: string | null = null;
  let tabPart: string | null = null;
  if (segments[0] === "project" && segments[1]) {
    id = segments[1];
    tabPart = segments[2] || null;
  }
  if (!id) {
    id = searchParams.get("id");
  }
  return { id, tab: tabPart || searchParams.get("tab") };
}

function ProjectView() {
  const pathname = usePathname() ?? "";
  const searchParams = useSearchParams();
  const { id, tab: routeTab } = parseProjectRoute(pathname, searchParams);
  // Zustände und Änderungen kommen über den Live-Kanal; das Intervall hält CPU und RAM der Zeilen grob aktuell
  const { data: project, error, mutate } = useApi<Project>(id ? `/projects/${id}` : null, { refreshInterval: usePoll(30000, 4000) });
  const [adding, setAdding] = useState(false);

  if (!id) return <ErrorState error={new Error(t("Kein Projekt angegeben. Wähle eins in der Übersicht."))} />;
  if (error) return <ErrorState error={error} retry={() => mutate()} />;
  if (!project) return <Loading />;

  const canSettings = can(project.myPermissions, "settings");
  const network = project.kind === "network";
  const canAdd = can(project.myPermissions, "servers") && (network || project.servers.length === 0);

  const base = `/project/${project.id}/`;
  const icon = (Icon: typeof ServerIcon) => <Icon className="h-3.5 w-3.5" />;
  const tabs: (PageTab & { id: Tab })[] = [
    { id: "servers", label: "Server", href: base, icon: icon(ServerIcon), count: project.servers.length },
    ...(network
      ? [
          { id: "network" as const, label: t("Netzwerk"), href: `${base}network/`, icon: icon(Network) },
          { id: "groups" as const, label: t("Gruppen"), href: `${base}groups/`, icon: icon(Boxes), count: project.groups.length },
        ]
      : []),
    { id: "schedules", label: t("Zeitpläne"), href: `${base}schedules/`, icon: icon(CalendarClock) },
    { id: "events", label: t("Ereignisse"), href: `${base}events/`, icon: icon(Activity) },
    { id: "members", label: t("Mitglieder"), href: `${base}members/`, icon: icon(Users), count: project.members?.length },
    ...(canSettings ? [{ id: "settings" as const, label: t("Einstellungen"), href: `${base}settings/`, icon: icon(Settings) }] : []),
  ];
  const active: Tab = tabs.find((entry) => entry.id === routeTab)?.id ?? "servers";
  const running = project.servers.filter((server) => server.view?.state === "running").length;

  return (
    <>
      <PageHeader
        crumbs={[{ label: t("Übersicht"), href: "/" }, { label: project.name }]}
        icon={<BlockIcon name={project.name} size={44} />}
        title={project.name}
        description={project.description || undefined}
        meta={
          <>
            <Badge tone={network ? "accent" : "neutral"}>{network ? t("Netzwerk mit Velocity") : t("Einzelner Server")}</Badge>
            {project.servers.length > 0 && <Badge tone={running > 0 ? "good" : "neutral"}>{tp("serversRunning", running, { total: project.servers.length })}</Badge>}
            <span>{t("Deine Rolle: {role}", { role: roleLabel()[project.myRole] })}</span>
          </>
        }
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

      <PageTabs value={active} items={tabs} />

      {/* Neuer Schlüssel pro Bereich: Der Inhalt blendet beim Wechsel kurz ein */}
      <div key={active} className="animate-fade-in motion-reduce:animate-none">
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
          text={
            project.kind === "network"
              ? t("Beginne mit dem Velocity-Proxy, danach kommen die Server dahinter.")
              : onAdd
                ? t("Lege den Server dieses Projekts an.")
                : t("Jemand mit dem Recht, Server anzulegen, muss den Server anlegen.")
          }
          action={
            onAdd && (
              <Button variant="primary" onClick={onAdd}>
                <Plus className="h-4 w-4" />
                {project.kind === "network" ? t("Proxy hinzufügen") : t("Server anlegen")}
              </Button>
            )
          }
        />
      </Card>
    );
  }
  const canOperate = can(project.myPermissions, "power");
  const groupName = (id?: string) => project.groups.find((group) => group.id === id)?.name;
  return (
    <div className="space-y-5">
      <Card className="overflow-hidden">
        {project.servers.length > 1 && (
          <div className="flex justify-end border-b border-white/[0.06] px-5 py-2.5">
            <ServerSortSelect value={sort} onChange={setSort} />
          </div>
        )}
        <div className="divide-y divide-white/[0.05]">
          {sortServers(project.servers, sort, players).map((server) => (
            <ServerRow
              key={server.id}
              server={server}
              badges={<RoleBadges server={server} groupName={groupName(server.groupId)} />}
              actions={<PowerButtons server={server} onDone={refresh} enabled={canOperate} />}
            />
          ))}
        </div>
      </Card>
      <ProjectStats project={project} />
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
