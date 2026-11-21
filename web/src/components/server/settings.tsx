"use client";

import { HardDrive, LifeBuoy, Moon, Plug, RefreshCw, Settings2, Trash2, Truck } from "lucide-react";
import { useEffect, useState } from "react";
import { Badge, Button, Card, CardHeader, SectionLayout, Switch, type Section } from "@/components/ui/primitives";
import { ConfirmDialog, useToast } from "@/components/ui/feedback";
import { TransferCard } from "@/components/server/transfer";
import { SleepCard } from "@/components/server/sleep";
import { StorageCard } from "@/components/server/storage";
import { api, useApi } from "@/lib/api";
import { megabytes } from "@/lib/format";
import { useUrlState } from "@/lib/url-state";
import { can, type Permission, type Server } from "@/lib/types";

const memoryOptions = [256, 512, 1024, 2048, 3072, 4096, 6144, 8192, 12288, 16384];

function envToText(env?: Record<string, string>): string {
  return Object.entries(env ?? {})
    .map(([key, value]) => `${key}=${value}`)
    .join("\n");
}

function textToEnv(text: string): Record<string, string> {
  const env: Record<string, string> = {};
  for (const line of text.split("\n")) {
    const trimmed = line.trim();
    if (!trimmed || trimmed.startsWith("#")) continue;
    const cut = trimmed.indexOf("=");
    if (cut > 0) env[trimmed.slice(0, cut).trim()] = trimmed.slice(cut + 1);
  }
  return env;
}

type SectionId = "general" | "ports" | "sleep" | "crash" | "version" | "transfer" | "storage" | "danger";

// Die Abschnitte erscheinen nach Rechten: alles außer Kopieren/Umziehen und Löschen mit „Einstellungen“, diese beiden
// mit „Server anlegen und löschen“.
export function SettingsTab({ server, permissions, onChanged, onDeleted }: { server: Server; permissions: Permission[]; onChanged: () => void; onDeleted: () => void }) {
  const canEdit = can(permissions, "settings");
  const canServers = can(permissions, "servers");
  const [section, setSection] = useUrlState<SectionId>("section", "general");
  const offline = server.view?.state === "offline";

  const sections: Section<SectionId>[] = [
    ...(canEdit
      ? [
          { id: "general" as const, label: "Allgemein", icon: Settings2 },
          { id: "ports" as const, label: "Anschlüsse", icon: Plug },
          { id: "sleep" as const, label: "Schlafmodus", icon: Moon },
          { id: "crash" as const, label: "Absturzerkennung", icon: LifeBuoy },
          { id: "version" as const, label: "Version", icon: RefreshCw },
        ]
      : []),
    ...(canServers ? [{ id: "transfer" as const, label: "Kopieren und umziehen", icon: Truck }] : []),
    ...(canEdit ? [{ id: "storage" as const, label: "Speicher", icon: HardDrive }] : []),
    ...(canServers ? [{ id: "danger" as const, label: "Gefahrenzone", icon: Trash2 }] : []),
  ];
  const active = sections.some((entry) => entry.id === section) ? section : sections[0]?.id;
  if (!active) return null;

  return (
    <SectionLayout label="Abschnitte der Einstellungen" sections={sections} value={active} onChange={setSection}>
      {active === "general" && <GeneralCard server={server} onChanged={onChanged} />}
      {active === "ports" && (
        <>
          <PortsCard server={server} />
          <PublicAddress server={server} onDone={onChanged} />
        </>
      )}
      {active === "sleep" && <SleepCard server={server} />}
      {active === "crash" && <AutoRestart server={server} onDone={onChanged} />}
      {active === "version" && <Reinstall server={server} offline={offline} onDone={onChanged} />}
      {active === "transfer" && <TransferCard server={server} permissions={permissions} />}
      {active === "storage" && <StorageCard server={server} manage />}
      {active === "danger" && <DeleteServer server={server} onDeleted={onDeleted} />}
    </SectionLayout>
  );
}

