"use client";

import Link from "next/link";
import { useRouter } from "next/navigation";
import { Bell, Cpu, Plus } from "lucide-react";
import { Badge, Button, Card, CardHeader, Dot, EmptyState, Meter, loadTone } from "@/components/ui/primitives";
import { ErrorState, Loading } from "@/components/ui/async";
import { EventIcon, eventLabels } from "@/components/events";
import { useApi, type ApiError } from "@/lib/api";
import { ago, bytes, dateTime } from "@/lib/format";
import { usePoll } from "@/lib/live";
import type { NodeInfo, Project, ProjectEvent } from "@/lib/types";
import { t } from "@/lib/i18n";

/** Kurzer Zustand aller Nodes für die Übersicht: erreichbar, Arbeitsspeicher, Anzahl Server. */
export function NodesOverviewCard({ nodes, error, retry, isAdmin }: { nodes?: NodeInfo[]; error?: ApiError; retry: () => void; isAdmin: boolean }) {
  const router = useRouter();
  return (
    <Card>
      <CardHeader
        title={t("Nodes")}
        description={t("Maschinen, auf denen die Server laufen.")}
        action={
          <Link href="/nodes/" className="text-xs text-accent hover:underline">
            {t("Alle Nodes")}
          </Link>
        }
      />
      {error ? (
        <ErrorState inline error={error} retry={retry} />
      ) : !nodes ? (
        <Loading />
      ) : nodes.length === 0 ? (
        <div className="pb-8">
          <EmptyState icon={<Cpu className="h-6 w-6" />} title={t("Noch kein Node")} text={isAdmin ? t("Richte zuerst eine Maschine ein, dann kannst du Server anlegen.") : t("Ein Administrator muss einen Node einrichten.")} />
          {isAdmin && (
            <div className="flex justify-center">
              <Button variant="primary" onClick={() => router.push("/nodes/?add=1")}>
                <Plus className="h-4 w-4" />
                {t("Node hinzufügen")}
              </Button>
            </div>
          )}
        </div>
      ) : (
        <div className="divide-y divide-white/[0.05]">
          {nodes.map((node) => {
            const total = node.system?.memoryBytes ?? 0;
            const used = total && node.memoryAvailable !== undefined ? total - node.memoryAvailable : 0;
            const percent = total ? (used / total) * 100 : 0;
            return (
              <div key={node.id} className="space-y-2 px-5 py-3">
                <div className="flex items-center gap-3">
                  <Dot tone={node.online ? "good" : "bad"} />
                  <p className="min-w-0 flex-1 truncate text-sm font-medium">{node.name}</p>
                  {node.online ? <span className="text-xs text-muted tabular-nums">{node.serverCount} Server</span> : <Badge tone="bad">{t("Nicht erreichbar")}</Badge>}
                </div>
                {node.online && total > 0 && (
                  <div>
                    <div className="mb-1 flex justify-between text-[11px] text-muted">
                      <span>{t("Arbeitsspeicher")}</span>
                      <span className="tabular-nums text-ink/80">
                        {bytes(used)} {t("von")} {bytes(total)}
                      </span>
                    </div>
                    <Meter value={percent} max={100} tone={loadTone(percent)} />
                  </div>
                )}
              </div>
            );
          })}
        </div>
      )}
    </Card>
  );
}

/** Die neuesten Ereignisse aus allen eigenen Projekten. */
export function RecentEventsCard({ projects }: { projects: Project[] }) {
  // Neue Ereignisse meldet der Live-Kanal
  const { data, error, mutate } = useApi<ProjectEvent[]>("/events/recent?limit=8", { refreshInterval: usePoll(120000, 15000) });
  const projectName = new Map(projects.map((project) => [project.id, project.name]));
  const serverName = new Map(projects.flatMap((project) => project.servers.map((server) => [server.id, server.name] as const)));
  return (
    <Card>
      <CardHeader title={t("Letzte Ereignisse")} description={t("Abstürze, Backups und Meldungen aus deinen Projekten.")} />
      {error ? (
        <ErrorState inline error={error} retry={() => mutate()} />
      ) : !data ? (
        <Loading />
      ) : data.length === 0 ? (
        <EmptyState icon={<Bell className="h-6 w-6" />} title={t("Noch nichts passiert")} text={t("Hier erscheinen Abstürze, Neustarts, Backups und Ausfälle, sobald es welche gibt.")} />
      ) : (
        <div className="divide-y divide-white/[0.05]">
          {data.map((event) => (
            <div key={event.id} className="flex items-start gap-3 px-5 py-3">
              <EventIcon level={event.level} />
              <div className="min-w-0 flex-1">
                <p className="line-clamp-2 break-words text-[13px]">{event.message}</p>
                <p className="mt-0.5 truncate text-xs text-muted">
                  <span title={dateTime(event.time)}>{ago(event.time)}</span> · {eventLabels()[event.kind] ?? event.kind} ·{" "}
                  <Link
                    href={event.serverId && serverName.has(event.serverId) ? `/server/${event.serverId}/` : `/project/${event.projectId}/events/`}
                    className="hover:text-ink hover:underline"
                  >
                    {(event.serverId && serverName.get(event.serverId)) || projectName.get(event.projectId) || t("Projekt")}
                  </Link>
                </p>
              </div>
            </div>
          ))}
        </div>
      )}
    </Card>
  );
}
