"use client";

import Link from "next/link";
import { Fingerprint, KeyRound, LogOut, Plus, ShieldCheck, ShieldOff, Trash2, Users, UserX } from "lucide-react";
import { useState } from "react";
import { Badge, Button, Card, EmptyState, PageHeader, RowMenu, SearchInput, Switch } from "@/components/ui/primitives";
import { ConfirmDialog, Dialog, useToast } from "@/components/ui/feedback";
import { ErrorState, Loading } from "@/components/ui/async";
import { useUser } from "@/components/layout/shell";
import { PixelAvatar } from "@/components/ui/mc";
import { api, useApi } from "@/lib/api";
import { dateTime } from "@/lib/format";
import { roleLabel, type AdminUser, type AdminUserDetail } from "@/lib/types";
import { t, tp } from "@/lib/i18n";

export default function UsersPage() {
  const me = useUser();
  const toast = useToast();
  const { data, error, mutate } = useApi<AdminUser[]>(me.isAdmin ? "/admin/users" : null);
  const [filter, setFilter] = useState("");
  const [creating, setCreating] = useState(false);
  const [resetting, setResetting] = useState<AdminUser | null>(null);
  const [resettingTotp, setResettingTotp] = useState<AdminUser | null>(null);
  const [killingSessions, setKillingSessions] = useState<AdminUser | null>(null);
  const [removing, setRemoving] = useState<AdminUser | null>(null);
  const [passkeys, setPasskeys] = useState<AdminUser | null>(null);
  const [selectedUserId, setSelectedUserId] = useState<string | null>(null);

  if (!me.isAdmin) return <ErrorState error={new Error(t("Dafür braucht es einen Administrator."))} />;
  if (error) return <ErrorState error={error} retry={() => mutate()} />;
  if (!data) return <Loading />;

  async function patch(user: AdminUser, body: Partial<Pick<AdminUser, "isAdmin" | "disabled">>, message: string) {
    await toast.run(() => api.patch(`/users/${user.id}`, body), message);
    mutate();
  }

  const trimmed = filter.trim().toLowerCase();
  const filtered = data.filter((user) => (!trimmed ? true : user.username.toLowerCase().includes(trimmed)));

  return (
    <>
      <PageHeader
        crumbs={[{ label: t("Verwaltung") }, { label: t("Benutzer") }]}
        title={t("Benutzer")}
        description={t("Wer sich am Panel anmelden darf. Zugriff auf Projekte vergibst du im jeweiligen Projekt unter „Mitglieder“.")}
        action={
          <Button variant="primary" onClick={() => setCreating(true)}>
            <Plus className="h-4 w-4" />
            {t("Benutzer anlegen")}
          </Button>
        }
      />

      <SearchInput
        value={filter}
        onChange={(event) => setFilter(event.target.value)}
        placeholder={t("Nach Benutzername suchen")}
        className="mb-5 max-w-sm"
      />

      <Card>
        {data.length === 0 ? (
          <EmptyState
            icon={<Users className="h-6 w-6" />}
            title={t("Keine Benutzer")}
            text={t("Es wurden noch keine Benutzer im System angelegt.")}
          />
        ) : filtered.length === 0 ? (
          <EmptyState
            icon={<Users className="h-6 w-6" />}
            title={t("Keine Benutzer gefunden")}
            text={t("Kein Benutzer entspricht dem Suchbegriff.")}
          />
        ) : (
          <div className="divide-y divide-white/[0.05]">
            {filtered.map((user) => (
              <div
                key={user.id}
                onClick={() => setSelectedUserId(user.id)}
                className="flex cursor-pointer flex-wrap items-center gap-4 px-5 py-3.5 transition duration-100 hover:bg-white/[0.02]"
              >
                <PixelAvatar name={user.username} size={32} />
                <div className="min-w-[160px] flex-1">
                  <p className="text-sm font-medium">
                    {user.username}
                    {user.id === me.id && <span className="ml-2 text-xs text-muted">{t("(du)")}</span>}
                  </p>
                  <p className="text-xs text-muted">{t("Seit")} {dateTime(user.createdAt)}</p>
                </div>

                <div className="flex flex-wrap items-center gap-2">
                  {user.isAdmin && (
                    <Badge tone="accent">
                      <ShieldCheck className="h-3 w-3" />
                      {t("Administrator")}
                    </Badge>
                  )}
                  {user.disabled && <Badge tone="bad">{t("Gesperrt")}</Badge>}
                  {user.totpEnabled && (
                    <Badge tone="good">
                      <KeyRound className="h-3 w-3" />
                      {t("Zwei-Faktor an")}
                    </Badge>
                  )}
                  <Badge>{tp("projects", user.projectCount)}</Badge>
                  <Badge>{tp("sessions", user.sessionCount)}</Badge>
                </div>

                <RowMenu
                  items={[
                    {
                      label: t("Passwort zurücksetzen"),
                      icon: <KeyRound className="h-3.5 w-3.5" />,
                      onClick: () => setResetting(user),
                    },
                    {
                      label: t("Passkeys zurücksetzen"),
                      icon: <Fingerprint className="h-3.5 w-3.5" />,
                      onClick: () => setPasskeys(user),
                    },
                    ...(user.totpEnabled
                      ? [
                          {
                            label: t("Zwei-Faktor zurücksetzen"),
                            icon: <ShieldOff className="h-3.5 w-3.5" />,
                            onClick: () => setResettingTotp(user),
                          },
                        ]
                      : []),
                    {
                      label: t("Sitzungen beenden"),
                      icon: <LogOut className="h-3.5 w-3.5" />,
                      onClick: () => setKillingSessions(user),
                    },
                    {
                      label: user.isAdmin ? t("Administratorrechte entziehen") : t("Zum Administrator machen"),
                      icon: <ShieldCheck className="h-3.5 w-3.5" />,
                      disabled: user.id === me.id,
                      onClick: () => patch(user, { isAdmin: !user.isAdmin }, t("Gespeichert")),
                    },
                    {
                      label: user.disabled ? t("Entsperren") : t("Sperren"),
                      icon: <UserX className="h-3.5 w-3.5" />,
                      disabled: user.id === me.id,
                      onClick: () => patch(user, { disabled: !user.disabled }, user.disabled ? t("Entsperrt") : t("Gesperrt")),
                    },
                    "separator",
                    {
                      label: t("Löschen"),
                      icon: <Trash2 className="h-3.5 w-3.5" />,
                      danger: true,
                      disabled: user.id === me.id,
                      onClick: () => setRemoving(user),
                    },
                  ]}
                />
              </div>
            ))}
          </div>
        )}
      </Card>

      <CreateDialog open={creating} onClose={() => setCreating(false)} onDone={() => mutate()} />
      <ResetDialog user={resetting} onClose={() => setResetting(null)} />
      <UserDetailDialog userId={selectedUserId} onClose={() => setSelectedUserId(null)} />

      <ConfirmDialog
        open={!!resettingTotp}
        onClose={() => setResettingTotp(null)}
        title={t("Zwei-Faktor-Anmeldung für „{username}“ zurücksetzen?", { username: resettingTotp?.username })}
        text={t("Der Benutzer meldet sich danach ohne Code aus der App an und muss die Zwei-Faktor-Anmeldung bei Bedarf neu einrichten.")}
        confirm={t("Zurücksetzen")}
        danger
        onConfirm={async () => {
          if (!resettingTotp) return;
          await toast.run(() => api.delete(`/users/${resettingTotp.id}/totp`), t("Zwei-Faktor zurückgesetzt"));
          mutate();
        }}
      />

      <ConfirmDialog
        open={!!killingSessions}
        onClose={() => setKillingSessions(null)}
        title={t("Alle Sitzungen von „{username}“ beenden?", { username: killingSessions?.username })}
        text={t("Der Benutzer wird auf allen Geräten abgemeldet und muss sich neu anmelden.")}
        confirm={t("Sitzungen beenden")}
        danger
        onConfirm={async () => {
          if (!killingSessions) return;
          await toast.run(() => api.delete(`/admin/users/${killingSessions.id}/sessions`), t("Sitzungen beendet"));
          mutate();
        }}
      />

      <ConfirmDialog
        open={!!passkeys}
        onClose={() => setPasskeys(null)}
        title={t("Passkeys von „{username}“ zurücksetzen?", { username: passkeys?.username })}
        text={t("Alle Passkeys des Benutzers werden gelöscht. Er meldet sich danach mit Passwort oder über OIDC an und kann neue Passkeys unter Konto hinzufügen. Hat er beides nicht, kommt er erst wieder herein, wenn du sein Passwort zurücksetzt.")}
        confirm={t("Zurücksetzen")}
        danger
        onConfirm={async () => {
          if (!passkeys) return;
          await toast.run(() => api.delete(`/users/${passkeys.id}/passkeys`), t("Passkeys zurückgesetzt"));
          mutate();
        }}
      />
      <ConfirmDialog
        open={!!removing}
        onClose={() => setRemoving(null)}
        title={t("„{username}“ löschen?", { username: removing?.username })}
        text={t("Der Benutzer und seine Mitgliedschaften werden gelöscht. Projekte bleiben bestehen.")}
        confirm={t("Löschen")}
        danger
        onConfirm={async () => {
          if (!removing) return;
          await toast.run(() => api.delete(`/users/${removing.id}`), t("Benutzer gelöscht"));
          mutate();
        }}
      />
    </>
  );
}

