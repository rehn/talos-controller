// Helpers for the drift view and role-based node selection.
// Kept dependency-free so they can be tested with `node --test`.

export const MISSING = "<missing>";

/** Renders a JSON-encoded config leaf for display. */
export function formatValue(v: string): string {
  if (v === MISSING) return "not set";
  let parsed: unknown;
  try {
    parsed = JSON.parse(v);
  } catch {
    return v;
  }
  if (typeof parsed === "string") return parsed;
  if (Array.isArray(parsed)) {
    if (parsed.length === 0) return "[]";
    if (parsed.every((x) => typeof x !== "object" || x === null)) return parsed.map(String).join(", ");
    return JSON.stringify(parsed);
  }
  if (parsed !== null && typeof parsed === "object") return JSON.stringify(parsed);
  return String(parsed);
}

/** "/machine/sysctls/x" -> "machine › sysctls › x"; "/#LinkConfig:eth0/mtu" -> "LinkConfig eth0 › mtu". */
export function prettyPath(p: string): string {
  return p
    .replace(/^\//, "")
    .split("/")
    .map((seg) => seg.replace(/~1/g, "/").replace(/~0/g, "~"))
    .map((seg) => (seg.startsWith("#") ? seg.slice(1).replace(":", " ") : seg))
    .join(" › ");
}

export interface RoleNode {
  name: string;
  role: string;
  reachable: boolean;
}

export type RoleFilter = "controlplane" | "worker" | "all";

export function nodesOfRole<T extends RoleNode>(nodes: T[], role: RoleFilter): T[] {
  return nodes.filter((n) => role === "all" || n.role === role);
}

/** Checkbox state selecting every reachable node of a role. */
export function roleSelection(nodes: RoleNode[], role: RoleFilter): Record<string, boolean> {
  const out: Record<string, boolean> = {};
  for (const n of nodesOfRole(nodes, role)) if (n.reachable) out[n.name] = true;
  return out;
}
