"use client";

import { Cable, Trash2 } from "lucide-react";
import Link from "next/link";
import { useState } from "react";
import { useUser } from "@/components/layout/shell";
import { Badge, Button, Card, CardHeader, EmptyState, Select } from "@/components/ui/primitives";
import { ErrorState, Loading } from "@/components/ui/async";
import { ConfirmDialog, Dialog, useToast } from "@/components/ui/feedback";
import { api, useApi } from "@/lib/api";
import type { Server, Tunnel, TunnelKind, TunnelList, TunnelOptions } from "@/lib/types";

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

/** Dialog zum Anlegen: Region und eigene Domain wählen, soweit das playit-Konto sie erlaubt. */
function CreateTunnelDialog({ server, kind, onClose, onCreated }: { server: Server; kind: TunnelKind; onClose: () => void; onCreated: () => void }) {
  const toast = useToast();
  const { data, error } = useApi<TunnelOptions>(`/servers/${server.id}/tunnels/options`);
  const [region, setRegion] = useState("");
  const [domainId, setDomainId] = useState("");
  const [busy, setBusy] = useState(false);
  const title = kinds.find((entry) => entry.kind === kind)?.title ?? "";
  const freeDomains = data?.domains.filter((domain) => !domain.used) ?? [];
  const canRegion = !!data?.premium && !domainId;
  const canDomain = !!data?.premium && kind === "java" && freeDomains.length > 0 && !region;

  async function submit() {
    setBusy(true);
    const ok = await toast.run(() => api.post(`/servers/${server.id}/tunnels`, { kind, region, domainId }), "Tunnel angelegt");
    setBusy(false);
    if (ok) onCreated();
  }

  return (
    <Dialog
      open
      onClose={onClose}
      title={`${title}-Tunnel anlegen`}
      description="Ohne Auswahl sucht playit selbst das beste Rechenzentrum aus."
      footer={
        <>
          <Button variant="ghost" onClick={onClose}>
            Abbrechen
          </Button>
          <Button variant="primary" loading={busy} onClick={submit}>
            Tunnel anlegen
          </Button>
        </>
      }
    >
      {error && <p className="rounded-lg border border-bad/25 bg-bad/10 px-4 py-3 text-xs text-bad">{error.message} Du kannst den Tunnel trotzdem automatisch anlegen.</p>}
      {!data && !error && <p className="text-xs text-muted">Die Möglichkeiten deines playit-Kontos werden geladen.</p>}
      {data && !data.premium && (
        <p className="rounded-lg border border-info/25 bg-info/10 px-4 py-3 text-xs text-info">
          Eigene Regionen und Domains gehören zu playit Premium.{" "}
          <a href="https://playit.gg/support/playit-premium/" target="_blank" rel="noreferrer" className="underline">
            Mehr zu playit Premium
          </a>
        </p>
      )}
      {(data || error) && (
        <>
          <div>
            <label className="label">Region</label>
            <Select
              value={region}
              onChange={setRegion}
              disabled={!canRegion}
              options={[{ value: "", label: "Automatisch" }, ...(data?.regions.map((entry) => ({ value: entry.id, label: entry.name })) ?? [])]}
            />
            <p className="mt-1 text-xs text-muted">
              {domainId ? "Bei einer eigenen Domain bestimmt die Domain die Region." : "Spieler in der Nähe der Region haben weniger Verzögerung. Eine Region lässt sich später nicht ändern – lege dann den Tunnel neu an."}
            </p>
          </div>
          {kind === "java" && (
            <div>
              <label className="label">Eigene Domain</label>
              <Select
                value={domainId}
                onChange={setDomainId}
                disabled={!canDomain && !domainId}
                options={[
                  { value: "", label: "Keine (Adresse von playit)" },
                  ...(data?.domains.map((domain) => ({ value: domain.id, label: domain.used ? `${domain.name} (schon vergeben)` : domain.name, disabled: domain.used })) ?? []),
                ]}
              />
              <p className="mt-1 text-xs text-muted">
                {data?.domainsProblem
                  ? `Die Domains ließen sich nicht laden: ${data.domainsProblem}`
                  : data && data.premium && data.domains.length === 0
                    ? "In deinem playit-Konto ist noch keine Domain eingerichtet. Lege sie im playit-Dashboard an."
                    : "Spieler betreten den Server dann über deine Domain statt über eine playit-Adresse."}
              </p>
            </div>
          )}
        </>
      )}
    </Dialog>
  );
}

/** playit-Tunnel des Servers: Das Panel legt sie bei playit.gg an und trägt die Adresse beim Server ein. */
export function TunnelsCard({ server, onDone }: { server: Server; onDone: () => void }) {
  const toast = useToast();
  const user = useUser();
  const { data, error, mutate } = useApi<TunnelList>(`/servers/${server.id}/tunnels`);
  const [busy, setBusy] = useState<TunnelKind | null>(null);
  const [creating, setCreating] = useState<TunnelKind | null>(null);
  const [removing, setRemoving] = useState<Tunnel | null>(null);

  async function adopt(kind: TunnelKind) {
    setBusy(kind);
    const ok = await toast.run(() => api.post(`/servers/${server.id}/tunnels`, { kind }), "Adresse übernommen");
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
                  {tunnel && (tunnel.region || tunnel.domain) && (
                    <p className="mt-1 flex flex-wrap gap-1.5">
                      {tunnel.region && <Badge>{`Region: ${tunnel.region}`}</Badge>}
                      {tunnel.domain && <Badge tone="accent">{`Domain: ${tunnel.domain}`}</Badge>}
                    </p>
                  )}
                </div>
                {!tunnel && (
                  <Button size="sm" onClick={() => setCreating(entry.kind)}>
                    Tunnel anlegen
                  </Button>
                )}
                {tunnel && !tunnel.address && (
                  <Button size="sm" loading={busy === entry.kind} onClick={() => adopt(entry.kind)}>
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
      {creating && (
        <CreateTunnelDialog
          server={server}
          kind={creating}
          onClose={() => setCreating(null)}
          onCreated={() => {
            setCreating(null);
            mutate();
            onDone();
          }}
        />
      )}
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
