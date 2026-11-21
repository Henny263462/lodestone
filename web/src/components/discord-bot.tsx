"use client";

import { Copy, Plus, Send, Trash2 } from "lucide-react";
import { useEffect, useState } from "react";
import { Button, Card, CardHeader, Checkbox, Switch, cx } from "@/components/ui/primitives";
import { ConfirmDialog, useToast } from "@/components/ui/feedback";
import { ErrorState, Loading } from "@/components/ui/async";
import { api, useApi } from "@/lib/api";
import { ago, dateTime } from "@/lib/format";
import { can, type DiscordAction, type DiscordConfig, type DiscordRule, type Project } from "@/lib/types";

const actionLabels: Record<DiscordAction, string> = { status: "/status", players: "/spieler", start: "/start", stop: "/stop", restart: "/restart" };
const powerActions: DiscordAction[] = ["start", "stop", "restart"];

/** Discord-Bot über den Interactions-Endpunkt: Einrichtung, Rechte je Rolle/Benutzer, Befehle eintragen. */
export function DiscordBotCard({ project }: { project: Project }) {
  const toast = useToast();
  const { data, error, mutate } = useApi<DiscordConfig>(`/projects/${project.id}/discord`);
  const [applicationId, setApplicationId] = useState("");
  const [publicKey, setPublicKey] = useState("");
  const [botToken, setBotToken] = useState("");
  const [rules, setRules] = useState<DiscordRule[]>([]);
  const [origin, setOrigin] = useState("");
  const [busy, setBusy] = useState<"" | "save" | "register">("");
  const [removing, setRemoving] = useState(false);
  // Start und Stopp über Discord gibt nur weiter, wer selbst starten und stoppen darf (das Backend prüft es auch)
  const canPower = can(project.myPermissions, "power");

  // Die Adresse des Panels gibt es erst im Browser (die Seite wird beim Bauen vorgerendert)
  useEffect(() => setOrigin(window.location.origin), []);
  useEffect(() => {
    if (!data) return;
    setApplicationId(data.applicationId);
    setPublicKey(data.publicKey);
    setRules(data.rules);
    setBotToken("");
  }, [data]);

  const header = (
    <CardHeader
      title="Discord-Bot"
      description="Slash-Befehle /status, /spieler, /start, /stop und /restart in deinem Discord. Das Panel muss dafür öffentlich per HTTPS erreichbar sein."
      action={
        data?.configured && (
          <Switch
            checked={data.enabled}
            onChange={async (value) => {
              await toast.run(() => api.put(`/projects/${project.id}/discord`, { applicationId: data.applicationId, publicKey: data.publicKey, rules: data.rules, enabled: value }), value ? "Discord-Bot ist an" : "Discord-Bot ist aus");
              mutate();
            }}
          />
        )
      }
    />
  );
  if (error)
    return (
      <Card>
        {header}
        <ErrorState inline error={error} retry={() => mutate()} />
      </Card>
    );
  if (!data)
    return (
      <Card>
        {header}
        <Loading />
      </Card>
    );

  const endpoint = `${origin}${data.interactionsPath}`;
  const complete = /^[0-9]{17,20}$/.test(applicationId.trim()) && /^[0-9a-fA-F]{64}$/.test(publicKey.trim()) && rules.every((rule) => rule.id.trim() && rule.actions.length > 0);
  const updateRule = (index: number, change: Partial<DiscordRule>) => setRules((current) => current.map((rule, at) => (at === index ? { ...rule, ...change } : rule)));

  async function save() {
    setBusy("save");
    const body = { applicationId: applicationId.trim(), publicKey: publicKey.trim(), rules, enabled: data?.configured ? data.enabled : true, ...(botToken.trim() ? { botToken: botToken.trim() } : {}) };
    const ok = await toast.run(() => api.put(`/projects/${project.id}/discord`, body), "Gespeichert");
    setBusy("");
    if (ok) mutate();
  }

  return (
    <Card>
      {header}
      <div className="space-y-4 px-5 py-4">
        <div className="rounded-lg border border-info/25 bg-info/10 px-4 py-3 text-xs leading-relaxed text-info">
          <ol className="list-decimal space-y-0.5 pl-4">
            <li>Lege im Discord Developer Portal eine Anwendung an und trag hier Application-ID, Public Key (General Information) und Bot-Token (Bot → Reset Token) ein.</li>
            <li>Trag unter General Information → Interactions Endpoint URL die Adresse unten ein. Discord prüft sie sofort; dafür muss hier vorher gespeichert sein.</li>
            <li>Lade den Bot mit den Scopes bot und applications.commands in deinen Discord ein und drück „Befehle eintragen“.</li>
          </ol>
        </div>
        {origin.startsWith("http://") && (
          <div className="rounded-lg border border-warn/25 bg-warn/10 px-4 py-3 text-xs text-warn">
            Das Panel läuft gerade über http://. Discord akzeptiert nur eine öffentliche HTTPS-Adresse – stell das Panel hinter einen Reverse-Proxy mit Zertifikat (z. B. Caddy oder Traefik).
          </div>
        )}
        <div>
          <label className="label">Interactions Endpoint URL</label>
          <div className="flex gap-2">
            <input className="field font-mono text-xs" value={endpoint} readOnly />
            <Button
              variant="secondary"
              size="icon"
              aria-label="Adresse kopieren"
              onClick={async () => {
                try {
                  await navigator.clipboard.writeText(endpoint);
                  toast.success("Adresse kopiert");
                } catch {
                  toast.error("Kopieren ging nicht. Markiere die Adresse und kopiere sie von Hand.");
                }
              }}
            >
              <Copy className="h-4 w-4" />
            </Button>
          </div>
        </div>
        <div className="grid gap-3 md:grid-cols-2">
          <div>
            <label className="label">Application-ID</label>
            <input className="field font-mono text-xs" inputMode="numeric" value={applicationId} onChange={(event) => setApplicationId(event.target.value)} placeholder="123456789012345678" />
          </div>
          <div>
            <label className="label">Public Key</label>
            <input className="field font-mono text-xs" value={publicKey} onChange={(event) => setPublicKey(event.target.value)} placeholder="64 Zeichen, 0–9 und a–f" spellCheck={false} />
          </div>
          <div className="md:col-span-2">
            <label className="label">Bot-Token</label>
            <input
              className="field font-mono text-xs"
              type="password"
              autoComplete="off"
              value={botToken}
              onChange={(event) => setBotToken(event.target.value)}
              placeholder={data.hasToken ? "gespeichert – leer lassen, um es zu behalten" : "nur zum Eintragen der Befehle nötig"}
            />
            <p className="mt-1 text-xs text-muted">Das Token wird verschlüsselt gespeichert und nie wieder angezeigt.</p>
          </div>
        </div>

        <div>
          <label className="label">Wer darf was?</label>
          <p className="mb-2 text-xs text-muted">
            Ohne Regel darf niemand einen Befehl benutzen. IDs kopierst du in Discord mit Rechtsklick → ID kopieren (Entwicklermodus in den Einstellungen). Die Rolle @everyone hat dieselbe ID wie dein Discord-Server.
          </p>
          <div className="space-y-2">
            {rules.length === 0 && <p className="rounded-lg border border-white/[0.07] bg-white/[0.02] px-3 py-2.5 text-xs text-muted">Noch keine Regel.</p>}
            {rules.map((rule, index) => (
              <div key={index} className="space-y-2 rounded-lg border border-white/[0.07] bg-white/[0.02] px-3 py-2.5">
                <div className="flex flex-wrap items-center gap-2">
                  <select className="field w-auto" value={rule.kind} onChange={(event) => updateRule(index, { kind: event.target.value as DiscordRule["kind"] })} aria-label="Art">
                    <option value="role">Rolle</option>
                    <option value="user">Benutzer</option>
                  </select>
                  <input className="field min-w-[180px] flex-1 font-mono text-xs" value={rule.id} onChange={(event) => updateRule(index, { id: event.target.value.trim() })} placeholder="Discord-ID" aria-label="Discord-ID" />
                  <input className="field min-w-[140px] flex-1" value={rule.label ?? ""} onChange={(event) => updateRule(index, { label: event.target.value })} maxLength={48} placeholder="Bezeichnung, z. B. Moderatoren" aria-label="Bezeichnung" />
                  <Button variant="ghost" size="icon-sm" aria-label="Regel entfernen" onClick={() => setRules((current) => current.filter((_, at) => at !== index))}>
                    <Trash2 className="h-3.5 w-3.5" />
                  </Button>
                </div>
                <div className="flex flex-wrap gap-x-4 gap-y-1.5">
                  {(Object.keys(actionLabels) as DiscordAction[]).map((action) => {
                    const locked = !canPower && powerActions.includes(action) && !rule.actions.includes(action);
                    return (
                      <label key={action} className={cx("flex items-center gap-2 font-mono text-xs", locked ? "cursor-not-allowed opacity-50" : "cursor-pointer")} title={locked ? "Dafür fehlt dir das Recht „Starten und Stoppen“" : undefined}>
                        <Checkbox
                          checked={rule.actions.includes(action)}
                          onChange={(on) => {
                            if (on && locked) return;
                            updateRule(index, { actions: on ? [...rule.actions, action] : rule.actions.filter((entry) => entry !== action) });
                          }}
                          label={actionLabels[action]}
                        />
                        {actionLabels[action]}
                      </label>
                    );
                  })}
                </div>
              </div>
            ))}
          </div>
          <Button size="sm" className="mt-2" disabled={rules.length >= 25} onClick={() => setRules((current) => [...current, { kind: "role", id: "", label: "", actions: ["status", "players"] }])}>
            <Plus className="h-3.5 w-3.5" />
            Regel
          </Button>
        </div>

        <div className="flex flex-wrap items-center justify-between gap-3 border-t border-white/[0.06] pt-4">
          <p className="text-xs text-muted">
            {data.registeredAt ? <span title={dateTime(data.registeredAt)}>Befehle eingetragen {ago(data.registeredAt)}</span> : "Die Befehle sind noch nicht bei Discord eingetragen."}
          </p>
          <div className="flex flex-wrap gap-2">
            {data.configured && (
              <Button variant="ghost" size="sm" onClick={() => setRemoving(true)}>
                <Trash2 className="h-3.5 w-3.5" />
                Entfernen
              </Button>
            )}
            <Button
              size="sm"
              loading={busy === "register"}
              disabled={!data.configured || !data.hasToken}
              title={!data.hasToken ? "Erst das Bot-Token speichern" : undefined}
              onClick={async () => {
                setBusy("register");
                const ok = await toast.run(() => api.post(`/projects/${project.id}/discord/register`), "Befehle bei Discord eingetragen");
                setBusy("");
                if (ok) mutate();
              }}
            >
              <Send className="h-3.5 w-3.5" />
              Befehle eintragen
            </Button>
            <Button variant="primary" size="sm" loading={busy === "save"} disabled={!complete} onClick={save}>
              Speichern
            </Button>
          </div>
        </div>
      </div>
      <ConfirmDialog
        open={removing}
        onClose={() => setRemoving(false)}
        title="Discord-Bot entfernen?"
        text="Application-ID, Public Key, Bot-Token und alle Regeln werden gelöscht. Befehle in Discord antworten danach nicht mehr, bis du den Bot neu einrichtest."
        confirm="Entfernen"
        danger
        onConfirm={async () => {
          await toast.run(() => api.delete(`/projects/${project.id}/discord`), "Discord-Bot entfernt");
          mutate();
        }}
      />
    </Card>
  );
}
