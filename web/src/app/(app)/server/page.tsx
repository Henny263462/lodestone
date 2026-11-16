"use client";

import { usePathname, useSearchParams } from "next/navigation";
import { Archive, BarChart3, FolderOpen, Puzzle, Settings, Terminal, Users, type LucideIcon } from "lucide-react";
import { Suspense } from "react";
import { useAppNavigation } from "@/components/layout/navigation";
import { Badge, PageHeader, PageTabs, type PageTab } from "@/components/ui/primitives";
import { BlockIcon } from "@/components/ui/mc";
import { softwareName } from "@/components/server-list";
import { ErrorState, Loading } from "@/components/ui/async";
import { CopyAddress, PowerButtons, StateBadge } from "@/components/status";
import { AddonsTab } from "@/components/server/addons";
import { BackupsTab } from "@/components/server/backups";
import { ConsoleTab } from "@/components/server/console";
import { FilesTab } from "@/components/server/files";
import { ModpackBanner } from "@/components/server/modpack";
import { PlayersTab } from "@/components/server/players";
import { SettingsTab } from "@/components/server/settings";
import { StatsTab } from "@/components/server/stats";
import { useApi } from "@/lib/api";
import { megabytes } from "@/lib/format";
import { roleRank, type Project, type Role, type Server } from "@/lib/types";

export default function ServerPage() {
  return (
    <Suspense fallback={null}>
      <ServerView />
    </Suspense>
  );
}

type Tab = "console" | "stats" | "files" | "addons" | "players" | "backups" | "settings";

interface Entry {
  id: Tab;
  label: string;
  icon: LucideIcon;
}

function parseServerRoute(pathname: string, searchParams: URLSearchParams): { id: string | null; tab: Tab } {
  const segments = pathname.split("/").filter(Boolean);
  let id: string | null = null;
  let tabPart: string | null = null;
  if (segments[0] === "server" && segments[1]) {
    id = segments[1];
    tabPart = segments[2] || null;
  }
  if (!id) {
    id = searchParams.get("id");
  }
  const tab = (tabPart as Tab) || (searchParams.get("tab") as Tab) || "console";
  return { id, tab };
}

function ServerView() {
  const navigation = useAppNavigation();
  const pathname = usePathname() ?? "";
  const params = useSearchParams();
  const { id, tab: routeTab } = parseServerRoute(pathname, params);
  const { data, error, mutate } = useApi<{ server: Server; myRole: Role }>(id ? `/servers/${id}` : null, { refreshInterval: 3000 });
  const { data: project } = useApi<Project>(data ? `/projects/${data.server.projectId}` : null);

  if (!id) return <ErrorState error={new Error("Kein Server angegeben")} />;
  if (error) return <ErrorState error={error} retry={() => mutate()} />;
  if (!data) return <Loading />;

  const { server, myRole } = data;
  const rank = roleRank[myRole];
  const view = server.view;
  const state = view?.state;
  const extensions = server.software === "fabric" || server.software === "neoforge" || server.software === "forge" ? "Mods" : server.software === "vanilla" ? null : "Plugins";

  const base = `/server/${server.id}/`;
  const entries: Entry[] = [
    { id: "console", label: "Konsole", icon: Terminal },
    { id: "stats", label: "Statistiken", icon: BarChart3 },
    ...(rank >= roleRank.operator ? [{ id: "files" as const, label: "Dateien", icon: FolderOpen }] : []),
    ...(extensions ? [{ id: "addons" as const, label: extensions, icon: Puzzle }] : []),
    { id: "players", label: "Spieler", icon: Users },
    { id: "backups", label: "Backups", icon: Archive },
    ...(rank >= roleRank.admin ? [{ id: "settings" as const, label: "Einstellungen", icon: Settings }] : []),
  ];
  const active = entries.some((entry) => entry.id === routeTab) ? routeTab : "console";
  const tabs: PageTab[] = entries.map((entry) => ({
    id: entry.id,
    label: entry.label,
    href: entry.id === "console" ? base : `${base}${entry.id}/`,
    icon: <entry.icon className="h-3.5 w-3.5" />,
  }));

  return (
    <>
      <PageHeader
        crumbs={[{ label: "Übersicht", href: "/" }, { label: project?.name ?? "Projekt", href: `/project/${server.projectId}/` }, { label: server.name }]}
        icon={<BlockIcon name={server.name} size={44} />}
        title={server.name}
        meta={
          <>
            <StateBadge server={server} />
            {server.role === "proxy" && <Badge tone="accent">Proxy</Badge>}
            <span>
              {softwareName(server.software)} {view?.version}
              {view && ` · ${megabytes(view.memoryMB)}`} · Node {server.nodeName}
            </span>
            {server.address && <CopyAddress address={server.address} />}
          </>
        }
        action={<PowerButtons server={server} onDone={() => mutate()} size="md" enabled={rank >= roleRank.operator} />}
      />

      <PageTabs value={active} items={tabs} />

      <ModpackBanner server={server} />
      {view?.lastError && state === "offline" && <p className="mb-5 rounded-lg border border-bad/25 bg-bad/10 px-4 py-3 text-xs text-bad">{view.lastError}</p>}
      {state === "installing" && <p className="mb-5 rounded-lg border border-info/25 bg-info/10 px-4 py-3 text-xs text-info">Die Server-Datei wird heruntergeladen. Das dauert einen Moment.</p>}

      {/* Neuer Schlüssel pro Bereich: Der Inhalt blendet beim Wechsel kurz ein */}
      <div key={active} className="animate-fade-in motion-reduce:animate-none">
        {active === "console" && <ConsoleTab serverId={server.id} canCommand={rank >= roleRank.operator} state={state} />}
        {active === "stats" && <StatsTab server={server} />}
        {active === "files" && <FilesTab serverId={server.id} canWrite={rank >= roleRank.operator} />}
        {active === "addons" && extensions && <AddonsTab serverId={server.id} canWrite={rank >= roleRank.operator} noun={extensions as "Plugins" | "Mods"} running={state === "running" || state === "starting"} />}
        {active === "players" && <PlayersTab server={server} canOperate={rank >= roleRank.operator} />}
        {active === "backups" && <BackupsTab serverId={server.id} canWrite={rank >= roleRank.operator} offline={state === "offline"} />}
        {active === "settings" && <SettingsTab server={server} onChanged={() => mutate()} onDeleted={() => navigation.replace(`/project/${server.projectId}/`)} />}
      </div>
    </>
  );
}
