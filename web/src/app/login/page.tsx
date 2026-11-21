"use client";

import { ArrowRight, Eye, EyeOff, Lock, User } from "lucide-react";
import { useEffect, useState, type FormEvent } from "react";
import { Button } from "@/components/ui/primitives";
import { LodestoneLogo, Wordmark } from "@/components/ui/mc";
import { PasskeySecondFactor, SignInOptions } from "@/components/sign-in";
import { LanguageSwitch } from "@/components/language";
import { api } from "@/lib/api";
import { signInErrorText } from "@/lib/sign-in";
import type { AuthStatus, LoginStep, SecondFactor } from "@/lib/types";
import { t } from "@/lib/i18n";

export default function LoginPage() {
  const [username, setUsername] = useState("");
  const [password, setPassword] = useState("");
  const [visible, setVisible] = useState(false);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [setup, setSetup] = useState(false);
  // Zweiter Schritt der Anmeldung (Zwei-Faktor): Ticket vom Server und der Code aus der App
  const [ticket, setTicket] = useState<string | null>(null);
  const [code, setCode] = useState("");
  const [status, setStatus] = useState<AuthStatus | null>(null);
  const [methods, setMethods] = useState<SecondFactor[]>(["totp"]);
  const passwordOff = !setup && status?.passwordLogin === false;

  useEffect(() => {
    // Rücksprung von einer Anmeldung über OIDC mit Fehlercode
    const failed = new URLSearchParams(window.location.search).get("error");
    if (failed) setError(signInErrorText(failed));
    api
      .get<AuthStatus>("/auth/status")
      .then((status) => {
        if (status.user) window.location.replace(nextTarget());
        setSetup(status.setupRequired);
        setStatus(status);
      })
      .catch(() => null);
  }, []);

  async function submit(event: FormEvent) {
    event.preventDefault();
    setBusy(true);
    setError(null);
    try {
      if (ticket) {
        await api.post("/auth/totp", { ticket, code });
        window.location.replace(nextTarget());
        return;
      }
      const answer = await api.post<LoginStep>(setup ? "/auth/setup" : "/auth/login", { username, password });
      if (answer?.needsTotp && answer.ticket) {
        setTicket(answer.ticket);
        setMethods(answer.secondFactor ?? ["totp"]);
        return;
      }
      window.location.replace(nextTarget());
    } catch (failure) {
      setError(failure instanceof Error ? failure.message : t("Anmeldung fehlgeschlagen"));
    } finally {
      setBusy(false);
    }
  }

  return (
    <main className="flex min-h-screen items-center justify-center px-4 py-10">
      <div className="w-full max-w-[400px]">
        <div className="mb-7 flex flex-col items-center text-center">
          <LodestoneLogo size={72} className="mb-4" />
          <Wordmark className="text-2xl" />
          <h1 className="mt-4 font-display text-lg tracking-wide">{setup ? t("Einrichtung") : t("Willkommen zurück")}</h1>
          <p className="mt-1 text-sm text-muted">
            {setup ? t("Lege den ersten Administrator an.") : t("Melde dich an, um deine Server zu verwalten.")}
          </p>
        </div>

        <form onSubmit={submit} className="rounded-xl border border-white/[0.07] bg-surface p-6 shadow-panel">
          <div className="space-y-4">
            {ticket ? (
              methods.includes("totp") && (
              <div>
                <label className="label" htmlFor="code">
                  {t("Code aus deiner Authenticator-App")}
                </label>
                <input
                  id="code"
                  className="field text-center font-mono text-lg tracking-[0.3em]"
                  inputMode="numeric"
                  autoComplete="one-time-code"
                  autoFocus
                  value={code}
                  onChange={(event) => setCode(event.target.value)}
                  placeholder="000000"
                  required
                />
                <p className="mt-2 text-xs text-muted">{t("Du hast die App nicht zur Hand? Gib stattdessen einen Wiederherstellungscode ein (z. B. a1b2c-d3e4f).")}</p>
              </div>
              )
            ) : passwordOff ? (
              <p className="text-center text-xs text-muted">{t("Die Anmeldung mit Passwort ist auf diesem Panel abgeschaltet.")}</p>
            ) : (
            <>
            <div>
              <label className="label" htmlFor="username">
                {t("Benutzername")}
              </label>
              <div className="relative">
                <User className="pointer-events-none absolute left-3 top-1/2 h-4 w-4 -translate-y-1/2 text-muted/70" />
                <input id="username" className="field pl-9" autoComplete="username" autoFocus value={username} onChange={(event) => setUsername(event.target.value)} placeholder="admin" required />
              </div>
            </div>

            <div>
              <label className="label" htmlFor="password">
                {t("Passwort")}
              </label>
              <div className="relative">
                <Lock className="pointer-events-none absolute left-3 top-1/2 h-4 w-4 -translate-y-1/2 text-muted/70" />
                <input
                  id="password"
                  className="field pl-9 pr-10"
                  type={visible ? "text" : "password"}
                  autoComplete={setup ? "new-password" : "current-password"}
                  value={password}
                  onChange={(event) => setPassword(event.target.value)}
                  placeholder={setup ? t("mindestens 8 Zeichen") : "••••••••"}
                  required
                />
                <button
                  type="button"
                  onClick={() => setVisible((value) => !value)}
                  className="absolute right-2 top-1/2 -translate-y-1/2 rounded-md p-1.5 text-muted transition hover:bg-white/[0.06] hover:text-ink"
                  aria-label={visible ? t("Passwort verbergen") : t("Passwort anzeigen")}
                >
                  {visible ? <EyeOff className="h-4 w-4" /> : <Eye className="h-4 w-4" />}
                </button>
              </div>
            </div>
            </>
            )}

            {error && <p className="rounded-lg border border-bad/25 bg-bad/10 px-3 py-2 text-xs leading-relaxed text-bad">{error}</p>}

            {ticket && methods.includes("passkey") && <PasskeySecondFactor ticket={ticket} next={nextTarget} onError={setError} />}

            {!passwordOff && (!ticket || methods.includes("totp")) && (
            <Button type="submit" variant="primary" size="lg" loading={busy} className="w-full">
              {busy ? t("Einen Moment") : ticket ? t("Bestätigen") : setup ? t("Administrator anlegen") : t("Anmelden")}
              {!busy && <ArrowRight className="h-4 w-4" />}
            </Button>
            )}

            {!ticket && !setup && status && <SignInOptions status={status} divider={!passwordOff} next={nextTarget} onError={setError} />}
          </div>
        </form>
        <div className="mt-6 flex justify-center">
          <LanguageSwitch />
        </div>
      </div>
    </main>
  );
}

function nextTarget(): string {
  const next = new URLSearchParams(window.location.search).get("next");
  return next && next.startsWith("/") && !next.startsWith("//") ? next : "/";
}
