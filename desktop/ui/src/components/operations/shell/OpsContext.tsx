import { createContext, useCallback, useContext, useEffect, useMemo, useState, type ReactNode } from "react";
import { useSearchParams } from "react-router-dom";
import type { DeployEnvironment } from "@/api";
import { usePolling } from "@/hooks/usePolling";

export type OpsRangePreset = "30m" | "1h" | "3h" | "6h" | "12h" | "24h" | "3d" | "7d" | "custom";

export type OpsPreset = Exclude<OpsRangePreset, "custom">;

export interface OpsTimeRange {
  preset: OpsRangePreset;
  from: number;
  to: number;
}

export interface OpsContextValue {
  range: OpsTimeRange;
  env: DeployEnvironment;
  compare: boolean;
  refreshKey: number;
  setPreset(p: OpsPreset): void;
  setCustom(from: number, to: number): void;
  setEnv(e: DeployEnvironment): void;
  setCompare(on: boolean): void;
  refresh(): void;
}

export const OPS_PRESETS: OpsPreset[] = ["30m", "1h", "3h", "6h", "12h", "24h", "3d", "7d"];

const MINUTE = 60_000;
const HOUR = 60 * MINUTE;

export const OPS_PRESET_MS: Record<OpsPreset, number> = {
  "30m": 30 * MINUTE,
  "1h": HOUR,
  "3h": 3 * HOUR,
  "6h": 6 * HOUR,
  "12h": 12 * HOUR,
  "24h": 24 * HOUR,
  "3d": 72 * HOUR,
  "7d": 168 * HOUR,
};

export const OPS_ENVIRONMENTS: DeployEnvironment[] = ["production", "staging", "preview", "development"];

export const DEFAULT_OPS_PRESET: OpsPreset = "1h";
export const DEFAULT_OPS_ENV: DeployEnvironment = "production";

export const OPS_PARAM_KEYS = ["range", "from", "to", "env", "compare"] as const;

// A preset window slides with the clock: `to` moves to now this often even
// when nobody presses Refresh, the way a "since 30 minutes ago" view should.
export const OPS_AUTO_ADVANCE_MS = 60_000;

export interface OpsUrlState {
  preset: OpsRangePreset;
  from?: number;
  to?: number;
  env: DeployEnvironment;
  compare: boolean;
}

function isPreset(value: string | null): value is OpsPreset {
  return OPS_PRESETS.some((p) => p === value);
}

export function isOpsEnvironment(value: string | null | undefined): value is DeployEnvironment {
  return OPS_ENVIRONMENTS.some((e) => e === value);
}

function readMs(value: string | null): number | undefined {
  if (value === null || value.trim() === "") return undefined;
  const n = Number(value);
  return Number.isFinite(n) && n > 0 ? Math.floor(n) : undefined;
}

export function readOpsParams(params: URLSearchParams): OpsUrlState {
  const env = params.get("env");
  const compare = params.get("compare");
  const base = {
    env: isOpsEnvironment(env) ? env : DEFAULT_OPS_ENV,
    compare: compare === "1" || compare === "true",
  };
  const range = params.get("range");
  if (range === "custom") {
    const from = readMs(params.get("from"));
    const to = readMs(params.get("to"));
    if (from !== undefined && to !== undefined && from < to) return { ...base, preset: "custom", from, to };
    return { ...base, preset: DEFAULT_OPS_PRESET };
  }
  return { ...base, preset: isPreset(range) ? range : DEFAULT_OPS_PRESET };
}

export function resolveRange(state: Pick<OpsUrlState, "preset" | "from" | "to">, now: number): OpsTimeRange {
  if (state.preset === "custom" && state.from !== undefined && state.to !== undefined) {
    return { preset: "custom", from: state.from, to: state.to };
  }
  const preset = state.preset === "custom" ? DEFAULT_OPS_PRESET : state.preset;
  return { preset, from: now - OPS_PRESET_MS[preset], to: now };
}

export function previousPeriod(range: Pick<OpsTimeRange, "from" | "to">): { from: number; to: number } {
  const span = range.to - range.from;
  return { from: range.from - span, to: range.from };
}

export function inRange(at: number, range: Pick<OpsTimeRange, "from" | "to">): boolean {
  return Number.isFinite(at) && at >= range.from && at <= range.to;
}

type OpsPatch = Partial<Pick<OpsUrlState, "env" | "compare">> & {
  range?: { preset: OpsPreset } | { preset: "custom"; from: number; to: number };
};

// Defaults stay out of the URL so a plain /operations link and a shared one
// with nothing picked read the same.
export function writeOpsParams(prev: URLSearchParams, patch: OpsPatch): URLSearchParams {
  const next = new URLSearchParams(prev);
  if (patch.range) {
    next.delete("from");
    next.delete("to");
    if (patch.range.preset === "custom") {
      next.set("range", "custom");
      next.set("from", String(Math.floor(patch.range.from)));
      next.set("to", String(Math.floor(patch.range.to)));
    } else if (patch.range.preset === DEFAULT_OPS_PRESET) {
      next.delete("range");
    } else {
      next.set("range", patch.range.preset);
    }
  }
  if (patch.env !== undefined) {
    if (patch.env === DEFAULT_OPS_ENV) next.delete("env");
    else next.set("env", patch.env);
  }
  if (patch.compare !== undefined) {
    if (patch.compare) next.set("compare", "1");
    else next.delete("compare");
  }
  return next;
}

/** `path` with the console's own params (range, env, compare) carried over, so a
 * link between Operations pages keeps what the reader picked. */
