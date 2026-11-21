"use client";

import { SlidersHorizontal, Trash2, UserPlus } from "lucide-react";
import { useState } from "react";
import { Badge, Button, Card, CardHeader, EmptyState } from "@/components/ui/primitives";
import { ConfirmDialog, Dialog, useToast } from "@/components/ui/feedback";
import { MemberPermissionsDialog } from "@/components/member-permissions";
import { useUser } from "@/components/layout/shell";
import { PixelAvatar } from "@/components/ui/mc";
import { api, useApi } from "@/lib/api";
import { can, roleLabel, roleRank, type Member, type Project, type Role } from "@/lib/types";
import { t } from "@/lib/i18n";

/** Tab „Mitglieder“: wer im Projekt ist, mit welcher Rolle, und angepasste Rechte. */
export function MembersTab({ project, refresh }: { project: Project; refresh: () => void }) {
  const toast = useToast();
  const user = useUser();
  const canManage = can(project.myPermissions, "members");
  const [adding, setAdding] = useState(false);
  const [removing, setRemoving] = useState<Member | null>(null);
  const [rights, setRights] = useState<Member | null>(null);
  const members = project.members ?? [];
  const editable = (member: Member) => canManage && roleRank[member.role] <= roleRank[project.myRole];

  async function setRole(member: Member, role: Role) {
    // Eine neue Rolle setzt die Rechte auf ihre Vorlage zurück
    await toast.run(() => api.put(`/projects/${project.id}/members/${member.userId}`, { role }), member.customPermissions ? t("Rolle geändert, Rechte folgen jetzt der Vorlage") : t("Rolle geändert"));
    refresh();
  }

  return (
    <Card>
      <CardHeader
        title={t("Mitglieder")}
        description={t("Die Rolle ist eine Vorlage für Rechte. Unter „Rechte“ passt du sie für einzelne Mitglieder an.")}
        action={
          canManage && (
            <Button size="sm" onClick={() => setAdding(true)}>
              <UserPlus className="h-3.5 w-3.5" />
              {t("Hinzufügen")}
            </Button>
          )
        }
      />
      <p className="border-b border-white/[0.06] px-5 py-3 text-xs text-muted">
        {t("Betrachter sehen Konsole und Spieler, Operatoren steuern Server und Dateien, Admins ändern Einstellungen, Eigentümer verwalten das Projekt.")}
      </p>
      {members.length === 0 ? (
        <EmptyState icon={<UserPlus className="h-6 w-6" />} title={t("Noch keine Mitglieder")} text={t("Füge Benutzer hinzu, damit sie das Projekt sehen und bedienen können.")} />
      ) : (
        <div className="divide-y divide-white/[0.05]">
          {members.map((member) => (
            <div key={member.userId} className="flex flex-wrap items-center gap-x-4 gap-y-2 px-5 py-3">
              <PixelAvatar name={member.username} size={32} />
              <p className="min-w-0 flex-1 truncate text-sm">
                {member.username}
                {member.userId === user.id && <span className="ml-2 text-xs text-muted">{t("(du)")}</span>}
              </p>
              {member.customPermissions && <Badge tone="accent">{t("Angepasst")}</Badge>}
              {member.role !== "owner" && (editable(member) || member.userId === user.id) && (
                <Button size="sm" variant="ghost" onClick={() => setRights(member)} title={editable(member) ? t("Rechte anpassen") : t("Rechte ansehen")}>
                  <SlidersHorizontal className="h-3.5 w-3.5" />
                  {t("Rechte")}
                </Button>
              )}
              <select className="field w-36" aria-label={t("Rolle von {username}", { username: member.username })} value={member.role} disabled={!editable(member)} onChange={(event) => setRole(member, event.target.value as Role)}>
                {(Object.keys(roleLabel()) as Role[]).map((role) => (
                  <option key={role} value={role} disabled={roleRank[role] > roleRank[project.myRole]}>
                    {roleLabel()[role]}
                  </option>
                ))}
              </select>
              {(canManage || member.userId === user.id) && (
                <Button variant="ghost" size="icon-sm" onClick={() => setRemoving(member)} aria-label={member.userId === user.id ? t("Projekt verlassen") : `${member.username} entfernen`} title={member.userId === user.id ? t("Projekt verlassen") : t("Entfernen")}>
                  <Trash2 className="h-3.5 w-3.5" />
                </Button>
              )}
            </div>
          ))}
        </div>
      )}
      <AddMemberDialog project={project} open={adding} onClose={() => setAdding(false)} onDone={refresh} />
      <MemberPermissionsDialog project={project} member={rights} editable={!!rights && editable(rights)} onClose={() => setRights(null)} onDone={refresh} />
      <ConfirmDialog
        open={!!removing}
        onClose={() => setRemoving(null)}
        title={removing?.userId === user.id ? t("Projekt verlassen?") : `${removing?.username ?? t("Mitglied")} entfernen?`}
        text={removing ? removing.userId === user.id ? t("Du verlierst den Zugriff auf dieses Projekt und seine Server.") : t("{name} verliert den Zugriff auf dieses Projekt und seine Server.", { name: removing.username }) : ""}
        confirm={removing?.userId === user.id ? t("Verlassen") : t("Entfernen")}
        danger
        onConfirm={async () => {
          if (!removing) return;
          const leaving = removing.userId === user.id;
          const ok = await toast.run(() => api.delete(`/projects/${project.id}/members/${removing.userId}`), leaving ? t("Projekt verlassen") : t("Mitglied entfernt"));
          if (ok && leaving) window.location.replace("/");
          refresh();
        }}
      />
    </Card>
  );
}

