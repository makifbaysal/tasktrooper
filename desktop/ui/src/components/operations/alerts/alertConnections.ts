import type { AlertToken, CreatedAlertToken, DeployEnv } from "@/api";
import { getApiBase } from "@/lib/apiBase";

/** The labels `domain.ValidDeployEnv` accepts for a connection's `default_env`. */
export const ALERT_DEFAULT_ENVS: DeployEnv[] = ["prod", "preprod", "stage", "local"];

export const DEFAULT_ALERT_ENV: DeployEnv = "prod";

export function isAlertEnv(value: string): value is DeployEnv {
  return ALERT_DEFAULT_ENVS.some((e) => e === value);
}

export const ALERT_LABEL_MAX = 100;

export const ALERT_DOCS_URL = "https://tasktrooper.ai/docs/incidents#alert-tokens";

export type AlertTool = "alertmanager" | "grafana" | "sentry" | "cloudMonitoring" | "curl";

export const ALERT_TOOLS: AlertTool[] = ["alertmanager", "grafana", "sentry", "cloudMonitoring", "curl"];

const TOOL_SOURCE: Record<Exclude<AlertTool, "curl">, string> = {
  alertmanager: "alertmanager",
  grafana: "grafana",
  sentry: "sentry",
  cloudMonitoring: "cloud_monitoring",
};

/** The base the alerter must call: the one `request()` uses, else this page's
 * own origin (an empty base is the dev server's proxy on the same origin). */
export function serverBase(): string {
  const base = getApiBase();
  if (base) return base;
  return typeof window === "undefined" ? "" : window.location.origin;
}

export function ingestUrl(base: string, ingestPath: string): string {
  return `${base.replace(/\/+$/, "")}${ingestPath}`;
}

export function serverHost(base: string): string | undefined {
  try {
    return new URL(base).host || undefined;
  } catch {
    return undefined;
  }
}

/** True when `base` is a loopback address: nothing off this computer can
 * reach it without a tunnel. */
export function isLocalOnly(base: string): boolean {
  let host: string;
  try {
    host = new URL(base).hostname.toLowerCase();
  } catch {
    return false;
  }
  host = host.replace(/^\[|\]$/g, "");
  return (
    host === "localhost" ||
    host.endsWith(".localhost") ||
    host === "::1" ||
    host === "0.0.0.0" ||
    /^127(\.\d{1,3}){3}$/.test(host)
  );
}

/** The desktop shell starts its server with PORT=0, so the address changes on
 * every launch and a tunnel set up for it has to be pointed again. */
export function portChangesOnRestart(): boolean {
  return typeof window !== "undefined" && Boolean(window.__tasktrooperDesktop?.apiBase);
}

export function withSource(url: string, source: string): string {
  return `${url}${url.includes("?") ? "&" : "?"}source=${encodeURIComponent(source)}`;
}

export function alertSnippet(tool: AlertTool, url: string, env: DeployEnv): string {
  if (tool === "curl") {
    return [
      `curl -X POST '${url}' \\`,
      `  -H 'Content-Type: application/json' \\`,
      `  -d '{`,
      `    "title": "Checkout is failing",`,
      `    "severity": "high",`,
      `    "env": "${env}",`,
      `    "detail": "5xx rate above 5% for 10 minutes",`,
      `    "fingerprint": "checkout-5xx",`,
      `    "resolved": false`,
      `  }'`,
    ].join("\n");
  }
  const target = withSource(url, TOOL_SOURCE[tool]);
  switch (tool) {
    case "alertmanager":
      return [
        "receivers:",
        "  - name: tasktrooper",
        "    webhook_configs:",
        `      - url: "${target}"`,
        "        send_resolved: true",
      ].join("\n");
    case "grafana":
      return [
        "apiVersion: 1",
        "contactPoints:",
        "  - orgId: 1",
        "    name: TaskTrooper",
        "    receivers:",
        "      - uid: tasktrooper",
        "        type: webhook",
        "        settings:",
        `          url: "${target}"`,
        "          httpMethod: POST",
      ].join("\n");
    case "sentry":
      return target;
    case "cloudMonitoring":
      return [
        "gcloud beta monitoring channels create \\",
        '  --display-name="TaskTrooper" \\',
        "  --type=webhook_tokenauth \\",
        `  --channel-labels=url='${target}'`,
      ].join("\n");
  }
}

/** The row a new connection adds to the list — built field by field so the
 * plaintext token never reaches the list or its cache. */
export function listedToken(created: CreatedAlertToken): AlertToken {
  return {
    id: created.id,
    label: created.label,
    default_env: created.default_env,
    created_at: created.created_at,
    last_used_at: null,
  };
}

export type LabelProblem = "required" | "tooLong";

export function labelProblem(label: string): LabelProblem | undefined {
  const trimmed = label.trim();
  if (trimmed === "") return "required";
  if ([...trimmed].length > ALERT_LABEL_MAX) return "tooLong";
  return undefined;
}
