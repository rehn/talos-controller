"use client";

import Link from "next/link";
import { api, type Overview } from "@/lib/api";
import { usePoll } from "@/lib/hooks";
import { ErrorBox, Loading, PageHead, RefreshButton, StatusBadge, StatusDot } from "@/components/ui";
import NodeTable from "@/components/NodeTable";

export default function Dashboard() {
  const ov = usePoll(() => api.get<Overview>("/overview"), 20000);
  const d = ov.data;

  const ready = d ? d.nodes.filter((n) => n.ready || (!n.inKubernetes && n.reachable)).length : 0;
  const cps = d ? d.nodes.filter((n) => n.role === "controlplane").length : 0;
  const members = d?.etcd.members ?? [];
  const healthyMembers = members.filter((m) => m.healthy).length;
  const problems = d?.workloads?.problems.length ?? 0;

  return (
    <>
      <PageHead
        title={
          <span className="row">
            {d?.clusterName ?? "Cluster"} {d ? <StatusBadge status={d.status} /> : null}
          </span>
        }
        subtitle="Health, capacity and versions at a glance"
      >
        <RefreshButton
          lastUpdated={ov.lastUpdated}
          onClick={async () => {
            await api.get<Overview>("/overview?refresh=1");
            await ov.refresh();
          }}
        />
      </PageHead>

      <ErrorBox error={ov.error} />
      {ov.loading && !d ? <Loading label="Collecting cluster state…" /> : null}

      {d ? (
        <div className="stack">
          <div className="grid grid-4">
            <div className="card stat">
              <div className="label">Nodes ready</div>
              <div className="value">
                {ready}/{d.nodes.length}
              </div>
              <div className="sub">
                {cps} control plane · {d.nodes.length - cps} worker
              </div>
            </div>
            <div className="card stat">
              <div className="label">Talos</div>
              <div className="value mono" style={{ fontSize: 20 }}>
                {(d.talosVersions ?? []).join(", ") || "–"}
              </div>
              <div className="sub">
                {(d.talosVersions ?? []).length > 1 ? "mixed versions" : "consistent across nodes"} ·{" "}
                <Link href="/upgrades/">upgrades</Link>
              </div>
            </div>
            <div className="card stat">
              <div className="label">Kubernetes</div>
              <div className="value mono" style={{ fontSize: 20 }}>
                {d.kubernetesVersion || "–"}
              </div>
              <div className="sub">API server version</div>
            </div>
            <div className="card stat">
              <div className="label">etcd</div>
              <div className="value">
                {healthyMembers}/{members.length}
              </div>
              <div className="sub">{d.etcd.leader ? `leader: ${d.etcd.leader}` : "no leader"}</div>
            </div>
            <div className="card stat">
              <div className="label">Pods</div>
              <div className="value">
                {d.workloads ? `${d.workloads.podsRunning}/${d.workloads.podsTotal}` : "–"}
              </div>
              <div className="sub">
                {problems > 0 ? (
                  <Link href="/workloads/">{problems} need attention</Link>
                ) : (
                  "all healthy"
                )}
              </div>
            </div>
          </div>

          <div className="card">
            <div className="card-head">
              <h2>Health checks</h2>
            </div>
            {d.checks.map((c) => (
              <div className="check-row" key={c.id}>
                <StatusDot status={c.status} />
                <div style={{ flex: 1 }}>
                  <div className="row">
                    <strong>{c.name}</strong>
                    <span className="spacer" />
                    <span className="muted small">{c.detail}</span>
                  </div>
                  {c.items && c.items.length > 0 && c.status !== "ok" ? (
                    <ul>
                      {c.items.slice(0, 8).map((it) => (
                        <li key={it}>{it}</li>
                      ))}
                      {c.items.length > 8 ? <li>…and {c.items.length - 8} more</li> : null}
                    </ul>
                  ) : null}
                </div>
              </div>
            ))}
          </div>

          <div className="card">
            <div className="card-head">
              <h2>Nodes</h2>
              <Link href="/nodes/" className="small">
                View all
              </Link>
            </div>
            <NodeTable nodes={d.nodes} />
          </div>
        </div>
      ) : null}
    </>
  );
}
