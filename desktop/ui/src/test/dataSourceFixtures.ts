import type {
  CloudAccount,
  DataSource,
  DataSourceField,
  DataSourceKindInfo,
  ObservabilityRecipe,
  ObservabilitySignal,
} from "@/api";

function field(partial: Partial<DataSourceField> & Pick<DataSourceField, "key" | "label" | "type">): DataSourceField {
  return {
    help: "",
    required: false,
    options: [],
    placeholder: "",
    show_when: {},
    secret: false,
    ...partial,
  };
}

/** The prometheus kind exactly as GET /v1/data-source-kinds lists it. */
export const prometheusKind: DataSourceKindInfo = {
  kind: "prometheus",
  label: "Prometheus compatible",
  description: "Prometheus, Grafana Mimir, Cortex, Thanos, VictoriaMetrics, Grafana Cloud, or anything else serving the Prometheus HTTP API.",
  capabilities: ["metrics"],
  fields: [
    field({
      key: "url",
      label: "URL",
      type: "url",
      required: true,
      placeholder: "http://localhost:9090",
      help: "The API base URL, without /api/v1. Mimir serves it under /prometheus, Grafana Cloud under /api/prom.",
    }),
    field({ key: "auth", label: "Authentication", type: "select", options: ["none", "bearer", "basic"], placeholder: "none" }),
    field({ key: "token", label: "Bearer token", type: "secret", required: true, secret: true, show_when: { auth: "bearer" } }),
    field({
      key: "username",
      label: "Username",
      type: "text",
      required: true,
      show_when: { auth: "basic" },
      help: "For Grafana Cloud, the instance ID.",
    }),
    field({
      key: "password",
      label: "Password",
      type: "secret",
      required: true,
      secret: true,
      show_when: { auth: "basic" },
      help: "For Grafana Cloud, an access policy token with metrics:read.",
    }),
    field({
      key: "headers",
      label: "Extra headers",
      type: "headers",
      secret: true,
      placeholder: "X-Scope-OrgID: tenant-1",
      help: "One \"Name: value\" per line.",
    }),
  ],
};

/** Google Cloud Monitoring as the server lists it: it reads through a connected account. */
export const gcpMonitoringKind: DataSourceKindInfo = {
  kind: "gcp_monitoring",
  label: "Google Cloud Monitoring",
  description: "Server description of Cloud Monitoring.",
  capabilities: ["metrics"],
  fields: [
    field({
      key: "cloud_account_id",
      label: "Google Cloud account",
      type: "cloud_account",
      provider: "gcp",
      required: true,
      help: "Its service account needs the Monitoring Viewer role (roles/monitoring.viewer) on the project.",
    }),
    field({ key: "project_id", label: "Project", type: "text", placeholder: "the account's project" }),
  ],
};

export function cloudAccount(overrides: Partial<CloudAccount> = {}): CloudAccount {
  return {
    id: "acc-gcp",
    provider: "gcp",
    label: "Acme production",
    meta: { project_id: "acme-prod" },
    status: "ok",
    created_at: "2026-10-15T09:00:00Z",
    updated_at: "2026-10-15T09:00:00Z",
    ...overrides,
  };
}

/** A kind the locale files know nothing about: everything must come from the server. */
export const lokiKind: DataSourceKindInfo = {
  kind: "made_up_logs",
  label: "Loki (server label)",
  description: "Server-side description of Loki.",
  capabilities: ["logs"],
  fields: [
    field({ key: "endpoint", label: "Endpoint (server label)", type: "url", required: true, help: "Server help for the endpoint." }),
  ],
};

export function dataSource(overrides: Partial<DataSource> = {}): DataSource {
  return {
    id: "ds-1",
    kind: "prometheus",
    name: "Prod Prometheus",
    config: { url: "https://prom.example.com", auth: "bearer" },
    secrets_set: ["token", "headers"],
    status: "ok",
    status_detail: "",
    info: { version: "2.53.0", detail: "" },
    verified_at: "2026-10-16T09:00:00Z",
    created_at: "2026-10-15T09:00:00Z",
    updated_at: "2026-10-16T09:00:00Z",
    capabilities: ["metrics"],
    ...overrides,
  };
}

export const savedSource = dataSource();

export const recipes: ObservabilityRecipe[] = [
  {
    id: "rabbitmq-prometheus",
    source_kind: "prometheus",
    applies_to: { technology: "rabbitmq", messaging_system: "rabbitmq", resource_kind: "", resource_params: {} },
    detect: { metric: "rabbitmq_queue_messages_ready" },
    params: ["queue", "vhost"],
    signals: { "queue.backlog": "sum(...)", "queue.consumers": "sum(...)" },
    origin: "catalog",
  },
  {
    id: "otel-http",
    source_kind: "prometheus",
    applies_to: { technology: "", messaging_system: "", resource_kind: "", resource_params: {} },
    detect: { metric: "http_server_request_duration_seconds_count" },
    params: null,
    signals: { "http.requests": "sum(...)" },
    origin: "catalog",
  },
];

export const signals: ObservabilitySignal[] = [
  { id: "queue.backlog", kind: "metric", unit: "messages", roles: ["worker"], aggregation: "gauge" },
  { id: "queue.consumers", kind: "metric", unit: "count", roles: ["worker"], aggregation: "gauge" },
  { id: "http.requests", kind: "metric", unit: "req/s", roles: ["backend"], aggregation: "rate" },
];
