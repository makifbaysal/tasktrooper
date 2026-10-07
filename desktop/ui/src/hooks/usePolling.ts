import { useEffect, useRef, useState } from "react";

/**
 * useDocumentVisible tracks whether the tab is in the foreground.
 *
 * Polling from a hidden tab is pure waste: nobody can see the result, and a
 * background tab has no reason to generate traffic at all.
 */
export function useDocumentVisible(): boolean {
  const [visible, setVisible] = useState(
    () => typeof document === "undefined" || document.visibilityState === "visible",
  );
  useEffect(() => {
    const onChange = () => setVisible(document.visibilityState === "visible");
    document.addEventListener("visibilitychange", onChange);
    onChange();
    return () => document.removeEventListener("visibilitychange", onChange);
  }, []);
  return visible;
}

/**
 * usePolling runs `callback` immediately and then every `intervalMs`, while
 * `enabled` is true AND the tab is visible. Hiding the tab stops the timer;
 * showing it again fires one immediate refresh (so the user never looks at
 * stale data) and restarts the timer. A tick that comes due while the
 * callback's previous promise is still pending is skipped, so a slow server
 * never has a pile of the same request queued against it.
 *
 * `leading: false` is for a caller that has just loaded on its own and only
 * wants the follow-up ticks: no tick when polling starts, though coming back
 * to a hidden tab still refreshes at once.
 */
export function usePolling(
  callback: () => void | Promise<void>,
  intervalMs: number,
  enabled: boolean,
  { leading = true }: { leading?: boolean } = {},
) {
  const savedCallback = useRef(callback);
  const visible = useDocumentVisible();
  const leadingRef = useRef(leading);
  leadingRef.current = leading;
  const pausedByHiddenTab = useRef(false);

  useEffect(() => {
    savedCallback.current = callback;
  }, [callback]);

  useEffect(() => {
    if (!enabled) {
      pausedByHiddenTab.current = false;
      return;
    }
    if (!visible) {
      pausedByHiddenTab.current = true;
      return;
    }
    const resuming = pausedByHiddenTab.current;
    pausedByHiddenTab.current = false;
    let running = false;
    const tick = () => {
      if (running) return;
      const result = savedCallback.current();
      if (!(result instanceof Promise)) return;
      running = true;
      void result.finally(() => {
        running = false;
      });
    };
    if (leadingRef.current || resuming) tick();
    const id = setInterval(tick, intervalMs);
    return () => clearInterval(id);
  }, [intervalMs, enabled, visible]);
}
