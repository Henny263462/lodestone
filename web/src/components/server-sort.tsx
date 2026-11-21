"use client";

import { ArrowUpDown } from "lucide-react";
import { useCallback, useEffect, useState } from "react";
import { serverSortLabels, type ServerSort } from "@/lib/order";
import { t } from "@/lib/i18n";

const sorts = Object.keys(serverSortLabels) as ServerSort[];

/** Sortierung der Serverliste eines Projekts; der Browser merkt sie sich pro Projekt. */
export function useServerSort(projectId: string): [ServerSort, (sort: ServerSort) => void] {
  const key = `lodestone.serversort.${projectId}`;
  const [sort, setSortState] = useState<ServerSort>("default");
  useEffect(() => {
    const saved = window.localStorage.getItem(key) as ServerSort | null;
    setSortState(saved && sorts.includes(saved) ? saved : "default");
  }, [key]);
  const setSort = useCallback(
    (next: ServerSort) => {
      setSortState(next);
      window.localStorage.setItem(key, next);
    },
    [key],
  );
  return [sort, setSort];
}

export function ServerSortSelect({ value, onChange }: { value: ServerSort; onChange: (sort: ServerSort) => void }) {
  return (
    <label className="flex items-center gap-2 text-xs text-muted">
      <ArrowUpDown className="h-3.5 w-3.5" />
      <span className="sr-only">{t("Server sortieren nach")}</span>
      <select className="field w-auto py-1.5 text-xs" value={value} onChange={(event) => onChange(event.target.value as ServerSort)}>
        {sorts.map((sort) => (
          <option key={sort} value={sort}>
            {serverSortLabels()[sort]}
          </option>
        ))}
      </select>
    </label>
  );
}
