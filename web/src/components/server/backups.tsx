"use client";

import { Archive, ArchiveRestore, CloudUpload, Download, Loader2, Lock, LockOpen, Plus, Trash2 } from "lucide-react";
import { useEffect, useState } from "react";
import { Badge, Button, Card, CardHeader, Checkbox, EmptyState, RowMenu, type MenuItem } from "@/components/ui/primitives";
import { ConfirmDialog, Dialog, useToast } from "@/components/ui/feedback";
import { ErrorState, Loading } from "@/components/ui/async";
import { api, downloadUrl, useApi } from "@/lib/api";
import { ago, bytes, dateTime } from "@/lib/format";
import { can, type BackupSettings, type Permission, type ServerBackup } from "@/lib/types";
import { t } from "@/lib/i18n";

type Removal = { backup: ServerBackup; where: "" | "local" | "remote" };

// Jede Aktion hängt an ihrem eigenen Recht: Erstellen und Hochladen „backups.create“, Wiederherstellen „backups.restore“,
// Löschen und Festhalten „backups.delete“, Herunterladen zusätzlich „files.read“, Ausschlüsse „settings“.
export function BackupsTab({ serverId, permissions, offline }: { serverId: string; permissions: Permission[]; offline: boolean }) {
  const toast = useToast();
  const canCreate = can(permissions, "backups.create");
  const canRestore = can(permissions, "backups.restore");
  const canDelete = can(permissions, "backups.delete");
  const canDownload = can(permissions, "files.read");
  const { data, error, mutate } = useApi<ServerBackup[]>(`/servers/${serverId}/backups`, {
    // Solange etwas entsteht oder hochlädt, regelmäßig nachsehen
    refreshInterval: (latest) => (latest?.some((backup) => backup.status === "running" || backup.upload === "running") ? 2000 : 0),
  });
  const settings = useApi<BackupSettings>(`/servers/${serverId}/backup-settings`);
  const targetConfigured = !!settings.data?.targetConfigured;
  const [creating, setCreating] = useState(false);
  const [label, setLabel] = useState("");
  const [remote, setRemote] = useState(false);
  const [restore, setRestore] = useState<ServerBackup | null>(null);
  const [remove, setRemove] = useState<Removal | null>(null);

  if (error) return <ErrorState error={error} retry={() => mutate()} />;
  if (!data) return <Loading />;

  const path = (backup: ServerBackup) => `/servers/${serverId}/backups/${backup.name}`;

  async function lock(backup: ServerBackup, locked: boolean) {
    if (await toast.run(() => api.post(`${path(backup)}/lock`, { locked }), locked ? t("Backup festgehalten") : t("Backup entsperrt"))) mutate();
  }

  async function upload(backup: ServerBackup) {
    if (await toast.run(() => api.post(`${path(backup)}/upload`), t("Hochladen gestartet"))) mutate();
  }

  function menu(backup: ServerBackup): (MenuItem | "separator")[] {
    const lockedHint = backup.locked ? t("Erst entsperren") : undefined;
    const items: (MenuItem | "separator")[] = [];
    if (canCreate && backup.local && !backup.remote && targetConfigured && backup.upload !== "running") {
      items.push({ label: backup.upload === "failed" ? t("Erneut hochladen") : t("Extern hochladen"), icon: <CloudUpload className="h-3.5 w-3.5" />, onClick: () => upload(backup) });
    }
    if (!canDelete) return items;
    if (backup.local || backup.remote) {
      items.push(
        backup.locked
          ? { label: t("Entsperren"), icon: <LockOpen className="h-3.5 w-3.5" />, onClick: () => lock(backup, false) }
          : { label: t("Festhalten"), icon: <Lock className="h-3.5 w-3.5" />, hint: t("Wird nicht automatisch gelöscht"), onClick: () => lock(backup, true) },
        "separator",
      );
    }
    if (backup.local && backup.remote) {
      items.push({ label: t("Nur lokal löschen"), icon: <Trash2 className="h-3.5 w-3.5" />, danger: true, disabled: backup.locked, hint: lockedHint, onClick: () => setRemove({ backup, where: "local" }) });
      items.push({ label: t("Nur extern löschen"), icon: <Trash2 className="h-3.5 w-3.5" />, danger: true, disabled: backup.locked, hint: lockedHint, onClick: () => setRemove({ backup, where: "remote" }) });
    }
    items.push({ label: t("Löschen"), icon: <Trash2 className="h-3.5 w-3.5" />, danger: true, disabled: backup.locked, hint: lockedHint, onClick: () => setRemove({ backup, where: "" }) });
    return items;
  }

  return (
    <div className="space-y-5">
      <Card>
        <CardHeader
          title="Backups"
          description={t("Archive des Serverordners, lokal auf dem Node und auf Wunsch im Backup-Ziel des Projekts.")}
          action={
            canCreate && (
              <Button size="sm" variant="primary" onClick={() => setCreating(true)}>
                <Plus className="h-3.5 w-3.5" />
                {t("Backup erstellen")}
              </Button>
            )
          }
        />
        {data.length === 0 ? (
          <EmptyState icon={<Archive className="h-6 w-6" />} title={t("Noch keine Backups")} text={canCreate ? t("Erstelle ein Backup oder lege unter Zeitpläne ein nächtliches an.") : t("Hier erscheinen die Backups dieses Servers.")} />
        ) : (
          <div className="divide-y divide-white/[0.05]">
            {data.map((backup) => {
              const running = backup.status === "running";
              const restorable = canRestore && backup.status === "done" && (backup.local || (backup.remote && !backup.otherTarget));
              const items = menu(backup);
              return (
                <div key={backup.name} className="flex flex-wrap items-center gap-x-4 gap-y-2 px-5 py-3">
                  {running ? <Loader2 className="h-4 w-4 animate-spin text-muted" /> : backup.locked ? <Lock className="h-4 w-4 text-muted" aria-label={t("Festgehalten")} /> : <Archive className="h-4 w-4 text-muted" />}
                  <div className="min-w-[180px] flex-1">
                    <p className="text-sm font-medium">{backup.label || dateTime(backup.created)}</p>
                    <p className="text-xs text-muted tabular-nums">
                      {backup.label && `${dateTime(backup.created)} · `}
                      {ago(backup.created)} · {running && !backup.local ? t("wird erstellt …") : bytes(backup.size)}
                    </p>
                    {backup.error && <p className="mt-1 text-xs text-bad">{backup.error}</p>}
                  </div>
                  <div className="flex flex-wrap items-center gap-1.5">
                    {running && backup.upload !== "running" && <Badge tone="info">{t("Läuft")}</Badge>}
                    {backup.upload === "running" && <Badge tone="info">{t("Lädt hoch")}{backup.progress ? ` · ${backup.progress} %` : ""}</Badge>}
                    {backup.status === "failed" && <Badge tone="bad">{t("Fehlgeschlagen")}</Badge>}
                    {backup.upload === "failed" && <Badge tone="bad">{t("Hochladen fehlgeschlagen")}</Badge>}
                    {backup.local && <Badge>{t("Lokal")}</Badge>}
                    {backup.remote && <Badge tone={backup.otherTarget ? "warn" : "accent"}>{backup.otherTarget ? t("Früheres Ziel") : t("Extern")}</Badge>}
                    {backup.locked && <Badge>{t("Festgehalten")}</Badge>}
                  </div>
                  {!running && (
                    <div className="flex items-center gap-1">
                      {restorable && (
                        <Button size="sm" disabled={!offline} title={offline ? undefined : t("Der Server muss aus sein")} onClick={() => setRestore(backup)}>
                          <ArchiveRestore className="h-3.5 w-3.5" />
                          {t("Wiederherstellen")}
                        </Button>
                      )}
                      {backup.local && canDownload && (
                        <Button variant="ghost" size="icon-sm" aria-label={t("Herunterladen")} onClick={() => window.open(downloadUrl(`${path(backup)}/download`))}>
                          <Download className="h-3.5 w-3.5" />
                        </Button>
                      )}
                      {items.length > 0 && <RowMenu items={items} />}
                    </div>
                  )}
                </div>
              );
            })}
          </div>
        )}

        <Dialog
          open={creating}
          onClose={() => setCreating(false)}
          title={t("Backup erstellen")}
          footer={
            <>
              <Button variant="ghost" onClick={() => setCreating(false)}>
                {t("Abbrechen")}
              </Button>
              <Button
                variant="primary"
                onClick={async () => {
                  if (await toast.run(() => api.post(`/servers/${serverId}/backups`, { label, remote: remote && targetConfigured }), t("Backup wird erstellt"))) {
                    setCreating(false);
                    setLabel("");
                    setRemote(false);
                    mutate();
                  }
                }}
              >
                {t("Erstellen")}
              </Button>
            </>
          }
        >
          <div>
            <label className="label">{t("Bezeichnung (optional)")}</label>
            <input className="field" value={label} onChange={(event) => setLabel(event.target.value)} maxLength={40} placeholder={t("vor-dem-update")} autoFocus />
          </div>
          {targetConfigured ? (
            <label className="flex cursor-pointer items-center gap-2.5 rounded-lg border border-white/[0.07] bg-white/[0.02] px-3 py-2.5 text-sm">
              <Checkbox checked={remote} onChange={setRemote} label={t("Auch zum Backup-Ziel hochladen")} />
              {t("Auch zum Backup-Ziel hochladen")}
            </label>
          ) : (
            <p className="text-xs text-muted">{t("Backups extern ablegen: Ein Admin richtet dafür unter Projekt → Einstellungen ein Backup-Ziel ein.")}</p>
          )}
        </Dialog>

        <ConfirmDialog
          open={!!restore}
          onClose={() => setRestore(null)}
          title={t("Backup wiederherstellen?")}
          text={
            (restore && !restore.local ? t("Das Backup wird zuerst vom Backup-Ziel geladen. ") : "") +
            t("Alle Dateien des Servers werden durch den Stand des Backups ersetzt. Aktuelle Welten und Einstellungen gehen verloren – erstelle vorher ein Backup, wenn du den heutigen Stand behalten willst. Ausgeschlossene Ordner bleiben unverändert.")
          }
          confirm={t("Wiederherstellen")}
          danger
          onConfirm={async () => {
            if (!restore) return;
            const action = restore.local ? "restore" : "restore-remote";
            await toast.run(() => api.post(`${path(restore)}/${action}`), t("Wiederherstellung läuft"));
            mutate();
          }}
        />
        <ConfirmDialog
          open={!!remove}
          onClose={() => setRemove(null)}
          title={remove?.where === "local" ? t("Lokale Kopie löschen?") : remove?.where === "remote" ? t("Externe Kopie löschen?") : t("Backup löschen?")}
          text={
            remove?.where === "local"
              ? t("Das Archiv auf dem Node wird gelöscht. Die Kopie im Backup-Ziel bleibt erhalten.")
              : remove?.where === "remote"
                ? t("Die Kopie im Backup-Ziel wird gelöscht. Das Archiv auf dem Node bleibt erhalten.")
                : t("Das Backup wird lokal und im Backup-Ziel unwiderruflich gelöscht.")
          }
          confirm={t("Löschen")}
          danger
          onConfirm={async () => {
            if (!remove) return;
            await toast.run(() => api.delete(`${path(remove.backup)}${remove.where ? `?where=${remove.where}` : ""}`), t("Gelöscht"));
            mutate();
          }}
        />
      </Card>

      <BackupSettingsCard serverId={serverId} canManage={can(permissions, "settings")} />
    </div>
  );
}

