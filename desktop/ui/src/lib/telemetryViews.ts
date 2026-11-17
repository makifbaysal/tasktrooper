import type { OpsStatus } from "@/components/operations/console/opsStatus";
import type { TelemetryHealth, TelemetrySeriesSet } from "@/lib/telemetryHub";

/** The telemetry product's overview, infrastructure, apps, profiling, LLM and
 * load-test reads (paths below its `/api/v1`). Only the fields the screens use. */

/** ok / warning / critical, plus `stale` (no data for five minutes) and a
 * serverless function's `idle` / `missing`. */
export type InfraStatus = string;

export function infraOpsStatus(status: InfraStatus | undefined): OpsStatus {
  if (status === "critical") return "critical";
  if (status === "warning") return "warning";
  if (status === "ok") return "healthy";
  return "notReporting";
}

const STATUS_RANK: Record<string, number> = { critical: 0, warning: 1, stale: 2, missing: 2, ok: 3, idle: 4 };

export function statusRank(status: string | undefined): number {
  return STATUS_RANK[status ?? ""] ?? 5;
}

// ---------------------------------------------------------------- service map

export type MapTier = "client" | "service" | "data" | "external";

export interface TelemetryMapNode {
  id: string;
  name: string;
  tier: MapTier | string;
  kind: string;
  system?: string;
  status: TelemetryHealth;
  rpm: number;
  errorRate: number;
  p95: number;
  incident?: { id: string; title: string; severity: string; state: string; open: number } | null;
}

export interface TelemetryMapEdge {
  id: string;
  from: string;
  to: string;
  callsPerMin: number;
  errorRate: number;
  p95: number;
  status: TelemetryHealth;
}

export interface TelemetryServiceMap {
  nodes: TelemetryMapNode[] | null;
  edges: TelemetryMapEdge[] | null;
}

export const MAP_TIERS: MapTier[] = ["client", "service", "data", "external"];

export interface MapLayoutNode extends TelemetryMapNode {
  x: number;
  y: number;
}

export interface MapLayout {
  width: number;
  height: number;
  nodeWidth: number;
  nodeHeight: number;
  columns: Array<{ tier: MapTier; x: number; count: number }>;
  nodes: MapLayoutNode[];
  edges: Array<TelemetryMapEdge & { path: string }>;
}

const NODE_W = 184;
const NODE_H = 44;
const COL_GAP = 96;
const ROW_GAP = 16;
const HEAD = 28;

/** Columns left to right by tier (callers, services, data, third parties),
 * worst first inside a column; only tiers that hold a node take a column. */
export function mapLayout(map: TelemetryServiceMap | undefined): MapLayout {
  const all = map?.nodes ?? [];
  const tiers = MAP_TIERS.filter((tier) => all.some((n) => (MAP_TIERS.includes(n.tier as MapTier) ? n.tier : "service") === tier));
  const rank = (n: TelemetryMapNode) => (n.status === "critical" ? 0 : n.status === "warning" ? 1 : 2);
  const nodes: MapLayoutNode[] = [];
  const columns = tiers.map((tier, col) => {
    const x = col * (NODE_W + COL_GAP);
    const members = all
      .filter((n) => (MAP_TIERS.includes(n.tier as MapTier) ? n.tier : "service") === tier)
      .sort((a, b) => rank(a) - rank(b) || b.rpm - a.rpm || a.name.localeCompare(b.name));
    members.forEach((n, row) => nodes.push({ ...n, x, y: HEAD + row * (NODE_H + ROW_GAP) }));
    return { tier, x, count: members.length };
  });
  const byId = new Map(nodes.map((n) => [n.id, n]));
  const edges = (map?.edges ?? []).flatMap((e) => {
    const from = byId.get(e.from);
    const to = byId.get(e.to);
    if (!from || !to || from === to) return [];
    const forward = to.x > from.x;
    const sx = forward ? from.x + NODE_W : from.x;
    const tx = forward ? to.x : to.x + NODE_W;
    const sy = from.y + NODE_H / 2;
    const ty = to.y + NODE_H / 2;
    const bend = forward ? Math.max(40, (tx - sx) / 2) : -60;
    return [{ ...e, path: `M ${sx} ${sy} C ${sx + bend} ${sy}, ${tx - bend} ${ty}, ${tx} ${ty}` }];
  });
  const rows = Math.max(0, ...columns.map((c) => c.count));
  return {
    width: Math.max(NODE_W, columns.length * NODE_W + Math.max(0, columns.length - 1) * COL_GAP),
    height: HEAD + rows * NODE_H + Math.max(0, rows - 1) * ROW_GAP,
    nodeWidth: NODE_W,
    nodeHeight: NODE_H,
    columns,
    nodes,
    edges,
  };
}

