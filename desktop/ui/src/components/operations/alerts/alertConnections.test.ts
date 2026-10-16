import { afterEach, describe, expect, it } from "vitest";
import {
  ALERT_TOOLS,
  alertSnippet,
  ingestUrl,
  isLocalOnly,
  labelProblem,
  listedToken,
  portChangesOnRestart,
  serverBase,
  withSource,
} from "@/components/operations/alerts/alertConnections";

const URL_ = "https://alerts.example.com/v1/alerts/ingest/tt_alert_abc";

afterEach(() => {
  delete window.__tasktrooperDesktop;
});

describe("alertConnections", () => {
  it("builds the ingest URL on the API base, else this page's origin", () => {
    expect(ingestUrl("http://127.0.0.1:5000/", "/v1/alerts/ingest/t")).toBe("http://127.0.0.1:5000/v1/alerts/ingest/t");
    expect(serverBase()).toBe(window.location.origin);
    window.__tasktrooperDesktop = { apiBase: "http://127.0.0.1:61234/" } as Window["__tasktrooperDesktop"];
    expect(serverBase()).toBe("http://127.0.0.1:61234");
    expect(portChangesOnRestart()).toBe(true);
  });

  it("knows a loopback server from a reachable one", () => {
    for (const base of ["http://127.0.0.1:4000", "http://localhost:5173", "http://[::1]:80", "http://app.localhost", "http://0.0.0.0:1"]) {
      expect(isLocalOnly(base)).toBe(true);
    }
    for (const base of ["https://tt.example.com", "http://10.0.0.4:8080", "not a url", ""]) {
      expect(isLocalOnly(base)).toBe(false);
    }
    expect(portChangesOnRestart()).toBe(false);
  });

  it("adds the tool as ?source= and keeps an existing query", () => {
    expect(withSource(URL_, "grafana")).toBe(`${URL_}?source=grafana`);
    expect(withSource(`${URL_}?env=stage`, "sentry")).toBe(`${URL_}?env=stage&source=sentry`);
  });

  it("writes a snippet per tool around the URL", () => {
    expect(alertSnippet("alertmanager", URL_, "prod")).toContain(`webhook_configs:\n      - url: "${URL_}?source=alertmanager"`);
    expect(alertSnippet("alertmanager", URL_, "prod")).toContain("send_resolved: true");
    expect(alertSnippet("grafana", URL_, "prod")).toContain("type: webhook");
    expect(alertSnippet("grafana", URL_, "prod")).toContain(`url: "${URL_}?source=grafana"`);
    expect(alertSnippet("sentry", URL_, "prod")).toBe(`${URL_}?source=sentry`);
    expect(alertSnippet("cloudMonitoring", URL_, "prod")).toContain("--type=webhook_tokenauth");
    expect(alertSnippet("cloudMonitoring", URL_, "prod")).toContain(`--channel-labels=url='${URL_}?source=cloud_monitoring'`);
    const curl = alertSnippet("curl", URL_, "stage");
    expect(curl).toContain(`curl -X POST '${URL_}'`);
    const body = curl.slice(curl.indexOf("-d '") + 4, curl.lastIndexOf("'"));
    expect(JSON.parse(body)).toEqual({
      title: "Checkout is failing",
      severity: "high",
      env: "stage",
      detail: "5xx rate above 5% for 10 minutes",
      fingerprint: "checkout-5xx",
      resolved: false,
    });
    expect(ALERT_TOOLS).toHaveLength(5);
  });

  it("keeps the token out of the listed row", () => {
    const row = listedToken({
      id: "t1",
      label: "Alertmanager",
      default_env: "prod",
      created_at: "2026-10-16T10:00:00Z",
      token: "tt_alert_secret",
      ingest_path: "/v1/alerts/ingest/tt_alert_secret",
    });
    expect(row).toEqual({ id: "t1", label: "Alertmanager", default_env: "prod", created_at: "2026-10-16T10:00:00Z", last_used_at: null });
    expect(JSON.stringify(row)).not.toContain("tt_alert_secret");
  });

  it("checks the label the way the server does", () => {
    expect(labelProblem("   ")).toBe("required");
    expect(labelProblem("a".repeat(100))).toBeUndefined();
    expect(labelProblem("a".repeat(101))).toBe("tooLong");
    expect(labelProblem("🚨".repeat(100))).toBeUndefined();
  });
});
