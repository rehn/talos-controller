// Thin fetch wrapper for the Go backend. All endpoints live under /api.

export class ApiError extends Error {
  status: number;
  constructor(status: number, message: string) {
    super(message);
    this.status = status;
  }
}

async function request<T>(method: string, path: string, body?: unknown): Promise<T> {
  const res = await fetch(`/api${path}`, {
    method,
    credentials: "same-origin",
    headers: body !== undefined || method !== "GET" ? { "Content-Type": "application/json" } : undefined,
    body: body !== undefined ? JSON.stringify(body) : method !== "GET" ? "{}" : undefined,
  });

  if (res.status === 401 && !path.startsWith("/auth/")) {
    if (typeof window !== "undefined" && !window.location.pathname.startsWith("/login")) {
      window.location.href = "/login/";
    }
    throw new ApiError(401, "not authenticated");
  }

  if (res.status === 204) return undefined as T;

  let data: unknown = null;
  try {
    data = await res.json();
  } catch {
    // ignore
  }

  if (res.status === 503 && (data as { code?: string } | null)?.code === "not_configured") {
    if (typeof window !== "undefined" && !window.location.pathname.startsWith("/settings")) {
      window.location.href = "/settings/?setup=1";
    }
  }

  if (!res.ok) {
    const msg = (data as { error?: string } | null)?.error ?? `HTTP ${res.status}`;
    throw new ApiError(res.status, msg);
  }
  return data as T;
}

export const api = {
  get: <T>(path: string) => request<T>("GET", path),
  post: <T>(path: string, body?: unknown) => request<T>("POST", path, body ?? {}),
};

// ---- Types mirroring the Go API ----

export type Status = "ok" | "warn" | "fail";

export interface Node {
  name: string;
  ip: string;
  role: "controlplane" | "worker" | "unknown";
  ready: boolean;
  schedulable: boolean;
  kubeletVersion: string;
  osImage: string;
  talosVersion: string;
  arch: string;
  reachable: boolean;
  error?: string;
  cpus: number;
  memTotal: number;
  memAvailable: number;
  load1: number;
  load5: number;
  bootTime: number;
  inKubernetes: boolean;
}

export interface Check {
  id: string;
  name: string;
  status: Status;
  detail: string;
  items?: string[];
}

export interface EtcdMember {
  id: string;
  hostname: string;
  nodeIp: string;
  isLearner: boolean;
  isLeader: boolean;
  dbSize: number;
  dbSizeInUse: number;
  raftIndex: number;
  errors: string[] | null;
  healthy: boolean;
  error?: string;
}

export interface EtcdInfo {
  members: EtcdMember[] | null;
  alarms: string[] | null;
  leader: string;
  error?: string;
}

export interface PodProblem {
  namespace: string;
  name: string;
  node: string;
  phase: string;
  reason: string;
  restarts: number;
  since: string;
}

export interface WorkloadStatus {
  kind: string;
  namespace: string;
  name: string;
  desired: number;
  ready: number;
  healthy: boolean;
}

export interface KubeEvent {
  namespace: string;
  object: string;
  reason: string;
  message: string;
  count: number;
  lastSeen: string;
}

export interface Workloads {
  podsTotal: number;
  podsRunning: number;
  problems: PodProblem[];
  controllers: WorkloadStatus[];
  events: KubeEvent[];
  namespaces: number;
}

export interface Overview {
  clusterName: string;
  kubernetesVersion: string;
  talosVersions: string[] | null;
  nodes: Node[];
  etcd: EtcdInfo;
  checks: Check[];
  status: Status;
  workloads?: Workloads;
  generatedAt: string;
}

export interface Service {
  id: string;
  state: string;
  healthy: boolean;
  healthKnown: boolean;
  message: string;
  lastChange: string;
  lastEvent: string;
}

export interface Mount {
  filesystem: string;
  mountedOn: string;
  size: number;
  available: number;
}

export interface MachineConfigInfo {
  installImage: string;
  type: string;
  yaml?: string;
}

