"use client";

import Link from "next/link";
import { useRouter, useSearchParams } from "next/navigation";
import { ArrowUpDown, Cpu, FolderPlus, Network, Play, Plus, Server as ServerIcon, Tag, Users } from "lucide-react";
import { useEffect, useState } from "react";
import { FavoriteButton, OrganizeDialog, ProjectFilterChips, ProjectTags, prefOf, useProjectPrefs } from "@/components/project-order";
import { NodesOverviewCard, RecentEventsCard } from "@/components/overview";
import { filterProjects, orderProjects, type ProjectFilter } from "@/lib/order";
import { Badge, Button, Card, EmptyState, PageHeader, StatCard, cx } from "@/components/ui/primitives";
import { Dialog, useToast } from "@/components/ui/feedback";
import { ErrorState, Loading } from "@/components/ui/async";
import { useUser } from "@/components/layout/shell";
import { StateBadge } from "@/components/status";
import { api, useApi } from "@/lib/api";
import { usePoll } from "@/lib/live";
import { roleLabel, type NodeInfo, type Project } from "@/lib/types";
import { t, tp } from "@/lib/i18n";

export default function OverviewPage() {
  const router = useRouter();
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
    window.history.replaceState(null, "", url);
  }, [params]);

  if (error) return <ErrorState error={error} retry={() => mutate()} />;
  if (!projects) return <Loading />;

  const ordered = orderProjects(projects, prefs);
  const shown = filterProjects(ordered, prefs, filter);

  const servers = projects.flatMap((project) => project.servers);
  const running = servers.filter((server) => server.view?.state === "running").length;
  const online = nodes.data?.filter((node) => node.online).length ?? 0;

  return (
    <>
      <PageHeader
        title={t("Übersicht")}
        description={t("Deine Projekte, ihre Server und was zuletzt passiert ist.")}
        action={
          <div className="flex flex-wrap gap-2">
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
          </div>
        }
      />

      <div className="grid gap-4 sm:grid-cols-2 xl:grid-cols-4">
        <StatCard label={t("Projekte")} value={projects.length} icon={<FolderPlus className="h-4 w-4" />} tone="violet" hint={tp("networks", projects.filter((p) => p.kind === "network").length)} />
        <StatCard label="Server" value={servers.length} icon={<ServerIcon className="h-4 w-4" />} tone="blue" hint={t("in allen Projekten")} />
        <StatCard label={t("Laufen gerade")} value={running} icon={<Play className="h-4 w-4" />} tone={running > 0 ? "green" : "neutral"} hint={t("von {total}", { total: servers.length })} />
        <StatCard
          label={t("Nodes online")}
          value={nodes.data ? `${online}/${nodes.data.length}` : "–"}
          icon={<Cpu className="h-4 w-4" />}
          tone={nodes.data && online < nodes.data.length ? "red" : "amber"}
          hint={nodes.data?.length === 0 ? t("Noch kein Node eingerichtet") : t("Maschinen mit Docker")}
        />
      </div>

      <div className="mt-8 grid gap-5 xl:grid-cols-[minmax(0,1fr)_360px]">
        <section aria-label={t("Projekte")} className="min-w-0">
          {projects.length === 0 ? (
            <Card>
              <EmptyState
                icon={<FolderPlus className="h-6 w-6" />}
                title={t("Noch kein Projekt")}
                text={nodes.data && nodes.data.length === 0 ? t("Richte zuerst unter „Nodes“ eine Maschine ein, dann lege hier ein Projekt an.") : t("Lege dein erstes Projekt an: ein einzelner Server oder ein Netzwerk mit Velocity.")}
              />
              <div className="flex justify-center pb-10">
                <Button variant="primary" onClick={() => setCreating(true)}>
                  <Plus className="h-4 w-4" />
                  {t("Projekt anlegen")}
                </Button>
              </div>
            </Card>
          ) : (
            <>
              <ProjectFilterChips prefs={prefs} value={filter} onChange={setFilter} />
              {shown.length === 0 && (
                <Card>
                  <EmptyState icon={<Tag className="h-6 w-6" />} title={t("Kein Projekt passt zum Filter")} text={t("Wähle „Alle“ oder ordne Projekten unter „Ordnen“ Schlagwörter zu.")} />
                </Card>
              )}
              <div className="grid gap-4 md:grid-cols-2 xl:grid-cols-1 2xl:grid-cols-2">
                {shown.map((project) => (
                  <ProjectCard key={project.id} project={project} favorite={<FavoriteButton project={project} pref={prefOf(prefs, project.id)} onChanged={() => mutatePrefs()} />} tags={<ProjectTags pref={prefOf(prefs, project.id)} />} />
                ))}
              </div>
            </>
          )}
        </section>

        <aside aria-label={t("Zustand")} className="min-w-0 space-y-5">
          <NodesOverviewCard nodes={nodes.data} error={nodes.error} retry={() => nodes.mutate()} isAdmin={user.isAdmin} />
          {projects.length > 0 && <RecentEventsCard projects={projects} />}
        </aside>
      </div>

      <OrganizeDialog open={organizing} onClose={() => setOrganizing(false)} projects={ordered} prefs={prefs} onSaved={() => mutatePrefs()} />
      <NewProjectDialog
        open={creating}
        onClose={() => setCreating(false)}
        onCreated={(project) => {
          mutate();
          router.push(`/project/?id=${project.id}`);
        }}
      />
    </>
  );
}

