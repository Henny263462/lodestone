"use client";

import { Fingerprint, Link2, Pencil, Plus, Trash2, Unlink } from "lucide-react";
import { useEffect, useState } from "react";
import { usePasskeySupport } from "@/components/sign-in";
import { ErrorState, Loading } from "@/components/ui/async";
import { ConfirmDialog, Dialog, useToast } from "@/components/ui/feedback";
import { Badge, Button, Card, CardHeader, EmptyState, RowMenu } from "@/components/ui/primitives";
import { api, useApi } from "@/lib/api";
import { ago, dateTime } from "@/lib/format";
import { signInErrorText } from "@/lib/sign-in";
import type { AuthStatus, OidcAccount, Passkey, PasskeyCeremony, PasskeyCreationOptions } from "@/lib/types";
import { createPasskey, passkeyErrorText } from "@/lib/webauthn";
import { t } from "@/lib/i18n";

// ---------------------------------------------------------------- Bestätigung vor einem neuen Anmeldeweg

/**
 * Ein neuer Anmeldeweg (Passkey, Verknüpfung) braucht eine frische Bestätigung: das aktuelle Passwort oder, bei Konten
 * ohne Passwort, eine Anmeldung vor höchstens 10 Minuten. Das Backend prüft dasselbe.
 */
function confirmBlocked(account: OidcAccount | undefined): string | undefined {
  if (!account || account.hasPassword || account.firstPasswordUntil > Date.now()) return undefined;
  return t("Bestätige, dass du es bist: Melde dich ab und neu an. Danach kannst du 10 Minuten lang einen Anmeldeweg hinzufügen.");
}

function ConfirmIdentity({ account, password, onChange }: { account: OidcAccount | undefined; password: string; onChange: (value: string) => void }) {
  const blocked = confirmBlocked(account);
  if (blocked) return <p className="rounded-lg border border-warn/25 bg-warn/10 px-4 py-3 text-xs text-warn">{blocked}</p>;
  if (!account?.hasPassword) return null;
  return (
    <div>
      <label className="label">{t("Aktuelles Passwort")}</label>
      <input className="field" type="password" autoComplete="current-password" value={password} onChange={(event) => onChange(event.target.value)} />
      <p className="mt-1.5 text-xs text-muted">{t("Zur Bestätigung, dass du es bist. So reicht eine fremde Sitzung nicht, um sich einen eigenen Zugang anzulegen.")}</p>
    </div>
  );
}

function confirmReady(account: OidcAccount | undefined, password: string): boolean {
  return !!account && !confirmBlocked(account) && (!account.hasPassword || password !== "");
}

// ---------------------------------------------------------------- Passkeys

