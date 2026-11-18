export function bytes(n: number | undefined | null): string {
  if (!n || n <= 0) return "0 B";
  const units = ["B", "KiB", "MiB", "GiB", "TiB"];
  let i = 0;
  let v = n;
  while (v >= 1024 && i < units.length - 1) {
    v /= 1024;
    i++;
  }
  return `${v >= 100 || i === 0 ? v.toFixed(0) : v.toFixed(1)} ${units[i]}`;
}

export function pct(part: number, total: number): number {
  if (!total) return 0;
  return Math.max(0, Math.min(100, (part / total) * 100));
}

export function duration(seconds: number): string {
  if (seconds < 0 || !isFinite(seconds)) return "–";
  const d = Math.floor(seconds / 86400);
  const h = Math.floor((seconds % 86400) / 3600);
  const m = Math.floor((seconds % 3600) / 60);
  if (d > 0) return `${d}d ${h}h`;
  if (h > 0) return `${h}h ${m}m`;
  if (m > 0) return `${m}m`;
  return `${Math.floor(seconds)}s`;
}

export function uptime(bootTime: number): string {
  if (!bootTime) return "–";
  return duration(Date.now() / 1000 - bootTime);
}

export function ago(ts: string | Date | null | undefined): string {
  if (!ts) return "–";
  const t = typeof ts === "string" ? new Date(ts) : ts;
  if (isNaN(t.getTime()) || t.getFullYear() < 2000) return "–";
  const s = (Date.now() - t.getTime()) / 1000;
  if (s < 5) return "just now";
  return `${duration(s)} ago`;
}

export function time(ts: string | undefined): string {
  if (!ts) return "";
  const t = new Date(ts);
  return t.toLocaleTimeString([], { hour: "2-digit", minute: "2-digit", second: "2-digit" });
}

export function dateTime(ts: string | undefined): string {
  if (!ts) return "–";
  const t = new Date(ts);
  if (isNaN(t.getTime())) return "–";
  return t.toLocaleString([], { year: "numeric", month: "short", day: "numeric", hour: "2-digit", minute: "2-digit" });
}

export function millicores(m: number | null | undefined): string {
  if (m === null || m === undefined) return "–";
  return m >= 1000 ? `${(m / 1000).toFixed(2)} cores` : `${m}m`;
}
