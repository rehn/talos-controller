"use client";

import { useRouter, useSearchParams } from "next/navigation";
import { Suspense, useEffect, useMemo, useState } from "react";
import { api, type Job, type UpgradeInfo } from "@/lib/api";
import { usePoll } from "@/lib/hooks";
import { ago, dateTime } from "@/lib/format";
import { Confirm, ErrorBox, Loading, PageHead, RefreshButton, RoleBadge } from "@/components/ui";

export default function UpgradesPage() {
  return (
    <Suspense fallback={<Loading />}>
      <Upgrades />
    </Suspense>
  );
}

function withTag(image: string, tag: string): string {
  const base = image || "ghcr.io/siderolabs/installer";
  const noDigest = base.split("@")[0];
  const slash = noDigest.lastIndexOf("/");
  const colon = noDigest.lastIndexOf(":");
  const repo = colon > slash ? noDigest.slice(0, colon) : noDigest;
  return `${repo}:${tag}`;
}

function cmp(a: string, b: string): number {
  const pa = a.replace(/^v/, "").split("-")[0].split(".").map(Number);
  const pb = b.replace(/^v/, "").split("-")[0].split(".").map(Number);
  for (let i = 0; i < 3; i++) {
    const d = (pa[i] || 0) - (pb[i] || 0);
    if (d) return d;
  }
  return 0;
}

