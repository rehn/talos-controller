"use client";

import { useRouter, useSearchParams } from "next/navigation";
import { Suspense, useEffect, useState } from "react";
import { api, type Job, type Node, type PatchResult } from "@/lib/api";
import { nodesOfRole, roleSelection, type RoleFilter } from "@/lib/drift";
import { usePoll } from "@/lib/hooks";
import { Confirm, ErrorBox, Loading, PageHead, RoleBadge, Spinner } from "@/components/ui";

const EXAMPLES: { label: string; patch: string }[] = [
  {
    label: "NTP servers",
    patch: `machine:
  time:
    servers:
      - time.cloudflare.com
      - pool.ntp.org`,
  },
  {
    label: "Sysctl",
    patch: `machine:
  sysctls:
    vm.max_map_count: "262144"`,
  },
  {
    label: "Kubelet extra args",
    patch: `machine:
  kubelet:
    extraArgs:
      rotate-server-certificates: "true"`,
  },
  {
    label: "Node label",
    patch: `machine:
  nodeLabels:
    topology.kubernetes.io/zone: home`,
  },
  {
    label: "JSON6902",
    patch: `- op: add
  path: /machine/network/nameservers
  value:
    - 1.1.1.1
    - 9.9.9.9`,
  },
];

const MODES = [
  { value: "auto", label: "Auto — apply live, reboot only if required" },
  { value: "no-reboot", label: "No reboot — fail if a reboot would be required" },
  { value: "staged", label: "Staged — apply on next reboot" },
  { value: "reboot", label: "Reboot — always reboot after applying" },
];

export default function ConfigPage() {
  return (
    <Suspense fallback={<Loading />}>
      <ConfigPatcher />
    </Suspense>
  );
}

const ROLE_LABEL: Record<RoleFilter, string> = {
  controlplane: "All control planes",
  worker: "All workers",
  all: "All nodes",
};

