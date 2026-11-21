"use client";

import { Download, FileArchive, Flame, Globe, Info, Moon, Package, RotateCcw, Trash2, Upload } from "lucide-react";
import { useEffect, useRef, useState } from "react";
import { Badge, Button, Card, CardHeader, Checkbox, EmptyState, Meter, RowMenu } from "@/components/ui/primitives";
import { ConfirmDialog, Dialog, useToast } from "@/components/ui/feedback";
import { ErrorState, Loading } from "@/components/ui/async";
import { api, downloadUrl, upload, useApi } from "@/lib/api";
import { ago, bytes } from "@/lib/format";
import type { Datapack, Server, World, WorldKind, WorldList } from "@/lib/types";
import { t, tp } from "@/lib/i18n";

const kindLabel = (): Record<WorldKind, string> => ({ overworld: t("Oberwelt"), nether: "Nether", end: "End", other: t("Weitere Welt") });
const kindIcon: Record<WorldKind, typeof Globe> = { overworld: Globe, nether: Flame, end: Moon, other: Globe };
const worldNamePattern = /^[A-Za-z0-9_][A-Za-z0-9 _.+-]{0,63}$/;
const path = (serverId: string, world: string, rest = "") => `/servers/${serverId}/worlds/${encodeURIComponent(world)}${rest}`;

/**
 * Welten eines Servers: Größe, Herunterladen, Zurücksetzen, Ersetzen per Zip und Datapacks. Ändern geht nur, wenn der
 * Server aus ist; das Backend prüft es noch einmal.
 */
