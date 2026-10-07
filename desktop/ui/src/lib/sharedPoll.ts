// One timer and at most one request in flight for an endpoint several mounted
// components poll at once (the header's notifications, the board, the
// backlog all read the same task list and activity feed). It runs at the
// shortest interval any subscriber asked for, pauses on a hidden tab, and
// forgets everything — including a request still in flight — once the last
// subscriber leaves.
//
// A new subscriber is handed the cached answer only while it is younger than
// the shortest interval, and never one read before the last `invalidate()`:
// a page that remounts paints its own (possibly just-moved) cards, and an
// older answer would briefly put them back.

export interface SharedPollListener<T> {
  intervalMs: number;
  /**
   * Captured when a read this listener will receive starts (or when it joins
   * one already in flight) and handed back with the value — so a page can
   * tell whether its own local change happened after the read began.
   */
  begin?: () => number;
  onValue: (value: T, ticket: number) => void;
  onError?: (error: unknown) => void;
}

export interface SharedPoll<T> {
  subscribe(listener: SharedPollListener<T>): () => void;
  /**
   * A local change made the cached answer, and the one in flight, stale: a
   * later subscriber gets neither, and waits for a read that began after it.
   */
  invalidate(): void;
}

export function createSharedPoll<T>(fetcher: () => Promise<T>): SharedPoll<T> {
  const listeners = new Set<SharedPollListener<T>>();
  let generation = 0;
  let inFlight: Map<SharedPollListener<T>, number> | null = null;
  let inFlightStale = false;
  let rerunWhenSettled = false;
  let cached: { value: T; startedAt: number } | null = null;
  let lastStart = Number.NEGATIVE_INFINITY;
  let timer: ReturnType<typeof setTimeout> | undefined;

  const hidden = () => typeof document !== "undefined" && document.visibilityState === "hidden";
  const ticketFor = (listener: SharedPollListener<T>) => listener.begin?.() ?? 0;

  function shortestInterval(): number {
    let ms = Number.POSITIVE_INFINITY;
    for (const listener of listeners) ms = Math.min(ms, listener.intervalMs);
    return ms;
  }

  function stopTimer() {
    clearTimeout(timer);
    timer = undefined;
  }

  function schedule() {
    stopTimer();
    if (listeners.size === 0 || inFlight || hidden()) return;
    timer = setTimeout(run, Math.max(0, lastStart + shortestInterval() - Date.now()));
  }

  function run() {
    stopTimer();
    if (inFlight || listeners.size === 0 || hidden()) return;
    const gen = generation;
    const tickets = new Map<SharedPollListener<T>, number>();
    for (const listener of listeners) tickets.set(listener, ticketFor(listener));
    inFlight = tickets;
    const startedAt = Date.now();
    lastStart = startedAt;
    fetcher()
      .then(
        (value) => {
          if (gen !== generation) return;
          if (!inFlightStale) cached = { value, startedAt };
          for (const [listener, ticket] of tickets) {
            if (listeners.has(listener)) listener.onValue(value, ticket);
          }
        },
        (error: unknown) => {
          if (gen !== generation) return;
          for (const listener of tickets.keys()) {
            if (listeners.has(listener)) listener.onError?.(error);
          }
        },
      )
      .finally(() => {
        if (gen !== generation) return;
        inFlight = null;
        inFlightStale = false;
        if (rerunWhenSettled) {
          rerunWhenSettled = false;
          run();
        } else {
          schedule();
        }
      });
  }

  const onVisibilityChange = () => (hidden() ? stopTimer() : run());

  return {
    subscribe(listener) {
      listeners.add(listener);
      if (listeners.size === 1) document.addEventListener("visibilitychange", onVisibilityChange);
      if (inFlight && !inFlightStale) {
        inFlight.set(listener, ticketFor(listener));
      } else if (inFlight) {
        rerunWhenSettled = true;
      } else if (cached && Date.now() - cached.startedAt < shortestInterval()) {
        listener.onValue(cached.value, ticketFor(listener));
        schedule();
      } else {
        run();
      }

      return () => {
        if (!listeners.delete(listener)) return;
        inFlight?.delete(listener);
        if (listeners.size > 0) {
          schedule();
          return;
        }
        document.removeEventListener("visibilitychange", onVisibilityChange);
        stopTimer();
        generation += 1;
        inFlight = null;
        inFlightStale = false;
        rerunWhenSettled = false;
        cached = null;
        lastStart = Number.NEGATIVE_INFINITY;
      };
    },

    invalidate() {
      cached = null;
      if (inFlight) inFlightStale = true;
    },
  };
}
