"use client";

import { Download } from "lucide-react";
import { useState } from "react";
import { Button, Checkbox, Tabs } from "@/components/ui/primitives";
import { ErrorState, Loading } from "@/components/ui/async";
import { Dialog, useToast } from "@/components/ui/feedback";
import { api, apiBase, useApi } from "@/lib/api";
import type { PositionPackDetail } from "@/lib/types";
import { t } from "@/lib/i18n";

type TabId = "meta" | "autoconfig" | "script";

export function PackDetailModal({
  id,
  onClose,
  onSaved,
}: {
  id: string;
  onClose: () => void;
  onSaved: () => void;
}) {
  const toast = useToast();
  const { data: detail, isLoading, error, mutate } = useApi<PositionPackDetail>(`/packs/${encodeURIComponent(id)}`);
  const [activeTab, setActiveTab] = useState<TabId>("meta");

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
        } catch {
          toast.error(t("Die Autokonfiguration ist kein gültiges JSON."));
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
      toast.success(t("Position-Pack gespeichert"));
      await mutate();
      onSaved();
    } catch (err) {
      toast.error(err instanceof Error ? err.message : t("Speichern fehlgeschlagen"));
    } finally {
      setSaving(false);
    }
  }

  const isOverridden = detail?.overridden ?? false;
  const isCustom = detail?.custom ?? false;
  const packTypeLabel = isOverridden ? t("Überschriebenes Standard-Pack") : isCustom ? t("Eigenes Pack") : t("Standard-Pack");

  return (
    <Dialog
      open={true}
      onClose={onClose}
      wide
      title={detail ? t("Position-Pack: {name}", { name: detail.meta.name }) : t("Position-Pack wird geladen …")}
      description={detail ? `${t("ID: {id}", { id: detail.meta.id })} · ${packTypeLabel}` : undefined}
      footer={
        <div className="flex w-full flex-wrap items-center justify-between gap-2">
          <a
            href={`${apiBase()}/api/packs/${encodeURIComponent(id)}/export`}
            download
            className="inline-flex items-center gap-1.5 text-xs text-muted hover:text-ink"
          >
            <Download className="h-3.5 w-3.5" />
            {t("Als ZIP herunterladen")}
          </a>
          <div className="flex gap-2">
            <Button variant="secondary" onClick={onClose} disabled={saving}>
              {t("Schließen")}
            </Button>
            <Button variant="primary" onClick={handleSave} loading={saving} disabled={!detail}>
              {t("Änderungen speichern")}
            </Button>
          </div>
        </div>
      }
    >
      {error ? (
        <ErrorState inline error={error} retry={() => mutate()} />
      ) : isLoading || !detail ? (
        <Loading />
      ) : (
        <div className="space-y-4">
          <Tabs<TabId>
            items={[
              { id: "meta", label: t("Übersicht und Metadaten") },
              { id: "autoconfig", label: t("Autokonfiguration") },
              { id: "script", label: t("Skript (autoconfig.sh)") },
            ]}
            value={activeTab}
            onChange={setActiveTab}
          />

          {activeTab === "meta" && (
            <div className="space-y-4">
              <div className="grid gap-3 sm:grid-cols-2">
                <div>
                  <label htmlFor="pack-name" className="label">
                    {t("Name")}
                  </label>
                  <input
                    id="pack-name"
                    type="text"
                    value={name}
                    onChange={(e) => setName(e.target.value)}
                    className="field mt-1"
                  />
                </div>
                <div>
                  <label htmlFor="pack-id" className="label">
                    {t("ID")}
                  </label>
                  <input
                    id="pack-id"
                    type="text"
                    disabled
                    value={detail.meta.id}
                    className="field mt-1 cursor-not-allowed font-mono opacity-60"
                  />
                </div>
              </div>

              <div>
                <label htmlFor="pack-description" className="label">
                  {t("Beschreibung")}
                </label>
                <textarea
                  id="pack-description"
                  rows={2}
                  value={description}
                  onChange={(e) => setDescription(e.target.value)}
                  className="field mt-1"
                />
              </div>

              <div className="grid gap-3 sm:grid-cols-3">
                <div>
                  <span className="label">{t("Typ")}</span>
                  <div className="mt-1 text-sm font-medium capitalize text-ink">{detail.meta.kind}</div>
                </div>
                <div>
                  <span className="label">{t("Erweiterungen")}</span>
                  <div className="mt-1 text-sm font-medium capitalize text-ink">{detail.meta.extensions}</div>
                </div>
                <div>
                  <span className="label">{t("Standard-Port")}</span>
                  <div className="mt-1 text-sm font-medium text-ink">{detail.meta.defaultPort}</div>
                </div>
              </div>

              <div>
                <label htmlFor="pack-startup" className="label">
                  {t("Startbefehl")}
                </label>
                <input
                  id="pack-startup"
                  type="text"
                  value={startup}
                  onChange={(e) => setStartup(e.target.value)}
                  className="field mt-1 font-mono text-xs"
                />
                <p className="mt-1 text-[11px] text-muted">
                  {t("Platzhalter: {memory} (RAM in MB), {jar} (Dateiname)", { memory: "{{MEMORY}}", jar: "{{JAR}}" })}
                </p>
              </div>

              <div className="grid gap-3 sm:grid-cols-2">
                <div>
                  <label htmlFor="pack-stop-command" className="label">
                    {t("Stopp-Befehl")}
                  </label>
                  <input
                    id="pack-stop-command"
                    type="text"
                    value={stopCommand}
                    onChange={(e) => setStopCommand(e.target.value)}
                    className="field mt-1 font-mono text-xs"
                  />
                </div>
                <div>
                  <label htmlFor="pack-ready-pattern" className="label">
                    {t("Muster für „bereit“ in der Konsole")}
                  </label>
                  <input
                    id="pack-ready-pattern"
                    type="text"
                    value={readyPattern}
                    onChange={(e) => setReadyPattern(e.target.value)}
                    className="field mt-1 font-mono text-xs"
                  />
                </div>
              </div>

              <div className="space-y-3 rounded-lg border border-white/[0.07] bg-white/[0.02] p-3">
                <div className="flex items-center gap-2">
                  <Checkbox
                    checked={backendSupported}
                    onChange={setBackendSupported}
                    label={t("Kann als Backend hinter dem Velocity-Proxy laufen")}
                  />
                  <span
                    aria-hidden="true"
                    onClick={() => setBackendSupported(!backendSupported)}
                    className="cursor-pointer select-none text-xs font-medium text-ink"
                  >
                    {t("Kann als Backend hinter dem Velocity-Proxy laufen")}
                  </span>
                </div>
                <div className="grid gap-3 sm:grid-cols-2">
                  <div>
                    <label htmlFor="pack-proxy-mod" className="label text-muted">
                      {t("Proxy-Mod (Modrinth-Slug für die Weiterleitung)")}
                    </label>
                    <input
                      id="pack-proxy-mod"
                      type="text"
                      value={proxyMod}
                      onChange={(e) => setProxyMod(e.target.value)}
                      placeholder={t("z. B. {example}", { example: "fabricproxy-lite" })}
                      className="field mt-1 text-xs"
                    />
                  </div>
                  <div>
                    <label htmlFor="pack-voice-config" className="label text-muted">
                      {t("Konfigurationsdatei von Simple Voice Chat")}
                    </label>
                    <input
                      id="pack-voice-config"
                      type="text"
                      value={voiceConfigFile}
                      onChange={(e) => setVoiceConfigFile(e.target.value)}
                      placeholder={t("z. B. {example}", { example: "plugins/voicechat/voicechat-server.properties" })}
                      className="field mt-1 text-xs"
                    />
                  </div>
                </div>
              </div>
            </div>
          )}

          {activeTab === "autoconfig" && (
            <div className="space-y-3">
              <div className="rounded-lg border border-info/20 bg-info/5 p-3 text-xs text-info">
                {t("In Werten und Pfaden kannst du diese Platzhalter benutzen:")}
                <code className="ml-1 text-ink">{"{{SECRET}}"}</code>,
                <code className="ml-1 text-ink">{"{{VOICE_HOST}}"}</code>,
                <code className="ml-1 text-ink">{"{{VOICE_PORT}}"}</code>,
                <code className="ml-1 text-ink">{"{{PROXY_PORT}}"}</code>,
                <code className="ml-1 text-ink">{"{{SERVER_NAME}}"}</code>,
                <code className="ml-1 text-ink">{"{{SERVER_SLUG}}"}</code>.
              </div>
              <div>
                <label htmlFor="pack-autoconfig-json" className="label">
                  autoconfig.json
                </label>
                <textarea
                  id="pack-autoconfig-json"
                  rows={14}
                  value={autoconfigJson}
                  onChange={(e) => setAutoconfigJson(e.target.value)}
                  className="field mt-1 font-mono text-xs"
                  placeholder="{}"
                />
              </div>
            </div>
          )}

          {activeTab === "script" && (
            <div className="space-y-3">
              <div className="rounded-lg border border-white/[0.08] bg-white/[0.02] p-3 text-xs text-muted">
                {t("Dieses Skript ({file}) liegt im ZIP-Paket getrennt von den Metadaten und wird beim Einrichten auf den Server geschrieben.", { file: "autoconfig.sh" })}
              </div>
              <div>
                <label htmlFor="pack-script-content" className="label">
                  {t("autoconfig.sh (Shell-Skript)")}
                </label>
                <textarea
                  id="pack-script-content"
                  rows={14}
                  value={scriptContent}
                  onChange={(e) => setScriptContent(e.target.value)}
                  className="field mt-1 font-mono text-xs"
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
