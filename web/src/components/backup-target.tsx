"use client";

import { PlugZap, Trash2 } from "lucide-react";
import { useEffect, useState } from "react";
import { Badge, Button, Card, CardHeader, Checkbox } from "@/components/ui/primitives";
import { ConfirmDialog, useToast } from "@/components/ui/feedback";
import { ErrorState, Loading } from "@/components/ui/async";
import { api, useApi } from "@/lib/api";
import { dateTime } from "@/lib/format";
import type { BackupTarget, Project } from "@/lib/types";
import { t } from "@/lib/i18n";

interface Form {
  endpoint: string;
  region: string;
  bucket: string;
  prefix: string;
  accessKey: string;
  secretKey: string;
  pathStyle: boolean;
}

const examples: { name: string; endpoint: string; region: string }[] = [
  { name: "AWS S3", endpoint: "https://s3.eu-central-1.amazonaws.com", region: "eu-central-1" },
  { name: "Cloudflare R2", endpoint: "https://<konto-id>.r2.cloudflarestorage.com", region: "auto" },
  { name: "Backblaze B2", endpoint: "https://s3.eu-central-003.backblazeb2.com", region: "eu-central-003" },
  { name: "Hetzner", endpoint: "https://fsn1.your-objectstorage.com", region: "fsn1" },
  { name: "MinIO", endpoint: "http://minio.lan:9000", region: "us-east-1" },
];

function formFrom(target: BackupTarget): Form {
  return { endpoint: target.endpoint, region: target.region, bucket: target.bucket, prefix: target.prefix, accessKey: target.accessKey, secretKey: "", pathStyle: target.pathStyle };
}

