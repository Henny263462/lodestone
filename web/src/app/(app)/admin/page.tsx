"use client";

import Link from "next/link";
import { Activity, ArrowRight, Cpu, FolderKanban, ScrollText, Server, ShieldCheck, Users } from "lucide-react";
import { useUser } from "@/components/layout/shell";
import { ErrorState, Loading } from "@/components/ui/async";
import { Badge, Card, CardHeader, EmptyState, PageHeader, StatCard } from "@/components/ui/primitives";
import { useApi } from "@/lib/api";
import { auditLabel } from "@/lib/audit";
import { dateTime } from "@/lib/format";
import type { AdminOverview, AuditEntry, UpdateStatus } from "@/lib/types";
import { t, tp } from "@/lib/i18n";

export default function AdminOverviewPage() {
  const user = useUser();
  const { data: overview, error: overviewError, mutate: mutateOverview } = useApi<AdminOverview>(
    user.isAdmin ? "/admin/overview" : null,
    { refreshInterval: 15000 },
  );
  const { data: audit, error: auditError, mutate: mutateAudit } = useApi<AuditEntry[]>(
    user.isAdmin ? "/audit?limit=8" : null,
    { refreshInterval: 15000 },
  );

  const { data: updates } = useApi<UpdateStatus>(user.isAdmin ? "/updates" : null, { refreshInterval: 60000 });

  if (!user.isAdmin) {
    return <ErrorState error={new Error(t("Dafür braucht es einen Administrator."))} />;
  }

  if (overviewError) {
    return (
      <>
        <ErrorState error={overviewError} retry={() => mutateOverview()} />
      </>
    );
  }

  if (!overview) {
    return (
      <>
        <Loading />
      </>
    );
  }

  return (
    <>
      <PageHeader
        crumbs={[{ label: t("Verwaltung") }, { label: t("System") }]}
        title={t("System")}
        description={t("Kennzahlen der ganzen Installation und die letzten Aktionen. Alle Verwaltungsseiten findest du links unter „Verwaltung“.")}
      />

      {updates?.updateAvailable && (
        <Link
          href="/admin/updates/"
          className="mb-4 flex items-center justify-between gap-3 rounded-lg border border-warn/25 bg-warn/10 px-3 py-2 text-xs text-warn transition duration-100 hover:bg-warn/20"
        >
          <span>{t("Eine neue Lodestone-Version ({version}) ist verfügbar.", { version: updates.latestVersion })}</span>
          <span className="inline-flex items-center gap-1 font-medium">
            {t("Zu den Updates")}
            <ArrowRight className="h-3.5 w-3.5" />
          </span>
        </Link>
      )}

      <div className="grid gap-4 sm:grid-cols-2 xl:grid-cols-3">
        <StatCard
          label={t("Benutzer")}
          value={overview.users}
          hint={overview.disabledUsers > 0 ? t("{count} gesperrt", { count: overview.disabledUsers }) : t("registrierte Konten")}
          icon={<Users className="h-4 w-4" />}
          tone="blue"
        />
        <StatCard
          label={t("Administratoren")}
          value={overview.admins}
          hint={t("mit vollem Zugriff")}
          icon={<ShieldCheck className="h-4 w-4" />}
          tone="violet"
        />
        <StatCard
          label={t("Projekte")}
          value={overview.projects}
          hint={tp("memberships", overview.members)}
          icon={<FolderKanban className="h-4 w-4" />}
          tone="green"
        />
        <StatCard
          label="Server"
          value={overview.servers}
          hint={t("in allen Projekten")}
          icon={<Server className="h-4 w-4" />}
          tone="amber"
        />
        <StatCard
          label="Nodes"
          value={overview.nodes}
          hint={t("Host-Maschinen")}
          icon={<Cpu className="h-4 w-4" />}
          tone="neutral"
        />
        <StatCard
          label={t("Aktive Sitzungen")}
          value={overview.sessions}
          hint={t("angemeldete Browser")}
          icon={<Activity className="h-4 w-4" />}
          tone="neutral"
        />
      </div>

      <div className="mt-8">
        <Card>
          <CardHeader
            title={t("Letzte Aktivität")}
            description={t("Die neuesten Aktionen im System")}
            action={
              <Link href="/audit/" className="text-xs text-accent hover:underline">
                {t("Alle anzeigen")}
              </Link>
            }
          />
          {auditError ? (
            <ErrorState inline error={auditError} retry={() => mutateAudit()} />
          ) : !audit ? (
            <Loading />
          ) : audit.length === 0 ? (
            <EmptyState
              icon={<ScrollText className="h-6 w-6" />}
              title={t("Keine Einträge")}
              text={t("Bisher wurden keine Aktionen protokolliert.")}
            />
          ) : (
            <div className="divide-y divide-white/[0.05]">
              {audit.map((entry) => (
                <div key={entry.id} className="flex flex-wrap items-center gap-x-4 gap-y-1 px-5 py-2.5 text-sm">
                  <span className="w-28 shrink-0 text-xs tabular-nums text-muted">{dateTime(entry.time)}</span>
                  <span className="w-24 shrink-0 truncate font-medium">{entry.username || "–"}</span>
                  <span className="min-w-[140px] flex-1 truncate">
                    {auditLabel(entry.action, entry.target)}
                    {entry.target && <span className="ml-1.5 font-mono text-[11px] text-muted">{entry.target}</span>}
                  </span>
                  {entry.status > 0 && (
                    <Badge tone={entry.status < 300 ? "good" : entry.status < 500 ? "warn" : "bad"}>
                      {entry.status}
                    </Badge>
                  )}
                </div>
              ))}
            </div>
          )}
        </Card>

      </div>
    </>
  );
}
