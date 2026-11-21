"use client";

import QRCode from "qrcode";
import { Copy, Fingerprint, HardDriveUpload, KeyRound, Link2, LockKeyhole, LogOut, MonitorSmartphone, Plus, ShieldCheck, Trash2 } from "lucide-react";
import { Suspense, useEffect, useState } from "react";
import { Badge, Button, Card, CardHeader, EmptyState, PageHeader, SectionLayout, type Section } from "@/components/ui/primitives";
import { ConfirmDialog, Dialog, useToast } from "@/components/ui/feedback";
import { ErrorState, Loading } from "@/components/ui/async";
import { useUser } from "@/components/layout/shell";
import { OidcCard, PasskeysCard } from "@/components/account-sign-in";
import { PixelAvatar } from "@/components/ui/mc";
import { SftpAccountCard } from "@/components/sftp-account";
import { api, useApi } from "@/lib/api";
import { ago, dateTime } from "@/lib/format";
import { useUrlState } from "@/lib/url-state";
import type { ApiKeyInfo, Me, OidcAccount, SessionInfo } from "@/lib/types";

type SectionId = "password" | "totp" | "passkeys" | "oidc" | "keys" | "sftp" | "sessions";

const sections: Section<SectionId>[] = [
  { id: "password", label: "Passwort", icon: LockKeyhole },
  { id: "totp", label: "Zwei-Faktor", icon: ShieldCheck },
  { id: "passkeys", label: "Passkeys", icon: Fingerprint },
  { id: "oidc", label: "Anmeldung über OIDC", icon: Link2 },
  { id: "keys", label: "API-Schlüssel", icon: KeyRound },
  { id: "sftp", label: "SFTP-Zugang", icon: HardDriveUpload },
  { id: "sessions", label: "Sitzungen", icon: MonitorSmartphone },
];

export default function AccountPage() {
  return (
    <Suspense fallback={<Loading />}>
      <AccountView />
    </Suspense>
  );
}

function AccountView() {
  const user = useUser();
  const [section, setSection] = useUrlState<SectionId>("section", "password");
  const active = sections.some((entry) => entry.id === section) ? section : "password";
  return (
    <>
      <PageHeader title="Konto" description="Anmeldung, Sicherheit und Zugriff für Skripte." />
      <div className="max-w-5xl space-y-5">
        <Card className="flex items-center gap-4 p-5">
          <PixelAvatar name={user.username} size={48} />
          <div className="min-w-0">
            <p className="truncate text-sm font-semibold">{user.username}</p>
            {user.isAdmin ? <Badge tone="accent">Administrator</Badge> : <p className="text-xs text-muted">Benutzer</p>}
          </div>
        </Card>
        <SectionLayout label="Abschnitte des Kontos" sections={sections} value={active} onChange={setSection}>
          {active === "password" && <PasswordCard />}
          {active === "totp" && <TwoFactorCard />}
          {active === "passkeys" && <PasskeysCard />}
          {active === "oidc" && <OidcCard />}
          {active === "keys" && <KeysCard />}
          {active === "sftp" && <SftpAccountCard />}
          {active === "sessions" && <SessionsCard />}
        </SectionLayout>
      </div>
    </>
  );
}

