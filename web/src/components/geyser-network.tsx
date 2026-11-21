"use client";

import { useState } from "react";
import { Card, CardHeader, Switch } from "@/components/ui/primitives";
import { ConfirmDialog, useToast } from "@/components/ui/feedback";
import { ErrorState, Loading } from "@/components/ui/async";
import { api, useApi } from "@/lib/api";
import type { BlockInfo, Project, Server } from "@/lib/types";
import { t, tx } from "@/lib/i18n";

/** Geyser + Floodgate für ein ganzes Netzwerk: Geyser und Floodgate auf dem Proxy, Floodgate auf den Servern dahinter,
 *  ein gemeinsamer Schlüssel. Gleiches Muster wie Simple Voice Chat über den Proxy. */
export function GeyserNetworkCard({ project, proxy, canManage, refresh }: { project: Project; proxy?: Server; canManage: boolean; refresh: () => void }) {
  const toast = useToast();
  const { data, error, mutate } = useApi<BlockInfo[]>(`/projects/${project.id}/blocks`);
  const [busy, setBusy] = useState(false);
  const [confirmOff, setConfirmOff] = useState(false);
  const stopped = proxy?.view?.state === "offline";

  async function change(enabled: boolean) {
    setBusy(true);
    const ok = await toast.run(
      () => api.put(`/projects/${project.id}/blocks/geyser`, { enabled }),
      enabled ? t("Geyser ist eingerichtet – Floodgate kommt auf die Server") : t("Geyser ist ausgeschaltet"),
    );
    setBusy(false);
    if (ok) {
      mutate();
      refresh();
    }
  }

  if (error) return <ErrorState error={error} retry={() => mutate()} />;
  if (!data) return <Loading />;
  const geyser = data.find((entry) => entry.id === "geyser");
  if (!geyser) return null;

  return (
    <Card>
      <CardHeader
        title={geyser.name}
        description={tx(geyser.description)}
        action={<Switch checked={geyser.enabled} disabled={!canManage || busy || !geyser.available || !stopped} onChange={(value) => (value ? change(true) : setConfirmOff(true))} />}
      />
      <div className="space-y-2 px-5 py-4 text-xs text-muted">
        {!geyser.available ? (
          <p>{geyser.reason && tx(geyser.reason)}</p>
        ) : geyser.enabled ? (
          <>
            <p>
              {t("Aktiv: Bedrock-Spieler verbinden sich mit")}{" "}
              {geyser.address ? <code className="rounded bg-black/30 px-2 py-0.5 text-ink">{geyser.address}</code> : t("dem Bedrock-Port des Proxys")}{t(". Gib den Port in der Firewall für TCP und UDP frei.")}
            </p>
            <p>{t("Neue Server hinter dem Proxy bekommen Floodgate und den Schlüssel automatisch. Nach Änderungen brauchen die Server einen Neustart.")}</p>
          </>
        ) : (
          <p>{t("Beim Einschalten bekommt der Proxy einen Bedrock-Port")}{stopped ? "" : t(" – dafür muss er ausgeschaltet sein")}{t(". Floodgate gibt es für Paper, Purpur und Fabric.")}</p>
        )}
        {geyser.note && <p className="rounded-lg border border-warn/25 bg-warn/10 px-4 py-3 text-warn">{tx(geyser.note)}</p>}
      </div>
      <ConfirmDialog
        open={confirmOff}
        onClose={() => setConfirmOff(false)}
        title={t("Geyser im Netzwerk ausschalten?")}
        text={t("Lodestone löscht Geyser und Floodgate vom Proxy und Floodgate von den Servern und schließt den Bedrock-Port. Bedrock-Spieler kommen dann nicht mehr ins Netzwerk.")}
        confirm={t("Ausschalten")}
        danger
        onConfirm={() => change(false)}
      />
    </Card>
  );
}
