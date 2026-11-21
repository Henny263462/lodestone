"use client";

import Link from "next/link";
import { usePathname, useSearchParams } from "next/navigation";
import { Suspense, createContext, useContext, useEffect, useState, type ReactNode } from "react";
import {
  ArrowUpCircle,
  Cable,
  Cpu,
  FolderKanban,
  LayoutDashboard,
  LayoutGrid,
  LogOut,
  Menu,
  Package,
  PanelLeftClose,
  PanelLeftOpen,
  ScrollText,
  Search,
  Users,
  X,
  type LucideIcon,
} from "lucide-react";
import { Dot, cx, usePresence } from "@/components/ui/primitives";
import { BlockIcon, LodestoneLogo, PixelAvatar, Wordmark } from "@/components/ui/mc";
import { Loading } from "@/components/ui/async";
import { CommandPalette } from "./palette";
import { api, prefetch, useApi } from "@/lib/api";
import { stateTone } from "@/lib/format";
import { usePoll, useLiveUpdates } from "@/lib/live";
import { orderProjects } from "@/lib/order";
import type { AuthStatus, Project, ProjectPref, Server, UpdateStatus, User } from "@/lib/types";
import { t, tp, type LanguageChoice } from "@/lib/i18n";
import { useUserLanguage } from "@/lib/i18n-provider";

const SessionContext = createContext<User | null>(null);
export const useUser = () => useContext(SessionContext) as User;

// ---------------------------------------------------------------- Anmeldung

/** Prüft die Anmeldung und zeigt erst dann die Oberfläche. */
export function AuthGate({ children }: { children: ReactNode }) {
  const [user, setUser] = useState<User | null>(null);
  const [language, setLanguage] = useState<LanguageChoice>();
  useUserLanguage(language);

  useEffect(() => {
    // Die Projektliste braucht die Shell sofort; sie lädt parallel zur Anmeldeprüfung statt danach
    prefetch("/projects");
    api
      .get<AuthStatus>("/auth/status")
      .then((status) => {
        if (status.setupRequired || !status.user) {
          const next = encodeURIComponent(window.location.pathname + window.location.search);
          window.location.replace(`/login/?next=${next}`);
          return;
        }
        setLanguage(status.language);
        setUser(status.user);
      })
      .catch(() => window.location.replace("/login/"));
  }, []);

  if (!user) {
    return (
      <div className="grid min-h-screen place-items-center">
        <Loading label={t("Anmeldung wird geprüft")} />
      </div>
    );
  }
  return (
    <SessionContext.Provider value={user}>
      <Suspense fallback={null}>
        <Shell user={user}>{children}</Shell>
      </Suspense>
    </SessionContext.Provider>
  );
}

// ---------------------------------------------------------------- Navigation

function normalize(path: string): string {
  const p = path.split("?")[0].replace(/\/+$/, "");
  return p ? p + "/" : "/";
}

const FOLD_KEY = "lodestone.sidebar.folded";
// So viele Projekte zeigt die Leiste, bevor sie „Alle anzeigen“ anbietet
const PROJECT_LIMIT = 8;

interface NavEntry {
  href: string;
  label: string;
  icon: LucideIcon;
  active: (path: string) => boolean;
}

// Listen mit Texten sind Funktionen: Auf Modulebene steht die Sprache noch nicht fest
const mainEntries = (): NavEntry[] => [
  { href: "/", label: t("Übersicht"), icon: LayoutGrid, active: (p) => p === "/" },
  { href: "/nodes/", label: t("Nodes"), icon: Cpu, active: (p) => p.startsWith("/nodes/") },
];