export function WorldsTab({ server, canManage, canOperate }: { server: Server; canManage: boolean; canOperate: boolean }) {
  const state = server.view?.state;
  const offline = state === "offline";
  const { data, error, mutate } = useApi<WorldList>(`/servers/${server.id}/worlds`);
  const [reset, setReset] = useState<World | null>(null);
  const [uploading, setUploading] = useState<{ name: string } | null>(null);
  const [packs, setPacks] = useState<World | null>(null);

  // Nach dem Zurücksetzen oder Ersetzen (Zustand „restoring“) die Liste neu laden
  useEffect(() => {
    if (state === "offline") void mutate();
  }, [state, mutate]);

  if (error) return <ErrorState error={error} retry={() => mutate()} />;
  if (!data) return <Loading />;

  const names = new Set(data.worlds.map((world) => world.name));
  const busyHint = state === "restoring" ? t("Gerade läuft eine Weltänderung oder Wiederherstellung") : t("Der Server muss aus sein");

  return (
    <div className="space-y-5">
      {!offline && state !== "restoring" && (
        <div className="flex gap-3 rounded-lg border border-warn/25 bg-warn/10 px-4 py-3 text-xs text-warn">
          <Info className="mt-0.5 h-4 w-4 shrink-0" />
          <p>{t("Der Server läuft. Zum Zurücksetzen oder Ersetzen einer Welt muss er aus sein. Herunterladen geht auch so – für eine stimmige Kopie führe vorher in der Konsole save-all aus.")}</p>
        </div>
      )}
      {state === "restoring" && (
        <p className="rounded-lg border border-info/25 bg-info/10 px-4 py-3 text-xs text-info">{t("Die Welt wird gerade geändert. Den Fortschritt siehst du in der Konsole.")}</p>
      )}

      <Card>
        <CardHeader
          title={t("Welten")}
          description={t("Ordner mit level.dat im Serverordner. Der Server lädt „{levelName}“ (level-name). Seed: {value} – er wirkt nur auf neu erzeugte Welten.", { levelName: data.levelName, value: data.seed || t("zufällig") })}
          action={
            canManage && (
              <Button size="sm" variant="primary" disabled={!offline} title={offline ? undefined : busyHint} onClick={() => setUploading({ name: names.has(data.levelName) ? "" : data.levelName })}>
                <Upload className="h-3.5 w-3.5" />
                {t("Welt hochladen")}
              </Button>
            )
          }
        />
        {data.worlds.length === 0 ? (
          <EmptyState icon={<Globe className="h-6 w-6" />} title={t("Noch keine Welt")} text={canManage ? t("Die Welt entsteht beim ersten Start des Servers. Du kannst auch eine vorhandene Welt als Zip hochladen.") : t("Die Welt entsteht beim ersten Start des Servers.")} />
        ) : (
          <div className="divide-y divide-white/[0.05]">
            {data.worlds.map((world) => {
              const Icon = kindIcon[world.kind];
              return (
                <div key={world.name} className="flex flex-wrap items-center gap-x-4 gap-y-2 px-5 py-3">
                  <Icon className="h-4 w-4 shrink-0 text-muted" />
                  <div className="min-w-[180px] flex-1">
                    <p className="flex items-center gap-2 text-sm font-medium">
                      <span className="truncate font-mono">{world.name}</span>
                      {world.main && <Badge tone="accent">{t("Hauptwelt")}</Badge>}
                      {world.kind !== "overworld" && world.kind !== "other" && <Badge>{kindLabel()[world.kind]}</Badge>}
                    </p>
                    <p className="text-xs text-muted tabular-nums">
                      {bytes(world.size)} {t("· geändert")} {ago(world.modified)} · {tp("datapacks", world.datapacks)}
                    </p>
                  </div>
                  <div className="flex items-center gap-1">
                    {canOperate && (
                      <Button size="sm" variant="ghost" onClick={() => setPacks(world)}>
                        <Package className="h-3.5 w-3.5" />
                        Datapacks
                      </Button>
                    )}
                    {canOperate && (
                      <Button variant="ghost" size="icon-sm" aria-label={`${world.name} herunterladen`} title={t("Als Zip herunterladen")} onClick={() => window.open(downloadUrl(path(server.id, world.name, "/download")))}>
                        <Download className="h-3.5 w-3.5" />
                      </Button>
                    )}
                    {canManage && (
                      <RowMenu
                        items={[
                          { label: t("Durch Zip ersetzen"), icon: <Upload className="h-3.5 w-3.5" />, disabled: !offline, hint: offline ? undefined : busyHint, onClick: () => setUploading({ name: world.name }) },
                          "separator",
                          { label: t("Zurücksetzen"), icon: <RotateCcw className="h-3.5 w-3.5" />, danger: true, disabled: !offline, hint: offline ? undefined : busyHint, onClick: () => setReset(world) },
                        ]}
                      />
                    )}
                  </div>
                </div>
              );
            })}
          </div>
        )}
      </Card>

      <ResetDialog serverId={server.id} world={reset} list={data} onClose={() => setReset(null)} onDone={() => mutate()} />
      <UploadDialog serverId={server.id} target={uploading} existing={names} onClose={() => setUploading(null)} onDone={() => mutate()} />
      <DatapacksDialog serverId={server.id} world={packs} canWrite={canOperate} onClose={() => setPacks(null)} onChanged={() => mutate()} />
    </div>
  );
}

// ---------------------------------------------------------------- Zurücksetzen

