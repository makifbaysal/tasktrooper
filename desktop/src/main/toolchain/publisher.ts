/**
 * At most one push per interval, the latest state always last.
 *
 * An installer can print hundreds of lines a second, and each one changes the
 * job's log; pushing every one to the page would spend the renderer on
 * re-renders nobody can read. A phase change (started, finished, dismissed)
 * goes out at once, because that is what the header chip appears and
 * disappears on.
 */

export interface PublisherTimers {
  now(): number;
  setTimeout(fn: () => void, ms: number): unknown;
  clearTimeout(handle: unknown): void;
}

const REAL_TIMERS: PublisherTimers = {
  now: () => Date.now(),
  setTimeout: (fn, ms) => {
    const handle = setTimeout(fn, ms);
    handle.unref?.();
    return handle;
  },
  clearTimeout: (handle) => clearTimeout(handle as NodeJS.Timeout),
};

export class ThrottledPublisher<T> {
  readonly #read: () => T;
  readonly #sinks: ((value: T) => void)[];
  readonly #intervalMs: number;
  readonly #timers: PublisherTimers;
  #last = Number.NEGATIVE_INFINITY;
  #pending: unknown = null;

  constructor(read: () => T, sinks: ((value: T) => void)[], intervalMs = 250, timers: PublisherTimers = REAL_TIMERS) {
    this.#read = read;
    this.#sinks = sinks;
    this.#intervalMs = intervalMs;
    this.#timers = timers;
  }

  notify(immediate = false): void {
    if (immediate) {
      this.#cancelPending();
      this.#publish();
      return;
    }
    if (this.#pending !== null) return;
    const wait = this.#last + this.#intervalMs - this.#timers.now();
    if (wait <= 0) {
      this.#publish();
      return;
    }
    this.#pending = this.#timers.setTimeout(() => {
      this.#pending = null;
      this.#publish();
    }, wait);
  }

  dispose(): void {
    this.#cancelPending();
  }

  #cancelPending(): void {
    if (this.#pending === null) return;
    this.#timers.clearTimeout(this.#pending);
    this.#pending = null;
  }

  #publish(): void {
    this.#last = this.#timers.now();
    const value = this.#read();
    for (const sink of this.#sinks) {
      try {
        sink(value);
      } catch {
        // One sink failing (a page mid-reload) must not starve the others.
      }
    }
  }
}
