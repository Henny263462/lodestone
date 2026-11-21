"use client";

import { HardDrive, RefreshCw } from "lucide-react";
import { useEffect, useState } from "react";
import { Button, Card, CardHeader, EmptyState, Meter, loadTone } from "@/components/ui/primitives";
import { useToast } from "@/components/ui/feedback";
import { ErrorState, Loading } from "@/components/ui/async";
import { api, useApi } from "@/lib/api";
import { ago, bytes } from "@/lib/format";
import { can, type Server, type ServerAccess, type StorageInfo } from "@/lib/types";

const MB = 1024 ** 2;

/** Limit in GB als Text (Dezimalkomma), 0 = keins. */
function limitText(limitMB: number): string {
  return limitMB ? String(Math.round((limitMB / 1024) * 10) / 10).replace(".", ",") : "";
}

/**
 * Speicherplatz des Servers: Größe des Ordners, Backups, freier Platz der Maschine und das weiche Limit. Unter
 * Statistiken nur zum Ansehen; mit `manage` (Einstellungen → Speicher) auch das Limit.
 */
export function StorageCard({ server, manage = false }: { server: Server; manage?: boolean }) {
  const toast = useToast();
  // Dieselbe Abfrage wie die Serverseite: SWR liefert die Rechte aus dem Zwischenspeicher
  const { data: me } = useApi<ServerAccess>(`/servers/${server.id}`);
  const { data, error, mutate } = useApi<StorageInfo>(`/servers/${server.id}/storage`, {
    refreshInterval: (latest) => (latest?.measuring ? 2000 : 60000),
  });
  const [limit, setLimit] = useState("");
  const [busy, setBusy] = useState(false);
  const canMeasure = can(me?.myPermissions, "files.read");
  const canLimit = manage && can(me?.myPermissions, "settings");

  useEffect(() => {
    if (data) setLimit(limitText(data.limitMB));
  }, [data?.limitMB]); // eslint-disable-line react-hooks/exhaustive-deps

  const header = (
    <CardHeader
      title="Speicherplatz"
      description="Größe des Serverordners mit Welten, Plugins und Configs. Der Node misst alle 5 Minuten."
      action={
        canMeasure && (
          <Button
            size="sm"
            variant="ghost"
            loading={data?.measuring}
            onClick={async () => {
              if (await toast.run(() => api.post(`/servers/${server.id}/storage/measure`), "Wird neu gemessen")) mutate();
            }}
          >
            <RefreshCw className="h-3.5 w-3.5" />
            Neu messen
          </Button>
        )
      }
    />
  );

  if (error)
    return (
      <Card>
        {header}
        <ErrorState inline error={error} retry={() => mutate()} />
      </Card>
    );
  if (!data)
    return (
      <Card>
        {header}
        <Loading />
      </Card>
    );

  const limitBytes = data.limitMB * MB;
  const percent = limitBytes ? (data.bytes / limitBytes) * 100 : 0;
  const parsed = limit.trim() === "" ? 0 : Number(limit.trim().replace(",", "."));
  const limitMB = Math.round(parsed * 1024);
  const valid = !isNaN(parsed) && parsed >= 0 && (limitMB === 0 || limitMB >= 256);

  return (
    <Card>
      {header}
      <div className="space-y-4 px-5 py-4">
        {data.measuredAt === 0 ? (
          <EmptyState icon={<HardDrive className="h-6 w-6" />} title="Noch nicht gemessen" text="Der Node misst den Ordner gleich im Hintergrund. Das dauert bei großen Welten einen Moment." />
        ) : (
          <>
            {data.blocked ? (
              <p className="rounded-lg border border-bad/25 bg-bad/10 px-4 py-3 text-xs text-bad">
                Das Speicherlimit ist erreicht. Starten, Hochladen und Entpacken sind gesperrt, bis du Dateien löschst oder ein Administrator das Limit erhöht.
              </p>
            ) : (
              percent >= 90 && <p className="rounded-lg border border-warn/25 bg-warn/10 px-4 py-3 text-xs text-warn">Speicher fast voll: Bei 100 % startet der Server nicht mehr und nimmt keine Uploads an.</p>
            )}
            <div>
              <div className="mb-1.5 flex justify-between text-[11px] text-muted">
                <span>Serverordner</span>
                <span className="tabular-nums text-ink/80">
                  {bytes(data.bytes)}
                  {limitBytes ? ` von ${bytes(limitBytes)} (${Math.round(percent)} %)` : " · kein Limit"}
                </span>
              </div>
              {limitBytes > 0 && <Meter value={Math.min(percent, 100)} max={100} tone={loadTone(percent)} />}
            </div>
            <dl className="grid grid-cols-2 gap-x-4 gap-y-2 text-xs sm:grid-cols-4">
              <dt className="text-muted">Dateien</dt>
              <dd className="tabular-nums">{data.files.toLocaleString("de-DE")}</dd>
              <dt className="text-muted">Backups</dt>
              <dd className="tabular-nums">{bytes(data.backups)}</dd>
              <dt className="text-muted">Frei auf dem Node</dt>
              <dd className="tabular-nums">{data.total ? `${bytes(data.free)} von ${bytes(data.total)}` : "unbekannt"}</dd>
              <dt className="text-muted">Gemessen</dt>
              <dd>{data.measuring ? "wird gerade gemessen …" : ago(data.measuredAt)}</dd>
            </dl>
          </>
        )}

        {canLimit && (
          <div className="flex flex-wrap items-end gap-3 border-t border-white/[0.06] pt-4">
            <div className="w-56">
              <label className="label">Speicherlimit (GB, leer = keins)</label>
              <input className="field" inputMode="decimal" value={limit} onChange={(event) => setLimit(event.target.value)} placeholder="z. B. 20" />
            </div>
            <Button
              size="sm"
              loading={busy}
              disabled={!valid || limitMB === data.limitMB}
              onClick={async () => {
                setBusy(true);
                const ok = await toast.run(() => api.put(`/servers/${server.id}/storage`, { limitMB }), limitMB ? "Speicherlimit gespeichert" : "Speicherlimit entfernt");
                setBusy(false);
                if (ok) mutate();
              }}
            >
              Speichern
            </Button>
            <p className="w-full text-xs text-muted">
              Weiches Limit: Der Server läuft weiter, aber ab 100 % lässt er sich nicht mehr starten und nimmt keine Uploads an. Bei 90 % gibt es das Ereignis „Speicher fast voll“. Mindestens 0,25 GB.
            </p>
          </div>
        )}
      </div>
    </Card>
  );
}
