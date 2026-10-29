"use client";

import { useSearchParams } from "next/navigation";
import { ChangeEvent, Suspense, useEffect, useState } from "react";
import { api, type EndpointCheck, type Settings } from "@/lib/api";
import { usePoll } from "@/lib/hooks";
import { Confirm, ErrorBox, Loading, PageHead, Spinner, StatusBadge, StatusDot } from "@/components/ui";

export default function SettingsPage() {
  return (
    <Suspense fallback={<Loading />}>
      <SettingsView />
    </Suspense>
  );
}

const SOURCE_LABEL: Record<string, string> = {
  none: "Not set",
  ui: "Uploaded here",
  file: "From file",
};

function lines(v: string): string[] {
  return v
    .split(/[\n,]/)
    .map((x) => x.trim())
    .filter(Boolean);
}

async function readFile(e: ChangeEvent<HTMLInputElement>): Promise<string | null> {
  const f = e.target.files?.[0];
  e.target.value = "";
  if (!f) return null;
  if (f.size > 1024 * 1024) throw new Error("File is larger than 1 MiB, that doesn't look like a config file");
  return f.text();
}

function SettingsView() {
  const setup = useSearchParams().get("setup") === "1";
  const st = usePoll(() => api.get<Settings>("/settings"), 0);
  const [s, setS] = useState<Settings | null>(null);
  const [checks, setChecks] = useState<EndpointCheck[] | null>(null);

  const [talosText, setTalosText] = useState("");
  const [kubeText, setKubeText] = useState("");
  const [context, setContext] = useState("");
  const [endpoints, setEndpoints] = useState("");
  const [nodes, setNodes] = useState("");

  const [busy, setBusy] = useState<string | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [removeTalos, setRemoveTalos] = useState(false);

  useEffect(() => {
    if (st.data) apply(st.data);
  }, [st.data]);

  function apply(d: Settings) {
    setS(d);
    setContext(d.prefs.context);
    setEndpoints(d.prefs.endpoints.join("\n"));
    setNodes(d.prefs.nodes.join("\n"));
    if (d.checks) setChecks(d.checks);
  }

  async function run(name: string, path: string, body?: unknown) {
    setBusy(name);
    setError(null);
    try {
      const d = await api.post<Settings>(path, body);
      apply(d);
      setChecks(d.checks ?? null);
      return true;
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e));
      return false;
    } finally {
      setBusy(null);
    }
  }

  const contexts = s?.talosconfig.contexts ?? [];
  const effectiveCtxName = context || s?.talosconfig.defaultContext || "";
  const ctx = contexts.find((c) => c.name === effectiveCtxName);
  const hasTalos = s ? s.talosconfig.source !== "none" : false;

  return (
    <>
      <PageHead title="Settings" subtitle="How Talos Controller connects to your cluster" />

      {setup && !s?.configured ? (
        <div className="info-box" style={{ marginBottom: 16 }}>
          <strong>Welcome!</strong> Add a talosconfig and the endpoint(s) of your control plane to get started.
        </div>
      ) : null}

      <ErrorBox error={st.error} />
      {st.loading && !s ? <Loading /> : null}

      {s ? (
        <div className="stack">
          {/* ---------- status ---------- */}
          <div className="card">
            <div className="card-head">
              <h2>Connection</h2>
              <div className="row">
                {s.configured ? <StatusBadge status="ok">Configured</StatusBadge> : <StatusBadge status="warn">Not connected</StatusBadge>}
                <button className="btn btn-sm" disabled={!s.configured || busy !== null} onClick={() => void run("test", "/settings/test")}>
                  {busy === "test" ? <Spinner /> : null} Test connection
                </button>
              </div>
            </div>
            <div className="card-body stack">
              {s.configured ? (
                <div className="row wrap">
                  <span className="muted">Cluster</span> <strong>{s.clusterName}</strong>
                  <span className="muted">· endpoints</span>
                  <code>{s.effectiveEndpoints.join(", ")}</code>
                </div>
              ) : (
                <div className="warn-box">{s.error ?? "Not configured"}</div>
              )}
              {checks && checks.length > 0 ? (
                <table>
                  <thead>
                    <tr>
                      <th>Endpoint</th>
                      <th>Result</th>
                    </tr>
                  </thead>
                  <tbody>
                    {checks.map((c) => (
                      <tr key={c.endpoint}>
                        <td className="mono">{c.endpoint}</td>
                        <td>
                          <span className="row">
                            <StatusDot status={c.ok ? "ok" : "fail"} />
                            {c.ok ? (
                              <span>
                                Talos <code>{c.version}</code>
                                {c.hostname ? <span className="muted"> on {c.hostname}</span> : null}
                              </span>
                            ) : (
                              <span className="small">{c.error}</span>
                            )}
                          </span>
                        </td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              ) : null}
            </div>
          </div>

          <ErrorBox error={error} />

          {/* ---------- talosconfig ---------- */}
          <div className="card">
            <div className="card-head">
              <h2>talosconfig</h2>
              <span className="badge">{SOURCE_LABEL[s.talosconfig.source]}</span>
            </div>
            <div className="card-body stack">
              {s.talosconfig.source === "file" ? (
                <div className="muted small">
                  Loaded from <code>{s.talosconfig.path}</code>. Uploading one here takes precedence.
                </div>
              ) : null}
              {s.talosconfig.source === "ui" ? (
                <div className="muted small">
                  Stored in the app's data directory (file mode 600). Key material is never sent back to the browser.
                </div>
              ) : null}
              <ErrorBox error={s.talosconfig.error} />
              <textarea
                rows={7}
                placeholder={"Paste your talosconfig here (e.g. the contents of ~/.talos/config), or choose a file below"}
                value={talosText}
                onChange={(e) => setTalosText(e.target.value)}
                spellCheck={false}
              />
              <div className="row wrap">
                <label className="btn">
                  Choose file…
                  <input
                    type="file"
                    style={{ display: "none" }}
                    onChange={async (e) => {
                      try {
                        const t = await readFile(e);
                        if (t !== null) setTalosText(t);
                      } catch (err) {
                        setError(err instanceof Error ? err.message : String(err));
                      }
                    }}
                  />
                </label>
                <span className="spacer" />
                {s.talosconfig.source === "ui" ? (
                  <button className="btn btn-danger" disabled={busy !== null} onClick={() => setRemoveTalos(true)}>
                    Remove
                  </button>
                ) : null}
                <button
                  className="btn btn-primary"
                  disabled={!talosText.trim() || busy !== null}
                  onClick={async () => {
                    if (await run("talos", "/settings/talosconfig", { content: talosText })) setTalosText("");
                  }}
                >
                  {busy === "talos" ? <Spinner /> : null} Save talosconfig
                </button>
              </div>
            </div>
          </div>

          {/* ---------- context + endpoints ---------- */}
          <div className="card">
            <div className="card-head">
              <h2>Context &amp; endpoints</h2>
            </div>
            <div className="card-body stack">
              {!hasTalos ? <div className="muted">Add a talosconfig first.</div> : null}
              <label className="field">
                Context
                <select value={context} disabled={!hasTalos} onChange={(e) => setContext(e.target.value)}>
                  <option value="">
                    Default{s.talosconfig.defaultContext ? ` (${s.talosconfig.defaultContext})` : ""}
                  </option>
                  {contexts.map((c) => (
                    <option key={c.name} value={c.name}>
                      {c.name}
                      {c.cluster && c.cluster !== c.name ? ` — ${c.cluster}` : ""}
                    </option>
                  ))}
                </select>
              </label>
              <div className="grid grid-2">
                <label className="field">
                  Endpoints
                  <textarea
                    rows={4}
                    disabled={!hasTalos}
                    value={endpoints}
                    onChange={(e) => setEndpoints(e.target.value)}
                    placeholder={ctx?.endpoints.length ? ctx.endpoints.join("\n") : "192.168.1.10\n192.168.1.11"}
                    spellCheck={false}
                  />
                  <span className="faint small" style={{ fontWeight: 400 }}>
                    Control plane IPs/hostnames (or a VIP) the app talks to, one per line.{" "}
                    {ctx?.endpoints.length
                      ? `Leave empty to use the talosconfig's: ${ctx.endpoints.join(", ")}`
                      : "This context has none, so set at least one."}
                  </span>
                </label>
                <label className="field">
                  Nodes (optional)
                  <textarea
                    rows={4}
                    disabled={!hasTalos}
                    value={nodes}
                    onChange={(e) => setNodes(e.target.value)}
                    placeholder={ctx?.nodes.length ? ctx.nodes.join("\n") : "192.168.1.10\n192.168.1.20"}
                    spellCheck={false}
                  />
                  <span className="faint small" style={{ fontWeight: 400 }}>
                    Only used when Kubernetes is unreachable. Normally nodes are discovered from Kubernetes.
                  </span>
                </label>
              </div>
              <div className="row" style={{ justifyContent: "flex-end" }}>
                <button
                  className="btn btn-primary"
                  disabled={!hasTalos || busy !== null}
                  onClick={() => void run("conn", "/settings/connection", { context, endpoints: lines(endpoints), nodes: lines(nodes) })}
                >
                  {busy === "conn" ? <Spinner /> : null} Save &amp; test
                </button>
              </div>
            </div>
          </div>

          {/* ---------- kubeconfig ---------- */}
          <div className="card">
            <div className="card-head">
              <h2>kubeconfig</h2>
              <span className="badge">{s.kubeconfig.source === "none" ? "Fetched via Talos" : SOURCE_LABEL[s.kubeconfig.source]}</span>
            </div>
            <div className="card-body stack">
              <div className="muted small">
                Optional. Without one, the app fetches an admin kubeconfig through the Talos API (requires an{" "}
                <code>os:admin</code> talosconfig).
                {s.kubeconfig.source === "file" ? (
                  <>
                    {" "}
                    Currently loaded from <code>{s.kubeconfig.path}</code>.
                  </>
                ) : null}
              </div>
              <textarea
                rows={5}
                placeholder="Paste a kubeconfig, or choose a file"
                value={kubeText}
                onChange={(e) => setKubeText(e.target.value)}
                spellCheck={false}
              />
              <div className="row wrap">
                <label className="btn">
                  Choose file…
                  <input
                    type="file"
                    style={{ display: "none" }}
                    onChange={async (e) => {
                      try {
                        const t = await readFile(e);
                        if (t !== null) setKubeText(t);
                      } catch (err) {
                        setError(err instanceof Error ? err.message : String(err));
                      }
                    }}
                  />
                </label>
                <span className="spacer" />
                {s.kubeconfig.source === "ui" ? (
                  <button
                    className="btn btn-danger"
                    disabled={busy !== null}
                    onClick={() => void run("kube-del", "/settings/kubeconfig/delete")}
                  >
                    Remove (fetch via Talos)
                  </button>
                ) : null}
                <button
                  className="btn btn-primary"
                  disabled={!kubeText.trim() || busy !== null}
                  onClick={async () => {
                    if (await run("kube", "/settings/kubeconfig", { content: kubeText })) setKubeText("");
                  }}
                >
                  {busy === "kube" ? <Spinner /> : null} Save kubeconfig
                </button>
              </div>
            </div>
          </div>
        </div>
      ) : null}

      <Confirm
        open={removeTalos}
        title="Remove the uploaded talosconfig?"
        danger
        confirmLabel="Remove"
        onClose={() => setRemoveTalos(false)}
        onConfirm={async () => {
          await run("talos-del", "/settings/talosconfig/delete");
        }}
      >
        <p>
          The app falls back to the talosconfig file from <code>TALOSCONFIG</code> if there is one, otherwise it
          disconnects until you add a new one.
        </p>
      </Confirm>
    </>
  );
}