function UserDetailDialog({ userId, onClose }: { userId: string | null; onClose: () => void }) {
  const { data, error, mutate } = useApi<AdminUserDetail>(userId ? `/admin/users/${userId}` : null);

  return (
    <Dialog
      open={!!userId}
      onClose={onClose}
      title={data ? t("Benutzer: {username}", { username: data.user.username }) : t("Benutzerdetails")}
      description={data ? (data.user.isAdmin ? t("Administrator") : t("Standardbenutzer")) : undefined}
      footer={
        <Button variant="secondary" onClick={onClose}>
          {t("Schließen")}
        </Button>
      }
    >
      {error ? (
        <div className="-mx-5 -my-4">
          <ErrorState inline error={error} retry={() => mutate()} />
        </div>
      ) : !data ? (
        <Loading />
      ) : (
        <div className="space-y-4">
          <div className="flex items-center gap-3 rounded-lg border border-white/[0.07] bg-white/[0.02] p-3 text-xs">
            <PixelAvatar name={data.user.username} size={36} />
            <div className="min-w-0 flex-1">
              <p className="font-medium text-ink">{data.user.username}</p>
              <p className="text-muted">{t("Angelegt am {date}", { date: dateTime(data.user.createdAt) })}</p>
            </div>
            <div className="flex flex-col items-end gap-1">
              {data.user.isAdmin && <Badge tone="accent">{t("Admin")}</Badge>}
              {data.user.disabled && <Badge tone="bad">{t("Gesperrt")}</Badge>}
              {data.user.totpEnabled && <Badge tone="good">{t("2FA an")}</Badge>}
            </div>
          </div>

          <div>
            <p className="label">{t("Zugeordnete Projekte ({count})", { count: data.projects.length })}</p>
            {data.projects.length === 0 ? (
              <p className="rounded-lg border border-white/[0.06] bg-black/20 p-3 text-center text-xs text-muted">{t("Keine Projekte zugewiesen")}</p>
            ) : (
              <div className="divide-y divide-white/[0.05] rounded-lg border border-white/[0.07]">
                {data.projects.map((project) => (
                  <div key={project.id} className="flex items-center justify-between gap-3 px-3 py-2.5 text-xs">
                    <div className="min-w-0 flex-1">
                      <Link
                        href={`/project/${project.id}/`}
                        className="font-medium text-ink transition hover:text-accent hover:underline"
                      >
                        {project.name}
                      </Link>
                      <span className="ml-2 text-muted">({project.kind === "network" ? t("Netzwerk") : "Server"})</span>
                    </div>
                    <Badge tone={project.role === "owner" ? "accent" : "neutral"}>
                      {roleLabel()[project.role]}
                    </Badge>
                  </div>
                ))}
              </div>
            )}
          </div>
        </div>
      )}
    </Dialog>
  );
}