// Konten aus OIDC haben anfangs kein Passwort. Ein erstes legt man nur kurz nach einer Anmeldung über OIDC oder einen
// Passkey fest (das Backend prüft es); sonst steht hier, wie es geht.
function PasswordCard() {
  const toast = useToast();
  const { data: account, error, mutate } = useApi<OidcAccount>("/me/oidc");
  const [current, setCurrent] = useState("");
  const [next, setNext] = useState("");
  const [repeat, setRepeat] = useState("");
  const [busy, setBusy] = useState(false);
  const mismatch = repeat !== "" && next !== repeat;
  const first = account ? !account.hasPassword : false;

  if (error)
    return (
      <Card>
        <CardHeader title="Passwort" />
        <ErrorState inline error={error} retry={() => mutate()} />
      </Card>
    );
  if (!account)
    return (
      <Card>
        <CardHeader title="Passwort" />
        <Loading />
      </Card>
    );
  const provider = account.name ?? "deinen Anbieter";
  if (first && !account.passwordLogin)
    return (
      <Card>
        <CardHeader title="Passwort" description="Dein Konto hat kein Passwort. Du meldest dich über OIDC oder einen Passkey an." />
        <p className="px-5 py-4 text-xs text-muted">Die Anmeldung mit Passwort ist auf diesem Panel abgeschaltet. Ein Passwort brauchst du deshalb nicht.</p>
      </Card>
    );
  if (first && account.firstPasswordUntil <= Date.now())
    return (
      <Card>
        <CardHeader title="Passwort festlegen" description="Dein Konto hat noch kein Passwort. Du meldest dich über OIDC oder einen Passkey an." />
        <p className="px-5 py-4 text-xs text-muted">
          Ein erstes Passwort kannst du in den ersten 10 Minuten nach einer Anmeldung festlegen. Melde dich ab und neu über {provider} oder einen Passkey an, dann kommst du hierher zurück.
        </p>
      </Card>
    );

  return (
    <Card>
      <CardHeader
        title={first ? "Passwort festlegen" : "Passwort ändern"}
        description={
          first
            ? `Dein Konto hat noch kein Passwort. Mit einem Passwort kommst du auch ohne ${provider} herein. Möglich bis ${dateTime(account.firstPasswordUntil)}.`
            : "Danach musst du dich auf anderen Geräten neu anmelden."
        }
      />
      <form
        className="space-y-4 px-5 py-4"
        onSubmit={async (event) => {
          event.preventDefault();
          setBusy(true);
          const ok = await toast.run(() => api.post("/me/password", { current: first ? "" : current, next }), first ? "Passwort festgelegt" : "Passwort geändert");
          setBusy(false);
          if (ok) {
            setCurrent("");
            setNext("");
            setRepeat("");
            mutate();
          }
        }}
      >
        {!first && (
          <div>
            <label className="label">Aktuelles Passwort</label>
            <input className="field" type="password" autoComplete="current-password" value={current} onChange={(event) => setCurrent(event.target.value)} required />
          </div>
        )}
        <div>
          <label className="label">Neues Passwort (mindestens 8 Zeichen)</label>
          <input className="field" type="password" autoComplete="new-password" value={next} onChange={(event) => setNext(event.target.value)} required minLength={8} />
        </div>
        <div>
          <label className="label">Neues Passwort wiederholen</label>
          <input className="field" type="password" autoComplete="new-password" value={repeat} onChange={(event) => setRepeat(event.target.value)} required />
          {mismatch && <p className="mt-1 text-xs text-bad">Die Passwörter stimmen nicht überein.</p>}
        </div>
        <Button type="submit" variant="primary" loading={busy} disabled={(!first && !current) || next.length < 8 || next !== repeat}>
          {first ? "Passwort festlegen" : "Passwort ändern"}
        </Button>
      </form>
    </Card>
  );
}

// ---------------------------------------------------------------- Zwei-Faktor

