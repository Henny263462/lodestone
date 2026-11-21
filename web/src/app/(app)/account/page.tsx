"use client";

import QRCode from "qrcode";
import { Copy, Fingerprint, HardDriveUpload, KeyRound, Languages, Link2, LockKeyhole, LogOut, MonitorSmartphone, Plus, ShieldCheck, Trash2 } from "lucide-react";
import { Suspense, useEffect, useState } from "react";
import { Badge, Button, Card, CardHeader, EmptyState, PageHeader, SectionLayout, type Section } from "@/components/ui/primitives";
import { ConfirmDialog, Dialog, useToast } from "@/components/ui/feedback";
import { ErrorState, Loading } from "@/components/ui/async";
import { useUser } from "@/components/layout/shell";
import { OidcCard, PasskeysCard } from "@/components/account-sign-in";
import { PixelAvatar } from "@/components/ui/mc";
import { SftpAccountCard } from "@/components/sftp-account";
import { LanguageCard } from "@/components/language";
import { api, useApi } from "@/lib/api";
import { ago, dateTime } from "@/lib/format";
import { useUrlState } from "@/lib/url-state";
import type { ApiKeyInfo, Me, OidcAccount, SessionInfo } from "@/lib/types";
import { t, tp } from "@/lib/i18n";

type SectionId = "password" | "totp" | "passkeys" | "oidc" | "keys" | "sftp" | "sessions" | "language";

