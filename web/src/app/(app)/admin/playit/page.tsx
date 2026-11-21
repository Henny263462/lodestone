"use client";

import Link from "next/link";
import { ArrowUpRight, Cable, Check, Copy, ExternalLink, Globe, Layers, Radio, RefreshCw, Server, Terminal, Trash2 } from "lucide-react";
import { useState } from "react";
import { Badge, Button, Card, Dot, EmptyState, PageHeader, StatCard } from "@/components/ui/primitives";
import { ConfirmDialog, Dialog, useToast } from "@/components/ui/feedback";
import { ErrorState, Loading } from "@/components/ui/async";
import { PlayitClaimDialog } from "@/components/playit-claim";
import { api, useApi } from "@/lib/api";
import type { PlayitNodeOverview, PlayitOverview } from "@/lib/types";
import { t, tp } from "@/lib/i18n";

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
        <ErrorState error={error} retry={() => mutate()} />
      </>
    );
  }

  if (!data) {
    return (
      <>
        <Loading label={t("playit.gg-Übersicht wird geladen")} />
      </>
    );
  }

  const nodes = data.nodes ?? [];
  const tunnels = data.tunnels ?? [];
  const configuredNodes = nodes.filter((n) => n.status?.configured).length;
  const runningNodes = nodes.filter((n) => n.status?.running).length;
  const javaTunnels = tunnels.filter((entry) => entry.tunnel.kind === "java").length;
  const voiceTunnels = tunnels.filter((entry) => entry.tunnel.kind === "voicechat").length;

  const copy = (text: string, id: string) => {
    navigator.clipboard.writeText(text);
    setCopiedId(id);
    toast.success(t("Adresse kopiert"));
    setTimeout(() => setCopiedId(null), 2000);
  };

  return (
    <>
      <PageHeader
        crumbs={[{ label: t("Verwaltung") }, { label: "playit.gg" }]}
        title="playit.gg"
        description={t("Macht Minecraft-Server und Voicechat ohne Portfreigaben im Router über sichere, weltweite Anycast-Tunnel erreichbar.")}
        action={
          <div className="flex flex-wrap gap-2">
            <Button variant="secondary" onClick={() => mutate()} loading={isLoading}>
              <RefreshCw className="h-4 w-4" />
              {t("Neu laden")}
            </Button>
            <a
              href="https://playit.gg/account/agents"
              target="_blank"
              rel="noreferrer"
              className="inline-flex h-9 items-center gap-1.5 rounded-lg border border-white/[0.08] bg-white/[0.04] px-3 text-xs font-medium text-ink transition hover:bg-white/[0.08]"
            >
              <ExternalLink className="h-3.5 w-3.5" />
              {t("playit.gg-Dashboard")}
            </a>
          </div>
        }
      />

      <div className="grid gap-4 sm:grid-cols-2 xl:grid-cols-4">
        <StatCard
          label={t("playit-Agenten")}
          value={`${configuredNodes}/${nodes.length}`}
          icon={<Cable className="h-4 w-4" />}
          tone={configuredNodes === nodes.length && nodes.length > 0 ? "green" : configuredNodes > 0 ? "amber" : "neutral"}
          hint={configuredNodes === 0 ? t("Noch kein Node verbunden") : tp("agentsActive", runningNodes)}
        />
        <StatCard
          label={t("Aktive Tunnel")}
          value={tunnels.length}
          icon={<Globe className="h-4 w-4" />}
          tone={tunnels.length > 0 ? "blue" : "neutral"}
          hint={t("{java} Java · {voice} Voicechat", { java: javaTunnels, voice: voiceTunnels })}
        />
        <StatCard
          label={t("Server mit Tunnel")}
          value={new Set(tunnels.map((entry) => entry.serverId)).size}
          icon={<Server className="h-4 w-4" />}
          tone="violet"
          hint={t("Erreichbar ohne Portfreigabe")}
        />
        <StatCard
          label={t("Nodes gesamt")}
          value={nodes.length}
          icon={<Layers className="h-4 w-4" />}
          tone="neutral"
          hint={t("Verfügbare Daemon-Instanzen")}
        />
      </div>

      <div className="mt-8 space-y-8">
        {/* Node-Agenten */}
        <div>
          <div className="mb-4 flex items-center justify-between">
            <div>
              <h2 className="font-display text-base font-medium tracking-wide">{t("Node-Agenten")}</h2>
              <p className="text-xs text-muted">{t("Jeder Node führt einen eigenen playit-Agenten als Docker-Container aus.")}</p>
            </div>
          </div>

          {nodes.length === 0 ? (
            <Card>
              <EmptyState
                icon={<Cable className="h-6 w-6" />}
                title={t("Keine Nodes vorhanden")}
                text={t("Richte zuerst unter „Nodes“ mindestens eine Maschine ein.")}
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
                            <p className="font-display text-sm font-medium tracking-wide">{node.nodeName}</p>
                            <p className="text-xs text-muted">{node.publicHost || t("Keine öffentliche Adresse")}</p>
                          </div>
                        </div>
                        <Badge tone={status?.configured ? (status.running ? "good" : "warn") : "neutral"}>
                          <Dot tone={status?.configured ? (status.running ? "good" : "warn") : "neutral"} />
                          {status?.configured ? (status.running ? t("Verbunden") : t("Gestoppt")) : t("Nicht eingerichtet")}
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
                        <dt className="text-muted">{t("Agent-Status")}</dt>
                        <dd>{status?.running ? t("Online (Container läuft)") : status?.configured ? t("Pausiert oder gestoppt") : t("Nicht eingerichtet")}</dd>
                        <dt className="text-muted">{t("Docker-Image")}</dt>
                        <dd className="truncate font-mono text-[11px]">{status?.image || "ghcr.io/playit-cloud/playit-agent:0.15"}</dd>
                        <dt className="text-muted">{t("Node-Verbindung")}</dt>
                        <dd>{node.online ? t("Node erreichbar") : t("Node offline")}</dd>
                        <dt className="text-muted">{t("Tunnel auf diesem Node")}</dt>
                        <dd className="font-medium tabular-nums">{tunnels.filter((entry) => entry.nodeId === node.nodeId).length}</dd>
                      </dl>

                      {status?.configured && status.logs && status.logs.length > 0 && (
                        <div className="mt-4">
                          <div className="mb-1.5 flex items-center gap-1.5 text-[11px] text-muted">
                            <Terminal className="h-3 w-3" />
                            <span>{t("Agent-Protokoll")}</span>
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
                          {status?.configured ? t("Neu verbinden") : t("Mit playit verbinden")}
                        </Button>
                        <Button size="sm" variant="ghost" onClick={() => setEditingNode(node)}>
                          {t("Schlüssel eintragen")}
                        </Button>
                      </div>
                      {status?.configured && (
                        <Button size="sm" variant="ghost" onClick={() => setRemovingNode(node)}>
                          <Trash2 className="h-3.5 w-3.5" />
                          {t("Entfernen")}
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
              <h2 className="font-display text-base font-medium tracking-wide">{t("Aktive Server-Tunnel")}</h2>
              <p className="text-xs text-muted">{t("Alle playit-Tunnel für Minecraft-Server und Simple Voice Chat.")}</p>
            </div>
          </div>

          {tunnels.length === 0 ? (
            <Card>
              <EmptyState
                icon={<Globe className="h-6 w-6" />}
                title={t("Noch keine Tunnel angelegt")}
                text={t("Lege bei einem Server unter Einstellungen → Anschlüsse einen playit-Tunnel an, dann bekommt er eine öffentliche Adresse zum Beitreten.")}
              />
            </Card>
          ) : (
            <div className="overflow-hidden rounded-xl border border-white/[0.08] bg-white/[0.02]">
              <div className="overflow-x-auto">
                <table className="w-full text-left text-xs">
                  <thead className="border-b border-white/[0.08] bg-white/[0.02] text-muted">
                    <tr>
                      <th className="px-4 py-3 font-medium">Server</th>
                      <th className="px-4 py-3 font-medium">{t("Projekt")}</th>
                      <th className="px-4 py-3 font-medium">Node</th>
                      <th className="px-4 py-3 font-medium">{t("Art")}</th>
                      <th className="px-4 py-3 font-medium">{t("Öffentliche Adresse")}</th>
                      <th className="px-4 py-3 font-medium">Status</th>
                      <th className="px-4 py-3 text-right font-medium">{t("Aktion")}</th>
                    </tr>
                  </thead>
                  <tbody className="divide-y divide-white/[0.06]">
                    {tunnels.map((entry) => {
                      const isCopied = copiedId === entry.tunnel.id;
                      return (
                        <tr key={entry.tunnel.id} className="transition hover:bg-white/[0.02]">
                          <td className="px-4 py-3 font-medium text-ink">
                            <Link href={`/server/${entry.serverId}/`} className="hover:text-accent hover:underline">
                              {entry.serverName}
                            </Link>
                          </td>
                          <td className="px-4 py-3 text-muted">
                            <Link href={`/project/${entry.projectId}/`} className="hover:text-ink hover:underline">
                              {entry.projectName || t("Projekt")}
                            </Link>
                          </td>
                          <td className="px-4 py-3 text-muted">{entry.nodeName}</td>
                          <td className="px-4 py-3">
                            <span className="inline-flex items-center gap-1.5 font-medium">
                              {entry.tunnel.kind === "voicechat" ? (
                                <>
                                  <Radio className="h-3.5 w-3.5 text-accent" />
                                  {t("Voicechat (UDP)")}
                                </>
                              ) : (
                                <>
                                  <Server className="h-3.5 w-3.5 text-good" />
                                  {t("Minecraft (TCP)")}
                                </>
                              )}
                            </span>
                          </td>
                          <td className="px-4 py-3">
                            {entry.tunnel.address ? (
                              <div className="inline-flex items-center gap-2">
                                <span className="font-mono text-[11px] text-accent">{entry.tunnel.address}</span>
                                <button
                                  onClick={() => copy(entry.tunnel.address, entry.tunnel.id)}
                                  className="rounded-sm p-1 text-muted transition hover:bg-white/[0.06] hover:text-ink"
                                  title={t("Adresse kopieren")}
                                  aria-label={t("Adresse kopieren")}
                                >
                                  {isCopied ? <Check className="h-3 w-3 text-good" /> : <Copy className="h-3 w-3" />}
                                </button>
                              </div>
                            ) : (
                              <span className="italic text-muted">{t("Adresse wird zugewiesen …")}</span>
                            )}
                          </td>
                          <td className="px-4 py-3">
                            <Badge tone={entry.tunnel.state === "allocated" ? "good" : "warn"}>
                              <Dot tone={entry.tunnel.state === "allocated" ? "good" : "warn"} />
                              {entry.tunnel.state === "allocated" ? t("Bereit") : entry.tunnel.state === "pending" ? t("Wird zugewiesen") : t("Deaktiviert")}
                            </Badge>
                          </td>
                          <td className="px-4 py-3 text-right">
                            <Link
                              href={`/server/${entry.serverId}/settings/?section=ports`}
                              className="inline-flex items-center gap-1 text-[11px] text-muted transition hover:text-ink"
                            >
                              {t("Verwalten")}
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
            toast.success(t("Node „{name}“ ist mit playit verbunden", { name: claimingNode.nodeName }));
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
        title={t("playit-Schlüssel für „{name}“", { name: editingNode?.nodeName })}
        description={t("Ein geheimer Schlüssel aus dem playit-Dashboard. Lodestone startet damit den playit-Agenten auf diesem Node.")}
        footer={
          <>
            <Button
              variant="ghost"
              onClick={() => {
                setEditingNode(null);
                setSecret("");
              }}
            >
              {t("Abbrechen")}
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
                  t("playit-Agent gestartet"),
                );
                setBusy(false);
                if (ok) {
                  setEditingNode(null);
                  setSecret("");
                  mutate();
                }
              }}
            >
              {t("Speichern und starten")}
            </Button>
          </>
        }
      >
        <ol className="list-decimal space-y-1 pl-5 text-xs text-muted">
          <li>
            {t("Melde dich an auf")}{" "}
            <a className="text-accent hover:underline" href="https://playit.gg/account/agents" target="_blank" rel="noreferrer">
              playit.gg/account/agents
            </a>{" "}
            {t("und lege einen neuen Agenten (Typ „Docker“) an.")}
          </li>
          <li>{t("Kopiere den geheimen Schlüssel (SECRET_KEY) und füge ihn unten ein.")}</li>
        </ol>
        <div>
          <label className="label">{t("Schlüssel (SECRET_KEY)")}</label>
          <input
            className="field font-mono text-xs"
            type="password"
            value={secret}
            onChange={(e) => setSecret(e.target.value)}
            placeholder={t("Geheimer Schlüssel von playit")}
            autoComplete="off"
            autoFocus
          />
        </div>
      </Dialog>

      {/* Remove Confirmation */}
      <ConfirmDialog
        open={!!removingNode}
        onClose={() => setRemovingNode(null)}
        title={t("playit-Agent auf „{name}“ entfernen?", { name: removingNode?.nodeName })}
        text={t("Der Agent wird gestoppt und der gespeicherte Schlüssel auf dem Node gelöscht. Bestehende Tunnel über diesen Node sind danach nicht mehr erreichbar.")}
        confirm={t("Agent entfernen")}
        danger
        onConfirm={async () => {
          if (!removingNode) return;
          await toast.run(() => api.delete(`/nodes/${removingNode.nodeId}/playit`), t("playit-Agent entfernt"));
          setRemovingNode(null);
          mutate();
        }}
      />
    </>
  );
}
