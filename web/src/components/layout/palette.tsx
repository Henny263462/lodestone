"use client";

import { useRouter } from "next/navigation";
import { useEffect, useMemo, useRef, useState } from "react";
import { createPortal } from "react-dom";
import {
  Archive,
  Bell,
  CalendarClock,
  Cpu,
  FolderOpen,
  FolderPlus,
  Globe,
  HardDriveUpload,
  KeyRound,
  LayoutDashboard,
  Network,
  Plus,
  Puzzle,
  ScrollText,
  Search,
  Server,
  Settings,
  ShieldCheck,
  SlidersHorizontal,
  Terminal,
  UserCog,
  Users,
  type LucideIcon,
} from "lucide-react";
import { cx, usePresence } from "@/components/ui/primitives";
import { can, type Permission, type Project, type User } from "@/lib/types";

interface Entry {
  key: string;
  label: string;
  hint: string;
  href: string;
  icon: LucideIcon;
  /** Unterziele (Bereiche eines Servers, Tabs eines Projekts) erscheinen erst, wenn jemand etwas eintippt. */
  deep?: boolean;
}

const MAX_SHOWN = 20;

/** Ziele innerhalb eines Servers, nach denselben Rechten wie die Serverseite. */
function serverTargets(project: Project, server: Project["servers"][number]): Entry[] {
  const allowed = (...needed: Permission[]) => can(project.myPermissions, ...needed);
  const base = `/server/?id=${server.id}`;
  const hint = `${server.name} · Server in ${project.name}`;
  const worlds = server.role !== "proxy" && server.software !== "velocity";
  const noun = server.software === "fabric" || server.software === "neoforge" || server.software === "forge" ? "Mods" : server.software === "vanilla" ? null : "Plugins";
  const list: [boolean, string, string, LucideIcon][] = [
    [allowed("console.read"), "console", "Konsole", Terminal],
    [allowed("files.read"), "files", "Dateien", FolderOpen],
    [allowed("sftp", "files.read"), "files", "SFTP-Zugang", HardDriveUpload],
    [!!noun, "addons", noun ?? "", Puzzle],
    [true, "players", "Spieler", Users],
    [allowed("backups.read"), "backups", "Backups", Archive],
    [allowed("files.read") && worlds, "worlds", "Welten", Globe],
    [allowed("files.read") && worlds, "properties", "Spieleinstellungen", SlidersHorizontal],
    [allowed("settings") || allowed("servers"), "settings", "Einstellungen", Settings],
  ];
  return list
    .filter(([ok]) => ok)
    .map(([, tab, label, icon]) => ({ key: `s-${server.id}-${label}`, label, hint, href: `${base}&tab=${tab}`, icon, deep: true }));
}

function projectTargets(project: Project): Entry[] {
  const base = `/project/?id=${project.id}`;
  const hint = `${project.kind === "network" ? "Netzwerk" : "Projekt"} ${project.name}`;
  const list: [boolean, string, string, LucideIcon][] = [
    [project.kind === "network", "network", "Netzwerk", Network],
    [true, "schedules", "Zeitpläne", CalendarClock],
    [true, "events", "Ereignisse", Bell],
    [true, "members", "Mitglieder", Users],
    [can(project.myPermissions, "settings"), "settings", "Einstellungen", Settings],
  ];
  return list.filter(([ok]) => ok).map(([, tab, label, icon]) => ({ key: `p-${project.id}-${tab}`, label, hint, href: `${base}&tab=${tab}`, icon, deep: true }));
}

