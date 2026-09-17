import "@testing-library/jest-dom/vitest";
import { cleanup } from "@testing-library/react";
import { afterEach } from "vitest";

// Vitest's `globals` option is off, so React Testing Library's own
// auto-cleanup (which hooks into a jest-style global `afterEach`) never
// fires — without this, component tests that render more than once leak
// nodes into the same jsdom `document` across tests/cases.
afterEach(() => {
  cleanup();
});

// Polyfills the global `localStorage` the SPA reads at module scope
// (src/api.ts's getStoredLocale) — jsdom doesn't implement DOM storage
// persistence beyond an in-memory map either, but this keeps the same
// deterministic backing store the tests relied on before the environment
// switched from "node" to "jsdom".
class MemoryStorage implements Storage {
  private store = new Map<string, string>();

  get length(): number {
    return this.store.size;
  }

  clear(): void {
    this.store.clear();
  }

  getItem(key: string): string | null {
    return this.store.has(key) ? this.store.get(key)! : null;
  }

  key(index: number): string | null {
    return Array.from(this.store.keys())[index] ?? null;
  }

  removeItem(key: string): void {
    this.store.delete(key);
  }

  setItem(key: string, value: string): void {
    this.store.set(key, value);
  }
}

Object.defineProperty(globalThis, "localStorage", {
  value: new MemoryStorage(),
  configurable: true,
  writable: true,
});
