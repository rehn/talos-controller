"use client";

import { useState } from "react";
import { api, type Workloads } from "@/lib/api";
import { usePoll } from "@/lib/hooks";
import { ago } from "@/lib/format";
import { ErrorBox, Loading, PageHead, RefreshButton, StatusDot } from "@/components/ui";

export default function WorkloadsPage() {
  const wl = usePoll(() => api.get<Workloads>("/workloads"), 20000);
  const [showAll, setShowAll] = useState(false);
  const d = wl.data;
  const controllers = d ? (showAll ? d.controllers : d.controllers.filter((c) => !c.healthy)) : [];
  const unhealthy = d ? d.controllers.filter((c) => !c.healthy).length : 0;

  return (
    <>
      <PageHead title="Workloads" subtitle="Pods, controllers and warning events across all namespaces">
        <RefreshButton onClick={wl.refresh} lastUpdated={wl.lastUpdated} />
      </PageHead>
      <ErrorBox error={wl.error} />
      {wl.loading && !d ? <Loading /> : null}
      {d ? (
        <div className="stack">
          <div className="grid grid-4">
            <div className="card stat">
              <div className="label">Pods running</div>
              <div className="value">
                {d.podsRunning}/{d.podsTotal}
              </div>
              <div className="sub">{d.namespaces} namespaces</div>
            </div>
            <div className="card stat">
              <div className="label">Problem pods</div>
              <div className="value" style={{ color: d.problems.length ? "var(--fail)" : undefined }}>
                {d.problems.length}
              </div>
              <div className="sub">crashlooping, pending, failed, not ready</div>
            </div>
            <div className="card stat">
              <div className="label">Controllers</div>
              <div className="value">
                {d.controllers.length - unhealthy}/{d.controllers.length}
              </div>
              <div className="sub">deployments · statefulsets · daemonsets ready</div>
            </div>
            <div className="card stat">
              <div className="label">Warnings (2h)</div>
              <div className="value">{d.events.length}</div>
              <div className="sub">Kubernetes warning events</div>
            </div>
          </div>

          <div className="card">
            <div className="card-head">
              <h2>Problem pods</h2>
            </div>
            {d.problems.length === 0 ? (
              <div className="empty">No problem pods</div>
            ) : (
              <div className="table-wrap">
                <table>
                  <thead>
                    <tr>
                      <th>Pod</th>
                      <th>Node</th>
                      <th>Reason</th>
                      <th className="num">Restarts</th>
                      <th>Since</th>
                    </tr>
                  </thead>
                  <tbody>
                    {d.problems.map((p) => (
                      <tr key={p.namespace + "/" + p.name}>
                        <td>
                          <span className="faint">{p.namespace}/</span>
                          <strong>{p.name}</strong>
                        </td>
                        <td className="mono small">{p.node || "–"}</td>
                        <td>
                          <span className="badge fail">{p.reason}</span>
                        </td>
                        <td className="num">{p.restarts}</td>
                        <td className="muted">{ago(p.since)}</td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>
            )}
          </div>

          <div className="card">
            <div className="card-head">
              <h2>Controllers</h2>
              <label className="check small">
                <input type="checkbox" checked={showAll} onChange={(e) => setShowAll(e.target.checked)} /> show healthy
              </label>
            </div>
            {controllers.length === 0 ? (
              <div className="empty">{showAll ? "No controllers" : "All controllers have their desired replicas ready"}</div>
            ) : (
              <div className="table-wrap">
                <table>
                  <thead>
                    <tr>
                      <th>Name</th>
                      <th>Kind</th>
                      <th className="num">Ready</th>
                    </tr>
                  </thead>
                  <tbody>
                    {controllers.map((c) => (
                      <tr key={c.kind + c.namespace + c.name}>
                        <td>
                          <span className="row">
                            <StatusDot status={c.healthy ? "ok" : "fail"} />
                            <span>
                              <span className="faint">{c.namespace}/</span>
                              {c.name}
                            </span>
                          </span>
                        </td>
                        <td className="muted">{c.kind}</td>
                        <td className="num">
                          {c.ready}/{c.desired}
                        </td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>
            )}
          </div>

          <div className="card">
            <div className="card-head">
              <h2>Recent warning events</h2>
            </div>
            {d.events.length === 0 ? (
              <div className="empty">No warnings in the last 2 hours</div>
            ) : (
              <div className="table-wrap">
                <table>
                  <thead>
                    <tr>
                      <th>When</th>
                      <th>Object</th>
                      <th>Reason</th>
                      <th>Message</th>
                    </tr>
                  </thead>
                  <tbody>
                    {d.events.map((e, i) => (
                      <tr key={i}>
                        <td className="muted" style={{ whiteSpace: "nowrap" }}>
                          {ago(e.lastSeen)}
                          {e.count > 1 ? <div className="faint small">×{e.count}</div> : null}
                        </td>
                        <td className="small">
                          <span className="faint">{e.namespace}/</span>
                          {e.object}
                        </td>
                        <td>
                          <span className="badge warn">{e.reason}</span>
                        </td>
                        <td className="small">{e.message}</td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>
            )}
          </div>
        </div>
      ) : null}
    </>
  );
}
