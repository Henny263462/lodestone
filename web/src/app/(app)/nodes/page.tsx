"use client";

import { ArrowUpCircle, Cable, Cpu, ExternalLink, Pencil, Plus, RefreshCw, Trash2 } from "lucide-react";
import { useState } from "react";
import { Badge, Button, Card, CardHeader, Dot, EmptyState, Meter, PageHeader, Switch } from "@/components/ui/primitives";
import { ConfirmDialog, Dialog, useToast } from "@/components/ui/feedback";
import { ErrorState, Loading } from "@/components/ui/async";
import { useUser } from "@/components/layout/shell";
import { api, useApi } from "@/lib/api";
import { ago, bytes } from "@/lib/format";
import type { NodeInfo, PlayitStatus, UpdateStatus } from "@/lib/types";

export default function NodesPage() {
  const user = useUser();
  const { data, error, mutate } = useApi<NodeInfo[]>("/nodes", { refreshInterval: 10000 });
  const [editing, setEditing] = useState<NodeInfo | "new" | null>(null);
  const [removing, setRemoving] = useState<NodeInfo | null>(null);
  const [updatingNode, setUpdatingNode] = useState<NodeInfo | null>(null);
  const toast = useToast();

  if (error) return <ErrorState error={error} retry={() => mutate()} />;
  if (!data) return <Loading />;

  return (
    <>
      <PageHeader
        title="Nodes"
        description="Maschinen mit Docker, auf denen die Server laufen. Auf jeder läuft der Lodestone-Node-Daemon."
        action={
          user.isAdmin && (
            <Button variant="primary" onClick={() => setEditing("new")}>
              <Plus className="h-4 w-4" />
              Node hinzufügen
            </Button>
          )
        }
      />
      {user.isAdmin && <UpdatesCard onMutateNodes={() => mutate()} />}
      {data.length === 0 ? (
        <Card>
          <EmptyState icon={<Cpu className="h-6 w-6" />} title="Noch kein Node" text={user.isAdmin ? "Starte den Node-Daemon auf einer Maschine und trage hier Adresse und Token ein." : "Ein Administrator muss einen Node einrichten."} />
        </Card>
      ) : (
        <div className="grid gap-4 lg:grid-cols-2">
          {data.map((node) => (
            <Card key={node.id} className="p-5">
              <div className="flex items-start justify-between gap-3">
                <div className="flex items-center gap-3">
                  <span className="grid h-9 w-9 place-items-center rounded-lg border border-white/[0.08] bg-white/[0.04] text-muted">
                    <Cpu className="h-4 w-4" />
                  </span>
                  <div>
                    <p className="text-sm font-semibold">{node.name}</p>
                    <p className="text-xs text-muted">{node.publicHost || "keine öffentliche Adresse"}</p>
                  </div>
                </div>
                <Badge tone={node.online ? "good" : "bad"}>
                  <Dot tone={node.online ? "good" : "bad"} />
                  {node.online ? "Online" : "Offline"}
                </Badge>
              </div>
              {node.online && node.system ? (
                <dl className="mt-4 grid grid-cols-2 gap-x-4 gap-y-2 text-xs">
                  <dt className="text-muted">Version</dt>
                  <dd className="flex items-center gap-1.5">
                    <span>{node.version ? `v${node.version}` : "–"}</span>
                    {node.updateAvailable && (
                      <Badge tone="warn">Update verfügbar</Badge>
                    )}
                  </dd>
                  <dt className="text-muted">Docker</dt>
                  <dd>{node.system.dockerVersion}</dd>
                  <dt className="text-muted">System</dt>
                  <dd>
                    {node.system.os} · {node.system.architecture}
                  </dd>
                  <dt className="text-muted">Kerne / RAM</dt>
                  <dd>
                    {node.system.cpus} · {bytes(node.system.memoryBytes)}
                  </dd>
                  <dt className="text-muted">Server</dt>
                  <dd>{node.serverCount}</dd>
                </dl>
              ) : (
                node.error && <p className="mt-4 rounded-md border border-bad/25 bg-bad/10 px-3 py-2 text-xs text-bad">{node.error}</p>
              )}
              {user.isAdmin && node.online && node.updateAvailable && (
                <div className="mt-3 flex items-center justify-between gap-2 rounded-lg border border-warn/25 bg-warn/10 px-3 py-2 text-xs">
                  <span className="text-warn">Ein Update ist verfügbar.</span>
                  <Button variant="secondary" size="sm" onClick={() => setUpdatingNode(node)}>
                    <ArrowUpCircle className="h-3.5 w-3.5" />
                    Aktualisieren
                  </Button>
                </div>
              )}
              {node.online && node.system && <MemoryPlanner node={node} />}
              {user.isAdmin && node.online && <PlayitCard node={node} />}
              {user.isAdmin && (
                <div className="mt-4 flex items-center justify-between gap-2 border-t border-white/[0.06] pt-3">
                  <span className="truncate font-mono text-[11px] text-muted">{node.url}</span>
                  <div className="flex gap-1">
                    <Button variant="ghost" size="icon-sm" onClick={() => setEditing(node)} aria-label="Bearbeiten">
                      <Pencil className="h-3.5 w-3.5" />
                    </Button>
                    <Button variant="ghost" size="icon-sm" onClick={() => setRemoving(node)} aria-label="Entfernen">
                      <Trash2 className="h-3.5 w-3.5" />
                    </Button>
                  </div>
                </div>
              )}
            </Card>
          ))}
        </div>
      )}
      <NodeDialog node={editing} onClose={() => setEditing(null)} onDone={() => mutate()} />
      <ConfirmDialog
        open={!!updatingNode}
        onClose={() => setUpdatingNode(null)}
        title={`Node „${updatingNode?.name}“ aktualisieren?`}
        text="Der Node-Daemon wird auf das neueste Docker-Image aktualisiert. Laufende Minecraft-Server auf diesem Node laufen ungestört weiter."
        confirm="Aktualisieren"
        onConfirm={async () => {
          if (!updatingNode) return;
          await toast.run(() => api.post(`/nodes/${updatingNode.id}/update`), "Node-Update gestartet");
          mutate();
        }}
      />
      <ConfirmDialog
        open={!!removing}
        onClose={() => setRemoving(null)}
        title={`„${removing?.name}“ entfernen?`}
        text="Der Node wird aus dem Panel entfernt. Das geht nur, wenn kein Server mehr auf ihm liegt. Der Daemon selbst wird nicht angehalten."
        confirm="Entfernen"
        danger
        onConfirm={async () => {
          if (!removing) return;
          await toast.run(() => api.delete(`/nodes/${removing.id}`), "Node entfernt");
          mutate();
        }}
      />
    </>
  );
}

