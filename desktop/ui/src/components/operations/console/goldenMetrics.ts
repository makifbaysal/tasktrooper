import type { GoldenSignalValue, OpsGolden } from "@/api";

export type GoldenIndex = Map<string, Map<string, GoldenSignalValue>>;

const EMPTY: GoldenIndex = new Map();

/** component id → signal id → its golden value. A signal the answer leaves
 * out has no data source bound for that component. */
export function indexGolden(golden: OpsGolden | null | undefined): GoldenIndex {
  if (!golden?.components?.length) return EMPTY;
  return new Map(
    golden.components.map((c) => [c.component_id, new Map((c.signals ?? []).map((s) => [s.signal, s]))] as const),
  );
}

export function goldenValue(index: GoldenIndex, componentId: string | undefined, signal: string): GoldenSignalValue | undefined {
  return componentId ? index.get(componentId)?.get(signal) : undefined;
}