function TwoFactorCard() {
  const toast = useToast();
  const { data: me, error, mutate } = useApi<Me>("/me");
  const [setup, setSetup] = useState<{ secret: string; uri: string } | null>(null);
  const [qr, setQr] = useState("");
  const [code, setCode] = useState("");
  const [codes, setCodes] = useState<string[] | null>(null);
  const [disabling, setDisabling] = useState(false);
  const [password, setPassword] = useState("");
  const [otp, setOtp] = useState("");

  useEffect(() => {
    if (setup) QRCode.toDataURL(setup.uri, { margin: 1, width: 192 }).then(setQr).catch(() => setQr(""));
    else setQr("");
  }, [setup]);

  const header = (action?: React.ReactNode) => (
    <CardHeader title="Zwei-Faktor-Anmeldung" description="Bei der Anmeldung braucht es zusätzlich einen Code aus einer App wie Aegis oder 1Password." action={action} />
  );
  if (error)
    return (
      <Card>
        {header()}
        <ErrorState inline error={error} retry={() => mutate()} />
      </Card>
    );
  if (!me)
    return (
      <Card>
        {header()}
        <Loading />
      </Card>
    );
  return (
    <Card>
      {header(me.totpEnabled ? <Badge tone="good">Eingeschaltet</Badge> : <Badge>Aus</Badge>)}
      <div className="space-y-4 px-5 py-4 text-sm">
        {me.totpEnabled ? (
          <>
            <p className="text-xs text-muted">Noch {me.recoveryCodes} Wiederherstellungscodes übrig. Jeder gilt einmal, falls du die App nicht hast.</p>
            <Button variant="danger" onClick={() => setDisabling(true)}>
              Ausschalten
            </Button>
          </>
        ) : setup ? (
          <div className="flex flex-wrap items-start gap-5">
            {/* eslint-disable-next-line @next/next/no-img-element */}
            {qr && <img src={qr} alt="QR-Code" width={160} height={160} className="rounded-lg bg-white p-1" />}
            <div className="min-w-[220px] flex-1 space-y-3">
              <p className="text-xs text-muted">Scanne den Code mit deiner App oder gib diesen Schlüssel ein:</p>
              <code className="block break-all rounded-md bg-black/30 px-3 py-2 text-xs">{setup.secret}</code>
              <div>
                <label className="label">Code aus der App zur Bestätigung</label>
                <input className="field w-40 font-mono tracking-widest" inputMode="numeric" value={code} onChange={(event) => setCode(event.target.value)} placeholder="000000" />
              </div>
              <div className="flex gap-2">
                <Button variant="ghost" onClick={() => setSetup(null)}>
                  Abbrechen
                </Button>
                <Button
                  variant="primary"
                  disabled={code.trim().length < 6}
                  onClick={async () => {
                    const ok = await toast.run(async () => setCodes((await api.post<{ recoveryCodes: string[] }>("/me/totp/enable", { code })).recoveryCodes), "Zwei-Faktor-Anmeldung ist an");
                    if (ok) {
                      setSetup(null);
                      setCode("");
                      mutate();
                    }
                  }}
                >
                  Einschalten
                </Button>
              </div>
            </div>
          </div>
        ) : (
          <Button
            onClick={() => toast.run(async () => setSetup(await api.post<{ secret: string; uri: string }>("/me/totp/setup")))}
          >
            <ShieldCheck className="h-4 w-4" />
            Einrichten
          </Button>
        )}
      </div>

      <Dialog
        open={!!codes}
        onClose={() => setCodes(null)}
        title="Deine Wiederherstellungscodes"
        description="Speichere sie jetzt an einem sicheren Ort. Sie werden nicht noch einmal angezeigt."
        footer={
          <>
            <Button onClick={() => navigator.clipboard.writeText((codes ?? []).join("\n")).then(() => toast.success("Kopiert"))}>
              <Copy className="h-3.5 w-3.5" />
              Kopieren
            </Button>
            <Button variant="primary" onClick={() => setCodes(null)}>
              Gespeichert
            </Button>
          </>
        }
      >
        <div className="grid grid-cols-2 gap-2 font-mono text-sm">
          {(codes ?? []).map((entry) => (
            <code key={entry} className="rounded bg-black/30 px-3 py-1.5 text-center">
              {entry}
            </code>
          ))}
        </div>
      </Dialog>

      <Dialog
        open={disabling}
        onClose={() => setDisabling(false)}
        title="Zwei-Faktor-Anmeldung ausschalten"
        footer={
          <>
            <Button variant="ghost" onClick={() => setDisabling(false)}>
              Abbrechen
            </Button>
            <Button
              variant="danger"
              disabled={!password || !otp}
              onClick={async () => {
                if (await toast.run(() => api.post("/me/totp/disable", { password, code: otp }), "Zwei-Faktor-Anmeldung ist aus")) {
                  setDisabling(false);
                  setPassword("");
                  setOtp("");
                  mutate();
                }
              }}
            >
              Ausschalten
            </Button>
          </>
        }
      >
        <div>
          <label className="label">Passwort</label>
          <input className="field" type="password" value={password} onChange={(event) => setPassword(event.target.value)} autoComplete="current-password" />
        </div>
        <div>
          <label className="label">Code aus der App oder Wiederherstellungscode</label>
          <input className="field font-mono" value={otp} onChange={(event) => setOtp(event.target.value)} autoComplete="off" />
        </div>
      </Dialog>
    </Card>
  );
}

