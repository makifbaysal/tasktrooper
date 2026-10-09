import { describe, expect, it, vi } from "vitest";
import type { KeysPrefill } from "../../ipc/channels.js";

const electron = vi.hoisted(() => {
  const windows: FakeWindow[] = [];
  class FakeWindow {
    sent: unknown[][] = [];
    closed: (() => void) | undefined;
    webContents = {
      send: (...args: unknown[]) => void this.sent.push(args),
      on: () => undefined,
      setWindowOpenHandler: () => undefined,
    };
    constructor() {
      windows.push(this);
    }
    once() {}
    on(event: string, fn: () => void) {
      if (event === "closed") this.closed = fn;
    }
    isDestroyed() {
      return false;
    }
    isMinimized() {
      return false;
    }
    show() {}
    focus() {}
    restore() {}
    close() {
      this.closed?.();
    }
    loadURL() {
      return Promise.resolve();
    }
  }
  return { windows, FakeWindow };
});

vi.mock("electron", () => ({
  BrowserWindow: electron.FakeWindow,
  app: { getAppPath: () => "/app" },
}));

const { KeysWindow } = await import("./window.js");
const { KEYS_EVENTS } = await import("../../ipc/channels.js");

const PREFILL: KeysPrefill = { id: "openai", type: "openai" };

describe("KeysWindow prefill", () => {
  it("holds what the opener asked for until the window closes", () => {
    const window = new KeysWindow();
    expect(window.prefill).toBeNull();
    window.open(PREFILL);
    expect(window.prefill).toEqual(PREFILL);
    electron.windows.at(-1)?.close();
    expect(window.prefill).toBeNull();
  });

  it("tells an open window when it is asked to focus on something else, and when it is not", () => {
    const window = new KeysWindow();
    window.open();
    const open = electron.windows.at(-1);
    window.open(PREFILL);
    window.open();
    expect(open?.sent).toEqual([
      [KEYS_EVENTS.prefill, PREFILL],
      [KEYS_EVENTS.prefill, null],
    ]);
    expect(electron.windows.at(-1)).toBe(open);
  });
});