// ---------------------------------------------------------------- infrastructure

export interface TelemetryHost {
  id: string;
  hostId: string;
  os: string;
  arch: string;
  cluster: string;
  node: string;
  cpu: number;
  cpuCores: number;
  memory: number;
  memoryBytes: number;
  disk: number;
  netIn: number;
  netOut: number;
  load1: number;
  uptimeS: number;
  containers: number;
  lastSeen: number;
  status: InfraStatus;
  spark?: { cpu: number[] | null; memory: number[] | null } | null;
  gpus?: Array<{ index: number; model: string; utilization: number; memory: number; temperatureC: number }> | null;
}

export interface TelemetryContainer {
  id: string;
  name: string;
  image: string;
  host: string;
  namespace: string;
  pod: string;
  podUid: string;
  service: string;
  cpu: number;
  cpuLimit: number;
  memoryBytes: number;
  memoryLimit: number;
  restarts: number;
  throttled: number;
  status: InfraStatus;
}

export interface TelemetryProcess {
  pid: number;
  exe: string;
  command: string;
  cpu: number;
  memoryBytes: number;
  threads: number;
}

export interface TelemetryHostDetail extends TelemetryHost {
  series?: { cpu?: TelemetrySeriesSet; memory?: TelemetrySeriesSet; disk?: TelemetrySeriesSet; network?: TelemetrySeriesSet } | null;
  containerList: TelemetryContainer[] | null;
  processes: TelemetryProcess[] | null;
  services: string[] | null;
}

export interface TelemetryK8sCluster {
  name: string;
  nodes: number;
  nodesReady: number;
  pods: number;
  podsRunning: number;
  podsFailing: number;
  namespaces: number;
  workloads: number;
  cpuUsed: number;
  cpuAllocatable: number;
  memoryUsed: number;
  memoryAllocatable: number;
  warningEvents: number;
  lastSeen: number;
  status: InfraStatus;
}

export interface TelemetryK8sClusterDetail extends TelemetryK8sCluster {
  nodeList: Array<{ name: string; ready: boolean; cpu: number; cpuAllocatable: number; memoryBytes: number; memoryAllocatable: number; pods: number; host: string; status: InfraStatus }> | null;
  namespaceList: Array<{ name: string; pods: number; workloads: number; failing: number }> | null;
  series?: { cpu?: TelemetrySeriesSet; memory?: TelemetrySeriesSet } | null;
}

export interface TelemetryWorkload {
  cluster: string;
  namespace: string;
  kind: string;
  name: string;
  desired: number;
  ready: number;
  restarts: number;
  cpu: number;
  memoryBytes: number;
  service: string;
  status: InfraStatus;
}

export interface TelemetryPod {
  uid: string;
  name: string;
  namespace: string;
  cluster: string;
  node: string;
  phase: string;
  reason: string;
  ready: boolean;
  restarts: number;
  restartsInRange: number;
  cpu: number;
  cpuRequest: number;
  cpuLimit: number;
  memoryBytes: number;
  memoryRequest: number;
  memoryLimit: number;
  workloadKind: string;
  workload: string;
  service: string;
  status: InfraStatus;
  containers: Array<{ name: string; image: string; cpu: number; memoryBytes: number; restarts: number }> | null;
}

export interface TelemetryK8sEvent {
  ts: number;
  type: string;
  reason: string;
  message: string;
  objectKind: string;
  objectName: string;
  namespace: string;
  cluster: string;
  count: number;
}

export interface TelemetryPodDetail extends TelemetryPod {
  series?: { cpu?: TelemetrySeriesSet; memory?: TelemetrySeriesSet; restarts?: TelemetrySeriesSet } | null;
  events: TelemetryK8sEvent[] | null;
}

export interface TelemetryDatabase {
  id: string;
  name: string;
  engine: string;
  version: string;
  host: string;
  port: number;
  env: string;
  instanceClass: string;
  source: string;
  status: TelemetryHealth;
  cpu: number;
  memory: number;
  disk: number;
  diskGb: number;
  connections: number;
  maxConnections: number;
  p95: number;
  prevP95: number;
  slowPerMin: number;
  opsPerSec: number;
  hitRatio: number;
  nodes: Array<{ id: string; role: string; zone: string; cpu: number; memory: number; connections: number; replLagMs?: number | null }> | null;
  problem?: { title: string; detail: string; fix: string } | null;
  noData?: boolean;
  missing?: boolean;
  observed?: boolean;
}