export function PasskeysCard() {
  const toast = useToast();
  const supported = usePasskeySupport();
  const { data: status } = useApi<AuthStatus>("/auth/status");
  const { data, error, mutate } = useApi<Passkey[]>("/me/passkeys");
  const { data: account } = useApi<OidcAccount>("/me/oidc");
  const [adding, setAdding] = useState(false);
  const [name, setName] = useState("");
  const [password, setPassword] = useState("");
  const [busy, setBusy] = useState(false);
  const [renaming, setRenaming] = useState<Passkey | null>(null);
  const [newName, setNewName] = useState("");
  const [removing, setRemoving] = useState<Passkey | null>(null);
  const enabled = status?.passkeys === true;
  const blocked = !enabled ? t("Passkeys sind auf diesem Panel nicht eingerichtet.") : !supported ? t("Dieser Browser kann hier keine Passkeys anlegen. Öffne das Panel über HTTPS.") : undefined;

  async function add() {
    setBusy(true);
    try {
      const start = await api.post<PasskeyCeremony<PasskeyCreationOptions>>("/me/passkeys/begin", { password });
      const credential = await createPasskey(start.options);
      await api.post("/me/passkeys/finish", { flow: start.flow, name: name.trim(), credential });
      toast.success(t("Passkey hinzugefügt"));
      setAdding(false);
      setName("");
      setPassword("");
      mutate();
    } catch (failure) {
      toast.error(passkeyErrorText(failure));
    } finally {
      setBusy(false);
    }
  }

  return (
    <Card>
      <CardHeader
        title="Passkeys"
        description={t("Anmeldung ohne Passwort per Fingerabdruck, Gesicht oder Sicherheitsschlüssel. Nach dem Passwort dient er als zweiter Schritt.")}
        action={
          <Button size="sm" disabled={!!blocked} title={blocked} onClick={() => setAdding(true)}>
            <Plus className="h-3.5 w-3.5" />
            {t("Passkey hinzufügen")}
          </Button>
        }
      />
      {status && !enabled && (
        <div className="px-5 pt-4">
          <p className="rounded-lg border border-info/25 bg-info/10 px-4 py-3 text-xs text-info">
            {t("Passkeys sind auf diesem Panel noch nicht eingerichtet. Ein Administrator muss LODESTONE_PUBLIC_URL auf die Adresse setzen, unter der du das Panel öffnest.")}
          </p>
        </div>
      )}
      {error ? (
        <ErrorState inline error={error} retry={() => mutate()} />
      ) : !data ? (
        <Loading />
      ) : data.length === 0 ? (
        <EmptyState icon={<Fingerprint className="h-6 w-6" />} title={t("Noch kein Passkey")} text={t("Lege einen Passkey an, um dich schneller und ohne Passwort anzumelden.")} />
      ) : (
        <div className="divide-y divide-white/[0.05]">
          {data.map((key) => (
            <div key={key.id} className="flex items-center gap-3 px-5 py-2.5">
              <Fingerprint className="h-4 w-4 shrink-0 text-muted" />
              <div className="min-w-0 flex-1">
                <p className="truncate text-sm font-medium">{key.name}</p>
                <p className="text-xs text-muted">
                  {t("angelegt")} {dateTime(key.createdAt)} · {key.lastUsed ? t("zuletzt {time}", { time: ago(key.lastUsed) }) : t("noch nie benutzt")}
                </p>
              </div>
              <RowMenu
                items={[
                  {
                    label: t("Umbenennen"),
                    icon: <Pencil className="h-3.5 w-3.5" />,
                    onClick: () => {
                      setRenaming(key);
                      setNewName(key.name);
                    },
                  },
                  "separator",
                  { label: t("Entfernen"), icon: <Trash2 className="h-3.5 w-3.5" />, danger: true, onClick: () => setRemoving(key) },
                ]}
              />
            </div>
          ))}
        </div>
      )}

      <Dialog
        open={adding}
        onClose={() => setAdding(false)}
        title={t("Passkey hinzufügen")}
        description={t("Gib dem Passkey einen Namen, an dem du ihn wiedererkennst. Danach fragt dein Browser nach dem Gerät oder Schlüssel.")}
        footer={
          <>
            <Button variant="ghost" onClick={() => setAdding(false)}>
              {t("Abbrechen")}
            </Button>
            <Button variant="primary" loading={busy} disabled={!name.trim() || !confirmReady(account, password)} onClick={add}>
              {t("Weiter")}
            </Button>
          </>
        }
      >
        <div className="space-y-4">
          <div>
            <label className="label">{t("Name")}</label>
            <input className="field" value={name} onChange={(event) => setName(event.target.value)} placeholder="Laptop" maxLength={48} autoFocus />
          </div>
          <ConfirmIdentity account={account} password={password} onChange={setPassword} />
        </div>
      </Dialog>

      <Dialog
        open={!!renaming}
        onClose={() => setRenaming(null)}
        title={t("Passkey umbenennen")}
        footer={
          <>
            <Button variant="ghost" onClick={() => setRenaming(null)}>
              {t("Abbrechen")}
            </Button>
            <Button
              variant="primary"
              disabled={!newName.trim()}
              onClick={async () => {
                if (!renaming) return;
                if (await toast.run(() => api.patch(`/me/passkeys/${renaming.id}`, { name: newName.trim() }), t("Umbenannt"))) {
                  setRenaming(null);
                  mutate();
                }
              }}
            >
              {t("Speichern")}
            </Button>
          </>
        }
      >
        <div>
          <label className="label">{t("Name")}</label>
          <input className="field" value={newName} onChange={(event) => setNewName(event.target.value)} maxLength={48} autoFocus />
        </div>
      </Dialog>

      <ConfirmDialog
        open={!!removing}
        onClose={() => setRemoving(null)}
        title={t("„{name}“ entfernen?", { name: removing?.name })}
        text={t("Mit diesem Passkey kannst du dich danach nicht mehr anmelden. Auf dem Gerät bleibt er gespeichert, bis du ihn dort löschst.")}
        confirm={t("Entfernen")}
        danger
        onConfirm={async () => {
          if (!removing) return;
          await toast.run(() => api.delete(`/me/passkeys/${removing.id}`), t("Passkey entfernt"));
          mutate();
        }}
      />
    </Card>
  );
}

