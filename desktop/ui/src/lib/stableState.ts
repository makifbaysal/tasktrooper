// Polls hand back a freshly parsed payload on every tick even when nothing
// changed; storing it as-is gives React a new identity and re-renders every
// consumer. These keep the previous value whenever the new one is equivalent.

export function sameRows<T>(prev: readonly T[], next: readonly T[], key: (item: T) => string): boolean {
  if (prev === next) return true;
  if (prev.length !== next.length) return false;
  for (let i = 0; i < prev.length; i++) {
    if (key(prev[i]) !== key(next[i])) return false;
  }
  return true;
}

export function keepRows<T>(prev: T[], next: T[], key: (item: T) => string): T[] {
  return sameRows(prev, next, key) ? prev : next;
}

// JSON comparison: only for plain API payloads (no Dates, Maps or Sets).
export function keepEqual<T>(prev: T, next: T): T {
  if (prev === next) return prev;
  return JSON.stringify(prev) === JSON.stringify(next) ? prev : next;
}

export function keepMap<K, V>(prev: Map<K, V>, next: Map<K, V>, key: (value: V) => string): Map<K, V> {
  if (prev === next) return prev;
  if (prev.size !== next.size) return next;
  for (const [k, value] of next) {
    const before = prev.get(k);
    if (before === undefined || key(before) !== key(value)) return next;
  }
  return prev;
}
