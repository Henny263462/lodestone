"use client";

import Link from "next/link";
import { useRouter, useSearchParams } from "next/navigation";
import { Archive, BarChart3, ChevronLeft, FolderOpen, Globe, Puzzle, Settings, SlidersHorizontal, Terminal, Users, type LucideIcon } from "lucide-react";
import { Suspense } from "react";
import { Badge, Meter, cx, loadTone } from "@/components/ui/primitives";
import { ErrorState, Loading } from "@/components/ui/async";
import { PowerButtons, StateBadge } from "@/components/status";
import { SecondarySidebar } from "@/components/layout/shell";
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
import { useUrlState } from "@/lib/url-state";
import { can, type Permission, type Project, type ServerAccess } from "@/lib/types";

export default function ServerPage() {
  return (
    <Suspense fallback={<Loading />}>
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

interface Group {
  label: string;
  entries: Entry[];
}

function ServerView() {
  const router = useRouter();
  const id = useSearchParams().get("id");
  // Zustandswechsel kommen über den Live-Kanal; solange der Server läuft, halten kürzere Abfragen CPU und RAM frisch
  const poll = usePoll(60000, 3000);
  const { data, error, mutate } = useApi<ServerAccess>(id ? `/servers/${id}` : null, {
    refreshInterval: (latest) => (latest?.server.view?.state === "running" ? Math.min(poll, 10000) : poll),
  });
  const { data: project } = useApi<Project>(data ? `/projects/${data.server.projectId}` : null);
  const [tab, setTab] = useUrlState<Tab>("tab", "console", ["section"]);

  if (!id) return <ErrorState error={new Error("Kein Server angegeben. Öffne ihn über sein Projekt.")} />;
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

  // Bereiche in drei Gruppen: was gerade passiert, was auf dem Server liegt, wie er eingestellt ist
  const allGroups: Group[] = [
    {
      label: "Betrieb",
      entries: [
        ...only(allowed("console.read"), { id: "console", label: "Konsole", icon: Terminal }),
        { id: "stats", label: "Statistiken", icon: BarChart3 },
        { id: "players", label: "Spieler", icon: Users },
      ],
    },
    {
      label: "Inhalte",
      entries: [
        ...only(allowed("files.read"), { id: "files", label: "Dateien", icon: FolderOpen }),
        ...only(!!extensions, { id: "addons", label: extensions ?? "", icon: Puzzle }),
        ...only(allowed("files.read") && hasWorlds, { id: "worlds", label: "Welten", icon: Globe }),
        ...only(allowed("backups.read"), { id: "backups", label: "Backups", icon: Archive }),
      ],
    },
    {
      label: "Konfiguration",
      entries: [
        ...only(allowed("files.read") && hasWorlds, { id: "properties", label: "Spieleinstellungen", icon: SlidersHorizontal }),
        ...only(allowed("settings") || allowed("servers"), { id: "settings", label: "Einstellungen", icon: Settings }),
      ],
    },
  ];
  const groups = allGroups.filter((group) => group.entries.length > 0);
  const entries = groups.flatMap((group) => group.entries);
  const active = entries.some((entry) => entry.id === tab) ? tab : entries[0].id;
  const stats = view?.stats;
  const memoryPercent = stats && stats.memoryLimit > 0 ? (stats.memoryBytes / stats.memoryLimit) * 100 : 0;

  return (
    <>
      <SecondarySidebar>
        <div className="flex flex-col gap-4 p-4 lg:min-h-full lg:p-5">
          <Link href={`/project/?id=${server.projectId}`} className="inline-flex items-center gap-1 text-xs text-muted transition hover:text-ink">
            <ChevronLeft className="h-3.5 w-3.5" />
            {project?.name ?? "Projekt"}
          </Link>

          <div className="min-w-0">
            <p className="truncate text-[15px] font-semibold tracking-tight">{server.name}</p>
            <p className="mt-0.5 truncate text-xs text-muted">
              {server.software} {view?.version}
            </p>
            <div className="mt-2.5 flex flex-wrap items-center gap-1.5">
              <StateBadge server={server} />
              {server.role === "proxy" && <Badge tone="accent">Proxy</Badge>}
              {server.role === "backend" && <Badge>Backend</Badge>}
            </div>
          </div>

          {/* Kleine Bildschirme: eine Auswahl statt einer Leiste, die seitlich aus dem Bild läuft */}
          <select className="field lg:hidden" aria-label="Bereich des Servers" value={active} onChange={(event) => setTab(event.target.value as Tab)}>
            {groups.map((group) => (
              <optgroup key={group.label} label={group.label}>
                {group.entries.map((entry) => (
                  <option key={entry.id} value={entry.id}>
                    {entry.label}
                  </option>
                ))}
              </optgroup>
            ))}
          </select>

          <nav aria-label="Bereiche des Servers" className="hidden lg:block">
            {groups.map((group, index) => (
              <div key={group.label} className={cx(index > 0 && "mt-4")}>
                <p className="px-3 pb-1 text-[10.5px] font-medium uppercase tracking-wider text-muted/70">{group.label}</p>
                <div className="space-y-0.5">
                  {group.entries.map((entry) => (
                    <button
                      key={entry.id}
                      type="button"
                      aria-current={active === entry.id ? "page" : undefined}
                      onClick={() => setTab(entry.id)}
                      className={cx(
                        "flex w-full items-center gap-3 whitespace-nowrap rounded-lg border px-3 py-2 text-left text-[13px] transition duration-150 active:scale-[0.97]",
                        active === entry.id ? "border-white/[0.07] bg-white/[0.07] text-ink" : "border-transparent text-muted hover:bg-white/[0.04] hover:text-ink",
                      )}
                    >
                      <entry.icon className={cx("h-4 w-4 shrink-0", active === entry.id ? "text-ink" : "text-muted")} />
                      {entry.label}
                    </button>
                  ))}
                </div>
              </div>
            ))}
          </nav>

          {state === "running" && stats && (
            <div className="mt-auto hidden space-y-3 rounded-lg border border-white/[0.06] bg-white/[0.02] p-3 lg:block">
              <div>
                <div className="mb-1.5 flex justify-between text-[11px] text-muted">
                  <span>CPU</span>
                  <span className="tabular-nums text-ink/80">{stats.cpuPercent.toFixed(0)} %</span>
                </div>
                <Meter value={stats.cpuPercent} max={100} tone={loadTone(stats.cpuPercent)} />
              </div>
              <div>
                <div className="mb-1.5 flex justify-between text-[11px] text-muted">
                  <span>Arbeitsspeicher</span>
                  <span className="tabular-nums text-ink/80">{Math.round(stats.memoryBytes / 1024 ** 2)} MB</span>
                </div>
                <Meter value={memoryPercent} max={100} tone={loadTone(memoryPercent)} />
              </div>
            </div>
          )}
        </div>
      </SecondarySidebar>

      <div className="mb-6 flex flex-wrap items-center justify-between gap-4">
        <div className="min-w-0">
          <h1 className="truncate text-xl font-semibold tracking-tight">{entries.find((entry) => entry.id === active)?.label}</h1>
          <p className="mt-1 break-words text-[13px] text-muted">
            {server.name} · {view ? megabytes(view.memoryMB) : "–"} · Node {server.nodeName}
            {server.address && (
              <>
                {" · "}
                <span className="font-mono text-ink/80">{server.address}</span>
              </>
            )}
          </p>
        </div>
        <PowerButtons server={server} onDone={() => mutate()} size="md" enabled={allowed("power")} />
      </div>

      <ModpackBanner server={server} />
      {view?.lastError && state === "offline" && <p className="mb-5 rounded-lg border border-bad/25 bg-bad/10 px-4 py-3 text-xs text-bad">{view.lastError}</p>}
      {state === "installing" && <p className="mb-5 rounded-lg border border-info/25 bg-info/10 px-4 py-3 text-xs text-info">Die Server-Datei wird heruntergeladen. Das dauert einen Moment.</p>}

      {/* Neuer Schlüssel pro Bereich: Der Inhalt blendet beim Wechsel kurz ein */}
      <div key={active} className="animate-fade-in">
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
          <SettingsTab server={server} permissions={myPermissions} onChanged={() => mutate()} onDeleted={() => router.replace(`/project/?id=${server.projectId}`)} />
        )}
      </div>
    </>
  );
}
