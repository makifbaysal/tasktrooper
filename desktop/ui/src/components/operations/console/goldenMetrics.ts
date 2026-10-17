import { OPS_GOLDEN_MAX_WINDOW_MS, type GoldenSignalValue, type OpsGolden } from "@/api";

export type GoldenIndex = Map<string, Map<string, GoldenSignalValue>>;

const EMPTY: GoldenIndex = new Map();

/** component id → signal id → its golden value. The answer lists every
 * active component with a confirmed binding in the environment, each with all
 * of its role's golden signals: an unbound or unreadable one comes back with
 * nulls (and `bound: false` when unbound). A component the answer leaves out
 * has no confirmed binding there. */
export function indexGolden(golden: OpsGolden | null | undefined): GoldenIndex {
  if (!golden?.components?.length) return EMPTY;
  return new Map(
    golden.components.map((c) => [c.component_id, new Map((c.signals ?? []).map((s) => [s.signal, s]))] as const),
  );
}

export function goldenValue(index: GoldenIndex, componentId: string | undefined, signal: string): GoldenSignalValue | undefined {
  return componentId ? index.get(componentId)?.get(signal) : undefined;
}

export interface GoldenWindow {
  from: number;
  to: number;
  /** The range was longer than the server reads, so only its last 30 days are asked. */
  clamped: boolean;
}

export function goldenWindow(range: { from: number; to: number }): GoldenWindow {
  const earliest = range.to - OPS_GOLDEN_MAX_WINDOW_MS;
  return range.from < earliest ? { from: earliest, to: range.to, clamped: true } : { from: range.from, to: range.to, clamped: false };
}

export interface GoldenReadState {
  data: OpsGolden | null;
  loading: boolean;
  error?: string;
}

/** What one golden cell shows. `unknown` is a null from a server that does
 * not say whether the signal is bound. */
export type GoldenCell =
  | { kind: "loading" }
  | { kind: "failed"; error: string }
  | { kind: "unbound" }
  | { kind: "noData" }
  | { kind: "unknown" }
  | { kind: "value"; value: GoldenSignalValue & { value: number } };

export function goldenCell(read: GoldenReadState, index: GoldenIndex, componentId: string | undefined, signal: string): GoldenCell {
  const found = goldenValue(index, componentId, signal);
  if (found) {
    if (found.value != null) return { kind: "value", value: found as GoldenSignalValue & { value: number } };
    if (found.bound === false) return { kind: "unbound" };
    return found.bound ? { kind: "noData" } : { kind: "unknown" };
  }
  if (!read.data) {
    if (read.error) return { kind: "failed", error: read.error };
    if (read.loading) return { kind: "loading" };
  }
  return { kind: "unbound" };
}
