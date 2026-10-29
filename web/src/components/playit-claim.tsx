"use client";

import { ExternalLink, Loader2 } from "lucide-react";
import { useEffect, useRef, useState } from "react";
import { Button } from "@/components/ui/primitives";
import { Dialog } from "@/components/ui/feedback";
import { api, ApiError } from "@/lib/api";
import type { PlayitClaim } from "@/lib/types";

const POLL_MS = 2500;

/**
 * Verbindet den Node mit playit.gg. Schlüssel aus dem playit-Dashboard dürfen keine Tunnel anlegen; den passenden Schlüssel
 * bekommt der Node, wenn du hier einmal auf playit.gg bestätigst. Er landet nie im Browser.
 */
export function PlayitClaimDialog({ nodeId, open, onClose, onDone }: { nodeId: string; open: boolean; onClose: () => void; onDone: () => void }) {
  const [claim, setClaim] = useState<PlayitClaim | null>(null);
  const [error, setError] = useState("");
  const [attempt, setAttempt] = useState(0);
  // Der Effekt darf nicht neu starten, nur weil die Elternkomponente eine neue Funktion übergibt.
  const done = useRef(onDone);
  done.current = onDone;

  useEffect(() => {
    if (!open) return;
    let stopped = false;
    let timer: ReturnType<typeof setTimeout> | undefined;
    const fail = (cause: unknown) => !stopped && setError(cause instanceof ApiError || cause instanceof Error ? cause.message : "Unbekannter Fehler");

    const poll = async () => {
      try {
        const next = await api.get<PlayitClaim>(`/nodes/${nodeId}/playit/claim`);
        if (stopped) return;
        // „none“ heißt, der Node hat die Verbindung vergessen (z. B. neu gestartet): wie abgelaufen behandeln
        setClaim(next.state === "none" ? { state: "expired" } : next);
        if (next.state === "accepted") done.current();
        else if (next.state === "waiting") timer = setTimeout(poll, POLL_MS);
      } catch (cause) {
        fail(cause);
      }
    };

    setClaim(null);
    setError("");
    api
      .post<PlayitClaim>(`/nodes/${nodeId}/playit/claim`)
      .then((started) => {
        if (stopped) return;
        setClaim(started);
        timer = setTimeout(poll, POLL_MS);
      })
      .catch(fail);
    return () => {
      stopped = true;
      clearTimeout(timer);
    };
  }, [open, nodeId, attempt]);

  const retry = (
    <Button variant="primary" onClick={() => setAttempt((count) => count + 1)}>
      Erneut versuchen
    </Button>
  );
  const ended = claim?.state === "rejected" || claim?.state === "expired";

  return (
    <Dialog
      open={open}
      onClose={onClose}
      title="Mit playit verbinden"
      description="Bestätige einmal auf playit.gg, dass dieser Node Tunnel anlegen darf."
      footer={
        <>
          <Button variant="ghost" onClick={onClose}>
            Abbrechen
          </Button>
          {(error || ended) && retry}
        </>
      }
    >
      {error && <p className="text-xs text-bad">{error}</p>}
      {ended && <p className="text-xs text-bad">{claim.state === "rejected" ? "Du hast die Verbindung auf playit.gg abgelehnt." : "Der Link ist abgelaufen."}</p>}
      {!error && claim?.state === "waiting" && claim.url && (
        <div className="space-y-3 text-xs text-muted">
          <ol className="list-decimal space-y-1 pl-5">
            <li>Öffne den Link und melde dich bei playit.gg an.</li>
            <li>Bestätige dort die Verbindung. Dieses Fenster erkennt das von selbst.</li>
          </ol>
          <a className="inline-flex items-center gap-1.5 text-sm text-accent hover:underline" href={claim.url} target="_blank" rel="noreferrer">
            <ExternalLink className="h-3.5 w-3.5" />
            Auf playit.gg bestätigen
          </a>
          <p className="flex items-center gap-2">
            <Loader2 className="h-3.5 w-3.5 animate-spin" />
            Warte auf deine Bestätigung
          </p>
        </div>
      )}
      {!error && !ended && claim?.state !== "waiting" && (
        <p className="flex items-center gap-2 text-xs text-muted">
          <Loader2 className="h-3.5 w-3.5 animate-spin" />
          Verbindung wird vorbereitet
        </p>
      )}
    </Dialog>
  );
}