function ResetDialog({ serverId, world, list, onClose, onDone }: { serverId: string; world: World | null; list: WorldList; onClose: () => void; onDone: () => void }) {
  const toast = useToast();
  const [backup, setBackup] = useState(true);
  const [dimensions, setDimensions] = useState(true);
  const [changeSeed, setChangeSeed] = useState(false);
  const [seed, setSeed] = useState("");
  const [confirming, setConfirming] = useState(false);

  // Beim Öffnen für eine Welt mit den sicheren Standards beginnen
  useEffect(() => {
    if (!world) return;
    setBackup(true);
    setDimensions(true);
    setChangeSeed(false);
    setSeed("");
  }, [world]);

  const extras = world?.main ? list.worlds.filter((entry) => entry.kind === "nether" || entry.kind === "end") : [];
  const lost = [world?.name, ...(dimensions ? extras.map((entry) => entry.name) : [])].filter(Boolean).map((name) => `„${name}“`).join(", ");

  return (
    <>
      <Dialog
        open={!!world && !confirming}
        onClose={onClose}
        title={world ? t("„{name}“ zurücksetzen", { name: world.name }) : t("Welt zurücksetzen")}
        description={t("Die Welt wird gelöscht und beim nächsten Start neu erzeugt.")}
        footer={
          <>
            <Button variant="ghost" onClick={onClose}>
              {t("Abbrechen")}
            </Button>
            <Button variant="danger" disabled={changeSeed && seed.length > 128} onClick={() => setConfirming(true)}>
              <RotateCcw className="h-3.5 w-3.5" />
              {t("Weiter")}
            </Button>
          </>
        }
      >
        <OptionRow checked={backup} onChange={setBackup} title={t("Vorher ein Backup erstellen")} text={t("Sichert den ganzen Serverordner. Du findest es danach unter Backups und kannst es wiederherstellen.")} />
        {extras.length > 0 && <OptionRow checked={dimensions} onChange={setDimensions} title={t("Nether und End mit zurücksetzen")} text={t("Löscht auch {value}, damit alle Dimensionen zum neuen Seed passen.", { value: extras.map((entry) => entry.name).join(t(" und ")) })} />}
        {world?.main && (
          <>
            <OptionRow checked={changeSeed} onChange={setChangeSeed} title={t("Neuen Seed setzen")} text={t("Aktuell: {value}. Der Seed bestimmt, wie die neue Welt aussieht.", { value: list.seed || t("zufällig") })} />
            {changeSeed && (
              <div>
                <label className="label" htmlFor="world-seed">
                  Seed
                </label>
                <input id="world-seed" className="field font-mono" value={seed} maxLength={128} onChange={(event) => setSeed(event.target.value)} placeholder={t("leer = zufällig")} />
              </div>
            )}
          </>
        )}
      </Dialog>
      <ConfirmDialog
        open={!!world && confirming}
        onClose={() => {
          setConfirming(false);
          onClose();
        }}
        title={world ? t("„{name}“ wirklich zurücksetzen?", { name: world.name }) : t("Welt zurücksetzen?")}
        text={tp("worldsDeleted", lost.includes(",") ? 2 : 1, { lost, backup: backup ? t("Vorher entsteht ein Backup.") : t("Es wird kein Backup erstellt, das lässt sich nicht rückgängig machen.") })}
        confirm={t("Zurücksetzen")}
        danger
        onConfirm={async () => {
          if (!world) return;
          const body: { backup: boolean; dimensions: boolean; seed?: string } = { backup, dimensions: dimensions && extras.length > 0 };
          if (world.main && changeSeed) body.seed = seed.trim();
          await toast.run(() => api.post(path(serverId, world.name, "/reset"), body), t("Welt wird zurückgesetzt"));
          onDone();
        }}
      />
    </>
  );
}

function OptionRow({ checked, onChange, title, text }: { checked: boolean; onChange: (value: boolean) => void; title: string; text: string }) {
  return (
    <div className="flex cursor-pointer items-start gap-3 rounded-lg border border-white/[0.07] bg-white/[0.02] px-3 py-2.5" onClick={() => onChange(!checked)}>
      <span className="mt-0.5">
        <Checkbox checked={checked} onChange={onChange} label={title} />
      </span>
      <div>
        <p className="text-sm">{title}</p>
        <p className="mt-0.5 text-xs text-muted">{text}</p>
      </div>
    </div>
  );
}

// ---------------------------------------------------------------- Hochladen

