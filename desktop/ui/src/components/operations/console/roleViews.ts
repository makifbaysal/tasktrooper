import type { ComponentRole } from "@/api";

export type EntityTab =
  | "summary"
  | "endpoints"
  | "pages"
  | "crashes"
  | "queues"
  | "releases"
  | "errors"
  | "logs"
  | "traces"
  | "changes"
  | "issues"
  | "alerts"
  | "signals";

export const ENTITY_TABS: EntityTab[] = [
  "summary",
  "endpoints",
  "pages",
  "crashes",
  "queues",
  "releases",
  "errors",
  "logs",
  "traces",
  "changes",
  "issues",
  "alerts",
  "signals",
];

export type SignalUnit = "percent" | "ms" | "perSecond" | "seconds" | "cores";

// `id` is the signal catalog's id (catalog/observability/signals.yaml), the
// key a data-source binding fills; `label` is its key under
// `operations.signals`, `short` an optional shorter one for table headers.
export interface SignalSpec {
  id: string;
  label: string;
  short?: string;
  unit?: SignalUnit;
}

export interface ViewSpec {
  tabs: EntityTab[];
  golden: SignalSpec[];
  charts: SignalSpec[];
}

// Read from what the server already stores, not from a data source.
export const BUILTIN_SIGNALS = {
  errors24h: "errors.count_24h",
  openIssues: "incidents.open",
  lastDeploy: "deploy.last",
  deploysInRange: "deploy.count",
  resource: "env.resource",
} as const;

export const BUILTIN_TILES: SignalSpec[] = [
  { id: BUILTIN_SIGNALS.errors24h, label: "errors24h" },
  { id: BUILTIN_SIGNALS.openIssues, label: "openIssues" },
  { id: BUILTIN_SIGNALS.deploysInRange, label: "deploysInRange" },
  { id: BUILTIN_SIGNALS.lastDeploy, label: "lastDeploy" },
  { id: BUILTIN_SIGNALS.resource, label: "resource" },
];

export type BuiltinSignalId = (typeof BUILTIN_SIGNALS)[keyof typeof BUILTIN_SIGNALS];

export function isBuiltinSignal(id: string): id is BuiltinSignalId {
  return (Object.values(BUILTIN_SIGNALS) as string[]).includes(id);
}

const RESPONSE_TIME: SignalSpec = { id: "http.latency", label: "httpLatency", unit: "ms" };
const THROUGHPUT: SignalSpec = { id: "http.requests", label: "httpRequests", unit: "perSecond" };
const ERROR_RATE: SignalSpec = { id: "http.error_ratio", label: "httpErrorRatio", unit: "percent" };
const CPU: SignalSpec = { id: "infra.cpu", label: "infraCpu", unit: "cores" };
const BACKLOG: SignalSpec = { id: "queue.backlog", label: "queueBacklog" };
const CONSUME_RATE: SignalSpec = { id: "queue.consume_rate", label: "queueConsumeRate", unit: "perSecond" };
const BACKLOG_AGE: SignalSpec = { id: "queue.backlog_age", label: "queueBacklogAge", unit: "seconds" };
const PROCESS_ERRORS: SignalSpec = { id: "process.errors", label: "processErrors", unit: "perSecond" };
const LCP: SignalSpec = { id: "web.lcp", label: "webLcp", short: "webLcpShort", unit: "ms" };
const PAGE_VIEWS: SignalSpec = { id: "web.page_views", label: "webPageViews", unit: "perSecond" };
const INP: SignalSpec = { id: "web.inp", label: "webInp", short: "webInpShort", unit: "ms" };
const JS_ERRORS: SignalSpec = { id: "web.js_errors", label: "webJsErrors", unit: "perSecond" };
const CRASH_FREE: SignalSpec = { id: "mobile.crash_free_users", label: "mobileCrashFreeUsers", unit: "percent" };
const COLD_START: SignalSpec = { id: "mobile.cold_start", label: "mobileColdStart", unit: "ms" };
const ANR_RATE: SignalSpec = { id: "mobile.anr_rate", label: "mobileAnrRate", unit: "percent" };
const ROLLOUT: SignalSpec = { id: "mobile.rollout_fraction", label: "mobileRollout", unit: "percent" };
const CRASH_RATE: SignalSpec = { id: "mobile.crash_rate", label: "mobileCrashRate", unit: "percent" };

// The rest of the signal catalog: never a chart slot, but a component can
// still bind them on its Signals page.
const OTHER_SIGNALS: SignalSpec[] = [
  { id: "infra.memory", label: "infraMemory" },
  { id: "infra.instances", label: "infraInstances" },
  { id: "infra.restarts", label: "infraRestarts" },
  { id: "infra.throttles", label: "infraThrottles", unit: "perSecond" },
  { id: "queue.publish_rate", label: "queuePublishRate", unit: "perSecond" },
  { id: "queue.dead_letter", label: "queueDeadLetter" },
  { id: "queue.consumers", label: "queueConsumers" },
  { id: "web.sessions", label: "webSessions" },
  { id: "web.visitors", label: "webVisitors" },
  { id: "web.fcp", label: "webFcp", short: "webFcpShort", unit: "ms" },
  { id: "web.cls", label: "webCls", short: "webClsShort" },
  { id: "web.ttfb", label: "webTtfb", short: "webTtfbShort", unit: "ms" },
  { id: "logs.lines", label: "logsLines" },
  { id: "traces.spans", label: "tracesSpans" },
  { id: "errors.issues", label: "errorsIssues" },
];