const sections = (): Section<SectionId>[] => [
  { id: "password", label: t("Passwort"), icon: LockKeyhole },
  { id: "totp", label: t("Zwei-Faktor"), icon: ShieldCheck },
  { id: "passkeys", label: "Passkeys", icon: Fingerprint },
  { id: "oidc", label: t("Anmeldung über OIDC"), icon: Link2 },
  { id: "keys", label: t("API-Schlüssel"), icon: KeyRound },
  { id: "sftp", label: t("SFTP-Zugang"), icon: HardDriveUpload },
  { id: "sessions", label: t("Sitzungen"), icon: MonitorSmartphone },
  { id: "language", label: t("Sprache"), icon: Languages },
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
  const active = sections().some((entry) => entry.id === section) ? section : "password";
  return (
    <>
      <PageHeader title={t("Konto")} description={t("Anmeldung, Sicherheit und Zugriff für Skripte.")} />
      <div className="max-w-5xl space-y-5">
        <Card className="flex items-center gap-4 p-5">
          <PixelAvatar name={user.username} size={48} />
          <div className="min-w-0">
            <p className="truncate text-sm font-semibold">{user.username}</p>
            {user.isAdmin ? <Badge tone="accent">{t("Administrator")}</Badge> : <p className="text-xs text-muted">{t("Benutzer")}</p>}
          </div>
        </Card>
        <SectionLayout label={t("Abschnitte des Kontos")} sections={sections()} value={active} onChange={setSection}>
          {active === "password" && <PasswordCard />}
          {active === "totp" && <TwoFactorCard />}
          {active === "passkeys" && <PasskeysCard />}
          {active === "oidc" && <OidcCard />}
          {active === "keys" && <KeysCard />}
          {active === "sftp" && <SftpAccountCard />}
          {active === "sessions" && <SessionsCard />}
          {active === "language" && <LanguageCard />}
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
        <CardHeader title={t("Passwort")} />
        <ErrorState inline error={error} retry={() => mutate()} />
      </Card>
    );
  if (!account)
    return (
      <Card>
        <CardHeader title={t("Passwort")} />
        <Loading />
      </Card>
    );
  const provider = account.name ?? t("deinen Anbieter");
  if (first && !account.passwordLogin)
    return (
      <Card>
        <CardHeader title={t("Passwort")} description={t("Dein Konto hat kein Passwort. Du meldest dich über OIDC oder einen Passkey an.")} />
        <p className="px-5 py-4 text-xs text-muted">{t("Die Anmeldung mit Passwort ist auf diesem Panel abgeschaltet. Ein Passwort brauchst du deshalb nicht.")}</p>
      </Card>
    );
  if (first && account.firstPasswordUntil <= Date.now())
    return (
      <Card>
        <CardHeader title={t("Passwort festlegen")} description={t("Dein Konto hat noch kein Passwort. Du meldest dich über OIDC oder einen Passkey an.")} />
        <p className="px-5 py-4 text-xs text-muted">
          {t("Ein erstes Passwort kannst du in den ersten 10 Minuten nach einer Anmeldung festlegen. Melde dich ab und neu über {provider} oder einen Passkey an, dann kommst du hierher zurück.", { provider })}
        </p>
      </Card>
    );

  return (
    <Card>
      <CardHeader
        title={first ? t("Passwort festlegen") : t("Passwort ändern")}
        description={
          first
            ? t("Dein Konto hat noch kein Passwort. Mit einem Passwort kommst du auch ohne {provider} herein. Möglich bis {firstPasswordUntil}.", { provider, firstPasswordUntil: dateTime(account.firstPasswordUntil) })
            : t("Danach musst du dich auf anderen Geräten neu anmelden.")
        }
      />
      <form
        className="space-y-4 px-5 py-4"
        onSubmit={async (event) => {
          event.preventDefault();
          setBusy(true);
          const ok = await toast.run(() => api.post("/me/password", { current: first ? "" : current, next }), first ? t("Passwort festgelegt") : t("Passwort geändert"));
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
            <label className="label">{t("Aktuelles Passwort")}</label>
            <input className="field" type="password" autoComplete="current-password" value={current} onChange={(event) => setCurrent(event.target.value)} required />
          </div>
        )}
        <div>
          <label className="label">{t("Neues Passwort (mindestens 8 Zeichen)")}</label>
          <input className="field" type="password" autoComplete="new-password" value={next} onChange={(event) => setNext(event.target.value)} required minLength={8} />
        </div>
        <div>
          <label className="label">{t("Neues Passwort wiederholen")}</label>
          <input className="field" type="password" autoComplete="new-password" value={repeat} onChange={(event) => setRepeat(event.target.value)} required />
          {mismatch && <p className="mt-1 text-xs text-bad">{t("Die Passwörter stimmen nicht überein.")}</p>}
        </div>
        <Button type="submit" variant="primary" loading={busy} disabled={(!first && !current) || next.length < 8 || next !== repeat}>
          {first ? t("Passwort festlegen") : t("Passwort ändern")}
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
    <CardHeader title={t("Zwei-Faktor-Anmeldung")} description={t("Bei der Anmeldung braucht es zusätzlich einen Code aus einer App wie Aegis oder 1Password.")} action={action} />
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
      {header(me.totpEnabled ? <Badge tone="good">{t("Eingeschaltet")}</Badge> : <Badge>{t("Aus")}</Badge>)}
      <div className="space-y-4 px-5 py-4 text-sm">
        {me.totpEnabled ? (
          <>
            <p className="text-xs text-muted">{tp("recoveryCodesLeft", me.recoveryCodes)} {t("Jeder gilt einmal, falls du die App nicht hast.")}</p>
            <Button variant="danger" onClick={() => setDisabling(true)}>
              {t("Ausschalten")}
            </Button>
          </>
        ) : setup ? (
          <div className="flex flex-wrap items-start gap-5">
            {/* eslint-disable-next-line @next/next/no-img-element */}
            {qr && <img src={qr} alt="QR-Code" width={160} height={160} className="rounded-lg bg-white p-1" />}
            <div className="min-w-[220px] flex-1 space-y-3">
              <p className="text-xs text-muted">{t("Scanne den Code mit deiner App oder gib diesen Schlüssel ein:")}</p>
              <code className="block break-all rounded-md bg-black/30 px-3 py-2 text-xs">{setup.secret}</code>
              <div>
                <label className="label">{t("Code aus der App zur Bestätigung")}</label>
                <input className="field w-40 font-mono tracking-widest" inputMode="numeric" value={code} onChange={(event) => setCode(event.target.value)} placeholder="000000" />
              </div>
              <div className="flex gap-2">
                <Button variant="ghost" onClick={() => setSetup(null)}>
                  {t("Abbrechen")}
                </Button>
                <Button
                  variant="primary"
                  disabled={code.trim().length < 6}
                  onClick={async () => {
                    const ok = await toast.run(async () => setCodes((await api.post<{ recoveryCodes: string[] }>("/me/totp/enable", { code })).recoveryCodes), t("Zwei-Faktor-Anmeldung ist an"));
                    if (ok) {
                      setSetup(null);
                      setCode("");
                      mutate();
                    }
                  }}
                >
                  {t("Einschalten")}
                </Button>
              </div>
            </div>
          </div>
        ) : (
          <Button
            onClick={() => toast.run(async () => setSetup(await api.post<{ secret: string; uri: string }>("/me/totp/setup")))}
          >
            <ShieldCheck className="h-4 w-4" />
            {t("Einrichten")}
          </Button>
        )}
      </div>

      <Dialog
        open={!!codes}
        onClose={() => setCodes(null)}
        title={t("Deine Wiederherstellungscodes")}
        description={t("Speichere sie jetzt an einem sicheren Ort. Sie werden nicht noch einmal angezeigt.")}
        footer={
          <>
            <Button onClick={() => navigator.clipboard.writeText((codes ?? []).join("\n")).then(() => toast.success(t("Kopiert")))}>
              <Copy className="h-3.5 w-3.5" />
              {t("Kopieren")}
            </Button>
            <Button variant="primary" onClick={() => setCodes(null)}>
              {t("Gespeichert")}
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
        title={t("Zwei-Faktor-Anmeldung ausschalten")}
        footer={
          <>
            <Button variant="ghost" onClick={() => setDisabling(false)}>
              {t("Abbrechen")}
            </Button>
            <Button
              variant="danger"
              disabled={!password || !otp}
              onClick={async () => {
                if (await toast.run(() => api.post("/me/totp/disable", { password, code: otp }), t("Zwei-Faktor-Anmeldung ist aus"))) {
                  setDisabling(false);
                  setPassword("");
                  setOtp("");
                  mutate();
                }
              }}
            >
              {t("Ausschalten")}
            </Button>
          </>
        }
      >
        <div>
          <label className="label">{t("Passwort")}</label>
          <input className="field" type="password" value={password} onChange={(event) => setPassword(event.target.value)} autoComplete="current-password" />
        </div>
        <div>
          <label className="label">{t("Code aus der App oder Wiederherstellungscode")}</label>
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
        title={t("API-Schlüssel")}
        description={t("Für Skripte: Ein Schlüssel hat deine Rechte, kommt aber nicht an deine Zugangsdaten.")}
        action={
          data &&
          data.length > 0 && (
            <Button size="sm" onClick={() => setCreating(true)}>
              <Plus className="h-3.5 w-3.5" />
              {t("Neuer Schlüssel")}
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
          <EmptyState icon={<KeyRound className="h-6 w-6" />} title={t("Noch kein API-Schlüssel")} text={t("Lege einen an, wenn ein Skript das Panel bedienen soll. Er gehört in den Header Authorization: Bearer lds_…")} />
          <div className="flex justify-center">
            <Button variant="primary" onClick={() => setCreating(true)}>
              <Plus className="h-4 w-4" />
              {t("Schlüssel anlegen")}
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
                  <span className="font-mono">{key.prefix}…</span> · {key.lastUsed ? t("zuletzt {time}", { time: ago(key.lastUsed) }) : t("noch nie benutzt")}
                  {key.expiresAt > 0 && t(" · läuft {expiresAt} ab", { expiresAt: dateTime(key.expiresAt) })}
                </p>
              </div>
              <Button variant="ghost" size="icon-sm" onClick={() => setRemoving(key)} aria-label={t("{name} widerrufen", { name: key.name })} title={t("Widerrufen")}>
                <Trash2 className="h-3.5 w-3.5" />
              </Button>
            </div>
          ))}
        </div>
      )}

      <Dialog
        open={creating}
        onClose={() => setCreating(false)}
        title={t("Neuer API-Schlüssel")}
        footer={
          <>
            <Button variant="ghost" onClick={() => setCreating(false)}>
              {t("Abbrechen")}
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
              {t("Anlegen")}
            </Button>
          </>
        }
      >
        <div>
          <label className="label">{t("Name")}</label>
          <input className="field" value={name} onChange={(event) => setName(event.target.value)} placeholder={t("Backup-Skript")} maxLength={48} autoFocus />
        </div>
        <div>
          <label className="label">{t("Gültig für (Tage, 0 = unbegrenzt)")}</label>
          <input className="field w-32" inputMode="numeric" value={days} onChange={(event) => setDays(event.target.value)} />
        </div>
      </Dialog>

      <Dialog
        open={!!created}
        onClose={() => setCreated(null)}
        title={t("Dein neuer Schlüssel")}
        description={t("Kopiere ihn jetzt. Aus Sicherheitsgründen siehst du ihn danach nicht mehr.")}
        footer={
          <>
            <Button onClick={() => navigator.clipboard.writeText(created ?? "").then(() => toast.success(t("Kopiert")))}>
              <Copy className="h-3.5 w-3.5" />
              {t("Kopieren")}
            </Button>
            <Button variant="primary" onClick={() => setCreated(null)}>
              {t("Fertig")}
            </Button>
          </>
        }
      >
        <code className="block break-all rounded-md bg-black/30 px-3 py-2 text-xs">{created}</code>
      </Dialog>

      <ConfirmDialog
        open={!!removing}
        onClose={() => setRemoving(null)}
        title={t("„{name}“ widerrufen?", { name: removing?.name })}
        text={t("Programme, die diesen Schlüssel nutzen, verlieren sofort den Zugriff.")}
        confirm={t("Widerrufen")}
        danger
        onConfirm={async () => {
          if (!removing) return;
          await toast.run(() => api.delete(`/me/keys/${removing.id}`), t("Schlüssel widerrufen"));
          mutate();
        }}
      />
    </Card>
  );
}

