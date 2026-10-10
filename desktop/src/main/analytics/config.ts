declare const __TT_GA_MEASUREMENT_ID__: string | undefined;
declare const __TT_GA_API_SECRET__: string | undefined;

export interface AnalyticsConfig {
  measurementId: string;
  apiSecret: string;
}

/** The one place the default lives; flip it to make the count opt-in. */
export const ANALYTICS_DEFAULT_ON = true;

/**
 * Both values are substituted by esbuild at build time (`scripts/build-main.mjs`),
 * so a shipped app never reads them from its environment or from a file. In an
 * unbuilt run (tests, `tsx`) the identifiers are undefined and analytics is off.
 */
export function buildConfig(): AnalyticsConfig | null {
  const measurementId = typeof __TT_GA_MEASUREMENT_ID__ === "string" ? __TT_GA_MEASUREMENT_ID__ : "";
  const apiSecret = typeof __TT_GA_API_SECRET__ === "string" ? __TT_GA_API_SECRET__ : "";
  return measurementId !== "" && apiSecret !== "" ? { measurementId, apiSecret } : null;
}

export function telemetryForcedOff(env: NodeJS.ProcessEnv): boolean {
  const own = (env.TASKTROOPER_TELEMETRY ?? "").trim().toLowerCase();
  const dnt = (env.DO_NOT_TRACK ?? "").trim().toLowerCase();
  return ["0", "false", "off", "no"].includes(own) || ["1", "true"].includes(dnt);
}
