import { describe, expect, it } from "vitest";
import type { DataSource, MetricSeries } from "@/api";
import {
  buildCreateRequest,
  buildPatchRequest,
  emptyDraft,
  fieldErrorText,
  fieldHelp,
  fieldLabel,
  headerRowValid,
  headersToText,
  kindDescription,
  kindLabel,
  newHeaderRow,
  optionLabel,
  secretsToResupply,
  seriesLabel,
  sourceAddress,
  toChartSeries,
  unusedCloudAccounts,
  validateDraft,
  visibleFields,
  type DataSourceDraft,
} from "@/components/operations/sources/dataSourceSchema";
import { cloudAccount, dataSource, gcpMonitoringKind, prometheusKind, savedSource } from "@/test/dataSourceFixtures";

function draftWith(patch: Partial<DataSourceDraft> & { values?: Record<string, string> }, source?: DataSource) {
  const base = emptyDraft(prometheusKind, source);
  return { ...base, ...patch, values: { ...base.values, ...(patch.values ?? {}) } };
}

describe("schema-driven fields", () => {
  it("starts a select at its placeholder option and hides fields whose show_when does not match", () => {
    const draft = emptyDraft(prometheusKind);
    expect(draft.values.auth).toBe("none");
    expect(visibleFields(prometheusKind, draft).map((f) => f.key)).toEqual(["url", "auth", "headers"]);
    const bearer = draftWith({ values: { auth: "bearer" } });
    expect(visibleFields(prometheusKind, bearer).map((f) => f.key)).toEqual(["url", "auth", "token", "headers"]);
    const basic = draftWith({ values: { auth: "basic" } });
    expect(visibleFields(prometheusKind, basic).map((f) => f.key)).toEqual(["url", "auth", "username", "password", "headers"]);
  });

  it("prefills non-secret config on edit and never a secret", () => {
    const draft = emptyDraft(prometheusKind, savedSource);
    expect(draft.name).toBe("Prod Prometheus");
    expect(draft.values).toMatchObject({ url: "https://prom.example.com", auth: "bearer", token: "" });
    expect(draft.headers.headers).toEqual([]);
  });
});

describe("validateDraft", () => {
  it("requires shown required fields only", () => {
    expect(validateDraft(prometheusKind, emptyDraft(prometheusKind))).toEqual({ url: "required" });
    const bearer = draftWith({ values: { url: "http://localhost:9090", auth: "bearer" } });
    expect(validateDraft(prometheusKind, bearer)).toEqual({ token: "required" });
  });

  it("rejects a URL that is not http(s)", () => {
    expect(validateDraft(prometheusKind, draftWith({ values: { url: "ftp://x" } }))).toEqual({ url: "invalid" });
  });

  it("keeps a stored secret when left empty, unless it is removed", () => {
    expect(validateDraft(prometheusKind, emptyDraft(prometheusKind, savedSource), savedSource)).toEqual({});
    const removed = { ...emptyDraft(prometheusKind, savedSource), removed: ["token"] };
    expect(validateDraft(prometheusKind, removed, savedSource)).toEqual({ token: "required" });
  });

  it("refuses framing headers and a value with no name", () => {
    expect(headerRowValid(newHeaderRow("X-Scope-OrgID", "t1"))).toBe(true);
    expect(headerRowValid(newHeaderRow("", ""))).toBe(true);
    expect(headerRowValid(newHeaderRow("Host", "evil"))).toBe(false);
    expect(headerRowValid(newHeaderRow("Metadata-Flavor", "Google"))).toBe(false);
    expect(headerRowValid(newHeaderRow("X-aws-ec2-metadata-token", "t"))).toBe(false);
    expect(headerRowValid(newHeaderRow("", "orphan"))).toBe(false);
    expect(headerRowValid(newHeaderRow("Bad Name", "x"))).toBe(false);
    const draft = draftWith({ values: { url: "http://localhost:9090" }, headers: { headers: [newHeaderRow("Content-Length", "1")] } });
    expect(validateDraft(prometheusKind, draft)).toEqual({ headers: "invalid" });
  });
});