// Die Verwaltung steht für Administratoren direkt in der Leiste, nicht versteckt hinter einem Menü
const adminEntries = (): NavEntry[] => [
  { href: "/admin/", label: t("System"), icon: LayoutDashboard, active: (p) => p === "/admin/" },
  { href: "/users/", label: t("Benutzer"), icon: Users, active: (p) => p.startsWith("/users/") },
  { href: "/admin/projects/", label: t("Alle Projekte"), icon: FolderKanban, active: (p) => p.startsWith("/admin/projects/") },
  { href: "/admin/packs/", label: t("Position-Packs"), icon: Package, active: (p) => p.startsWith("/admin/packs/") },
  { href: "/admin/playit/", label: "playit.gg", icon: Cable, active: (p) => p.startsWith("/admin/playit/") },
  { href: "/admin/updates/", label: t("Updates"), icon: ArrowUpCircle, active: (p) => p.startsWith("/admin/updates/") },
  { href: "/audit/", label: t("Protokoll"), icon: ScrollText, active: (p) => p.startsWith("/audit/") },
];

/** Projekt- und Server-ID aus sauberen Pfaden (/project/<id>/…) oder alten Suchparametern (?id=…). */
function currentIds(pathname: string, params: URLSearchParams): { projectId: string | null; serverId: string | null } {
  const [section, id] = pathname.split("/").filter(Boolean);
  const pick = (name: string) => (section === name ? id || params.get("id") : null);
  return { projectId: pick("project"), serverId: pick("server") };
}

function NavItem({ href, label, icon: Icon, active, compact, trailing }: { href: string; label: string; icon: LucideIcon; active: boolean; compact: boolean; trailing?: ReactNode }) {
  return (
    <Link
      href={href}
      title={compact ? label : undefined}
      aria-current={active ? "page" : undefined}
      className={cx(
        "relative flex items-center rounded-md text-[13px] transition duration-100",
        compact ? "justify-center py-2.5" : "gap-3 px-3 py-2",
        active ? "bg-white/[0.08] text-ink" : "text-muted hover:bg-white/[0.04] hover:text-ink",
      )}
    >
      {active && <span aria-hidden className="absolute inset-y-1.5 left-0 w-[3px] bg-accent" />}
      <Icon className={cx("h-4 w-4 shrink-0", active ? "text-accent" : "text-muted")} />
      {!compact && <span className="min-w-0 flex-1 truncate">{label}</span>}
      {!compact && trailing}
    </Link>
  );
}

function SectionTitle({ children, compact, action }: { children: ReactNode; compact: boolean; action?: ReactNode }) {
  if (compact) return <div className="mx-3 my-3 h-px bg-white/[0.07]" />;
  return (
    <div className="mb-1 mt-5 flex items-center justify-between px-3">
      <p className="font-display text-[11px] uppercase tracking-[0.12em] text-muted/80">{children}</p>
      {action}
    </div>
  );
}

function serverTone(server: Server) {
  if (!server.online) return "bad" as const;
  return stateTone(server.view?.state);
}

function ProjectItem({ project, active, activeServerId, compact }: { project: Project; active: boolean; activeServerId: string | null; compact: boolean }) {
  const running = project.servers.filter((server) => server.view?.state === "running").length;
  return (
    <div>
      <Link
        href={`/project/${project.id}/`}
        title={compact ? project.name : undefined}
        aria-current={active && !activeServerId ? "page" : undefined}
        className={cx(
          "relative flex items-center rounded-md text-[13px] transition duration-100",
          compact ? "justify-center py-2" : "gap-2.5 px-3 py-1.5",
          active ? "bg-white/[0.08] text-ink" : "text-muted hover:bg-white/[0.04] hover:text-ink",
        )}
      >
        {active && !activeServerId && <span aria-hidden className="absolute inset-y-1.5 left-0 w-[3px] bg-accent" />}
        <BlockIcon name={project.name} size={18} />
        {!compact && (
          <>
            <span className="min-w-0 flex-1 truncate">{project.name}</span>
            {project.servers.length > 0 && (
              <span className="text-[10.5px] tabular-nums text-muted" title={tp("serversRunningOf", running, { total: project.servers.length })}>
                {running}/{project.servers.length}
              </span>
            )}
          </>
        )}
      </Link>
      {/* Das aktive Projekt klappt seine Server auf, damit man ohne Umweg zwischen ihnen wechselt */}
      {active && !compact && project.servers.length > 0 && (
        <div className="mb-1 ml-[21px] mt-0.5 space-y-px border-l border-white/[0.08] pl-2">
          {project.servers.map((server) => {
            const selected = server.id === activeServerId;
            return (
              <Link
                key={server.id}
                href={`/server/${server.id}/`}
                aria-current={selected ? "page" : undefined}
                className={cx(
                  "flex items-center gap-2 rounded-md px-2 py-1.5 text-[12.5px] transition duration-100",
                  selected ? "bg-white/[0.08] text-ink" : "text-muted hover:bg-white/[0.04] hover:text-ink",
                )}
              >
                <Dot tone={serverTone(server)} />
                <span className="min-w-0 flex-1 truncate">{server.name}</span>
                {server.role === "proxy" && <span className="text-[10px] text-muted">Proxy</span>}
              </Link>
            );
          })}
        </div>
      )}
    </div>
  );
}

