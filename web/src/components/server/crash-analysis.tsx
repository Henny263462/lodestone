"use client";

import { FileWarning } from "lucide-react";
import { useApi } from "@/lib/api";
import type { CrashAnalysis } from "@/lib/types";
import { t } from "@/lib/i18n";

/**
 * Zeigt über der Konsole, was die Absturzanalyse des Nodes im neuesten Bericht unter crash-reports/ und im Log gefunden hat.
 * Nur, wenn der Server aus ist. Ist nichts zu finden oder die Abfrage scheitert, bleibt der Platz leer: Es ist ein
 * zusätzlicher Hinweis, die Konsole selbst funktioniert ohne ihn.
 */
export function CrashReportHint({ serverId, offline }: { serverId: string; offline: boolean }) {
  const { data } = useApi<CrashAnalysis>(offline ? `/servers/${serverId}/crash-analysis` : null);
  if (!offline || !data?.found || !data.summary) return null;
  return (
    <div className="border-b border-white/[0.06] bg-white/[0.015] px-4 py-3">
      <div className="flex items-start gap-3 rounded-lg border border-bad/25 bg-bad/10 px-3 py-2.5 text-xs">
        <FileWarning className="mt-0.5 h-4 w-4 shrink-0 text-bad" />
        <div className="min-w-0 flex-1">
          <p className="font-medium text-ink">{t("Absturzanalyse")}</p>
          <p className="mt-0.5 leading-relaxed text-ink/70">{data.summary}</p>
          {data.report && <p className="mt-1 truncate font-mono text-[11px] text-muted">{data.report}</p>}
        </div>
      </div>
    </div>
  );
}
