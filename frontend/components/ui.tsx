"use client";

import { ReactNode, useState } from "react";
import type { Status } from "@/lib/api";
import { ago } from "@/lib/format";

export function StatusDot({ status }: { status: Status | "none" }) {
  return <span className={`dot ${status === "none" ? "" : status}`} />;
}

const LABEL: Record<Status, string> = { ok: "Healthy", warn: "Degraded", fail: "Failing" };

export function StatusBadge({ status, children }: { status: Status; children?: ReactNode }) {
  return (
    <span className={`badge ${status}`}>
      <StatusDot status={status} />
      {children ?? LABEL[status]}
    </span>
  );
}

export function Meter({ value, warnAt = 75, failAt = 90 }: { value: number; warnAt?: number; failAt?: number }) {
  const cls = value >= failAt ? "fail" : value >= warnAt ? "warn" : "";
  return (
    <div className={`meter ${cls}`} title={`${value.toFixed(0)}%`}>
      <div style={{ width: `${Math.max(2, value)}%` }} />
    </div>
  );
}

export function ErrorBox({ error }: { error: string | null | undefined }) {
  if (!error) return null;
  return <div className="err-box">{error}</div>;
}

export function Loading({ label = "Loading…" }: { label?: string }) {
  return (
    <div className="empty row" style={{ justifyContent: "center" }}>
      <Spinner /> {label}
    </div>
  );
}

export function Spinner() {
  return (
    <svg className="spin" width="15" height="15" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2.5">
      <path d="M21 12a9 9 0 1 1-6.2-8.56" strokeLinecap="round" />
    </svg>
  );
}

export function PageHead({
  title,
  subtitle,
  children,
}: {
  title: ReactNode;
  subtitle?: ReactNode;
  children?: ReactNode;
}) {
  return (
    <div className="page-head">
      <div>
        <h1>{title}</h1>
        {subtitle ? <p>{subtitle}</p> : null}
      </div>
      {children ? <div className="page-actions">{children}</div> : null}
    </div>
  );
}

export function RefreshButton({
  onClick,
  lastUpdated,
}: {
  onClick: () => Promise<void> | void;
  lastUpdated?: Date | null;
}) {
  const [busy, setBusy] = useState(false);
  return (
    <>
      {lastUpdated ? <span className="faint small">updated {ago(lastUpdated)}</span> : null}
      <button
        className="btn"
        disabled={busy}
        onClick={async () => {
          setBusy(true);
          try {
            await onClick();
          } finally {
            setBusy(false);
          }
        }}
      >
        {busy ? <Spinner /> : null} Refresh
      </button>
    </>
  );
}

/**
 * Confirmation dialog. If `typeToConfirm` is set the user must type it exactly.
 */
export function Confirm({
  open,
  title,
  children,
  confirmLabel = "Confirm",
  danger = false,
  typeToConfirm,
  onConfirm,
  onClose,
}: {
  open: boolean;
  title: string;
  children?: ReactNode;
  confirmLabel?: string;
  danger?: boolean;
  typeToConfirm?: string;
  onConfirm: (typed: string) => Promise<void> | void;
  onClose: () => void;
}) {
  const [typed, setTyped] = useState("");
  const [busy, setBusy] = useState(false);
  const [err, setErr] = useState<string | null>(null);

  if (!open) return null;
  const blocked = typeToConfirm !== undefined && typed !== typeToConfirm;

  return (
    <div className="modal-backdrop" onClick={() => !busy && onClose()}>
      <div className="modal" onClick={(e) => e.stopPropagation()} role="dialog" aria-modal="true">
        <div className="card-head">
          <h2>{title}</h2>
        </div>
        <div className="card-body">
          {children}
          {typeToConfirm !== undefined ? (
            <label className="field" style={{ marginTop: 8 }}>
              <span>
                Type <code>{typeToConfirm}</code> to confirm
              </span>
              <input type="text" value={typed} autoFocus onChange={(e) => setTyped(e.target.value)} />
            </label>
          ) : null}
          {err ? (
            <div className="err-box" style={{ marginTop: 12 }}>
              {err}
            </div>
          ) : null}
          <div className="row" style={{ justifyContent: "flex-end", marginTop: 16 }}>
            <button className="btn" disabled={busy} onClick={onClose}>
              Cancel
            </button>
            <button
              className={danger ? "btn btn-danger solid" : "btn btn-primary"}
              disabled={busy || blocked}
              onClick={async () => {
                setBusy(true);
                setErr(null);
                try {
                  await onConfirm(typed);
                  setTyped("");
                  onClose();
                } catch (e) {
                  setErr(e instanceof Error ? e.message : String(e));
                } finally {
                  setBusy(false);
                }
              }}
            >
              {busy ? <Spinner /> : null}
              {confirmLabel}
            </button>
          </div>
        </div>
      </div>
    </div>
  );
}

export function RoleBadge({ role }: { role: string }) {
  if (role === "controlplane") return <span className="badge accent">control plane</span>;
  if (role === "worker") return <span className="badge">worker</span>;
  return <span className="badge">{role}</span>;
}
