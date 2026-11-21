"use client";

import dynamic from "next/dynamic";
import { Info, Lock, RotateCcw, Save } from "lucide-react";
import { useCallback, useEffect, useMemo, useState } from "react";
import { Badge, Button, Card, CardHeader, Checkbox, EmptyState, SearchInput, Tabs, cx } from "@/components/ui/primitives";
import { ConfirmDialog, useToast } from "@/components/ui/feedback";
import { ErrorState, Loading } from "@/components/ui/async";
import { ApiError, api, fetchText, putText, useApi } from "@/lib/api";
import { knownPropertyKeys, propertyGroups, propertyProblem, type PropertyField } from "@/lib/properties";
import type { Server, ServerProperties } from "@/lib/types";
import { t, tp } from "@/lib/i18n";

const CodeEditor = dynamic(() => import("./code-editor").then((module) => module.CodeEditor), {
  ssr: false,
  loading: () => <Loading label={t("Editor wird geladen")} />,
});

type View = "form" | "raw";

/** server.properties als Formular (die wichtigsten Schlüssel mit Erklärung) und als Rohtext im Editor. */
export function PropertiesTab({ server, canWrite }: { server: Server; canWrite: boolean }) {
  const [view, setView] = useState<View>("form");
  const running = server.view?.state === "running" || server.view?.state === "starting";
  return (
    <div className="space-y-5">
      <div className="flex gap-3 rounded-lg border border-info/25 bg-info/10 px-4 py-3 text-xs text-info">
        <Info className="mt-0.5 h-4 w-4 shrink-0" />
        <p>
          {t("Änderungen an der server.properties gelten erst nach einem Neustart des Servers.")}
          {running && t(" Der Server läuft gerade – starte ihn nach dem Speichern neu.")}
        </p>
      </div>
      <Tabs
        value={view}
        onChange={setView}
        items={[
          { id: "form", label: t("Formular") },
          { id: "raw", label: t("Rohansicht") },
        ]}
      />
      <div key={view} className="animate-fade-in">
        {view === "form" ? <PropertiesForm serverId={server.id} canWrite={canWrite} /> : <RawProperties serverId={server.id} canWrite={canWrite} />}
      </div>
    </div>
  );
}