// ---------------------------------------------------------------- API-Schlüssel

function KeysCard() {
  const toast = useToast();
  const { data, error, mutate } = useApi<ApiKeyInfo[]>("/me/keys");
  const [creating, setCreating] = useState(false);
  const [busy, setBusy] = useState(false);
  const [name, setName] = useState("");
  const [days, setDays] = useState("0");
  const [created, setCreated] = useState<string | null>(null);
  const [removing, setRemoving] = useState<ApiKeyInfo | null>(null);

  return (
    <Card>
      <CardHeader
        title="API-Schlüssel"
        description="Für Skripte: Ein Schlüssel hat deine Rechte, kommt aber nicht an deine Zugangsdaten."
        action={
          data &&
          data.length > 0 && (
            <Button size="sm" onClick={() => setCreating(true)}>
              <Plus className="h-3.5 w-3.5" />
              Neuer Schlüssel
            </Button>
          )
        }
      />
      {error ? (
        <ErrorState inline error={error} retry={() => mutate()} />
      ) : !data ? (
        <Loading />
      ) : data.length === 0 ? (
        <div className="pb-8">
          <EmptyState icon={<KeyRound className="h-6 w-6" />} title="Noch kein API-Schlüssel" text="Lege einen an, wenn ein Skript das Panel bedienen soll. Er gehört in den Header Authorization: Bearer lds_…" />
          <div className="flex justify-center">
            <Button variant="primary" onClick={() => setCreating(true)}>
              <Plus className="h-4 w-4" />
              Schlüssel anlegen
            </Button>
          </div>
        </div>
      ) : (
        <div className="divide-y divide-white/[0.05]">
          {data.map((key) => (
            <div key={key.id} className="flex flex-wrap items-center gap-x-4 gap-y-1 px-5 py-3">
              <KeyRound className="h-4 w-4 text-muted" />
              <div className="min-w-[160px] flex-1">
                <p className="text-sm font-medium">{key.name}</p>
                <p className="text-xs text-muted">
                  <span className="font-mono">{key.prefix}…</span> · {key.lastUsed ? `zuletzt ${ago(key.lastUsed)}` : "noch nie benutzt"}
                  {key.expiresAt > 0 && ` · läuft ${dateTime(key.expiresAt)} ab`}
                </p>
              </div>
              <Button variant="ghost" size="icon-sm" onClick={() => setRemoving(key)} aria-label={`${key.name} widerrufen`} title="Widerrufen">
                <Trash2 className="h-3.5 w-3.5" />
              </Button>
            </div>
          ))}
        </div>
      )}

      <Dialog
        open={creating}
        onClose={() => setCreating(false)}
        title="Neuer API-Schlüssel"
        footer={
          <>
            <Button variant="ghost" onClick={() => setCreating(false)}>
              Abbrechen
            </Button>
            <Button
              variant="primary"
              loading={busy}
              disabled={!name.trim()}
              onClick={async () => {
                setBusy(true);
                const ok = await toast.run(async () => setCreated((await api.post<{ key: string }>("/me/keys", { name, expiresDays: Number(days) || 0 })).key));
                setBusy(false);
                if (ok) {
                  setCreating(false);
                  setName("");
                  mutate();
                }
              }}
            >
              Anlegen
            </Button>
          </>
        }
      >
        <div>
          <label className="label">Name</label>
          <input className="field" value={name} onChange={(event) => setName(event.target.value)} placeholder="Backup-Skript" maxLength={48} autoFocus />
        </div>
        <div>
          <label className="label">Gültig für (Tage, 0 = unbegrenzt)</label>
          <input className="field w-32" inputMode="numeric" value={days} onChange={(event) => setDays(event.target.value)} />
        </div>
      </Dialog>

      <Dialog
        open={!!created}
        onClose={() => setCreated(null)}
        title="Dein neuer Schlüssel"
        description="Kopiere ihn jetzt. Aus Sicherheitsgründen siehst du ihn danach nicht mehr."
        footer={
          <>
            <Button onClick={() => navigator.clipboard.writeText(created ?? "").then(() => toast.success("Kopiert"))}>
              <Copy className="h-3.5 w-3.5" />
              Kopieren
            </Button>
            <Button variant="primary" onClick={() => setCreated(null)}>
              Fertig
            </Button>
          </>
        }
      >
        <code className="block break-all rounded-md bg-black/30 px-3 py-2 text-xs">{created}</code>
      </Dialog>

      <ConfirmDialog
        open={!!removing}
        onClose={() => setRemoving(null)}
        title={`„${removing?.name}“ widerrufen?`}
        text="Programme, die diesen Schlüssel nutzen, verlieren sofort den Zugriff."
        confirm="Widerrufen"
        danger
        onConfirm={async () => {
          if (!removing) return;
          await toast.run(() => api.delete(`/me/keys/${removing.id}`), "Schlüssel widerrufen");
          mutate();
        }}
      />
    </Card>
  );
}