/** Zeigt, wie viel Arbeitsspeicher die Server zusagen (Limits) und wie viel der Node tatsächlich noch frei hat. */
function MemoryPlanner({ node }: { node: NodeInfo }) {
  const total = node.system?.memoryBytes ?? 0;
  const committed = node.memoryCommitted ?? 0;
  const available = node.memoryAvailable ?? 0;
  if (!total) return null;
  const used = Math.max(0, total - available);
  const committedPercent = (committed / total) * 100;
  const usedPercent = (used / total) * 100;
  return (
    <div className="mt-4 space-y-3 border-t border-white/[0.06] pt-3">
      <div>
        <div className="mb-1.5 flex justify-between text-[11px] text-muted">
          <span>Zugesagt (Limits laufender Server)</span>
          <span className="tabular-nums text-ink/80">
            {bytes(committed)} von {bytes(total)}
          </span>
        </div>
        <Meter value={committedPercent} max={100} tone={committedPercent > 100 ? "bad" : committedPercent > 85 ? "warn" : "good"} />
      </div>
      <div>
        <div className="mb-1.5 flex justify-between text-[11px] text-muted">
          <span>Tatsächlich belegt</span>
          <span className="tabular-nums text-ink/80">
            {bytes(used)} · {bytes(available)} frei
          </span>
        </div>
        <Meter value={usedPercent} max={100} tone={usedPercent > 90 ? "bad" : usedPercent > 75 ? "warn" : "good"} />
      </div>
      {committedPercent > 100 && <p className="text-[11px] text-warn">Die Limits überschreiten den Speicher des Nodes. Das geht gut, solange nicht alle Server voll ausgelastet sind.</p>}
    </div>
  );
}

