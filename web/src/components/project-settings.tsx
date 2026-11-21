"use client";

import { useRouter } from "next/navigation";
import { Archive, Bot, Globe, Settings2, Trash2, Truck } from "lucide-react";
import { useState } from "react";
import { Button, Card, CardHeader, SectionLayout, type Section } from "@/components/ui/primitives";
import { ConfirmDialog, useToast } from "@/components/ui/feedback";
import { StatusCard } from "@/components/status-settings";
import { BackupTargetCard } from "@/components/backup-target";
import { ProjectTransferCard } from "@/components/server/transfer";
import { DiscordBotCard } from "@/components/discord-bot";
import { api } from "@/lib/api";
import { useUrlState } from "@/lib/url-state";
import type { Project } from "@/lib/types";

type SectionId = "general" | "status" | "backups" | "discord" | "transfer" | "danger";

/** Tab „Einstellungen“ eines Projekts (Recht „Einstellungen“), in Abschnitte geteilt. Löschen nur für Eigentümer. */
export function ProjectSettingsTab({ project, refresh }: { project: Project; refresh: () => void }) {
  const isOwner = project.myRole === "owner";
  const [section, setSection] = useUrlState<SectionId>("section", "general");
  const sections: Section<SectionId>[] = [
    { id: "general", label: "Allgemein", icon: Settings2 },
    { id: "status", label: "Statusseite", icon: Globe },
    { id: "backups", label: "Backup-Ziel", icon: Archive },
    { id: "discord", label: "Discord-Bot", icon: Bot },
    { id: "transfer", label: "Umziehen", icon: Truck },
    ...(isOwner ? [{ id: "danger" as const, label: "Gefahrenzone", icon: Trash2 }] : []),
  ];
  const active = sections.some((entry) => entry.id === section) ? section : "general";

  return (
    <SectionLayout label="Abschnitte der Projekteinstellungen" sections={sections} value={active} onChange={setSection}>
      {active === "general" && <GeneralCard project={project} refresh={refresh} />}
      {active === "status" && <StatusCard project={project} refresh={refresh} />}
      {active === "backups" && <BackupTargetCard project={project} />}
      {active === "discord" && <DiscordBotCard project={project} />}
      {active === "transfer" && <ProjectTransferCard project={project} />}
      {active === "danger" && <DeleteProject project={project} />}
    </SectionLayout>
  );
}

function GeneralCard({ project, refresh }: { project: Project; refresh: () => void }) {
  const toast = useToast();
  const [name, setName] = useState(project.name);
  const [description, setDescription] = useState(project.description);
  const [busy, setBusy] = useState(false);
  return (
    <Card>
      <CardHeader title="Allgemein" description="Name und Beschreibung erscheinen in der Übersicht und in der Seitenleiste." />
      <div className="space-y-4 px-5 py-4">
        <div>
          <label className="label">Name</label>
          <input className="field max-w-md" value={name} onChange={(event) => setName(event.target.value)} maxLength={48} />
        </div>
        <div>
          <label className="label">Beschreibung (optional)</label>
          <input className="field max-w-xl" value={description} onChange={(event) => setDescription(event.target.value)} maxLength={500} />
        </div>
      </div>
      <div className="flex justify-end border-t border-white/[0.06] px-5 py-3">
        <Button
          variant="primary"
          loading={busy}
          disabled={!name.trim() || (name === project.name && description === project.description)}
          onClick={async () => {
            setBusy(true);
            const ok = await toast.run(() => api.patch(`/projects/${project.id}`, { name, description }), "Gespeichert");
            setBusy(false);
            if (ok) refresh();
          }}
        >
          Speichern
        </Button>
      </div>
    </Card>
  );
}

function DeleteProject({ project }: { project: Project }) {
  const toast = useToast();
  const router = useRouter();
  const [deleting, setDeleting] = useState(false);
  return (
    <>
      <Card className="border-bad/25">
        <CardHeader title="Projekt löschen" description="Löscht alle Server dieses Projekts samt Dateien und Backups. Das lässt sich nicht rückgängig machen." />
        <div className="px-5 py-4">
          <Button variant="danger" onClick={() => setDeleting(true)}>
            <Trash2 className="h-4 w-4" />
            Projekt löschen
          </Button>
        </div>
      </Card>
      <ConfirmDialog
        open={deleting}
        onClose={() => setDeleting(false)}
        title={`„${project.name}“ löschen?`}
        text="Alle Server, Welten, Plugins und Backups dieses Projekts werden unwiderruflich gelöscht."
        confirm="Endgültig löschen"
        danger
        onConfirm={async () => {
          if (await toast.run(() => api.delete(`/projects/${project.id}`), "Projekt gelöscht")) router.replace("/");
        }}
      />
    </>
  );
}
