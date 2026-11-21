"use client";

import Link from "next/link";
import { AlertTriangle, Bell, History, Info, Pencil, Plus, ScanSearch, Send, Trash2, XCircle } from "lucide-react";
import { useState } from "react";
import { Badge, Button, Card, CardHeader, Checkbox, EmptyState, RowMenu, SectionLayout, Switch, type Section } from "@/components/ui/primitives";
import { ConfirmDialog, Dialog, useToast } from "@/components/ui/feedback";
import { ErrorState, Loading } from "@/components/ui/async";
import { ConsoleTriggersCard } from "@/components/console-triggers";
import { api, useApi } from "@/lib/api";
import { ago, dateTime } from "@/lib/format";
import { usePoll } from "@/lib/live";
import { useUrlState } from "@/lib/url-state";
import { can, type Project, type ProjectEvent, type Webhook } from "@/lib/types";
import { t } from "@/lib/i18n";

export const eventLabels = (): Record<string, string> => ({
  crash: t("Absturz"),
  restart: t("Neustart nach Absturz"),
  crashloop: t("Absturzschleife"),
  backup: t("Backup fertig"),
  backup_failed: t("Backup fehlgeschlagen"),
  schedule_failed: t("Zeitplan fehlgeschlagen"),
  node_offline: t("Node nicht erreichbar"),
  node_online: t("Node wieder erreichbar"),
  lowmemory: t("Wenig Arbeitsspeicher"),
  scale: t("Skalierung"),
  storage: t("Speicher fast voll"),
  transfer: t("Kopie und Umzug"),
  player_join: t("Spieler betritt"),
  player_leave: t("Spieler verlässt"),
  console_match: t("Konsolen-Auslöser"),
  sleep: t("Server schläft ein"),
  wake: t("Server wacht auf"),
  sftp_login: t("SFTP-Anmeldung"),
});

const defaults = ["crash", "crashloop", "backup_failed", "schedule_failed", "node_offline", "lowmemory", "storage"];

type SectionId = "history" | "webhooks" | "triggers";

/** Tab „Ereignisse“: Verlauf für alle Mitglieder, Benachrichtigungen und Konsolen-Auslöser mit dem Recht „Webhooks“. */
export function EventsTab({ project }: { project: Project }) {
  const canManage = can(project.myPermissions, "webhooks");
  const [section, setSection] = useUrlState<SectionId>("section", "history");
  const sections: Section<SectionId>[] = [
    { id: "history", label: t("Verlauf"), icon: History },
    ...(canManage
      ? [
          { id: "webhooks" as const, label: t("Benachrichtigungen"), icon: Bell },
          { id: "triggers" as const, label: t("Konsolen-Auslöser"), icon: ScanSearch },
        ]
      : []),
  ];
  const active = sections.some((entry) => entry.id === section) ? section : "history";
  return (
    <SectionLayout label={t("Abschnitte der Ereignisse")} sections={sections} value={active} onChange={setSection}>
      {active === "history" && <HistoryCard project={project} />}
      {active === "webhooks" && <WebhooksCard project={project} />}
      {active === "triggers" && <ConsoleTriggersCard project={project} />}
    </SectionLayout>
  );
}

/** Symbol für die Stufe eines Ereignisses (auch in der Übersicht). */
export function EventIcon({ level }: { level: string }) {
  if (level === "error") return <XCircle className="mt-0.5 h-4 w-4 shrink-0 text-bad" aria-label={t("Fehler")} />;
  if (level === "warn") return <AlertTriangle className="mt-0.5 h-4 w-4 shrink-0 text-warn" aria-label={t("Warnung")} />;
  return <Info className="mt-0.5 h-4 w-4 shrink-0 text-info" aria-label={t("Hinweis")} />;
}

