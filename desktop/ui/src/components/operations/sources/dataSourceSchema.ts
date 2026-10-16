import type {
  DataSource,
  DataSourceCapability,
  DataSourceField,
  DataSourceKindInfo,
  MetricSeries,
  PatchDataSourceRequest,
  SaveDataSourceRequest,
} from "@/api";
import type { ChartSeries } from "@/components/operations/charts/TimeSeriesChart";

export type Translate = (key: string, params?: Record<string, string | number>) => string;

export interface HeaderRow {
  id: number;
  name: string;
  value: string;
}

/** What the add/edit form holds. Secret values start empty (they never
 * round-trip); `removed` lists stored secrets the user chose to drop. */
export interface DataSourceDraft {
  name: string;
  values: Record<string, string>;
  headers: Record<string, HeaderRow[]>;
  removed: string[];
  /** Set when the server answered `secrets_required` for an address change
   * this side did not see (a default port spelled out, say). */
  mustResupply?: boolean;
  /** The stored settings the draft was opened with: an edit sends only what
   * differs from them, so a form left open while someone else changed the
   * source does not put its stale values back. */
  base?: Record<string, string>;
}

export type FieldErrors = Record<string, string>;

const CHART_COLORS = 8;
const NS = "operations.sources";

// Mirrors domain.forbiddenHeaders: framing the HTTP client sets itself, and
// the headers that unlock a cloud metadata service.
const FORBIDDEN_HEADERS = new Set([
  "host",
  "content-length",
  "transfer-encoding",
  "connection",
  "upgrade",
  "te",
  "trailer",
  "metadata-flavor",
  "x-aws-ec2-metadata-token",
  "x-aws-ec2-metadata-token-ttl-seconds",
]);
const HEADER_NAME = /^[A-Za-z0-9!#$%&'*+\-.^_`|~]+$/;

let nextHeaderRow = 0;

export function newHeaderRow(name = "", value = ""): HeaderRow {
  return { id: nextHeaderRow++, name, value };
}

export function isSecretField(field: DataSourceField): boolean {
  return field.secret || field.type === "secret" || field.type === "headers";
}

export function fieldShown(field: DataSourceField, values: Record<string, string>): boolean {
  return Object.entries(field.show_when ?? {}).every(([key, want]) => (values[key] ?? "") === want);
}

export function visibleFields(kind: DataSourceKindInfo, draft: DataSourceDraft): DataSourceField[] {
  return kind.fields.filter((f) => fieldShown(f, draft.values));
}

function selectDefault(field: DataSourceField): string {
  return (field.options ?? []).includes(field.placeholder) ? field.placeholder : "";
}

export function emptyDraft(kind: DataSourceKindInfo, source?: DataSource | null): DataSourceDraft {
  const values: Record<string, string> = {};
  const headers: Record<string, HeaderRow[]> = {};
  const base: Record<string, string> = {};
  for (const field of kind.fields) {
    if (field.type === "headers") {
      headers[field.key] = [];
      continue;
    }
    if (isSecretField(field)) {
      values[field.key] = "";
      continue;
    }
    const stored = source?.config?.[field.key] ?? "";
    base[field.key] = stored;
    values[field.key] = stored || (field.type === "select" ? selectDefault(field) : "");
  }
  return { name: source?.name ?? "", values, headers, removed: [], ...(source ? { base } : {}) };
}

export function headersToText(rows: HeaderRow[] | undefined): string {
  return (rows ?? [])
    .filter((r) => r.name.trim() !== "")
    .map((r) => `${r.name.trim()}: ${r.value.trim()}`)
    .join("\n");
}

/** Splits a server placeholder like "X-Scope-OrgID: tenant-1" into the row's two inputs. */
export function headerPlaceholder(placeholder: string): { name: string; value: string } {
  const at = placeholder.indexOf(":");
  if (at < 0) return { name: placeholder.trim(), value: "" };
  return { name: placeholder.slice(0, at).trim(), value: placeholder.slice(at + 1).trim() };
}

export function headerRowValid(row: HeaderRow): boolean {
  const name = row.name.trim();
  if (name === "") return row.value.trim() === "";
  return HEADER_NAME.test(name) && !FORBIDDEN_HEADERS.has(name.toLowerCase()) && !/[\r\n\0]/.test(row.value);
}

export function headerRowsValid(rows: HeaderRow[] | undefined): boolean {
  return (rows ?? []).every(headerRowValid);
}

function draftValue(field: DataSourceField, draft: DataSourceDraft): string {
  if (field.type === "headers") return headersToText(draft.headers[field.key]);
  return (draft.values[field.key] ?? "").trim();
}

export function secretStored(source: DataSource | null | undefined, key: string): boolean {
  return Boolean(source?.secrets_set?.includes(key));
}

function validUrl(value: string): boolean {
  try {
    const u = new URL(value);
    return (u.protocol === "http:" || u.protocol === "https:") && u.hostname !== "";
  } catch {
    return false;
  }
}

// Mirrors the server's origin(): host case-insensitive, a trailing dot
// ignored, an omitted port read as 80/443.
function urlOrigin(raw: string): string | null {
  try {
    const u = new URL(raw.trim());
    const port = u.port || (u.protocol === "https:" ? "443" : u.protocol === "http:" ? "80" : "");
    return `${u.protocol}//${u.hostname.toLowerCase().replace(/\.$/, "")}:${port}`;
  } catch {
    return null;
  }
}

/** True when a shown URL field now points at another scheme, host or port
 * than the saved one; a path change is not a move. A value that does not
 * parse yet is not counted: the URL check reports it instead. */
export function addressMoved(kind: DataSourceKindInfo, draft: DataSourceDraft, source?: DataSource | null): boolean {
  if (!source) return false;
  return visibleFields(kind, draft).some((field) => {
    if (field.type !== "url") return false;
    const before = source.config?.[field.key] ?? "";
    const after = (draft.values[field.key] ?? "").trim();
    if (before === "" || before === after) return false;
    const next = urlOrigin(after);
    return next !== null && urlOrigin(before) !== next;
  });
}

/** The stored secrets this save must type again or remove: the server never
 * carries saved credentials to a new address (400 `secrets_required`). A
 * secret whose field the draft hides is dropped by the save anyway, so it
 * is not asked for. */
export function secretsToResupply(
  kind: DataSourceKindInfo,
  draft: DataSourceDraft,
  source?: DataSource | null,
): string[] {
  const stored = source?.secrets_set ?? [];
  if (stored.length === 0) return [];
  if (!draft.mustResupply && !addressMoved(kind, draft, source)) return [];
  const shown = new Set(visibleFields(kind, draft).map((field) => field.key));
  return stored.filter((key) => shown.has(key));
}

/** The checks worth making before a round trip; the server's schema check stays the authority. */
export function validateDraft(kind: DataSourceKindInfo, draft: DataSourceDraft, source?: DataSource | null): FieldErrors {
  const errors: FieldErrors = {};
  if (source && draft.name.trim() === "") errors.name = "required";
  const resupply = new Set(secretsToResupply(kind, draft, source));
  for (const field of visibleFields(kind, draft)) {
    const value = draftValue(field, draft);
    if (field.type === "headers" && !headerRowsValid(draft.headers[field.key])) {
      errors[field.key] = "invalid";
      continue;
    }
    if (value === "") {
      const stored = isSecretField(field) && secretStored(source, field.key);
      const removed = draft.removed.includes(field.key);
      if (stored && !removed && resupply.has(field.key)) {
        errors[field.key] = "secrets_required";
        continue;
      }
      if (field.required && !(stored && !removed)) errors[field.key] = "required";
      continue;
    }
    if (field.type === "url" && !validUrl(value)) errors[field.key] = "invalid";
  }
  return errors;
}

export function buildCreateRequest(kind: DataSourceKindInfo, draft: DataSourceDraft): SaveDataSourceRequest {
  const config: Record<string, string> = {};
  const secrets: Record<string, string> = {};
  for (const field of visibleFields(kind, draft)) {
    const value = draftValue(field, draft);
    if (value === "") continue;
    (isSecretField(field) ? secrets : config)[field.key] = value;
  }
  const name = draft.name.trim();
  return { kind: kind.kind, ...(name ? { name } : {}), config, secrets };
}

/** The server merges a PATCH into what it stores, so only what the person
 * changed is sent: a shown setting that differs from what the form opened
 * with ("" clears it), a secret when typed or removed. Hidden fields are left
 * out: the server drops their stored values itself. Sending every shown key
 * would let a form opened before another edit (auth switched to bearer and a
 * token saved, say) send its stale auth back and so hide — and wipe — that
 * token. */
export function buildPatchRequest(
  kind: DataSourceKindInfo,
  draft: DataSourceDraft,
  source: DataSource,
): PatchDataSourceRequest {
  const base = draft.base ?? source.config ?? {};
  const config: Record<string, string> = {};
  const secrets: Record<string, string> = {};
  for (const field of visibleFields(kind, draft)) {
    const value = draftValue(field, draft);
    if (!isSecretField(field)) {
      if (value !== (base[field.key] ?? "")) config[field.key] = value;
      continue;
    }
    if (draft.removed.includes(field.key)) secrets[field.key] = "";
    else if (value !== "") secrets[field.key] = value;
  }
  const patch: PatchDataSourceRequest = { config, secrets };
  const name = draft.name.trim();
  if (name !== source.name) patch.name = name;
  return patch;
}

function firstTranslated(t: Translate, keys: string[], fallback: string): string {
  for (const key of keys) {
    const text = t(key);
    if (text !== key) return text;
  }
  return fallback;
}

// A kind or field the locale files do not know yet still renders with the
// server's own English text, so a new kind needs no UI change.
export function kindLabel(t: Translate, kind: Pick<DataSourceKindInfo, "kind" | "label">): string {
  return firstTranslated(t, [`${NS}.kinds.${kind.kind}.label`], kind.label || kind.kind);
}

export function kindDescription(t: Translate, kind: Pick<DataSourceKindInfo, "kind" | "description">): string {
  return firstTranslated(t, [`${NS}.kinds.${kind.kind}.description`], kind.description);
}

export function fieldLabel(t: Translate, kind: string, field: DataSourceField): string {
  return firstTranslated(
    t,
    [`${NS}.kinds.${kind}.fields.${field.key}.label`, `${NS}.fields.${field.key}.label`],
    field.label || field.key,
  );
}

export function fieldHelp(t: Translate, kind: string, field: DataSourceField): string {
  return firstTranslated(
    t,
    [`${NS}.kinds.${kind}.fields.${field.key}.help`, `${NS}.fields.${field.key}.help`],
    field.help ?? "",
  );
}

export function optionLabel(t: Translate, kind: string, field: DataSourceField, option: string): string {
  return firstTranslated(
    t,
    [`${NS}.kinds.${kind}.fields.${field.key}.options.${option}`, `${NS}.fields.${field.key}.options.${option}`],
    option,
  );
}

export function capabilityLabel(t: Translate, capability: DataSourceCapability | string): string {
  return firstTranslated(t, [`${NS}.capabilities.${capability}`], capability);
}

export function queryPlaceholder(t: Translate, kind: string): string {
  return firstTranslated(t, [`${NS}.kinds.${kind}.queryPlaceholder`], t(`${NS}.query.placeholder`));
}

export function fieldErrorText(t: Translate, key: string, code: string): string {
  return firstTranslated(t, [`${NS}.errors.byField.${key}.${code}`, `${NS}.errors.${code}`], code);
}

/** The address a source points at, for the list: its first URL-typed field. */
export function sourceAddress(source: DataSource, kind?: DataSourceKindInfo): string {
  const urlKey = kind?.fields.find((f) => f.type === "url")?.key ?? "url";
  return source.config?.[urlKey] ?? "";
}

// A series without a metric name arrives already named after its label set
// ("{queue=\"jobs\"}"); appending the labels again would print them twice.
export function seriesLabel(series: MetricSeries): string {
  const name = series.name ?? "";
  if (name.startsWith("{")) return name;
  const labels = Object.entries(series.labels ?? {})
    .sort(([a], [b]) => (a < b ? -1 : a > b ? 1 : 0))
    .map(([k, v]) => `${k}=${JSON.stringify(v)}`)
    .join(", ");
  if (!labels) return name || "{}";
  return `${name}{${labels}}`;
}

export function toChartSeries(series: MetricSeries[]): ChartSeries[] {
  return series.map((s, i) => ({
    id: `${i}:${seriesLabel(s)}`,
    label: seriesLabel(s),
    points: s.points,
    colorIndex: (i % CHART_COLORS) + 1,
  }));
}

export function queryWindow(rangeMs: number, now = Date.now()): { from: string; to: string } {
  return { from: new Date(now - rangeMs).toISOString(), to: new Date(now).toISOString() };
}
