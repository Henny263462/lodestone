"use client";

import { Copy, RadioTower } from "lucide-react";
import { useEffect, useState } from "react";
import { Badge, Button, Dot, Tabs } from "@/components/ui/primitives";
import { Dialog, useToast } from "@/components/ui/feedback";
import { api, apiBase } from "@/lib/api";
import { ago, dateTime } from "@/lib/format";
import { composeFile, defaultDataDir, dockerRunCommand, insecurePanelUrl, validDataDir, validPanelUrl } from "@/lib/node-link";
import type { JoinToken, NodeInfo, NodeLink } from "@/lib/types";
import { t } from "@/lib/i18n";

/** „Node hinzufügen“: legt einen Node an, der sich selbst zum Panel verbindet, und zeigt danach den Startbefehl. */
export function AddNodeDialog({ open, onClose, onDirect, onDone }: { open: boolean; onClose: () => void; onDirect: () => void; onDone: () => void }) {
  const toast = useToast();
  const [form, setForm] = useState({ name: "", publicHost: "" });
  const [busy, setBusy] = useState(false);
  const [issued, setIssued] = useState<JoinToken | null>(null);

  const set = (key: keyof typeof form) => (event: React.ChangeEvent<HTMLInputElement>) => setForm((current) => ({ ...current, [key]: event.target.value }));

  return (
    <>
      <Dialog
        open={open}
        onClose={onClose}
        title={t("Node hinzufügen")}
        description={t("Der Node verbindet sich selbst zum Panel. Er braucht keine offenen Ports und funktioniert auch hinter NAT.")}
        footer={
          <>
            <Button variant="ghost" onClick={onClose}>
              {t("Abbrechen")}
            </Button>
            <Button
              variant="primary"
              loading={busy}
              disabled={!form.name.trim()}
              onClick={async () => {
                setBusy(true);
                try {
                  const answer = await api.post<JoinToken>("/nodes/link", { name: form.name.trim(), publicHost: form.publicHost.trim() });
                  setIssued(answer);
                  setForm({ name: "", publicHost: "" });
                  onClose();
                  onDone();
                } catch (error) {
                  toast.error(error instanceof Error ? error.message : t("Der Node konnte nicht angelegt werden."));
                } finally {
                  setBusy(false);
                }
              }}
            >
              {t("Join-Token erzeugen")}
            </Button>
          </>
        }
      >
        <div>
          <label className="label">{t("Name")}</label>
          <input className="field" value={form.name} onChange={set("name")} placeholder="hetzner-1" maxLength={48} autoFocus />
        </div>
        <div>
          <label className="label">{t("Öffentliche Adresse (für Spieler)")}</label>
          <input className="field" value={form.publicHost} onChange={set("publicHost")} placeholder="mc2.example.org" maxLength={253} />
          <p className="mt-1 text-xs text-muted">{t("Wird für die Beitritts-Adresse und die Voicechat-Konfiguration verwendet.")}</p>
        </div>
        <p className="text-xs text-muted">
          {t("Der Node ist schon über eine Adresse erreichbar (z. B. im selben Netz)?")}{" "}
          <button type="button" className="text-accent hover:underline" onClick={onDirect}>
            {t("Adresse und Token eintragen")}
          </button>
        </p>
      </Dialog>
      <JoinCommandDialog issued={issued} onClose={() => setIssued(null)} />
    </>
  );
}

/** Zeigt einmalig das Join-Token als fertigen Startbefehl (docker run oder Docker Compose). */
function JoinCommandDialog({ issued, onClose, renewal }: { issued: JoinToken | null; onClose: () => void; renewal?: boolean }) {
  const toast = useToast();
  const [panelUrl, setPanelUrl] = useState("");
  const [dataDir, setDataDir] = useState(defaultDataDir);
  const [format, setFormat] = useState<"run" | "compose">("run");

  // Die Adresse des Panels kennt nur der Browser; sie steht erst nach dem Laden der Seite fest
  useEffect(() => {
    if (issued) setPanelUrl((current) => current || apiBase() || window.location.origin);
  }, [issued]);

  const urlOk = validPanelUrl(panelUrl);
  const dirOk = validDataDir(dataDir);
  const input = { panelUrl, token: issued?.token ?? "", publicHost: issued?.node.publicHost ?? "", dataDir };
  const command = format === "run" ? dockerRunCommand(input) : composeFile(input);

  return (
    <Dialog
      open={!!issued}
      onClose={onClose}
      wide
      title={t("„{value}“ starten", { value: issued?.node.name ?? "" })}
      description={t("Führe den Befehl auf der Maschine des Nodes aus. Das Join-Token gilt bis {expiresAt} und nur einmal; danach hat der Node ein eigenes Geheimnis. Es erscheint nur jetzt.", { expiresAt: dateTime(issued?.expiresAt) })}
      footer={
        <>
          <Button disabled={!urlOk || !dirOk} onClick={() => navigator.clipboard.writeText(command).then(() => toast.success(t("Kopiert")))}>
            <Copy className="h-3.5 w-3.5" />
            {t("Kopieren")}
          </Button>
          <Button variant="primary" onClick={onClose}>
            {t("Fertig")}
          </Button>
        </>
      }
    >
      {renewal && <p className="rounded-lg border border-info/25 bg-info/10 px-4 py-3 text-xs text-info">{t("Die bestehende Verbindung bleibt, bis sich ein Node mit diesem Token koppelt. Ab dann gilt das alte Geheimnis nicht mehr.")}</p>}
      <div className="grid gap-3 sm:grid-cols-2">
        <div>
          <label className="label">{t("Adresse des Panels (so erreicht der Node es)")}</label>
          <input className="field font-mono text-xs" value={panelUrl} onChange={(event) => setPanelUrl(event.target.value)} placeholder="https://panel.example.org" />
          {!urlOk && panelUrl && <p className="mt-1 text-xs text-bad">{t("Gib eine Adresse wie https://panel.example.org an.")}</p>}
        </div>
        <div>
          <label className="label">{t("Datenordner auf der Maschine")}</label>
          <input className="field font-mono text-xs" value={dataDir} onChange={(event) => setDataDir(event.target.value)} placeholder={defaultDataDir} />
          {!dirOk && <p className="mt-1 text-xs text-bad">{t("Gib einen absoluten Pfad wie /srv/lodestone an.")}</p>}
        </div>
      </div>
      {urlOk && insecurePanelUrl(panelUrl) && (
        <p className="rounded-lg border border-warn/25 bg-warn/10 px-4 py-3 text-xs text-warn">
          {t("Unverschlüsselt: Mit http:// gehen Token und Geheimnis des Nodes im Klartext über das Netz. Nutze https:// (Reverse-Proxy mit TLS vor dem Panel); http:// nur im eigenen Netz.")}
        </p>
      )}
      <Tabs
        value={format}
        onChange={setFormat}
        items={[
          { id: "run", label: "docker run" },
          { id: "compose", label: "Docker Compose" },
        ]}
      />
      <pre key={format} className="animate-fade-in max-h-72 overflow-auto rounded-md bg-black/30 p-3 font-mono text-[11px] leading-relaxed text-ink/80">
        {command}
      </pre>
      <p className="text-xs text-muted">
        {format === "run"
          ? t("Der Datenordner wird mit demselben Pfad eingebunden, weil Docker die Serverordner über den Pfad des Hosts einbindet.")
          : t("Speichere das als docker-compose.yml und trage bei group_add die Gruppe des Docker-Sockets ein (stat -c %g /var/run/docker.sock). Dann: docker compose up -d")}
      </p>
    </Dialog>
  );
}