export interface TelemetryDbQuery {
  id: string;
  text: string;
  serviceId: string;
  endpoint: string;
  perCall: number;
  avgMs: number;
  p95: number;
  rows: number;
  callsPerMin: number;
  share: number;
}

export interface TelemetryBrokerCluster {
  id: string;
  name: string;
  kind: string;
  version: string;
  endpoint: string;
  source: string;
  status: TelemetryHealth;
  totalLag: number;
  topics: number;
  groups: number;
  nodes: Array<{ id: string; host: string; status: TelemetryHealth }> | null;
  missing?: boolean;
}

export interface TelemetryConsumerGroup {
  clusterId: string;
  id: string;
  serviceId?: string;
  process?: string;
  members: number;
  state: string;
  rebalancesPerHour: number;
  trend: string;
  reads: Array<{ topic: string; partitionLag: number[] | null; consumePerSec: number; oldestAgeS?: number }> | null;
  lag: number;
  health: TelemetryHealth;
}

export interface TelemetryTopic {
  clusterId: string;
  name: string;
  partitions: number;
  inPerSec: number;
  retention: string;
  producers: string[] | null;
  lag: number;
  health: TelemetryHealth;
}

export interface TelemetryServerlessSummary {
  functions: number;
  invocations: number;
  errors: number;
  errorRate: number;
  throttles: number;
  coldStarts: number;
  costUsd: number;
  byProvider: Array<{ provider: string; functions: number; invocations: number; costUsd: number }> | null;
}

export interface TelemetryFunction {
  id: string;
  slug: string;
  name: string;
  provider: string;
  app: string;
  region: string;
  runtime: string;
  memoryMb?: number | null;
  timeoutS?: number | null;
  arch: string;
  triggers: string[] | null;
  service: string;
  invocations: number;
  errors: number;
  errorRate: number;
  throttles: number;
  p95Ms: number;
  p99Ms: number;
  coldStarts: number;
  coldStartRate: number;
  concurrencyMax: number;
  costUsd: number;
  status: InfraStatus;
  spark: number[] | null;
}

export interface TelemetryFunctionDetail extends TelemetryFunction {
  series?: { invocations?: TelemetrySeriesSet; duration?: TelemetrySeriesSet; concurrency?: TelemetrySeriesSet; coldStarts?: TelemetrySeriesSet } | null;
  recentErrors: Array<{ ts: number; traceId: string; message: string }> | null;
  children?: TelemetryFunction[] | null;
}

// ---------------------------------------------------------------- web & mobile apps

export interface TelemetryVital {
  id: string;
  label: string;
  value: number;
  good: number;
  poor: number;
  unit: string;
}

export interface TelemetryAppSummary {
  activeUsers: number;
  prevActiveUsers: number;
  sessions: number;
  pageViewsPerMin: number;
  avgSessionSec: number;
  errorRate: number;
  prevErrorRate: number;
  vitals: TelemetryVital[] | null;
}

export interface TelemetryRumPage {
  id: string;
  path: string;
  views: number;
  users: number;
  avgTime: number;
  lcp: number;
  inp: number;
  cls: number;
  errorRate: number;
  bounce: number;
}

export interface TelemetrySession {
  id: string;
  user: string;
  identified: boolean;
  country: string;
  device: string;
  startedAt: number;
  duration: number;
  pages: number;
  errors: number;
  entry: string;
  exit: string;
  hasReplay?: boolean;
}

export interface TelemetryRumError {
  fingerprint: string;
  type: string;
  message: string;
  view: string;
  count: number;
  sessions: number;
  crash: boolean;
  firstSeen: number;
  lastSeen: number;
  appVersion: string;
}

export interface TelemetryAppStart {
  kind: string;
  count: number;
  p50: number;
  p75: number;
  p95: number;
}

export type VitalRating = "good" | "needsImprovement" | "poor";

export function vitalRating(v: Pick<TelemetryVital, "value" | "good" | "poor">): VitalRating {
  if (v.value <= v.good) return "good";
  if (v.value <= v.poor) return "needsImprovement";
  return "poor";
}

export interface TelemetryReplaySummary {
  sessionId: string;
  app: string;
  platform: string;
  format: string;
  startedAt: number;
  durationMs: number;
  segments: number;
  user?: { id?: string; anonymousId?: string } | null;
  country?: string;
  device?: string;
  browser?: string;
  os?: string;
  appVersion?: string;
  firstView: string;
  views: number;
  errors: number;
  clicks: number;
  rageClicks: number;
}

