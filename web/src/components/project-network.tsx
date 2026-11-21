"use client";

import { ArrowDown, ArrowUp, Check, Copy, MessageSquareText, Mic, Server as ServerIcon, Smartphone, Waypoints } from "lucide-react";
import { useEffect, useState } from "react";
import { Button, Card, CardHeader, EmptyState, SectionLayout, Switch, type Section } from "@/components/ui/primitives";
import { useToast } from "@/components/ui/feedback";
import { MotdCard } from "@/components/motd";
import { GeyserNetworkCard } from "@/components/geyser-network";
import { api } from "@/lib/api";
import { useUrlState } from "@/lib/url-state";
import { can, type Project, type Server, type SyncResult } from "@/lib/types";

type SectionId = "servers" | "motd" | "voicechat" | "geyser";

const sections: Section<SectionId>[] = [
  { id: "servers", label: "Server und Beitritt", icon: Waypoints },
  { id: "motd", label: "MOTD", icon: MessageSquareText },
  { id: "voicechat", label: "Voicechat", icon: Mic },
  { id: "geyser", label: "Bedrock (Geyser)", icon: Smartphone },
];

/** Tab „Netzwerk“ eines Netzwerk-Projekts: Proxy-Anbindung, MOTD, Voicechat und Geyser über den Proxy. */
export function NetworkTab({ project, canManage, refresh }: { project: Project; canManage: boolean; refresh: () => void }) {
  const [section, setSection] = useUrlState<SectionId>("section", "servers");
  const active = sections.some((entry) => entry.id === section) ? section : "servers";
  const proxy = project.servers.find((server) => server.role === "proxy");
  return (
    <SectionLayout label="Abschnitte des Netzwerks" sections={sections} value={active} onChange={setSection}>
      {active === "servers" && <BackendsSection project={project} proxy={proxy} canManage={canManage} refresh={refresh} />}
      {active === "motd" && <MotdCard key={project.settings?.motd ?? ""} project={project} canManage={canManage} refresh={refresh} />}
      {active === "voicechat" && <VoicechatCard project={project} proxy={proxy} canManage={canManage} refresh={refresh} />}
      {active === "geyser" && <GeyserNetworkCard project={project} proxy={proxy} canManage={canManage && can(project.myPermissions, "addons")} refresh={refresh} />}
    </SectionLayout>
  );
}

interface Entry {
  id: string;
  name: string;
  software: string;
  inTry: boolean;
  forcedHost: string;
  voicechat: boolean;
  state?: string;
}

