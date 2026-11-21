"use client";

import Link from "next/link";
import { useSearchParams } from "next/navigation";
import { ArrowUpDown, ChevronRight, Cpu, FolderKanban, FolderPlus, MemoryStick, Network, Plus, Server as ServerIcon, Tag } from "lucide-react";
import { useEffect, useState, type ReactNode } from "react";
import { useAppNavigation } from "@/components/layout/navigation";
import { FavoriteButton, OrganizeDialog, ProjectFilterChips, ProjectTags, prefOf, useProjectPrefs } from "@/components/project-order";
import { NodesOverviewCard, RecentEventsCard } from "@/components/overview";
import { filterProjects, orderProjects, type ProjectFilter } from "@/lib/order";
import { Badge, Button, Card, EmptyState, PageHeader, StatCard, cx } from "@/components/ui/primitives";
import { BlockIcon } from "@/components/ui/mc";
import { useUser } from "@/components/layout/shell";
import { ServerRow } from "@/components/server-list";
import { Dialog, useToast } from "@/components/ui/feedback";
import { ErrorState, Loading } from "@/components/ui/async";
import { api, useApi } from "@/lib/api";
import { bytes } from "@/lib/format";
import { usePoll } from "@/lib/live";
import { roleLabel, type NodeInfo, type Project } from "@/lib/types";
import { t, tp } from "@/lib/i18n";

