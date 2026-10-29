"use client";

import Link from "next/link";
import type { Node } from "@/lib/api";
import { bytes, pct, uptime } from "@/lib/format";
import { Meter, RoleBadge, StatusDot } from "./ui";

export function nodeStatus(n: Node): "ok" | "warn" | "fail" {
  if (!n.reachable) return "fail";
  if (n.inKubernetes && !n.ready) return "fail";
  if (n.inKubernetes && !n.schedulable) return "warn";
  return "ok";
}

export default function NodeTable({ nodes }: { nodes: Node[] }) {
  if (nodes.length === 0) return <div className="empty">No nodes found</div>;
  return (
    <div className="table-wrap">
      <table>
        <thead>
          <tr>
            <th>Node</th>
            <th>Role</th>
            <th>Status</th>
            <th>Talos</th>
            <th>Kubelet</th>
            <th>CPU load</th>
            <th>Memory</th>
            <th>Uptime</th>
          </tr>
        </thead>
        <tbody>
          {nodes.map((n) => {
            const memUsed = pct(n.memTotal - n.memAvailable, n.memTotal);
            const loadPct = n.cpus ? (n.load1 / n.cpus) * 100 : 0;
            const st = nodeStatus(n);
            return (
              <tr key={n.name}>
                <td>
                  <Link href={`/node/?name=${encodeURIComponent(n.name)}`}>
                    <strong>{n.name}</strong>
                  </Link>
                  <div className="faint small mono">{n.ip}</div>
                </td>
                <td>
                  <RoleBadge role={n.role} />
                </td>
                <td>
                  <div className="row">
                    <StatusDot status={st} />
                    <span>
                      {!n.reachable
                        ? "Unreachable"
                        : !n.inKubernetes
                          ? "Talos only"
                          : n.ready
                            ? n.schedulable
                              ? "Ready"
                              : "Ready, cordoned"
                            : "NotReady"}
                    </span>
                  </div>
                  {n.error ? <div className="faint small" title={n.error}>{n.error.slice(0, 60)}</div> : null}
                </td>
                <td className="mono">{n.talosVersion || "–"}</td>
                <td className="mono">{n.kubeletVersion || "–"}</td>
                <td style={{ minWidth: 120 }}>
                  {n.reachable ? (
                    <>
                      <Meter value={Math.min(100, loadPct)} />
                      <div className="faint small">
                        {n.load1.toFixed(2)} / {n.cpus} cores
                      </div>
                    </>
                  ) : (
                    "–"
                  )}
                </td>
                <td style={{ minWidth: 130 }}>
                  {n.memTotal ? (
                    <>
                      <Meter value={memUsed} />
                      <div className="faint small">
                        {bytes(n.memTotal - n.memAvailable)} / {bytes(n.memTotal)}
                      </div>
                    </>
                  ) : (
                    "–"
                  )}
                </td>
                <td>{uptime(n.bootTime)}</td>
              </tr>
            );
          })}
        </tbody>
      </table>
    </div>
  );
}
