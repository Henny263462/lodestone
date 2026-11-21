"use client";

import { Play, RotateCw, Skull, Square } from "lucide-react";
import { useState } from "react";
import { Badge, Button, Dot } from "@/components/ui/primitives";
import { ConfirmDialog, useToast } from "@/components/ui/feedback";
import { api } from "@/lib/api";
import { stateLabel, stateTone } from "@/lib/format";
import type { Server } from "@/lib/types";
import { t } from "@/lib/i18n";

export function StateBadge({ server }: { server: Server }) {
  if (!server.online) return <Badge tone="bad">{t("Node offline")}</Badge>;
  const state = server.view?.state;
  const tone = stateTone(state);
  return (
    <Badge tone={tone === "neutral" ? "neutral" : tone}>
      <Dot tone={tone} />
      {state ? stateLabel(state) : t("Unbekannt")}
    </Badge>
  );
}

/** Start, Stopp, Neustart und Kill für einen Server. */
export function PowerButtons({ server, onDone, size = "sm", enabled = true }: { server: Server; onDone: () => void; size?: "sm" | "md"; enabled?: boolean }) {
  const toast = useToast();
  const [busy, setBusy] = useState<string | null>(null);
  const [killing, setKilling] = useState(false);
  const state = server.view?.state;
  const usable = enabled && server.online && !!state;

  async function power(action: string, label: string) {
    setBusy(action);
    await toast.run(() => api.post(`/servers/${server.id}/power`, { action }), label);
    setBusy(null);
    onDone();
  }

  const canStart = usable && state === "offline";
  const canStop = usable && (state === "running" || state === "starting");
  const canRestart = usable && state === "running";

  return (
    <div className="flex flex-wrap items-center gap-1.5">
      <Button size={size} variant="secondary" disabled={!canStart} loading={busy === "start"} onClick={() => power("start", t("Server startet"))}>
        <Play className="h-3.5 w-3.5 text-good" />
        {t("Start")}
      </Button>
      <Button size={size} variant="secondary" disabled={!canRestart} loading={busy === "restart"} onClick={() => power("restart", t("Server startet neu"))}>
        <RotateCw className="h-3.5 w-3.5" />
        {t("Neustart")}
      </Button>
      <Button size={size} variant="secondary" disabled={!canStop} loading={busy === "stop"} onClick={() => power("stop", t("Server wird gestoppt"))}>
        <Square className="h-3.5 w-3.5 text-bad" />
        {t("Stopp")}
      </Button>
      {usable && (state === "running" || state === "starting" || state === "stopping") && (
        <Button size={size} variant="ghost" onClick={() => setKilling(true)} title={t("Beendet den Server sofort, ungespeicherte Daten gehen verloren")}>
          <Skull className="h-3.5 w-3.5" />
        </Button>
      )}
      <ConfirmDialog
        open={killing}
        onClose={() => setKilling(false)}
        title={t("Server beenden erzwingen?")}
        text={t("Der Server wird sofort beendet, ohne zu speichern. Nutze das nur, wenn „Stopp“ nicht mehr hilft.")}
        confirm={t("Sofort beenden")}
        danger
        onConfirm={() => power("kill", t("Server beendet"))}
      />
    </div>
  );
}

export function ServerAddress({ server }: { server: Server }) {
  if (!server.address) return <span className="text-muted">–</span>;
  return <span className="font-mono text-xs">{server.address}</span>;
}
