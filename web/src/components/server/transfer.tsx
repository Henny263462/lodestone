"use client";

import Link from "next/link";
import { ArrowRightLeft, Copy, Loader2, Truck } from "lucide-react";
import { useState } from "react";
import { Badge, Button, Card, CardHeader, Checkbox, Meter } from "@/components/ui/primitives";
import { Dialog, useToast } from "@/components/ui/feedback";
import { ErrorState, Loading } from "@/components/ui/async";
import { api, useApi } from "@/lib/api";
import { usePoll } from "@/lib/live";
import { ago, bytes } from "@/lib/format";
import { can, type NodeInfo, type NodeStorage, type Permission, type Project, type Server, type ServerOp, type ServerOpState } from "@/lib/types";

const stateBadge: Record<ServerOpState, { tone: "info" | "good" | "bad" | "neutral"; label: string }> = {
  running: { tone: "info", label: "Läuft" },
  done: { tone: "good", label: "Fertig" },
  failed: { tone: "bad", label: "Fehlgeschlagen" },
  canceled: { tone: "neutral", label: "Abgebrochen" },
};

/** Kopieren und Umziehen eines Servers (Server → Einstellungen). Kopieren braucht zusätzlich „Dateien ansehen“, Umziehen „Starten und Stoppen“. */
export function TransferCard({ server, permissions }: { server: Server; permissions: Permission[] }) {
  const { data: project } = useApi<Project>(`/projects/${server.projectId}`);
  const [cloning, setCloning] = useState(false);
  const [moving, setMoving] = useState(false);
  const network = project?.kind === "network";
  const canClone = can(permissions, "servers", "files.read");
  const canMove = can(permissions, "servers", "power");
  const cloneBlocked = server.groupId ? "Instanzen einer Gruppe legt der Autoscaler selbst an." : server.role === "proxy" ? "Ein Netzwerk hat genau einen Proxy." : "";

  return (
    <Card>
      <CardHeader
        title="Kopieren und umziehen"
        description="Kopie auf demselben Node oder Umzug auf einen anderen. Beides läuft im Hintergrund."
      />
      <div className="space-y-3 px-5 py-4">
        <div className="flex flex-wrap gap-2">
          <Button disabled={!project || !!cloneBlocked || !canClone} title={cloneBlocked || (canClone ? undefined : "Dafür fehlt dir das Recht „Dateien ansehen“")} onClick={() => setCloning(true)}>
            <Copy className="h-4 w-4" />
            Kopieren
          </Button>
          <Button disabled={!project || !canMove} title={canMove ? undefined : "Dafür fehlt dir das Recht „Starten und Stoppen“"} onClick={() => setMoving(true)}>
            <Truck className="h-4 w-4" />
            {network ? "Netzwerk umziehen" : "Auf anderen Node umziehen"}
          </Button>
        </div>
        {cloneBlocked && <p className="text-xs text-muted">Kopieren geht hier nicht: {cloneBlocked}</p>}
        {network && <p className="text-xs text-muted">Ein Netzwerk liegt immer auf einem Node, damit Proxy und Server sich im Docker-Netz finden. Es zieht deshalb nur als Ganzes um.</p>}
      </div>
      {project && <OpsList project={project} />}
      {project && <CloneDialog open={cloning} onClose={() => setCloning(false)} server={server} project={project} />}
      {project && <MoveDialog open={moving} onClose={() => setMoving(false)} project={project} />}
    </Card>
  );
}

/** Umzug eines ganzen Projekts (Projekt → Einstellungen). */
export function ProjectTransferCard({ project }: { project: Project }) {
  const [moving, setMoving] = useState(false);
  const nodes = [...new Set(project.servers.map((server) => server.nodeName))];
  const canMove = can(project.myPermissions, "servers", "power");
  return (
    <Card>
      <CardHeader
        title="Auf anderen Node umziehen"
        description={
          project.kind === "network"
            ? "Ein Netzwerk liegt immer auf einem Node, damit Proxy und Server sich im Docker-Netz finden. Es zieht deshalb als Ganzes um."
            : "Der Server zieht samt Welten, Plugins und Einstellungen auf eine andere Maschine."
        }
        action={
          <Button size="sm" disabled={project.servers.length === 0 || !canMove} title={canMove ? undefined : "Dafür fehlen dir die Rechte „Server anlegen und löschen“ und „Starten und Stoppen“"} onClick={() => setMoving(true)}>
            <Truck className="h-3.5 w-3.5" />
            Umziehen
          </Button>
        }
      />
      {nodes.length > 0 && (
        <p className="border-b border-white/[0.06] px-5 py-3 text-xs text-muted">
          Liegt auf: <span className="text-ink/80">{nodes.join(", ")}</span>
        </p>
      )}
      <OpsList project={project} />
      <MoveDialog open={moving} onClose={() => setMoving(false)} project={project} />
    </Card>
  );
}

