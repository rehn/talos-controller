"use client";

import Link from "next/link";
import { useRouter, useSearchParams } from "next/navigation";
import { Suspense, useState } from "react";
import { api, type Job, type MachineConfigInfo, type NodeDetail } from "@/lib/api";
import { usePoll } from "@/lib/hooks";
import { bytes, dateTime, pct, uptime, ago } from "@/lib/format";
import {
  Confirm,
  ErrorBox,
  Loading,
  Meter,
  PageHead,
  RefreshButton,
  RoleBadge,
  Spinner,
  StatusDot,
} from "@/components/ui";
import { nodeStatus } from "@/components/NodeTable";

type Tab = "overview" | "services" | "logs" | "dmesg" | "config";

export default function NodePage() {
  return (
    <Suspense fallback={<Loading />}>
      <NodeView />
    </Suspense>
  );
}

function NodeView() {
  const params = useSearchParams();
  const router = useRouter();
  const name = params.get("name") ?? "";
  const [tab, setTab] = useState<Tab>((params.get("tab") as Tab) || "overview");
  const [logService, setLogService] = useState(params.get("service") ?? "kubelet");
  const [rebootOpen, setRebootOpen] = useState(false);
  const [restart, setRestart] = useState<string | null>(null);

  const detail = usePoll(() => api.get<NodeDetail>(`/nodes/${encodeURIComponent(name)}`), 15000, [name]);

  if (!name) return <ErrorBox error="No node selected" />;

  const d = detail.data;
  const n = d?.node;

  return (
    <>
      <div className="small" style={{ marginBottom: 8 }}>
        <Link href="/nodes/">← Nodes</Link>
      </div>
      <PageHead
        title={
          <span className="row">
            {n ? <StatusDot status={nodeStatus(n)} /> : null}
            {name}
            {n ? <RoleBadge role={n.role} /> : null}
          </span>
        }
        subtitle={n ? `${n.ip} · ${n.osImage || n.talosVersion || ""} · ${n.arch}` : undefined}
      >
        <RefreshButton onClick={detail.refresh} lastUpdated={detail.lastUpdated} />
        <Link className="btn" href={`/upgrades/?node=${encodeURIComponent(name)}`}>
          Upgrade
        </Link>
        <button className="btn btn-danger" disabled={!n?.reachable} onClick={() => setRebootOpen(true)}>
          Reboot
        </button>
      </PageHead>

      <ErrorBox error={detail.error} />
      {detail.loading && !d ? <Loading /> : null}

      {d && n ? (
        <>
          <div className="tabs">
            {(["overview", "services", "logs", "dmesg", "config"] as Tab[]).map((t) => (
              <button key={t} className={tab === t ? "active" : ""} onClick={() => setTab(t)}>
                {t === "dmesg" ? "Kernel log" : t[0].toUpperCase() + t.slice(1)}
              </button>
            ))}
          </div>

          {tab === "overview" ? <OverviewTab d={d} /> : null}

          {tab === "services" ? (
            <div className="card">
              <ErrorBox error={d.servicesError} />
              <div className="table-wrap">
                <table>
                  <thead>
                    <tr>
                      <th>Service</th>
                      <th>State</th>
                      <th>Health</th>
                      <th>Last change</th>
                      <th />
                    </tr>
                  </thead>
                  <tbody>
                    {d.services.map((s) => {
                      const bad = !["Running", "Finished", "Skipped"].includes(s.state) || (s.healthKnown && !s.healthy);
                      return (
                        <tr key={s.id}>
                          <td>
                            <strong className="mono">{s.id}</strong>
                            {s.lastEvent ? <div className="faint small">{s.lastEvent}</div> : null}
                          </td>
                          <td>
                            <span className="row">
                              <StatusDot status={bad ? "fail" : "ok"} />
                              {s.state}
                            </span>
                          </td>
                          <td>
                            {s.healthKnown ? (s.healthy ? "healthy" : "unhealthy") : <span className="faint">?</span>}
                            {s.message ? <div className="faint small">{s.message}</div> : null}
                          </td>
                          <td className="muted">{ago(s.lastChange)}</td>
                          <td className="num">
                            <div className="row" style={{ justifyContent: "flex-end" }}>
                              <button
                                className="btn btn-sm"
                                onClick={() => {
                                  setLogService(s.id);
                                  setTab("logs");
                                }}
                              >
                                Logs
                              </button>
                              <button className="btn btn-sm" onClick={() => setRestart(s.id)}>
                                Restart
                              </button>
                            </div>
                          </td>
                        </tr>
                      );
                    })}
                  </tbody>
                </table>
              </div>
            </div>
          ) : null}

          {tab === "logs" ? (
            <LogsTab
              node={name}
              services={d.services.map((s) => s.id)}
              service={logService}
              onService={setLogService}
            />
          ) : null}

          {tab === "dmesg" ? <DmesgTab node={name} /> : null}
          {tab === "config" ? <ConfigTab node={name} /> : null}
        </>
      ) : null}

      <Confirm
        open={rebootOpen}
        title={`Reboot ${name}?`}
        danger
        confirmLabel="Reboot"
        typeToConfirm={name}
        onClose={() => setRebootOpen(false)}
        onConfirm={async (typed) => {
          const job = await api.post<Job>(`/nodes/${encodeURIComponent(name)}/reboot`, { confirm: typed });
          router.push(`/jobs/?id=${job.id}`);
        }}
      >
        <p>
          The node reboots now and its pods are interrupted. On a control plane node, make sure etcd keeps quorum
          while it is down.
        </p>
      </Confirm>

      <Confirm
        open={restart !== null}
        title={`Restart service ${restart ?? ""}?`}
        confirmLabel="Restart"
        onClose={() => setRestart(null)}
        onConfirm={async () => {
          await api.post(`/nodes/${encodeURIComponent(name)}/services/${encodeURIComponent(restart ?? "")}/restart`);
          setTimeout(() => void detail.refresh(), 2000);
        }}
      >
        <p>
          Restarts <code>{restart}</code> on {name}. Restarting <code>etcd</code> or <code>apid</code> briefly affects
          cluster access.
        </p>
      </Confirm>
    </>
  );
}

