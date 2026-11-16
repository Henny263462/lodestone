"use client";

import Link from "next/link";
import { ChevronRight, Cpu, FolderKanban, FolderPlus, MemoryStick, Network, Plus, Server as ServerIcon } from "lucide-react";
import { useState } from "react";
import { useAppNavigation } from "@/components/layout/navigation";
import { Badge, Button, Card, EmptyState, PageHeader, StatCard, cx } from "@/components/ui/primitives";
import { BlockIcon } from "@/components/ui/mc";
import { useUser } from "@/components/layout/shell";
import { ServerRow } from "@/components/server-list";
import { Dialog, useToast } from "@/components/ui/feedback";
import { ErrorState, Loading } from "@/components/ui/async";
import { api, useApi } from "@/lib/api";
import { bytes } from "@/lib/format";
import { roleLabel, type NodeInfo, type Project } from "@/lib/types";

export default function OverviewPage() {
  const navigation = useAppNavigation();
  const user = useUser();
  const { data: projects, error, mutate } = useApi<Project[]>("/projects", { refreshInterval: 5000 });
  const { data: nodes } = useApi<NodeInfo[]>("/nodes", { refreshInterval: 30000 });
  const [creating, setCreating] = useState(false);

  if (error) return <ErrorState error={error} retry={() => mutate()} />;
  if (!projects) return <Loading />;

  const servers = projects.flatMap((project) => project.servers);
  const running = servers.filter((server) => server.view?.state === "running");
  const memoryInUse = running.reduce((sum, server) => sum + (server.view?.stats?.memoryBytes ?? 0), 0);
  const nodesOnline = nodes?.filter((node) => node.online).length;

  return (
    <>
      <PageHeader
        title="Übersicht"
        description={`Hallo ${user.username}! Hier siehst du alle Projekte und Server, auf die du Zugriff hast.`}
        action={
          <Button variant="primary" onClick={() => setCreating(true)}>
            <Plus className="h-4 w-4" />
            Neues Projekt
          </Button>
        }
      />

      <div className="mb-8 grid gap-4 sm:grid-cols-2 xl:grid-cols-4">
        <StatCard label="Projekte" value={projects.length} hint={`${projects.filter((project) => project.kind === "network").length} davon Netzwerke`} icon={<FolderKanban className="h-4 w-4" />} tone="violet" />
        <StatCard label="Server laufen" value={`${running.length}/${servers.length}`} hint={servers.length === 0 ? "noch keine Server" : `${servers.length - running.length} aus oder beschäftigt`} icon={<ServerIcon className="h-4 w-4" />} tone="green" />
        <StatCard label="Arbeitsspeicher belegt" value={bytes(memoryInUse)} hint="von laufenden Servern" icon={<MemoryStick className="h-4 w-4" />} tone="blue" />
        <StatCard
          label="Nodes erreichbar"
          value={nodes ? `${nodesOnline}/${nodes.length}` : "–"}
          hint={nodes && nodesOnline !== nodes.length ? "mindestens ein Node antwortet nicht" : "Maschinen, auf denen Server laufen"}
          icon={<Cpu className="h-4 w-4" />}
          tone={nodes && nodesOnline !== nodes.length ? "red" : "neutral"}
        />
      </div>

      <h2 className="mb-3 font-display text-base tracking-wide text-pixel-shadow">Projekte</h2>
      {projects.length === 0 ? (
        <Card>
          <EmptyState
            icon={<FolderPlus className="h-6 w-6" />}
            title="Noch kein Projekt"
            text="Lege dein erstes Projekt an: ein einzelner Server oder ein Netzwerk mit Velocity."
            action={
              <Button variant="primary" onClick={() => setCreating(true)}>
                <Plus className="h-4 w-4" />
                Projekt anlegen
              </Button>
            }
          />
        </Card>
      ) : (
        <div className="grid gap-4 2xl:grid-cols-2">
          {projects.map((project) => (
            <ProjectCard key={project.id} project={project} />
          ))}
        </div>
      )}

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

function ProjectCard({ project }: { project: Project }) {
  const live = project.servers.filter((server) => server.view?.state === "running").length;
  return (
    <Card className="overflow-hidden">
      <Link href={`/project/${project.id}/`} className="group flex items-center gap-3.5 border-b border-white/[0.06] px-5 py-4 transition hover:bg-white/[0.03]">
        <BlockIcon name={project.name} size={36} />
        <div className="min-w-0 flex-1">
          <p className="truncate font-display text-base tracking-wide text-pixel-shadow group-hover:underline">{project.name}</p>
          <p className="truncate text-xs text-muted">
            {project.kind === "network" ? "Netzwerk" : "Einzelner Server"} · {roleLabel[project.myRole]}
            {project.description && ` · ${project.description}`}
          </p>
        </div>
        {project.servers.length > 0 && (
          <Badge tone={live > 0 ? "good" : "neutral"}>
            {live}/{project.servers.length} laufen
          </Badge>
        )}
        <ChevronRight className="h-4 w-4 shrink-0 text-muted transition group-hover:translate-x-0.5 group-hover:text-ink" />
      </Link>
      {project.servers.length === 0 ? (
        <p className="px-5 py-4 text-xs text-muted">Noch kein Server. Öffne das Projekt, um einen anzulegen.</p>
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
    try {
      const project = await api.post<Project>("/projects", { name, kind, description });
      setName("");
      setDescription("");
      onClose();
      onCreated(project);
    } catch (error) {
      toast.error(error instanceof Error ? error.message : "Fehler");
    } finally {
      setBusy(false);
    }
  }

  const options = [
    { id: "server" as const, icon: ServerIcon, title: "Einzelner Server", text: "Ein Server: Paper, Purpur, Folia, Leaf, Fabric, NeoForge, Forge, Vanilla oder Velocity." },
    { id: "network" as const, icon: Network, title: "Netzwerk", text: "Velocity-Proxy mit mehreren Servern dahinter. Wird automatisch verbunden." },
  ];

  return (
    <Dialog
      open={open}
      onClose={onClose}
      title="Neues Projekt"
      footer={
        <>
          <Button variant="ghost" onClick={onClose}>
            Abbrechen
          </Button>
          <Button variant="primary" loading={busy} disabled={!name.trim()} onClick={create}>
            Anlegen
          </Button>
        </>
      }
    >
      <div className="grid gap-3 sm:grid-cols-2">
        {options.map((option) => (
          <button
            key={option.id}
            type="button"
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
        <label className="label">Name</label>
        <input className="field" value={name} onChange={(event) => setName(event.target.value)} placeholder={kind === "network" ? "Mein Netzwerk" : "Survival"} maxLength={48} autoFocus />
      </div>
      <div>
        <label className="label">Beschreibung (optional)</label>
        <input className="field" value={description} onChange={(event) => setDescription(event.target.value)} maxLength={500} />
      </div>
    </Dialog>
  );
}
