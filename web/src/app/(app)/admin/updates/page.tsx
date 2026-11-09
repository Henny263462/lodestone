"use client";

import { ArrowUpCircle, Cpu, ExternalLink, RefreshCw } from "lucide-react";
import { useState } from "react";
import { AdminSidebar } from "@/components/admin-sidebar";
import { useUser } from "@/components/layout/shell";
import { ErrorState, Loading } from "@/components/ui/async";
import { Badge, Button, Card, CardHeader, Dot, EmptyState, PageHeader, Switch, Tabs } from "@/components/ui/primitives";
import { ConfirmDialog, useToast } from "@/components/ui/feedback";
import { api, useApi } from "@/lib/api";
import { ago } from "@/lib/format";
import type { NodeUpdateStatus, UpdateStatus } from "@/lib/types";

const isDev = (version?: string) => !!version && version.startsWith("dev");

/** „0.2.0“ wird „v0.2.0“; Entwicklungsstände („dev“, „dev-1a2b3c4“) bleiben, wie sie sind. */
function versionLabel(version?: string): string {
  if (!version) return "–";
  return isDev(version) ? version : `v${version}`;
}

/** „Update auf …“, oder „Wechsel auf …“, wenn dabei zwischen Dev-Build und Release gewechselt wird. */
function updateLabel(data: UpdateStatus, current: string): string {
  const target = versionLabel(data.latestVersion);
  return isDev(current) !== isDev(data.latestVersion) ? `Wechsel auf ${target} verfügbar` : `Update auf ${target} verfügbar`;
}