function HistoryCard({ project }: { project: Project }) {
  // Neue Ereignisse meldet der Live-Kanal; das Intervall ist nur der Rückfall
  const { data, error, mutate } = useApi<ProjectEvent[]>(`/projects/${project.id}/events`, { refreshInterval: usePoll(120000, 10000) });
  const names = new Map(project.servers.map((server) => [server.id, server.name]));
  return (
    <Card>
      <CardHeader title={t("Verlauf")} description={t("Abstürze, Neustarts, Backups und Meldungen der letzten 30 Tage.")} />
      {error ? (
        <ErrorState inline error={error} retry={() => mutate()} />
      ) : !data ? (
        <Loading />
      ) : data.length === 0 ? (
        <EmptyState icon={<Bell className="h-6 w-6" />} title={t("Noch nichts passiert")} text={t("Hier erscheinen Abstürze, automatische Neustarts, Backups und Ausfälle.")} />
      ) : (
        <div className="divide-y divide-white/[0.05]">
          {data.map((event) => (
            <div key={event.id} className="flex items-start gap-3 px-5 py-3">
              <EventIcon level={event.level} />
              <div className="min-w-0 flex-1">
                <p className="break-words text-sm">{event.message}</p>
                <p className="mt-0.5 text-xs text-muted">
                  <span title={dateTime(event.time)}>{ago(event.time)}</span> · {eventLabels()[event.kind] ?? event.kind}
                  {event.serverId && names.has(event.serverId) && (
                    <>
                      {" · "}
                      <Link href={`/server/?id=${event.serverId}`} className="hover:text-ink hover:underline">
                        {names.get(event.serverId)}
                      </Link>
                    </>
                  )}
                </p>
              </div>
            </div>
          ))}
        </div>
      )}
    </Card>
  );
}

// ---------------------------------------------------------------- Webhooks

function WebhooksCard({ project }: { project: Project }) {
  const toast = useToast();
  const { data, error, mutate } = useApi<Webhook[]>(`/projects/${project.id}/webhooks`);
  const [editing, setEditing] = useState<Webhook | "new" | null>(null);
  const [removing, setRemoving] = useState<Webhook | null>(null);

  async function test(hook: Webhook) {
    let status = "";
    let ok = false;
    const sent = await toast.run(async () => {
      const result = await api.post<{ ok: boolean; status: string }>(`/webhooks/${hook.id}/test`);
      ok = result.ok;
      status = result.status;
    });
    if (sent) {
      if (ok) toast.success(t("Testnachricht gesendet"));
      else toast.error(t("Die Adresse hat die Testnachricht abgelehnt: {status}", { status }));
    }
    mutate();
  }

  return (
    <Card>
      <CardHeader
        title={t("Benachrichtigungen")}
        description={t("Schickt Meldungen an Discord oder eine eigene Adresse, etwa n8n oder ntfy.")}
        action={
          data &&
          data.length > 0 && (
            <Button size="sm" onClick={() => setEditing("new")}>
              <Plus className="h-3.5 w-3.5" />
              Webhook
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
          <EmptyState icon={<Send className="h-6 w-6" />} title={t("Noch kein Webhook")} text={t("Lege einen an, damit du bei einem Absturz sofort Bescheid weißt. Adressen im internen Netz sind gesperrt.")} />
          <div className="flex justify-center">
            <Button variant="primary" onClick={() => setEditing("new")}>
              <Plus className="h-4 w-4" />
              {t("Webhook anlegen")}
            </Button>
          </div>
        </div>
      ) : (
        <div className="divide-y divide-white/[0.05]">
          {data.map((hook) => (
            <div key={hook.id} className="flex items-center gap-4 px-5 py-3">
              <div className="min-w-0 flex-1">
                <p className="flex flex-wrap items-center gap-2 text-sm font-medium">
                  <span className="truncate">{hook.name}</span> <Badge tone="info">{hook.kind === "discord" ? "Discord" : t("Allgemein")}</Badge>
                </p>
                <p className="truncate text-xs text-muted">
                  {hook.events.length} {t("Ereignisse")}
                  {hook.lastStatus && (
                    <>
                      {" · "}
                      <span className={hook.lastStatus.startsWith("OK") ? "text-good" : "text-bad"}>{hook.lastStatus}</span>
                    </>
                  )}
                </p>
              </div>
              <Switch
                checked={hook.enabled}
                onChange={async (value) => {
                  await toast.run(() => api.patch(`/webhooks/${hook.id}`, { enabled: value }), value ? t("Webhook ist an") : t("Webhook ist aus"));
                  mutate();
                }}
              />
              <RowMenu
                items={[
                  { label: t("Testnachricht senden"), icon: <Send className="h-3.5 w-3.5" />, onClick: () => test(hook) },
                  { label: t("Bearbeiten"), icon: <Pencil className="h-3.5 w-3.5" />, onClick: () => setEditing(hook) },
                  "separator",
                  { label: t("Löschen"), icon: <Trash2 className="h-3.5 w-3.5" />, onClick: () => setRemoving(hook), danger: true },
                ]}
              />
            </div>
          ))}
        </div>
      )}
      <WebhookDialog project={project} hook={editing} onClose={() => setEditing(null)} onDone={() => mutate()} />
      <ConfirmDialog
        open={!!removing}
        onClose={() => setRemoving(null)}
        title={t("„{name}“ löschen?", { name: removing?.name })}
        text={t("Es werden keine Meldungen mehr an diese Adresse geschickt.")}
        confirm={t("Löschen")}
        danger
        onConfirm={async () => {
          if (!removing) return;
          await toast.run(() => api.delete(`/webhooks/${removing.id}`), t("Webhook gelöscht"));
          mutate();
        }}
      />
    </Card>
  );
}