export default function OverviewPage() {
  const navigation = useAppNavigation();
  const params = useSearchParams();
  const user = useUser();
  // Zustände und neue Ereignisse kommen über den Live-Kanal; die Intervalle sind nur der Rückfall
  const { data: projects, error, mutate } = useApi<Project[]>("/projects", { refreshInterval: usePoll(60000, 5000) });
  const nodes = useApi<NodeInfo[]>("/nodes", { refreshInterval: usePoll(120000, 15000) });
  const [creating, setCreating] = useState(false);
  const { data: prefs, mutate: mutatePrefs } = useProjectPrefs();
  const [filter, setFilter] = useState<ProjectFilter>({ kind: "all" });
  const [organizing, setOrganizing] = useState(false);

  // Strg+K → „Neues Projekt“ landet hier mit ?new=1
  useEffect(() => {
    if (params.get("new") !== "1") return;
    setCreating(true);
    const url = new URL(window.location.href);
    url.searchParams.delete("new");
    window.history.replaceState({ ...window.history.state }, "", url);
  }, [params]);

  if (error) return <ErrorState error={error} retry={() => mutate()} />;
  if (!projects) return <Loading />;

  const ordered = orderProjects(projects, prefs);
  const shown = filterProjects(ordered, prefs, filter);

  const servers = projects.flatMap((project) => project.servers);
  const running = servers.filter((server) => server.view?.state === "running");
  const memoryInUse = running.reduce((sum, server) => sum + (server.view?.stats?.memoryBytes ?? 0), 0);
  const nodeList = nodes.data;
  const nodesOnline = nodeList?.filter((node) => node.online).length ?? 0;

  return (
    <>
      <PageHeader
        title={t("Übersicht")}
        description={t("Hallo {name}! Hier siehst du alle Projekte und Server, auf die du Zugriff hast.", { name: user.username })}
        action={
          <>
            {projects.length > 1 && (
              <Button onClick={() => setOrganizing(true)}>
                <ArrowUpDown className="h-4 w-4" />
                {t("Ordnen")}
              </Button>
            )}
            <Button variant="primary" onClick={() => setCreating(true)}>
              <Plus className="h-4 w-4" />
              {t("Neues Projekt")}
            </Button>
          </>
        }
      />

      <div className="mb-8 grid gap-4 sm:grid-cols-2 xl:grid-cols-4">
        <StatCard
          label={t("Projekte")}
          value={projects.length}
          hint={tp("networksAmong", projects.filter((project) => project.kind === "network").length)}
          icon={<FolderKanban className="h-4 w-4" />}
          tone="violet"
        />
        <StatCard
          label={t("Server laufen")}
          value={`${running.length}/${servers.length}`}
          hint={servers.length === 0 ? t("noch keine Server") : t("{count} aus oder beschäftigt", { count: servers.length - running.length })}
          icon={<ServerIcon className="h-4 w-4" />}
          tone="green"
        />
        <StatCard label={t("Arbeitsspeicher belegt")} value={bytes(memoryInUse)} hint={t("von laufenden Servern")} icon={<MemoryStick className="h-4 w-4" />} tone="blue" />
        <StatCard
          label={t("Nodes erreichbar")}
          value={nodeList ? `${nodesOnline}/${nodeList.length}` : "–"}
          hint={
            nodeList?.length === 0
              ? t("Noch kein Node eingerichtet")
              : nodeList && nodesOnline !== nodeList.length
                ? t("mindestens ein Node antwortet nicht")
                : t("Maschinen, auf denen Server laufen")
          }
          icon={<Cpu className="h-4 w-4" />}
          tone={nodeList && nodesOnline !== nodeList.length ? "red" : "neutral"}
        />
      </div>

      <div className="grid gap-5 xl:grid-cols-[minmax(0,1fr)_360px]">
        <section aria-label={t("Projekte")} className="min-w-0">
          <h2 className="mb-3 font-display text-base tracking-wide text-pixel-shadow">{t("Projekte")}</h2>
          {projects.length === 0 ? (
            <Card>
              <EmptyState
                icon={<FolderPlus className="h-6 w-6" />}
                title={t("Noch kein Projekt")}
                text={
                  nodeList && nodeList.length === 0
                    ? t("Richte zuerst unter „Nodes“ eine Maschine ein, dann lege hier ein Projekt an.")
                    : t("Lege dein erstes Projekt an: ein einzelner Server oder ein Netzwerk mit Velocity.")
                }
                action={
                  <Button variant="primary" onClick={() => setCreating(true)}>
                    <Plus className="h-4 w-4" />
                    {t("Projekt anlegen")}
                  </Button>
                }
              />
            </Card>
          ) : (
            <>
              <ProjectFilterChips prefs={prefs} value={filter} onChange={setFilter} />
              {shown.length === 0 && (
                <Card>
                  <EmptyState icon={<Tag className="h-6 w-6" />} title={t("Kein Projekt passt zum Filter")} text={t("Wähle „Alle“ oder ordne Projekten unter „Ordnen“ Schlagwörter zu.")} />
                </Card>
              )}
              <div className="grid gap-4">
                {shown.map((project) => (
                  <ProjectCard
                    key={project.id}
                    project={project}
                    favorite={<FavoriteButton project={project} pref={prefOf(prefs, project.id)} onChanged={() => mutatePrefs()} />}
                    tags={<ProjectTags pref={prefOf(prefs, project.id)} />}
                  />
                ))}
              </div>
            </>
          )}
        </section>

        <aside aria-label={t("Zustand")} className="min-w-0 space-y-5">
          <NodesOverviewCard nodes={nodeList} error={nodes.error} retry={() => nodes.mutate()} isAdmin={user.isAdmin} />
          {projects.length > 0 && <RecentEventsCard projects={projects} />}
        </aside>
      </div>

      <OrganizeDialog open={organizing} onClose={() => setOrganizing(false)} projects={ordered} prefs={prefs} onSaved={() => mutatePrefs()} />
      <NewProjectDialog
        open={creating}
        onClose={() => setCreating(false)}
        onCreated={(project) => {
          mutate();
          navigation.push(`/project/${project.id}/`);
        }}
      />
    </>
  );
}