export function withOpsParams(path: string, current: URLSearchParams): string {
  const hashAt = path.indexOf("#");
  const hash = hashAt >= 0 ? path.slice(hashAt) : "";
  const withoutHash = hashAt >= 0 ? path.slice(0, hashAt) : path;
  const queryAt = withoutHash.indexOf("?");
  const base = queryAt >= 0 ? withoutHash.slice(0, queryAt) : withoutHash;
  const next = new URLSearchParams(queryAt >= 0 ? withoutHash.slice(queryAt + 1) : "");
  for (const key of OPS_PARAM_KEYS) {
    const value = current.get(key);
    if (value !== null && !next.has(key)) next.set(key, value);
  }
  const qs = next.toString();
  return `${base}${qs ? `?${qs}` : ""}${hash}`;
}

/** What the shell's top bar offers on the page shown: a page whose data the
 * time range does not change says so, and the picker gives way to `note`. */
export interface OpsPageScope {
  time?: boolean;
  compare?: boolean;
  env?: boolean;
  note?: string;
}

export interface ResolvedOpsPageScope {
  time: boolean;
  compare: boolean;
  env: boolean;
  note?: string;
}

export const DEFAULT_OPS_SCOPE: ResolvedOpsPageScope = { time: true, compare: true, env: true };

interface OpsShellState {
  refreshedAt: number;
  scope: ResolvedOpsPageScope;
}

const OpsContext = createContext<OpsContextValue | null>(null);
const OpsShellStateContext = createContext<OpsShellState | null>(null);
const OpsScopeSetterContext = createContext<((scope: ResolvedOpsPageScope) => void) | null>(null);

export function OpsProvider({ children }: { children: ReactNode }) {
  const [params, setParams] = useSearchParams();
  const [refreshedAt, setRefreshedAt] = useState(() => Date.now());
  const [refreshKey, setRefreshKey] = useState(0);
  const [scope, setScope] = useState<ResolvedOpsPageScope>(DEFAULT_OPS_SCOPE);

  const state = readOpsParams(params);
  const { preset, env, compare } = state;
  const customFrom = state.from;
  const customTo = state.to;

  const range = useMemo(
    () => resolveRange({ preset, from: customFrom, to: customTo }, refreshedAt),
    [preset, customFrom, customTo, refreshedAt],
  );

  const update = useCallback(
    (patch: OpsPatch) => setParams((prev) => writeOpsParams(prev, patch), { replace: true }),
    [setParams],
  );

  const setPreset = useCallback(
    (p: OpsPreset) => {
      setRefreshedAt(Date.now());
      update({ range: { preset: p } });
    },
    [update],
  );
  const setCustom = useCallback(
    (from: number, to: number) => {
      if (!Number.isFinite(from) || !Number.isFinite(to) || from === to) return;
      update({ range: { preset: "custom", from: Math.min(from, to), to: Math.max(from, to) } });
    },
    [update],
  );
  const setEnv = useCallback((e: DeployEnvironment) => update({ env: e }), [update]);
  const setCompare = useCallback((on: boolean) => update({ compare: on }), [update]);
  const refresh = useCallback(() => {
    setRefreshedAt(Date.now());
    setRefreshKey((k) => k + 1);
  }, []);

  const advance = useCallback(() => setRefreshedAt(Date.now()), []);
  usePolling(advance, OPS_AUTO_ADVANCE_MS, preset !== "custom", { leading: false });

  const value = useMemo<OpsContextValue>(
    () => ({ range, env, compare, refreshKey, setPreset, setCustom, setEnv, setCompare, refresh }),
    [range, env, compare, refreshKey, setPreset, setCustom, setEnv, setCompare, refresh],
  );
  const shellState = useMemo(() => ({ refreshedAt, scope }), [refreshedAt, scope]);

  return (
    <OpsContext.Provider value={value}>
      <OpsScopeSetterContext.Provider value={setScope}>
        <OpsShellStateContext.Provider value={shellState}>{children}</OpsShellStateContext.Provider>
      </OpsScopeSetterContext.Provider>
    </OpsContext.Provider>
  );
}

export function useOpsContext(): OpsContextValue {
  const ctx = useContext(OpsContext);
  if (!ctx) throw new Error("useOpsContext must be used inside <OpsProvider> (the Operations shell)");
  return ctx;
}

export function useOptionalOpsContext(): OpsContextValue | null {
  return useContext(OpsContext);
}

/** Builds a link to another Operations page that keeps the range, environment
 * and compare switch. Outside the shell it returns the path unchanged. */
export function useOpsHref(): (path: string) => string {
  const [params] = useSearchParams();
  return useCallback((path: string) => withOpsParams(path, params), [params]);
}

/** Declares what the top bar's controls do on this page; omitted fields keep
 * the default (time, compare and environment all apply). A no-op outside the shell. */
export function useOpsPageScope(scope: OpsPageScope): void {
  const setScope = useContext(OpsScopeSetterContext);
  const time = scope.time ?? true;
  const compare = scope.compare ?? time;
  const env = scope.env ?? true;
  const note = scope.note;
  useEffect(() => {
    if (!setScope) return;
    setScope({ time, compare, env, note });
    return () => setScope(DEFAULT_OPS_SCOPE);
  }, [setScope, time, compare, env, note]);
}

export function useOpsShellState(): OpsShellState {
  const ctx = useContext(OpsShellStateContext);
  if (!ctx) throw new Error("useOpsShellState must be used inside <OpsProvider>");
  return ctx;
}