export interface NodeDetail {
  node: Node;
  services: Service[];
  mounts: Mount[];
  config?: MachineConfigInfo;
  servicesError?: string;
  configError?: string;
}

export interface PatchResult {
  node: string;
  ok: boolean;
  dryRun: boolean;
  mode: string;
  details: string;
  warnings?: string[];
  error?: string;
}

export interface Release {
  version: string;
  publishedAt: string;
  url: string;
  prerelease: boolean;
  defaultKubernetes?: string;
}

export interface UpgradeNode {
  name: string;
  ip: string;
  role: string;
  reachable: boolean;
  talosVersion: string;
  kubeletVersion: string;
  installImage: string;
  arch: string;
}

export interface UpgradeInfo {
  nodes: UpgradeNode[];
  kubernetesVersion: string;
  latestTalos: Release | null;
  recommendedTalos: Release | null;
  latestKubernetes: Release | null;
  talosReleases: Release[];
  kubernetesReleases: Release[];
  releasesError?: string;
  releasesFetchedAt: string;
  notes: string[];
  controlPlaneIp: string;
}

export interface JobLine {
  time: string;
  msg: string;
}

export interface Job {
  id: string;
  kind: string;
  title: string;
  status: "running" | "succeeded" | "failed" | "cancelled";
  error?: string;
  startedAt: string;
  finishedAt?: string;
  log: JobLine[] | null;
}

export interface ContextInfo {
  name: string;
  cluster: string;
  endpoints: string[];
  nodes: string[];
}

export interface Prefs {
  context: string;
  endpoints: string[];
  nodes: string[];
}

export interface EndpointCheck {
  endpoint: string;
  ok: boolean;
  version?: string;
  hostname?: string;
  error?: string;
}

export interface Settings {
  configured: boolean;
  error?: string;
  talosconfig: {
    source: "none" | "ui" | "file";
    path?: string;
    defaultContext: string;
    contexts: ContextInfo[];
    error?: string;
  };
  kubeconfig: { source: "none" | "ui" | "file"; path?: string };
  prefs: Prefs;
  clusterName?: string;
  effectiveEndpoints: string[];
  checks?: EndpointCheck[];
}

export interface DriftDiff {
  path: string;
  values: Record<string, string>;
  majority: string;
  outliers: string[];
  patchable: boolean;
  patch?: string;
}

export interface DriftRole {
  role: string;
  nodes: string[];
  identical: number;
  diffs: DriftDiff[];
}

export interface DriftReport {
  roles: DriftRole[];
  errors: Record<string, string>;
  ignored: string[];
}

// ---- Kubernetes browser ----

export interface KubeDeployment {
  namespace: string;
  name: string;
  desired: number;
  ready: number;
  updated: number;
  available: number;
  images: string[] | null;
  created: string;
}

export interface KubePod {
  namespace: string;
  name: string;
  node: string;
  phase: string;
  status: string;
  ready: string;
  restarts: number;
  containers: string[] | null;
  created: string;
  cpuMilli: number | null;
  memBytes: number | null;
  cpuRequestMilli: number;
  cpuLimitMilli: number;
  memRequestBytes: number;
  memLimitBytes: number;
}

export interface KubePods {
  pods: KubePod[];
  metricsAvailable: boolean;
  metricsError?: string;
}

export interface KubeService {
  namespace: string;
  name: string;
  type: string;
  clusterIP: string;
  externalIPs: string[] | null;
  ports: string[] | null;
  selector: Record<string, string> | null;
  created: string;
}

export interface KubeHTTPRoute {
  namespace: string;
  name: string;
  hostnames: string[] | null;
  gateways: string[] | null;
  rules: string[] | null;
  accepted: "True" | "False" | "Unknown";
  created: string;
}

export interface KubeKeyed {
  namespace: string;
  name: string;
  type?: string;
  keys: Record<string, number>;
  created: string;
}

export interface KubeConfigMap {
  namespace: string;
  name: string;
  data: Record<string, string>;
  binary: Record<string, number>;
}
