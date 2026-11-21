"use client";

import Link from "next/link";
import { HardDrive } from "lucide-react";
import { Meter, loadTone } from "@/components/ui/primitives";
import { Loading } from "@/components/ui/async";
import { useApi } from "@/lib/api";
import { bytes } from "@/lib/format";
import type { NodeInfo, NodeStorage } from "@/lib/types";

/** Festplatte eines Nodes: belegt, frei und (für Administratoren) die größten Server. */
export function NodeStorageCard({ node }: { node: NodeInfo }) {
  const { data, error } = useApi<NodeStorage>(`/nodes/${node.id}/storage`, { refreshInterval: 60000 });

  return (
    <div className="mt-4 space-y-3 border-t border-white/[0.06] pt-3">
      <p className="flex items-center gap-2 text-xs">
        <HardDrive className="h-3.5 w-3.5 text-muted" />
        Festplatte
      </p>
      {error ? (
        <p className="text-xs text-bad">{error.message}</p>
      ) : !data ? (
        <Loading />
      ) : !data.online ? (
        <p className="text-xs text-bad">{data.error || "Der Node antwortet nicht."}</p>
      ) : data.total === 0 ? (
        <p className="text-xs text-muted">Der Node kann den freien Platz auf diesem System nicht messen. Die Server belegen {bytes(data.used)}.</p>
      ) : (
        <Usage data={data} />
      )}
    </div>
  );
}

function Usage({ data }: { data: NodeStorage }) {
  const used = data.total - data.free;
  const percent = (used / data.total) * 100;
  const servers = [...(data.servers ?? [])].sort((a, b) => b.bytes + b.backups - (a.bytes + a.backups)).slice(0, 5);
  return (
    <>
      <div>
        <div className="mb-1.5 flex justify-between text-[11px] text-muted">
          <span>Belegt (davon Server und Backups: {bytes(data.used)})</span>
          <span className="tabular-nums text-ink/80">
            {bytes(data.free)} frei von {bytes(data.total)}
          </span>
        </div>
        <Meter value={percent} max={100} tone={loadTone(percent)} />
      </div>
      {percent >= 95 && <p className="text-[11px] text-bad">Die Platte ist fast voll. Lösche alte Backups oder ziehe Server auf einen anderen Node um.</p>}
      {servers.length > 0 && (
        <ul className="space-y-1 text-[11px]">
          {servers.map((server) => (
            <li key={server.id} className="flex items-center gap-2">
              <Link href={`/server/?id=${server.id}`} className="min-w-0 flex-1 truncate text-ink/80 hover:text-ink hover:underline">
                {server.name}
                <span className="text-muted"> · {server.projectName}</span>
              </Link>
              <span className="tabular-nums text-muted">
                {bytes(server.bytes)}
                {server.backups > 0 && ` + ${bytes(server.backups)} Backups`}
                {server.limitMB > 0 && ` · Limit ${bytes(server.limitMB * 1024 ** 2)}`}
              </span>
            </li>
          ))}
        </ul>
      )}
    </>
  );
}