export interface TelemetryReplayTimelineItem {
  ts: number;
  kind: string;
  label: string;
  detail?: string;
  viewName?: string;
  traceId?: string;
  status?: number;
  durationMs?: number;
}

export interface TelemetryReplayDetail {
  summary: TelemetryReplaySummary;
  segments: Array<{ segment: number; startTs: number; endTs: number; events: number; bytes: number }> | null;
  timeline: TelemetryReplayTimelineItem[] | null;
}

// ---------------------------------------------------------------- profiling

export const PROFILE_TYPES = ["cpu", "wall", "alloc_space", "alloc_objects", "inuse_space", "inuse_objects", "goroutines", "lock", "block"] as const;

export interface TelemetryProfileTypes {
  types: Array<{ type: string; unit: string; runtime: string; hosts: string[] | null; versions: string[] | null; routes: number }> | null;
}

export interface TelemetryFlameNode {
  name: string;
  value: number;
  self: number;
  children: TelemetryFlameNode[] | null;
}

export interface TelemetryFlamegraph {
  type: string;
  unit: string;
  total: number;
  profiles: number;
  root: TelemetryFlameNode;
}

export interface TelemetryTopFunctions {
  unit: string;
  total: number;
  functions: Array<{ name: string; self: number; total: number }> | null;
}

export interface FlameCell {
  key: string;
  name: string;
  value: number;
  self: number;
  depth: number;
  /** Share of the focused node's width, 0..1. */
  x: number;
  w: number;
  path: number[];
}

/** The node a path of child indexes leads to from the root, or the root when
 * the path no longer exists (the graph was re-read). */
export function flameNodeAt(root: TelemetryFlameNode, path: number[]): { node: TelemetryFlameNode; path: number[] } {
  let node = root;
  const walked: number[] = [];
  for (const i of path) {
    const next = node.children?.[i];
    if (!next) break;
    node = next;
    walked.push(i);
  }
  return { node, path: walked };
}

/** Icicle cells below the focused node, cut at `maxDepth` and at cells
 * narrower than `minWidth` (a share of the focused width). */
export function flameCells(root: TelemetryFlameNode, focus: number[] = [], { maxDepth = 24, minWidth = 0.004 } = {}): FlameCell[] {
  const start = flameNodeAt(root, focus);
  const out: FlameCell[] = [];
  const total = start.node.value || 1;
  const walk = (node: TelemetryFlameNode, depth: number, x: number, path: number[]) => {
    const w = node.value / total;
    if (w < minWidth || depth > maxDepth) return;
    out.push({ key: path.join("."), name: node.name, value: node.value, self: node.self, depth, x, w, path });
    let offset = x;
    (node.children ?? []).forEach((child, i) => {
      walk(child, depth + 1, offset, [...path, i]);
      offset += child.value / total;
    });
  };
  walk(start.node, 0, 0, start.path);
  return out;
}

/** A profile value in its unit: CPU and wall time arrive as nanoseconds. */
export function formatProfileValue(value: number, unit: string, locale: string): string {
  if (unit === "nanoseconds") {
    const ms = value / 1e6;
    if (ms >= 1000) return `${new Intl.NumberFormat(locale, { maximumFractionDigits: 2 }).format(ms / 1000)} s`;
    return `${new Intl.NumberFormat(locale, { maximumFractionDigits: ms < 10 ? 2 : 0 }).format(ms)} ms`;
  }
  if (unit === "bytes") return formatBytes(value, locale);
  return new Intl.NumberFormat(locale, { notation: "compact", maximumFractionDigits: 1 }).format(value);
}

// ---------------------------------------------------------------- LLM monitoring

export interface TelemetryAiTotals {
  calls: number;
  errors: number;
  errorRate: number;
  inputTokens: number;
  outputTokens: number;
  cacheReadTokens: number;
  costUsd: number | null;
  costPartial: boolean;
  p50Ms: number;
  p95Ms: number;
  ttftP95Ms?: number;
}

export interface TelemetryAiOverview {
  totals: TelemetryAiTotals;
  series?: { calls?: TelemetrySeriesSet; tokens?: TelemetrySeriesSet; cost?: TelemetrySeriesSet; latency?: TelemetrySeriesSet } | null;
  byModel: Array<{ provider: string; model: string; calls: number; errors: number; inputTokens: number; outputTokens: number; costUsd: number | null; p95Ms: number; priceKnown: boolean }> | null;
  byService: Array<{ service: string; calls: number; inputTokens: number; outputTokens: number; costUsd: number | null; models: string[] | null }> | null;
  byEndpoint: Array<{ service: string; route: string; method: string; calls: number; avgInputTokens: number; avgOutputTokens: number; costUsd: number | null; costPerCallUsd: number | null; p95Ms: number }> | null;
}

