import { useCallback, useEffect, useRef, useState } from "react";
import { aiJobRefusal, api, ApiError, type AIJob, type AIJobRefusal, type AIJobStarted } from "@/api";
import { AI_JOB_POLL_MS, isJobRunning } from "@/components/operations/ai/aiJobs";
import { usePolling } from "@/hooks/usePolling";
import { readCache, writeCache } from "@/lib/uiCache";

export interface AIJobRead {
  job: AIJob | null;
  starting: boolean;
  /** A coded refusal to start: no release engineer, or no AI on this server. */
  refusal: AIJobRefusal | null;
  /** Any other refusal to start, as the server worded it. */
  startError: string | null;
  /** The last progress read failed; the job shown is the last one read. */
  pollError: string | null;
  start(run: () => Promise<AIJobStarted>): Promise<AIJob | null>;
  dismiss(): void;
}

/** One AI job per `cacheKey` (a page and its scope): started by `start`,
 * read every 3 s while it runs, and kept for the session so a page left and
 * opened again picks a running job back up. `onFinished` runs once when a job
 * this hook follows leaves `running`. */
export function useAIJob(cacheKey: string, onFinished?: (job: AIJob) => void): AIJobRead {
  const [state, setState] = useState(() => ({ key: cacheKey, job: readCache<AIJob | null>(cacheKey) ?? null }));
  if (state.key !== cacheKey) setState({ key: cacheKey, job: readCache<AIJob | null>(cacheKey) ?? null });
  const job = state.key === cacheKey ? state.job : null;
  const [starting, setStarting] = useState(false);
  const [refusal, setRefusal] = useState<AIJobRefusal | null>(null);
  const [startError, setStartError] = useState<string | null>(null);
  const [pollError, setPollError] = useState<string | null>(null);
  const finished = useRef(onFinished);
  finished.current = onFinished;
  const generation = useRef(0);

  useEffect(() => {
    setRefusal(null);
    setStartError(null);
    setPollError(null);
  }, [cacheKey]);

  const put = useCallback(
    (next: AIJob | null) => {
      writeCache(cacheKey, next);
      setState({ key: cacheKey, job: next });
    },
    [cacheKey],
  );

  const poll = useCallback(async () => {
    if (!job || !isJobRunning(job)) return;
    const mine = generation.current;
    try {
      const next = await api.getAIJob(job.id);
      if (mine !== generation.current) return;
      setPollError(null);
      put(next);
      if (!isJobRunning(next)) finished.current?.(next);
    } catch (e) {
      if (mine !== generation.current) return;
      if (e instanceof ApiError && e.status === 404) {
        put(null);
        return;
      }
      setPollError(e instanceof Error ? e.message : String(e));
    }
  }, [job, put]);

  usePolling(poll, AI_JOB_POLL_MS, isJobRunning(job));

  const start = useCallback(
    async (run: () => Promise<AIJobStarted>) => {
      generation.current += 1;
      setStarting(true);
      setRefusal(null);
      setStartError(null);
      setPollError(null);
      try {
        const answer = await run();
        put(answer.job);
        if (!isJobRunning(answer.job)) finished.current?.(answer.job);
        return answer.job;
      } catch (e) {
        const coded = aiJobRefusal(e);
        if (coded) setRefusal(coded);
        else setStartError(e instanceof Error ? e.message : String(e));
        return null;
      } finally {
        setStarting(false);
      }
    },
    [put],
  );

  const dismiss = useCallback(() => {
    generation.current += 1;
    setRefusal(null);
    setStartError(null);
    setPollError(null);
    put(null);
  }, [put]);

  return { job, starting, refusal, startError, pollError, start, dismiss };
}
