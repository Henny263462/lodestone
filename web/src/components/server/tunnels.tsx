"use client";

import { Cable, Trash2 } from "lucide-react";
import Link from "next/link";
import { useState } from "react";
import { useUser } from "@/components/layout/shell";
import { Badge, Button, Card, CardHeader, EmptyState } from "@/components/ui/primitives";
import { ErrorState, Loading } from "@/components/ui/async";
import { ConfirmDialog, useToast } from "@/components/ui/feedback";
import { api, useApi } from "@/lib/api";
import type { Server, Tunnel, TunnelKind, TunnelList } from "@/lib/types";

const kinds: { kind: TunnelKind; title: string; text: string; missing: string }[] = [
  {
    kind: "java",
    title: "Minecraft Java",
    text: "Spieler betreten den Server über diese Adresse.",
    missing: "Dieser Server ist nicht nach außen veröffentlicht. Lege den Tunnel am Proxy an.",
  },
  {
    kind: "voicechat",
    title: "Voicechat",
    text: "Simple Voice Chat über UDP. Die Adresse gilt nach dem nächsten Neustart des Servers.",
    missing: "Für Voicechat braucht der Server einen veröffentlichten UDP-Port. Schalte zuerst Voicechat ein.",
  },
];

/** playit-Tunnel des Servers: Das Panel legt sie bei playit.gg an und trägt die Adresse beim Server ein. */
export function TunnelsCard({ server, onDone }: { server: Server; onDone: () => void }) {
  const toast = useToast();
  const user = useUser();
  const { data, error, mutate } = useApi<TunnelList>(`/servers/${server.id}/tunnels`);
  const [busy, setBusy] = useState<TunnelKind | null>(null);
  const [removing, setRemoving] = useState<Tunnel | null>(null);

  async function create(kind: TunnelKind, done: string) {
    setBusy(kind);
    const ok = await toast.run(() => api.post(`/servers/${server.id}/tunnels`, { kind }), done);
    setBusy(null);
    mutate();
    if (ok) onDone();
  }

  let body;
  if (error) {
    body = <ErrorState error={error} retry={() => mutate()} />;
  } else if (!data) {
    body = <Loading />;
  } else if (!data.configured) {
    body = (
      <EmptyState
        icon={<Cable className="h-5 w-5" />}
        title="playit ist auf diesem Node nicht eingerichtet"
        text={user.isAdmin ? "Hinterlege unter Nodes den Schlüssel deines playit-Agenten. Danach legst du hier Tunnel mit einem Klick an." : "Ein Administrator muss unter Nodes den Schlüssel des playit-Agenten hinterlegen."}
      />
    );
  } else {
    const rows = kinds.filter((entry) => data.available[entry.kind] || data.tunnels.some((tunnel) => tunnel.kind === entry.kind));
    body =
      rows.length === 0 ? (
        <EmptyState icon={<Cable className="h-5 w-5" />} title="Kein Port veröffentlicht" text={kinds[0].missing} />
      ) : (
        <div className="divide-y divide-white/[0.05]">
          {data.problem && <p className="rounded-lg border border-bad/25 bg-bad/10 px-4 py-3 text-xs text-bad">{data.problem}</p>}
          {rows.map((entry) => {
            const tunnel = data.tunnels.find((candidate) => candidate.kind === entry.kind);
            return (
              <div key={entry.kind} className="flex items-center gap-3 px-5 py-3">
                <div className="min-w-0 flex-1">
                  <p className="flex items-center gap-2 text-sm font-medium">
                    {entry.title}
                    {tunnel && <Badge tone={tunnel.state === "allocated" ? "good" : tunnel.state === "pending" ? "info" : "warn"}>{tunnel.state === "allocated" ? "Bereit" : tunnel.state === "pending" ? "Wird eingerichtet" : "Aus"}</Badge>}
                  </p>
                  {tunnel?.address ? <p className="mt-0.5 truncate font-mono text-xs">{tunnel.address}</p> : <p className="mt-0.5 text-xs text-muted">{tunnel ? "playit hat die Adresse noch nicht vergeben." : entry.text}</p>}
                </div>
                {!tunnel && (
                  <Button size="sm" loading={busy === entry.kind} onClick={() => create(entry.kind, "Tunnel angelegt")}>
                    Tunnel anlegen
                  </Button>
                )}
                {tunnel && !tunnel.address && (
                  <Button size="sm" loading={busy === entry.kind} onClick={() => create(entry.kind, "Adresse übernommen")}>
                    Adresse übernehmen
                  </Button>
                )}
                {tunnel && (
                  <Button size="icon-sm" variant="ghost" aria-label={`${entry.title}-Tunnel löschen`} onClick={() => setRemoving(tunnel)}>
                    <Trash2 className="h-4 w-4" />
                  </Button>
                )}
              </div>
            );
          })}
        </div>
      );
  }

  return (
    <Card>
      <CardHeader
        title="playit-Tunnel"
        description="Legt bei playit.gg automatisch einen Tunnel auf den Port dieses Servers an und trägt die Adresse ein. Spieler brauchen dann keine Portfreigabe im Router."
        action={
          user.isAdmin && data && !data.configured ? (
            <Link href="/nodes/" className="text-xs text-accent hover:underline">
              Zu den Nodes
            </Link>
          ) : undefined
        }
      />
      {body}
      <ConfirmDialog
        open={!!removing}
        onClose={() => setRemoving(null)}
        title={`${removing?.kind === "voicechat" ? "Voicechat" : "Minecraft-Java"}-Tunnel löschen?`}
        text="Der Tunnel wird bei playit.gg gelöscht und die Adresse beim Server entfernt. Spieler, die sie benutzen, kommen nicht mehr hinein."
        confirm="Tunnel löschen"
        danger
        onConfirm={async () => {
          if (!removing) return;
          const ok = await toast.run(() => api.delete(`/servers/${server.id}/tunnels/${removing.kind}`), "Tunnel gelöscht");
          mutate();
          if (ok) onDone();
        }}
      />
    </Card>
  );
}
