"use client";

import { RotateCcw } from "lucide-react";
import { useState } from "react";
import { Badge, Button, Switch } from "@/components/ui/primitives";
import { Dialog, useToast } from "@/components/ui/feedback";
import { ErrorState, Loading } from "@/components/ui/async";
import { api, useApi } from "@/lib/api";
import { can, roleLabel, roleRank, type Member, type Permission, type PermissionCatalog, type Project, type Role } from "@/lib/types";
import { permissionText, t } from "@/lib/i18n";

interface Draft {
  userId: string;
  role: Role;
  selected: Set<Permission>;
}

/**
 * MemberPermissionsDialog zeigt die Rechte eines Mitglieds gruppiert mit Schaltern. Die Rolle ist die Vorlage; einzelne
 * Rechte lassen sich dazugeben oder wegnehmen. Dazugeben geht nur, was man selbst hat (das Backend prüft es ebenso).
 */
export function MemberPermissionsDialog({ project, member, editable, onClose, onDone }: { project: Project; member: Member | null; editable: boolean; onClose: () => void; onDone: () => void }) {
  const toast = useToast();
  const { data: catalog, error, mutate } = useApi<PermissionCatalog>(member ? "/permissions" : null);
  const [draft, setDraft] = useState<Draft | null>(null);
  const [busy, setBusy] = useState(false);

  // Beim Öffnen mit dem Stand des Mitglieds beginnen
  if (member && draft?.userId !== member.userId) setDraft({ userId: member.userId, role: member.role, selected: new Set(member.permissions) });

  function close() {
    setDraft(null);
    onClose();
  }

  const template = new Set(draft && catalog ? catalog.roles[draft.role] ?? [] : []);
  const owner = draft?.role === "owner";
  const differs = !!draft && !owner && (draft.selected.size !== template.size || [...draft.selected].some((permission) => !template.has(permission)));
  const groups = catalog ? [...new Set(catalog.permissions.map((info) => info.groupId))] : [];

  function update(patch: Partial<Draft>) {
    setDraft((current) => (current ? { ...current, ...patch } : current));
  }

  function toggle(permission: Permission, on: boolean) {
    if (!draft) return;
    const selected = new Set(draft.selected);
    if (on) selected.add(permission);
    else selected.delete(permission);
    update({ selected });
  }

  async function save() {
    if (!draft || !catalog || !member) return;
    setBusy(true);
    const body = owner ? { role: draft.role } : { role: draft.role, permissions: catalog.permissions.map((info) => info.id).filter((id) => draft.selected.has(id)) };
    const ok = await toast.run(() => api.put(`/projects/${project.id}/members/${member.userId}`, body), t("Rechte gespeichert"));
    setBusy(false);
    if (ok) {
      close();
      onDone();
    }
  }

  return (
    <Dialog
      open={!!member}
      onClose={close}
      wide
      title={member ? t("Rechte von „{username}“", { username: member.username }) : t("Rechte")}
      description={editable ? t("Die Rolle ist die Vorlage. Du kannst einzelne Rechte dazugeben oder wegnehmen – dazugeben aber nur, was du selbst hast.") : t("Was dieses Mitglied im Projekt darf.")}
      footer={
        <>
          <Button variant="ghost" onClick={close}>
            {editable ? t("Abbrechen") : t("Schließen")}
          </Button>
          {editable && (
            <Button variant="primary" loading={busy} disabled={!catalog || !draft} onClick={save}>
              {t("Speichern")}
            </Button>
          )}
        </>
      }
    >
      {error ? (
        <ErrorState error={error} retry={() => mutate()} />
      ) : !catalog || !draft || !member ? (
        <Loading />
      ) : (
        <>
          <div className="flex flex-wrap items-end gap-3">
            <div className="min-w-[180px] flex-1">
              <label className="label">{t("Rolle (Vorlage)")}</label>
              <select className="field" value={draft.role} disabled={!editable} onChange={(event) => update({ role: event.target.value as Role, selected: new Set(catalog.roles[event.target.value as Role] ?? []) })}>
                {(Object.keys(roleLabel()) as Role[]).map((value) => (
                  <option key={value} value={value} disabled={roleRank[value] > roleRank[project.myRole]}>
                    {roleLabel()[value]}
                  </option>
                ))}
              </select>
            </div>
            {differs && <Badge tone="accent">{t("Angepasst")}</Badge>}
            {editable && differs && (
              <Button size="sm" variant="ghost" onClick={() => update({ selected: new Set(template) })}>
                <RotateCcw className="h-3.5 w-3.5" />
                {t("Vorlage übernehmen")}
              </Button>
            )}
          </div>

          {owner ? (
            <p className="rounded-lg border border-info/25 bg-info/10 px-4 py-3 text-xs text-info">{t("Eigentümer haben immer alle Rechte und dürfen zusätzlich das Projekt löschen.")}</p>
          ) : (
            groups.map((group) => (
              <div key={group}>
                <p className="label">{permissionText(catalog.permissions.find((info) => info.groupId === group)!).group}</p>
                <div className="grid gap-2 sm:grid-cols-2">
                  {catalog.permissions
                    .filter((info) => info.groupId === group)
                    .map((info) => {
                      // Wegnehmen geht immer, dazugeben nur, was man selbst hat oder was das Mitglied schon hatte
                      const allowed = editable && (can(project.myPermissions, info.id) || member.permissions.includes(info.id));
                      return (
                        <div
                          key={info.id}
                          className="flex items-start gap-3 rounded-lg border border-white/[0.07] bg-white/[0.02] px-3 py-2.5"
                          title={editable && !allowed ? t("Dieses Recht hast du selbst nicht, darum kannst du es nicht vergeben.") : undefined}
                        >
                          <div className="min-w-0 flex-1">
                            <p className="text-sm">{permissionText(info).label}</p>
                            <p className="mt-0.5 text-xs text-muted">{permissionText(info).description}</p>
                          </div>
                          <Switch checked={draft.selected.has(info.id)} disabled={!allowed} onChange={(value) => toggle(info.id, value)} />
                        </div>
                      );
                    })}
                </div>
              </div>
            ))
          )}
        </>
      )}
    </Dialog>
  );
}
