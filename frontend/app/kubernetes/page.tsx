"use client";

import { useState, type ReactNode } from "react";
import {
  api,
  type KubeConfigMap,
  type KubeDeployment,
  type KubeHTTPRoute,
  type KubeKeyed,
  type KubePod,
  type KubePods,
  type KubeService,
} from "@/lib/api";
import { usePoll } from "@/lib/hooks";
import { ago, bytes, millicores } from "@/lib/format";
import { ErrorBox, Loading, Meter, PageHead, RefreshButton, Spinner, StatusDot } from "@/components/ui";

type Tab = "pods" | "deployments" | "services" | "httproutes" | "configmaps" | "secrets";

const TABS: { id: Tab; label: string }[] = [
  { id: "pods", label: "Pods" },
  { id: "deployments", label: "Deployments" },
  { id: "services", label: "Services" },
  { id: "httproutes", label: "HTTPRoutes" },
  { id: "configmaps", label: "ConfigMaps" },
  { id: "secrets", label: "Secrets" },
];

const q = (ns: string) => `?namespace=${encodeURIComponent(ns)}`;

function Name({ ns, name, showNs }: { ns: string; name: string; showNs: boolean }) {
  return (
    <>
      {showNs ? <span className="faint">{ns}/</span> : null}
      <strong>{name}</strong>
    </>
  );
}

export default function KubernetesPage() {
  const [tab, setTab] = useState<Tab>("pods");
  const [ns, setNs] = useState("");
  const [filter, setFilter] = useState("");
  const namespaces = usePoll(() => api.get<string[]>("/k8s/namespaces"), 0);
  const showNs = ns === "";
  const f = filter.trim().toLowerCase();
  const match = (r: { namespace: string; name: string }) => !f || `${r.namespace}/${r.name}`.toLowerCase().includes(f);

  return (
    <>
      <PageHead title="Kubernetes" subtitle="Browse pods, deployments, services, routes, configmaps and secrets">
        <div className="row wrap">
          <select value={ns} onChange={(e) => setNs(e.target.value)} style={{ width: 200 }} aria-label="Namespace">
            <option value="">All namespaces</option>
            {(namespaces.data ?? []).map((n) => (
              <option key={n} value={n}>
                {n}
              </option>
            ))}
          </select>
          <input type="text" placeholder="Filter by name…" value={filter} onChange={(e) => setFilter(e.target.value)} style={{ width: 200 }} />
        </div>
      </PageHead>
      <div className="tabs">
        {TABS.map((t) => (
          <button key={t.id} className={tab === t.id ? "active" : ""} onClick={() => setTab(t.id)}>
            {t.label}
          </button>
        ))}
      </div>
      {tab === "pods" ? <PodsTab ns={ns} showNs={showNs} match={match} /> : null}
      {tab === "deployments" ? <DeploymentsTab ns={ns} showNs={showNs} match={match} /> : null}
      {tab === "services" ? <ServicesTab ns={ns} showNs={showNs} match={match} /> : null}
      {tab === "httproutes" ? <RoutesTab ns={ns} showNs={showNs} match={match} /> : null}
      {tab === "configmaps" ? <ConfigMapsTab ns={ns} showNs={showNs} match={match} /> : null}
      {tab === "secrets" ? <SecretsTab ns={ns} showNs={showNs} match={match} /> : null}
    </>
  );
}

interface TabProps {
  ns: string;
  showNs: boolean;
  match: (r: { namespace: string; name: string }) => boolean;
}

function Card({
  title,
  poll,
  count,
  children,
  note,
}: {
  title: string;
  poll: { error: string | null; loading: boolean; refresh: () => Promise<void>; lastUpdated: Date | null; data: unknown };
  count: number;
  children: ReactNode;
  note?: ReactNode;
}) {
  return (
    <div className="stack">
      <ErrorBox error={poll.error} />
      {note}
      <div className="card">
        <div className="card-head">
          <h2>
            {title} <span className="faint">({count})</span>
          </h2>
          <div className="row"><RefreshButton onClick={poll.refresh} lastUpdated={poll.lastUpdated} /></div>
        </div>
        {poll.loading && !poll.data ? <Loading /> : count === 0 && poll.data ? <div className="empty">Nothing found</div> : children}
      </div>
    </div>
  );
}

function statusClass(s: string): string {
  if (s === "Running" || s === "Completed" || s === "Succeeded") return "ok";
  if (s === "Pending" || s === "ContainerCreating" || s === "PodInitializing" || s === "Terminating") return "warn";
  return "fail";
}

