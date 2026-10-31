"use client";

import { useState } from "react";
import { Download, FileArchive, Package, Plus, Trash2, Upload, Wrench } from "lucide-react";
import { AdminSidebar } from "@/components/admin-sidebar";
import { useUser } from "@/components/layout/shell";
import { ErrorState, Loading } from "@/components/ui/async";
import { ConfirmDialog, Dialog, useToast } from "@/components/ui/feedback";
import { Badge, Button, Card, EmptyState, PageHeader, SearchInput, Tabs } from "@/components/ui/primitives";
import { api, apiBase, upload, useApi } from "@/lib/api";
import { bytes } from "@/lib/format";
import type { PositionPackDetail, PositionPackSummary } from "@/lib/types";

export default function PositionPacksPage() {
  const user = useUser();
  const toast = useToast();
  const { data: packs, error, mutate, isLoading } = useApi<PositionPackSummary[]>(
    user.isAdmin ? "/packs" : null,
    { refreshInterval: 15000 },
  );

  const [filter, setFilter] = useState("");
  const [uploadOpen, setUploadOpen] = useState(false);
  const [uploadFile, setUploadFile] = useState<File | null>(null);
  const [uploadProgress, setUploadProgress] = useState(0);
  const [uploadBusy, setUploadBusy] = useState(false);

  const [detailId, setDetailId] = useState<string | null>(null);
  const [deleteTarget, setDeleteTarget] = useState<PositionPackSummary | null>(null);

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

  if (isLoading || !packs) {
    return (
      <>
        <AdminSidebar />
        <Loading />
      </>
    );
  }

  const trimmed = filter.trim().toLowerCase();
  const filtered = packs.filter((pack) => {
    if (!trimmed) return true;
    return (
      pack.name.toLowerCase().includes(trimmed) ||
      pack.id.toLowerCase().includes(trimmed) ||
      (pack.description && pack.description.toLowerCase().includes(trimmed))
    );
  });

  async function handleUpload() {
    if (!uploadFile) return;
    setUploadBusy(true);
    setUploadProgress(0);
    try {
      await upload("/packs/upload", [uploadFile], (fraction) => setUploadProgress(fraction));
      toast.success(`Position-Pack „${uploadFile.name}“ erfolgreich hinzugefügt.`);
      setUploadFile(null);
      setUploadOpen(false);
      await mutate();
    } catch (err) {
      toast.error(err instanceof Error ? err.message : "Upload fehlgeschlagen");
    } finally {
      setUploadBusy(false);
    }
  }

  async function handleDelete(pack: PositionPackSummary) {
    const ok = await toast.run(async () => {
      await api.delete(`/packs/${encodeURIComponent(pack.id)}`);
      await mutate();
    }, `Position-Pack „${pack.name}“ wurde gelöscht.`);
    if (ok) setDeleteTarget(null);
  }

  return (
    <>
      <AdminSidebar />
      <div className="flex flex-col gap-4 sm:flex-row sm:items-center sm:justify-between">
        <PageHeader
          title="Position-Packs"
          description="Verwalte Minecraft-Server-Software, Autokonfigurationen und Skripte als modulare ZIP-Pakete."
        />
        <Button variant="primary" onClick={() => setUploadOpen(true)} className="shrink-0 self-start sm:self-auto">
          <Upload className="h-4 w-4" />
          Pack hochladen
        </Button>
      </div>

      <SearchInput
        value={filter}
        onChange={(event) => setFilter(event.target.value)}
        placeholder="Nach Name, ID oder Beschreibung suchen..."
        className="mb-5 max-w-md"
      />

      {packs.length === 0 ? (
        <Card className="p-8">
          <EmptyState
            icon={<Package className="h-6 w-6" />}
            title="Keine Position-Packs vorhanden"
            text="Lade ein Position-Pack als ZIP-Archiv hoch, um neue Server-Software bereitzustellen."
          />
          <div className="mt-4 flex justify-center">
            <Button variant="primary" onClick={() => setUploadOpen(true)}>
              <Upload className="h-4 w-4" />
              Pack hochladen
            </Button>
          </div>
        </Card>
      ) : filtered.length === 0 ? (
        <Card>
          <EmptyState
            icon={<Package className="h-6 w-6" />}
            title="Keine Position-Packs gefunden"
            text="Kein Position-Pack entspricht dem eingegebenen Suchbegriff."
          />
        </Card>
      ) : (
        <div className="grid gap-4 sm:grid-cols-2 lg:grid-cols-3">
          {filtered.map((pack) => (
            <Card key={pack.id} className="flex flex-col justify-between p-5 transition hover:border-white/[0.12]">
              <div>
                <div className="flex items-start justify-between gap-3">
                  <div>
                    <h3 className="font-semibold tracking-tight text-ink">{pack.name}</h3>
                    <p className="font-mono text-xs text-muted">id: {pack.id}</p>
                  </div>
                  <div className="flex flex-wrap items-center gap-1.5">
                    {pack.kind === "proxy" ? (
                      <Badge tone="accent">Proxy</Badge>
                    ) : (
                      <Badge tone="info">Server</Badge>
                    )}
                    {pack.custom ? (
                      <Badge tone="good">Eigenes Pack</Badge>
                    ) : (
                      <Badge tone="neutral">Standard</Badge>
                    )}
                  </div>
                </div>

                <p className="mt-2 text-xs leading-relaxed text-muted line-clamp-2">
                  {pack.description || "Keine Beschreibung angegeben."}
                </p>

                <div className="mt-3 flex flex-wrap gap-1.5 text-[11px]">
                  {pack.extensions === "plugins" && <Badge tone="warn">Plugins</Badge>}
                  {pack.extensions === "mods" && <Badge tone="good">Mods</Badge>}
                  {pack.hasAutoconfig && <Badge tone="good">Autokonfig</Badge>}
                  {pack.hasScript && <Badge tone="info">Skript</Badge>}
                  {pack.canBeBackend && <Badge tone="accent">Proxy-Backend</Badge>}
                  {pack.canVoice && <Badge tone="info">Voicechat</Badge>}
                </div>
              </div>

              <div className="mt-5 flex items-center justify-between border-t border-white/[0.06] pt-3">
                <span className="text-[11px] text-muted">
                  {pack.version ? `v${pack.version}` : "v1.0.0"}
                  {pack.zipSize > 0 && ` • ${bytes(pack.zipSize)}`}
                </span>
                <div className="flex items-center gap-1.5">
                  <a
                    href={`${apiBase()}/api/packs/${encodeURIComponent(pack.id)}/export`}
                    download
                    className="inline-flex h-8 w-8 items-center justify-center rounded-lg border border-white/[0.08] text-muted transition hover:bg-white/[0.05] hover:text-ink"
                    title="Als ZIP herunterladen"
                  >
                    <Download className="h-3.5 w-3.5" />
                  </a>
                  <Button
                    variant="secondary"
                    size="sm"
                    onClick={() => setDetailId(pack.id)}
                    title="Details und Autokonfiguration"
                  >
                    <Wrench className="h-3.5 w-3.5" />
                    Details
                  </Button>
                  {pack.custom && (
                    <Button
                      variant="ghost"
                      size="sm"
                      onClick={() => setDeleteTarget(pack)}
                      className="text-bad hover:bg-bad/10"
                      title="Position-Pack löschen"
                    >
                      <Trash2 className="h-3.5 w-3.5" />
                    </Button>
                  )}
                </div>
              </div>
            </Card>
          ))}
        </div>
      )}

      {/* Upload Dialog */}
      <Dialog
        open={uploadOpen}
        onClose={() => {
          if (!uploadBusy) {
            setUploadOpen(false);
            setUploadFile(null);
          }
        }}
        title="Position-Pack hochladen"
        description="Wähle ein ZIP-Archiv (.zip oder .positionpack), das meta.json und optionale autoconfig.json bzw. Skripte enthält."
        footer={
          <div className="flex justify-end gap-2">
            <Button variant="secondary" onClick={() => setUploadOpen(false)} disabled={uploadBusy}>
              Abbrechen
            </Button>
            <Button variant="primary" onClick={handleUpload} loading={uploadBusy} disabled={!uploadFile}>
              <Upload className="h-4 w-4" />
              Hochladen
            </Button>
          </div>
        }
      >
        <div className="space-y-4">
          <label className="flex cursor-pointer flex-col items-center justify-center rounded-xl border-2 border-dashed border-white/[0.12] bg-white/[0.02] p-6 text-center transition hover:border-accent hover:bg-accent/5">
            <FileArchive className="h-8 w-8 text-muted" />
            <span className="mt-2 text-sm font-medium text-ink">
              {uploadFile ? uploadFile.name : "ZIP-Archiv auswählen oder hier ablegen"}
            </span>
            <span className="mt-1 text-xs text-muted">
              {uploadFile ? bytes(uploadFile.size) : "Unterstützt .zip und .positionpack (max. 32 MB)"}
            </span>
            <input
              type="file"
              accept=".zip,.positionpack,application/zip"
              className="hidden"
              onChange={(e) => {
                const f = e.target.files?.[0];
                if (f) setUploadFile(f);
              }}
            />
          </label>

          {uploadBusy && (
            <div className="space-y-1">
              <div className="flex justify-between text-xs text-muted">
                <span>Wird hochgeladen...</span>
                <span>{Math.round(uploadProgress * 100)} %</span>
              </div>
              <div className="h-1.5 w-full overflow-hidden rounded-full bg-white/[0.08]">
                <div
                  className="h-full bg-accent transition-all duration-150"
                  style={{ width: `${uploadProgress * 100}%` }}
                />
              </div>
            </div>
          )}
        </div>
      </Dialog>

      {/* Detail & Edit Modal */}
      {detailId && (
        <PackDetailModal
          id={detailId}
          onClose={() => setDetailId(null)}
          onSaved={() => {
            mutate();
          }}
        />
      )}

      {/* Delete Confirmation */}
      {deleteTarget && (
        <ConfirmDialog
          open={true}
          onClose={() => setDeleteTarget(null)}
          onConfirm={() => handleDelete(deleteTarget)}
          title="Position-Pack löschen"
          text={`Möchtest du das Position-Pack „${deleteTarget.name}“ wirklich löschen? Bestehende Server bleiben erhalten, aber die Software steht beim Neu-Anlegen nicht mehr zur Verfügung.`}
          confirm="Position-Pack löschen"
          danger
        />
      )}
    </>
  );
}

