"use client";

import { Clock, Gauge, Users } from "lucide-react";
import { useState } from "react";
import { Badge, Card, CardHeader, EmptyState, StatCard } from "@/components/ui/primitives";
import { SeriesChart } from "@/components/ui/chart";
import { ErrorState, Loading } from "@/components/ui/async";
import { ChartCard, RangeTabs } from "./stats";
import { PlayerAdmin, PlayerMenu } from "./player-admin";
import { useApi } from "@/lib/api";
import { usePoll } from "@/lib/live";
import { ago } from "@/lib/format";
import type { Metrics, PlayerInfo, Range, SeenPlayer, Server } from "@/lib/types";
import { formatNumber, t } from "@/lib/i18n";

function playtime(seconds: number): string {
  if (seconds < 60) return t("<1 Min");
  const minutes = Math.floor(seconds / 60);
  const hours = Math.floor(minutes / 60);
  if (hours >= 24) return t("{floor} T {value} Std", { floor: Math.floor(hours / 24), value: hours % 24 });
  if (hours > 0) return t("{hours} Std {value} Min", { hours, value: minutes % 60 });
  return t("{minutes} Min", { minutes });
}

function Head({ name, size = 28 }: { name: string; size?: number }) {
  return (
    // eslint-disable-next-line @next/next/no-img-element
    <img src={`https://mc-heads.net/avatar/${encodeURIComponent(name)}/${size * 2}`} alt="" width={size} height={size} className="rounded-md bg-raised [image-rendering:pixelated]" loading="lazy" />
  );
}

export function PlayersTab({ server, canOperate = false }: { server: Server; canOperate?: boolean }) {
  const running = server.view?.state === "running";
  const [range, setRange] = useState<Range>("24h");
  // Beitritte und Abgänge meldet der Live-Kanal sofort; das Intervall ist der Rückfall
  const live = useApi<PlayerInfo>(running ? `/servers/${server.id}/players` : null, { refreshInterval: usePoll(60000, 10000) });
  const metrics = useApi<Metrics>(`/servers/${server.id}/metrics?range=${range}`, { refreshInterval: 30000 });
  const history = useApi<{ players: SeenPlayer[]; online: number; max: number }>(`/servers/${server.id}/player-history`, { refreshInterval: 30000 });

  if (metrics.error) return <ErrorState error={metrics.error} retry={() => metrics.mutate()} />;
  if (!metrics.data) return <Loading />;

  const series = metrics.data.series;
  const info = live.data;
  const isProxy = server.role === "proxy";
  const seen = history.data?.players ?? [];
  const totalSeconds = seen.reduce((sum, player) => sum + player.seconds, 0);

  return (
    <div className="space-y-5">
      <div className="grid gap-4 sm:grid-cols-2 xl:grid-cols-4">
        <StatCard label={t("Jetzt online")} value={running ? `${info?.online ?? metrics.data.players}${info?.max ? ` / ${info.max}` : ""}` : "–"} icon={<Users className="h-4 w-4" />} tone={running ? "green" : "neutral"} hint={running ? undefined : t("Der Server läuft nicht")} />
        <StatCard label={t("Spitze im Zeitraum")} value={series.samples ? series.peakPlayers : "–"} icon={<Users className="h-4 w-4" />} tone="amber" hint={series.samples ? t("Ø {average} gleichzeitig", { average: formatNumber(series.avgPlayers, 1) }) : undefined} />
        <StatCard label={t("Verschiedene Spieler")} value={seen.length || "–"} icon={<Users className="h-4 w-4" />} tone="violet" hint={t("seit der ersten Messung")} />
        <StatCard label={t("Antwortzeit")} value={info ? `${info.latencyMs} ms` : "–"} icon={<Gauge className="h-4 w-4" />} tone="blue" hint={info?.version} />
      </div>

      <div className="flex justify-end">
        <RangeTabs value={range} onChange={setRange} />
      </div>
      <ChartCard title={t("Spieler im Verlauf")} empty={series.samples === 0}>
        <SeriesChart times={series.times} series={[{ name: t("Spieler"), color: "#f59e0b", values: series.players }]} long={range === "24h" || range === "7d"} format={(value) => String(Math.round(value))} />
      </ChartCard>

      <div className="grid gap-5 xl:grid-cols-2">
        <Card>
          <CardHeader title="Online" description={info?.motd ? t("MOTD: {motd}", { motd: info.motd }) : undefined} />
          {!running ? (
            <EmptyState icon={<Users className="h-6 w-6" />} title={t("Der Server läuft nicht")} />
          ) : !info || info.names.length === 0 ? (
            <EmptyState
              icon={<Users className="h-6 w-6" />}
              title={info && info.online > 0 ? t("{online} Spieler online", { online: info.online }) : t("Niemand online")}
              text={isProxy && info && info.online > 0 ? t("Der Proxy nennt keine Namen – schau bei den Servern dahinter.") : info && info.online > 0 ? t("Der Server nennt ihre Namen nicht.") : undefined}
            />
          ) : (
            <div className="grid gap-2 p-5 sm:grid-cols-2">
              {info.names.map((name) => (
                <div key={name} className="flex items-center gap-3 rounded-lg border border-white/[0.06] bg-white/[0.02] px-3 py-2">
                  <Head name={name} />
                  <span className="min-w-0 flex-1 truncate text-sm">{name}</span>
                  {canOperate && !isProxy && <PlayerMenu server={server} name={name} onDone={() => live.mutate()} />}
                </div>
              ))}
            </div>
          )}
        </Card>

        <Card>
          <CardHeader title={t("Zuletzt gesehen")} description={seen.length ? t("Gesamt {playtime} Spielzeit", { playtime: playtime(totalSeconds) }) : undefined} />
          {seen.length === 0 ? (
            <EmptyState icon={<Clock className="h-6 w-6" />} title={t("Noch keine Spieler erfasst")} text={t("Namen werden erfasst, solange der Server sie bei der Abfrage nennt.")} />
          ) : (
            <div className="max-h-[420px] divide-y divide-white/[0.05] overflow-y-auto">
              {seen.map((player) => (
                <div key={player.name} className="flex items-center gap-3 px-5 py-2.5">
                  <Head name={player.name} size={24} />
                  <span className="min-w-0 flex-1 truncate text-sm">{player.name}</span>
                  {player.online ? <Badge tone="good">{t("online")}</Badge> : <span className="text-xs text-muted">{ago(player.lastSeen)}</span>}
                  <span className="w-20 text-right text-xs tabular-nums text-muted" title={t("Spielzeit (aus den Messungen)")}>
                    {playtime(player.seconds)}
                  </span>
                </div>
              ))}
            </div>
          )}
        </Card>
      </div>
      {canOperate && !isProxy && <PlayerAdmin server={server} />}
    </div>
  );
}