function ConfigPatcher() {
  const params = useSearchParams();
  const router = useRouter();
  const preNode = params.get("node");
  const preRole = params.get("role") as RoleFilter | null;
  const prePatch = params.get("patch");

  const nodes = usePoll(() => api.get<Node[]>("/nodes"), 0);
  const [selected, setSelected] = useState<Record<string, boolean>>({});
  // When set, the rollout targets the whole role (resolved when the job starts).
  const [role, setRole] = useState<RoleFilter | null>(null);
  const [patch, setPatch] = useState(prePatch ?? EXAMPLES[0].patch);
  const [mode, setMode] = useState("auto");
  const [continueOnError, setContinueOnError] = useState(false);
  const [results, setResults] = useState<PatchResult[] | null>(null);
  const [dryRunOK, setDryRunOK] = useState(false);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [confirmOpen, setConfirmOpen] = useState(false);

  // Pre-selection from links (node page, drift view).
  useEffect(() => {
    if (!nodes.data) return;
    if (preRole && ["controlplane", "worker", "all"].includes(preRole)) {
      setRole(preRole);
      setSelected(roleSelection(nodes.data, preRole));
    } else if (preNode) {
      setSelected(Object.fromEntries(preNode.split(",").map((n) => [n, true])));
    }
  }, [nodes.data, preNode, preRole]);

  // Any change invalidates the previous dry run.
  useEffect(() => setDryRunOK(false), [patch, mode, selected]);

  const all = nodes.data ?? [];
  const chosen = all.filter((n) => selected[n.name]).map((n) => n.name);

  function pickRole(r: RoleFilter) {
    setRole(r);
    setSelected(roleSelection(all, r));
  }

  async function dryRun() {
    setBusy(true);
    setError(null);
    try {
      const r = await api.post<PatchResult[]>("/config/patch", { nodes: chosen, patch, mode, dryRun: true });
      setResults(r);
      setDryRunOK(r.every((x) => x.ok));
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e));
      setResults(null);
    } finally {
      setBusy(false);
    }
  }

  async function rollout() {
    const body = role
      ? { role, patch, mode, continueOnError }
      : { nodes: chosen, patch, mode, continueOnError };
    const job = await api.post<Job>("/config/rollout", body);
    router.push(`/jobs/?id=${job.id}`);
  }

  const unreachableInRole = role ? nodesOfRole(all, role).filter((n) => !n.reachable).map((n) => n.name) : [];

  return (
    <>
      <PageHead
        title="Config patches"
        subtitle="Patch machine configuration on a role or specific nodes. Dry run first, then roll it out one node at a time."
      />
      <div className="grid grid-2" style={{ alignItems: "start" }}>
        <div className="card">
          <div className="card-head">
            <h2>Patch</h2>
            <div className="row wrap">
              {EXAMPLES.map((e) => (
                <button key={e.label} className="btn btn-sm" onClick={() => setPatch(e.patch)}>
                  {e.label}
                </button>
              ))}
            </div>
          </div>
          <div className="card-body stack">
            <textarea rows={16} value={patch} onChange={(e) => setPatch(e.target.value)} spellCheck={false} />
            <div className="faint small">
              Strategic merge patch (YAML, can contain multiple documents) or a JSON6902 patch list.
            </div>
            <label className="field">
              Apply mode
              <select value={mode} onChange={(e) => setMode(e.target.value)}>
                {MODES.map((m) => (
                  <option key={m.value} value={m.value}>
                    {m.label}
                  </option>
                ))}
              </select>
            </label>
          </div>
        </div>

        <div className="card">
          <div className="card-head">
            <h2>Targets</h2>
            <button
              className="btn btn-sm"
              onClick={() => {
                setRole(null);
                setSelected({});
              }}
            >
              Clear
            </button>
          </div>
          <div className="card-body">
            <div className="row wrap" style={{ marginBottom: 12 }}>
              {(Object.keys(ROLE_LABEL) as RoleFilter[]).map((r) => (
                <button
                  key={r}
                  className={`btn btn-sm ${role === r ? "btn-primary" : ""}`}
                  onClick={() => pickRole(r)}
                  disabled={!nodes.data}
                >
                  {ROLE_LABEL[r]}
                </button>
              ))}
            </div>
            <ErrorBox error={nodes.error} />
            {nodes.loading && !nodes.data ? <Loading /> : null}
            <div className="stack" style={{ gap: 6 }}>
              {all.map((n) => (
                <label key={n.name} className="check" style={{ marginTop: 6 }}>
                  <input
                    type="checkbox"
                    disabled={!n.reachable}
                    checked={!!selected[n.name]}
                    onChange={(e) => {
                      setRole(null);
                      setSelected({ ...selected, [n.name]: e.target.checked });
                    }}
                  />
                  <strong>{n.name}</strong>
                  <RoleBadge role={n.role} />
                  <span className="faint small mono">{n.ip}</span>
                  {!n.reachable ? <span className="badge fail">unreachable</span> : null}
                </label>
              ))}
            </div>
            {role ? (
              <div className="info-box small" style={{ marginTop: 12 }}>
                Targets <strong>{ROLE_LABEL[role].toLowerCase()}</strong>, resolved when the rollout starts.
                {unreachableInRole.length ? ` Unreachable nodes are skipped: ${unreachableInRole.join(", ")}.` : ""}
              </div>
            ) : null}
            <label className="check small" style={{ marginTop: 12 }}>
              <input type="checkbox" checked={continueOnError} onChange={(e) => setContinueOnError(e.target.checked)} />
              Continue with the next node if one fails
            </label>
            <div className="row" style={{ marginTop: 18, justifyContent: "flex-end" }}>
              <button className="btn" disabled={busy || chosen.length === 0} onClick={() => void dryRun()}>
                {busy ? <Spinner /> : null} Dry run
              </button>
              <button
                className="btn btn-primary"
                disabled={busy || chosen.length === 0 || !dryRunOK}
                title={dryRunOK ? "" : "Run a successful dry run first"}
                onClick={() => setConfirmOpen(true)}
              >
                Roll out
              </button>
            </div>
          </div>
        </div>
      </div>

      <div style={{ marginTop: 16 }} className="stack">
        <ErrorBox error={error} />
        {results?.map((r) => (
          <div className="card" key={r.node}>
            <div className="card-head">
              <div className="row">
                <strong>{r.node}</strong>
                {r.ok ? <span className="badge ok">dry run OK</span> : <span className="badge fail">failed</span>}
                <span className="faint small">{r.mode}</span>
                {r.ok && r.mode === "REBOOT" ? <span className="badge warn">needs reboot</span> : null}
              </div>
            </div>
            <div className="card-body stack">
              {r.error ? <div className="err-box">{r.error}</div> : null}
              {r.warnings?.length ? <div className="warn-box">{r.warnings.join("\n")}</div> : null}
              {r.details ? <pre className="log">{r.details}</pre> : null}
            </div>
          </div>
        ))}
      </div>

      <Confirm
        open={confirmOpen}
        title={role ? `Roll out to ${ROLE_LABEL[role].toLowerCase()}?` : `Roll out to ${chosen.length} node(s)?`}
        confirmLabel="Start rollout"
        danger={mode === "reboot"}
        onClose={() => setConfirmOpen(false)}
        onConfirm={rollout}
      >
        <p>
          The patch is dry-run on every node again, then applied <strong>one node at a time</strong> (control planes
          first). If a node needs a reboot, the next one only starts after it is back, Ready and (for control planes)
          etcd is healthy.
        </p>
        <p className="muted small">
          Mode <code>{mode}</code> on: {chosen.join(", ")}
        </p>
      </Confirm>
    </>
  );
}