function ProjectCard({ project, favorite, tags }: { project: Project; favorite: ReactNode; tags: ReactNode }) {
  const live = project.servers.filter((server) => server.view?.state === "running").length;
  return (
    <Card className="overflow-hidden">
      <div className="flex items-center gap-2 border-b border-white/[0.06] pr-3 transition hover:bg-white/[0.03]">
        <Link href={`/project/${project.id}/`} className="group flex min-w-0 flex-1 items-center gap-3.5 py-4 pl-5">
          <BlockIcon name={project.name} size={36} />
          <div className="min-w-0 flex-1">
            <p className="truncate font-display text-base tracking-wide text-pixel-shadow group-hover:underline">{project.name}</p>
            <p className="truncate text-xs text-muted">
              {project.kind === "network" ? t("Netzwerk") : t("Einzelner Server")} · {roleLabel()[project.myRole]}
              {project.description && ` · ${project.description}`}
            </p>
          </div>
          {project.servers.length > 0 && <Badge tone={live > 0 ? "good" : "neutral"}>{tp("serversRunning", live, { total: project.servers.length })}</Badge>}
          <ChevronRight className="h-4 w-4 shrink-0 text-muted transition group-hover:translate-x-0.5 group-hover:text-ink" />
        </Link>
        {favorite}
      </div>
      {tags && <div className="px-5 [&>div]:mb-3">{tags}</div>}
      {project.servers.length === 0 ? (
        <p className="px-5 py-4 text-xs text-muted">{t("Noch kein Server. Öffne das Projekt, um einen anzulegen.")}</p>
      ) : (
        <div className="divide-y divide-white/[0.05]">
          {project.servers.map((server) => (
            <ServerRow key={server.id} server={server} dense badges={server.role === "proxy" ? <Badge tone="accent">Proxy</Badge> : undefined} />
          ))}
        </div>
      )}
    </Card>
  );
}

function NewProjectDialog({ open, onClose, onCreated }: { open: boolean; onClose: () => void; onCreated: (project: Project) => void }) {
  const toast = useToast();
  const [name, setName] = useState("");
  const [kind, setKind] = useState<"server" | "network">("server");
  const [description, setDescription] = useState("");
  const [busy, setBusy] = useState(false);

  async function create() {
    setBusy(true);
    let project: Project | null = null;
    const ok = await toast.run(async () => {
      project = await api.post<Project>("/projects", { name, kind, description });
    }, t("Projekt angelegt"));
    setBusy(false);
    if (ok && project) {
      setName("");
      setDescription("");
      onClose();
      onCreated(project);
    }
  }

  const options = [
    { id: "server" as const, icon: ServerIcon, title: t("Einzelner Server"), text: t("Ein Server: Paper, Purpur, Folia, Leaf, Fabric, NeoForge, Forge, Vanilla oder Velocity.") },
    { id: "network" as const, icon: Network, title: t("Netzwerk"), text: t("Velocity-Proxy mit mehreren Servern dahinter. Wird automatisch verbunden.") },
  ];

  return (
    <Dialog
      open={open}
      onClose={onClose}
      title={t("Neues Projekt")}
      footer={
        <>
          <Button variant="ghost" onClick={onClose}>
            {t("Abbrechen")}
          </Button>
          <Button variant="primary" loading={busy} disabled={!name.trim()} onClick={create}>
            {t("Anlegen")}
          </Button>
        </>
      }
    >
      <div className="grid gap-3 sm:grid-cols-2">
        {options.map((option) => (
          <button
            key={option.id}
            type="button"
            aria-pressed={kind === option.id}
            onClick={() => setKind(option.id)}
            className={cx(
              "rounded-lg border p-3 text-left transition active:translate-y-px",
              kind === option.id ? "border-accent/70 bg-accent/10" : "border-white/[0.08] bg-black/20 hover:border-white/[0.16]",
            )}
          >
            <option.icon className={cx("mb-2 h-4 w-4", kind === option.id ? "text-accent" : "text-muted")} />
            <p className="font-display text-sm tracking-wide">{option.title}</p>
            <p className="mt-1 text-xs text-muted">{option.text}</p>
          </button>
        ))}
      </div>
      <div>
        <label className="label">{t("Name")}</label>
        <input className="field" value={name} onChange={(event) => setName(event.target.value)} placeholder={kind === "network" ? t("Mein Netzwerk") : "Survival"} maxLength={48} autoFocus />
      </div>
      <div>
        <label className="label">{t("Beschreibung (optional)")}</label>
        <input className="field" value={description} onChange={(event) => setDescription(event.target.value)} maxLength={500} />
      </div>
    </Dialog>
  );
}
