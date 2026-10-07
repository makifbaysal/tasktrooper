import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { api, type OrchestrationPlan, type SessionRun, type SessionStep } from "@/api";
import { isRunLive, mergeSteps, type FeedRunInput } from "@/lib/activityFeed";
import { sessionRunKey } from "@/lib/chat";
import { keepEqual } from "@/lib/stableState";
import { usePolling } from "@/hooks/usePolling";

const MAX_IN_FLIGHT = 6;
const PLAN_STEP = "orchestration_plan_created";

interface RunCache {
  steps: SessionStep[];
  plan: OrchestrationPlan | null;
  fetched: boolean;
  settled: boolean;
}

function startOf(run: SessionRun): number {
  return Date.parse(run.started_at) || 0;
}

export function useSessionActivity(runs: SessionRun[], enabled: boolean) {
  const cache = useRef(new Map<string, RunCache>());
  const busy = useRef(false);
  const runsRef = useRef(runs);
  runsRef.current = runs;
  const [version, setVersion] = useState(0);
  const [loaded, setLoaded] = useState(false);

  const refreshRun = useCallback(async (run: SessionRun): Promise<boolean> => {
    const entry = cache.current.get(run.id) ?? { steps: [], plan: null, fetched: false, settled: false };
    cache.current.set(run.id, entry);
    const last = entry.steps[entry.steps.length - 1];
    let changed = false;
    try {
      const res = await api.runSteps(run.id, entry.fetched ? last?.created_at : undefined);
      const merged = mergeSteps(entry.steps, res.steps ?? []);
      const stepsChanged = merged !== entry.steps;
      if (stepsChanged) {
        entry.steps = merged;
        changed = true;
      }
      entry.fetched = true;
      // A plan only moves when a step lands, so a quiet live tick does not
      // refetch it. A read that finds the run over is its last (it settles and
      // is never polled again), so it re-reads the plan's final statuses too.
      if (entry.steps.some((s) => s.step_type === PLAN_STEP)) {
        const live = isRunLive(run.status, entry.steps, entry.plan);
        if (entry.plan === null || stepsChanged || !live) {
          const plan = await api.getRunPlan(run.id).catch(() => null);
          if (plan) {
            const kept = entry.plan === null ? plan : keepEqual(entry.plan, plan);
            if (kept !== entry.plan) {
              entry.plan = kept;
              changed = true;
            }
          }
        }
      }
      if (!isRunLive(run.status, entry.steps, entry.plan)) entry.settled = true;
    } catch {
      // keep what we have; the next tick retries
    }
    return changed;
  }, []);

  const tick = useCallback(async () => {
    if (busy.current) return;
    busy.current = true;
    try {
      const todo = runsRef.current.filter((run) => !cache.current.get(run.id)?.settled);
      let changed = false;
      for (let i = 0; i < todo.length; i += MAX_IN_FLIGHT) {
        const results = await Promise.all(todo.slice(i, i + MAX_IN_FLIGHT).map(refreshRun));
        if (results.some(Boolean)) changed = true;
      }
      if (changed) setVersion((v) => v + 1);
    } finally {
      busy.current = false;
      setLoaded(true);
    }
  }, [refreshRun]);

  usePolling(tick, 2000, enabled);

  // Keyed on content, not on the array: the caller's poll may hand over an
  // equal list with a new identity, and rebuilding the feed for it is wasted.
  const runKey = runs.map(sessionRunKey).join("|");
  useEffect(() => {
    if (enabled) void tick();
  }, [enabled, runKey, tick]);

  const inputs = useMemo<FeedRunInput[]>(() => {
    void version;
    void runKey;
    return runsRef.current
      .map((run, index) => ({ run, index }))
      .sort((a, b) => startOf(a.run) - startOf(b.run) || b.index - a.index)
      .map(({ run }) => {
        const entry = cache.current.get(run.id);
        const steps = entry?.steps ?? [];
        const plan = entry?.plan ?? null;
        return {
          runId: run.id,
          live: isRunLive(run.status, steps, plan),
          startedAt: run.started_at,
          steps,
          plan,
        };
      });
  }, [runKey, version]);

  return {
    inputs,
    loading: enabled && runs.length > 0 && !loaded,
    anyLive: inputs.some((input) => input.live),
  };
}
