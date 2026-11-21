"use client";

import { Languages } from "lucide-react";
import { useState } from "react";
import { Card, CardHeader, Select } from "@/components/ui/primitives";
import { useToast } from "@/components/ui/feedback";
import { ErrorState, Loading } from "@/components/ui/async";
import { api, useApi } from "@/lib/api";
import { browserLanguage, languageNames, t, tFor, type Language, type LanguageChoice } from "@/lib/i18n";
import { useLanguage } from "@/lib/i18n-provider";
import type { Me } from "@/lib/types";

const choices: Language[] = ["de", "en"];

/** LanguageCard ist die Sprachwahl im Konto. Sie gilt für den Benutzer auf allen Geräten. */
export function LanguageCard() {
  const toast = useToast();
  const { setLanguage } = useLanguage();
  const { data, error, mutate } = useApi<Me>("/me");
  const [saving, setSaving] = useState(false);

  async function choose(choice: LanguageChoice) {
    const target = choice || browserLanguage();
    setSaving(true);
    // Erst speichern, dann umschalten: Die Shell übernimmt nach dem Neuaufbau die Sprache aus dem Konto.
    const ok = await toast.run(() => api.put("/me/language", { language: choice }));
    setSaving(false);
    if (!ok) return;
    await mutate({ ...data!, language: choice }, { revalidate: false });
    await setLanguage(choice).catch(() => undefined);
    toast.success(tFor(target, "Sprache gespeichert"));
  }

  return (
    <Card>
      <CardHeader title={t("Sprache")} description={t("In welcher Sprache Lodestone für dich erscheint, auf allen Geräten.")} />
      {error ? (
        <ErrorState inline error={error} retry={() => mutate()} />
      ) : !data ? (
        <Loading />
      ) : (
        <div className="space-y-2 px-5 py-4">
          <p className="label">{t("Sprache der Oberfläche")}</p>
          <Select
            className="sm:max-w-xs"
            value={data.language ?? ""}
            disabled={saving}
            onChange={(value) => choose(value as LanguageChoice)}
            aria-label={t("Sprache der Oberfläche")}
            options={[
              { value: "", label: t("Wie der Browser ({language})", { language: languageNames[browserLanguage()] }) },
              ...choices.map((choice) => ({ value: choice, label: languageNames[choice] })),
            ]}
          />
          <p className="text-xs text-muted">{t("Vor der Anmeldung und auf öffentlichen Statusseiten gilt die Sprache des Browsers.")}</p>
        </div>
      )}
    </Card>
  );
}

/** LanguageSwitch wählt die Sprache vor der Anmeldung; sie gilt auf diesem Gerät, bis das Konto eine andere vorgibt. */
export function LanguageSwitch() {
  const { language, setLanguage } = useLanguage();
  return (
    <div className="flex items-center gap-2 text-xs text-muted">
      <Languages className="h-3.5 w-3.5" />
      <Select
        size="sm"
        className="w-32"
        value={language}
        onChange={(value) => void setLanguage(value as Language)}
        aria-label={t("Sprache")}
        options={choices.map((choice) => ({ value: choice, label: languageNames[choice] }))}
      />
    </div>
  );
}