function PropertiesForm({ serverId, canWrite }: { serverId: string; canWrite: boolean }) {
  const toast = useToast();
  const { data, error, mutate } = useApi<ServerProperties>(`/servers/${serverId}/properties`);
  const [draft, setDraft] = useState<Record<string, string>>({});
  const [saving, setSaving] = useState(false);
  const [filter, setFilter] = useState("");

  const current = useMemo(() => Object.fromEntries((data?.entries ?? []).map((entry) => [entry.key, entry.value])), [data]);
  // Geändert ist, was vom Stand der Datei abweicht (fehlende Schlüssel zählen mit ihrem Standardwert)
  const changes = useMemo(() => {
    const out: Record<string, string> = {};
    for (const [key, value] of Object.entries(draft)) if (value !== current[key]) out[key] = value;
    return out;
  }, [draft, current]);
  const fields = useMemo(() => new Map(propertyGroups().flatMap((group) => group.fields.map((field) => [field.key, field] as const))), []);
  const problems = Object.entries(changes)
    .map(([key, value]) => {
      const field = fields.get(key);
      return field ? propertyProblem(field, value) : null;
    })
    .filter(Boolean);
  const dirty = Object.keys(changes).length > 0;

  // Ungespeicherte Änderungen nicht stillschweigend verlieren
  useEffect(() => {
    if (!dirty) return;
    const warn = (event: BeforeUnloadEvent) => event.preventDefault();
    window.addEventListener("beforeunload", warn);
    return () => window.removeEventListener("beforeunload", warn);
  }, [dirty]);

  if (error) return <ErrorState error={error} retry={() => mutate()} />;
  if (!data) return <Loading />;

  const locked = data.locked ?? {};
  const valueOf = (key: string, fallback: string) => draft[key] ?? current[key] ?? fallback;
  const set = (key: string, value: string) => setDraft((previous) => ({ ...previous, [key]: value }));
  const unknown = data.entries.filter((entry) => !knownPropertyKeys.has(entry.key) && !locked[entry.key]);
  const shownUnknown = unknown.filter((entry) => entry.key.toLowerCase().includes(filter.trim().toLowerCase()));

  async function save() {
    setSaving(true);
    const ok = await toast.run(() => api.put(`/servers/${serverId}/properties`, { values: changes }), t("Gespeichert – gilt nach dem nächsten Neustart"));
    setSaving(false);
    if (ok) {
      setDraft({});
      mutate();
    }
  }

  return (
    <div className="space-y-5">
      {!data.exists && (
        <p className="rounded-lg border border-white/[0.07] bg-white/[0.02] px-4 py-3 text-xs text-muted">
          {t("Noch gibt es keine server.properties – sie entsteht beim ersten Start. Was du hier speicherst, übernimmt der Server dabei; die übrigen Werte ergänzt er selbst.")}
        </p>
      )}
      {propertyGroups().map((group) => (
        <Card key={group.title}>
          <CardHeader title={group.title} description={group.description} />
          <div className="grid gap-4 px-5 py-4 md:grid-cols-2">
            {group.fields.map((field) => (
              <FieldInput
                key={field.key}
                field={field}
                value={valueOf(field.key, field.fallback)}
                changed={field.key in changes}
                missing={!(field.key in current)}
                lockedReason={locked[field.key]}
                disabled={!canWrite}
                onChange={(value) => set(field.key, value)}
              />
            ))}
          </div>
        </Card>
      ))}

      <Card>
        <CardHeader title={t("Weitere Einstellungen")} description={t("Alle übrigen Schlüssel der Datei, auch die von Plugins und neueren Versionen. Sie bleiben beim Speichern erhalten. Neue Schlüssel legst du in der Rohansicht an.")} />
        {unknown.length === 0 ? (
          <EmptyState icon={<Info className="h-6 w-6" />} title={t("Keine weiteren Schlüssel")} text={t("Die Datei enthält nur die Einstellungen oben.")} />
        ) : (
          <div className="px-5 py-4">
            {unknown.length > 8 && <SearchInput className="mb-3" value={filter} onChange={(event) => setFilter(event.target.value)} placeholder={t("Schlüssel suchen")} />}
            <div className="divide-y divide-white/[0.05]">
              {shownUnknown.map((entry) => (
                <div key={entry.key} className="flex flex-wrap items-center gap-x-4 gap-y-1.5 py-2">
                  <label htmlFor={`prop-${entry.key}`} className="min-w-[200px] flex-1 truncate font-mono text-xs text-ink/80">
                    {entry.key}
                  </label>
                  <input id={`prop-${entry.key}`} className={cx("field max-w-md font-mono text-xs", entry.key in changes && "border-warn/50")} value={valueOf(entry.key, "")} disabled={!canWrite} onChange={(event) => set(entry.key, event.target.value)} />
                </div>
              ))}
              {shownUnknown.length === 0 && <p className="py-3 text-xs text-muted">{t("Kein Schlüssel passt zur Suche.")}</p>}
            </div>
          </div>
        )}
      </Card>

      {Object.keys(locked).length > 0 && (
        <p className="flex gap-2 text-xs text-muted">
          <Lock className="mt-0.5 h-3.5 w-3.5 shrink-0" />
          {t("Von Lodestone verwaltet und hier gesperrt:")} {Object.keys(locked).join(", ")}.
        </p>
      )}

      {canWrite && (
        <div className="sticky bottom-4 z-10 flex flex-wrap items-center justify-end gap-3 rounded-xl border border-white/[0.07] bg-surface px-4 py-3 shadow-panel">
          <p className="mr-auto text-xs text-muted">{problems[0] ?? (dirty ? tp("changesPending", Object.keys(changes).length) : t("Keine Änderungen"))}</p>
          <Button variant="ghost" disabled={!dirty || saving} onClick={() => setDraft({})}>
            <RotateCcw className="h-3.5 w-3.5" />
            {t("Verwerfen")}
          </Button>
          <Button variant="primary" loading={saving} disabled={!dirty || problems.length > 0} onClick={save}>
            <Save className="h-3.5 w-3.5" />
            {t("Speichern")}
          </Button>
        </div>
      )}
    </div>
  );
}

