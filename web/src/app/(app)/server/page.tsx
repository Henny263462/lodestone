"use client";

import { usePathname, useSearchParams } from "next/navigation";
import { Archive, BarChart3, FolderOpen, Globe, Puzzle, Settings, SlidersHorizontal, Terminal, Users, type LucideIcon } from "lucide-react";
import { Suspense } from "react";
import { useAppNavigation } from "@/components/layout/navigation";
import { Badge, PageHeader, PageTabs, type PageTab } from "@/components/ui/primitives";
import { BlockIcon } from "@/components/ui/mc";
import { softwareName } from "@/components/server-list";
import { ErrorState, Loading } from "@/components/ui/async";
import { CopyAddress, PowerButtons, StateBadge } from "@/components/status";
import { AddonsTab } from "@/components/server/addons";
import { BlocksTab } from "@/components/server/blocks";
import { BackupsTab } from "@/components/server/backups";
import { PropertiesTab } from "@/components/server/properties";
import { WorldsTab } from "@/components/server/worlds";
import { ConsoleTab } from "@/components/server/console";
import { FilesTab } from "@/components/server/files";
import { SftpCard } from "@/components/server/sftp";
import { ModpackBanner } from "@/components/server/modpack";
import { PlayersTab } from "@/components/server/players";
import { SettingsTab } from "@/components/server/settings";
import { StatsTab } from "@/components/server/stats";
import { useApi } from "@/lib/api";
import { megabytes } from "@/lib/format";
import { usePoll } from "@/lib/live";
import { can, type Permission, type Project, type ServerAccess } from "@/lib/types";
import { t } from "@/lib/i18n";

export default function ServerPage() {
  return (
    <Suspense fallback={null}>
      <ServerView />
    </Suspense>
  );
}

type Tab = "console" | "stats" | "files" | "addons" | "players" | "backups" | "worlds" | "properties" | "settings";

interface Entry {
  id: Tab;
  label: string;
  icon: LucideIcon;
}

/** Server-ID und Bereich aus dem sauberen Pfad (/server/<id>/<bereich>/) oder aus alten Suchparametern (?id=…&tab=…). */
function parseServerRoute(pathname: string, searchParams: URLSearchParams): { id: string | null; tab: string | null } {
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
  return { id, tab: tabPart || searchParams.get("tab") };
}

