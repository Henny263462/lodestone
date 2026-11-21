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
import { t, tx } from "@/lib/i18n";

// Eine Funktion, weil die Texte erst beim Rendern übersetzt werden können
const kinds = (): { kind: TunnelKind; title: string; text: string; missing: string }[] => [
  {
    kind: "java",
    title: "Minecraft Java",
    text: t("Spieler betreten den Server über diese Adresse."),
    missing: t("Dieser Server ist nicht nach außen veröffentlicht. Lege den Tunnel am Proxy an."),
  },
  {
    kind: "voicechat",
    title: t("Voicechat"),
    text: t("Simple Voice Chat über UDP. Die Adresse gilt nach dem nächsten Neustart des Servers."),
    missing: t("Für Voicechat braucht der Server einen veröffentlichten UDP-Port. Schalte zuerst Voicechat ein."),
  },
];

/** Dialog zum Anlegen: Region und eigene Domain wählen, soweit das playit-Konto sie erlaubt. */
function CreateTunnelDialog({ server, kind, onClose, onCreated }: { server: Server; kind: TunnelKind; onClose: () => void; onCreated: () => void }) {
  const toast = useToast();
  const { data, error } = useApi<TunnelOptions>(`/servers/${server.id}/tunnels/options`);
  const [region, setRegion] = useState("");
  const [domainId, setDomainId] = useState("");
  const [busy, setBusy] = useState(false);
  const title = kinds().find((entry) => entry.kind === kind)?.title ?? "";
  const freeDomains = data?.domains.filter((domain) => !domain.used) ?? [];
  const canRegion = !!data?.premium && !domainId;
  const canDomain = !!data?.premium && kind === "java" && freeDomains.length > 0 && !region;

  async function submit() {
    setBusy(true);
    const ok = await toast.run(() => api.post(`/servers/${server.id}/tunnels`, { kind, region, domainId }), t("Tunnel angelegt"));
    setBusy(false);
    if (ok) onCreated();
  }

  return (
    <Dialog
      open
      onClose={onClose}
      title={t("{kind}-Tunnel anlegen", { kind: title })}
      description={t("Ohne Auswahl sucht playit selbst das beste Rechenzentrum aus.")}
      footer={
        <>
          <Button variant="ghost" onClick={onClose}>
            {t("Abbrechen")}
          </Button>
          <Button variant="primary" loading={busy} onClick={submit}>
            {t("Tunnel anlegen")}
          </Button>
        </>
      }
    >
      {error && <p className="rounded-lg border border-bad/25 bg-bad/10 px-4 py-3 text-xs text-bad">{error.message} {t("Du kannst den Tunnel trotzdem automatisch anlegen.")}</p>}
      {!data && !error && <p className="text-xs text-muted">{t("Die Möglichkeiten deines playit-Kontos werden geladen.")}</p>}
      {data && !data.premium && (
        <p className="rounded-lg border border-info/25 bg-info/10 px-4 py-3 text-xs text-info">
          {t("Eigene Regionen und Domains gehören zu playit Premium.")}{" "}
          <a href="https://playit.gg/support/playit-premium/" target="_blank" rel="noreferrer" className="underline">
            {t("Mehr zu playit Premium")}
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
              aria-label="Region"
              options={[{ value: "", label: t("Automatisch") }, ...(data?.regions.map((entry) => ({ value: entry.id, label: entry.name })) ?? [])]}
            />
            <p className="mt-1 text-xs text-muted">
              {domainId ? t("Bei einer eigenen Domain bestimmt die Domain die Region.") : t("Spieler in der Nähe der Region haben weniger Verzögerung. Eine Region lässt sich später nicht ändern – lege dann den Tunnel neu an.")}
            </p>
          </div>
          {kind === "java" && (
            <div>
              <label className="label">{t("Eigene Domain")}</label>
              <Select
                value={domainId}
                onChange={setDomainId}
                disabled={!canDomain && !domainId}
                aria-label={t("Eigene Domain")}
                options={[
                  { value: "", label: t("Keine (Adresse von playit)") },
                  ...(data?.domains.map((domain) => ({ value: domain.id, label: domain.used ? t("{name} (schon vergeben)", { name: domain.name }) : domain.name, disabled: domain.used })) ?? []),
                ]}
              />
              <p className="mt-1 text-xs text-muted">
                {data?.domainsProblem
                  ? t("Die Domains ließen sich nicht laden: {problem}", { problem: tx(data.domainsProblem) })
                  : data && data.premium && data.domains.length === 0
                    ? t("In deinem playit-Konto ist noch keine Domain eingerichtet. Lege sie im playit-Dashboard an.")
                    : t("Spieler betreten den Server dann über deine Domain statt über eine playit-Adresse.")}
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
    const ok = await toast.run(() => api.post(`/servers/${server.id}/tunnels`, { kind }), t("Adresse übernommen"));
    setBusy(null);
    mutate();
    if (ok) onDone();
  }

  let body;
  if (error) {
    body = <ErrorState inline error={error} retry={() => mutate()} />;
  } else if (!data) {
    body = <Loading />;
  } else if (!data.configured) {
    body = (
      <EmptyState
        icon={<Cable className="h-5 w-5" />}
        title={t("playit ist auf diesem Node nicht eingerichtet")}
        text={user.isAdmin ? t("Verbinde den Node unter Verwaltung → playit.gg mit playit. Danach legst du hier Tunnel mit einem Klick an.") : t("Ein Administrator muss den Node erst mit playit verbinden.")}
      />
    );
  } else {
    const all = kinds();
    const rows = all.filter((entry) => data.available[entry.kind] || data.tunnels.some((tunnel) => tunnel.kind === entry.kind));
    body =
      rows.length === 0 ? (
        <EmptyState icon={<Cable className="h-5 w-5" />} title={t("Kein Port veröffentlicht")} text={all[0].missing} />
      ) : (
        <div className="divide-y divide-white/[0.05]">
          {data.problem && <p className="rounded-lg border border-bad/25 bg-bad/10 px-4 py-3 text-xs text-bad">{tx(data.problem)}</p>}
          {rows.map((entry) => {
            const tunnel = data.tunnels.find((candidate) => candidate.kind === entry.kind);
            return (
              <div key={entry.kind} className="flex flex-wrap items-center gap-3 px-5 py-3">
                <div className="min-w-0 flex-1">
                  <p className="flex items-center gap-2 text-sm font-medium">
                    {entry.title}
                    {tunnel && <Badge tone={tunnel.state === "allocated" ? "good" : tunnel.state === "pending" ? "info" : "warn"}>{tunnel.state === "allocated" ? t("Bereit") : tunnel.state === "pending" ? t("Wird eingerichtet") : t("Aus")}</Badge>}
                  </p>
                  {tunnel?.address ? <p className="mt-0.5 truncate font-mono text-xs">{tunnel.address}</p> : <p className="mt-0.5 text-xs text-muted">{tunnel ? t("playit hat die Adresse noch nicht vergeben.") : entry.text}</p>}
                  {tunnel && (tunnel.region || tunnel.domain) && (
                    <p className="mt-1 flex flex-wrap gap-1.5">
                      {tunnel.region && <Badge>{t("Region: {region}", { region: tunnel.region })}</Badge>}
                      {tunnel.domain && <Badge tone="accent">{t("Domain: {domain}", { domain: tunnel.domain })}</Badge>}
                    </p>
                  )}
                </div>
                {!tunnel && (
                  <Button size="sm" onClick={() => setCreating(entry.kind)}>
                    {t("Tunnel anlegen")}
                  </Button>
                )}
                {tunnel && !tunnel.address && (
                  <Button size="sm" loading={busy === entry.kind} onClick={() => adopt(entry.kind)}>
                    {t("Adresse übernehmen")}
                  </Button>
                )}
                {tunnel && (
                  <Button size="icon-sm" variant="ghost" aria-label={t("{kind}-Tunnel löschen", { kind: entry.title })} onClick={() => setRemoving(tunnel)}>
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
        title={t("playit-Tunnel")}
        description={t("Legt bei playit.gg automatisch einen Tunnel auf den Port dieses Servers an und trägt die Adresse ein. Spieler brauchen dann keine Portfreigabe im Router.")}
        action={
          user.isAdmin && data && !data.configured ? (
            <Link href="/admin/playit/" className="text-xs text-accent hover:underline">
              {t("Zu playit.gg")}
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
        title={t("{kind}-Tunnel löschen?", { kind: removing?.kind === "voicechat" ? t("Voicechat") : "Minecraft Java" })}
        text={t("Der Tunnel wird bei playit.gg gelöscht und die Adresse beim Server entfernt. Spieler, die sie benutzen, kommen nicht mehr hinein.")}
        confirm={t("Tunnel löschen")}
        danger
        onConfirm={async () => {
          if (!removing) return;
          const ok = await toast.run(() => api.delete(`/servers/${server.id}/tunnels/${removing.kind}`), t("Tunnel gelöscht"));
          mutate();
          if (ok) onDone();
        }}
      />
    </Card>
  );
}