describe("requests", () => {
  it("create routes secrets by schema, joins headers and leaves hidden or empty fields out", () => {
    const draft = draftWith({
      name: "  ",
      values: { url: "http://localhost:9090", auth: "bearer", token: "s3cret", username: "ghost" },
      headers: { headers: [newHeaderRow("X-Scope-OrgID", "tenant-1"), newHeaderRow("", ""), newHeaderRow("X-Extra", " v ")] },
    });
    expect(buildCreateRequest(prometheusKind, draft)).toEqual({
      kind: "prometheus",
      config: { url: "http://localhost:9090", auth: "bearer" },
      secrets: { token: "s3cret", headers: "X-Scope-OrgID: tenant-1\nX-Extra: v" },
    });
  });

  it("patch keeps an untouched secret, sends a typed one and \"\" only for an explicit removal", () => {
    const untouched = emptyDraft(prometheusKind, savedSource);
    expect(buildPatchRequest(prometheusKind, untouched, savedSource)).toEqual({ config: {}, secrets: {} });
    const typed = { ...untouched, values: { ...untouched.values, token: "new" }, removed: ["headers"] };
    expect(buildPatchRequest(prometheusKind, typed, savedSource).secrets).toEqual({ token: "new", headers: "" });
    const renamed = { ...untouched, name: "Renamed" };
    expect(buildPatchRequest(prometheusKind, renamed, savedSource).name).toBe("Renamed");
  });

  it("does not send a field the auth switch hid", () => {
    const none = { ...emptyDraft(prometheusKind, savedSource), values: { url: "https://prom.example.com", auth: "none" } };
    expect(buildPatchRequest(prometheusKind, none, savedSource)).toEqual({ config: { auth: "none" }, secrets: {} });
  });

  it("sends only what changed since the form opened, so a stale form cannot hide a secret saved since", () => {
    const opened = { ...savedSource, config: { url: "https://prom.example.com", auth: "none" }, secrets_set: [] };
    const draft = { ...emptyDraft(prometheusKind, opened), name: "Renamed" };
    const latest = { ...savedSource, config: { url: "https://prom.example.com", auth: "bearer" } };
    expect(buildPatchRequest(prometheusKind, draft, latest)).toEqual({ config: {}, secrets: {}, name: "Renamed" });
    const moved = { ...draft, values: { ...draft.values, url: "https://mimir.example.com" } };
    expect(buildPatchRequest(prometheusKind, moved, latest).config).toEqual({ url: "https://mimir.example.com" });
  });

  it("sends a select default the stored source never held", () => {
    const bare = { ...savedSource, config: { url: "https://prom.example.com" }, secrets_set: [] };
    expect(buildPatchRequest(prometheusKind, emptyDraft(prometheusKind, bare), bare).config).toEqual({ auth: "none" });
  });
});

describe("a move to another host", () => {
  const moved = (url: string, extra: Partial<DataSourceDraft> = {}) => {
    const d = emptyDraft(prometheusKind, savedSource);
    return { ...d, ...extra, values: { ...d.values, url, ...(extra.values ?? {}) } };
  };

  it("is a change of scheme, host or port, not of path", () => {
    expect(secretsToResupply(prometheusKind, moved("https://prom.example.com/prometheus"), savedSource)).toEqual([]);
    expect(secretsToResupply(prometheusKind, moved("https://PROM.example.com.:443/api/prom"), savedSource)).toEqual([]);
    expect(secretsToResupply(prometheusKind, moved("https://other.example.com"), savedSource)).toEqual(["token", "headers"]);
    expect(secretsToResupply(prometheusKind, moved("http://prom.example.com"), savedSource)).toEqual(["token", "headers"]);
    expect(secretsToResupply(prometheusKind, moved("https://prom.example.com:8443"), savedSource)).toEqual(["token", "headers"]);
  });

  it("makes every stored secret required again, typed or removed", () => {
    expect(validateDraft(prometheusKind, moved("https://other.example.com"), savedSource)).toEqual({
      token: "secrets_required",
      headers: "secrets_required",
    });
    const answered = moved("https://other.example.com", { values: { token: "fresh" }, removed: ["headers"] });
    expect(validateDraft(prometheusKind, answered, savedSource)).toEqual({});
    expect(buildPatchRequest(prometheusKind, answered, savedSource).secrets).toEqual({ token: "fresh", headers: "" });
  });

  it("does not ask for a stored secret whose field the same save hides", () => {
    const draft = moved("https://other.example.com", { values: { auth: "none" }, removed: ["headers"] });
    expect(secretsToResupply(prometheusKind, draft, savedSource)).toEqual(["headers"]);
    expect(validateDraft(prometheusKind, draft, savedSource)).toEqual({});
    expect(buildPatchRequest(prometheusKind, draft, savedSource).secrets).toEqual({ headers: "" });
  });

  it("follows the server when it saw a move this side did not", () => {
    const draft = { ...emptyDraft(prometheusKind, savedSource), mustResupply: true };
    expect(secretsToResupply(prometheusKind, draft, savedSource)).toEqual(["token", "headers"]);
  });
});

