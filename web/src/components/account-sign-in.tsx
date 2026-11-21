"use client";

import { Fingerprint, Link2, Pencil, Plus, RefreshCw, Trash2, Unlink } from "lucide-react";
import { useEffect, useState } from "react";
import { usePasskeySupport } from "@/components/sign-in";
import { Loading } from "@/components/ui/async";
import { ConfirmDialog, Dialog, useToast } from "@/components/ui/feedback";
import { Badge, Button, Card, CardHeader, EmptyState, RowMenu } from "@/components/ui/primitives";
import { api, useApi, type ApiError } from "@/lib/api";
import { ago, dateTime } from "@/lib/format";
import { signInErrorText } from "@/lib/sign-in";
import type { AuthStatus, OidcAccount, Passkey, PasskeyCeremony, PasskeyCreationOptions } from "@/lib/types";
import { createPasskey, passkeyErrorText } from "@/lib/webauthn";

/** Fehlerkasten innerhalb einer Karte (ErrorState ist selbst eine Karte). */
function CardError({ error, retry }: { error: ApiError; retry: () => void }) {
  return (
    <div className="px-5 py-4">
      <div className="flex flex-wrap items-center gap-3 rounded-lg border border-bad/25 bg-bad/10 px-4 py-3 text-xs text-bad">
        <span className="min-w-0 flex-1">{error.message}</span>
        <Button size="sm" onClick={retry}>
          <RefreshCw className="h-3.5 w-3.5" />
          Erneut versuchen
        </Button>
      </div>
    </div>
  );
}

// ---------------------------------------------------------------- Passkeys

