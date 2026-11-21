"use client";

import { Blocks, ExternalLink, Map as MapIcon, Smartphone } from "lucide-react";
import Link from "next/link";
import { useState } from "react";
import { Badge, Card, CardHeader, EmptyState, Switch } from "@/components/ui/primitives";
import { ConfirmDialog, useToast } from "@/components/ui/feedback";
import { ErrorState, Loading } from "@/components/ui/async";
import { api, useApi } from "@/lib/api";
import type { BlockInfo, Server } from "@/lib/types";
import { t, tx } from "@/lib/i18n";

const icons = { geyser: Smartphone, bluemap: MapIcon };

/** Bausteine eines Servers: Geyser + Floodgate (Bedrock-Spieler) und BlueMap (Karte im Browser). Lodestone lädt die
 *  Dateien, veröffentlicht den Port und stellt die Konfiguration ein. */
export function BlocksTab({ server, canManage, offline }: { server: Server; canManage: boolean; offline: boolean }) {
  const toast = useToast();
  const { data, error, mutate } = useApi<BlockInfo[]>(`/servers/${server.id}/blocks`);
  const [busy, setBusy] = useState<string | null>(null);
  const [disable, setDisable] = useState<BlockInfo | null>(null);

  if (error) return <ErrorState error={error} retry={() => mutate()} />;
  if (!data) return <Loading />;
  if (data.length === 0) return <Card><EmptyState icon={<Blocks className="h-6 w-6" />} title={t("Noch keine Bausteine")} text={t("Für diese Software gibt es keine Bausteine.")} /></Card>;

  async function change(block: BlockInfo, enabled: boolean) {
    setBusy(block.id);
    const ok = await toast.run(() => api.put(`/servers/${server.id}/blocks/${block.id}`, { enabled }), enabled ? t("{name} ist eingerichtet – starte den Server", { name: block.name }) : t("{name} ist ausgeschaltet", { name: block.name }));
    setBusy(null);
    if (ok) mutate();
  }

  return (
    <div className="space-y-4">
      {!offline && canManage && <p className="rounded-lg border border-info/25 bg-info/10 px-4 py-3 text-xs text-info">{t("Bausteine bekommen eigene Ports. Schalte den Server aus, um sie ein- oder auszuschalten.")}</p>}
      {data.map((block) => {
        const Icon = icons[block.id] ?? Blocks;
        const switchable = canManage && block.available && !block.managed && offline && busy === null;
        return (
          <Card key={block.id}>
            <CardHeader
              title={block.name}
              description={tx(block.description)}
              action={
                <div className="flex items-center gap-2">
                  {block.enabled ? <Badge tone="good">{t("Aktiv")}</Badge> : <Badge>{t("Aus")}</Badge>}
                  <Switch checked={block.enabled} disabled={!switchable} onChange={(value) => (value ? change(block, true) : setDisable(block))} />
                </div>
              }
            />
            <div className="flex flex-wrap items-center gap-x-5 gap-y-2 px-5 py-4 text-xs text-muted">
              <Icon className="h-4 w-4 shrink-0" />
              {block.managed === "network" ? (
                <span>
                  {block.reason && tx(block.reason)}{" "}
                  <Link className="text-accent hover:underline" href={`/project/${server.projectId}/`}>
                    {t("Zum Netzwerk")}
                  </Link>
                </span>
              ) : !block.available ? (
                <span>{block.reason && tx(block.reason)}</span>
              ) : block.enabled ? (
                <>
                  {block.address && (
                    <span>
                      {t("Bedrock-Adresse:")} <code className="rounded bg-black/30 px-2 py-0.5 text-ink">{block.address}</code> {t("(TCP und UDP freigeben)")}
                    </span>
                  )}
                  {block.url && (
                    <a className="inline-flex items-center gap-1 text-accent hover:underline" href={block.url} target="_blank" rel="noreferrer">
                      <ExternalLink className="h-3 w-3" />
                      {t("Karte öffnen")}
                    </a>
                  )}
                  {!block.address && !block.url && <span>{t("Eingeschaltet. Die Adresse erscheint, sobald der Node eine öffentliche Adresse hat.")}</span>}
                </>
              ) : (
                <span>{canManage ? t("Beim Einschalten lädt Lodestone die Dateien und richtet Port und Konfiguration ein.") : t("Einschalten kann, wer die Rechte „Einstellungen“ und „Plugins und Mods“ hat.")}</span>
              )}
            </div>
          </Card>
        );
      })}
      <ConfirmDialog
        open={!!disable}
        onClose={() => setDisable(null)}
        title={t("„{value}“ ausschalten?", { value: disable?.name ?? "" })}
        text={t("Lodestone löscht die Dateien, die es dafür geladen hat, und schließt den Port. Konfiguration und Daten (zum Beispiel die gerenderte Karte) bleiben im Serverordner.")}
        confirm={t("Ausschalten")}
        danger
        onConfirm={async () => {
          if (disable) await change(disable, false);
        }}
      />
    </div>
  );
}