// ---------------------------------------------------------------- Anmeldung über einen Anbieter (OIDC)

export function OidcCard() {
  const toast = useToast();
  const { data, error, mutate } = useApi<OidcAccount>("/me/oidc");
  const [busy, setBusy] = useState(false);
  const [linking, setLinking] = useState(false);
  const [password, setPassword] = useState("");
  const [unlinking, setUnlinking] = useState(false);

  // Rückmeldung nach dem Rücksprung vom Anbieter (/account/?oidc=linked oder ein Fehlercode)
  useEffect(() => {
    const result = new URLSearchParams(window.location.search).get("oidc");
    if (!result) return;
    if (result === "linked") toast.success(t("Konto verknüpft"));
    else toast.error(signInErrorText(result));
    window.history.replaceState(null, "", window.location.pathname);
  }, [toast]);

  if (data && !data.configured) return null;
  const name = data?.name ?? t("Anbieter");
  return (
    <Card>
      <CardHeader
        title={t("Anmeldung über {name}", { name })}
        description={t("Verknüpfe dein Konto bei {name}, dann meldest du dich mit einem Klick an.", { name })}
        action={data?.link ? <Badge tone="good">{t("Verknüpft")}</Badge> : data ? <Badge>{t("Nicht verknüpft")}</Badge> : undefined}
      />
      {error ? (
        <ErrorState inline error={error} retry={() => mutate()} />
      ) : !data ? (
        <Loading />
      ) : (
        <div className="space-y-4 px-5 py-4 text-sm">
          {!data.passwordLogin && <p className="text-xs text-muted">{t("Die Anmeldung mit Passwort ist auf diesem Panel abgeschaltet.")}</p>}
          {data.link ? (
            <>
              <p className="text-xs text-muted">
                {data.link.email || t("Konto beim Anbieter")} {t("· verknüpft am")} {dateTime(data.link.createdAt)}
                {data.link.lastLogin > 0 && t(" · zuletzt angemeldet {lastLogin}", { lastLogin: ago(data.link.lastLogin) })}
              </p>
              <Button variant="danger" onClick={() => setUnlinking(true)}>
                <Unlink className="h-4 w-4" />
                {t("Verknüpfung lösen")}
              </Button>
            </>
          ) : (
            <Button onClick={() => setLinking(true)}>
              <Link2 className="h-4 w-4" />
              {t("Mit {name} verknüpfen", { name })}
            </Button>
          )}
        </div>
      )}

      <Dialog
        open={linking}
        onClose={() => setLinking(false)}
        title={t("Mit {name} verknüpfen", { name })}
        description={t("Danach meldest du dich auch über {name} an. Dein Browser wechselt dafür kurz zu {name}.", { name })}
        footer={
          <>
            <Button variant="ghost" onClick={() => setLinking(false)}>
              {t("Abbrechen")}
            </Button>
            <Button
              variant="primary"
              loading={busy}
              disabled={!confirmReady(data, password)}
              onClick={async () => {
                setBusy(true);
                try {
                  const answer = await api.post<{ url: string }>("/me/oidc/link", { password });
                  window.location.assign(answer.url);
                } catch (failure) {
                  toast.error(failure instanceof Error ? failure.message : t("Die Verknüpfung konnte nicht beginnen."));
                  setBusy(false);
                }
              }}
            >
              {t("Weiter")}
            </Button>
          </>
        }
      >
        <ConfirmIdentity account={data} password={password} onChange={setPassword} />
      </Dialog>

      <ConfirmDialog
        open={unlinking}
        onClose={() => setUnlinking(false)}
        title={t("Verknüpfung mit {name} lösen?", { name })}
        text={t("Danach kannst du dich nicht mehr über {name} anmelden, bis du dein Konto wieder verknüpfst.", { name })}
        confirm={t("Lösen")}
        danger
        onConfirm={async () => {
          await toast.run(() => api.delete("/me/oidc"), t("Verknüpfung gelöst"));
          mutate();
        }}
      />
    </Card>
  );
}
