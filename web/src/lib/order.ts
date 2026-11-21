// Reine Hilfen für die eigene Ordnung: Projekte nach Favoriten und Reihenfolge, Server nach Name, Zustand oder Spielern.
import type { Project, ProjectPref, Server } from "./types";

/** Favoriten zuerst, dann die eigene Reihenfolge, dann die Reihenfolge des Panels (alphabetisch). */
export function orderProjects(projects: Project[], prefs: ProjectPref[] | undefined): Project[] {
  const byId = new Map((prefs ?? []).map((pref) => [pref.projectId, pref]));
  return projects
    .map((project, index) => ({ project, index, pref: byId.get(project.id) }))
    .sort((a, b) => {
      const favorite = Number(!!b.pref?.favorite) - Number(!!a.pref?.favorite);
      if (favorite !== 0) return favorite;
      // Projekte ohne eigene Position (0) kommen nach den geordneten
      const pa = a.pref?.position || Number.MAX_SAFE_INTEGER;
      const pb = b.pref?.position || Number.MAX_SAFE_INTEGER;
      if (pa !== pb) return pa - pb;
      return a.index - b.index;
    })
    .map((entry) => entry.project);
}

/** Alle Schlagwörter, alphabetisch und ohne Doppelte (Groß/Klein egal). */
export function allTags(prefs: ProjectPref[] | undefined): string[] {
  const seen = new Map<string, string>();
  for (const pref of prefs ?? []) for (const tag of pref.tags) if (!seen.has(tag.toLowerCase())) seen.set(tag.toLowerCase(), tag);
  return [...seen.values()].sort((a, b) => a.localeCompare(b, "de"));
}

/** Filter der Übersicht: alle, nur Favoriten oder ein Schlagwort. */
export type ProjectFilter = { kind: "all" } | { kind: "favorites" } | { kind: "tag"; tag: string };

export function filterProjects(projects: Project[], prefs: ProjectPref[] | undefined, filter: ProjectFilter): Project[] {
  if (filter.kind === "all") return projects;
  const byId = new Map((prefs ?? []).map((pref) => [pref.projectId, pref]));
  if (filter.kind === "favorites") return projects.filter((project) => byId.get(project.id)?.favorite);
  const wanted = filter.tag.toLowerCase();
  return projects.filter((project) => byId.get(project.id)?.tags.some((tag) => tag.toLowerCase() === wanted));
}

export type ServerSort = "default" | "name" | "state" | "players";

export const serverSortLabels: Record<ServerSort, string> = {
  default: "Standard",
  name: "Name",
  state: "Zustand (laufende zuerst)",
  players: "Spieler (meiste zuerst)",
};

const stateOrder: Record<string, number> = { running: 0, starting: 1, stopping: 2, restoring: 3, installing: 4, offline: 5 };

/** Sortiert Server einer Liste. players sind die Spielerzahlen der letzten Messung (Server-ID → Zahl). */
export function sortServers(servers: Server[], sort: ServerSort, players?: Record<string, number>): Server[] {
  const byName = (a: Server, b: Server) => a.name.localeCompare(b.name, "de", { numeric: true, sensitivity: "base" });
  const list = [...servers];
  switch (sort) {
    case "name":
      return list.sort(byName);
    case "state":
      return list.sort((a, b) => {
        // Ein Node, der nicht antwortet, kommt ans Ende
        const sa = a.online ? (stateOrder[a.view?.state ?? "offline"] ?? 6) : 7;
        const sb = b.online ? (stateOrder[b.view?.state ?? "offline"] ?? 6) : 7;
        return sa - sb || byName(a, b);
      });
    case "players":
      return list.sort((a, b) => (players?.[b.id] ?? 0) - (players?.[a.id] ?? 0) || byName(a, b));
    default:
      return list;
  }
}