function UploadDialog({ serverId, target, existing, onClose, onDone }: { serverId: string; target: { name: string } | null; existing: Set<string>; onClose: () => void; onDone: () => void }) {
  const toast = useToast();
  const [name, setName] = useState("");
  const [file, setFile] = useState<File | null>(null);
  const [backup, setBackup] = useState(true);
  const [progress, setProgress] = useState<number | null>(null);
  const [confirming, setConfirming] = useState(false);

  useEffect(() => {
    if (!target) return;
    setName(target.name);
    setFile(null);
    setBackup(true);
    setProgress(null);
  }, [target]);

  const replaces = existing.has(name);
  const validName = worldNamePattern.test(name);

  async function send() {
    if (!file) return;
    setProgress(0);
    const ok = await toast.run(() => upload(path(serverId, name, `/upload${replaces && backup ? "?backup=1" : ""}`), [file], setProgress), replaces ? t("Welt ersetzt") : t("Welt hochgeladen"));
    setProgress(null);
    if (ok) {
      onClose();
      onDone();
    }
  }

  return (
    <>
      <Dialog
        open={!!target && !confirming}
        onClose={() => progress === null && onClose()}
        title={t("Welt hochladen")}
        description={t("Ein Zip mit dem Weltordner. level.dat liegt darin ganz oben oder in genau einem Ordner.")}
        footer={
          <>
            <Button variant="ghost" disabled={progress !== null} onClick={onClose}>
              {t("Abbrechen")}
            </Button>
            <Button variant={replaces ? "danger" : "primary"} loading={progress !== null} disabled={!file || !validName} onClick={() => (replaces ? setConfirming(true) : send())}>
              <Upload className="h-3.5 w-3.5" />
              {replaces ? t("Ersetzen") : t("Hochladen")}
            </Button>
          </>
        }
      >
        <div>
          <label className="label" htmlFor="world-name">
            {t("Name der Welt (Ordner)")}
          </label>
          <input id="world-name" className="field font-mono" value={name} maxLength={64} onChange={(event) => setName(event.target.value)} placeholder="world" disabled={progress !== null} />
          <p className="mt-1.5 text-xs text-muted">
            {name && !validName ? t("Nur Buchstaben, Ziffern, Leerzeichen und _ . + - (ohne Schrägstrich).") : replaces ? t("Diese Welt gibt es schon – sie wird ersetzt.") : t("Eine neue Welt. Damit der Server sie lädt, trage den Namen unter Spieleinstellungen als Weltordner ein.")}
          </p>
        </div>
        <label className="flex cursor-pointer items-center gap-3 rounded-lg border border-dashed border-white/[0.15] px-3 py-3 text-sm hover:bg-white/[0.03]">
          <FileArchive className="h-5 w-5 shrink-0 text-muted" />
          <span className="min-w-0 flex-1 truncate">{file ? `${file.name} (${bytes(file.size)})` : t("Zip-Datei auswählen")}</span>
          <input type="file" accept=".zip" className="hidden" disabled={progress !== null} onChange={(event) => setFile(event.target.files?.[0] ?? null)} />
        </label>
        {replaces && <OptionRow checked={backup} onChange={setBackup} title={t("Vorher ein Backup erstellen")} text={t("Sichert den ganzen Serverordner, bevor die alte Welt weicht.")} />}
        {progress !== null && (
          <div className="space-y-1.5">
            <Meter value={progress * 100} max={100} />
            <p className="text-xs text-muted">{progress < 1 ? t("Wird hochgeladen … {round} %", { round: Math.round(progress * 100) }) : t("Wird entpackt und geprüft …")}</p>
          </div>
        )}
      </Dialog>
      <ConfirmDialog
        open={!!target && confirming}
        onClose={() => setConfirming(false)}
        title={t("„{name}“ ersetzen?", { name })}
        text={t("Die vorhandene Welt „{name}“ wird durch das Zip ersetzt und ist danach weg. {value}", { name, value: backup ? t("Vorher entsteht ein Backup.") : t("Es wird kein Backup erstellt.") })}
        confirm={t("Ersetzen")}
        danger
        onConfirm={() => {
          // Zurück in den Upload-Dialog, dort ist der Fortschritt zu sehen
          setConfirming(false);
          void send();
        }}
      />
    </>
  );
}

// ---------------------------------------------------------------- Datapacks