/** Verbindungszustand eines Nodes, der sich selbst zum Panel verbindet, mit „Neu koppeln“. Nur für Administratoren. */
export function NodeLinkCard({ node, link, error, retry, onChanged }: { node: NodeInfo; link?: NodeLink; error?: Error; retry: () => void; onChanged: () => void }) {
  const toast = useToast();
  const [busy, setBusy] = useState(false);
  const [issued, setIssued] = useState<JoinToken | null>(null);

  let tone: "good" | "warn" | "bad" | "neutral" = "neutral";
  let label = t("Wird abgefragt");
  let text = "";
  if (link?.connected) {
    tone = "good";
    label = t("Verbunden");
    text = t("Verbunden {connectedAt} von {remote}{version}.", { connectedAt: ago(link.connectedAt), remote: link.remote ?? t("unbekannt"), version: link.version ? t(" · Version {version}", { version: link.version }) : "" });
  } else if (link && !link.joinedAt) {
    tone = "warn";
    label = t("Wartet auf Kopplung");
    text = link.joinExpiresAt ? t("Starte den Node mit dem Befehl aus „Node hinzufügen“. Das Join-Token gilt bis {joinExpiresAt}.", { joinExpiresAt: dateTime(link.joinExpiresAt) }) : t("Das Join-Token ist abgelaufen. Erzeuge mit „Neu koppeln“ ein neues.");
  } else if (link) {
    tone = "bad";
    label = t("Getrennt");
    text = t("Der Node ist gerade nicht verbunden. Er verbindet sich von selbst neu, sobald er läuft und das Panel erreicht.");
    if (link.joinExpiresAt) text += t(" Ein neues Join-Token ist offen bis {joinExpiresAt}.", { joinExpiresAt: dateTime(link.joinExpiresAt) });
  }

  return (
    <div className="mt-4 border-t border-white/[0.06] pt-3">
      <div className="flex items-center justify-between gap-2">
        <span className="flex items-center gap-2 text-xs">
          <RadioTower className="h-3.5 w-3.5 text-muted" />
          {t("Verbindung zum Panel")}
          {!error && (
            <Badge tone={tone}>
              <Dot tone={tone} />
              {label}
            </Badge>
          )}
        </span>
        <Button
          size="sm"
          variant="ghost"
          loading={busy}
          onClick={async () => {
            setBusy(true);
            try {
              setIssued(await api.post<JoinToken>(`/nodes/${node.id}/join-token`));
              onChanged();
            } catch (failure) {
              toast.error(failure instanceof Error ? failure.message : t("Das Join-Token konnte nicht erzeugt werden."));
            } finally {
              setBusy(false);
            }
          }}
        >
          {t("Neu koppeln")}
        </Button>
      </div>
      {error ? (
        <p className="mt-2 text-xs text-bad">
          {error.message}{" "}
          <button type="button" className="text-accent hover:underline" onClick={retry}>
            {t("Erneut versuchen")}
          </button>
        </p>
      ) : (
        text && <p className="mt-2 text-xs text-muted">{text}</p>
      )}
      {link?.connected && link.insecure && <p className="mt-2 text-xs text-warn">{t("Die Verbindung ist unverschlüsselt (ws://). Stelle das Panel hinter HTTPS und nutze https:// als Adresse des Panels.")}</p>}
      <JoinCommandDialog issued={issued} onClose={() => setIssued(null)} renewal />
    </div>
  );
}