// ---------------------------------------------------------------- Sitzungen

function browserName(agent: string): string {
  const match = /(Edg|Firefox|Chrome|Safari|OPR)\/[\d.]+/.exec(agent);
  const name = match ? ({ Edg: "Edge", OPR: "Opera" } as Record<string, string>)[match[1]] ?? match[1] : t("Browser");
  const system = /Windows/.test(agent) ? "Windows" : /Android/.test(agent) ? "Android" : /iPhone|iPad/.test(agent) ? "iOS" : /Mac OS/.test(agent) ? "macOS" : /Linux/.test(agent) ? "Linux" : "";
  return system ? t("{name} auf {system}", { name, system }) : name;
}

function SessionsCard() {
  const toast = useToast();
  const { data, error, mutate } = useApi<SessionInfo[]>("/me/sessions");
  return (
    <Card>
      <CardHeader title={t("Angemeldete Geräte")} description={t("Beende Sitzungen, die du nicht kennst.")} />
      {error ? (
        <ErrorState inline error={error} retry={() => mutate()} />
      ) : !data ? (
        <Loading />
      ) : data.length === 0 ? (
        <EmptyState icon={<MonitorSmartphone className="h-6 w-6" />} title={t("Keine Sitzungen")} text={t("Du nutzt das Panel gerade über einen API-Schlüssel.")} />
      ) : (
        <div className="divide-y divide-white/[0.05]">
          {data.map((session) => (
            <div key={session.id} className="flex flex-wrap items-center gap-x-4 gap-y-1 px-5 py-3">
              <div className="min-w-[180px] flex-1">
                <p className="text-sm font-medium">
                  {browserName(session.userAgent)} {session.current && <Badge tone="good">{t("Dieses Gerät")}</Badge>}
                </p>
                <p className="text-xs text-muted">
                  {session.ip || t("unbekannte Adresse")} {t("· angemeldet")} {ago(session.createdAt)}
                </p>
              </div>
              {!session.current && (
                <Button
                  size="sm"
                  onClick={async () => {
                    await toast.run(() => api.delete(`/me/sessions/${session.id}`), t("Sitzung beendet"));
                    mutate();
                  }}
                >
                  <LogOut className="h-3.5 w-3.5" />
                  {t("Beenden")}
                </Button>
              )}
            </div>
          ))}
        </div>
      )}
    </Card>
  );
}
