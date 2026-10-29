"use client";

import { api, type EtcdInfo } from "@/lib/api";
import { usePoll } from "@/lib/hooks";
import { bytes, pct } from "@/lib/format";
import { ErrorBox, Loading, Meter, PageHead, RefreshButton, StatusDot } from "@/components/ui";

export default function EtcdPage() {
  const etcd = usePoll(() => api.get<EtcdInfo>("/etcd"), 15000);
  const d = etcd.data;
  const members = d?.members ?? [];
  const healthy = members.filter((m) => m.healthy).length;
  const quorum = Math.floor(members.length / 2) + 1;

  return (
    <>
      <PageHead title="etcd" subtitle="Cluster datastore membership and health">
        <RefreshButton onClick={etcd.refresh} lastUpdated={etcd.lastUpdated} />
      </PageHead>
      <ErrorBox error={etcd.error ?? d?.error} />
      {etcd.loading && !d ? <Loading /> : null}
      {d ? (
        <div className="stack">
          <div className="grid grid-4">
            <div className="card stat">
              <div className="label">Healthy members</div>
              <div className="value">
                {healthy}/{members.length}
              </div>
              <div className="sub">quorum needs {quorum}</div>
            </div>
            <div className="card stat">
              <div className="label">Fault tolerance</div>
              <div className="value">{Math.max(0, healthy - quorum)}</div>
              <div className="sub">members that can fail right now</div>
            </div>
            <div className="card stat">
              <div className="label">Leader</div>
              <div className="value" style={{ fontSize: 18 }}>
                {d.leader || "none"}
              </div>
            </div>
            <div className="card stat">
              <div className="label">Alarms</div>
              <div className="value" style={{ color: d.alarms?.length ? "var(--fail)" : undefined }}>
                {d.alarms?.length ?? 0}
              </div>
              <div className="sub">{d.alarms?.join(", ") || "none active"}</div>
            </div>
          </div>
          {members.length === 1 ? (
            <div className="warn-box">
              Single-member etcd: losing this control plane node means losing the cluster datastore. Take regular
              snapshots (<code>talosctl etcd snapshot</code>).
            </div>
          ) : null}
          <div className="card">
            <div className="table-wrap">
              <table>
                <thead>
                  <tr>
                    <th>Member</th>
                    <th>ID</th>
                    <th>Status</th>
                    <th>DB size</th>
                    <th className="num">Raft index</th>
                  </tr>
                </thead>
                <tbody>
                  {members.map((m) => (
                    <tr key={m.id}>
                      <td>
                        <span className="row">
                          <StatusDot status={m.healthy ? "ok" : "fail"} />
                          <strong>{m.hostname}</strong>
                          {m.isLeader ? <span className="badge accent">leader</span> : null}
                          {m.isLearner ? <span className="badge warn">learner</span> : null}
                        </span>
                        <div className="faint small mono">{m.nodeIp}</div>
                      </td>
                      <td className="mono small">{m.id}</td>
                      <td>
                        {m.healthy ? "healthy" : m.error || (m.errors ?? []).join("; ") || "unhealthy"}
                      </td>
                      <td style={{ minWidth: 150 }}>
                        <Meter value={pct(m.dbSizeInUse, m.dbSize)} warnAt={101} failAt={101} />
                        <div className="faint small">
                          {bytes(m.dbSizeInUse)} in use of {bytes(m.dbSize)}
                        </div>
                      </td>
                      <td className="num mono">{m.raftIndex}</td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          </div>
        </div>
      ) : null}
    </>
  );
}
