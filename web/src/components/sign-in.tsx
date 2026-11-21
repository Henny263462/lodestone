"use client";

import { Fingerprint, LogIn } from "lucide-react";
import { useEffect, useState } from "react";
import { Button } from "@/components/ui/primitives";
import { api, apiBase } from "@/lib/api";
import type { AuthStatus, PasskeyAssertionOptions, PasskeyCeremony } from "@/lib/types";
import { getPasskey, passkeyErrorText, passkeysSupported } from "@/lib/webauthn";
import { t } from "@/lib/i18n";

/** Ob dieser Browser Passkeys kann; erst nach dem Laden bekannt (beim Bauen gibt es kein window). */
export function usePasskeySupport(): boolean {
  const [supported, setSupported] = useState(false);
  useEffect(() => setSupported(passkeysSupported()), []);
  return supported;
}

/** Weitere Anmeldewege unter dem Formular: „Mit <Anbieter> anmelden“ und „Mit Passkey anmelden“. */
export function SignInOptions({ status, divider, next, onError }: { status: AuthStatus; divider: boolean; next: () => string; onError: (message: string | null) => void }) {
  const supported = usePasskeySupport();
  const [busy, setBusy] = useState<"oidc" | "passkey" | null>(null);
  const showPasskey = !!status.passkeys && supported;
  if (!status.oidc && !showPasskey) return null;

  async function passkeyLogin() {
    setBusy("passkey");
    onError(null);
    try {
      const start = await api.post<PasskeyCeremony<PasskeyAssertionOptions>>("/auth/passkey/begin");
      const credential = await getPasskey(start.options);
      await api.post("/auth/passkey/finish", { flow: start.flow, credential });
      window.location.replace(next());
    } catch (failure) {
      onError(passkeyErrorText(failure));
      setBusy(null);
    }
  }

  return (
    <div className="space-y-3">
      {divider && (
        <div className="flex items-center gap-3 text-[11px] text-muted">
          <span className="h-px flex-1 bg-white/[0.06]" />
          {t("oder")}
          <span className="h-px flex-1 bg-white/[0.06]" />
        </div>
      )}
      {status.oidc && (
        <Button
          type="button"
          size="lg"
          className="w-full"
          loading={busy === "oidc"}
          disabled={busy !== null}
          onClick={() => {
            setBusy("oidc");
            window.location.assign(`${apiBase()}/api/auth/oidc/start?next=${encodeURIComponent(next())}`);
          }}
        >
          {busy !== "oidc" && <LogIn className="h-4 w-4" />}
          {t("Mit {name} anmelden", { name: status.oidc.name })}
        </Button>
      )}
      {showPasskey && (
        <Button type="button" size="lg" className="w-full" loading={busy === "passkey"} disabled={busy !== null} onClick={passkeyLogin}>
          {busy !== "passkey" && <Fingerprint className="h-4 w-4" />}
          {t("Mit Passkey anmelden")}
        </Button>
      )}
    </div>
  );
}

/** Zweiter Schritt nach dem Passwort mit einem Passkey statt eines Codes aus der App. */
export function PasskeySecondFactor({ ticket, next, onError }: { ticket: string; next: () => string; onError: (message: string | null) => void }) {
  const supported = usePasskeySupport();
  const [busy, setBusy] = useState(false);
  if (!supported) {
    return <p className="text-xs text-muted">{t("Dein Konto hat einen Passkey, aber dieser Browser kann ihn hier nicht benutzen. Öffne das Panel über HTTPS.")}</p>;
  }
  return (
    <Button
      type="button"
      size="lg"
      className="w-full"
      loading={busy}
      onClick={async () => {
        setBusy(true);
        onError(null);
        try {
          const start = await api.post<PasskeyCeremony<PasskeyAssertionOptions>>("/auth/passkey/second/begin", { ticket });
          const credential = await getPasskey(start.options);
          await api.post("/auth/passkey/second/finish", { ticket, flow: start.flow, credential });
          window.location.replace(next());
        } catch (failure) {
          onError(passkeyErrorText(failure));
          setBusy(false);
        }
      }}
    >
      {!busy && <Fingerprint className="h-4 w-4" />}
      {t("Mit Passkey bestätigen")}
    </Button>
  );
}
