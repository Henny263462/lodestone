"use client";

import { Cpu, MemoryStick, Timer, Users } from "lucide-react";
import { useState } from "react";
import { Card, CardHeader, StatCard, Tabs } from "@/components/ui/primitives";
import { SeriesChart } from "@/components/ui/chart";
import { ErrorState, Loading } from "@/components/ui/async";
import { StorageCard } from "@/components/server/storage";
import { useApi } from "@/lib/api";
import { megabytes, uptime } from "@/lib/format";
import type { Metrics, Range, Server } from "@/lib/types";
import { formatNumber, t } from "@/lib/i18n";

export const ranges = (): { id: Range; label: string }[] => [
  { id: "1h", label: t("1 Std") },
  { id: "6h", label: t("6 Std") },
  { id: "24h", label: t("24 Std") },
  { id: "7d", label: t("7 Tage") },
];

const MB = 1024 * 1024;

export function RangeTabs({ value, onChange }: { value: Range; onChange: (range: Range) => void }) {
  return <Tabs value={value} onChange={onChange} items={ranges()} />;
}

/** Ein Diagramm mit Titel und Zusammenfassung; fehlen Messwerte, steht ein Hinweis statt einer leeren Fläche. */
export function ChartCard({
  title,
  description,
  empty,
  children,
  action,
}: {
  title: string;
  description?: string;
  empty: boolean;
  children: React.ReactNode;
  action?: React.ReactNode;
}) {
  return (
    <Card>
      <CardHeader title={title} description={description} action={action} />
      <div className="px-3 pb-3 pt-1">
        {empty ? <p className="px-4 py-14 text-center text-sm text-muted">{t("Für diesen Zeitraum gibt es noch keine Messwerte. Sie werden alle 30 Sekunden erfasst, solange der Server läuft.")}</p> : children}
      </div>
    </Card>
  );
}

export function StatsTab({ server }: { server: Server }) {
  const [range, setRange] = useState<Range>("6h");
  const { data, error, mutate } = useApi<Metrics>(`/servers/${server.id}/metrics?range=${range}`, { refreshInterval: 30000 });
  const view = server.view;
  const stats = view?.stats;
  const running = view?.state === "running";

  if (error) return <ErrorState error={error} retry={() => mutate()} />;
  if (!data) return <Loading />;

  const series = data.series;
  const long = range === "24h" || range === "7d";
  const limitMB = view?.memoryMB ?? 0;
  const empty = series.samples === 0;
  const memoryMB = series.memory.map((value) => (value === null ? null : value / MB));
  const memoryPercent = stats && stats.memoryLimit > 0 ? (stats.memoryBytes / stats.memoryLimit) * 100 : 0;

  return (
    <div className="space-y-5">
      <div className="grid gap-4 sm:grid-cols-2 xl:grid-cols-4">
        <StatCard label={t("Laufzeit")} value={running ? uptime(view?.startedAt) : "–"} icon={<Timer className="h-4 w-4" />} tone={running ? "green" : "neutral"} hint={running ? t("seit dem letzten Start") : t("Der Server läuft nicht")} />
        <StatCard
          label="CPU"
          value={running && stats ? `${stats.cpuPercent.toFixed(0)} %` : "–"}
          icon={<Cpu className="h-4 w-4" />}
          tone="blue"
          hint={series.samples ? t("Ø {average} % · Spitze {peak} %", { average: series.avgCpu.toFixed(0), peak: series.peakCpu.toFixed(0) }) : undefined}
        />
        <StatCard
          label={t("Arbeitsspeicher")}
          value={running && stats ? `${Math.round(stats.memoryBytes / MB)} MB` : "–"}
          icon={<MemoryStick className="h-4 w-4" />}
          tone={memoryPercent >= 90 ? "red" : memoryPercent >= 75 ? "amber" : "violet"}
          hint={limitMB ? t("von {total}", { total: megabytes(limitMB) }) + (running && stats ? ` (${formatNumber(memoryPercent, 0)} %)` : "") : undefined}
        />
        <StatCard
          label={t("Spieler")}
          value={running ? `${data.players}${data.maxPlayers ? ` / ${data.maxPlayers}` : ""}` : "–"}
          icon={<Users className="h-4 w-4" />}
          tone="amber"
          hint={series.samples ? t("Spitze {peakPlayers} · Ø {average}", { peakPlayers: series.peakPlayers, average: formatNumber(series.avgPlayers, 1) }) : undefined}
        />
      </div>

      <div className="flex justify-end">
        <RangeTabs value={range} onChange={setRange} />
      </div>

      <ChartCard title={t("Spieler")} description={t("Gleichzeitig online")} empty={empty}>
        <SeriesChart times={series.times} series={[{ name: t("Spieler"), color: "#f59e0b", values: series.players }]} long={long} format={(value) => String(Math.round(value))} />
      </ChartCard>
      <div className="grid gap-5 xl:grid-cols-2">
        <ChartCard title="CPU" description={t("Auslastung des Containers")} empty={empty}>
          <SeriesChart times={series.times} series={[{ name: "CPU", color: "#3b82f6", values: series.cpu }]} long={long} format={(value) => `${Math.round(value)} %`} />
        </ChartCard>
        <ChartCard title={t("Arbeitsspeicher")} description={limitMB ? t("Limit {limitMB}", { limitMB: megabytes(limitMB) }) : undefined} empty={empty}>
          <SeriesChart
            times={series.times}
            series={[{ name: t("Speicher"), color: "#8b5cf6", values: memoryMB }]}
            yMax={limitMB || undefined}
            long={long}
            format={(value) => `${Math.round(value)} MB`}
          />
        </ChartCard>
      </div>
      <StorageCard server={server} />
    </div>
  );
}
