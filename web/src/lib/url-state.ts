"use client";

import { useSearchParams } from "next/navigation";
import { useCallback, useEffect, useState } from "react";

/**
 * Zustand, der in der Adresse steht (`?tab=…`, `?section=…`), damit Neuladen, Links und die Schnellsuche an derselben
 * Stelle landen. Ändert sich die Adresse von außen (Link auf dieselbe Seite), folgt der Zustand. `reset` sind Parameter,
 * die beim Wechsel wegfallen (der Abschnitt eines anderen Tabs gilt hier nicht).
 */
export function useUrlState<T extends string>(name: string, fallback: T, reset: string[] = []): [T, (next: T) => void] {
  const fromUrl = (useSearchParams().get(name) as T | null) ?? fallback;
  const [value, setValue] = useState<T>(fromUrl);
  useEffect(() => setValue(fromUrl), [fromUrl]);
  const set = useCallback(
    (next: T) => {
      setValue(next);
      const url = new URL(window.location.href);
      url.searchParams.set(name, next);
      for (const other of reset) url.searchParams.delete(other);
      // Den Zustand von Next.js behalten, damit Zurück und Vor weiter clientseitig laufen (wie in components/layout/navigation)
      window.history.replaceState({ ...window.history.state }, "", url);
    },
    // reset ist eine feste Liste je Aufruf; als Text verglichen, damit ein neues Array keinen neuen Setter erzeugt
    // eslint-disable-next-line react-hooks/exhaustive-deps
    [name, reset.join(",")],
  );
  return [value, set];
}