function PackDetailModal({
  id,
  onClose,
  onSaved,
}: {
  id: string;
  onClose: () => void;
  onSaved: () => void;
}) {
  const toast = useToast();
  const { data: detail, error, mutate, isLoading } = useApi<PositionPackDetail>(`/packs/${encodeURIComponent(id)}`);
  const [activeTab, setActiveTab] = useState<"meta" | "autoconfig" | "script">("meta");

  const [name, setName] = useState("");
  const [description, setDescription] = useState("");
  const [startup, setStartup] = useState("");
  const [stopCommand, setStopCommand] = useState("");
  const [readyPattern, setReadyPattern] = useState("");
  const [backendSupported, setBackendSupported] = useState(false);
  const [proxyMod, setProxyMod] = useState("");
  const [voiceConfigFile, setVoiceConfigFile] = useState("");

  const [autoconfigJson, setAutoconfigJson] = useState("");
  const [scriptContent, setScriptContent] = useState("");
  const [initialized, setInitialized] = useState(false);
  const [saving, setSaving] = useState(false);

  if (detail && !initialized) {
    setName(detail.meta.name || "");
    setDescription(detail.meta.description || "");
    setStartup(detail.meta.startup || "");
    setStopCommand(detail.meta.stopCommand || "");
    setReadyPattern(detail.meta.readyPattern || "");
    setBackendSupported(detail.meta.backend?.supported ?? false);
    setProxyMod(detail.meta.backend?.proxyMod || "");
    setVoiceConfigFile(detail.meta.backend?.voiceConfigFile || "");

    setAutoconfigJson(JSON.stringify(detail.autoconfig || {}, null, 2));
    setScriptContent(detail.script || "");
    setInitialized(true);
  }

  async function handleSave() {
    if (!detail) return;
    setSaving(true);
    try {
      let parsedAutoconfig = {};
      if (autoconfigJson.trim()) {
        try {
          parsedAutoconfig = JSON.parse(autoconfigJson);
        } catch (e) {
          toast.error("Autokonfiguration enthält ungültiges JSON");
          setSaving(false);
          return;
        }
      }

      const updatedMeta = {
        ...detail.meta,
        name,
        description,
        startup,
        stopCommand,
        readyPattern,
        backend: {
          ...detail.meta.backend,
          supported: backendSupported,
          proxyMod,
          voiceConfigFile,
        },
      };

      await api.put(`/packs/${encodeURIComponent(id)}`, {
        meta: updatedMeta,
        autoconfig: parsedAutoconfig,
        script: scriptContent,
      });
      toast.success("Position-Pack erfolgreich gespeichert.");
      await mutate();
      onSaved();
    } catch (err) {
      toast.error(err instanceof Error ? err.message : "Speichern fehlgeschlagen");
    } finally {
      setSaving(false);
    }
  }

  return (
    <Dialog
      open={true}
      onClose={onClose}
      wide
      title={detail ? `Position-Pack: ${detail.meta.name}` : "Position-Pack wird geladen..."}
      description={detail ? `ID: ${detail.meta.id} • ${detail.custom ? "Eigenes Pack" : "Standard-Pack"}` : undefined}
      footer={
        <div className="flex w-full items-center justify-between">
          <a
            href={`${apiBase()}/api/packs/${encodeURIComponent(id)}/export`}
            download
            className="inline-flex items-center gap-1.5 text-xs text-muted hover:text-ink"
          >
            <Download className="h-3.5 w-3.5" />
            Als ZIP herunterladen
          </a>
          <div className="flex gap-2">
            <Button variant="secondary" onClick={onClose} disabled={saving}>
              Schließen
            </Button>
            <Button variant="primary" onClick={handleSave} loading={saving} disabled={!detail}>
              Änderungen speichern
            </Button>
          </div>
        </div>
      }
    >
      {isLoading || !detail ? (
        <div className="py-8">
          <Loading />
        </div>
      ) : error ? (
        <ErrorState error={error} retry={() => mutate()} />
      ) : (
        <div className="space-y-4">
          <Tabs
            items={[
              { id: "meta", label: "Übersicht & Metadaten" },
              { id: "autoconfig", label: "Autokonfiguration" },
              { id: "script", label: "Skript (autoconfig.sh)" },
            ]}
            value={activeTab}
            onChange={(tab) => setActiveTab(tab as any)}
          />

          {activeTab === "meta" && (
            <div className="space-y-4">
              <div className="grid gap-3 sm:grid-cols-2">
                <div>
                  <label className="text-xs font-medium text-muted">Name</label>
                  <input
                    type="text"
                    value={name}
                    onChange={(e) => setName(e.target.value)}
                    className="mt-1 w-full rounded-lg border border-white/[0.08] bg-white/[0.04] px-3 py-1.5 text-sm text-ink outline-none focus:border-accent"
                  />
                </div>
                <div>
                  <label className="text-xs font-medium text-muted">ID</label>
                  <input
                    type="text"
                    disabled
                    value={detail.meta.id}
                    className="mt-1 w-full cursor-not-allowed rounded-lg border border-white/[0.05] bg-white/[0.02] px-3 py-1.5 font-mono text-sm text-muted"
                  />
                </div>
              </div>

              <div>
                <label className="text-xs font-medium text-muted">Beschreibung</label>
                <textarea
                  rows={2}
                  value={description}
                  onChange={(e) => setDescription(e.target.value)}
                  className="mt-1 w-full rounded-lg border border-white/[0.08] bg-white/[0.04] px-3 py-1.5 text-sm text-ink outline-none focus:border-accent"
                />
              </div>

              <div className="grid gap-3 sm:grid-cols-3">
                <div>
                  <label className="text-xs font-medium text-muted">Typ</label>
                  <div className="mt-1 text-sm font-medium capitalize text-ink">{detail.meta.kind}</div>
                </div>
                <div>
                  <label className="text-xs font-medium text-muted">Erweiterungen</label>
                  <div className="mt-1 text-sm font-medium capitalize text-ink">{detail.meta.extensions}</div>
                </div>
                <div>
                  <label className="text-xs font-medium text-muted">Standard-Port</label>
                  <div className="mt-1 text-sm font-medium text-ink">{detail.meta.defaultPort}</div>
                </div>
              </div>

              <div>
                <label className="text-xs font-medium text-muted">Startbefehl (Startup)</label>
                <input
                  type="text"
                  value={startup}
                  onChange={(e) => setStartup(e.target.value)}
                  className="mt-1 w-full rounded-lg border border-white/[0.08] bg-white/[0.04] px-3 py-1.5 font-mono text-xs text-ink outline-none focus:border-accent"
                />
                <p className="mt-1 text-[11px] text-muted">Platzhalter: {"{{MEMORY}}"} (RAM in MB), {"{{JAR}}"} (Dateiname)</p>
              </div>

              <div className="grid gap-3 sm:grid-cols-2">
                <div>
                  <label className="text-xs font-medium text-muted">Stop-Befehl</label>
                  <input
                    type="text"
                    value={stopCommand}
                    onChange={(e) => setStopCommand(e.target.value)}
                    className="mt-1 w-full rounded-lg border border-white/[0.08] bg-white/[0.04] px-3 py-1.5 font-mono text-xs text-ink outline-none focus:border-accent"
                  />
                </div>
                <div>
                  <label className="text-xs font-medium text-muted">Bereit-Muster (Ready Pattern)</label>
                  <input
                    type="text"
                    value={readyPattern}
                    onChange={(e) => setReadyPattern(e.target.value)}
                    className="mt-1 w-full rounded-lg border border-white/[0.08] bg-white/[0.04] px-3 py-1.5 font-mono text-xs text-ink outline-none focus:border-accent"
                  />
                </div>
              </div>

              <div className="rounded-lg border border-white/[0.07] bg-white/[0.02] p-3 space-y-3">
                <label className="flex items-center gap-2 cursor-pointer">
                  <input
                    type="checkbox"
                    checked={backendSupported}
                    onChange={(e) => setBackendSupported(e.target.checked)}
                    className="rounded border-white/[0.2] bg-white/[0.05] text-accent focus:ring-accent"
                  />
                  <span className="text-xs font-medium text-ink">Kann als Backend hinter Velocity-Proxy laufen</span>
                </label>
                <div className="grid gap-3 sm:grid-cols-2">
                  <div>
                    <label className="text-xs text-muted">Proxy-Mod (Modrinth-Slug für Forwarding)</label>
                    <input
                      type="text"
                      value={proxyMod}
                      onChange={(e) => setProxyMod(e.target.value)}
                      placeholder="z. B. fabricproxy-lite"
                      className="mt-1 w-full rounded-lg border border-white/[0.08] bg-white/[0.04] px-3 py-1.5 text-xs text-ink outline-none focus:border-accent"
                    />
                  </div>
                  <div>
                    <label className="text-xs text-muted">Simple Voice Chat Konfigurationsdatei</label>
                    <input
                      type="text"
                      value={voiceConfigFile}
                      onChange={(e) => setVoiceConfigFile(e.target.value)}
                      placeholder="z. B. plugins/voicechat/voicechat-server.properties"
                      className="mt-1 w-full rounded-lg border border-white/[0.08] bg-white/[0.04] px-3 py-1.5 text-xs text-ink outline-none focus:border-accent"
                    />
                  </div>
                </div>
              </div>
            </div>
          )}

          {activeTab === "autoconfig" && (
            <div className="space-y-3">
              <div className="rounded-lg border border-info/20 bg-info/5 p-3 text-xs text-info">
                In den Werten und Pfaden können Platzhalter verwendet werden:
                <code className="ml-1 text-white">{"{{SECRET}}"}</code>,
                <code className="ml-1 text-white">{"{{VOICE_HOST}}"}</code>,
                <code className="ml-1 text-white">{"{{VOICE_PORT}}"}</code>,
                <code className="ml-1 text-white">{"{{SERVER_NAME}}"}</code>,
                <code className="ml-1 text-white">{"{{SERVER_SLUG}}"}</code>.
              </div>
              <div>
                <label className="text-xs font-medium text-muted">autoconfig.json</label>
                <textarea
                  rows={14}
                  value={autoconfigJson}
                  onChange={(e) => setAutoconfigJson(e.target.value)}
                  className="mt-1 w-full rounded-lg border border-white/[0.08] bg-white/[0.03] p-3 font-mono text-xs text-ink outline-none focus:border-accent"
                  placeholder="{}"
                />
              </div>
            </div>
          )}

          {activeTab === "script" && (
            <div className="space-y-3">
              <div className="rounded-lg border border-white/[0.08] bg-white/[0.02] p-3 text-xs text-muted">
                Dieses Skript (<code className="text-white">autoconfig.sh</code>) wird im ZIP-Paket getrennt von den Metadaten abgelegt und bei der Bereitstellung auf den Server geschrieben.
              </div>
              <div>
                <label className="text-xs font-medium text-muted">autoconfig.sh (Shell-Skript)</label>
                <textarea
                  rows={14}
                  value={scriptContent}
                  onChange={(e) => setScriptContent(e.target.value)}
                  className="mt-1 w-full rounded-lg border border-white/[0.08] bg-white/[0.03] p-3 font-mono text-xs text-ink outline-none focus:border-accent"
                  placeholder="#!/bin/sh"
                />
              </div>
            </div>
          )}
        </div>
      )}
    </Dialog>
  );
}