/** Strg+K: springt zu Projekten, Servern, ihren Bereichen, Seiten und häufigen Aktionen. */
export function CommandPalette({ projects, user, open, onClose }: { projects: Project[] | undefined; user: User; open: boolean; onClose: () => void }) {
  const router = useRouter();
  const [query, setQuery] = useState("");
  const [index, setIndex] = useState(0);
  const input = useRef<HTMLInputElement>(null);
  const { mounted, leaving } = usePresence(open, 120);

  const entries = useMemo<Entry[]>(() => {
    const list: Entry[] = [{ key: "home", label: "Übersicht", hint: "Seite", href: "/", icon: LayoutDashboard }];
    const deep: Entry[] = [];
    for (const project of projects ?? []) {
      list.push({ key: `p-${project.id}`, label: project.name, hint: project.kind === "network" ? "Netzwerk" : "Projekt", href: `/project/?id=${project.id}`, icon: project.kind === "network" ? Network : Server });
      deep.push(...projectTargets(project));
      for (const server of project.servers) {
        list.push({ key: `s-${server.id}`, label: server.name, hint: `Server in ${project.name}`, href: `/server/?id=${server.id}`, icon: Server });
        deep.push(...serverTargets(project, server));
      }
    }
    list.push({ key: "nodes", label: "Nodes", hint: "Seite", href: "/nodes/", icon: Cpu });
    if (user.isAdmin) {
      list.push({ key: "users", label: "Benutzer", hint: "Seite", href: "/users/", icon: Users });
      list.push({ key: "audit", label: "Protokoll", hint: "Seite", href: "/audit/", icon: ScrollText });
    }
    list.push({ key: "account", label: "Konto", hint: "Seite", href: "/account/", icon: UserCog });
    list.push({ key: "new-project", label: "Neues Projekt", hint: "Aktion", href: "/?new=1", icon: FolderPlus });
    if (user.isAdmin) list.push({ key: "add-node", label: "Node hinzufügen", hint: "Aktion", href: "/nodes/?add=1", icon: Plus });
    const account: [string, string, LucideIcon][] = [
      ["password", "Passwort ändern", KeyRound],
      ["totp", "Zwei-Faktor-Anmeldung", ShieldCheck],
      ["passkeys", "Passkeys", ShieldCheck],
      ["oidc", "Anmeldung über OIDC", ShieldCheck],
      ["keys", "API-Schlüssel", KeyRound],
      ["sftp", "SFTP-Passwort und SSH-Schlüssel", HardDriveUpload],
      ["sessions", "Angemeldete Geräte", UserCog],
    ];
    for (const [section, label, icon] of account) deep.push({ key: `account-${section}`, label, hint: "Konto", href: `/account/?section=${section}`, icon, deep: true });
    return [...list, ...deep];
  }, [projects, user.isAdmin]);

  const shown = useMemo(() => {
    const words = query.toLowerCase().split(/\s+/).filter(Boolean);
    const found = entries.filter((entry) => (words.length > 0 || !entry.deep) && words.every((word) => `${entry.label} ${entry.hint}`.toLowerCase().includes(word)));
    // Treffer im Namen (oder im Servernamen vor „ · “) zuerst: „survival backups“ zeigt die Backups von Survival vor denen
    // anderer Server im Projekt „Survival-Netz“
    const score = (entry: Entry) => {
      const primary = `${entry.label} ${entry.hint.split(" · ")[0]}`.toLowerCase();
      return words.filter((word) => primary.includes(word)).length;
    };
    return found
      .map((entry, position) => ({ entry, position, score: score(entry) }))
      .sort((a, b) => b.score - a.score || a.position - b.position)
      .map(({ entry }) => entry)
      .slice(0, MAX_SHOWN);
  }, [entries, query]);

  useEffect(() => {
    if (open) {
      setQuery("");
      setIndex(0);
      setTimeout(() => input.current?.focus(), 0);
    }
  }, [open]);
  useEffect(() => setIndex(0), [query]);

  if (!mounted || typeof document === "undefined") return null;

  function go(entry: Entry | undefined) {
    if (!entry) return;
    onClose();
    router.push(entry.href);
  }

  return createPortal(
    <div
      className={cx("fixed inset-0 z-[70] grid place-items-start bg-black/60 px-4 pt-[15vh] backdrop-blur-sm", leaving ? "pointer-events-none animate-overlay-out" : "animate-overlay-in")}
      onMouseDown={onClose}
    >
      <div
        className={cx("mx-auto w-full max-w-lg overflow-hidden rounded-xl border border-white/[0.1] bg-surface shadow-2xl", leaving ? "animate-pop-out" : "animate-pop-in")}
        role="dialog"
        aria-label="Schnellsuche"
        onMouseDown={(event) => event.stopPropagation()}
      >
        <div className="flex items-center gap-3 border-b border-white/[0.07] px-4">
          <Search className="h-4 w-4 text-muted" />
          <input
            ref={input}
            value={query}
            onChange={(event) => setQuery(event.target.value)}
            onKeyDown={(event) => {
              if (event.key === "ArrowDown") {
                event.preventDefault();
                setIndex((value) => Math.min(shown.length - 1, value + 1));
              } else if (event.key === "ArrowUp") {
                event.preventDefault();
                setIndex((value) => Math.max(0, value - 1));
              } else if (event.key === "Enter") {
                event.preventDefault();
                go(shown[index]);
              } else if (event.key === "Escape") {
                onClose();
              }
            }}
            placeholder="Projekt, Server, Bereich oder Seite suchen"
            aria-label="Suchbegriff"
            className="h-12 min-w-0 flex-1 bg-transparent text-sm outline-none placeholder:text-muted"
          />
          <kbd className="hidden rounded border border-white/[0.1] px-1.5 py-0.5 text-[10px] text-muted sm:inline">Esc</kbd>
        </div>
        <div className="max-h-80 overflow-y-auto p-1.5">
          {shown.length === 0 && <p className="px-3 py-6 text-center text-sm text-muted">Nichts gefunden. Versuche einen Teil des Namens, z. B. „Backups“ oder den Servernamen.</p>}
          {shown.map((entry, position) => (
            <button
              key={entry.key}
              onMouseMove={() => setIndex(position)}
              onClick={() => go(entry)}
              className={cx("flex w-full items-center gap-3 rounded-lg px-3 py-2 text-left text-sm transition-colors duration-75", position === index ? "bg-white/[0.08] text-ink" : "text-muted")}
            >
              <entry.icon className="h-4 w-4 shrink-0" />
              <span className="min-w-0 flex-1 truncate">{entry.label}</span>
              <span className="min-w-0 max-w-[50%] shrink truncate text-xs text-muted/80">{entry.hint}</span>
            </button>
          ))}
        </div>
        {query === "" && <p className="border-t border-white/[0.07] px-4 py-2 text-[11px] text-muted">Tipp: Servername und „Backups“ springt direkt zu den Backups dieses Servers.</p>}
      </div>
    </div>,
    document.body,
  );
}
