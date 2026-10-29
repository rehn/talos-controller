"use client";

import Link from "next/link";
import { useState } from "react";
import { api, type DriftReport, type DriftRole } from "@/lib/api";
import { usePoll } from "@/lib/hooks";
import { MISSING, formatValue, prettyPath } from "@/lib/drift";
import { ErrorBox, Loading, PageHead, RefreshButton, StatusBadge } from "@/components/ui";

const ROLE_TITLE: Record<string, string> = {
  controlplane: "Control planes",
  worker: "Workers",
  unknown: "Unknown role",
};

export default function DriftPage() {
  const [all, setAll] = useState(false);
  const report = usePoll(() => api.get<DriftReport>(`/drift${all ? "?all=1" : ""}`), 0, [all]);
  const d = report.data;
  const errors = d ? Object.entries(d.errors) : [];

  return (
    <>
      <PageHead
        title="Config drift"
        subtitle="Settings that differ between nodes of the same role, compared on the live (secret-redacted) configs"
      >
        <label className="check small">
          <input type="checkbox" checked={all} onChange={(e) => setAll(e.target.checked)} />
          Include per-node settings
        </label>
        <RefreshButton onClick={report.refresh} lastUpdated={report.lastUpdated} />
      </PageHead>

      <ErrorBox error={report.error} />
      {report.loading && !d ? <Loading label="Reading machine configs…" /> : null}

      {d ? (
        <div className="stack">
          {errors.length ? (
            <div className="warn-box">
              Could not read the config of {errors.map(([n, e]) => `${n} (${e})`).join(", ")}; they are not compared.
            </div>
          ) : null}
          {!all && d.ignored.length ? (
            <div className="faint small">
              Hidden because they are expected to differ per node: hostname, network interfaces/addresses, install disk,
              certSANs, per-node network and volume documents.
            </div>
          ) : null}
          {d.roles.map((r) => (
            <RoleCard key={r.role} r={r} />
          ))}
          {d.roles.length === 0 ? <div className="empty">No node configs could be read</div> : null}
        </div>
      ) : null}
    </>
  );
}

function RoleCard({ r }: { r: DriftRole }) {
  const title = ROLE_TITLE[r.role] ?? r.role;
  const single = r.nodes.length < 2;

  return (
    <div className="card">
      <div className="card-head">
        <div className="row">
          <h2>{title}</h2>
          <span className="faint small">
            {r.nodes.length} node{r.nodes.length === 1 ? "" : "s"}
          </span>
        </div>
        {single ? (
          <span className="badge">nothing to compare</span>
        ) : r.diffs.length === 0 ? (
          <StatusBadge status="ok">consistent</StatusBadge>
        ) : (
          <StatusBadge status="warn">
            {r.diffs.length} difference{r.diffs.length === 1 ? "" : "s"}
          </StatusBadge>
        )}
      </div>

      {single ? (
        <div className="empty">Only one {r.role} node, so there is nothing to compare it with.</div>
      ) : r.diffs.length === 0 ? (
        <div className="empty">All {r.identical} settings are identical across {r.nodes.join(", ")}.</div>
      ) : (
        <>
          <div className="table-wrap">
            <table>
              <thead>
                <tr>
                  <th>Setting</th>
                  {r.nodes.map((n) => (
                    <th key={n}>{n}</th>
                  ))}
                  <th />
                </tr>
              </thead>
              <tbody>
                {r.diffs.map((diff) => (
                  <tr key={diff.path}>
                    <td className="mono small" style={{ maxWidth: 320, wordBreak: "break-word" }}>
                      {prettyPath(diff.path)}
                      {!diff.majority ? <div className="faint">no majority</div> : null}
                    </td>
                    {r.nodes.map((n) => {
                      const v = diff.values[n] ?? MISSING;
                      const odd = diff.outliers.includes(n);
                      return (
                        <td key={n} className="small" style={{ maxWidth: 220, wordBreak: "break-word" }}>
                          {odd ? (
                            <span className={`badge ${diff.majority ? "warn" : ""}`} style={{ whiteSpace: "normal" }}>
                              {formatValue(v)}
                            </span>
                          ) : (
                            <span className={v === MISSING ? "faint" : "mono"}>{formatValue(v)}</span>
                          )}
                        </td>
                      );
                    })}
                    <td className="num">
                      {diff.patchable && diff.patch ? (
                        <Link
                          className="btn btn-sm"
                          href={`/config/?node=${encodeURIComponent(diff.outliers.join(","))}&patch=${encodeURIComponent(diff.patch)}`}
                          title={`Set ${diff.outliers.join(", ")} to the majority value`}
                        >
                          Align {diff.outliers.length === 1 ? diff.outliers[0] : `${diff.outliers.length} nodes`}…
                        </Link>
                      ) : null}
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
          <div className="card-body faint small" style={{ borderTop: "1px solid var(--border)" }}>
            {r.identical} other settings are identical. Highlighted values differ from the majority.
          </div>
        </>
      )}
    </div>
  );
}