/** playit.gg: macht Server ohne Portfreigabe im Router erreichbar. Der Node startet den Agenten mit dem Schlüssel. */
function PlayitCard({ node }: { node: NodeInfo }) {
  const toast = useToast();
  const { data, mutate } = useApi<PlayitStatus>(`/nodes/${node.id}/playit`, { refreshInterval: 8000 });
  const [open, setOpen] = useState(false);
  const [secret, setSecret] = useState("");
  const [busy, setBusy] = useState(false);
  const [removing, setRemoving] = useState(false);

  return (
    <div className="mt-4 border-t border-white/[0.06] pt-3">
      <div className="flex items-center justify-between gap-2">
        <span className="flex items-center gap-2 text-xs">
          <Cable className="h-3.5 w-3.5 text-muted" />
          playit.gg
          {data?.configured ? <Badge tone={data.running ? "good" : "warn"}>{data.running ? "Verbunden" : "Gestoppt"}</Badge> : <Badge>Nicht eingerichtet</Badge>}
        </span>
        <div className="flex gap-1">
          <Button size="sm" variant="ghost" onClick={() => setOpen(true)}>
            {data?.configured ? "Schlüssel ändern" : "Einrichten"}
          </Button>
          {data?.configured && (
            <Button size="sm" variant="ghost" onClick={() => setRemoving(true)}>
              Entfernen
            </Button>
          )}
        </div>
      </div>
      {data?.error && <p className="mt-2 text-xs text-bad">{data.error}</p>}
      {data?.configured && data.logs.length > 0 && <pre className="mt-2 max-h-28 overflow-auto rounded-md bg-black/30 p-2 font-mono text-[10px] leading-relaxed text-muted">{data.logs.slice(-12).join("\n")}</pre>}
      <Dialog
        open={open}
        onClose={() => setOpen(false)}
        title="playit.gg einrichten"
        description="Spieler verbinden sich über eine playit-Adresse, du brauchst keine Portfreigabe im Router."
        footer={
          <>
            <Button variant="ghost" onClick={() => setOpen(false)}>
              Abbrechen
            </Button>
            <Button
              variant="primary"
              loading={busy}
              disabled={secret.trim().length < 16}
              onClick={async () => {
                setBusy(true);
                const ok = await toast.run(() => api.put(`/nodes/${node.id}/playit`, { secret: secret.trim() }), "playit-Agent gestartet");
                setBusy(false);
                if (ok) {
                  setOpen(false);
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
              playit.gg
            </a>{" "}
            an und lege einen neuen Agenten an (Typ „Docker“).
          </li>
          <li>Kopiere den Geheimschlüssel (SECRET_KEY) hierher.</li>
          <li>Tunnel musst du nicht selbst anlegen: Beim Server unter Einstellungen → playit-Tunnel legt Lodestone sie für Minecraft Java und Voicechat an.</li>
        </ol>
        <div>
          <label className="label">Schlüssel</label>
          <input className="field font-mono text-xs" type="password" value={secret} onChange={(event) => setSecret(event.target.value)} autoComplete="off" />
        </div>
      </Dialog>
      <ConfirmDialog
        open={removing}
        onClose={() => setRemoving(false)}
        title="playit-Agent entfernen?"
        text="Der Agent wird gestoppt und der Schlüssel vergessen. Die Tunnel-Adressen funktionieren dann nicht mehr."
        confirm="Entfernen"
        danger
        onConfirm={async () => {
          await toast.run(() => api.delete(`/nodes/${node.id}/playit`), "Agent entfernt");
          mutate();
        }}
      />
    </div>
  );
}

function NodeDialog({ node, onClose, onDone }: { node: NodeInfo | "new" | null; onClose: () => void; onDone: () => void }) {
  const toast = useToast();
  const existing = node && node !== "new" ? node : null;
  const [form, setForm] = useState({ name: "", url: "", token: "", publicHost: "" });
  const [busy, setBusy] = useState(false);
  const [seen, setSeen] = useState<NodeInfo | "new" | null>(null);

  if (node !== seen) {
    setSeen(node);
    setForm({ name: existing?.name ?? "", url: existing?.url ?? "", token: "", publicHost: existing?.publicHost ?? "" });
  }

  const set = (key: keyof typeof form) => (event: React.ChangeEvent<HTMLInputElement>) => setForm((current) => ({ ...current, [key]: event.target.value }));

  return (
    <Dialog
      open={!!node}
      onClose={onClose}
      title={existing ? "Node bearbeiten" : "Node hinzufügen"}
      footer={
        <>
          <Button variant="ghost" onClick={onClose}>
            Abbrechen
          </Button>
          <Button
            variant="primary"
            loading={busy}
            disabled={!form.name.trim() || !form.url.trim() || (!existing && !form.token.trim())}
            onClick={async () => {
              setBusy(true);
              const ok = await toast.run(() => (existing ? api.patch(`/nodes/${existing.id}`, form) : api.post("/nodes", form)), existing ? "Gespeichert" : "Node hinzugefügt");
              setBusy(false);
              if (ok) {
                onClose();
                onDone();
              }
            }}
          >
            {existing ? "Speichern" : "Hinzufügen"}
          </Button>
        </>
      }
    >
      <div>
        <label className="label">Name</label>
        <input className="field" value={form.name} onChange={set("name")} placeholder="home" maxLength={48} autoFocus />
      </div>
      <div>
        <label className="label">Adresse des Daemons</label>
        <input className="field font-mono text-xs" value={form.url} onChange={set("url")} placeholder="http://node.example.org:8480" />
      </div>
      <div>
        <label className="label">Token {existing && "(leer lassen, um es zu behalten)"}</label>
        <input className="field font-mono text-xs" type="password" value={form.token} onChange={set("token")} autoComplete="off" />
      </div>
      <div>
        <label className="label">Öffentliche Adresse (für Spieler)</label>
        <input className="field" value={form.publicHost} onChange={set("publicHost")} placeholder="mc.example.org" />
        <p className="mt-1 text-xs text-muted">Wird für die Beitritts-Adresse und die Voicechat-Konfiguration verwendet.</p>
      </div>
    </Dialog>
  );
}

function UpdatesCard({ onMutateNodes }: { onMutateNodes: () => void }) {
  const toast = useToast();
  const { data, mutate } = useApi<UpdateStatus>("/updates", { refreshInterval: 15000 });
  const [checking, setChecking] = useState(false);
  const [toggling, setToggling] = useState(false);
  const [updatingPanel, setUpdatingPanel] = useState(false);
  const [updatingAllNodes, setUpdatingAllNodes] = useState(false);

  if (!data) return null;

  const nodeUpdatesCount = data.nodes?.filter((n) => n.updateAvailable).length ?? 0;

  return (
    <>
      <Card className="mb-6">
        <CardHeader
          title="Updates"
          description="Versionsstand von Panel und Nodes verwalten und automatische Aktualisierungen konfigurieren."
          action={
            <div className="flex items-center gap-2">
              <span className="text-xs text-muted">Zuletzt geprüft: {ago(data.lastChecked)}</span>
              <Button
                variant="ghost"
                size="sm"
                loading={checking}
                onClick={async () => {
                  setChecking(true);
                  await toast.run(() => api.post("/updates/check"), "Nach Updates gesucht");
                  setChecking(false);
                  mutate();
                  onMutateNodes();
                }}
              >
                <RefreshCw className="h-3.5 w-3.5" />
                Prüfen
              </Button>
            </div>
          }
        />
        <div className="space-y-4 p-5">
          <div className="flex flex-col gap-4 sm:flex-row sm:items-center sm:justify-between">
            <div>
              <div className="flex items-center gap-2">
                <span className="text-sm font-semibold">Lodestone Panel</span>
                <span className="font-mono text-xs text-muted">v{data.panelVersion}</span>
                {data.updateAvailable ? (
                  <Badge tone="warn">Update auf v{data.latestVersion} verfügbar</Badge>
                ) : (
                  <Badge tone="good">Aktuell</Badge>
                )}
              </div>
              {data.updateAvailable && (
                <div className="mt-1 flex flex-wrap items-center gap-2 text-xs text-muted">
                  {data.releaseTitle && <span>{data.releaseTitle}</span>}
                  {data.releaseUrl && (
                    <a
                      className="inline-flex items-center gap-1 text-accent hover:underline"
                      href={data.releaseUrl}
                      target="_blank"
                      rel="noreferrer"
                    >
                      Release Notes
                      <ExternalLink className="h-3 w-3" />
                    </a>
                  )}
                </div>
              )}
            </div>
            <div className="flex flex-wrap items-center gap-2">
              {data.updateAvailable && (
                <Button variant="primary" size="sm" onClick={() => setUpdatingPanel(true)}>
                  <ArrowUpCircle className="h-4 w-4" />
                  Panel aktualisieren
                </Button>
              )}
              {nodeUpdatesCount > 0 && (
                <Button variant="secondary" size="sm" onClick={() => setUpdatingAllNodes(true)}>
                  <ArrowUpCircle className="h-4 w-4" />
                  Alle Nodes aktualisieren ({nodeUpdatesCount})
                </Button>
              )}
            </div>
          </div>

          <div className="flex items-center justify-between border-t border-white/[0.06] pt-3">
            <div>
              <p className="text-xs font-medium text-ink">Automatische Updates</p>
              <p className="text-[11px] text-muted">
                Sucht im Hintergrund regelmäßig nach Updates und installiert diese automatisch.
              </p>
            </div>
            <Switch
              checked={data.autoUpdate}
              disabled={toggling}
              onChange={async (value) => {
                setToggling(true);
                const ok = await toast.run(
                  () => api.put("/updates/settings", { autoUpdate: value }),
                  value ? "Automatische Updates aktiviert" : "Automatische Updates deaktiviert",
                );
                setToggling(false);
                if (ok) mutate();
              }}
            />
          </div>
        </div>
      </Card>

      <ConfirmDialog
        open={updatingPanel}
        onClose={() => setUpdatingPanel(false)}
        title="Panel auf neuere Version aktualisieren?"
        text="Der Panel-Container wird gestoppt, das neue Image heruntergeladen und das Panel neu gestartet. Das dauert etwa 10 bis 30 Sekunden. Laufende Minecraft-Server bleiben unberührt."
        confirm="Jetzt aktualisieren"
        onConfirm={async () => {
          await toast.run(() => api.post("/panel/update"), "Panel-Update gestartet");
          mutate();
        }}
      />

      <ConfirmDialog
        open={updatingAllNodes}
        onClose={() => setUpdatingAllNodes(false)}
        title={`Alle ${nodeUpdatesCount} Nodes aktualisieren?`}
        text="Die Node-Daemons werden nacheinander mit dem neuesten Docker-Image neu gestartet. Laufende Minecraft-Server auf den Nodes laufen ungestört weiter."
        confirm="Alle aktualisieren"
        onConfirm={async () => {
          await toast.run(() => api.post("/nodes/update-all"), "Updates für alle Nodes gestartet");
          mutate();
          onMutateNodes();
        }}
      />
    </>
  );
}