function FieldInput({
  field,
  value,
  changed,
  missing,
  lockedReason,
  disabled,
  onChange,
}: {
  field: PropertyField;
  value: string;
  changed: boolean;
  missing: boolean;
  lockedReason?: string;
  disabled: boolean;
  onChange: (value: string) => void;
}) {
  const id = `prop-${field.key}`;
  const off = disabled || !!lockedReason;
  const problem = changed ? propertyProblem(field, value) : null;
  const meta = (
    <>
      {lockedReason ? (
        <p className="mt-1.5 flex gap-1.5 text-xs text-warn">
          <Lock className="mt-0.5 h-3 w-3 shrink-0" />
          {lockedReason}
        </p>
      ) : (
        <p className="mt-1.5 text-xs text-muted">{field.help}</p>
      )}
      {problem && <p className="mt-1 text-xs text-bad">{problem}</p>}
    </>
  );
  const title = (
    <span className="flex items-center gap-2">
      {field.label}
      <span className="font-mono text-[10.5px] font-normal text-muted/70">{field.key}</span>
      {changed && <Badge tone="warn">{t("Geändert")}</Badge>}
      {missing && !changed && <Badge>{t("Standard")}</Badge>}
    </span>
  );

  if (field.type === "bool") {
    const checked = value === "true";
    return (
      <div className={cx("rounded-lg border border-white/[0.07] bg-white/[0.02] px-3 py-2.5", off && "opacity-60")}>
        <div className={cx("flex items-center gap-3", !off && "cursor-pointer")} onClick={() => !off && onChange(checked ? "false" : "true")}>
          <Checkbox checked={checked} onChange={(next) => !off && onChange(next ? "true" : "false")} label={field.label} />
          <span className="text-sm">{title}</span>
        </div>
        {meta}
      </div>
    );
  }

  return (
    <div>
      <label htmlFor={id} className="label">
        {title}
      </label>
      {field.type === "enum" ? (
        <select id={id} className="field" value={value} disabled={off} onChange={(event) => onChange(event.target.value)}>
          {!field.options.some((option) => option.value === value) && <option value={value}>{value}</option>}
          {field.options.map((option) => (
            <option key={option.value} value={option.value}>
              {option.label}
            </option>
          ))}
        </select>
      ) : field.type === "int" ? (
        <input id={id} type="number" inputMode="numeric" className="field tabular-nums" min={field.min} max={field.max} value={value} disabled={off} onChange={(event) => onChange(event.target.value)} />
      ) : field.multiline ? (
        <textarea id={id} rows={2} className="field resize-y font-mono text-xs" maxLength={field.maxLength} value={value} disabled={off} placeholder={field.placeholder} onChange={(event) => onChange(event.target.value)} />
      ) : (
        <input id={id} className="field" maxLength={field.maxLength} value={value} disabled={off} placeholder={field.placeholder} onChange={(event) => onChange(event.target.value)} />
      )}
      {meta}
    </div>
  );
}

const RAW_PATH = "server.properties";

function RawProperties({ serverId, canWrite }: { serverId: string; canWrite: boolean }) {
  const toast = useToast();
  const [original, setOriginal] = useState<string | null>(null);
  const [text, setText] = useState("");
  const [failure, setFailure] = useState<Error | null>(null);
  const [saving, setSaving] = useState(false);
  const [confirmReload, setConfirmReload] = useState(false);
  const [version, setVersion] = useState(0);
  const dirty = original !== null && text !== original;
  const path = `/servers/${serverId}/files/content?path=${encodeURIComponent(RAW_PATH)}`;

  useEffect(() => {
    setFailure(null);
    setOriginal(null);
    fetchText(path)
      .then((content) => {
        setOriginal(content);
        setText(content);
      })
      .catch((error: Error) => setFailure(error));
  }, [path, version]);

  const save = useCallback(async () => {
    setSaving(true);
    const ok = await toast.run(() => putText(path, text), t("Gespeichert – gilt nach dem nächsten Neustart"));
    setSaving(false);
    if (ok) setOriginal(text);
  }, [path, text, toast]);

  return (
    <Card className="flex flex-col overflow-hidden">
      <div className="flex flex-wrap items-center justify-between gap-3 border-b border-white/[0.06] px-4 py-2.5">
        <div className="flex min-w-0 items-center gap-2">
          <span className="truncate font-mono text-sm">{RAW_PATH}</span>
          {dirty && <Badge tone="warn">{t("Ungespeichert")}</Badge>}
          {!canWrite && <Badge>{t("Nur lesen")}</Badge>}
        </div>
        <div className="flex items-center gap-2">
          <Button size="sm" variant="ghost" onClick={() => (dirty ? setConfirmReload(true) : setVersion((value) => value + 1))}>
            <RotateCcw className="h-3.5 w-3.5" />
            {t("Neu laden")}
          </Button>
          {canWrite && (
            <Button size="sm" variant="primary" onClick={save} loading={saving} disabled={!dirty}>
              <Save className="h-3.5 w-3.5" />
              {t("Speichern")}
            </Button>
          )}
        </div>
      </div>
      <div className="h-[min(68vh,720px)] min-h-[360px] bg-black/30">
        {failure ? (
          <div className="p-4">
            {failure instanceof ApiError && failure.status === 404 ? (
              <EmptyState icon={<Info className="h-6 w-6" />} title={t("Noch keine server.properties")} text={t("Die Datei entsteht beim ersten Start des Servers. Im Formular kannst du schon vorher Werte festlegen.")} />
            ) : (
              <ErrorState error={failure} retry={() => setVersion((value) => value + 1)} />
            )}
          </div>
        ) : original === null ? (
          <Loading label={t("Datei wird geladen")} />
        ) : (
          <CodeEditor key={version} value={original} language="properties" onChange={setText} onSave={() => canWrite && save()} readOnly={!canWrite} />
        )}
      </div>
      <ConfirmDialog
        open={confirmReload}
        onClose={() => setConfirmReload(false)}
        title={t("Änderungen verwerfen?")}
        text={t("Die Rohansicht hat ungespeicherte Änderungen. Beim Neuladen gehen sie verloren.")}
        confirm={t("Verwerfen")}
        danger
        onConfirm={() => setVersion((value) => value + 1)}
      />
    </Card>
  );
}
