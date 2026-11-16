"use client";

import { Check, Copy } from "lucide-react";
import { usePathname, useSearchParams } from "next/navigation";
import { Suspense, useEffect, useState } from "react";
import { BlockIcon, LodestoneLogo, SignalBars } from "@/components/ui/mc";
import { Badge, Button } from "@/components/ui/primitives";
import { api } from "@/lib/api";
import type { PublicStatus } from "@/lib/types";

/** Öffentliche Statusseite eines Projekts, ohne Anmeldung. Aufruf: /status/<Kürzel>/ oder /status/?p=<Kürzel>, mit &embed=1 ohne Rahmen. */
export default function StatusPage() {
  return (
    <Suspense fallback={null}>
      <StatusView />
    </Suspense>
  );
}

function parseStatusSlug(pathname: string, searchParams: URLSearchParams): string | null {
  const segments = pathname.split("/").filter(Boolean);
  if (segments[0] === "status" && segments[1]) {
    return segments[1];
  }
  return searchParams.get("p");
}

function StatusView() {
  const pathname = usePathname() ?? "";
  const params = useSearchParams();
  const slug = parseStatusSlug(pathname, params);
  const embed = params.get("embed") === "1";
  const [status, setStatus] = useState<PublicStatus | null>(null);
  const [missing, setMissing] = useState(false);
  const [copied, setCopied] = useState(false);

  useEffect(() => {
    if (!slug) {
      setMissing(true);
      return;
    }
    let stop = false;
    const load = () =>
      api
        .get<PublicStatus>(`/public/status/${encodeURIComponent(slug)}`)
        .then((data) => {
          if (!stop) {
            setStatus(data);
            setMissing(false);
          }
        })
        .catch(() => !stop && setStatus((current) => (current ? current : (setMissing(true), null))));
    load();
    const timer = setInterval(load, 20000);
    return () => {
      stop = true;
      clearInterval(timer);
    };
  }, [slug]);

  async function copy() {
    if (!status?.address) return;
    await navigator.clipboard.writeText(status.address).catch(() => null);
    setCopied(true);
    setTimeout(() => setCopied(false), 1500);
  }

  return (
    <main className={embed ? "p-3" : "grid min-h-screen place-items-center px-4 py-10"}>
      <div className="w-full max-w-md">
        {missing && !status ? (
          <div className="rounded-xl border border-white/[0.07] bg-surface p-8 text-center shadow-panel">
            <p className="text-sm text-muted">404</p>
            <h1 className="mt-1 font-display text-xl tracking-wide">Diese Statusseite gibt es nicht</h1>
            <p className="mt-2 text-sm text-muted">Sie ist nicht veröffentlicht oder der Link stimmt nicht.</p>
          </div>
        ) : !status ? (
          <p className="text-center text-sm text-muted">Wird geladen</p>
        ) : (
          <div className="overflow-hidden rounded-xl border border-white/[0.07] bg-surface shadow-panel">
            <div className="flex items-center gap-3 border-b border-white/[0.06] p-5">
              <BlockIcon name={status.name} size={40} />
              <div className="min-w-0 flex-1">
                <h1 className="truncate font-display text-lg tracking-wide text-pixel-shadow">{status.name}</h1>
                {status.description && <p className="truncate text-xs text-muted">{status.description}</p>}
              </div>
              <Badge tone={status.online ? "good" : "bad"}>
                <SignalBars level={status.online ? 5 : 0} />
                {status.online ? "Online" : "Offline"}
              </Badge>
            </div>

            <div className="grid grid-cols-2 gap-px bg-white/[0.05]">
              <div className="bg-surface p-5">
                <p className="text-xs text-muted">Spieler</p>
                <p className="mt-1 font-display text-2xl tabular-nums tracking-wide">
                  {status.online ? status.players : "–"}
                  {status.online && status.maxPlayers ? <span className="text-base font-normal text-muted"> / {status.maxPlayers}</span> : null}
                </p>
              </div>
              <div className="bg-surface p-5">
                <p className="text-xs text-muted">Version</p>
                <p className="mt-1 truncate font-display text-2xl tracking-wide">{status.version || "–"}</p>
              </div>
            </div>

            {status.address && (
              <div className="flex items-center gap-2 border-t border-white/[0.06] p-4">
                <code className="min-w-0 flex-1 truncate rounded-md border border-white/[0.08] bg-black/30 px-3 py-1.5 font-mono text-sm shadow-sunken">{status.address}</code>
                <Button size="sm" onClick={copy}>
                  {copied ? <Check className="h-3.5 w-3.5 text-good" /> : <Copy className="h-3.5 w-3.5" />}
                  {copied ? "Kopiert" : "Kopieren"}
                </Button>
              </div>
            )}

            {status.servers.length > 1 && (
              <ul className="divide-y divide-white/[0.05] border-t border-white/[0.06]">
                {status.servers.map((server) => (
                  <li key={server.name} className="flex items-center gap-3 px-5 py-2.5 text-sm">
                    <SignalBars level={server.online ? 5 : 0} className={server.online ? "text-good" : "text-bad"} />
                    <span className="min-w-0 flex-1 truncate">{server.name}</span>
                    <span className="text-xs tabular-nums text-muted">{server.online ? `${server.players}${server.maxPlayers ? ` / ${server.maxPlayers}` : ""} Spieler` : "offline"}</span>
                  </li>
                ))}
              </ul>
            )}
          </div>
        )}
        {!embed && status && <p className="mt-4 flex items-center justify-center gap-1.5 text-[11px] text-muted/80"><LodestoneLogo size={12} /> Angetrieben von Lodestone</p>}
      </div>
    </main>
  );
}
