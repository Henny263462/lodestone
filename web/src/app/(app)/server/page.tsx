"use client";

import Link from "next/link";
import { useRouter, useSearchParams } from "next/navigation";
import { Archive, BarChart3, FolderOpen, Puzzle, Settings, Terminal, Users, type LucideIcon } from "lucide-react";
import { Suspense, useEffect, useState } from "react";
import { cx } from "@/components/ui/primitives";
import { ErrorState, Loading } from "@/components/ui/async";
import { PowerButtons, StateBadge } from "@/components/status";
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

function ServerView() {
  const router = useRouter();
  const params = useSearchParams();
  const id = params.get("id");
  const tabParam = params.get("tab") as Tab | null;
  const { data, error, mutate } = useApi<{ server: Server; myRole: Role }>(id ? `/servers/${id}` : null, { refreshInterval: 3000 });
  const { data: project } = useApi<Project>(data ? `/projects/${data.server.projectId}` : null);
  const [tab, setTabState] = useState<Tab>(tabParam || "console");

  useEffect(() => {
    if (tabParam && tabParam !== tab) {
      setTabState(tabParam);
    }
  }, [tabParam, tab]);

  function setTab(next: Tab) {
    setTabState(next);
    // Der Bereich steht in der Adresse, damit sich ein Link oder Neuladen merkt, wo man war
    const url = new URL(window.location.href);
    url.searchParams.set("tab", next);
    window.history.replaceState(null, "", url);
  }

  if (!id) return <ErrorState error={new Error("Kein Server angegeben")} />;
  if (error) return <ErrorState error={error} retry={() => mutate()} />;
  if (!data) return <Loading />;

  const { server, myRole } = data;
  const rank = roleRank[myRole];
  const view = server.view;
  const state = view?.state;
  const extensions = server.software === "fabric" || server.software === "neoforge" ? "Mods" : server.software === "vanilla" ? null : "Plugins";

  const entries: Entry[] = [
    { id: "console", label: "Konsole", icon: Terminal },
    { id: "stats", label: "Statistiken", icon: BarChart3 },
    ...(rank >= roleRank.operator ? [{ id: "files" as const, label: "Dateien", icon: FolderOpen }] : []),
    ...(extensions ? [{ id: "addons" as const, label: extensions, icon: Puzzle }] : []),
    { id: "players", label: "Spieler", icon: Users },
    { id: "backups", label: "Backups", icon: Archive },
    ...(rank >= roleRank.admin ? [{ id: "settings" as const, label: "Einstellungen", icon: Settings }] : []),
  ];
  const active = entries.some((entry) => entry.id === tab) ? tab : "console";

  return (
    <>
      {/* Auf Mobilgeräten Reiterleiste oben anzeigen */}
      <div className="mb-5 flex gap-1 overflow-x-auto pb-2 border-b border-white/[0.06] lg:hidden">
        {entries.map((entry) => (
          <button
            key={entry.id}
            onClick={() => setTab(entry.id)}
            className={cx(
              "flex shrink-0 items-center gap-2 whitespace-nowrap rounded-lg border px-3 py-1.5 text-left text-[12.5px] transition duration-100 active:scale-[0.97]",
              active === entry.id ? "border-white/[0.07] bg-white/[0.07] text-ink font-medium" : "border-transparent text-muted hover:bg-white/[0.04] hover:text-ink",
            )}
          >
            <entry.icon className={cx("h-3.5 w-3.5 shrink-0", active === entry.id ? "text-ink" : "text-muted")} />
            {entry.label}
          </button>
        ))}
      </div>

      <div className="mb-6 flex flex-wrap items-center justify-between gap-4">
        <div className="min-w-0">
          <h1 className="truncate text-xl font-semibold tracking-tight">{entries.find((entry) => entry.id === active)?.label}</h1>
          <p className="mt-1 text-[13px] text-muted">
            {server.name} · {view ? megabytes(view.memoryMB) : "–"} · Node {server.nodeName}
            {server.address && (
              <>
                {" · "}
                <span className="font-mono text-ink/80">{server.address}</span>
              </>
            )}
          </p>
        </div>
        <PowerButtons server={server} onDone={() => mutate()} size="md" enabled={rank >= roleRank.operator} />
      </div>

      <ModpackBanner server={server} />
      {view?.lastError && state === "offline" && <p className="mb-5 rounded-lg border border-bad/25 bg-bad/10 px-4 py-3 text-xs text-bad">{view.lastError}</p>}
      {state === "installing" && <p className="mb-5 rounded-lg border border-info/25 bg-info/10 px-4 py-3 text-xs text-info">Die Server-Datei wird heruntergeladen. Das dauert einen Moment.</p>}

      {active === "console" && <ConsoleTab serverId={server.id} canCommand={rank >= roleRank.operator} state={state} />}
      {active === "stats" && <StatsTab server={server} />}
      {active === "files" && <FilesTab serverId={server.id} canWrite={rank >= roleRank.operator} />}
      {active === "addons" && extensions && <AddonsTab serverId={server.id} canWrite={rank >= roleRank.operator} noun={extensions as "Plugins" | "Mods"} running={state === "running" || state === "starting"} />}
      {active === "players" && <PlayersTab server={server} canOperate={rank >= roleRank.operator} />}
      {active === "backups" && <BackupsTab serverId={server.id} canWrite={rank >= roleRank.operator} offline={state === "offline"} />}
      {active === "settings" && <SettingsTab server={server} onChanged={() => mutate()} onDeleted={() => router.replace(`/project/?id=${server.projectId}`)} />}
    </>
  );
}
