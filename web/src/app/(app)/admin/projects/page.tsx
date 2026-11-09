"use client";

import Link from "next/link";
import { FolderKanban } from "lucide-react";
import { useState } from "react";
import { AdminSidebar } from "@/components/admin-sidebar";
import { useUser } from "@/components/layout/shell";
import { ErrorState, Loading } from "@/components/ui/async";
import { PixelAvatar } from "@/components/ui/mc";
import { Badge, Card, EmptyState, PageHeader, SearchInput } from "@/components/ui/primitives";
import { useApi } from "@/lib/api";
import { dateTime } from "@/lib/format";
import type { AdminProject } from "@/lib/types";

export default function AdminProjectsPage() {
  const user = useUser();
  const { data, error, mutate } = useApi<AdminProject[]>(
    user.isAdmin ? "/admin/projects" : null,
    { refreshInterval: 15000 },
  );
  const [filter, setFilter] = useState("");

  if (!user.isAdmin) {
    return <ErrorState error={new Error("Dafür braucht es einen Administrator.")} />;
  }

  if (error) {
    return (
      <>
        <AdminSidebar />
        <ErrorState error={error} retry={() => mutate()} />
      </>
    );
  }

  if (!data) {
    return (
      <>
        <AdminSidebar />
        <Loading />
      </>
    );
  }

  const trimmed = filter.trim().toLowerCase();
  const filtered = data.filter((project) => {
    if (!trimmed) return true;
    return (
      project.name.toLowerCase().includes(trimmed) ||
      project.description.toLowerCase().includes(trimmed) ||
      project.ownerName.toLowerCase().includes(trimmed) ||
      project.kind.toLowerCase().includes(trimmed)
    );
  });

  return (
    <>
      <AdminSidebar />
      <PageHeader
        title="Projekte"
        description="Alle Projekte im System mit Eigentümern, Mitgliedern und zugewiesenen Servern."
      />

      <SearchInput
        value={filter}
        onChange={(event) => setFilter(event.target.value)}
        placeholder="Nach Projektname, Beschreibung oder Eigentümer suchen"
        className="mb-5 max-w-md"
      />

      {data.length === 0 ? (
        <Card>
          <EmptyState
            icon={<FolderKanban className="h-6 w-6" />}
            title="Noch keine Projekte"
            text="Es wurden noch keine Projekte im System angelegt."
          />
        </Card>
      ) : filtered.length === 0 ? (
        <Card>
          <EmptyState
            icon={<FolderKanban className="h-6 w-6" />}
            title="Keine Projekte gefunden"
            text="Kein Projekt entspricht dem eingegebenen Suchbegriff."
          />
        </Card>
      ) : (
        <Card className="overflow-x-auto">
          <div className="min-w-[760px] divide-y divide-white/[0.05]">
            <div className="flex items-center gap-4 bg-white/[0.015] px-5 py-3 text-[11px] font-medium uppercase tracking-wider text-muted">
              <span className="min-w-0 flex-1">Projekt</span>
              <span className="w-28 shrink-0">Art</span>
              <span className="w-36 shrink-0">Eigentümer</span>
              <span className="w-24 shrink-0 text-right">Mitglieder</span>
              <span className="w-20 shrink-0 text-right">Server</span>
              <span className="w-36 shrink-0 text-right">Erstellt</span>
            </div>
            {filtered.map((project) => (
              <div
                key={project.id}
                className="flex items-center gap-4 px-5 py-3.5 text-sm transition duration-100 hover:bg-white/[0.02]"
              >
                <div className="flex min-w-0 flex-1 items-center gap-3">
                  <PixelAvatar name={project.name} size={30} />
                  <div className="min-w-0 flex-1">
                    <Link
                      href={`/project/${project.id}/`}
                      className="truncate font-medium text-ink transition hover:text-accent hover:underline"
                    >
                      {project.name}
                    </Link>
                    {project.description && (
                      <p className="truncate text-xs text-muted">{project.description}</p>
                    )}
                  </div>
                </div>

                <div className="w-28 shrink-0">
                  {project.kind === "network" ? (
                    <Badge tone="accent">Netzwerk</Badge>
                  ) : (
                    <Badge>Server</Badge>
                  )}
                </div>

                <div className="w-36 shrink-0 truncate text-xs text-ink/80">
                  {project.ownerName || project.ownerId}
                </div>

                <div className="w-24 shrink-0 text-right text-xs tabular-nums text-muted">
                  {project.memberCount}
                </div>

                <div className="w-20 shrink-0 text-right text-xs tabular-nums text-muted">
                  {project.serverCount}
                </div>

                <div className="w-36 shrink-0 text-right text-xs tabular-nums text-muted">
                  {dateTime(project.createdAt)}
                </div>
              </div>
            ))}
          </div>
        </Card>
      )}
    </>
  );
}