function AddMemberDialog({ project, open, onClose, onDone }: { project: Project; open: boolean; onClose: () => void; onDone: () => void }) {
  const toast = useToast();
  const { data: users, error } = useApi<{ id: string; username: string }[]>(open ? "/users" : null);
  const [userId, setUserId] = useState("");
  const [role, setRole] = useState<Role>("viewer");
  const [busy, setBusy] = useState(false);
  const present = new Set((project.members ?? []).map((member) => member.userId));
  const choices = (users ?? []).filter((candidate) => !present.has(candidate.id));

  return (
    <Dialog
      open={open}
      onClose={onClose}
      title={t("Mitglied hinzufügen")}
      footer={
        <>
          <Button variant="ghost" onClick={onClose}>
            {t("Abbrechen")}
          </Button>
          <Button
            variant="primary"
            loading={busy}
            disabled={!userId}
            onClick={async () => {
              setBusy(true);
              const ok = await toast.run(() => api.put(`/projects/${project.id}/members/${userId}`, { role }), t("Mitglied hinzugefügt"));
              setBusy(false);
              if (ok) {
                setUserId("");
                onClose();
                onDone();
              }
            }}
          >
            {t("Hinzufügen")}
          </Button>
        </>
      }
    >
      <div>
        <label className="label">{t("Benutzer")}</label>
        <select className="field" value={userId} onChange={(event) => setUserId(event.target.value)} disabled={!users}>
          <option value="">{users ? t("Auswählen …") : t("Benutzer werden geladen …")}</option>
          {choices.map((candidate) => (
            <option key={candidate.id} value={candidate.id}>
              {candidate.username}
            </option>
          ))}
        </select>
        {error && <p className="mt-1.5 text-xs text-bad">{error.message}</p>}
        {users && choices.length === 0 && <p className="mt-1.5 text-xs text-muted">{t("Alle Benutzer sind schon Mitglied. Neue Benutzer legt ein Administrator unter „Benutzer“ an.")}</p>}
      </div>
      <div>
        <label className="label">{t("Rolle")}</label>
        <select className="field" value={role} onChange={(event) => setRole(event.target.value as Role)}>
          {(Object.keys(roleLabel()) as Role[]).map((value) => (
            <option key={value} value={value} disabled={roleRank[value] > roleRank[project.myRole]}>
              {roleLabel()[value]}
            </option>
          ))}
        </select>
      </div>
    </Dialog>
  );
}