function OverviewTab({ d }: { d: NodeDetail }) {
  const n = d.node;
  const memUsed = pct(n.memTotal - n.memAvailable, n.memTotal);
  return (
    <div className="stack">
      <div className="grid grid-4">
        <div className="card stat">
          <div className="label">Talos</div>
          <div className="value mono" style={{ fontSize: 20 }}>
            {n.talosVersion || "–"}
          </div>
          <div className="sub">uptime {uptime(n.bootTime)}</div>
        </div>
        <div className="card stat">
          <div className="label">Kubelet</div>
          <div className="value mono" style={{ fontSize: 20 }}>
            {n.kubeletVersion || "–"}
          </div>
          <div className="sub">{n.inKubernetes ? (n.ready ? "Ready" : "NotReady") : "not registered"}</div>
        </div>
        <div className="card stat">
          <div className="label">CPU load (1m / 5m)</div>
          <div className="value">
            {n.load1.toFixed(2)} <span className="muted" style={{ fontSize: 15 }}>/ {n.load5.toFixed(2)}</span>
          </div>
          <div className="sub">{n.cpus} cores</div>
        </div>
        <div className="card stat">
          <div className="label">Memory</div>
          <div className="value">{memUsed.toFixed(0)}%</div>
          <div className="sub">
            {bytes(n.memTotal - n.memAvailable)} of {bytes(n.memTotal)}
          </div>
        </div>
      </div>

      <div className="card">
        <div className="card-head">
          <h2>Install</h2>
        </div>
        <div className="card-body">
          <ErrorBox error={d.configError} />
          {d.config ? (
            <div className="row wrap">
              <span className="muted">Installer image</span>
              <code>{d.config.installImage || "–"}</code>
              <span className="muted">· machine type</span>
              <code>{d.config.type}</code>
            </div>
          ) : null}
          <div className="faint small" style={{ marginTop: 6 }}>
            Booted {n.bootTime ? dateTime(new Date(n.bootTime * 1000).toISOString()) : "–"}
          </div>
        </div>
      </div>

      <div className="card">
        <div className="card-head">
          <h2>Filesystems</h2>
        </div>
        <div className="table-wrap">
          <table>
            <thead>
              <tr>
                <th>Mount</th>
                <th>Device</th>
                <th>Usage</th>
                <th className="num">Used</th>
                <th className="num">Size</th>
              </tr>
            </thead>
            <tbody>
              {d.mounts.map((m) => {
                const used = pct(m.size - m.available, m.size);
                return (
                  <tr key={m.mountedOn + m.filesystem}>
                    <td className="mono">{m.mountedOn}</td>
                    <td className="mono faint">{m.filesystem}</td>
                    <td style={{ minWidth: 140 }}>
                      <Meter value={used} warnAt={85} failAt={95} />
                    </td>
                    <td className="num">{used.toFixed(0)}%</td>
                    <td className="num">{bytes(m.size)}</td>
                  </tr>
                );
              })}
              {d.mounts.length === 0 ? (
                <tr>
                  <td colSpan={5} className="empty">
                    No data
                  </td>
                </tr>
              ) : null}
            </tbody>
          </table>
        </div>
      </div>
    </div>
  );
}