describe("locale fallback", () => {
  const dict: Record<string, string> = {
    "operations.sources.kinds.prometheus.label": "Prometheus (localized)",
    "operations.sources.fields.url.label": "Address",
    "operations.sources.kinds.prometheus.fields.url.help": "Prometheus-specific help",
    "operations.sources.fields.auth.options.bearer": "Bearer (localized)",
    "operations.sources.errors.required": "Needed.",
    "operations.sources.errors.byField.url.invalid": "Bad URL.",
  };
  const t = (key: string) => dict[key] ?? key;
  const loki = {
    kind: "loki",
    label: "Loki",
    description: "Server-side description",
    capabilities: [],
    fields: [],
  };
  const endpoint = { ...prometheusKind.fields[0], key: "endpoint", label: "Endpoint", help: "Server help" };

  it("prefers the kind's own entry, then the shared field entry, then the server's text", () => {
    expect(kindLabel(t, prometheusKind)).toBe("Prometheus (localized)");
    expect(kindLabel(t, loki)).toBe("Loki");
    expect(kindDescription(t, loki)).toBe("Server-side description");
    expect(fieldLabel(t, "prometheus", prometheusKind.fields[0])).toBe("Address");
    expect(fieldHelp(t, "prometheus", prometheusKind.fields[0])).toBe("Prometheus-specific help");
    expect(fieldLabel(t, "loki", endpoint)).toBe("Endpoint");
    expect(fieldHelp(t, "loki", endpoint)).toBe("Server help");
    expect(optionLabel(t, "prometheus", prometheusKind.fields[1], "bearer")).toBe("Bearer (localized)");
    expect(optionLabel(t, "prometheus", prometheusKind.fields[1], "sigv4")).toBe("sigv4");
    expect(fieldErrorText(t, "url", "invalid")).toBe("Bad URL.");
    expect(fieldErrorText(t, "name", "required")).toBe("Needed.");
    expect(fieldErrorText(t, "name", "brand_new_code")).toBe("brand_new_code");
  });
});

describe("query results", () => {
  const series = (name: string, labels: Record<string, string> = {}): MetricSeries => ({
    name,
    labels,
    points: [
      [1_000, 1],
      [2_000, null],
    ],
  });

  it("labels a series by name and its sorted labels", () => {
    expect(seriesLabel(series("up", { job: "api", instance: "a:9090" }))).toBe('up{instance="a:9090", job="api"}');
    expect(seriesLabel(series("up"))).toBe("up");
    expect(seriesLabel(series('{queue="jobs"}', { queue: "jobs" }))).toBe('{queue="jobs"}');
  });

  it("maps every series to a chart series, cycling colours 1..8", () => {
    const out = toChartSeries(Array.from({ length: 10 }, (_, i) => series("up", { i: String(i) })));
    expect(out.map((s) => s.colorIndex)).toEqual([1, 2, 3, 4, 5, 6, 7, 8, 1, 2]);
    expect(out[0]).toEqual({ id: '0:up{i="0"}', label: 'up{i="0"}', points: [[1_000, 1], [2_000, null]], colorIndex: 1 });
    expect(new Set(out.map((s) => s.id)).size).toBe(10);
  });

  it("joins header rows into Name: value lines", () => {
    expect(headersToText([newHeaderRow(" A ", " 1 "), newHeaderRow("", "x"), newHeaderRow("B", "")])).toBe("A: 1\nB: ");
  });
});

describe("cloud accounts", () => {
  const gcp = cloudAccount();
  const gcp2 = cloudAccount({ id: "acc-gcp-2", label: "Acme staging" });
  const aws = cloudAccount({ id: "acc-aws", provider: "aws", label: "Acme AWS" });
  const vercel = cloudAccount({ id: "acc-vercel", provider: "vercel", label: "Acme Vercel" });

  it("offers, per provider, the first account no source reads through, with the kind that can", () => {
    const offers = unusedCloudAccounts([gcp, gcp2, aws, vercel], [prometheusKind, gcpMonitoringKind], []);
    expect(offers.map((o) => [o.account.id, o.kind.kind, o.field.key])).toEqual([["acc-gcp", "gcp_monitoring", "cloud_account_id"]]);
  });

  it("skips an account a source already reads through", () => {
    const used = dataSource({ id: "ds-gcp", kind: "gcp_monitoring", config: { cloud_account_id: "acc-gcp" } });
    expect(unusedCloudAccounts([gcp, gcp2], [gcpMonitoringKind], [used]).map((o) => o.account.id)).toEqual(["acc-gcp-2"]);
    expect(unusedCloudAccounts([gcp], [gcpMonitoringKind], [used])).toEqual([]);
  });

  it("shows a cloud kind's source by its account, not an address", () => {
    const source = dataSource({ kind: "gcp_monitoring", config: { cloud_account_id: "acc-gcp" } });
    expect(sourceAddress(source, gcpMonitoringKind, [gcp])).toBe("Acme production");
    expect(sourceAddress(source, gcpMonitoringKind, [])).toBe("acc-gcp");
    expect(sourceAddress(savedSource, prometheusKind, [gcp])).toBe("https://prom.example.com");
  });
});