function WebhookDialog({ project, hook, onClose, onDone }: { project: Project; hook: Webhook | "new" | null; onClose: () => void; onDone: () => void }) {
  const toast = useToast();
  const existing = hook && hook !== "new" ? hook : null;
  const [name, setName] = useState("");
  const [kind, setKind] = useState<"discord" | "generic">("discord");
  const [url, setUrl] = useState("");
  const [events, setEvents] = useState<string[]>(defaults);
  const [seen, setSeen] = useState<Webhook | "new" | null>(null);
  const [busy, setBusy] = useState(false);

  if (hook !== seen) {
    setSeen(hook);
    setName(existing?.name ?? "");
    setKind(existing?.kind ?? "discord");
    setUrl(existing?.url ?? "");
    setEvents(existing?.events ?? defaults);
  }

  return (
    <Dialog
      open={hook !== null}
      onClose={onClose}
      title={existing ? t("Webhook bearbeiten") : t("Webhook anlegen")}
      footer={
        <>
          <Button variant="ghost" onClick={onClose}>
            {t("Abbrechen")}
          </Button>
          <Button
            variant="primary"
            loading={busy}
            disabled={!name.trim() || !url.trim() || events.length === 0}
            onClick={async () => {
              setBusy(true);
              const body = { name, kind, url, events };
              const ok = await toast.run(() => (existing ? api.patch(`/webhooks/${existing.id}`, body) : api.post(`/projects/${project.id}/webhooks`, body)), existing ? t("Gespeichert") : t("Webhook angelegt"));
              setBusy(false);
              if (ok) {
                onClose();
                onDone();
              }
            }}
          >
            {existing ? t("Speichern") : t("Anlegen")}
          </Button>
        </>
      }
    >
      <div className="grid gap-3 sm:grid-cols-2">
        <div>
          <label className="label">{t("Name")}</label>
          <input className="field" value={name} onChange={(event) => setName(event.target.value)} maxLength={48} placeholder={t("Discord-Kanal #server")} autoFocus />
        </div>
        <div>
          <label className="label">{t("Art")}</label>
          <select className="field" value={kind} onChange={(event) => setKind(event.target.value as "discord" | "generic")}>
            <option value="discord">Discord</option>
            <option value="generic">{t("Allgemein (JSON)")}</option>
          </select>
        </div>
      </div>
      <div>
        <label className="label">{t("Adresse (https://…)")}</label>
        <input className="field font-mono text-xs" value={url} onChange={(event) => setUrl(event.target.value)} placeholder={kind === "discord" ? "https://discord.com/api/webhooks/…" : "https://example.org/hook"} />
        {kind === "discord" && <p className="mt-1 text-xs text-muted">{t("In Discord: Kanal → Einstellungen → Integrationen → Webhooks → Neuer Webhook → URL kopieren.")}</p>}
      </div>
      <div>
        <label className="label">{t("Wobei melden?")}</label>
        <div className="grid gap-2 sm:grid-cols-2">
          {Object.entries(eventLabels()).map(([id, label]) => (
            <label key={id} className="flex cursor-pointer items-center gap-2.5 text-sm">
              <Checkbox checked={events.includes(id)} onChange={(on) => setEvents((current) => (on ? [...current, id] : current.filter((entry) => entry !== id)))} label={label} />
              {label}
            </label>
          ))}
        </div>
      </div>
    </Dialog>
  );
}

