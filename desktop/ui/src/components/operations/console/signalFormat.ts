import { defaultValueFormatter } from "@/components/operations/charts/chartData";
import { signalSpec } from "@/components/operations/console/roleViews";
import type { SignalDelta } from "@/components/operations/console/SignalTile";

export type Translate = (key: string, params?: Record<string, string | number>) => string;

export interface SignalFormatContext {
  locale: string;
  t: Translate;
}

/** Which way is better for a signal; a signal missing here (CPU, rollout,
 * instances) is neutral, so its delta is never coloured. */
export const SIGNAL_TREND: Record<string, "upIsGood" | "upIsBad"> = {
  "http.requests": "upIsGood",
  "http.error_ratio": "upIsBad",
  "http.latency": "upIsBad",
  "infra.restarts": "upIsBad",
  "infra.throttles": "upIsBad",
  "queue.backlog": "upIsBad",
  "queue.backlog_age": "upIsBad",
  "queue.consume_rate": "upIsGood",
  "queue.dead_letter": "upIsBad",
  "process.errors": "upIsBad",
  "web.page_views": "upIsGood",
  "web.sessions": "upIsGood",
  "web.visitors": "upIsGood",
  "web.fcp": "upIsBad",
  "web.lcp": "upIsBad",
  "web.inp": "upIsBad",
  "web.cls": "upIsBad",
  "web.ttfb": "upIsBad",
  "web.js_errors": "upIsBad",
  "mobile.crash_free_users": "upIsGood",
  "mobile.crash_rate": "upIsBad",
  "mobile.anr_rate": "upIsBad",
  "mobile.cold_start": "upIsBad",
};

// A change smaller than this, relative to the previous period, reads as flat.
const FLAT_RELATIVE = 0.01;

const RATE_UNITS: Record<string, string> = {
  "req/s": "reqPerSecond",
  "msg/s": "msgPerSecond",
  "errors/s": "errorsPerSecond",
  "views/s": "viewsPerSecond",
};

const WORD_UNITS: Record<string, string> = {
  cores: "cores",
  messages: "messages",
  restarts: "restarts",
};

const PLAIN_UNITS = new Set(["", "count", "score"]);

// Binary multiples, so labelled as such.
const BYTE_STEPS = ["B", "KiB", "MiB", "GiB", "TiB"];

export interface SignalValueParts {
  value: string;
  unit: string;
}

/** A value in the signal's catalog unit, scaled to read well: ms past a
 * second as seconds, seconds past two minutes as minutes, bytes in KiB…TiB. */
export function signalValueParts(v: number, unit: string | undefined, ctx: SignalFormatContext): SignalValueParts {
  const num = defaultValueFormatter(ctx.locale);
  const u = (key: string) => ctx.t(`operations.units.${key}`);
  const abs = Math.abs(v);
  switch (unit ?? "") {
    case "ms":
      return abs >= 1000 ? { value: num(v / 1000), unit: u("seconds") } : { value: num(v), unit: u("ms") };
    case "s":
      if (abs >= 7200) return { value: num(v / 3600), unit: u("hours") };
      if (abs >= 120) return { value: num(v / 60), unit: u("minutes") };
      return { value: num(v), unit: u("seconds") };
    case "%":
      return { value: num(v), unit: u("percent") };
    case "bytes": {
      let scaled = v;
      let step = 0;
      while (Math.abs(scaled) >= 1024 && step < BYTE_STEPS.length - 1) {
        scaled /= 1024;
        step += 1;
      }
      return { value: num(scaled), unit: BYTE_STEPS[step] };
    }
  }
  if (unit && RATE_UNITS[unit]) return { value: num(v), unit: u(RATE_UNITS[unit]) };
  if (unit && WORD_UNITS[unit]) return { value: num(v), unit: u(WORD_UNITS[unit]) };
  if (!unit || PLAIN_UNITS.has(unit)) return { value: num(v), unit: "" };
  return { value: num(v), unit };
}

export function joinSignalValue(parts: SignalValueParts): string {
  if (!parts.unit) return parts.value;
  return parts.unit === "%" ? `${parts.value}%` : `${parts.value} ${parts.unit}`;
}

export function formatSignalValue(v: number, unit: string | undefined, ctx: SignalFormatContext): string {
  return joinSignalValue(signalValueParts(v, unit, ctx));
}

export function signalFormatter(unit: string | undefined, ctx: SignalFormatContext): (v: number) => string {
  return (v) => formatSignalValue(v, unit, ctx);
}

// A percentage-point change smaller than this reads as flat (it rounds to 0
// at the two decimals values are shown with).
const FLAT_POINTS = 0.005;

/** The change since the previous period, as the tile shows it: for a `%`
 * signal always the difference in percentage points (2% → 3% is "+1 pp", not
 * "+50%"); otherwise relative when the previous value is not zero, else the
 * difference itself. */
export function signalDelta(
  signal: string,
  value: number | null | undefined,
  previous: number | null | undefined,
  unit: string | undefined,
  ctx: SignalFormatContext,
): SignalDelta | undefined {
  if (value == null || previous == null || !Number.isFinite(value) || !Number.isFinite(previous)) return undefined;
  const diff = value - previous;
  const label = ctx.t("operations.signalDelta.vsPrevious");
  let text: string;
  let flat: boolean;
  if (unit === "%") {
    flat = Math.abs(diff) < FLAT_POINTS;
    const points = defaultValueFormatter(ctx.locale)(Math.abs(diff));
    text = flat ? ctx.t("operations.signalDelta.flat") : `${diff > 0 ? "+" : "−"}${points} ${ctx.t("operations.units.percentPoints")}`;
  } else if (previous !== 0) {
    const relative = diff / Math.abs(previous);
    flat = Math.abs(relative) < FLAT_RELATIVE;
    const pct = new Intl.NumberFormat(ctx.locale, { maximumFractionDigits: Math.abs(relative) < 0.1 ? 1 : 0 }).format(
      Math.abs(relative) * 100,
    );
    text = flat ? ctx.t("operations.signalDelta.flat") : `${diff > 0 ? "+" : "−"}${pct}%`;
  } else {
    flat = diff === 0;
    text = flat ? ctx.t("operations.signalDelta.flat") : `${diff > 0 ? "+" : "−"}${formatSignalValue(Math.abs(diff), unit, ctx)}`;
  }
  const direction = flat ? "flat" : diff > 0 ? "up" : "down";
  const trend = SIGNAL_TREND[signal];
  const tone = flat || !trend ? "flat" : (direction === "up") === (trend === "upIsGood") ? "good" : "bad";
  return { text, tone, direction, label };
}

/** A catalog signal's name in the reader's language, else its id. */
export function signalLabel(t: Translate, id: string, short = false): string {
  const spec = signalSpec(id);
  if (!spec) return id;
  return t(`operations.signals.${short ? (spec.short ?? spec.label) : spec.label}`);
}