function ServerView() {
  const navigation = useAppNavigation();
  const pathname = usePathname() ?? "";
  const params = useSearchParams();
  const { id, tab: routeTab } = parseServerRoute(pathname, params);
  // Zustandswechsel kommen über den Live-Kanal; solange der Server läuft, halten kürzere Abfragen CPU und RAM frisch
  const poll = usePoll(60000, 3000);
  const { data, error, mutate } = useApi<ServerAccess>(id ? `/servers/${id}` : null, {
    refreshInterval: (latest) => (latest?.server.view?.state === "running" ? Math.min(poll, 10000) : poll),
  });
  const { data: project } = useApi<Project>(data ? `/projects/${data.server.projectId}` : null);

  if (!id) return <ErrorState error={new Error(t("Kein Server angegeben. Öffne ihn über sein Projekt."))} />;
  if (error) return <ErrorState error={error} retry={() => mutate()} />;
  if (!data) return <Loading />;

  const { server, myPermissions } = data;
  const allowed = (...needed: Permission[]) => can(myPermissions, ...needed);
  const view = server.view;
  const state = view?.state;
  const extensions = server.software === "fabric" || server.software === "neoforge" || server.software === "forge" ? "Mods" : server.software === "vanilla" ? null : "Plugins";
  // Welten und server.properties gibt es nur bei Minecraft-Servern, nicht beim Velocity-Proxy
  const hasWorlds = server.role !== "proxy" && server.software !== "velocity";
  const only = (condition: boolean, entry: Entry) => (condition ? [entry] : []);

  // Bereiche nach Rechten, in der Reihenfolge: was gerade passiert, was auf dem Server liegt, wie er eingestellt ist
  const base = `/server/${server.id}/`;
  const entries: Entry[] = [
    ...only(allowed("console.read"), { id: "console", label: t("Konsole"), icon: Terminal }),
    { id: "stats", label: t("Statistiken"), icon: BarChart3 },
    { id: "players", label: t("Spieler"), icon: Users },
    ...only(allowed("files.read"), { id: "files", label: t("Dateien"), icon: FolderOpen }),
    ...only(!!extensions, { id: "addons", label: extensions ?? "", icon: Puzzle }),
    ...only(allowed("files.read") && hasWorlds, { id: "worlds", label: t("Welten"), icon: Globe }),
    ...only(allowed("backups.read"), { id: "backups", label: "Backups", icon: Archive }),
    ...only(allowed("files.read") && hasWorlds, { id: "properties", label: t("Spieleinstellungen"), icon: SlidersHorizontal }),
    ...only(allowed("settings") || allowed("servers"), { id: "settings", label: t("Einstellungen"), icon: Settings }),
  ];
  const active: Tab = entries.find((entry) => entry.id === routeTab)?.id ?? entries[0].id;
  // Die Konsole liegt direkt unter /server/<id>/, die anderen Bereiche haben einen eigenen Pfad
  const tabs: PageTab[] = entries.map((entry) => ({
    id: entry.id,
    label: entry.label,
    href: entry.id === "console" ? base : `${base}${entry.id}/`,
    icon: <entry.icon className="h-3.5 w-3.5" />,
  }));

  return (
    <>
      <PageHeader
        crumbs={[{ label: t("Übersicht"), href: "/" }, { label: project?.name ?? t("Projekt"), href: `/project/${server.projectId}/` }, { label: server.name }]}
        icon={<BlockIcon name={server.name} size={44} />}
        title={server.name}
        meta={
          <>
            <StateBadge server={server} />
            {server.role === "proxy" && <Badge tone="accent">Proxy</Badge>}
            {server.role === "backend" && <Badge>Backend</Badge>}
            <span className="min-w-0 break-words">
              {softwareName(server.software)} {view?.version}
              {view && ` · ${megabytes(view.memoryMB)}`} {t("· Node")} {server.nodeName}
            </span>
            {server.address && <CopyAddress address={server.address} />}
          </>
        }
        action={<PowerButtons server={server} onDone={() => mutate()} size="md" enabled={allowed("power")} />}
      />

      <PageTabs value={active} items={tabs} />

      <ModpackBanner server={server} />
      {view?.lastError && state === "offline" && <p className="mb-5 rounded-lg border border-bad/25 bg-bad/10 px-4 py-3 text-xs text-bad">{view.lastError}</p>}
      {state === "installing" && <p className="mb-5 rounded-lg border border-info/25 bg-info/10 px-4 py-3 text-xs text-info">{t("Die Server-Datei wird heruntergeladen. Das dauert einen Moment.")}</p>}

      {/* Neuer Schlüssel pro Bereich: Der Inhalt blendet beim Wechsel kurz ein */}
      <div key={active} className="animate-fade-in motion-reduce:animate-none">
        {active === "console" && <ConsoleTab serverId={server.id} canCommand={allowed("console.command")} state={state} />}
        {active === "stats" && <StatsTab server={server} />}
        {active === "files" && (
          <div className="space-y-5">
            <FilesTab serverId={server.id} canWrite={allowed("files.write")} />
            {allowed("sftp", "files.read") && <SftpCard serverId={server.id} />}
          </div>
        )}
        {active === "addons" && extensions && (
          <AddonsTab
            serverId={server.id}
            canWrite={allowed("addons")}
            canFiles={allowed("files.write")}
            noun={extensions as "Plugins" | "Mods"}
            running={state === "running" || state === "starting"}
            blocks={<BlocksTab server={server} canManage={allowed("settings", "addons")} offline={state === "offline"} />}
          />
        )}
        {active === "players" && <PlayersTab server={server} canOperate={allowed("players")} />}
        {active === "backups" && <BackupsTab serverId={server.id} permissions={myPermissions} offline={state === "offline"} />}
        {active === "worlds" && <WorldsTab server={server} canManage={allowed("settings", "files.write")} canOperate={allowed("files.write")} />}
        {active === "properties" && <PropertiesTab server={server} canWrite={allowed("settings")} />}
        {active === "settings" && (
          <SettingsTab server={server} permissions={myPermissions} onChanged={() => mutate()} onDeleted={() => navigation.replace(`/project/${server.projectId}/`)} />
        )}
      </div>
    </>
  );
}
