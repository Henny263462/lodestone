"use client";

import { Pencil, Plus, ScanSearch, Trash2 } from "lucide-react";
import { useState } from "react";
import { Badge, Button, Card, CardHeader, EmptyState, RowMenu, Switch } from "@/components/ui/primitives";
import { ConfirmDialog, Dialog, useToast } from "@/components/ui/feedback";
import { ErrorState, Loading } from "@/components/ui/async";
import { api, useApi } from "@/lib/api";
import { usePoll } from "@/lib/live";
import { ago, dateTime } from "@/lib/format";
import type { ConsoleTrigger, Project, Schedule, TriggerLevel } from "@/lib/types";

const levelLabel: Record<TriggerLevel, string> = { info: "Info", warn: "Warnung", error: "Fehler" };
const levelTone: Record<TriggerLevel, "info" | "warn" | "bad"> = { info: "info", warn: "warn", error: "bad" };

/** Eigene Muster in der Konsole als Auslöser für Ereignisse, Webhooks und Zeitpläne (nur Projekt-Admins). */
export function ConsoleTriggersCard({ project }: { project: Project }) {
  const toast = useToast();
  // „Zuletzt getroffen“ ändert sich mit jedem Treffer; der Treffer selbst kommt als Ereignis über den Live-Kanal
  const { data, error, mutate } = useApi<ConsoleTrigger[]>(`/projects/${project.id}/console-triggers`, { refreshInterval: usePoll(120000, 15000) });
  const { data: schedules } = useApi<Schedule[]>(`/projects/${project.id}/schedules`);
  const [editing, setEditing] = useState<ConsoleTrigger | "new" | null>(null);
  const [removing, setRemoving] = useState<ConsoleTrigger | null>(null);
  const serverName = (id: string) => (id ? (project.servers.find((server) => server.id === id)?.name ?? "gelöschter Server") : "alle Server");
  const scheduleName = (id: string) => schedules?.find((schedule) => schedule.id === id)?.name;

  return (
    <Card>
      <CardHeader
        title="Konsolen-Auslöser"
        description="Eigene Muster in der Konsole werden zum Ereignis „Konsolen-Auslöser“ und können einen Zeitplan starten."
        action={
          data &&
          data.length > 0 && (
            <Button size="sm" onClick={() => setEditing("new")}>
              <Plus className="h-3.5 w-3.5" />
              Auslöser
            </Button>
          )
        }
      />
      {error ? (
        <ErrorState inline error={error} retry={() => mutate()} />
      ) : !data ? (
        <Loading />
      ) : data.length === 0 ? (
        <div className="pb-8">
          <EmptyState icon={<ScanSearch className="h-6 w-6" />} title="Noch kein Konsolen-Auslöser" text="Lege ein Muster an, z. B. „Can't keep up!“, um bei Lag sofort Bescheid zu bekommen. Beitritte und Abgänge erkennt Lodestone von selbst." />
          <div className="flex justify-center">
            <Button variant="primary" onClick={() => setEditing("new")}>
              <Plus className="h-4 w-4" />
              Auslöser anlegen
            </Button>
          </div>
        </div>
      ) : (
        <div className="divide-y divide-white/[0.05]">
          {data.map((trigger) => (
            <div key={trigger.id} className="flex flex-wrap items-center gap-x-4 gap-y-1 px-5 py-3">
              <div className="min-w-[200px] flex-1">
                <p className="text-sm font-medium">
                  {trigger.name} <Badge tone={levelTone[trigger.level]}>{levelLabel[trigger.level]}</Badge>
                </p>
                <p className="truncate font-mono text-xs text-ink/70">{trigger.pattern}</p>
                <p className="text-xs text-muted">
                  {serverName(trigger.serverId)}
                  {trigger.scheduleId && <> · startet „{scheduleName(trigger.scheduleId) ?? "Zeitplan"}“</>}
                  {" · "}
                  {trigger.lastMatch ? <span title={dateTime(trigger.lastMatch)}>zuletzt {ago(trigger.lastMatch)}</span> : "noch kein Treffer"}
                </p>
              </div>
              <Switch
                checked={trigger.enabled}
                onChange={async (value) => {
                  await toast.run(() => api.patch(`/console-triggers/${trigger.id}`, { enabled: value }), value ? "Auslöser ist an" : "Auslöser ist aus");
                  mutate();
                }}
              />
              <RowMenu
                items={[
                  { label: "Bearbeiten", icon: <Pencil className="h-3.5 w-3.5" />, onClick: () => setEditing(trigger) },
                  "separator",
                  { label: "Löschen", icon: <Trash2 className="h-3.5 w-3.5" />, onClick: () => setRemoving(trigger), danger: true },
                ]}
              />
            </div>
          ))}
        </div>
      )}
      <TriggerDialog project={project} schedules={schedules ?? []} trigger={editing} onClose={() => setEditing(null)} onDone={() => mutate()} />
      <ConfirmDialog
        open={!!removing}
        onClose={() => setRemoving(null)}
        title={`„${removing?.name}“ löschen?`}
        text="Das Muster wird nicht mehr beobachtet. Bisherige Ereignisse im Verlauf bleiben."
        confirm="Löschen"
        danger
        onConfirm={async () => {
          if (!removing) return;
          await toast.run(() => api.delete(`/console-triggers/${removing.id}`), "Gelöscht");
          mutate();
        }}
      />
    </Card>
  );
}

