import { useCallback, useEffect, useRef, useState, useSyncExternalStore } from "react";
import {
  desktopToolchain,
  type ToolchainId,
  type ToolchainJobState,
  type ToolchainReport,
} from "@/lib/desktop-bridge";

export interface UseToolchainOptions {
  /** Run detection on mount. The header leaves it off: it only follows the job, and a sweep spawns processes. */
  report?: boolean;
}

export interface ToolchainControls {
  /** False in a browser and in a shell older than bridge 5 — every surface then steps aside. */
  available: boolean;
  report: ToolchainReport | null;
  reportError: string;
  refreshing: boolean;
  state: ToolchainJobState | null;
  refresh: (force?: boolean) => Promise<void>;
  plan: (ids: ToolchainId[]) => Promise<ToolchainId[]>;
  /** Resolves once the shell has queued the ids; the installs run on in the background. */
  install: (ids: ToolchainId[]) => Promise<ToolchainJobState>;
  cancel: () => Promise<void>;
  dismiss: () => Promise<void>;
  openPage: (id: ToolchainId) => Promise<void>;
}

function messageOf(error: unknown): string {
  return error instanceof Error ? error.message : String(error);
}

/**
 * The shell's toolchain installer, as React sees it: the detection report and
 * the job state, pushed by the shell rather than polled — the job runs in the
 * main process and outlives this page, so a page opened mid-install picks it
 * up where it is.
 */
export function useToolchain({ report: loadReport = true }: UseToolchainOptions = {}): ToolchainControls {
  const [host] = useState(() => desktopToolchain());
  const [state, setState] = useState<ToolchainJobState | null>(null);
  const [report, setReport] = useState<ToolchainReport | null>(null);
  const [reportError, setReportError] = useState("");
  const [refreshing, setRefreshing] = useState(false);
  const reportSeq = useRef(0);

  const refresh = useCallback(
    async (force = false) => {
      if (!host) return;
      const seq = ++reportSeq.current;
      setRefreshing(true);
      try {
        const next = await host.report(force);
        if (seq !== reportSeq.current) return;
        setReport(next);
        setReportError("");
      } catch (e) {
        if (seq === reportSeq.current) setReportError(messageOf(e));
      } finally {
        if (seq === reportSeq.current) setRefreshing(false);
      }
    },
    [host],
  );

  useEffect(() => {
    if (!host) return;
    let live = true;
    let pushed = false;
    void host.state().then(
      (initial) => {
        if (live && !pushed) setState(initial);
      },
      () => undefined,
    );
    const off = host.subscribe((next) => {
      pushed = true;
      setState(next);
    });
    return () => {
      live = false;
      off();
    };
  }, [host]);

  useEffect(() => {
    if (loadReport) void refresh(false);
  }, [loadReport, refresh]);

  // A finished job changed what is installed; the report from before it is
  // stale, and only a forced sweep re-probes rather than answering from cache.
  const phase = state?.phase;
  const previousPhase = useRef(phase);
  useEffect(() => {
    const was = previousPhase.current;
    previousPhase.current = phase;
    if (loadReport && was === "running" && phase === "done") void refresh(true);
  }, [phase, loadReport, refresh]);

  const plan = useCallback(async (ids: ToolchainId[]) => (host && ids.length > 0 ? host.plan(ids) : []), [host]);

  const install = useCallback(
    async (ids: ToolchainId[]) => {
      if (!host) throw new Error("The toolchain installer needs the TaskTrooper desktop app.");
      const next = await host.install(ids);
      setState(next);
      return next;
    },
    [host],
  );

  const cancel = useCallback(async () => {
    if (host) setState(await host.cancel());
  }, [host]);

  const dismiss = useCallback(async () => {
    if (host) setState(await host.dismiss());
  }, [host]);

  const openPage = useCallback(async (id: ToolchainId) => {
    if (host) await host.openPage(id);
  }, [host]);

  return {
    available: host !== null,
    report,
    reportError,
    refreshing,
    state,
    refresh,
    plan,
    install,
    cancel,
    dismiss,
    openPage,
  };
}

/**
 * How to restart whatever runs the agents, or null without a runner bridge.
 * Local mode's agents run inside the backend, so it is `agent-server` that
 * restarts (the shell reloads this window onto the new port); `restart()` is
 * the account-mode runner's and does nothing for the local backend.
 */
export function agentRestarter(): (() => Promise<unknown>) | null {
  const host = window.__tasktrooperDesktop;
  const runner = host?.runner;
  if (!runner) return null;
  if (host.mode === "account") return () => runner.restart();
  return () => runner.restartChild("agent-server");
}

// Which finished job the agents were last restarted for. localStorage, not the
// tab's cache: a local-mode restart reloads this window, and the job (with its
// restartRecommended) is still the shell's afterwards.
const RESTARTED_JOB_KEY = "tt.toolchain.restartedJob";
const restartListeners = new Set<() => void>();
let restartedFallback: string | null = null;

function jobKey(state: ToolchainJobState): string | null {
  return state.jobId ?? (state.finishedAt !== undefined ? String(state.finishedAt) : null);
}

function readRestartedJob(): string | null {
  try {
    return window.localStorage.getItem(RESTARTED_JOB_KEY) ?? restartedFallback;
  } catch {
    return restartedFallback;
  }
}

function subscribeRestarted(listener: () => void): () => void {
  restartListeners.add(listener);
  return () => {
    restartListeners.delete(listener);
  };
}

function writeRestartedJob(key: string | null): void {
  restartedFallback = key;
  try {
    if (key === null) window.localStorage.removeItem(RESTARTED_JOB_KEY);
    else window.localStorage.setItem(RESTARTED_JOB_KEY, key);
  } catch {
    /* private-mode storage — the in-memory value covers this session */
  }
  for (const listener of restartListeners) listener();
}

/**
 * Records that the agents are being restarted for this job, so its prompt
 * stops asking. Returns how to take that back if the restart fails.
 */
export function markAgentsRestarted(state: ToolchainJobState): () => void {
  const key = jobKey(state);
  if (key === null) return () => {};
  const previous = readRestartedJob();
  writeRestartedJob(key);
  return () => writeRestartedJob(previous);
}

/** The job recommends a restart and the agents have not been restarted for it yet. */
export function useRestartPending(state: ToolchainJobState | null): boolean {
  const restarted = useSyncExternalStore(subscribeRestarted, readRestartedJob, () => null);
  if (!state?.restartRecommended) return false;
  const key = jobKey(state);
  return key === null || key !== restarted;
}