function CloneDialog({ open, onClose, server, project }: { open: boolean; onClose: () => void; server: Server; project: Project }) {
  const toast = useToast();
  const [name, setName] = useState(`${server.name} Kopie`);
  const [worlds, setWorlds] = useState(true);
  const [busy, setBusy] = useState(false);
  const { mutate } = useApi<ServerOp[]>(`/projects/${project.id}/server-ops`);

  return (
    <Dialog
      open={open}
      onClose={onClose}
      title={`„${server.name}“ kopieren`}
      description="Die Kopie entsteht auf demselben Node mit neuen Ports. Läuft der Server, wird er dafür kurz am Speichern gehindert, wie beim Backup."
      footer={
        <>
          <Button variant="ghost" onClick={onClose}>
            Abbrechen
          </Button>
          <Button
            variant="primary"
            loading={busy}
            disabled={!name.trim()}
            onClick={async () => {
              setBusy(true);
              const ok = await toast.run(() => api.post(`/servers/${server.id}/clone`, { name: name.trim(), withWorlds: worlds }), "Kopie wird angelegt");
              setBusy(false);
              if (ok) {
                onClose();
                mutate();
              }
            }}
          >
            Kopieren
          </Button>
        </>
      }
    >
      <div>
        <label className="label">Name der Kopie</label>
        <input className="field" value={name} onChange={(event) => setName(event.target.value)} maxLength={48} autoFocus />
      </div>
      <Checkbox checked={worlds} onChange={setWorlds} label="Welten mitkopieren" />
      <p className="text-xs text-muted">Ohne Welten entsteht beim ersten Start eine neue. Plugins, Mods und Configs kommen immer mit, Backups nicht.</p>
      {project.kind === "network" ? (
        <p className="rounded-lg border border-info/25 bg-info/10 px-4 py-3 text-xs text-info">
          Die Kopie kommt als Backend ins Netzwerk, aber noch nicht in den Try-Ablauf des Proxys. Schalte sie im Tab „Netzwerk“ ein, wenn sie fertig eingerichtet ist.
        </p>
      ) : (
        <p className="rounded-lg border border-info/25 bg-info/10 px-4 py-3 text-xs text-info">
          Ein Server-Projekt hat genau einen Server. Die Kopie bekommt deshalb ein eigenes Projekt mit dir als Eigentümer; Mitglieder und Zeitpläne kommen nicht mit.
        </p>
      )}
    </Dialog>
  );
}

function MoveDialog({ open, onClose, project }: { open: boolean; onClose: () => void; project: Project }) {
  const toast = useToast();
  const { data: nodes, error } = useApi<NodeInfo[]>(open ? "/nodes" : null);
  const { mutate } = useApi<ServerOp[]>(`/projects/${project.id}/server-ops`);
  const current = project.servers[0]?.nodeId;
  const candidates = (nodes ?? []).filter((entry) => entry.id !== current);
  const [target, setTarget] = useState("");
  const [busy, setBusy] = useState(false);
  const chosen = target || candidates.find((entry) => entry.online)?.id || "";
  const { data: storage } = useApi<NodeStorage>(open && chosen ? `/nodes/${chosen}/storage` : null);
  const tunnels = project.servers.filter((server) => server.publicAddress || server.publicVoiceAddress);
  const count = project.servers.length;

  return (
    <Dialog
      open={open}
      onClose={onClose}
      title={`„${project.name}“ umziehen`}
      description={count > 1 ? `Alle ${count} Server des Netzwerks ziehen zusammen um.` : "Der Server zieht samt Welten, Plugins und Einstellungen um."}
      footer={
        <>
          <Button variant="ghost" onClick={onClose}>
            Abbrechen
          </Button>
          <Button
            variant="primary"
            loading={busy}
            disabled={!chosen}
            onClick={async () => {
              setBusy(true);
              const ok = await toast.run(() => api.post(`/projects/${project.id}/migrate`, { nodeId: chosen }), "Umzug gestartet");
              setBusy(false);
              if (ok) {
                onClose();
                mutate();
              }
            }}
          >
            <ArrowRightLeft className="h-4 w-4" />
            Umzug starten
          </Button>
        </>
      }
    >
      {error ? (
        <ErrorState error={error} />
      ) : !nodes ? (
        <Loading />
      ) : candidates.length === 0 ? (
        <p className="text-sm text-muted">Es gibt keinen anderen Node. Ein Administrator muss unter „Nodes“ erst einen zweiten einrichten.</p>
      ) : (
        <>
          <div>
            <label className="label">Ziel-Node</label>
            <select className="field" value={chosen} onChange={(event) => setTarget(event.target.value)}>
              {candidates.map((entry) => (
                <option key={entry.id} value={entry.id} disabled={!entry.online}>
                  {entry.name}
                  {entry.online ? "" : " (nicht erreichbar)"}
                </option>
              ))}
            </select>
            {storage?.online && storage.total > 0 && (
              <p className="mt-1 text-xs text-muted">
                Frei: {bytes(storage.free)} von {bytes(storage.total)}
              </p>
            )}
          </div>
          <ol className="list-decimal space-y-1 pl-5 text-xs text-muted">
            <li>Laufende Server werden gestoppt.</li>
            <li>Die Dateien werden vom alten zum neuen Node übertragen; die Server bekommen dort neue Ports.</li>
            <li>Erst wenn alles angekommen ist, wird auf dem alten Node gelöscht. Geht etwas schief oder brichst du ab, bleibt alles, wie es war.</li>
            <li>Server, die liefen, starten auf dem neuen Node wieder.</li>
          </ol>
          {tunnels.length > 0 && (
            <p className="rounded-lg border border-warn/25 bg-warn/10 px-4 py-3 text-xs text-warn">
              {tunnels.map((server) => server.name).join(", ")} {tunnels.length === 1 ? "hat" : "haben"} eine öffentliche Adresse (z. B. einen playit-Tunnel). Sie gehört zur alten Maschine und wird beim
              Umzug entfernt. Lege danach auf dem neuen Node einen neuen Tunnel an.
            </p>
          )}
          <p className="text-xs text-muted">Spieler erreichen den Server danach unter der Adresse des neuen Nodes; Backups bleiben auf dem alten Node und werden mit dem Server dort gelöscht.</p>
        </>
      )}
    </Dialog>
  );
}