const BASE_TABS: EntityTab[] = ["summary", "errors", "logs", "changes", "issues"];
// New Relic's order: the entity's own view (Endpoints, Pages, Queues) right
// after Summary, then errors, logs and traces, then what changed.
function tracedTabs(own: EntityTab): EntityTab[] {
  return ["summary", own, "errors", "logs", "traces", "changes", "issues", "alerts", "signals"];
}
// Alerts for every role: a custom-query rule can watch any component, and an
// alert notification links to `/operations/entity/<id>/alerts?rule=`.
const NO_RUNTIME: ViewSpec = { tabs: ["summary", "changes", "issues", "alerts"], golden: [], charts: [] };

// `golden` is the server's list for the role (catalog/observability/golden.yaml),
// the signals GET /v1/operations/golden answers.
export const ROLE_VIEWS: Record<ComponentRole, ViewSpec> = {
  backend: {
    tabs: tracedTabs("endpoints"),
    golden: [RESPONSE_TIME, THROUGHPUT, ERROR_RATE],
    charts: [RESPONSE_TIME, THROUGHPUT, ERROR_RATE, CPU],
  },
  worker: {
    tabs: tracedTabs("queues"),
    golden: [BACKLOG, CONSUME_RATE, PROCESS_ERRORS],
    charts: [BACKLOG, CONSUME_RATE, BACKLOG_AGE, PROCESS_ERRORS],
  },
  frontend: {
    tabs: tracedTabs("pages"),
    golden: [LCP, PAGE_VIEWS, INP],
    charts: [LCP, PAGE_VIEWS, INP, JS_ERRORS],
  },
  mobile: {
    tabs: ["summary", "crashes", "releases", "errors", "logs", "changes", "issues", "alerts", "signals"],
    golden: [CRASH_FREE, COLD_START, ANR_RATE],
    charts: [CRASH_FREE, COLD_START, ANR_RATE, ROLLOUT],
  },
  desktop: {
    tabs: [...BASE_TABS, "alerts", "signals"],
    golden: [CRASH_FREE, CRASH_RATE, COLD_START],
    charts: [CRASH_FREE, CRASH_RATE, COLD_START],
  },
  library: NO_RUNTIME,
  cli: NO_RUNTIME,
  infra: NO_RUNTIME,
  data: NO_RUNTIME,
  game: NO_RUNTIME,
  other: NO_RUNTIME,
};

export function viewFor(role: ComponentRole | undefined): ViewSpec {
  return (role && ROLE_VIEWS[role]) || NO_RUNTIME;
}

const SIGNAL_SPECS = new Map<string, SignalSpec>(
  [...Object.values(ROLE_VIEWS).flatMap((v) => v.charts), ...OTHER_SIGNALS].map((spec) => [spec.id, spec]),
);

export const CATALOG_SIGNALS: SignalSpec[] = [...SIGNAL_SPECS.values()];

/** The console's spec for a catalog signal id; one the console does not know
 * yet is labelled by its id. */
export function signalSpec(id: string): SignalSpec | undefined {
  return SIGNAL_SPECS.get(id);
}

export function isEntityTab(value: string | null | undefined): value is EntityTab {
  return ENTITY_TABS.some((t) => t === value);
}

export function hasTab(role: ComponentRole | undefined, tab: EntityTab): boolean {
  return viewFor(role).tabs.includes(tab);
}

export function entityPath(entityId: string, tab: EntityTab = "summary"): string {
  return `/operations/entity/${encodeURIComponent(entityId)}/${tab}`;
}

const CONSOLE_PAGES: Partial<Record<EntityTab, string>> = {
  errors: "/operations/errors",
  logs: "/operations/logs",
  changes: "/operations/changes",
  issues: "/operations/issues",
  alerts: "/operations/alerts",
};

/** The entity's own section, or for a role without it (a data or CLI
 * component's errors) the console-wide page filtered to the entity — never a
 * tab its page would only send back to Summary. */
export function entitySectionPath(entity: { id: string; role?: ComponentRole }, tab: EntityTab): string {
  if (hasTab(entity.role, tab)) return entityPath(entity.id, tab);
  const page = CONSOLE_PAGES[tab];
  return page ? `${page}?entity=${encodeURIComponent(entity.id)}` : entityPath(entity.id);
}

export function tabOrDefault(view: ViewSpec, tab: string | null | undefined): EntityTab {
  return view.tabs.find((t) => t === tab) ?? view.tabs[0];
}
