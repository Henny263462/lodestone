"use client";

import { ArrowRight, Eye, EyeOff, Lock, User } from "lucide-react";
import { useEffect, useState, type FormEvent } from "react";
import { Button } from "@/components/ui/primitives";
import { LodestoneLogo, Wordmark } from "@/components/ui/mc";
import { api } from "@/lib/api";
import type { AuthStatus } from "@/lib/types";

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

  useEffect(() => {
    api
      .get<AuthStatus>("/auth/status")
      .then((status) => {
        if (status.user) window.location.replace(nextTarget());
        setSetup(status.setupRequired);
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
      const answer = await api.post<{ needsTotp?: boolean; ticket?: string }>(setup ? "/auth/setup" : "/auth/login", { username, password });
      if (answer?.needsTotp && answer.ticket) {
        setTicket(answer.ticket);
        return;
      }
      window.location.replace(nextTarget());
    } catch (failure) {
      setError(failure instanceof Error ? failure.message : "Anmeldung fehlgeschlagen");
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
          <h1 className="mt-4 font-display text-lg tracking-wide">{setup ? "Einrichtung" : "Willkommen zurück"}</h1>
          <p className="mt-1 text-sm text-muted">
            {setup ? "Lege den ersten Administrator an." : "Melde dich an, um deine Server zu verwalten."}
          </p>
        </div>

        <form onSubmit={submit} className="rounded-xl border border-white/[0.07] bg-surface p-6 shadow-panel">
          <div className="space-y-4">
            {ticket ? (
              <div>
                <label className="label" htmlFor="code">
                  Code aus deiner Authenticator-App
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
                <p className="mt-2 text-xs text-muted">Du hast die App nicht zur Hand? Gib stattdessen einen Wiederherstellungscode ein (z. B. a1b2c-d3e4f).</p>
              </div>
            ) : (
            <>
            <div>
              <label className="label" htmlFor="username">
                Benutzername
              </label>
              <div className="relative">
                <User className="pointer-events-none absolute left-3 top-1/2 h-4 w-4 -translate-y-1/2 text-muted/70" />
                <input id="username" className="field pl-9" autoComplete="username" autoFocus value={username} onChange={(event) => setUsername(event.target.value)} placeholder="admin" required />
              </div>
            </div>

            <div>
              <label className="label" htmlFor="password">
                Passwort
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
                  placeholder={setup ? "mindestens 8 Zeichen" : "••••••••"}
                  required
                />
                <button
                  type="button"
                  onClick={() => setVisible((value) => !value)}
                  className="absolute right-2 top-1/2 -translate-y-1/2 rounded-md p-1.5 text-muted transition hover:bg-white/[0.06] hover:text-ink"
                  aria-label={visible ? "Passwort verbergen" : "Passwort anzeigen"}
                >
                  {visible ? <EyeOff className="h-4 w-4" /> : <Eye className="h-4 w-4" />}
                </button>
              </div>
            </div>
            </>
            )}

            {error && <p className="rounded-lg border border-bad/25 bg-bad/10 px-3 py-2 text-xs leading-relaxed text-bad">{error}</p>}

            <Button type="submit" variant="primary" size="lg" loading={busy} className="w-full">
              {busy ? "Einen Moment" : ticket ? "Bestätigen" : setup ? "Administrator anlegen" : "Anmelden"}
              {!busy && <ArrowRight className="h-4 w-4" />}
            </Button>
          </div>
        </form>
      </div>
    </main>
  );
}

function nextTarget(): string {
  const next = new URLSearchParams(window.location.search).get("next");
  return next && next.startsWith("/") && !next.startsWith("//") ? next : "/";
}