// ---------------------------------------------------------------- Shell

function Shell({ user, children }: { user: User; children: ReactNode }) {
  const pathname = normalize(usePathname() ?? "/");
  const params = useSearchParams();
  const [open, setOpen] = useState(false);
  const drawer = usePresence(open, 160);
  const [folded, setFolded] = useState(false);
  const [palette, setPalette] = useState(false);
  const [allProjects, setAllProjects] = useState(false);

  // Ein Kanal für die ganze Oberfläche: Änderungen im Panel laden die betroffenen Abfragen neu
  useLiveUpdates();
  const { data: projects } = useApi<Project[]>("/projects", { refreshInterval: usePoll(120000, 15000) });
  // Eigene Ordnung: angeheftete Projekte oben, dann die selbst gewählte Reihenfolge
  const { data: prefs } = useApi<ProjectPref[]>("/me/project-prefs");
  const { data: updates } = useApi<UpdateStatus>(user.isAdmin ? "/updates" : null, { refreshInterval: 300000 });

  useEffect(() => setOpen(false), [pathname, params]);
  useEffect(() => {
    try {
      setFolded(window.localStorage.getItem(FOLD_KEY) === "1");
    } catch {}
  }, []);

  // Bei einem Seitenwechsel beginnt der Inhalt wieder oben
  useEffect(() => {
    window.scrollTo({ top: 0 });
  }, [pathname]);

  function toggleFold() {
    setFolded((value) => {
      try {
        window.localStorage.setItem(FOLD_KEY, value ? "0" : "1");
      } catch {}
      return !value;
    });
  }

  // Strg+B klappt die Seitenleiste ein und aus, Strg+K öffnet die Schnellsuche
  useEffect(() => {
    function onKey(event: KeyboardEvent) {
      if ((event.ctrlKey || event.metaKey) && event.key.toLowerCase() === "k") {
        event.preventDefault();
        setPalette((value) => !value);
      }
      if ((event.ctrlKey || event.metaKey) && event.key.toLowerCase() === "b") {
        event.preventDefault();
        toggleFold();
      }
    }
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, []);

  async function logout() {
    await api.post("/auth/logout").catch(() => null);
    window.location.replace("/login/");
  }

  const { projectId, serverId } = currentIds(pathname, params);
  const list = orderProjects(projects ?? [], prefs);
  const activeProjectId = projectId ?? list.find((project) => project.servers.some((server) => server.id === serverId))?.id ?? null;
  // Das aktive Projekt bleibt sichtbar, auch wenn es hinter der Grenze läge
  const shown = allProjects ? list : list.filter((project, index) => index < PROJECT_LIMIT || project.id === activeProjectId);

  function sidebar(compact: boolean) {
    return (
      <div className="flex h-full flex-col">
        <div className={cx("flex items-center pb-3 pt-4", compact ? "justify-center px-2" : "gap-2.5 px-4")}>
          <Link href="/" className="flex min-w-0 flex-1 items-center gap-2.5" title={t("Zur Übersicht")}>
            <LodestoneLogo size={compact ? 30 : 28} />
            {!compact && <Wordmark className="truncate text-[15px]" />}
          </Link>
          {!compact && (
            <button onClick={() => setOpen(false)} className="rounded-md p-1.5 text-muted hover:bg-white/[0.06] hover:text-ink lg:hidden" aria-label={t("Menü schließen")}>
              <X className="h-4 w-4" />
            </button>
          )}
        </div>

        <div className={cx("pb-2", compact ? "px-2" : "px-3")}>
          <button
            onClick={() => setPalette(true)}
            title={compact ? t("Suchen (Strg K)") : undefined}
            aria-label={t("Suchen")}
            className={cx(
              "flex w-full items-center rounded-md border border-white/[0.08] bg-black/25 text-[13px] text-muted shadow-sunken transition hover:border-white/[0.14] hover:text-ink",
              compact ? "justify-center py-2.5" : "gap-3 px-3 py-2",
            )}
          >
            <Search className="h-4 w-4 shrink-0" />
            {!compact && (
              <>
                <span className="flex-1 text-left">{t("Suchen")}</span>
                <kbd className="rounded-sm border border-white/[0.1] px-1.5 font-sans text-[10px]">{t("Strg K")}</kbd>
              </>
            )}
          </button>
        </div>

        <nav aria-label={t("Hauptnavigation")} className={cx("no-scrollbar flex-1 overflow-y-auto pb-4", compact ? "px-2" : "px-3")}>
          <div className="space-y-px">
            {mainEntries().map((entry) => (
              <NavItem key={entry.href} href={entry.href} label={entry.label} icon={entry.icon} active={entry.active(pathname)} compact={compact} />
            ))}
          </div>

          <SectionTitle compact={compact}>{t("Projekte")}</SectionTitle>
          <div className="space-y-px">
            {shown.map((project) => (
              <ProjectItem key={project.id} project={project} active={project.id === activeProjectId} activeServerId={serverId} compact={compact} />
            ))}
            {projects && list.length === 0 && !compact && (
              <Link href="/" className="block px-3 py-1.5 text-xs text-muted hover:text-ink">
                {t("Noch keine Projekte. Lege eins auf der Übersicht an.")}
              </Link>
            )}
            {!compact && list.length > PROJECT_LIMIT && (
              <button
                type="button"
                onClick={() => setAllProjects((value) => !value)}
                className="w-full rounded-md px-3 py-1.5 text-left text-xs text-muted transition hover:bg-white/[0.04] hover:text-ink"
              >
                {allProjects ? t("Weniger anzeigen") : t("Alle {count} anzeigen", { count: list.length })}
              </button>
            )}
          </div>

          {user.isAdmin && (
            <>
              <SectionTitle compact={compact}>{t("Verwaltung")}</SectionTitle>
              <div className="space-y-px">
                {adminEntries().map((entry) => (
                  <NavItem
                    key={entry.href}
                    href={entry.href}
                    label={entry.label}
                    icon={entry.icon}
                    active={entry.active(pathname)}
                    compact={compact}
                    trailing={
                      entry.href === "/admin/updates/" && updates?.updateAvailable ? (
                        <span className="rounded-sm bg-warn/15 px-1.5 text-[10px] font-medium text-warn">{t("Neu")}</span>
                      ) : undefined
                    }
                  />
                ))}
              </div>
            </>
          )}
        </nav>

        <div className={cx("border-t border-white/[0.07]", compact ? "space-y-1 p-2" : "p-3")}>
          <div className={cx("flex items-center", compact ? "flex-col gap-1" : "gap-1")}>
            <Link
              href="/account/"
              title={compact ? `${user.username} · ${t("Konto und Darstellung")}` : t("Konto und Darstellung")}
              aria-current={pathname === "/account/" ? "page" : undefined}
              className={cx(
                "flex min-w-0 flex-1 items-center rounded-md transition hover:bg-white/[0.05]",
                compact ? "justify-center p-1.5" : "gap-2.5 p-1.5",
                pathname === "/account/" && "bg-white/[0.08]",
              )}
            >
              <PixelAvatar name={user.username} size={30} />
              {!compact && (
                <span className="min-w-0 flex-1">
                  <span className="block truncate text-[13px] font-medium leading-snug">{user.username}</span>
                  <span className="block text-[11px] leading-tight text-muted">{user.isAdmin ? t("Administrator") : t("Konto und Darstellung")}</span>
                </span>
              )}
            </Link>
            <button onClick={logout} className="rounded-md p-2 text-muted transition hover:bg-white/[0.06] hover:text-ink" title={t("Abmelden")} aria-label={t("Abmelden")}>
              <LogOut className="h-4 w-4" />
            </button>
          </div>
          <button
            onClick={toggleFold}
            className={cx(
              "mt-1 hidden w-full items-center rounded-md py-1.5 text-xs text-muted transition hover:bg-white/[0.05] hover:text-ink lg:flex",
              compact ? "justify-center" : "gap-3 px-3",
            )}
            title={folded ? t("Seitenleiste ausklappen (Strg B)") : t("Seitenleiste einklappen (Strg B)")}
            aria-label={folded ? t("Seitenleiste ausklappen") : t("Seitenleiste einklappen")}
          >
            {folded ? <PanelLeftOpen className="h-4 w-4" /> : <PanelLeftClose className="h-4 w-4" />}
            {!compact && <span>{t("Einklappen")}</span>}
          </button>
        </div>
      </div>
    );
  }

  return (
    <>
      <CommandPalette projects={projects} user={user} open={palette} onClose={() => setPalette(false)} />
      <aside
        className={cx(
          "fixed inset-y-0 left-0 z-30 hidden border-r border-black/50 bg-black/25 transition-[width] duration-200 ease-snappy lg:block",
          folded ? "w-[68px]" : "w-[248px]",
        )}
      >
        {sidebar(folded)}
      </aside>

      {drawer.mounted && (
        <div className={cx("fixed inset-0 z-40 lg:hidden", drawer.leaving && "pointer-events-none")}>
          <div className={cx("absolute inset-0 bg-black/70 backdrop-blur-sm", drawer.leaving ? "animate-overlay-out" : "animate-overlay-in")} onClick={() => setOpen(false)} />
          <aside className={cx("relative h-full w-[272px] border-r border-black/50 bg-bg", drawer.leaving ? "animate-drawer-out" : "animate-drawer-in")}>{sidebar(false)}</aside>
        </div>
      )}

      <div className={cx("min-h-screen transition-[padding] duration-200 ease-snappy", folded ? "lg:pl-[68px]" : "lg:pl-[248px]")}>
        <header className="sticky top-0 z-20 flex h-12 items-center gap-3 border-b border-black/50 bg-bg/95 px-4 backdrop-blur-xl lg:hidden">
          <button onClick={() => setOpen(true)} className="rounded-md border border-white/[0.08] p-1.5 text-muted hover:bg-white/[0.06] hover:text-ink" aria-label={t("Menü öffnen")}>
            <Menu className="h-4 w-4" />
          </button>
          <Link href="/" className="flex min-w-0 items-center gap-2">
            <LodestoneLogo size={22} />
            <Wordmark className="truncate text-[13px]" />
          </Link>
          <button onClick={() => setPalette(true)} className="ml-auto rounded-md p-1.5 text-muted hover:bg-white/[0.06] hover:text-ink" aria-label={t("Suchen")}>
            <Search className="h-4 w-4" />
          </button>
        </header>

        <main className="min-w-0">
          <div key={pathname} className="mx-auto w-full max-w-[1400px] animate-fade-in px-4 py-6 motion-reduce:animate-none lg:px-10 lg:py-8">
            {children}
          </div>
        </main>
      </div>
    </>
  );
}