function UsageCell({ used, request, limit, fmt }: { used: number | null; request: number; limit: number; fmt: (n: number) => string }) {
  if (used === null) return <span className="faint">–</span>;
  const cap = limit || request;
  return (
    <div style={{ minWidth: 110 }}>
      <div>
        {fmt(used)}
        {cap ? <span className="faint small"> / {fmt(cap)}</span> : null}
      </div>
      {cap ? <Meter value={Math.min(100, (used / cap) * 100)} /> : null}
    </div>
  );
}

function PodsTab({ ns, showNs, match }: TabProps) {
  const pods = usePoll(() => api.get<KubePods>(`/k8s/pods${q(ns)}`), 15000, [ns]);
  const [logPod, setLogPod] = useState<KubePod | null>(null);
  const rows = (pods.data?.pods ?? []).filter(match);
  const mem = (n: number) => bytes(n);

  return (
    <>
      <Card
        title="Pods"
        poll={pods}
        count={rows.length}
        note={
          pods.data && !pods.data.metricsAvailable ? (
            <div className="warn-box">CPU/memory unavailable: {pods.data.metricsError}. Install metrics-server to see usage.</div>
          ) : null
        }
      >
        <div className="table-wrap">
          <table>
            <thead>
              <tr>
                <th>Pod</th>
                <th>Status</th>
                <th>Ready</th>
                <th className="num">Restarts</th>
                <th>CPU</th>
                <th>Memory</th>
                <th>Node</th>
                <th>Age</th>
                <th />
              </tr>
            </thead>
            <tbody>
              {rows.map((p) => (
                <tr key={p.namespace + "/" + p.name}>
                  <td>
                    <Name ns={p.namespace} name={p.name} showNs={showNs} />
                  </td>
                  <td>
                    <span className={`badge ${statusClass(p.status)}`}>{p.status}</span>
                  </td>
                  <td>{p.ready}</td>
                  <td className="num">{p.restarts}</td>
                  <td>
                    <UsageCell used={p.cpuMilli} request={p.cpuRequestMilli} limit={p.cpuLimitMilli} fmt={(n) => millicores(n)} />
                  </td>
                  <td>
                    <UsageCell used={p.memBytes} request={p.memRequestBytes} limit={p.memLimitBytes} fmt={mem} />
                  </td>
                  <td className="mono small">{p.node || "–"}</td>
                  <td className="muted">{ago(p.created)}</td>
                  <td>
                    <button className="btn btn-sm" onClick={() => setLogPod(p)}>
                      Logs
                    </button>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      </Card>
      {logPod ? <LogModal pod={logPod} onClose={() => setLogPod(null)} /> : null}
    </>
  );
}

function LogModal({ pod, onClose }: { pod: KubePod; onClose: () => void }) {
  const containers = pod.containers ?? [];
  const [container, setContainer] = useState(containers[0] ?? "");
  const [lines, setLines] = useState(300);
  const [previous, setPrevious] = useState(false);
  const [auto, setAuto] = useState(false);
  const logs = usePoll(
    () =>
      api.get<{ logs: string }>(
        `/k8s/pods/${encodeURIComponent(pod.namespace)}/${encodeURIComponent(pod.name)}/logs?container=${encodeURIComponent(container)}&lines=${lines}&previous=${previous}`,
      ),
    auto ? 5000 : 0,
    [pod.namespace, pod.name, container, lines, previous, auto],
  );

  return (
    <div className="modal-backdrop" onClick={onClose}>
      <div className="modal" style={{ maxWidth: 1000, maxHeight: "92vh", overflow: "auto" }} onClick={(e) => e.stopPropagation()}>
        <div className="card-head">
          <h2>
            <span className="faint">{pod.namespace}/</span>
            {pod.name}
          </h2>
          <button className="btn btn-sm" onClick={onClose}>
            Close
          </button>
        </div>
        <div className="card-body stack">
          <div className="row wrap">
            <select value={container} onChange={(e) => setContainer(e.target.value)} style={{ width: 200 }}>
              {containers.map((c) => (
                <option key={c} value={c}>
                  {c}
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
              <input type="checkbox" checked={previous} onChange={(e) => setPrevious(e.target.checked)} /> previous container
            </label>
            <label className="check small">
              <input type="checkbox" checked={auto} onChange={(e) => setAuto(e.target.checked)} /> auto-refresh
            </label>
            <span className="spacer" />
            <button className="btn btn-sm" onClick={() => void logs.refresh()}>
              {logs.loading ? <Spinner /> : null} Reload
            </button>
          </div>
          <ErrorBox error={logs.error} />
          {logs.data ? <pre className="log">{logs.data.logs || "(empty)"}</pre> : logs.loading ? <Loading /> : null}
        </div>
      </div>
    </div>
  );
}

function DeploymentsTab({ ns, showNs, match }: TabProps) {
  const deps = usePoll(() => api.get<KubeDeployment[]>(`/k8s/deployments${q(ns)}`), 15000, [ns]);
  const rows = (deps.data ?? []).filter(match);
  return (
    <Card title="Deployments" poll={deps} count={rows.length}>
      <div className="table-wrap">
        <table>
          <thead>
            <tr>
              <th>Name</th>
              <th className="num">Ready</th>
              <th className="num">Up-to-date</th>
              <th className="num">Available</th>
              <th>Images</th>
              <th>Age</th>
            </tr>
          </thead>
          <tbody>
            {rows.map((d) => (
              <tr key={d.namespace + "/" + d.name}>
                <td>
                  <span className="row">
                    <StatusDot status={d.ready >= d.desired ? "ok" : "fail"} />
                    <span>
                      <Name ns={d.namespace} name={d.name} showNs={showNs} />
                    </span>
                  </span>
                </td>
                <td className="num">
                  {d.ready}/{d.desired}
                </td>
                <td className="num">{d.updated}</td>
                <td className="num">{d.available}</td>
                <td className="mono small">{(d.images ?? []).join(", ")}</td>
                <td className="muted">{ago(d.created)}</td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
    </Card>
  );
}

function ServicesTab({ ns, showNs, match }: TabProps) {
  const svcs = usePoll(() => api.get<KubeService[]>(`/k8s/services${q(ns)}`), 30000, [ns]);
  const rows = (svcs.data ?? []).filter(match);
  return (
    <Card title="Services" poll={svcs} count={rows.length}>
      <div className="table-wrap">
        <table>
          <thead>
            <tr>
              <th>Name</th>
              <th>Type</th>
              <th>Cluster IP</th>
              <th>External</th>
              <th>Ports</th>
              <th>Selector</th>
              <th>Age</th>
            </tr>
          </thead>
          <tbody>
            {rows.map((s) => (
              <tr key={s.namespace + "/" + s.name}>
                <td>
                  <Name ns={s.namespace} name={s.name} showNs={showNs} />
                </td>
                <td>
                  <span className="badge">{s.type}</span>
                </td>
                <td className="mono small">{s.clusterIP || "–"}</td>
                <td className="mono small">{(s.externalIPs ?? []).join(", ") || "–"}</td>
                <td className="mono small">{(s.ports ?? []).join(", ") || "–"}</td>
                <td className="mono small">
                  {Object.entries(s.selector ?? {})
                    .map(([k, v]) => `${k}=${v}`)
                    .join(", ") || "–"}
                </td>
                <td className="muted">{ago(s.created)}</td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
    </Card>
  );
}

function RoutesTab({ ns, showNs, match }: TabProps) {
  const routes = usePoll(() => api.get<KubeHTTPRoute[]>(`/k8s/httproutes${q(ns)}`), 30000, [ns]);
  const rows = (routes.data ?? []).filter(match);
  return (
    <Card title="HTTPRoutes" poll={routes} count={rows.length}>
      <div className="table-wrap">
        <table>
          <thead>
            <tr>
              <th>Name</th>
              <th>Hostnames</th>
              <th>Gateway</th>
              <th>Rules</th>
              <th>Accepted</th>
              <th>Age</th>
            </tr>
          </thead>
          <tbody>
            {rows.map((r) => (
              <tr key={r.namespace + "/" + r.name}>
                <td>
                  <Name ns={r.namespace} name={r.name} showNs={showNs} />
                </td>
                <td className="mono small">{(r.hostnames ?? []).join(", ") || "–"}</td>
                <td className="mono small">{(r.gateways ?? []).join(", ") || "–"}</td>
                <td className="mono small">
                  {(r.rules ?? []).map((x, i) => (
                    <div key={i}>{x}</div>
                  ))}
                </td>
                <td>
                  <span className={`badge ${r.accepted === "True" ? "ok" : r.accepted === "False" ? "fail" : ""}`}>{r.accepted}</span>
                </td>
                <td className="muted">{ago(r.created)}</td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
    </Card>
  );
}

function KeyList({ keys }: { keys: Record<string, number> }) {
  const entries = Object.entries(keys).sort(([a], [b]) => a.localeCompare(b));
  if (!entries.length) return <span className="faint">–</span>;
  return (
    <span className="mono small">
      {entries.map(([k, n], i) => (
        <span key={k}>
          {i ? ", " : ""}
          {k} <span className="faint">({bytes(n)})</span>
        </span>
      ))}
    </span>
  );
}

function ConfigMapsTab({ ns, showNs, match }: TabProps) {
  const cms = usePoll(() => api.get<KubeKeyed[]>(`/k8s/configmaps${q(ns)}`), 30000, [ns]);
  const [open, setOpen] = useState<KubeKeyed | null>(null);
  const rows = (cms.data ?? []).filter(match);
  return (
    <>
      <Card title="ConfigMaps" poll={cms} count={rows.length}>
        <div className="table-wrap">
          <table>
            <thead>
              <tr>
                <th>Name</th>
                <th>Keys</th>
                <th>Age</th>
                <th />
              </tr>
            </thead>
            <tbody>
              {rows.map((c) => (
                <tr key={c.namespace + "/" + c.name}>
                  <td>
                    <Name ns={c.namespace} name={c.name} showNs={showNs} />
                  </td>
                  <td>
                    <KeyList keys={c.keys} />
                  </td>
                  <td className="muted">{ago(c.created)}</td>
                  <td>
                    <button className="btn btn-sm" onClick={() => setOpen(c)}>
                      View
                    </button>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      </Card>
      {open ? <ConfigMapModal cm={open} onClose={() => setOpen(null)} /> : null}
    </>
  );
}

function ConfigMapModal({ cm, onClose }: { cm: KubeKeyed; onClose: () => void }) {
  const d = usePoll(() => api.get<KubeConfigMap>(`/k8s/configmaps/${encodeURIComponent(cm.namespace)}/${encodeURIComponent(cm.name)}`), 0, [
    cm.namespace,
    cm.name,
  ]);
  return (
    <div className="modal-backdrop" onClick={onClose}>
      <div className="modal" style={{ maxWidth: 900, maxHeight: "92vh", overflow: "auto" }} onClick={(e) => e.stopPropagation()}>
        <div className="card-head">
          <h2>
            <span className="faint">{cm.namespace}/</span>
            {cm.name}
          </h2>
          <button className="btn btn-sm" onClick={onClose}>
            Close
          </button>
        </div>
        <div className="card-body stack">
          <ErrorBox error={d.error} />
          {d.loading && !d.data ? <Loading /> : null}
          {d.data
            ? Object.entries(d.data.data)
                .sort(([a], [b]) => a.localeCompare(b))
                .map(([k, v]) => (
                  <div key={k}>
                    <div className="mono small" style={{ marginBottom: 4 }}>
                      <strong>{k}</strong>
                    </div>
                    <pre className="log">{v || "(empty)"}</pre>
                  </div>
                ))
            : null}
          {d.data && Object.keys(d.data.binary).length ? (
            <div className="muted small">
              Binary keys not shown: <KeyList keys={d.data.binary} />
            </div>
          ) : null}
          {d.data && !Object.keys(d.data.data).length && !Object.keys(d.data.binary).length ? <div className="empty">No data</div> : null}
        </div>
      </div>
    </div>
  );
}

function SecretsTab({ ns, showNs, match }: TabProps) {
  const secrets = usePoll(() => api.get<KubeKeyed[]>(`/k8s/secrets${q(ns)}`), 30000, [ns]);
  const rows = (secrets.data ?? []).filter(match);
  return (
    <Card
      title="Secrets"
      poll={secrets}
      count={rows.length}
      note={<div className="info-box">Only key names and sizes are shown. Secret values never leave the cluster API.</div>}
    >
      <div className="table-wrap">
        <table>
          <thead>
            <tr>
              <th>Name</th>
              <th>Type</th>
              <th>Keys</th>
              <th>Age</th>
            </tr>
          </thead>
          <tbody>
            {rows.map((s) => (
              <tr key={s.namespace + "/" + s.name}>
                <td>
                  <Name ns={s.namespace} name={s.name} showNs={showNs} />
                </td>
                <td className="small">{s.type}</td>
                <td>
                  <KeyList keys={s.keys} />
                </td>
                <td className="muted">{ago(s.created)}</td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
    </Card>
  );
}