// ---------------------------------------------------------------- Sitzungen

function browserName(agent: string): string {
  const match = /(Edg|Firefox|Chrome|Safari|OPR)\/[\d.]+/.exec(agent);
  const name = match ? ({ Edg: "Edge", OPR: "Opera" } as Record<string, string>)[match[1]] ?? match[1] : "Browser";
  const system = /Windows/.test(agent) ? "Windows" : /Android/.test(agent) ? "Android" : /iPhone|iPad/.test(agent) ? "iOS" : /Mac OS/.test(agent) ? "macOS" : /Linux/.test(agent) ? "Linux" : "";
  return system ? `${name} auf ${system}` : name;
}

function SessionsCard() {
  const toast = useToast();
  const { data, error, mutate } = useApi<SessionInfo[]>("/me/sessions");
  return (
    <Card>
      <CardHeader title="Angemeldete Geräte" description="Beende Sitzungen, die du nicht kennst." />
      {error ? (
        <ErrorState inline error={error} retry={() => mutate()} />
      ) : !data ? (
        <Loading />
      ) : data.length === 0 ? (
        <EmptyState icon={<MonitorSmartphone className="h-6 w-6" />} title="Keine Sitzungen" text="Du nutzt das Panel gerade über einen API-Schlüssel." />
      ) : (
        <div className="divide-y divide-white/[0.05]">
          {data.map((session) => (
            <div key={session.id} className="flex flex-wrap items-center gap-x-4 gap-y-1 px-5 py-3">
              <div className="min-w-[180px] flex-1">
                <p className="text-sm font-medium">
                  {browserName(session.userAgent)} {session.current && <Badge tone="good">Dieses Gerät</Badge>}
                </p>
                <p className="text-xs text-muted">
                  {session.ip || "unbekannte Adresse"} · angemeldet {ago(session.createdAt)}
                </p>
              </div>
              {!session.current && (
                <Button
                  size="sm"
                  onClick={async () => {
                    await toast.run(() => api.delete(`/me/sessions/${session.id}`), "Sitzung beendet");
                    mutate();
                  }}
                >
                  <LogOut className="h-3.5 w-3.5" />
                  Beenden
                </Button>
              )}
            </div>
          ))}
        </div>
      )}
    </Card>
  );
}