function DatapacksDialog({ serverId, world, canWrite, onClose, onChanged }: { serverId: string; world: World | null; canWrite: boolean; onClose: () => void; onChanged: () => void }) {
  const toast = useToast();
  const { data, error, mutate } = useApi<Datapack[]>(world ? path(serverId, world.name, "/datapacks") : null);
  const [remove, setRemove] = useState<Datapack | null>(null);
  const [progress, setProgress] = useState<number | null>(null);
  const picker = useRef<HTMLInputElement>(null);

  async function send(files: File[]) {
    if (!world) return;
    setProgress(0);
    for (const file of files) {
      const ok = await toast.run(() => upload(path(serverId, world.name, "/datapacks"), [file], setProgress), t("{name} hinzugefügt", { name: file.name }));
      if (!ok) break;
    }
    setProgress(null);
    mutate();
    onChanged();
  }

  return (
    <>
      <Dialog
        open={!!world}
        onClose={onClose}
        wide
        title={world ? t("Datapacks von „{name}“", { name: world.name }) : "Datapacks"}
        description={t("Zip-Dateien mit pack.mcmeta im Ordner datapacks der Welt. Neue Datapacks gelten nach /reload oder einem Neustart.")}
        footer={
          <>
            <Button variant="ghost" onClick={onClose}>
              {t("Schließen")}
            </Button>
            {canWrite && (
              <Button variant="primary" loading={progress !== null} onClick={() => picker.current?.click()}>
                <Upload className="h-3.5 w-3.5" />
                {t("Datapack hinzufügen")}
              </Button>
            )}
          </>
        }
      >
        <input
          ref={picker}
          type="file"
          accept=".zip"
          multiple
          className="hidden"
          onChange={(event) => {
            const files = Array.from(event.target.files ?? []);
            event.target.value = "";
            if (files.length > 0) void send(files);
          }}
        />
        {progress !== null && <Meter value={progress * 100} max={100} />}
        {error ? (
          <ErrorState error={error} retry={() => mutate()} />
        ) : !data ? (
          <Loading />
        ) : data.length === 0 ? (
          <EmptyState icon={<Package className="h-6 w-6" />} title={t("Noch keine Datapacks")} text={canWrite ? t("Füge eine Zip-Datei mit pack.mcmeta hinzu, z. B. von Modrinth oder Planet Minecraft.") : undefined} />
        ) : (
          <div className="divide-y divide-white/[0.05] rounded-lg border border-white/[0.07]">
            {data.map((pack) => (
              <div key={pack.name} className="flex items-center gap-3 px-3 py-2.5">
                <Package className="h-4 w-4 shrink-0 text-muted" />
                <div className="min-w-0 flex-1">
                  <p className="truncate font-mono text-sm">{pack.name}</p>
                  <p className="text-xs text-muted">{pack.dir ? t("Ordner") : bytes(pack.size)} · {ago(pack.modified)}</p>
                </div>
                {canWrite && (
                  <Button variant="ghost" size="icon-sm" aria-label={t("{name} löschen", { name: pack.name })} onClick={() => setRemove(pack)}>
                    <Trash2 className="h-3.5 w-3.5" />
                  </Button>
                )}
              </div>
            ))}
          </div>
        )}
      </Dialog>
      <ConfirmDialog
        open={!!remove}
        onClose={() => setRemove(null)}
        title={remove ? t("„{name}“ löschen?", { name: remove.name }) : t("Datapack löschen?")}
        text={t("Das Datapack wird aus der Welt gelöscht. Was es in der Welt erzeugt hat (Bauten, Gegenstände), bleibt; seine Rezepte und Funktionen fehlen nach /reload oder einem Neustart.")}
        confirm={t("Löschen")}
        danger
        onConfirm={async () => {
          if (!remove || !world) return;
          await toast.run(() => api.delete(path(serverId, world.name, `/datapacks/${encodeURIComponent(remove.name)}`)), t("Datapack gelöscht"));
          mutate();
          onChanged();
        }}
      />
    </>
  );
}