/** Ausschlüsse und externe Aufbewahrung eines Servers. */
function BackupSettingsCard({ serverId, canManage }: { serverId: string; canManage: boolean }) {
  const toast = useToast();
  const { data, error, mutate } = useApi<BackupSettings>(`/servers/${serverId}/backup-settings`);
  const [exclude, setExclude] = useState("");
  const [keep, setKeep] = useState("7");
  const [busy, setBusy] = useState(false);

  useEffect(() => {
    if (!data) return;
    setExclude(data.exclude.join("\n"));
    setKeep(String(data.remoteKeep));
  }, [data]);

  if (error) return <ErrorState error={error} retry={() => mutate()} />;
  if (!data) return <Loading />;

  const patterns = exclude.split("\n").map((line) => line.trim()).filter(Boolean);
  const changed = patterns.join("\n") !== data.exclude.join("\n") || Number(keep) !== data.remoteKeep;

  async function save() {
    setBusy(true);
    const ok = await toast.run(() => api.put(`/servers/${serverId}/backup-settings`, { exclude: patterns, remoteKeep: Number(keep) || 0 }), t("Gespeichert"));
    setBusy(false);
    if (ok) mutate();
  }

  return (
    <Card>
      <CardHeader title={t("Backup-Einstellungen")} description={t("Was nicht ins Archiv soll und wie viele Backups extern bleiben. Lokal: Einstellungen → Allgemein.")} />
      <div className="grid gap-4 px-5 py-4 md:grid-cols-[1fr_220px]">
        <div>
          <label className="label">{t("Ausschlüsse (ein Pfadmuster pro Zeile)")}</label>
          <textarea className="field font-mono text-xs" rows={4} value={exclude} onChange={(event) => setExclude(event.target.value)} disabled={!canManage} spellCheck={false} placeholder={"dynmap/**\nlogs/**\ncache/**"} />
          <p className="mt-1 text-xs text-muted">
            <code>**</code> {t("steht für beliebig viele Ordner,")} <code>*</code> {t("für einen Namensteil. Muster ohne")} <code>/</code> {t("gelten überall (")}<code>*.log</code>{t("). Ausgeschlossenes bleibt beim Wiederherstellen unangetastet.")}
          </p>
        </div>
        <div>
          <label className="label">{t("Extern aufbewahren")}</label>
          <input className="field" inputMode="numeric" value={keep} onChange={(event) => setKeep(event.target.value.replace(/\D/g, ""))} disabled={!canManage} />
          <p className="mt-1 text-xs text-muted">{t("Ältere werden nach jedem Hochladen gelöscht. Festgehaltene zählen nicht mit.")}</p>
        </div>
      </div>
      {canManage && (
        <div className="flex justify-end border-t border-white/[0.06] px-5 py-3">
          <Button variant="primary" size="sm" loading={busy} disabled={!changed} onClick={save}>
            {t("Speichern")}
          </Button>
        </div>
      )}
    </Card>
  );
}