function GeneralCard({ server, onChanged }: { server: Server; onChanged: () => void }) {
  const toast = useToast();
  const view = server.view;
  const offline = view?.state === "offline";
  const [name, setName] = useState(server.name);
  const [memory, setMemory] = useState(view?.memoryMB ?? 2048);
  const [cpu, setCpu] = useState(String(view?.cpu ?? 0));
  const [keep, setKeep] = useState(String(view?.backupKeep ?? 5));
  const [startup, setStartup] = useState(view?.startup ?? "");
  const [env, setEnv] = useState(envToText(view?.env));
  const [busy, setBusy] = useState(false);

  useEffect(() => {
    if (!view) return;
    setName(server.name);
    setMemory(view.memoryMB);
    setCpu(String(view.cpu));
    setKeep(String(view.backupKeep));
    setStartup(view.startup ?? "");
    setEnv(envToText(view.env));
    // nur beim Wechsel des Servers oder nach Speichern neu laden
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [server.id, view?.memoryMB, view?.cpu, view?.backupKeep, view?.startup]);

  async function save() {
    setBusy(true);
    const ok = await toast.run(
      () =>
        api.patch(`/servers/${server.id}`, {
          name,
          memoryMB: memory,
          cpu: Number(cpu) || 0,
          backupKeep: Number(keep) || 5,
          startup,
          env: textToEnv(env),
        }),
      "Einstellungen gespeichert",
    );
    setBusy(false);
    if (ok) onChanged();
  }

  return (
    <Card>
      <CardHeader title="Allgemein" description={offline ? "Änderungen gelten ab dem nächsten Start." : "Zum Ändern muss der Server ausgeschaltet sein."} />
      <div className="grid gap-4 px-5 py-4 md:grid-cols-2">
        <div>
          <label className="label">Name</label>
          <input className="field" value={name} onChange={(event) => setName(event.target.value)} maxLength={48} disabled={!offline} />
        </div>
        <div>
          <label className="label">Arbeitsspeicher</label>
          <select className="field" value={memory} onChange={(event) => setMemory(Number(event.target.value))} disabled={!offline}>
            {[...new Set([...memoryOptions, memory])].sort((a, b) => a - b).map((value) => (
              <option key={value} value={value}>
                {megabytes(value)}
              </option>
            ))}
          </select>
        </div>
        <div>
          <label className="label">CPU-Kerne (0 = unbegrenzt)</label>
          <input className="field" inputMode="decimal" value={cpu} onChange={(event) => setCpu(event.target.value)} disabled={!offline} />
        </div>
        <div>
          <label className="label">Backups lokal aufbewahren</label>
          <input className="field" inputMode="numeric" value={keep} onChange={(event) => setKeep(event.target.value)} disabled={!offline} />
        </div>
        <div className="md:col-span-2">
          <label className="label">Startzeile (leer = Standard der Software)</label>
          <textarea
            className="field font-mono text-xs"
            rows={3}
            value={startup}
            onChange={(event) => setStartup(event.target.value)}
            disabled={!offline}
            spellCheck={false}
            placeholder="exec java -Xms{{MEMORY}}M -Xmx{{MEMORY}}M -jar {{JAR}} nogui"
          />
        </div>
        <div className="md:col-span-2">
          <label className="label">Umgebungsvariablen (eine pro Zeile: NAME=Wert)</label>
          <textarea className="field font-mono text-xs" rows={3} value={env} onChange={(event) => setEnv(event.target.value)} disabled={!offline} spellCheck={false} />
        </div>
      </div>
      <div className="flex justify-end border-t border-white/[0.06] px-5 py-3">
        <Button variant="primary" loading={busy} disabled={!offline} onClick={save}>
          Speichern
        </Button>
      </div>
    </Card>
  );
}

function PortsCard({ server }: { server: Server }) {
  const ports = server.view?.ports ?? [];
  return (
    <Card>
      <CardHeader title="Anschlüsse" description="Ports der Maschine, über die der Server von außen erreichbar ist." />
      <div className="flex flex-wrap gap-2 px-5 py-4">
        {ports.length === 0 ? (
          <p className="text-sm text-muted">Der Server ist nicht nach außen veröffentlicht. Er ist nur im Docker-Netzwerk erreichbar, etwa hinter dem Proxy.</p>
        ) : (
          ports.map((port) => (
            <Badge key={`${port.container}/${port.protocol}`} tone="info">
              {port.host} → {port.container}/{port.protocol}
            </Badge>
          ))
        )}
      </div>
    </Card>
  );
}

function DeleteServer({ server, onDeleted }: { server: Server; onDeleted: () => void }) {
  const toast = useToast();
  const [deleting, setDeleting] = useState(false);
  return (
    <>
      <Card className="border-bad/25">
        <CardHeader title="Server löschen" description="Löscht den Server samt Welten, Plugins und Backups. Das lässt sich nicht rückgängig machen." />
        <div className="px-5 py-4">
          <Button variant="danger" onClick={() => setDeleting(true)}>
            <Trash2 className="h-4 w-4" />
            Server löschen
          </Button>
        </div>
      </Card>
      <ConfirmDialog
        open={deleting}
        onClose={() => setDeleting(false)}
        title={`„${server.name}“ löschen?`}
        text="Alle Dateien dieses Servers, auch Welten und Backups, werden unwiderruflich gelöscht."
        confirm="Endgültig löschen"
        danger
        onConfirm={async () => {
          if (await toast.run(() => api.delete(`/servers/${server.id}`), "Server gelöscht")) onDeleted();
        }}
      />
    </>
  );
}

function AutoRestart({ server, onDone }: { server: Server; onDone: () => void }) {
  const toast = useToast();
  const grouped = !!server.groupId;
  return (
    <Card>
      <CardHeader
        title="Absturzerkennung"
        description={grouped ? "Instanzen einer Gruppe startet der Autoscaler selbst neu." : "Auto-Restart startet den Server nach einem Absturz neu. Der Schalter wirkt sofort."}
        action={
          <Switch
            checked={server.autoRestart}
            disabled={grouped}
            onChange={async (value) => {
              if (await toast.run(() => api.put(`/servers/${server.id}/auto-restart`, { enabled: value }), value ? "Auto-Restart ist an" : "Auto-Restart ist aus")) onDone();
            }}
          />
        }
      />
      <div className="space-y-2 px-5 py-4 text-xs text-muted">
        <p>
          Auto-Restart: <b className={server.autoRestart ? "text-good" : "text-muted"}>{server.autoRestart ? "eingeschaltet" : "aus"}</b>
        </p>
        <p>
          Als Absturz zählt, wenn der Prozess mit einem Fehler endet, ohne dass ihn jemand gestoppt hat. Jeder Absturz erscheint unter „Ereignisse“. Auto-Restart versucht es bis zu dreimal in 15
          Minuten und gibt dann auf, damit der Server nicht endlos abstürzt.
        </p>
      </div>
    </Card>
  );
}

function PublicAddress({ server, onDone }: { server: Server; onDone: () => void }) {
  const toast = useToast();
  const [address, setAddress] = useState(server.publicAddress ?? "");
  const [busy, setBusy] = useState(false);
  useEffect(() => setAddress(server.publicAddress ?? ""), [server.id, server.publicAddress]);
  return (
    <Card>
      <CardHeader title="Öffentliche Adresse" description="Die Adresse, die Spieler sehen, etwa ein playit-Tunnel. Leer heißt: Adresse der Maschine." />
      <div className="flex flex-wrap items-end gap-3 px-5 py-4">
        <div className="min-w-0 flex-1 basis-60">
          <label className="label">Adresse (wird nur angezeigt)</label>
          <input className="field font-mono text-xs" value={address} onChange={(event) => setAddress(event.target.value)} placeholder="mc.example.org oder abc.joinmc.link:12345" spellCheck={false} />
        </div>
        <Button
          loading={busy}
          disabled={address.trim() === (server.publicAddress ?? "")}
          onClick={async () => {
            setBusy(true);
            const ok = await toast.run(() => api.put(`/servers/${server.id}/public-address`, { address: address.trim() }), "Adresse gespeichert");
            setBusy(false);
            if (ok) onDone();
          }}
        >
          Speichern
        </Button>
      </div>
    </Card>
  );
}

function Reinstall({ server, offline, onDone }: { server: Server; offline: boolean; onDone: () => void }) {
  const toast = useToast();
  const { data: versions } = useApi<string[]>(`/software/${server.software}/versions`);
  const [version, setVersion] = useState("");
  const [confirm, setConfirm] = useState(false);
  const current = server.view?.version;

  useEffect(() => {
    if (versions && !version) setVersion(current && versions.includes(current) ? current : versions[0]);
  }, [versions, current, version]);

  return (
    <Card>
      <CardHeader title="Version wechseln" description="Lädt die Server-Datei der gewählten Version neu. Welten, Plugins und Configs bleiben." />
      <div className="flex flex-wrap items-end gap-3 px-5 py-4">
        <div className="w-full sm:w-56">
          <label className="label">Version (aktuell {current})</label>
          <select className="field" value={version} onChange={(event) => setVersion(event.target.value)} disabled={!offline || !versions}>
            {(versions ?? []).slice(0, 80).map((entry) => (
              <option key={entry} value={entry}>
                {entry}
              </option>
            ))}
          </select>
        </div>
        <Button disabled={!offline || !version || version === current} onClick={() => setConfirm(true)}>
          <RefreshCw className="h-4 w-4" />
          Version ändern
        </Button>
        <p className="w-full text-xs text-muted">{offline ? "Sichere vorher ein Backup, besonders vor einem Downgrade." : "Zum Wechseln muss der Server ausgeschaltet sein."}</p>
      </div>
      <ConfirmDialog
        open={confirm}
        onClose={() => setConfirm(false)}
        title={`Auf ${version} wechseln?`}
        text="Die Server-Datei wird ersetzt. Welten, die mit einer neueren Version gespeichert wurden, lassen sich mit einer älteren oft nicht mehr laden."
        confirm="Wechseln"
        onConfirm={async () => {
          if (await toast.run(() => api.post(`/servers/${server.id}/reinstall`, { software: server.software, version }), "Neue Version wird installiert")) onDone();
        }}
      />
    </Card>
  );
}