function ProjectCard({ project, favorite, tags }: { project: Project; favorite: React.ReactNode; tags: React.ReactNode }) {
  const live = project.servers.filter((server) => server.view?.state === "running").length;
  const first = project.servers[0];
  return (
    <Link href={`/project/?id=${project.id}`} className="group min-w-0">
      <Card className="h-full p-5 transition duration-100 group-hover:border-white/[0.16] group-active:scale-[0.99]">
        <div className="flex items-start justify-between gap-3">
          <div className="flex min-w-0 items-center gap-3">
            <span className="grid h-9 w-9 shrink-0 place-items-center rounded-lg border border-white/[0.08] bg-white/[0.04] text-muted">
              {project.kind === "network" ? <Network className="h-4 w-4" /> : <ServerIcon className="h-4 w-4" />}
            </span>
            <div className="min-w-0">
              <p className="truncate text-sm font-semibold">{project.name}</p>
              <p className="text-xs text-muted">{project.kind === "network" ? t("Netzwerk") : t("Einzelner Server")}</p>
            </div>
          </div>
          <div className="flex shrink-0 items-center gap-1">
            {project.kind === "server" && first ? (
              <StateBadge server={first} />
            ) : (
              <Badge tone={live > 0 ? "good" : "neutral"}>
                {live}/{project.servers.length} {t("laufen")}
              </Badge>
            )}
            {favorite}
          </div>
        </div>
        {project.description && <p className="mt-3 line-clamp-2 text-xs text-muted">{project.description}</p>}
        {tags}
        <div className="mt-4 flex min-w-0 items-center gap-3 text-xs text-muted">
          <span className="inline-flex shrink-0 items-center gap-1">
            <Users className="h-3.5 w-3.5" />
            {roleLabel()[project.myRole]}
          </span>
          {first?.address && <span className="truncate font-mono">{first.address}</span>}
        </div>
      </Card>
    </Link>
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
    { id: "server" as const, icon: ServerIcon, title: t("Einzelner Server"), text: t("Ein Server: Paper, Purpur, Folia, Fabric, NeoForge, Forge, Vanilla oder Velocity.") },
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
              "rounded-lg border p-3 text-left transition active:scale-[0.98]",
              kind === option.id ? "border-accent/60 bg-accent/10" : "border-white/[0.08] bg-white/[0.02] hover:border-white/[0.16]",
            )}
          >
            <option.icon className="mb-2 h-4 w-4 text-muted" />
            <p className="text-sm font-medium">{option.title}</p>
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