export function PasskeysCard() {
  const toast = useToast();
  const supported = usePasskeySupport();
  const { data: status } = useApi<AuthStatus>("/auth/status");
  const { data, error, mutate } = useApi<Passkey[]>("/me/passkeys");
  const [adding, setAdding] = useState(false);
  const [name, setName] = useState("");
  const [busy, setBusy] = useState(false);
  const [renaming, setRenaming] = useState<Passkey | null>(null);
  const [newName, setNewName] = useState("");
  const [removing, setRemoving] = useState<Passkey | null>(null);
  const enabled = status?.passkeys === true;
  const blocked = !enabled ? "Passkeys sind auf diesem Panel nicht eingerichtet." : !supported ? "Dieser Browser kann hier keine Passkeys anlegen. Öffne das Panel über HTTPS." : undefined;

  async function add() {
    setBusy(true);
    try {
      const start = await api.post<PasskeyCeremony<PasskeyCreationOptions>>("/me/passkeys/begin");
      const credential = await createPasskey(start.options);
      await api.post("/me/passkeys/finish", { flow: start.flow, name: name.trim(), credential });
      toast.success("Passkey hinzugefügt");
      setAdding(false);
      setName("");
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
        description="Melde dich ohne Passwort mit Fingerabdruck, Gesichtserkennung oder Sicherheitsschlüssel an. Hast du einen Passkey, verlangt die Anmeldung mit Passwort ihn (oder den Code aus der App) als zweiten Schritt."
        action={
          <Button size="sm" disabled={!!blocked} title={blocked} onClick={() => setAdding(true)}>
            <Plus className="h-3.5 w-3.5" />
            Passkey hinzufügen
          </Button>
        }
      />
      {status && !enabled && (
        <div className="px-5 pt-4">
          <p className="rounded-lg border border-info/25 bg-info/10 px-4 py-3 text-xs text-info">
            Passkeys sind auf diesem Panel noch nicht eingerichtet. Ein Administrator muss LODESTONE_PUBLIC_URL auf die Adresse setzen, unter der du das Panel öffnest.
          </p>
        </div>
      )}
      {error ? (
        <CardError error={error} retry={() => mutate()} />
      ) : !data ? (
        <Loading />
      ) : data.length === 0 ? (
        <EmptyState icon={<Fingerprint className="h-6 w-6" />} title="Noch kein Passkey" text="Lege einen Passkey an, um dich schneller und ohne Passwort anzumelden." />
      ) : (
        <div className="divide-y divide-white/[0.05]">
          {data.map((key) => (
            <div key={key.id} className="flex items-center gap-3 px-5 py-2.5">
              <Fingerprint className="h-4 w-4 shrink-0 text-muted" />
              <div className="min-w-0 flex-1">
                <p className="truncate text-sm font-medium">{key.name}</p>
                <p className="text-xs text-muted">
                  angelegt {dateTime(key.createdAt)} · {key.lastUsed ? `zuletzt ${ago(key.lastUsed)}` : "noch nie benutzt"}
                </p>
              </div>
              <RowMenu
                items={[
                  {
                    label: "Umbenennen",
                    icon: <Pencil className="h-3.5 w-3.5" />,
                    onClick: () => {
                      setRenaming(key);
                      setNewName(key.name);
                    },
                  },
                  "separator",
                  { label: "Entfernen", icon: <Trash2 className="h-3.5 w-3.5" />, danger: true, onClick: () => setRemoving(key) },
                ]}
              />
            </div>
          ))}
        </div>
      )}

      <Dialog
        open={adding}
        onClose={() => setAdding(false)}
        title="Passkey hinzufügen"
        description="Gib dem Passkey einen Namen, an dem du ihn wiedererkennst. Danach fragt dein Browser nach dem Gerät oder Schlüssel."
        footer={
          <>
            <Button variant="ghost" onClick={() => setAdding(false)}>
              Abbrechen
            </Button>
            <Button variant="primary" loading={busy} disabled={!name.trim()} onClick={add}>
              Weiter
            </Button>
          </>
        }
      >
        <div>
          <label className="label">Name</label>
          <input className="field" value={name} onChange={(event) => setName(event.target.value)} placeholder="Laptop" maxLength={48} autoFocus />
        </div>
      </Dialog>

      <Dialog
        open={!!renaming}
        onClose={() => setRenaming(null)}
        title="Passkey umbenennen"
        footer={
          <>
            <Button variant="ghost" onClick={() => setRenaming(null)}>
              Abbrechen
            </Button>
            <Button
              variant="primary"
              disabled={!newName.trim()}
              onClick={async () => {
                if (!renaming) return;
                if (await toast.run(() => api.patch(`/me/passkeys/${renaming.id}`, { name: newName.trim() }), "Umbenannt")) {
                  setRenaming(null);
                  mutate();
                }
              }}
            >
              Speichern
            </Button>
          </>
        }
      >
        <div>
          <label className="label">Name</label>
          <input className="field" value={newName} onChange={(event) => setNewName(event.target.value)} maxLength={48} autoFocus />
        </div>
      </Dialog>

      <ConfirmDialog
        open={!!removing}
        onClose={() => setRemoving(null)}
        title={`„${removing?.name}“ entfernen?`}
        text="Mit diesem Passkey kannst du dich danach nicht mehr anmelden. Auf dem Gerät bleibt er gespeichert, bis du ihn dort löschst."
        confirm="Entfernen"
        danger
        onConfirm={async () => {
          if (!removing) return;
          await toast.run(() => api.delete(`/me/passkeys/${removing.id}`), "Passkey entfernt");
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
  const [unlinking, setUnlinking] = useState(false);

  // Rückmeldung nach dem Rücksprung vom Anbieter (/account/?oidc=linked oder ein Fehlercode)
  useEffect(() => {
    const result = new URLSearchParams(window.location.search).get("oidc");
    if (!result) return;
    if (result === "linked") toast.success("Konto verknüpft");
    else toast.error(signInErrorText(result));
    window.history.replaceState(null, "", window.location.pathname);
  }, [toast]);

  if (data && !data.configured) return null;
  const name = data?.name ?? "Anbieter";
  return (
    <Card>
      <CardHeader
        title={`Anmeldung über ${name}`}
        description={`Verknüpfe dein Konto bei ${name}, dann meldest du dich mit einem Klick an.`}
        action={data?.link ? <Badge tone="good">Verknüpft</Badge> : data ? <Badge>Nicht verknüpft</Badge> : undefined}
      />
      {error ? (
        <CardError error={error} retry={() => mutate()} />
      ) : !data ? (
        <Loading />
      ) : (
        <div className="space-y-4 px-5 py-4 text-sm">
          {!data.passwordLogin && <p className="text-xs text-muted">Die Anmeldung mit Passwort ist auf diesem Panel abgeschaltet.</p>}
          {data.link ? (
            <>
              <p className="text-xs text-muted">
                {data.link.email || "Konto beim Anbieter"} · verknüpft am {dateTime(data.link.createdAt)}
                {data.link.lastLogin > 0 && ` · zuletzt angemeldet ${ago(data.link.lastLogin)}`}
              </p>
              <Button variant="danger" onClick={() => setUnlinking(true)}>
                <Unlink className="h-4 w-4" />
                Verknüpfung lösen
              </Button>
            </>
          ) : (
            <Button
              loading={busy}
              onClick={async () => {
                setBusy(true);
                try {
                  const answer = await api.post<{ url: string }>("/me/oidc/link");
                  window.location.assign(answer.url);
                } catch (failure) {
                  toast.error(failure instanceof Error ? failure.message : "Die Verknüpfung konnte nicht beginnen.");
                  setBusy(false);
                }
              }}
            >
              {!busy && <Link2 className="h-4 w-4" />}
              Mit {name} verknüpfen
            </Button>
          )}
        </div>
      )}

      <ConfirmDialog
        open={unlinking}
        onClose={() => setUnlinking(false)}
        title={`Verknüpfung mit ${name} lösen?`}
        text={`Danach kannst du dich nicht mehr über ${name} anmelden, bis du dein Konto wieder verknüpfst.`}
        confirm="Lösen"
        danger
        onConfirm={async () => {
          await toast.run(() => api.delete("/me/oidc"), "Verknüpfung gelöst");
          mutate();
        }}
      />
    </Card>
  );
}
