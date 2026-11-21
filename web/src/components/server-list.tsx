"use client";

import Link from "next/link";
import type { ReactNode } from "react";
import { Badge, cx } from "@/components/ui/primitives";
import { BlockIcon, SignalBars } from "@/components/ui/mc";
import { CopyAddress, StateBadge } from "@/components/status";
import { megabytes } from "@/lib/format";
import type { Server } from "@/lib/types";
import { formatNumber, t } from "@/lib/i18n";

/** Leuchtende Balken je Zustand: läuft = alle, startet = einige, aus = keine. */
function signal(server: Server): { level: number; tone: string } {
  if (!server.online) return { level: 0, tone: "text-bad" };
  switch (server.view?.state) {
    case "running":
      return { level: 5, tone: "text-good" };
    case "starting":
    case "restoring":
      return { level: 3, tone: "text-warn" };
    case "installing":
    case "stopping":
      return { level: 2, tone: "text-warn" };
    default:
      return { level: 0, tone: "text-muted" };
  }
}

/** Name der Software für Menschen („paper“ → „Paper“). */
export function softwareName(software: string): string {
  const names: Record<string, string> = { neoforge: "NeoForge", velocity: "Velocity", vanilla: "Vanilla" };
  return names[software] ?? software.charAt(0).toUpperCase() + software.slice(1);
}

/**
 * Eine Zeile im Stil der Serverliste aus dem Mehrspielermenü: Blocksymbol, Name, Software, Ping-Balken und Adresse.
 * `actions` erscheint rechts (z. B. Start/Stopp); `badges` hinter dem Namen (Proxy, Gruppe …).
 */
export function ServerRow({ server, badges, actions, dense }: { server: Server; badges?: ReactNode; actions?: ReactNode; dense?: boolean }) {
  const bars = signal(server);
  const stats = server.view?.state === "running" ? server.view.stats : undefined;
  return (
    <div className={cx("flex flex-wrap items-center gap-x-5 gap-y-3", dense ? "px-4 py-3" : "px-5 py-4")}>
      <Link href={`/server/${server.id}/`} className="group flex min-w-[220px] flex-1 items-center gap-3.5">
        <BlockIcon name={server.name} size={dense ? 32 : 40} />
        <div className="min-w-0">
          <div className="flex min-w-0 flex-wrap items-center gap-2">
            <p className="truncate font-display text-[15px] tracking-wide group-hover:underline">{server.name}</p>
            {badges}
          </div>
          <p className="mt-0.5 truncate text-xs text-muted">
            {softwareName(server.software)} {server.view?.version}
            {server.view && ` · ${megabytes(server.view.memoryMB)}`}
            {` · ${server.nodeName}`}
          </p>
        </div>
      </Link>
      <div className="flex min-w-[150px] flex-col items-start gap-1 text-xs">
        {server.address ? <CopyAddress address={server.address} /> : <span className="text-muted">{server.role === "backend" ? t("Nur im Netzwerk erreichbar") : t("Keine Adresse")}</span>}
        {stats && (
          <span className="tabular-nums text-muted">
            {t("{cpu} % CPU · {memory} MB", { cpu: formatNumber(stats.cpuPercent, 0), memory: formatNumber(Math.round(stats.memoryBytes / 1024 ** 2)) })}
          </span>
        )}
      </div>
      <div className="flex items-center gap-2.5">
        <SignalBars level={bars.level} className={bars.tone} />
        <StateBadge server={server} />
      </div>
      {actions}
      {server.view?.lastError && server.view.state === "offline" && (
        <p className="w-full rounded-md border border-bad/25 bg-bad/10 px-3 py-2 text-xs text-bad">{server.view.lastError}</p>
      )}
    </div>
  );
}

/** Abzeichen für die Rolle eines Servers im Netzwerk. */
export function RoleBadges({ server, groupName }: { server: Server; groupName?: string }) {
  return (
    <>
      {server.role === "proxy" && <Badge tone="accent">Proxy</Badge>}
      {server.role === "backend" && <Badge>{server.inTry ? t("Beitritt #{value}", { value: server.tryPosition + 1 }) : "Backend"}</Badge>}
      {groupName && <Badge tone="info">{t("Gruppe {name}", { name: groupName })}</Badge>}
      {server.voicechat && <Badge tone="info">{t("Voicechat")}</Badge>}
    </>
  );
}