export default function AdminUpdatesPage() {
  const user = useUser();
  const toast = useToast();
  const { data, error, mutate } = useApi<UpdateStatus>(user.isAdmin ? "/updates" : null, { refreshInterval: 15000 });
  const [checking, setChecking] = useState(false);
  const [toggling, setToggling] = useState(false);
  const [updatingPanel, setUpdatingPanel] = useState(false);
  const [updatingNode, setUpdatingNode] = useState<NodeUpdateStatus | null>(null);
  const [updatingAll, setUpdatingAll] = useState(false);
  const [switchingChannel, setSwitchingChannel] = useState(false);
  const [savingChannel, setSavingChannel] = useState(false);

  if (!user.isAdmin) {
    return <ErrorState error={new Error("Dafür braucht es einen Administrator.")} />;
  }
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
        <Loading />
      </>
    );
  }

  const outdatedNodes = data.nodes.filter((node) => node.updateAvailable);
  const panelOutdated = data.panelUpdateAvailable;

  async function chooseChannel(channel: "stable" | "dev") {
    setSavingChannel(true);
    await toast.run(() => api.put("/updates/settings", { channel }), channel === "dev" ? "Dev-Kanal gewählt" : "Stabiler Kanal gewählt");
    setSavingChannel(false);
    mutate();
  }

  return (
    <>
      <AdminSidebar />
      <PageHeader
        title="Updates"
        description="Versionsstand von Panel und Nodes. Updates laufen über Docker; laufende Minecraft-Server bleiben unberührt."
        action={
          <div className="flex items-center gap-3">
            <span className="text-xs text-muted">Zuletzt geprüft: {ago(data.lastChecked)}</span>
            <Button
              variant="secondary"
              loading={checking}
              onClick={async () => {
                setChecking(true);
                await toast.run(() => api.post("/updates/check"), "Nach Updates gesucht");
                setChecking(false);
                mutate();
              }}
            >
              <RefreshCw className="h-4 w-4" />
              Jetzt prüfen
            </Button>
          </div>
        }
      />

      {data.checkFailed && (
        <p className="mb-4 rounded-lg border border-warn/25 bg-warn/10 px-3 py-2 text-xs text-warn">
          Die neueste Version konnte nicht von GitHub geladen werden. Prüfe, ob das Panel ins Internet kommt, und versuche es erneut.
        </p>
      )}

      <div className="space-y-5">
        <Card>
          <CardHeader title="Update-Kanal" description="Welche Versionen dir das Panel anbietet. Die Wahl gilt für Panel und Nodes und wirkt sofort." />
          <div className="space-y-3 px-5 py-4">
            <Tabs
              value={data.channel}
              onChange={(next) => {
                if (savingChannel || next === data.channel) return;
                if (next === "dev") setSwitchingChannel(true);
                else chooseChannel(next);
              }}
              items={[
                { id: "stable", label: "Stabil" },
                { id: "dev", label: "Dev" },
              ]}
            />
            <p className="text-sm text-muted">
              {data.channel === "dev"
                ? "Dev: Du bekommst immer den neuesten gebauten Stand von main, also den letzten Commit. Neue Funktionen kommen früher, aber es können Fehler drin sein."
                : "Stabil: Du bekommst nur veröffentlichte Releases wie v0.3.0. Das ist die richtige Wahl für den Alltag."}
            </p>
          </div>
        </Card>

        <Card>
          <CardHeader
            title="Lodestone-Panel"
            description="Das Panel startet beim Update neu und ist dabei etwa 10 bis 30 Sekunden nicht erreichbar."
            action={
              panelOutdated && (
                <Button variant="primary" size="sm" onClick={() => setUpdatingPanel(true)}>
                  <ArrowUpCircle className="h-4 w-4" />
                  Panel aktualisieren
                </Button>
              )
            }
          />
          <div className="flex flex-wrap items-center gap-x-3 gap-y-2 px-5 py-4">
            <span className="font-mono text-sm">{versionLabel(data.panelVersion)}</span>
            {panelOutdated ? <Badge tone="warn">{updateLabel(data, data.panelVersion)}</Badge> : data.checkFailed ? <Badge>Unbekannt</Badge> : <Badge tone="good">Aktuell</Badge>}
            {panelOutdated && data.releaseTitle && <span className="text-xs text-muted">{data.releaseTitle}</span>}
            {panelOutdated && data.releaseUrl && (
              <a className="inline-flex items-center gap-1 text-xs text-accent hover:underline" href={data.releaseUrl} target="_blank" rel="noreferrer">
                {data.channel === "dev" ? "Commit ansehen" : "Release-Notizen"}
                <ExternalLink className="h-3 w-3" />
              </a>
            )}
          </div>
        </Card>

        <Card>
          <CardHeader
            title="Nodes"
            description="Der Node-Daemon startet beim Update neu. Das Panel zeigt den Node solange als nicht erreichbar."
            action={
              outdatedNodes.length > 1 && (
                <Button variant="secondary" size="sm" onClick={() => setUpdatingAll(true)}>
                  <ArrowUpCircle className="h-4 w-4" />
                  Alle aktualisieren ({outdatedNodes.length})
                </Button>
              )
            }
          />
          {data.nodes.length === 0 ? (
            <EmptyState icon={<Cpu className="h-6 w-6" />} title="Noch kein Node" text="Lege unter Nodes einen Node an, dann erscheint hier sein Versionsstand." />
          ) : (
            <div className="divide-y divide-white/[0.05]">
              {data.nodes.map((node) => (
                <div key={node.id} className="flex flex-wrap items-center gap-x-4 gap-y-2 px-5 py-3">
                  <span className="flex min-w-[140px] flex-1 items-center gap-2 text-sm font-medium">
                    <Dot tone={node.online ? "good" : "bad"} />
                    {node.name}
                  </span>
                  <span className="w-20 font-mono text-xs text-muted">{versionLabel(node.version)}</span>
                  {!node.online ? <Badge tone="bad">Nicht erreichbar</Badge> : node.updateAvailable ? <Badge tone="warn">{updateLabel(data, node.version)}</Badge> : <Badge tone="good">Aktuell</Badge>}
                  <span className="w-28 text-right">
                    {node.updateAvailable && (
                      <Button variant="secondary" size="sm" onClick={() => setUpdatingNode(node)}>
                        <ArrowUpCircle className="h-3.5 w-3.5" />
                        Aktualisieren
                      </Button>
                    )}
                  </span>
                </div>
              ))}
            </div>
          )}
        </Card>

        <Card>
          <CardHeader title="Automatische Updates" description="Der Schalter wirkt sofort und braucht kein Speichern." />
          <div className="flex items-center justify-between gap-4 px-5 py-4">
            <p className="text-sm text-muted">
              Das Panel prüft alle 6 Stunden auf neue Versionen des gewählten Kanals und installiert sie selbst: erst die Nodes, zuletzt das Panel. Im Dev-Kanal
              heißt das: jeder neue Commit auf main. Du bekommst in jedem Projekt einen Hinweis im Verlauf.
            </p>
            <Switch
              checked={data.autoUpdate}
              disabled={toggling}
              onChange={async (value) => {
                setToggling(true);
                await toast.run(
                  () => api.put("/updates/settings", { autoUpdate: value }),
                  value ? "Automatische Updates eingeschaltet" : "Automatische Updates ausgeschaltet",
                );
                setToggling(false);
                mutate();
              }}
            />
          </div>
        </Card>
      </div>

      <ConfirmDialog
        open={switchingChannel}
        onClose={() => setSwitchingChannel(false)}
        title="Zum Dev-Kanal wechseln?"
        text="Das Panel bietet dir dann den neuesten gebauten Stand von main an, auch wenn er noch nicht als Release getestet ist. Es wird nichts sofort geändert, die Aktualisierung startest du selbst. Ist das automatische Update an, installiert das Panel neue Dev-Builds von allein."
        confirm="Dev-Kanal wählen"
        onConfirm={() => chooseChannel("dev")}
      />
      <ConfirmDialog
        open={updatingPanel}
        onClose={() => setUpdatingPanel(false)}
        title={`Panel auf ${versionLabel(data.latestVersion)} aktualisieren?`}
        text="Das neue Image wird geladen und der Panel-Container ersetzt. Das Panel ist dabei kurz nicht erreichbar, danach musst du die Seite neu laden. Laufende Minecraft-Server bleiben unberührt."
        confirm="Aktualisieren"
        onConfirm={async () => {
          await toast.run(() => api.post("/panel/update"), "Panel-Update gestartet");
          mutate();
        }}
      />
      <ConfirmDialog
        open={!!updatingNode}
        onClose={() => setUpdatingNode(null)}
        title={`Node „${updatingNode?.name}“ aktualisieren?`}
        text="Der Node-Daemon wird auf das neue Image gehoben und neu gestartet. Laufende Minecraft-Server auf diesem Node laufen ungestört weiter."
        confirm="Aktualisieren"
        onConfirm={async () => {
          if (!updatingNode) return;
          await toast.run(() => api.post(`/nodes/${updatingNode.id}/update`), "Node-Update gestartet");
          mutate();
        }}
      />
      <ConfirmDialog
        open={updatingAll}
        onClose={() => setUpdatingAll(false)}
        title={`Alle ${outdatedNodes.length} Nodes aktualisieren?`}
        text="Die Node-Daemons werden nacheinander auf das neue Image gehoben und neu gestartet. Laufende Minecraft-Server auf den Nodes laufen ungestört weiter."
        confirm="Alle aktualisieren"
        onConfirm={async () => {
          await toast.run(() => api.post("/nodes/update-all"), "Updates der Nodes gestartet");
          mutate();
        }}
      />
    </>
  );
}