function Upgrades() {
  const params = useSearchParams();
  const router = useRouter();
  const preselect = params.get("node");
  const info = usePoll(() => api.get<UpgradeInfo>("/upgrades"), 0);
  const d = info.data;

  const [version, setVersion] = useState("");
  const [selected, setSelected] = useState<Record<string, boolean>>({});
  const [overrides, setOverrides] = useState<Record<string, string>>({});
  const [stage, setStage] = useState(false);
  const [force, setForce] = useState(false);
  const [skipPreflight, setSkipPreflight] = useState(false);
  const [continueOnError, setContinueOnError] = useState(false);
  const [confirmOpen, setConfirmOpen] = useState(false);
  const [copied, setCopied] = useState(false);

  // Initialise selection + version once data arrives.
  useEffect(() => {
    if (!d) return;
    setVersion((v) => v || d.recommendedTalos?.version || d.latestTalos?.version || "");
    setSelected((s) => {
      if (Object.keys(s).length) return s;
      const out: Record<string, boolean> = {};
      for (const n of d.nodes) out[n.name] = preselect ? n.name === preselect : n.reachable;
      return out;
    });
  }, [d, preselect]);

  const plan = useMemo(() => {
    if (!d || !version) return [];
    const nodes = d.nodes.filter((n) => selected[n.name]);
    const cps = nodes.filter((n) => n.role === "controlplane");
    const rest = nodes.filter((n) => n.role !== "controlplane");
    return [...cps, ...rest].map((n) => ({
      node: n,
      image: overrides[n.name] || withTag(n.installImage, version),
      upToDate: n.talosVersion === version,
    }));
  }, [d, version, selected, overrides]);

  const latestK8s = d?.latestKubernetes?.version;
  const targetRelease = d?.talosReleases.find((r) => r.version === version);
  const k8sTarget = targetRelease?.defaultKubernetes || latestK8s || "";
  const k8sCmd = d?.controlPlaneIp
    ? `talosctl --nodes ${d.controlPlaneIp} upgrade-k8s --to ${k8sTarget.replace(/^v/, "")}`
    : "";
  const k8sBehind = d?.kubernetesVersion && k8sTarget && cmp(d.kubernetesVersion, k8sTarget) < 0;

  return (
    <>
      <PageHead title="Upgrades" subtitle="Rolling Talos upgrades and Kubernetes version tracking">
        <RefreshButton
          lastUpdated={info.lastUpdated}
          onClick={async () => {
            await api.get<UpgradeInfo>("/upgrades?refresh=1");
            await info.refresh();
          }}
        />
      </PageHead>
      <ErrorBox error={info.error} />
      {info.loading && !d ? <Loading label="Checking versions and releases…" /> : null}

      {d ? (
        <div className="stack">
          {d.releasesError ? (
            <div className="warn-box">Could not check GitHub for releases: {d.releasesError}</div>
          ) : null}

          <div className="grid grid-4">
            <div className="card stat">
              <div className="label">Talos running</div>
              <div className="value mono" style={{ fontSize: 18 }}>
                {Array.from(new Set(d.nodes.map((n) => n.talosVersion).filter(Boolean))).join(", ") || "–"}
              </div>
            </div>
            <div className="card stat">
              <div className="label">Latest Talos</div>
              <div className="value mono" style={{ fontSize: 18 }}>
                {d.latestTalos ? (
                  <a href={d.latestTalos.url} target="_blank" rel="noreferrer">
                    {d.latestTalos.version}
                  </a>
                ) : (
                  "–"
                )}
              </div>
              <div className="sub">{d.latestTalos ? `released ${ago(d.latestTalos.publishedAt)}` : ""}</div>
            </div>
            <div className="card stat">
              <div className="label">Kubernetes running</div>
              <div className="value mono" style={{ fontSize: 18 }}>
                {d.kubernetesVersion || "–"}
              </div>
            </div>
            <div className="card stat">
              <div className="label">Latest Kubernetes</div>
              <div className="value mono" style={{ fontSize: 18 }}>
                {latestK8s ?? "–"}
              </div>
              <div className="sub">checked {ago(d.releasesFetchedAt)}</div>
            </div>
          </div>

          {d.notes.length ? (
            <div className="info-box">
              <ul style={{ margin: 0, paddingLeft: 18 }}>
                {d.notes.map((n) => (
                  <li key={n}>{n}</li>
                ))}
              </ul>
            </div>
          ) : null}

          <div className="card">
            <div className="card-head">
              <h2>Talos rolling upgrade</h2>
              <div className="row">
                <span className="muted small">Target</span>
                <select value={version} onChange={(e) => setVersion(e.target.value)} style={{ width: 220 }}>
                  {d.talosReleases.map((r) => (
                    <option key={r.version} value={r.version}>
                      {r.version}
                      {r.prerelease ? " (pre-release)" : ""}
                      {r.version === d.recommendedTalos?.version ? " — recommended" : ""}
                    </option>
                  ))}
                  {version && !d.talosReleases.find((r) => r.version === version) ? (
                    <option value={version}>{version}</option>
                  ) : null}
                </select>
              </div>
            </div>
            <div className="table-wrap">
              <table>
                <thead>
                  <tr>
                    <th style={{ width: 32 }} />
                    <th>Node</th>
                    <th>Current</th>
                    <th>Installer image after upgrade</th>
                  </tr>
                </thead>
                <tbody>
                  {d.nodes.map((n) => {
                    const img = overrides[n.name] ?? withTag(n.installImage, version);
                    return (
                      <tr key={n.name}>
                        <td>
                          <input
                            type="checkbox"
                            disabled={!n.reachable}
                            checked={!!selected[n.name]}
                            onChange={(e) => setSelected({ ...selected, [n.name]: e.target.checked })}
                          />
                        </td>
                        <td>
                          <div className="row">
                            <strong>{n.name}</strong>
                            <RoleBadge role={n.role} />
                          </div>
                          <div className="faint small mono">
                            {n.ip} · {n.arch}
                          </div>
                        </td>
                        <td className="mono">
                          {n.talosVersion || "–"}
                          {n.talosVersion === version ? (
                            <div>
                              <span className="badge ok">up to date</span>
                            </div>
                          ) : null}
                        </td>
                        <td style={{ minWidth: 340 }}>
                          <input
                            type="text"
                            className="mono"
                            value={img}
                            onChange={(e) => setOverrides({ ...overrides, [n.name]: e.target.value })}
                          />
                          <div className="faint small">current: {n.installImage || "unknown"}</div>
                        </td>
                      </tr>
                    );
                  })}
                </tbody>
              </table>
            </div>
            <div className="card-body" style={{ borderTop: "1px solid var(--border)" }}>
              <div className="row wrap" style={{ gap: 18 }}>
                <label className="check">
                  <input type="checkbox" checked={stage} onChange={(e) => setStage(e.target.checked)} /> Stage (apply
                  on next boot — useful if files are locked)
                </label>
                <label className="check">
                  <input type="checkbox" checked={force} onChange={(e) => setForce(e.target.checked)} /> Force (skip
                  etcd health check on the node / reinstall same version)
                </label>
                <label className="check">
                  <input type="checkbox" checked={skipPreflight} onChange={(e) => setSkipPreflight(e.target.checked)} />{" "}
                  Skip preflight
                </label>
                <label className="check">
                  <input
                    type="checkbox"
                    checked={continueOnError}
                    onChange={(e) => setContinueOnError(e.target.checked)}
                  />{" "}
                  Continue on error
                </label>
                <span className="spacer" />
                <button
                  className="btn btn-primary"
                  disabled={!plan.length || plan.every((p) => p.upToDate && !force)}
                  onClick={() => setConfirmOpen(true)}
                >
                  Upgrade {plan.length} node{plan.length === 1 ? "" : "s"}
                </button>
              </div>
            </div>
          </div>

          <div className="card">
            <div className="card-head">
              <h2>Kubernetes</h2>
              {k8sBehind ? <span className="badge warn">update available</span> : <span className="badge ok">current</span>}
            </div>
            <div className="card-body stack">
              <div>
                Running <code>{d.kubernetesVersion || "?"}</code>.{" "}
                {targetRelease?.defaultKubernetes ? (
                  <>
                    Talos {version} ships with Kubernetes <code>{targetRelease.defaultKubernetes}</code>.
                  </>
                ) : latestK8s ? (
                  <>
                    Latest upstream release is <code>{latestK8s}</code> — check that your Talos version supports it.
                  </>
                ) : null}
              </div>
              <div className="muted small">
                Kubernetes upgrades update control plane static pods, kubelets and bootstrap manifests across the whole
                cluster. Run them after Talos is upgraded, from a machine with <code>talosctl</code>:
              </div>
              {k8sCmd ? (
                <div className="row">
                  <pre className="log" style={{ flex: 1, margin: 0 }}>
                    {k8sCmd + " --dry-run\n" + k8sCmd}
                  </pre>
                  <button
                    className="btn btn-sm"
                    onClick={() => {
                      void navigator.clipboard?.writeText(k8sCmd);
                      setCopied(true);
                      setTimeout(() => setCopied(false), 1500);
                    }}
                  >
                    {copied ? "Copied" : "Copy"}
                  </button>
                </div>
              ) : null}
            </div>
          </div>

          <div className="card">
            <div className="card-head">
              <h2>Recent Talos releases</h2>
            </div>
            <div className="table-wrap">
              <table>
                <thead>
                  <tr>
                    <th>Version</th>
                    <th>Released</th>
                    <th>Kubernetes</th>
                  </tr>
                </thead>
                <tbody>
                  {d.talosReleases.map((r) => (
                    <tr key={r.version}>
                      <td>
                        <a className="mono" href={r.url} target="_blank" rel="noreferrer">
                          {r.version}
                        </a>{" "}
                        {r.prerelease ? <span className="badge warn">pre-release</span> : null}
                        {r.version === d.latestTalos?.version ? <span className="badge accent">latest</span> : null}
                      </td>
                      <td className="muted">{dateTime(r.publishedAt)}</td>
                      <td className="mono">{r.defaultKubernetes || "–"}</td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          </div>
        </div>
      ) : null}

      <Confirm
        open={confirmOpen}
        title={`Upgrade ${plan.length} node(s) to ${version}?`}
        confirmLabel="Start rolling upgrade"
        typeToConfirm={version}
        onClose={() => setConfirmOpen(false)}
        onConfirm={async () => {
          const images: Record<string, string> = {};
          for (const p of plan) images[p.node.name] = p.image;
          const job = await api.post<Job>("/upgrades/talos", {
            nodes: plan.map((p) => p.node.name),
            version,
            images,
            stage,
            force,
            skipPreflight,
            continueOnError,
          });
          router.push(`/jobs/?id=${job.id}`);
        }}
      >
        <p>Nodes are upgraded one at a time, control planes first. Each node reboots and must come back Ready (and etcd healthy) before the next one starts.</p>
        <ol className="small" style={{ paddingLeft: 18 }}>
          {plan.map((p) => (
            <li key={p.node.name}>
              <strong>{p.node.name}</strong> <span className="faint">({p.node.talosVersion} →)</span>{" "}
              <code>{p.image}</code>
            </li>
          ))}
        </ol>
      </Confirm>
    </>
  );
}
