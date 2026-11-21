"use client";

import { Moon } from "lucide-react";
import { useEffect, useState, type ReactNode } from "react";
import { Badge, Button, Card, CardHeader, Switch } from "@/components/ui/primitives";
import { useToast } from "@/components/ui/feedback";
import { ErrorState, Loading } from "@/components/ui/async";
import { api, useApi } from "@/lib/api";
import { usePoll } from "@/lib/live";
import { ago, dateTime } from "@/lib/format";
import type { Server, SleepStatus } from "@/lib/types";
import { t } from "@/lib/i18n";

/** Schlafmodus: Start beim Beitritt, Stopp im Leerlauf. Nur für Einzelserver und den Proxy eines Netzwerks. */
export function SleepCard({ server }: { server: Server }) {
  const toast = useToast();
  // Einschlafen und Aufwachen kommen über den Live-Kanal (Zustand des Servers)
  const { data, error, mutate } = useApi<SleepStatus>(`/servers/${server.id}/sleep`, { refreshInterval: usePoll(60000, 10000) });
  const [minutes, setMinutes] = useState("10");
  const [busy, setBusy] = useState(false);

  useEffect(() => {
    if (data) setMinutes(String(data.idleMinutes));
  }, [data?.idleMinutes]); // eslint-disable-line react-hooks/exhaustive-deps

  const header = (action?: ReactNode) => (
    <CardHeader
      title={t("Schlafmodus")}
      description={t("Startet den Server, wenn jemand beitritt, und stoppt ihn, wenn niemand mehr da ist. Der Schalter wirkt sofort.")}
      action={action}
    />
  );

  if (error)
    return (
      <Card>
        {header()}
        <ErrorState inline error={error} retry={() => mutate()} />
      </Card>
    );
  if (!data)
    return (
      <Card>
        {header()}
        <Loading />
      </Card>
    );

  const idle = Number(minutes);
  const idleValid = Number.isInteger(idle) && idle >= 1 && idle <= 1440;
  const canSwitch = data.enabled || (data.allowed && data.supported);

  async function save(enabled: boolean, idleMinutes: number, message: string) {
    setBusy(true);
    const ok = await toast.run(() => api.put(`/servers/${server.id}/sleep`, { enabled, idleMinutes }), message);
    setBusy(false);
    if (ok) mutate();
  }

  return (
    <Card>
      {header(
        <Switch
          checked={data.enabled}
          disabled={!canSwitch || busy}
          onChange={(value) => save(value, idleValid ? idle : data.idleMinutes, value ? t("Schlafmodus ist an") : t("Schlafmodus ist aus"))}
        />,
      )}
      <div className="space-y-4 px-5 py-4">
        {!data.allowed && <div className="rounded-lg border border-info/25 bg-info/10 px-4 py-3 text-xs text-info">{data.reason}</div>}
        {data.allowed && !data.supported && (
          <div className="rounded-lg border border-warn/25 bg-warn/10 px-4 py-3 text-xs text-warn">
            {t("Dieser Node läuft in einem Docker-Container mit eigenem Netz und kann die Ports der Maschine nicht selbst belegen. Starte den Node direkt auf der Maschine oder mit")}{" "}
            <code className="font-mono">network_mode: host</code> {t("und")} <code className="font-mono">LODESTONE_NODE_SLEEP_MODE=on</code>.
          </div>
        )}
        {data.problem && <div className="rounded-lg border border-warn/25 bg-warn/10 px-4 py-3 text-xs text-warn">{data.problem}</div>}
        <p className="text-xs text-muted">{t("Ist der Server aus, wartet Lodestone auf seinem Port: Die Serverliste zeigt „Schläft – zum Starten beitreten“, ein Beitritt weckt ihn.")}</p>
        {server.role === "proxy" && data.allowed && (
          <p className="text-xs text-muted">{t("Schläft der Proxy ein, stoppt Lodestone auch die Backend-Server des Netzwerks; wacht er auf, startet es sie mit.")}</p>
        )}

        <div className="flex flex-wrap items-center gap-2 text-xs text-muted">
          <Moon className="h-3.5 w-3.5" />
          {data.listening ? (
            <Badge tone="info">{t("schläft · Port")} {data.port} {t("wartet auf Spieler")}</Badge>
          ) : data.enabled ? (
            <Badge tone="good">{t("wach")}</Badge>
          ) : (
            <Badge>{t("aus")}</Badge>
          )}
          {data.lastWake ? <span title={dateTime(data.lastWake)}>{t("zuletzt geweckt")} {ago(data.lastWake)}</span> : null}
          {data.lastSleep ? <span title={dateTime(data.lastSleep)}>{t("· zuletzt eingeschlafen")} {ago(data.lastSleep)}</span> : null}
        </div>

        <div className="flex flex-wrap items-end justify-between gap-3">
          <div>
            <label className="label">{t("Stoppen nach Minuten ohne Spieler")}</label>
            <input className="field max-w-[160px]" inputMode="numeric" value={minutes} onChange={(event) => setMinutes(event.target.value)} disabled={!data.allowed} />
            {!idleValid && <p className="mt-1 text-xs text-muted">{t("Erlaubt sind 1 bis 1440 Minuten.")}</p>}
          </div>
          <Button size="sm" loading={busy} disabled={!data.allowed || !idleValid || idle === data.idleMinutes} onClick={() => save(data.enabled, idle, t("Gespeichert"))}>
            {t("Speichern")}
          </Button>
        </div>
      </div>
    </Card>
  );
}
