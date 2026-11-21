"use client";

import { KeyRound, Plus, Trash2 } from "lucide-react";
import { useState } from "react";
import { Badge, Button, Card, CardHeader, EmptyState, Switch } from "@/components/ui/primitives";
import { ConfirmDialog, Dialog, useToast } from "@/components/ui/feedback";
import { ErrorState, Loading } from "@/components/ui/async";
import { api, useApi } from "@/lib/api";
import { ago } from "@/lib/format";
import type { SftpAccount, SftpKey } from "@/lib/types";
import { t } from "@/lib/i18n";

// Karte „SFTP-Zugang“ im Konto: eigenes SFTP-Passwort, Passwort-Login an/aus und SSH-Schlüssel.
export function SftpAccountCard() {
  const toast = useToast();
  const { data, error, mutate } = useApi<SftpAccount>("/me/sftp");
  const [next, setNext] = useState("");
  const [repeat, setRepeat] = useState("");
  const [busy, setBusy] = useState(false);
  const [clearing, setClearing] = useState(false);
  const [adding, setAdding] = useState(false);
  const [name, setName] = useState("");
  const [publicKey, setPublicKey] = useState("");
  const [removing, setRemoving] = useState<SftpKey | null>(null);
  const mismatch = repeat !== "" && next !== repeat;

  return (
    <Card>
      <CardHeader
        title={t("SFTP-Zugang")}
        description={t("Für FileZilla, WinSCP & Co. auf allen Servern, für die du das Recht „SFTP-Zugang“ hast. Host und Port stehen beim Server unter Dateien.")}
        action={data ? data.passwordSet && data.passwordLogin ? <Badge tone="good">{t("Passwort aktiv")}</Badge> : data.keys.length > 0 ? <Badge tone="good">{t("Nur Schlüssel")}</Badge> : <Badge>{t("Nicht eingerichtet")}</Badge> : undefined}
      />
      {error ? (
        <ErrorState inline error={error} retry={() => mutate()} />
      ) : !data ? (
        <Loading />
      ) : (
        <>
          <form
            className="space-y-4 px-5 py-4"
            onSubmit={async (event) => {
              event.preventDefault();
              setBusy(true);
              const ok = await toast.run(() => api.put("/me/sftp/password", { password: next }), data.passwordSet ? t("SFTP-Passwort geändert") : t("SFTP-Passwort gesetzt"));
              setBusy(false);
              if (ok) {
                setNext("");
                setRepeat("");
                mutate();
              }
            }}
          >
            <p className="text-xs text-muted">
              {data.passwordSet ? t("Ein SFTP-Passwort ist gesetzt. Du kannst es hier ändern.") : t("Noch kein SFTP-Passwort. Nimm ein anderes als dein Panel-Passwort.")}
            </p>
            <div className="grid gap-3 sm:grid-cols-2">
              <div>
                <label className="label">{t("Neues SFTP-Passwort (mindestens 8 Zeichen)")}</label>
                <input className="field" type="password" autoComplete="new-password" value={next} onChange={(event) => setNext(event.target.value)} minLength={8} />
              </div>
              <div>
                <label className="label">{t("Wiederholen")}</label>
                <input className="field" type="password" autoComplete="new-password" value={repeat} onChange={(event) => setRepeat(event.target.value)} />
                {mismatch && <p className="mt-1 text-xs text-bad">{t("Die Passwörter stimmen nicht überein.")}</p>}
              </div>
            </div>
            <div className="flex flex-wrap justify-end gap-2">
              {data.passwordSet && (
                <Button type="button" variant="danger" onClick={() => setClearing(true)}>
                  {t("Passwort entfernen")}
                </Button>
              )}
              <Button type="submit" variant="primary" loading={busy} disabled={next.length < 8 || next !== repeat}>
                {data.passwordSet ? t("Passwort ändern") : t("Passwort setzen")}
              </Button>
            </div>
          </form>

          <div className="border-t border-white/[0.05] px-5 py-4">
            <div className="flex items-center justify-between gap-4 rounded-lg border border-white/[0.07] bg-white/[0.02] px-3 py-2.5">
              <div>
                <p className="text-sm">{t("Anmeldung mit Passwort erlauben")}</p>
                <p className="text-xs text-muted">{t("Aus: Es gelten nur noch deine SSH-Schlüssel. Das ist sicherer. Wirkt sofort.")}</p>
              </div>
              <Switch
                checked={data.passwordLogin}
                onChange={async (value) => {
                  await toast.run(() => api.put("/me/sftp/password-login", { enabled: value }), value ? t("Passwort-Anmeldung an") : t("Passwort-Anmeldung aus"));
                  mutate();
                }}
              />
            </div>
          </div>

          <div className="flex items-center justify-between gap-4 border-t border-white/[0.05] px-5 py-3">
            <p className="text-[13px] font-semibold tracking-tight">{t("SSH-Schlüssel")}</p>
            <Button size="sm" onClick={() => setAdding(true)}>
              <Plus className="h-3.5 w-3.5" />
              {t("Schlüssel hinzufügen")}
            </Button>
          </div>
          {data.keys.length === 0 ? (
            <EmptyState icon={<KeyRound className="h-6 w-6" />} title={t("Noch keine SSH-Schlüssel")} text={t("Mit einem Schlüssel meldest du dich ohne Passwort an. Füge den Inhalt deiner .pub-Datei hinzu.")} />
          ) : (
            <div className="divide-y divide-white/[0.05] border-t border-white/[0.05]">
              {data.keys.map((key) => (
                <div key={key.id} className="flex flex-wrap items-center gap-x-4 gap-y-1 px-5 py-3">
                  <KeyRound className="h-4 w-4 text-muted" />
                  <div className="min-w-0 flex-1">
                    <p className="truncate text-sm font-medium">{key.name}</p>
                    <p className="truncate text-xs text-muted">
                      <span className="font-mono">{key.fingerprint}</span> · {key.lastUsed ? t("zuletzt {time}", { time: ago(key.lastUsed) }) : t("noch nie benutzt")}
                    </p>
                  </div>
                  <Button variant="ghost" size="icon-sm" onClick={() => setRemoving(key)} aria-label={t("Schlüssel löschen")}>
                    <Trash2 className="h-3.5 w-3.5" />
                  </Button>
                </div>
              ))}
            </div>
          )}
        </>
      )}

      <Dialog
        open={adding}
        onClose={() => setAdding(false)}
        title={t("SSH-Schlüssel hinzufügen")}
        description={t("Nur den öffentlichen Teil (.pub). Den privaten Schlüssel gibst du nie weiter.")}
        footer={
          <>
            <Button variant="ghost" onClick={() => setAdding(false)}>
              {t("Abbrechen")}
            </Button>
            <Button
              variant="primary"
              disabled={!publicKey.trim()}
              onClick={async () => {
                if (await toast.run(() => api.post("/me/sftp/keys", { name: name.trim(), publicKey: publicKey.trim() }), t("Schlüssel hinzugefügt"))) {
                  setAdding(false);
                  setName("");
                  setPublicKey("");
                  mutate();
                }
              }}
            >
              {t("Hinzufügen")}
            </Button>
          </>
        }
      >
        <div>
          <label className="label">{t("Name (optional)")}</label>
          <input className="field" value={name} onChange={(event) => setName(event.target.value)} placeholder="Laptop" maxLength={48} />
        </div>
        <div>
          <label className="label">{t("Öffentlicher Schlüssel")}</label>
          <textarea
            className="field min-h-[96px] font-mono text-xs"
            value={publicKey}
            onChange={(event) => setPublicKey(event.target.value)}
            placeholder="ssh-ed25519 AAAAC3Nza… name@host"
            spellCheck={false}
          />
          <p className="mt-1 text-xs text-muted">{t("Inhalt von ~/.ssh/id_ed25519.pub. Noch keinen Schlüssel? Erzeuge einen mit: ssh-keygen -t ed25519")}</p>
        </div>
      </Dialog>

      <ConfirmDialog
        open={clearing}
        onClose={() => setClearing(false)}
        title={t("SFTP-Passwort entfernen?")}
        text={t("Danach kannst du dich per SFTP nur noch mit einem SSH-Schlüssel anmelden. Hast du keinen, endet dein SFTP-Zugang sofort.")}
        confirm={t("Entfernen")}
        danger
        onConfirm={async () => {
          await toast.run(() => api.delete("/me/sftp/password"), t("SFTP-Passwort entfernt"));
          mutate();
        }}
      />

      <ConfirmDialog
        open={!!removing}
        onClose={() => setRemoving(null)}
        title={t("„{name}“ löschen?", { name: removing?.name })}
        text={t("Mit diesem Schlüssel kann sich danach niemand mehr per SFTP anmelden.")}
        confirm={t("Löschen")}
        danger
        onConfirm={async () => {
          if (!removing) return;
          await toast.run(() => api.delete(`/me/sftp/keys/${removing.id}`), t("Schlüssel gelöscht"));
          mutate();
        }}
      />
    </Card>
  );
}
