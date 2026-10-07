import { useCallback, useEffect, useRef, useState } from "react";
import { api, type HealthResponse } from "@/api";
import { tStatic } from "@/hooks/useI18n";
import { usePolling } from "@/hooks/usePolling";

// Health changes rarely and the window's focus refresh below catches the moment
// someone looks; a 10s poll woke the app six times a minute for nothing.
export const HEALTH_POLL_MS = 60_000;

const offline = () => typeof navigator !== "undefined" && navigator.onLine === false;

export function useHealth() {
  const [health, setHealth] = useState<HealthResponse | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const checked = useRef(false);

  const refresh = useCallback(async () => {
    try {
      const data = await api.health();
      setHealth(data);
      setError(null);
    } catch (e) {
      setError(e instanceof Error ? e.message : tStatic("frame.layout.health.checkFailed"));
      setHealth(null);
    } finally {
      checked.current = true;
      setLoading(false);
    }
  }, []);

  // The first check runs regardless, so the badge never sits on "loading";
  // after that an offline machine skips the poll until it is back online.
  const poll = useCallback(() => (checked.current && offline() ? undefined : refresh()), [refresh]);

  // Visibility-gated (usePolling): a hidden tab must not keep polling /health.
  usePolling(poll, HEALTH_POLL_MS, true);

  useEffect(() => {
    const onReturn = () => void poll();
    window.addEventListener("focus", onReturn);
    window.addEventListener("online", onReturn);
    return () => {
      window.removeEventListener("focus", onReturn);
      window.removeEventListener("online", onReturn);
    };
  }, [poll]);

  return { health, loading, error, refresh };
}
