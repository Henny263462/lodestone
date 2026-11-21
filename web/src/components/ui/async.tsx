"use client";

import { AlertTriangle, Loader2, RefreshCw } from "lucide-react";
import { Button, Card } from "./primitives";
import { t } from "@/lib/i18n";

export function Loading({ label = t("Daten werden geladen") }: { label?: string }) {
  return (
    <div role="status" className="flex animate-fade-in-late items-center justify-center gap-2 py-16 text-sm text-muted">
      <Loader2 className="h-4 w-4 animate-spin" />
      {label}
    </div>
  );
}

/**
 * Fehler beim Laden. Allein steht er als eigene Karte; `inline` ist die Form innerhalb einer Karte (unter dem CardHeader),
 * damit keine Karte in einer Karte entsteht.
 */
export function ErrorState({ error, retry, inline }: { error: Error; retry?: () => void; inline?: boolean }) {
  if (inline) {
    return (
      <div className="animate-fade-in px-5 py-4">
        <div role="alert" className="flex flex-wrap items-center gap-3 rounded-lg border border-bad/25 bg-bad/10 px-4 py-3 text-xs text-bad">
          <AlertTriangle className="h-4 w-4 shrink-0" />
          <span className="min-w-0 flex-1">{error.message}</span>
          {retry && (
            <Button size="sm" onClick={retry}>
              <RefreshCw className="h-3.5 w-3.5" />
              {t("Erneut versuchen")}
            </Button>
          )}
        </div>
      </div>
    );
  }
  return (
    <Card className="flex animate-fade-in flex-col items-center gap-3 px-6 py-12 text-center">
      <span className="rounded-xl border border-bad/30 bg-bad/10 p-3 text-bad">
        <AlertTriangle className="h-5 w-5" />
      </span>
      <div>
        <p className="text-sm font-medium">{t("Daten konnten nicht geladen werden")}</p>
        <p className="mx-auto mt-1 max-w-md text-xs text-muted">{error.message}</p>
      </div>
      {retry && (
        <Button size="sm" onClick={retry}>
          <RefreshCw className="h-3.5 w-3.5" />
          {t("Erneut versuchen")}
        </Button>
      )}
    </Card>
  );
}
