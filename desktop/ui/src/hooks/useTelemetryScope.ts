import { useSearchParams } from "react-router-dom";
import { useOpsContext, useOpsPageScope } from "@/components/operations/shell/OpsContext";
import { telemetryRange, type TelemetryRange } from "@/lib/telemetryHub";

export const TELEMETRY_ENV_PARAM = "tenv";

export interface TelemetryScope {
  range: TelemetryRange;
  /** The product's own environment name; unset reads every environment. */
  env: string | undefined;
  from: number;
  to: number;
}

/** The product's environment filter alone, for a control that must not touch what the top bar shows. */
export function useTelemetryEnv(): string | undefined {
  const [params] = useSearchParams();
  return params.get(TELEMETRY_ENV_PARAM) || undefined;
}

interface ScopeOptions {
  /** The page reads no time window (monitors, a deploy list of its own). */
  time?: boolean;
}

/** The console's window plus the product's environment filter. The product
 * names its environments itself, so the console's four fixed ones do not
 * apply: the top bar's picker is hidden and the page offers its own. */
export function useTelemetryScope({ time = true }: ScopeOptions = {}): TelemetryScope {
  const { range } = useOpsContext();
  const env = useTelemetryEnv();
  useOpsPageScope({ time, compare: false, env: false });
  return { range: telemetryRange(range), env, from: range.from, to: range.to };
}
