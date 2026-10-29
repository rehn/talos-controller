"use client";

import Link from "next/link";
import { useSearchParams } from "next/navigation";
import { Suspense, useEffect, useRef, useState } from "react";
import { api, type Job } from "@/lib/api";
import { usePoll } from "@/lib/hooks";
import { ago, dateTime, duration, time } from "@/lib/format";
import { Confirm, ErrorBox, Loading, PageHead, RefreshButton, Spinner } from "@/components/ui";

export default function JobsPage() {
  return (
    <Suspense fallback={<Loading />}>
      <JobsRouter />
    </Suspense>
  );
}

function JobsRouter() {
  const id = useSearchParams().get("id");
  return id ? <JobDetail id={id} /> : <JobList />;
}

function JobStatus({ status }: { status: Job["status"] }) {
  const cls = status === "succeeded" ? "ok" : status === "running" ? "accent" : status === "failed" ? "fail" : "warn";
  return (
    <span className={`badge ${cls}`}>
      {status === "running" ? <Spinner /> : null}
      {status}
    </span>
  );
}

function jobDuration(j: Job): string {
  const end = j.finishedAt ? new Date(j.finishedAt).getTime() : Date.now();
  return duration((end - new Date(j.startedAt).getTime()) / 1000);
}

function JobList() {
  const jobs = usePoll(() => api.get<Job[]>("/jobs"), 5000);
  return (
    <>
      <PageHead title="Jobs" subtitle="Upgrades, reboots and other long-running operations">
        <RefreshButton onClick={jobs.refresh} lastUpdated={jobs.lastUpdated} />
      </PageHead>
      <ErrorBox error={jobs.error} />
      {jobs.loading && !jobs.data ? <Loading /> : null}
      {jobs.data ? (
        <div className="card">
          {jobs.data.length === 0 ? (
            <div className="empty">No jobs yet</div>
          ) : (
            <div className="table-wrap">
              <table>
                <thead>
                  <tr>
                    <th>Job</th>
                    <th>Status</th>
                    <th>Started</th>
                    <th>Duration</th>
                  </tr>
                </thead>
                <tbody>
                  {jobs.data.map((j) => (
                    <tr key={j.id}>
                      <td>
                        <Link href={`/jobs/?id=${j.id}`}>
                          <strong>{j.title}</strong>
                        </Link>
                        {j.error ? <div className="faint small">{j.error}</div> : null}
                      </td>
                      <td>
                        <JobStatus status={j.status} />
                      </td>
                      <td className="muted">{dateTime(j.startedAt)}</td>
                      <td className="muted">{jobDuration(j)}</td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          )}
        </div>
      ) : null}
    </>
  );
}

function JobDetail({ id }: { id: string }) {
  const [running, setRunning] = useState(true);
  const job = usePoll(() => api.get<Job>(`/jobs/${id}`), running ? 3000 : 0, [id, running]);
  const [cancelOpen, setCancelOpen] = useState(false);
  const logRef = useRef<HTMLPreElement>(null);
  const j = job.data;

  useEffect(() => {
    if (j && j.status !== "running") setRunning(false);
  }, [j]);

  useEffect(() => {
    const el = logRef.current;
    if (el) el.scrollTop = el.scrollHeight;
  }, [j?.log?.length]);

  return (
    <>
      <div className="small" style={{ marginBottom: 8 }}>
        <Link href="/jobs/">← Jobs</Link>
      </div>
      <PageHead
        title={j?.title ?? "Job"}
        subtitle={j ? `started ${ago(j.startedAt)} · ${jobDuration(j)}` : undefined}
      >
        {j ? <JobStatus status={j.status} /> : null}
        {j?.status === "running" ? (
          <button className="btn btn-danger" onClick={() => setCancelOpen(true)}>
            Cancel
          </button>
        ) : null}
      </PageHead>
      <ErrorBox error={job.error} />
      {j?.error ? <div className="err-box" style={{ marginBottom: 16 }}>{j.error}</div> : null}
      {!j && job.loading ? <Loading /> : null}
      {j ? (
        <pre className="log" ref={logRef} style={{ maxHeight: "70vh" }}>
          {(j.log ?? []).map((l) => `${time(l.time)}  ${l.msg}`).join("\n") || "(no output yet)"}
        </pre>
      ) : null}
      <Confirm
        open={cancelOpen}
        title="Cancel job?"
        danger
        confirmLabel="Cancel job"
        onClose={() => setCancelOpen(false)}
        onConfirm={async () => {
          await api.post(`/jobs/${id}/cancel`);
          await job.refresh();
        }}
      >
        <p>
          The job stops after the current step. A node that is already rebooting into a new version will still finish
          that upgrade.
        </p>
      </Confirm>
    </>
  );
}