function LogsTab({
  node,
  services,
  service,
  onService,
}: {
  node: string;
  services: string[];
  service: string;
  onService: (s: string) => void;
}) {
  const [lines, setLines] = useState(300);
  const [auto, setAuto] = useState(false);
  const logs = usePoll(
    () =>
      api.get<{ log: string }>(
        `/nodes/${encodeURIComponent(node)}/logs/${encodeURIComponent(service)}?lines=${lines}`,
      ),
    auto ? 5000 : 0,
    [node, service, lines, auto],
  );

  return (
    <div className="card">
      <div className="card-head">
        <div className="row wrap">
          <select value={service} onChange={(e) => onService(e.target.value)} style={{ width: 200 }}>
            {(services.includes(service) ? services : [service, ...services]).map((s) => (
              <option key={s} value={s}>
                {s}
              </option>
            ))}
          </select>
          <select value={lines} onChange={(e) => setLines(Number(e.target.value))} style={{ width: 130 }}>
            {[100, 300, 1000, 3000].map((l) => (
              <option key={l} value={l}>
                last {l} lines
              </option>
            ))}
          </select>
          <label className="check small">
            <input type="checkbox" checked={auto} onChange={(e) => setAuto(e.target.checked)} /> auto-refresh
          </label>
        </div>
        <button className="btn btn-sm" onClick={() => void logs.refresh()}>
          {logs.loading ? <Spinner /> : null} Reload
        </button>
      </div>
      <div className="card-body">
        <ErrorBox error={logs.error} />
        {logs.data ? <pre className="log">{logs.data.log || "(empty)"}</pre> : logs.loading ? <Loading /> : null}
      </div>
    </div>
  );
}

function DmesgTab({ node }: { node: string }) {
  const logs = usePoll(() => api.get<{ log: string }>(`/nodes/${encodeURIComponent(node)}/dmesg?lines=500`), 0, [node]);
  return (
    <div className="card">
      <div className="card-head">
        <h2>Kernel log (last 500 lines)</h2>
        <button className="btn btn-sm" onClick={() => void logs.refresh()}>
          Reload
        </button>
      </div>
      <div className="card-body">
        <ErrorBox error={logs.error} />
        {logs.data ? <pre className="log">{logs.data.log || "(empty)"}</pre> : <Loading />}
      </div>
    </div>
  );
}

function ConfigTab({ node }: { node: string }) {
  const cfg = usePoll(() => api.get<MachineConfigInfo>(`/nodes/${encodeURIComponent(node)}/config`), 0, [node]);
  return (
    <div className="card">
      <div className="card-head">
        <h2>Machine config</h2>
        <div className="row">
          <span className="faint small">secrets redacted</span>
          <Link className="btn btn-sm" href={`/config/?node=${encodeURIComponent(node)}`}>
            Patch…
          </Link>
        </div>
      </div>
      <div className="card-body">
        <ErrorBox error={cfg.error} />
        {cfg.data ? <pre className="log">{cfg.data.yaml}</pre> : cfg.loading ? <Loading /> : null}
      </div>
    </div>
  );
}