function BackendsSection({ project, proxy, canManage, refresh }: { project: Project; proxy?: Server; canManage: boolean; refresh: () => void }) {
  const toast = useToast();
  const initial = (): Entry[] =>
    project.servers
      .filter((server) => server.role === "backend")
      .sort((a, b) => a.tryPosition - b.tryPosition)
      .map((server) => ({ id: server.id, name: server.name, software: server.software, inTry: server.inTry, forcedHost: server.forcedHost, voicechat: server.voicechat, state: server.view?.state }));

  const [entries, setEntries] = useState<Entry[]>(initial);
  const [dirty, setDirty] = useState(false);
  const [busy, setBusy] = useState(false);
  const [result, setResult] = useState<SyncResult | null>(null);
  const [copied, setCopied] = useState(false);
  const signature = project.servers.map((server) => `${server.id}:${server.role}`).join(",");

  // Neu laden, wenn Server dazukommen oder wegfallen und nichts bearbeitet wird
  useEffect(() => {
    if (!dirty) setEntries(initial());
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [signature, dirty]);

  function change(index: number, patch: Partial<Entry>) {
    setEntries((list) => list.map((entry, position) => (position === index ? { ...entry, ...patch } : entry)));
    setDirty(true);
  }

  function move(index: number, delta: number) {
    setEntries((list) => {
      const next = [...list];
      const target = index + delta;
      if (target < 0 || target >= next.length) return list;
      [next[index], next[target]] = [next[target], next[index]];
      return next;
    });
    setDirty(true);
  }

  async function save() {
    setBusy(true);
    const ok = await toast.run(async () => {
      setResult(
        await api.put<SyncResult>(`/projects/${project.id}/network`, {
          servers: entries.map(({ id, inTry, forcedHost, voicechat }) => ({ id, inTry, forcedHost, voicechat })),
        }),
      );
    }, "Netzwerk-Konfiguration gespeichert");
    setBusy(false);
    if (ok) {
      setDirty(false);
      refresh();
    }
  }

  async function resync() {
    setBusy(true);
    await toast.run(async () => setResult(await api.post<SyncResult>(`/projects/${project.id}/network/sync`)), "Konfiguration neu geschrieben");
    setBusy(false);
  }

  const joinAddress = proxy?.address;

  return (
    <>
      <Card>
        <CardHeader title="Beitritts-Adresse" description="Spieler verbinden sich mit dem Proxy. Alles dahinter ist von außen nicht erreichbar." />
        <div className="flex flex-wrap items-center justify-between gap-3 px-5 py-4">
          {joinAddress ? (
            <>
              <code className="min-w-0 break-all rounded-md bg-black/30 px-3 py-1.5 text-sm">{joinAddress}</code>
              <Button
                size="sm"
                onClick={() => {
                  navigator.clipboard.writeText(joinAddress).then(
                    () => {
                      setCopied(true);
                      setTimeout(() => setCopied(false), 1500);
                    },
                    () => toast.error("Kopieren ging nicht. Markiere die Adresse und kopiere sie von Hand."),
                  );
                }}
              >
                {copied ? <Check className="h-3.5 w-3.5 text-good" /> : <Copy className="h-3.5 w-3.5" />}
                Kopieren
              </Button>
            </>
          ) : (
            <p className="text-sm text-muted">{proxy ? "Der Node hat keine öffentliche Adresse eingetragen. Die Adresse ist die des Nodes mit dem Port des Proxys." : "Es gibt noch keinen Proxy. Lege ihn im Tab „Server“ an."}</p>
          )}
        </div>
      </Card>

      <Card>
        <CardHeader title="Server hinter dem Proxy" description="Die Reihenfolge bestimmt, wohin Spieler beim Beitritt kommen: der erste erreichbare Server mit „Beitritt“." />
        {entries.length === 0 ? (
          <EmptyState icon={<ServerIcon className="h-6 w-6" />} title="Noch keine Server" text="Füge im Tab „Server“ Paper-, Purpur-, Folia- oder Fabric-Server hinzu. Der Proxy lernt sie automatisch kennen." />
        ) : (
          <div className="divide-y divide-white/[0.05]">
            {entries.map((entry, index) => (
              <div key={entry.id} className="flex flex-wrap items-center gap-x-5 gap-y-3 px-5 py-3.5">
                <div className="flex items-center gap-1">
                  <Button variant="ghost" size="icon-sm" disabled={!canManage || index === 0} onClick={() => move(index, -1)} aria-label={`${entry.name} nach oben`}>
                    <ArrowUp className="h-3.5 w-3.5" />
                  </Button>
                  <Button variant="ghost" size="icon-sm" disabled={!canManage || index === entries.length - 1} onClick={() => move(index, 1)} aria-label={`${entry.name} nach unten`}>
                    <ArrowDown className="h-3.5 w-3.5" />
                  </Button>
                </div>
                <div className="min-w-[140px] flex-1">
                  <p className="text-sm font-medium">{entry.name}</p>
                  <p className="text-xs text-muted">{entry.software}</p>
                </div>
                <label className="flex items-center gap-2 text-xs text-muted">
                  <Switch checked={entry.inTry} disabled={!canManage} onChange={(value) => change(index, { inTry: value })} />
                  Beitritt
                </label>
                <div className="w-full sm:w-64">
                  <input
                    className="field"
                    aria-label={`Forced Host für ${entry.name}`}
                    placeholder="Forced Host, z. B. survival.example.org"
                    value={entry.forcedHost}
                    disabled={!canManage}
                    onChange={(event) => change(index, { forcedHost: event.target.value })}
                  />
                </div>
                {project.voicechat ? (
                  <span className="text-xs text-muted">Voicechat über den Proxy</span>
                ) : (
                  <label className="flex items-center gap-2 text-xs text-muted" title="Simple Voice Chat mit eigenem UDP-Port installieren (Server muss aus sein)">
                    <Switch checked={entry.voicechat} disabled={!canManage} onChange={(value) => change(index, { voicechat: value })} />
                    Eigener Voicechat
                  </label>
                )}
              </div>
            ))}
          </div>
        )}
        {canManage && (
          <div className="flex flex-wrap justify-end gap-2 border-t border-white/[0.06] px-5 py-3">
            <Button onClick={resync} disabled={busy || dirty || !proxy} title="Schreibt die Konfiguration erneut in alle Server">
              Neu abgleichen
            </Button>
            <Button variant="primary" onClick={save} loading={busy} disabled={!dirty || !proxy}>
              Speichern und anwenden
            </Button>
          </div>
        )}
      </Card>

      {result && (
        <Card>
          <CardHeader title="Ergebnis des Abgleichs" description="Server mit geänderten Einstellungen brauchen einen Neustart. Der Proxy lädt seine Liste selbst neu." />
          <ul className="space-y-1 px-5 py-4 text-xs">
            {result.applied.length > 0 && <li className="text-good">Angewendet: {result.applied.join(", ")}</li>}
            {result.pending.length > 0 && <li className="text-warn">Gespeichert, gilt ab dem nächsten Start (die Server legen ihre Dateien erst dann an): {result.pending.join(", ")}</li>}
            {result.failed.map((failure) => (
              <li key={failure} className="text-bad">
                Fehlgeschlagen: {failure}
              </li>
            ))}
            {result.warnings?.map((warning) => (
              <li key={warning} className="text-muted">
                {warning}
              </li>
            ))}
          </ul>
        </Card>
      )}
    </>
  );
}

// ---------------------------------------------------------------- Voicechat über den Proxy

function VoicechatCard({ project, proxy, canManage, refresh }: { project: Project; proxy?: Server; canManage: boolean; refresh: () => void }) {
  const toast = useToast();
  const [busy, setBusy] = useState(false);
  const canEnable = !!proxy;
  const stopped = proxy?.view?.state === "offline";

  async function toggle(enabled: boolean) {
    setBusy(true);
    let failed: string[] = [];
    const ok = await toast.run(
      async () => {
        failed = (await api.put<SyncResult>(`/projects/${project.id}/voicechat`, { enabled })).failed;
      },
      enabled ? "Voicechat über den Proxy ist eingerichtet, die Plugins werden installiert" : "Voicechat über den Proxy ist aus",
    );
    if (ok && failed.length) toast.error(`Fehlgeschlagen: ${failed.join(", ")}`);
    setBusy(false);
    refresh();
  }

  return (
    <Card>
      <CardHeader
        title="Simple Voice Chat"
        description="Der Proxy nimmt den Voicechat aller Server über einen UDP-Port an. Der Schalter wirkt sofort."
        action={<Switch checked={project.voicechat} disabled={!canManage || busy || !canEnable} onChange={toggle} />}
      />
      <div className="space-y-2 px-5 py-4 text-xs text-muted">
        {project.voicechat ? (
          <>
            <p>
              Aktiv: Das Proxy-Plugin liegt auf dem Proxy, das normale Plugin auf allen Servern. Neue Server bekommen es automatisch. Öffne in der Firewall den{" "}
              <b className="text-ink">UDP-Port {proxy?.voiceAddress?.split(":")[1] ?? "des Proxys"}</b>.
            </p>
            {proxy?.voiceAddress && (
              <p>
                Voicechat-Adresse: <code className="rounded bg-black/30 px-2 py-0.5 text-ink">{proxy.voiceAddress}</code>
              </p>
            )}
            <p>Änderst du Server oder Proxy, braucht der betroffene Server einen Neustart, damit er das Plugin lädt.</p>
          </>
        ) : !canEnable ? (
          <p>Lege zuerst den Proxy an.</p>
        ) : (
          <p>
            Beim Einschalten bekommt der Proxy zusätzlich einen UDP-Port{!stopped && ", dafür muss er kurz ausgeschaltet sein"}. Alternativ schaltest du Voicechat unter „Server und Beitritt“ pro Server mit
            eigenem UDP-Port ein.
          </p>
        )}
      </div>
    </Card>
  );
}