export interface TelemetryAiCall {
  ts: number;
  traceId: string;
  spanId: string;
  service: string;
  route: string;
  provider: string;
  model: string;
  operation: string;
  inputTokens: number;
  outputTokens: number;
  costUsd: number | null;
  durationMs: number;
  error?: string;
  finishReason?: string;
}

// ---------------------------------------------------------------- load tests

export interface TelemetryLoadScenario {
  id: string;
  name: string;
  target: string;
  source: string;
  steps: number;
  peakVus: number;
  duration: number;
  regions: string[] | null;
  state?: string;
}

export interface TelemetryLoadRunStep {
  key: string;
  count: number;
  errors: number;
  errorRate: number;
  rps: number;
  p50: number;
  p95: number;
  p99: number;
}

export interface TelemetryLoadRun {
  id: string;
  scenarioId: string;
  state: string;
  startedAt: number;
  finishedAt?: number;
  region?: string;
  error?: string;
  progress: number;
  rps: number;
  p95: number;
  errorRate: number;
  vus: number;
  thresholds: Array<{ name: string; passed: boolean }> | null;
  thresholdsPassed?: boolean;
  steps?: TelemetryLoadRunStep[] | null;
  statusCodes?: Record<string, number> | null;
}

export interface TelemetryRunSeries {
  ts: number[] | null;
  vus: number[] | null;
  rps: number[] | null;
  p95: number[] | null;
  errorRate: number[] | null;
}

export function isLiveRun(state: string): boolean {
  return state === "queued" || state === "running";
}

/** One column of a run's timeline as a chart line. */
export function runSeriesPoints(set: TelemetryRunSeries | undefined, key: "vus" | "rps" | "p95" | "errorRate", scale = 1): Array<[number, number | null]> {
  const ts = set?.ts ?? [];
  const values = set?.[key] ?? [];
  return ts.map((t, i): [number, number | null] => {
    const v = values[i];
    return [t < 1e11 ? t * 1000 : t, typeof v === "number" && Number.isFinite(v) ? v * scale : null];
  });
}

// ---------------------------------------------------------------- formatting

const BYTE_UNITS = ["B", "KB", "MB", "GB", "TB", "PB"];

export function formatBytes(n: number, locale: string): string {
  if (!Number.isFinite(n)) return "-";
  let v = Math.abs(n);
  let unit = 0;
  while (v >= 1024 && unit < BYTE_UNITS.length - 1) {
    v /= 1024;
    unit++;
  }
  return `${new Intl.NumberFormat(locale, { maximumFractionDigits: v < 10 && unit > 0 ? 1 : 0 }).format(Math.sign(n) * v)} ${BYTE_UNITS[unit]}`;
}

export function formatCores(n: number, locale: string): string {
  if (!Number.isFinite(n)) return "-";
  return new Intl.NumberFormat(locale, { maximumFractionDigits: n < 1 ? 2 : 1 }).format(n);
}

export function formatUsd(n: number | null | undefined, locale: string): string {
  if (n === null || n === undefined || !Number.isFinite(n)) return "-";
  return new Intl.NumberFormat(locale, { style: "currency", currency: "USD", maximumFractionDigits: n > 0 && n < 1 ? 4 : 2 }).format(n);
}

/** "3d 4h", "2h 5m", "4m 10s", "12s". */
export function formatDuration(seconds: number): string {
  if (!Number.isFinite(seconds) || seconds < 0) return "-";
  const s = Math.round(seconds);
  const d = Math.floor(s / 86_400);
  const h = Math.floor((s % 86_400) / 3600);
  const m = Math.floor((s % 3600) / 60);
  const r = s % 60;
  if (d > 0) return `${d}d ${h}h`;
  if (h > 0) return `${h}h ${m}m`;
  if (m > 0) return `${m}m ${r}s`;
  return `${r}s`;
}

/** used / limit as 0..100, 0 without a limit. */
export function usagePercent(used: number, limit: number): number {
  if (!Number.isFinite(used) || !Number.isFinite(limit) || limit <= 0) return 0;
  return Math.max(0, Math.min(100, (used / limit) * 100));
}

export function usageTone(percent: number): "success" | "warning" | "destructive" {
  if (percent >= 90) return "destructive";
  if (percent >= 75) return "warning";
  return "success";
}

export function seriesFromValues(values: number[] | null | undefined): Array<[number, number | null]> {
  return (values ?? []).map((v, i): [number, number | null] => [i, Number.isFinite(v) ? v : null]);
}
