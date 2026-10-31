"use client";

import Link from "next/link";
import { ArrowUpRight, Cable, Check, Copy, ExternalLink, Globe, Layers, Radio, RefreshCw, Server, Terminal, Trash2 } from "lucide-react";
import { useState } from "react";
import { AdminSidebar } from "@/components/admin-sidebar";
import { Badge, Button, Card, Dot, EmptyState, PageHeader, StatCard } from "@/components/ui/primitives";
import { ConfirmDialog, Dialog, useToast } from "@/components/ui/feedback";
import { ErrorState, Loading } from "@/components/ui/async";
import { PlayitClaimDialog } from "@/components/playit-claim";
import { api, useApi } from "@/lib/api";
import type { PlayitNodeOverview, PlayitOverview } from "@/lib/types";

export default function AdminPlayitPage() {
  const toast = useToast();
  const { data, error, mutate, isLoading } = useApi<PlayitOverview>("/playit/overview", { refreshInterval: 8000 });

  const [claimingNode, setClaimingNode] = useState<PlayitNodeOverview | null>(null);
  const [editingNode, setEditingNode] = useState<PlayitNodeOverview | null>(null);
  const [secret, setSecret] = useState("");
  const [busy, setBusy] = useState(false);
  const [removingNode, setRemovingNode] = useState<PlayitNodeOverview | null>(null);
  const [copiedId, setCopiedId] = useState<string | null>(null);

  if (error) {
    return (
      <>
        <AdminSidebar />
        <ErrorState error={error} retry={() => mutate()} />
      </>
    );
  }

  if (!data) {
    return (
      <>
        <AdminSidebar />
        <Loading label="playit.gg-Übersicht wird geladen" />
      </>
    );
  }

  const nodes = data.nodes ?? [];
  const tunnels = data.tunnels ?? [];
  const configuredNodes = nodes.filter((n) => n.status?.configured).length;
  const runningNodes = nodes.filter((n) => n.status?.running).length;
  const javaTunnels = tunnels.filter((t) => t.tunnel.kind === "java").length;
  const voiceTunnels = tunnels.filter((t) => t.tunnel.kind === "voicechat").length;

  const copy = (text: string, id: string) => {
    navigator.clipboard.writeText(text);
    setCopiedId(id);
    toast.success("Adresse in Zwischenablage kopiert");
    setTimeout(() => setCopiedId(null), 2000);
  };

  return (
    <>
      <AdminSidebar />
      <PageHeader
        title="playit.gg"
        description="Macht Minecraft-Server und Voicechat ohne Router-Portfreigaben über sichere, globale Anycast-Tunnel weltweit erreichbar."
        action={
          <div className="flex gap-2">
            <Button variant="secondary" onClick={() => mutate()} loading={isLoading}>
              <RefreshCw className="h-4 w-4" />
              Aktualisieren
            </Button>
            <a
              href="https://playit.gg/account/agents"
              target="_blank"
              rel="noreferrer"
              className="inline-flex h-9 items-center gap-1.5 rounded-lg border border-white/[0.08] bg-white/[0.04] px-3 text-xs font-medium text-ink transition hover:bg-white/[0.08]"
            >
              <ExternalLink className="h-3.5 w-3.5" />
              playit.gg Dashboard
            </a>
          </div>
        }
      />

      <div className="grid gap-4 sm:grid-cols-2 xl:grid-cols-4">
        <StatCard
          label="playit-Agenten"
          value={`${configuredNodes}/${nodes.length}`}
          icon={<Cable className="h-4 w-4" />}
          tone={configuredNodes === nodes.length && nodes.length > 0 ? "green" : configuredNodes > 0 ? "amber" : "neutral"}
          hint={configuredNodes === 0 ? "Noch kein Node verbunden" : `${runningNodes} Agenten aktiv`}
        />
        <StatCard
          label="Aktive Tunnel"
          value={tunnels.length}
          icon={<Globe className="h-4 w-4" />}
          tone={tunnels.length > 0 ? "blue" : "neutral"}
          hint={`${javaTunnels} Java · ${voiceTunnels} Voicechat`}
        />
        <StatCard
          label="Server mit Tunnel"
          value={new Set(tunnels.map((t) => t.serverId)).size}
          icon={<Server className="h-4 w-4" />}
          tone="violet"
          hint="Erreichbar ohne Portfreigabe"
        />
        <StatCard
          label="Nodes gesamt"
          value={nodes.length}
          icon={<Layers className="h-4 w-4" />}
          tone="neutral"
          hint="Verfügbare Daemon-Instanzen"
        />
      </div>

      <div className="mt-8 space-y-8">
        {/* Node-Agenten */}
        <div>
          <div className="mb-4 flex items-center justify-between">
            <div>
              <h2 className="text-base font-semibold">Node-Agenten</h2>
              <p className="text-xs text-muted">Jeder Node führt einen eigenen playit-Agenten als Docker-Container aus.</p>
            </div>
          </div>

          {nodes.length === 0 ? (
            <Card>
              <EmptyState
                icon={<Cable className="h-6 w-6" />}
                title="Keine Nodes vorhanden"
                text="Richte zuerst unter „Nodes“ mindestens eine Maschine ein."
              />
            </Card>
          ) : (
            <div className="grid gap-4 lg:grid-cols-2">
              {nodes.map((node) => {
                const status = node.status;
                return (
                  <Card key={node.nodeId} className="flex flex-col justify-between p-5">
                    <div>
                      <div className="flex items-start justify-between gap-3">
                        <div className="flex items-center gap-3">
                          <span className="grid h-9 w-9 place-items-center rounded-lg border border-white/[0.08] bg-white/[0.04] text-muted">
                            <Cable className="h-4 w-4" />
                          </span>
                          <div>
                            <p className="text-sm font-semibold">{node.nodeName}</p>
                            <p className="text-xs text-muted">{node.publicHost || "Keine öffentliche Adresse"}</p>
                          </div>
                        </div>
                        <Badge tone={status?.configured ? (status.running ? "good" : "warn") : "neutral"}>
                          <Dot tone={status?.configured ? (status.running ? "good" : "warn") : "neutral"} />
                          {status?.configured ? (status.running ? "Verbunden" : "Gestoppt") : "Nicht eingerichtet"}
                        </Badge>
                      </div>

                      {node.error && (
                        <p className="mt-3 rounded-md border border-bad/25 bg-bad/10 px-3 py-2 text-xs text-bad">
                          {node.error}
                        </p>
                      )}

                      {status?.error && (
                        <p className="mt-3 rounded-md border border-bad/25 bg-bad/10 px-3 py-2 text-xs text-bad">
                          {status.error}
                        </p>
                      )}

                      <dl className="mt-4 grid grid-cols-2 gap-x-4 gap-y-2 text-xs">
                        <dt className="text-muted">Agent-Status</dt>
                        <dd>{status?.running ? "Online (Container läuft)" : status?.configured ? "Pausiert / Gestoppt" : "Nicht konfiguriert"}</dd>
                        <dt className="text-muted">Docker-Image</dt>
                        <dd className="truncate font-mono text-[11px]">{status?.image || "ghcr.io/playit-cloud/playit-agent:0.15"}</dd>
                        <dt className="text-muted">Node-Verbindung</dt>
                        <dd>{node.online ? "Node erreichbar" : "Node offline"}</dd>
                        <dt className="text-muted">Tunnel auf diesem Node</dt>
                        <dd className="font-semibold">{tunnels.filter((t) => t.nodeId === node.nodeId).length}</dd>
                      </dl>

                      {status?.configured && status.logs && status.logs.length > 0 && (
                        <div className="mt-4">
                          <div className="mb-1.5 flex items-center gap-1.5 text-[11px] text-muted">
                            <Terminal className="h-3 w-3" />
                            <span>Agent-Protokoll</span>
                          </div>
                          <pre className="max-h-28 overflow-auto rounded-md bg-black/40 p-2.5 font-mono text-[10px] leading-relaxed text-muted">
                            {status.logs.slice(-10).join("\n")}
                          </pre>
                        </div>
                      )}
                    </div>

                    <div className="mt-5 flex flex-wrap items-center justify-between gap-2 border-t border-white/[0.06] pt-3">
                      <div className="flex gap-1.5">
                        <Button size="sm" variant="secondary" onClick={() => setClaimingNode(node)}>
                          {status?.configured ? "Neu verbinden" : "Mit playit verbinden"}
                        </Button>
                        <Button size="sm" variant="ghost" onClick={() => setEditingNode(node)}>
                          Schlüssel eintragen
                        </Button>
                      </div>
                      {status?.configured && (
                        <Button size="sm" variant="ghost" onClick={() => setRemovingNode(node)}>
                          <Trash2 className="h-3.5 w-3.5" />
                          Entfernen
                        </Button>
                      )}
                    </div>
                  </Card>
                );
              })}
            </div>
          )}
        </div>

        {/* Aktive Server-Tunnel */}
        <div>
          <div className="mb-4 flex items-center justify-between">
            <div>
              <h2 className="text-base font-semibold">Aktive Server-Tunnel</h2>
              <p className="text-xs text-muted">Alle für Minecraft-Server und Simple Voice Chat freigegebenen playit-Tunnel.</p>
            </div>
          </div>

          {tunnels.length === 0 ? (
            <Card>
              <EmptyState
                icon={<Globe className="h-6 w-6" />}
                title="Noch keine Tunnel angelegt"
                text="Aktiviere in den Einstellungen eines Servers unter „Netzwerk & Ports“ playit, um eine öffentliche Beitrittsadresse zu erhalten."
              />
            </Card>
          ) : (
            <div className="overflow-hidden rounded-xl border border-white/[0.08] bg-white/[0.02]">
              <div className="overflow-x-auto">
                <table className="w-full text-left text-xs">
                  <thead className="border-b border-white/[0.08] bg-white/[0.02] text-muted">
                    <tr>
                      <th className="px-4 py-3 font-medium">Server</th>
                      <th className="px-4 py-3 font-medium">Projekt</th>
                      <th className="px-4 py-3 font-medium">Node</th>
                      <th className="px-4 py-3 font-medium">Art</th>
                      <th className="px-4 py-3 font-medium">Öffentliche Adresse</th>
                      <th className="px-4 py-3 font-medium">Status</th>
                      <th className="px-4 py-3 text-right font-medium">Aktion</th>
                    </tr>
                  </thead>
                  <tbody className="divide-y divide-white/[0.06]">
                    {tunnels.map((t) => {
                      const isCopied = copiedId === t.tunnel.id;
                      return (
                        <tr key={t.tunnel.id} className="transition hover:bg-white/[0.02]">
                          <td className="px-4 py-3 font-medium text-ink">
                            <Link href={`/server/${t.serverId}/`} className="hover:text-accent hover:underline">
                              {t.serverName}
                            </Link>
                          </td>
                          <td className="px-4 py-3 text-muted">
                            <Link href={`/project/${t.projectId}/`} className="hover:text-ink hover:underline">
                              {t.projectName || "Projekt"}
                            </Link>
                          </td>
                          <td className="px-4 py-3 text-muted">{t.nodeName}</td>
                          <td className="px-4 py-3">
                            <span className="inline-flex items-center gap-1.5 font-medium">
                              {t.tunnel.kind === "voicechat" ? (
                                <>
                                  <Radio className="h-3.5 w-3.5 text-accent" />
                                  Voicechat (UDP)
                                </>
                              ) : (
                                <>
                                  <Server className="h-3.5 w-3.5 text-good" />
                                  Minecraft (TCP)
                                </>
                              )}
                            </span>
                          </td>
                          <td className="px-4 py-3">
                            {t.tunnel.address ? (
                              <div className="inline-flex items-center gap-2">
                                <span className="font-mono text-[11px] font-semibold text-accent">{t.tunnel.address}</span>
                                <button
                                  onClick={() => copy(t.tunnel.address, t.tunnel.id)}
                                  className="rounded p-1 text-muted transition hover:bg-white/[0.06] hover:text-ink"
                                  title="Adresse kopieren"
                                >
                                  {isCopied ? <Check className="h-3 w-3 text-good" /> : <Copy className="h-3 w-3" />}
                                </button>
                              </div>
                            ) : (
                              <span className="text-muted italic">Adresse wird zugewiesen...</span>
                            )}
                          </td>
                          <td className="px-4 py-3">
                            <Badge tone={t.tunnel.state === "allocated" ? "good" : "warn"}>
                              <Dot tone={t.tunnel.state === "allocated" ? "good" : "warn"} />
                              {t.tunnel.state === "allocated" ? "Bereit" : t.tunnel.state === "pending" ? "Wird zugewiesen" : "Deaktiviert"}
                            </Badge>
                          </td>
                          <td className="px-4 py-3 text-right">
                            <Link
                              href={`/server/${t.serverId}/?tab=settings`}
                              className="inline-flex items-center gap-1 text-[11px] text-muted transition hover:text-ink"
                            >
                              Verwalten
                              <ArrowUpRight className="h-3 w-3" />
                            </Link>
                          </td>
                        </tr>
                      );
                    })}
                  </tbody>
                </table>
              </div>
            </div>
          )}
        </div>
      </div>

      {/* Claim Dialog */}
      {claimingNode && (
        <PlayitClaimDialog
          nodeId={claimingNode.nodeId}
          open={!!claimingNode}
          onClose={() => setClaimingNode(null)}
          onDone={() => {
            setClaimingNode(null);
            toast.success(`Node „${claimingNode.nodeName}“ erfolgreich mit playit verbunden`);
            mutate();
          }}
        />
      )}

      {/* Manual Key Dialog */}
      <Dialog
        open={!!editingNode}
        onClose={() => {
          setEditingNode(null);
          setSecret("");
        }}
        title={`playit-Schlüssel für „${editingNode?.nodeName}“`}
        description="Ein geheimer Schlüssel aus dem playit-Dashboard. Lodestone startet damit den playit-Agenten auf diesem Node."
        footer={
          <>
            <Button
              variant="ghost"
              onClick={() => {
                setEditingNode(null);
                setSecret("");
              }}
            >
              Abbrechen
            </Button>
            <Button
              variant="primary"
              loading={busy}
              disabled={secret.trim().length < 16}
              onClick={async () => {
                if (!editingNode) return;
                setBusy(true);
                const ok = await toast.run(
                  () => api.put(`/nodes/${editingNode.nodeId}/playit`, { secret: secret.trim() }),
                  "playit-Agent gestartet",
                );
                setBusy(false);
                if (ok) {
                  setEditingNode(null);
                  setSecret("");
                  mutate();
                }
              }}
            >
              Speichern und starten
            </Button>
          </>
        }
      >
        <ol className="list-decimal space-y-1 pl-5 text-xs text-muted">
          <li>
            Melde dich auf{" "}
            <a className="text-accent hover:underline" href="https://playit.gg/account/agents" target="_blank" rel="noreferrer">
              playit.gg/account/agents
            </a>{" "}
            an und lege einen neuen Agenten (Typ „Docker“) an.
          </li>
          <li>Kopiere den geheimen Schlüssel (SECRET_KEY) und füge ihn unten ein.</li>
        </ol>
        <div>
          <label className="label">Schlüssel (SECRET_KEY)</label>
          <input
            className="field font-mono text-xs"
            type="password"
            value={secret}
            onChange={(e) => setSecret(e.target.value)}
            placeholder="playit secret key..."
            autoComplete="off"
            autoFocus
          />
        </div>
      </Dialog>

      {/* Remove Confirmation */}
      <ConfirmDialog
        open={!!removingNode}
        onClose={() => setRemovingNode(null)}
        title={`playit-Agent auf „${removingNode?.nodeName}“ entfernen?`}
        text="Der Agent wird gestoppt und der gespeicherte Schlüssel auf dem Node gelöscht. Bestehende Tunnel über diesen Node sind danach nicht mehr erreichbar."
        confirm="Agent entfernen"
        danger
        onConfirm={async () => {
          if (!removingNode) return;
          await toast.run(() => api.delete(`/nodes/${removingNode.nodeId}/playit`), "playit-Agent entfernt");
          setRemovingNode(null);
          mutate();
        }}
      />
    </>
  );
}