/** Externes Backup-Ziel (S3-kompatibel) eines Projekts, mit Verbindungstest. */
export function BackupTargetCard({ project }: { project: Project }) {
  const toast = useToast();
  const path = `/projects/${project.id}/backup-target`;
  const { data, error, mutate } = useApi<BackupTarget>(path);
  const [form, setForm] = useState<Form | null>(null);
  const [busy, setBusy] = useState<"" | "save" | "test">("");
  const [removing, setRemoving] = useState(false);

  useEffect(() => {
    if (data) setForm(formFrom(data));
  }, [data]);

  if (error) return <ErrorState error={error} retry={() => mutate()} />;
  if (!data || !form) return <Loading />;

  const patch = (change: Partial<Form>) => setForm({ ...form, ...change });
  const complete = form.endpoint.trim() && form.bucket.trim() && form.accessKey.trim() && (form.secretKey.trim() || data.hasSecret);

  async function run(kind: "save" | "test") {
    setBusy(kind);
    const ok = await toast.run(() => (kind === "save" ? api.put(path, form) : api.post(`${path}/test`, form)), kind === "save" ? t("Backup-Ziel gespeichert") : t("Verbindung klappt"));
    setBusy("");
    if (ok && kind === "save") mutate();
  }

  return (
    <Card>
      <CardHeader
        title={t("Backup-Ziel")}
        description={t("S3-kompatibler Speicher für externe Backups, etwa AWS, Cloudflare R2, Backblaze B2, Hetzner oder MinIO.")}
        action={data.configured ? <Badge tone="good">{t("Eingerichtet")}</Badge> : <Badge>{t("Nicht eingerichtet")}</Badge>}
      />
      <div className="space-y-4 px-5 py-4">
        <div className="grid gap-3 sm:grid-cols-2">
          <div className="sm:col-span-2">
            <label className="label">Endpoint</label>
            <input className="field font-mono text-xs" value={form.endpoint} onChange={(event) => patch({ endpoint: event.target.value })} placeholder="https://s3.eu-central-1.amazonaws.com" spellCheck={false} />
          </div>
          <div>
            <label className="label">Region</label>
            <input className="field" value={form.region} onChange={(event) => patch({ region: event.target.value })} placeholder="eu-central-1" spellCheck={false} />
          </div>
          <div>
            <label className="label">Bucket</label>
            <input className="field" value={form.bucket} onChange={(event) => patch({ bucket: event.target.value })} placeholder="lodestone-backups" spellCheck={false} />
          </div>
          <div>
            <label className="label">{t("Präfix (optional)")}</label>
            <input className="field" value={form.prefix} onChange={(event) => patch({ prefix: event.target.value })} placeholder={t("mein-netzwerk/")} spellCheck={false} />
          </div>
          <div>
            <label className="label">Access Key</label>
            <input className="field font-mono text-xs" value={form.accessKey} onChange={(event) => patch({ accessKey: event.target.value })} autoComplete="off" spellCheck={false} />
          </div>
          <div className="sm:col-span-2">
            <label className="label">Secret</label>
            <input
              className="field font-mono text-xs"
              type="password"
              value={form.secretKey}
              onChange={(event) => patch({ secretKey: event.target.value })}
              autoComplete="new-password"
              placeholder={data.hasSecret ? t("Gespeichert – leer lassen, um es zu behalten") : ""}
            />
            <p className="mt-1 text-xs text-muted">{t("Wird verschlüsselt gespeichert und nie wieder angezeigt.")}</p>
          </div>
        </div>
        <label className="flex cursor-pointer items-center gap-2.5 rounded-lg border border-white/[0.07] bg-white/[0.02] px-3 py-2.5 text-sm">
          <Checkbox checked={form.pathStyle} onChange={(value) => patch({ pathStyle: value })} label={t("Pfad-Adressen")} />
          <span>
            {t("Pfad-Adressen")} <span className="text-xs text-muted">{t("(Endpoint/Bucket/… statt Bucket.Endpoint/…; für MinIO nötig, bei den anderen Anbietern meist egal)")}</span>
          </span>
        </label>
        <div className="space-y-1 text-xs text-muted">
          <p>{t("Beispiele für den Endpoint:")}</p>
          <ul className="space-y-0.5">
            {examples.map((example) => (
              <li key={example.name}>
                {example.name}: <code className="break-all rounded bg-black/30 px-1.5 py-0.5">{example.endpoint}</code> {t("· Region")} <code className="rounded bg-black/30 px-1.5 py-0.5">{example.region}</code>
              </li>
            ))}
          </ul>
          <p>{t("Ziele im internen Netz (z. B. MinIO im LAN) muss der Betreiber mit LODESTONE_BACKUP_ALLOW_INTERNAL=true freigeben.")}</p>
          {data.configured && data.updatedAt ? <p>{t("Zuletzt geändert:")} {dateTime(data.updatedAt)}</p> : null}
        </div>
      </div>
      <div className="flex flex-wrap items-center justify-end gap-2 border-t border-white/[0.06] px-5 py-3">
        {data.configured && (
          <Button variant="ghost" size="sm" className="mr-auto" onClick={() => setRemoving(true)}>
            <Trash2 className="h-3.5 w-3.5" />
            {t("Entfernen")}
          </Button>
        )}
        <Button size="sm" loading={busy === "test"} disabled={!complete || busy !== ""} onClick={() => run("test")}>
          <PlugZap className="h-3.5 w-3.5" />
          {t("Verbindung testen")}
        </Button>
        <Button variant="primary" size="sm" loading={busy === "save"} disabled={!complete || busy !== ""} onClick={() => run("save")}>
          {t("Speichern")}
        </Button>
      </div>
      <ConfirmDialog
        open={removing}
        onClose={() => setRemoving(false)}
        title={t("Backup-Ziel entfernen?")}
        text={t("Das Panel vergisst Endpoint und Zugangsdaten. Externe Backups bleiben im Bucket liegen, lassen sich aber erst wieder laden, wenn du dasselbe Ziel erneut einrichtest. Zeitpläne mit „auch extern hochladen“ schlagen bis dahin fehl.")}
        confirm={t("Entfernen")}
        danger
        onConfirm={async () => {
          if (await toast.run(() => api.delete(path), t("Backup-Ziel entfernt"))) mutate();
        }}
      />
    </Card>
  );
}
