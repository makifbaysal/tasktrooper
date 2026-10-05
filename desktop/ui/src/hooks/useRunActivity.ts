import { useCallback, useEffect, useRef, useState } from "react";
import { api, type OrchestrationPlan, type SessionStep } from "@/api";
import { isRunLive, mergeSteps } from "@/lib/activityFeed";
import { tStatic } from "@/hooks/useI18n";
import { usePolling } from "@/hooks/usePolling";

const PLAN_STEP = "orchestration_plan_created";

export function useRunActivity(runId: string | null, enabled: boolean, runStatus?: string | null) {
  const [steps, setSteps] = useState<SessionStep[]>([]);
  const [plan, setPlan] = useState<OrchestrationPlan | null>(null);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const activeRun = useRef<string | null>(null);
  const inflight = useRef<string | null>(null);
  const stepsRef = useRef<SessionStep[]>([]);
  const planRef = useRef<OrchestrationPlan | null>(null);
  const liveRef = useRef(false);

  const fetchData = useCallback(async () => {
    if (!runId || inflight.current === runId) return;
    inflight.current = runId;
    try {
      const known = stepsRef.current;
      const since = known.length > 0 ? known[known.length - 1].created_at : undefined;
      const res = await api.runSteps(runId, since);
      if (activeRun.current !== runId) return;
      const merged = mergeSteps(stepsRef.current, res.steps ?? []);
      if (merged !== stepsRef.current) {
        stepsRef.current = merged;
        setSteps(merged);
      }
      if (merged.some((s) => s.step_type === PLAN_STEP) && (planRef.current === null || liveRef.current)) {
        const next = await api.getRunPlan(runId).catch(() => null);
        if (activeRun.current !== runId) return;
        if (next) {
          planRef.current = next;
          setPlan(next);
        }
      }
      setError(null);
    } catch (e) {
      if (activeRun.current === runId) {
        setError(e instanceof Error ? e.message : tStatic("chatArea.chat.activityPanel.runLoadFailed"));
      }
    } finally {
      if (inflight.current === runId) inflight.current = null;
      if (activeRun.current === runId) setLoading(false);
    }
  }, [runId]);

  const live = isRunLive(runStatus, steps, plan);
  const isLive = enabled && !!runId && live;
  liveRef.current = isLive;

  useEffect(() => {
    activeRun.current = runId;
    inflight.current = null;
    stepsRef.current = [];
    planRef.current = null;
    setSteps([]);
    setPlan(null);
    setError(null);
    if (!runId || !enabled) {
      setLoading(false);
      return;
    }
    setLoading(true);
    void fetchData();
  }, [runId, enabled, fetchData]);

  // The run row can turn terminal between two ticks; one last read picks up the
  // steps written in that gap, since polling stops the moment isLive drops.
  const wasLive = useRef(false);
  useEffect(() => {
    if (wasLive.current && !isLive) void fetchData();
    wasLive.current = isLive;
  }, [isLive, fetchData]);

  usePolling(fetchData, 2000, isLive);

  return { steps, plan, isLive, loading, error };
}
