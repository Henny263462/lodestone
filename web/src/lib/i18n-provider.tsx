"use client";

import { Fragment, createContext, useCallback, useContext, useEffect, useMemo, useState, type ReactNode } from "react";
import { browserLanguage, detectLanguage, isLanguage, loadLanguage, rememberLanguage, setActiveLanguage, type Language, type LanguageChoice } from "@/lib/i18n";

// Provider und Hooks zur Sprache. Die Übersetzung selbst (t, tp, …) steht in lib/i18n.ts und braucht kein React.

type LanguageContextValue = {
  language: Language;
  /** setLanguage wechselt die Sprache auf diesem Gerät ("" heißt „wie der Browser“), sobald ihr Wörterbuch geladen ist. */
  setLanguage: (choice: LanguageChoice) => Promise<void>;
};

const LanguageContext = createContext<LanguageContextValue>({ language: "de", setLanguage: async () => undefined });

/**
 * I18nProvider hält die Sprache. Beim Bauen und beim ersten Rendern im Browser ist sie Deutsch (wie das vorgerenderte HTML);
 * danach gilt die gespeicherte Wahl bzw. die des Browsers. Den Neuaufbau bei einem Wechsel macht `LanguageRoot`.
 */
export function I18nProvider({ children }: { children: ReactNode }) {
  const [active, setActive] = useState<Language>("de");
  setActiveLanguage(active);

  useEffect(() => {
    const target = detectLanguage();
    loadLanguage(target).then(
      () => setActive(target),
      () => undefined, // Wörterbuch nicht ladbar (z. B. offline): Es bleibt Deutsch.
    );
  }, []);

  useEffect(() => {
    document.documentElement.lang = active;
  }, [active]);

  const setLanguage = useCallback(async (choice: LanguageChoice) => {
    rememberLanguage(choice);
    const target = choice || browserLanguage();
    await loadLanguage(target);
    setActive(target);
  }, []);

  const value = useMemo(() => ({ language: active, setLanguage }), [active, setLanguage]);
  return <LanguageContext.Provider value={value}>{children}</LanguageContext.Provider>;
}

/**
 * LanguageRoot baut den Baum darunter bei einem Sprachwechsel neu auf, damit jeder Text die neue Sprache bekommt, auch solche
 * aus reinen Hilfen. Er steht unter dem Toast-Provider, damit eine Meldung den Wechsel überlebt.
 */
export function LanguageRoot({ children }: { children: ReactNode }) {
  const { language: active } = useLanguage();
  return <Fragment key={active}>{children}</Fragment>;
}

/** useLanguage liefert die Sprache und den Umschalter. */
export function useLanguage(): LanguageContextValue {
  return useContext(LanguageContext);
}

/** useUserLanguage übernimmt die im Konto gespeicherte Sprache, sobald der Benutzer geladen ist ("" ändert nichts). */
export function useUserLanguage(choice: LanguageChoice | undefined) {
  const { language: active, setLanguage } = useLanguage();
  useEffect(() => {
    if (isLanguage(choice) && choice !== active) setLanguage(choice).catch(() => undefined);
  }, [choice, active, setLanguage]);
}