function TriggerDialog({
  project,
  schedules,
  trigger,
  onClose,
  onDone,
}: {
  project: Project;
  schedules: Schedule[];
  trigger: ConsoleTrigger | "new" | null;
  onClose: () => void;
  onDone: () => void;
}) {
  const toast = useToast();
  const existing = trigger && trigger !== "new" ? trigger : null;
  const [name, setName] = useState("");
  const [pattern, setPattern] = useState("");
  const [serverId, setServerId] = useState("");
  const [level, setLevel] = useState<TriggerLevel>("info");
  const [scheduleId, setScheduleId] = useState("");
  const [seen, setSeen] = useState<ConsoleTrigger | "new" | null>(null);
  const [busy, setBusy] = useState(false);

  if (trigger !== seen) {
    setSeen(trigger);
    setName(existing?.name ?? "");
    setPattern(existing?.pattern ?? "");
    setServerId(existing?.serverId ?? "");
    setLevel(existing?.level ?? "info");
    setScheduleId(existing?.scheduleId ?? "");
  }

  return (
    <Dialog
      open={trigger !== null}
      onClose={onClose}
      title={existing ? "Auslöser bearbeiten" : "Konsolen-Auslöser anlegen"}
      footer={
        <>
          <Button variant="ghost" onClick={onClose}>
            Abbrechen
          </Button>
          <Button
            variant="primary"
            loading={busy}
            disabled={!name.trim() || !pattern.trim()}
            onClick={async () => {
              setBusy(true);
              const body = { name, pattern, serverId, level, scheduleId };
              const ok = await toast.run(
                () => (existing ? api.patch(`/console-triggers/${existing.id}`, body) : api.post(`/projects/${project.id}/console-triggers`, body)),
                existing ? "Gespeichert" : "Auslöser angelegt",
              );
              setBusy(false);
              if (ok) {
                onClose();
                onDone();
              }
            }}
          >
            {existing ? "Speichern" : "Anlegen"}
          </Button>
        </>
      }
    >
      <div className="grid gap-3 sm:grid-cols-2">
        <div>
          <label className="label">Name</label>
          <input className="field" value={name} onChange={(event) => setName(event.target.value)} maxLength={48} placeholder="Lag erkannt" autoFocus />
        </div>
        <div>
          <label className="label">Server</label>
          <select className="field" value={serverId} onChange={(event) => setServerId(event.target.value)}>
            <option value="">Alle Server des Projekts</option>
            {project.servers.map((server) => (
              <option key={server.id} value={server.id}>
                {server.name}
              </option>
            ))}
          </select>
        </div>
      </div>
      <div>
        <label className="label">Muster (regulärer Ausdruck)</label>
        <input className="field font-mono text-xs" value={pattern} onChange={(event) => setPattern(event.target.value)} maxLength={200} placeholder="Can't keep up!" spellCheck={false} />
        <p className="mt-1 text-xs text-muted">
          Höchstens 200 Zeichen, Schreibweise wie in Go (RE2). Groß- und Kleinschreibung ignorierst du mit <code className="font-mono">(?i)</code> am Anfang. Jeder Auslöser meldet sich höchstens alle 30 Sekunden pro Server.
        </p>
      </div>
      <div className="grid gap-3 sm:grid-cols-2">
        <div>
          <label className="label">Stufe des Ereignisses</label>
          <select className="field" value={level} onChange={(event) => setLevel(event.target.value as TriggerLevel)}>
            {(Object.keys(levelLabel) as TriggerLevel[]).map((entry) => (
              <option key={entry} value={entry}>
                {levelLabel[entry]}
              </option>
            ))}
          </select>
        </div>
        <div>
          <label className="label">Zeitplan starten</label>
          <select className="field" value={scheduleId} onChange={(event) => setScheduleId(event.target.value)}>
            <option value="">Keinen</option>
            {schedules.map((schedule) => (
              <option key={schedule.id} value={schedule.id}>
                {schedule.name} ({schedule.serverName})
              </option>
            ))}
          </select>
        </div>
      </div>
    </Dialog>
  );
}