/** Die letzten Kopien und Umzüge des Projekts mit Fortschritt. */
function OpsList({ project }: { project: Project }) {
  const toast = useToast();
  // Fortschritt laufender Vorgänge kommt über den Live-Kanal; das kurze Intervall bleibt, solange einer läuft
  const poll = usePoll(120000, 15000);
  const { data, error, mutate } = useApi<ServerOp[]>(`/projects/${project.id}/server-ops`, {
    refreshInterval: (latest) => (latest?.some((op) => op.state === "running") ? 1500 : poll),
  });
  const canCancel = can(project.myPermissions, "servers");

  if (error) return <ErrorState inline error={error} retry={() => mutate()} />;
  if (!data) return <Loading />;
  if (data.length === 0) return <p className="px-5 py-4 text-xs text-muted">Noch keine Kopie und kein Umzug in diesem Projekt.</p>;

  return (
    <div className="divide-y divide-white/[0.05] border-t border-white/[0.06]">
      {data.slice(0, 5).map((op) => {
        const badge = stateBadge[op.state];
        const percent = op.total > 0 ? Math.min(100, (op.done / op.total) * 100) : 0;
        return (
          <div key={op.id} className="space-y-2 px-5 py-3">
            <div className="flex flex-wrap items-center gap-2">
              {op.state === "running" ? <Loader2 className="h-4 w-4 animate-spin text-muted" /> : op.kind === "clone" ? <Copy className="h-4 w-4 text-muted" /> : <Truck className="h-4 w-4 text-muted" />}
              <p className="min-w-0 flex-1 truncate text-sm font-medium">{op.label}</p>
              <Badge tone={badge.tone}>{badge.label}</Badge>
              {op.state === "running" && canCancel && (
                <Button
                  size="sm"
                  variant="ghost"
                  disabled={op.cancelRequested}
                  onClick={async () => {
                    if (await toast.run(() => api.post(`/server-ops/${op.id}/cancel`), "Wird abgebrochen")) mutate();
                  }}
                >
                  {op.cancelRequested ? "Wird abgebrochen …" : "Abbrechen"}
                </Button>
              )}
            </div>
            {op.state === "running" ? (
              <>
                <p className="text-xs text-muted">{op.step || "Läuft …"}</p>
                {op.kind === "migrate" && op.total > 0 && (
                  <div>
                    <Meter value={percent} max={100} />
                    <p className="mt-1 text-[11px] tabular-nums text-muted">
                      {bytes(op.done)} von etwa {bytes(op.total)} übertragen
                    </p>
                  </div>
                )}
              </>
            ) : (
              <p className={op.state === "failed" ? "text-xs text-bad" : "text-xs text-muted"}>
                {op.message} <span className="text-muted">· {ago(op.updatedAt)}</span>
              </p>
            )}
            {op.warnings.length > 0 && (
              <ul className="space-y-1 rounded-lg border border-warn/25 bg-warn/10 px-4 py-3 text-xs text-warn">
                {op.warnings.map((warning) => (
                  <li key={warning}>{warning}</li>
                ))}
              </ul>
            )}
            {op.state === "done" && op.kind === "clone" && op.resultServerId && (
              <Link href={`/server/?id=${op.resultServerId}`} className="text-xs text-accent hover:underline">
                Zur Kopie
              </Link>
            )}
          </div>
        );
      })}
    </div>
  );
}
