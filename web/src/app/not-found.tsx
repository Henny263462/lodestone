"use client";

import Link from "next/link";
import { t } from "@/lib/i18n";
import { LodestoneLogo } from "@/components/ui/mc";

export default function NotFound() {
  return (
    <main className="grid min-h-screen place-items-center px-4 text-center">
      <div className="flex flex-col items-center">
        <LodestoneLogo size={48} />
        <p className="mt-5 text-sm text-muted">404</p>
        <h1 className="mt-1 font-display text-2xl tracking-wide text-pixel-shadow">{t("Seite nicht gefunden")}</h1>
        <p className="mt-2 text-sm text-muted">{t("Die Adresse gibt es nicht (mehr). Prüfe den Link oder geh zurück zur Übersicht.")}</p>
        <Link href="/" className="mt-5 inline-block text-sm text-accent hover:underline">
          {t("Zur Übersicht")}
        </Link>
      </div>
    </main>
  );
}