function CreateDialog({ open, onClose, onDone }: { open: boolean; onClose: () => void; onDone: () => void }) {
  const toast = useToast();
  const [username, setUsername] = useState("");
  const [password, setPassword] = useState("");
  const [isAdmin, setIsAdmin] = useState(false);
  return (
    <Dialog
      open={open}
      onClose={onClose}
      title={t("Benutzer anlegen")}
      footer={
        <>
          <Button variant="ghost" onClick={onClose}>
            {t("Abbrechen")}
          </Button>
          <Button
            variant="primary"
            disabled={username.trim().length < 3 || password.length < 8}
            onClick={async () => {
              if (await toast.run(() => api.post("/users", { username: username.trim(), password, isAdmin }), t("Benutzer angelegt"))) {
                setUsername("");
                setPassword("");
                setIsAdmin(false);
                onClose();
                onDone();
              }
            }}
          >
            {t("Anlegen")}
          </Button>
        </>
      }
    >
      <div>
        <label className="label">{t("Benutzername")}</label>
        <input className="field" value={username} onChange={(event) => setUsername(event.target.value)} autoComplete="off" autoFocus />
      </div>
      <div>
        <label className="label">{t("Startpasswort (mindestens 8 Zeichen)")}</label>
        <input className="field" type="text" value={password} onChange={(event) => setPassword(event.target.value)} autoComplete="off" />
      </div>
      <label className="flex items-center justify-between gap-3 text-sm">
        <span>{t("Administrator (voller Zugriff auf alles)")}</span>
        <Switch checked={isAdmin} onChange={setIsAdmin} />
      </label>
    </Dialog>
  );
}

function ResetDialog({ user, onClose }: { user: AdminUser | null; onClose: () => void }) {
  const toast = useToast();
  const [password, setPassword] = useState("");
  return (
    <Dialog
      open={!!user}
      onClose={onClose}
      title={t("Passwort von {username} zurücksetzen", { username: user?.username })}
      description={t("Alle Sitzungen des Benutzers enden.")}
      footer={
        <>
          <Button variant="ghost" onClick={onClose}>
            {t("Abbrechen")}
          </Button>
          <Button
            variant="primary"
            disabled={password.length < 8}
            onClick={async () => {
              if (user && (await toast.run(() => api.post(`/users/${user.id}/password`, { password }), t("Passwort geändert")))) {
                setPassword("");
                onClose();
              }
            }}
          >
            {t("Setzen")}
          </Button>
        </>
      }
    >
      <div>
        <label className="label">{t("Neues Passwort (mindestens 8 Zeichen)")}</label>
        <input className="field" type="text" value={password} onChange={(event) => setPassword(event.target.value)} autoComplete="off" autoFocus />
      </div>
    </Dialog>
  );
}
