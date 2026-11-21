"use client";

import Link from "next/link";
import { Copy, HardDriveUpload } from "lucide-react";
import { Badge, Button, Card, CardHeader, EmptyState, Switch } from "@/components/ui/primitives";
import { useToast } from "@/components/ui/feedback";
import { ErrorState, Loading } from "@/components/ui/async";
import { api, useApi } from "@/lib/api";
import type { ServerSftp } from "@/lib/types";
import { t } from "@/lib/i18n";

// Karte „SFTP“ im Dateien-Tab: Zugangsdaten zum Kopieren. Die Zugänge selbst verwaltet das Panel (Rechte + Konto).
// Sichtbar mit den Rechten „sftp“ und „files.read“; ohne „files.write“ ist der Zugang nur lesend.
export function SftpCard({ serverId }: { serverId: string }) {
  const toast = useToast();
  const { data, error, mutate } = useApi<ServerSftp>(`/servers/${serverId}/sftp`);

  const copy = (label: string, value: string) =>
    navigator.clipboard.writeText(value).then(
      () => toast.success(`${label} kopiert`),
      () => toast.error(t("Kopieren ging nicht. Markiere den Text und kopiere ihn von Hand.")),
    );

  return (
    <Card>
      <CardHeader
        title="SFTP"
        description={t("Mit FileZilla, WinSCP oder sftp direkt auf die Dateien zugreifen. Anmeldung mit SFTP-Passwort oder SSH-Schlüssel aus dem Konto.")}
        action={data?.enabled && data.readOnly ? <Badge tone="warn">{t("Nur lesen")}</Badge> : undefined}
      />
      {error ? (
        <ErrorState inline error={error} retry={() => mutate()} />
      ) : !data ? (
        <Loading label={t("SFTP-Zugang wird geladen")} />
      ) : !data.enabled ? (
        <EmptyState
          icon={<HardDriveUpload className="h-6 w-6" />}
          title={t("SFTP ist auf diesem Node aus")}
          text={t("Ein Administrator kann es auf dem Node mit LODESTONE_NODE_SFTP_LISTEN (z. B. :2022) einschalten und den Port freigeben.")}
        />
      ) : (
        <>
          {!data.passwordReady && data.keyCount === 0 && (
            <div className="mx-5 mt-4 rounded-lg border border-warn/25 bg-warn/10 px-4 py-3 text-xs text-warn">
              {t("Du hast noch kein SFTP-Passwort und keinen SSH-Schlüssel. Lege eins davon unter")}{" "}
              <Link href="/account/?section=sftp" className="underline">
                {t("Konto")}
              </Link>{" "}
              {t("an, dann kannst du dich hier anmelden.")}
            </div>
          )}
          <div className="divide-y divide-white/[0.05]">
            <CopyRow label="Host" value={data.host} onCopy={copy} />
            <CopyRow label="Port" value={String(data.port)} onCopy={copy} />
            <CopyRow label={t("Benutzername")} value={data.username} onCopy={copy} />
            <CopyRow label={t("Fingerabdruck")} value={data.fingerprint} onCopy={copy} />
          </div>
          <p className="border-t border-white/[0.05] px-5 py-3 text-xs text-muted">
            {t("Vergleiche beim ersten Verbinden den Fingerabdruck, den dein Programm anzeigt, mit dem hier. Stimmt er nicht, verbinde dich nicht.")}
          </p>
          {data.canConfigure && (
            <div className="border-t border-white/[0.05] px-5 py-4">
              <div className="flex items-center justify-between gap-4 rounded-lg border border-white/[0.07] bg-white/[0.02] px-3 py-2.5">
                <div>
                  <p className="text-sm">{t("Operatoren nur lesend")}</p>
                  <p className="text-xs text-muted">{t("Operatoren dürfen über SFTP nur herunterladen. Wer die Einstellungen des Servers ändern darf (Admins, Eigentümer), schreibt weiter. Wirkt sofort.")}</p>
                </div>
                <Switch
                  checked={data.operatorsReadOnly}
                  onChange={async (value) => {
                    await toast.run(() => api.put(`/servers/${serverId}/sftp`, { operatorsReadOnly: value }), value ? t("Operatoren lesen nur noch") : t("Operatoren dürfen wieder schreiben"));
                    mutate();
                  }}
                />
              </div>
            </div>
          )}
        </>
      )}
    </Card>
  );
}

function CopyRow({ label, value, onCopy }: { label: string; value: string; onCopy: (label: string, value: string) => void }) {
  return (
    <div className="flex items-center gap-3 px-5 py-2.5">
      <span className="w-28 shrink-0 text-xs text-muted">{label}</span>
      <span className="min-w-0 flex-1 truncate font-mono text-sm" title={value}>
        {value}
      </span>
      <Button variant="ghost" size="icon-sm" onClick={() => onCopy(label, value)} aria-label={`${label} kopieren`}>
        <Copy className="h-3.5 w-3.5" />
      </Button>
    </div>
  );
}
