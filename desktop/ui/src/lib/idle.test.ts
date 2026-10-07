import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { IDLE_AFTER_MS, IDLE_ATTRIBUTE, startIdleTracking } from "@/lib/idle";

let visibility: DocumentVisibilityState = "visible";
let stop: (() => void) | undefined;

const idle = () => document.documentElement.hasAttribute(IDLE_ATTRIBUTE);

function setVisibility(next: DocumentVisibilityState) {
  visibility = next;
  document.dispatchEvent(new Event("visibilitychange"));
}

beforeEach(() => {
  vi.useFakeTimers();
  visibility = "visible";
  Object.defineProperty(document, "visibilityState", { configurable: true, get: () => visibility });
  vi.spyOn(document, "hasFocus").mockReturnValue(true);
});

afterEach(() => {
  stop?.();
  stop = undefined;
  vi.useRealTimers();
});

describe("startIdleTracking", () => {
  it("starts active in a focused, visible window", () => {
    stop = startIdleTracking();
    expect(idle()).toBe(false);
  });

  it("starts idle when the window does not have focus", () => {
    vi.mocked(document.hasFocus).mockReturnValue(false);
    stop = startIdleTracking();
    expect(idle()).toBe(true);
  });

  it("follows window blur and focus", () => {
    stop = startIdleTracking();
    window.dispatchEvent(new Event("blur"));
    expect(idle()).toBe(true);
    window.dispatchEvent(new Event("focus"));
    expect(idle()).toBe(false);
  });

  it("follows the document's visibility", () => {
    stop = startIdleTracking();
    setVisibility("hidden");
    expect(idle()).toBe(true);
    setVisibility("visible");
    expect(idle()).toBe(false);
  });

  it("goes idle after a minute without input and wakes on the next input", () => {
    stop = startIdleTracking();
    vi.advanceTimersByTime(IDLE_AFTER_MS - 1);
    expect(idle()).toBe(false);
    vi.advanceTimersByTime(1);
    expect(idle()).toBe(true);

    window.dispatchEvent(new Event("pointermove"));
    expect(idle()).toBe(false);
  });

  it.each(["pointerdown", "pointermove", "keydown", "wheel"])("counts %s as input", (type) => {
    stop = startIdleTracking();
    vi.advanceTimersByTime(IDLE_AFTER_MS - 1000);
    window.dispatchEvent(new Event(type));
    vi.advanceTimersByTime(IDLE_AFTER_MS - 1000);
    expect(idle()).toBe(false);
    vi.advanceTimersByTime(1000);
    expect(idle()).toBe(true);
  });

  it("stays idle on input while the window is blurred", () => {
    stop = startIdleTracking();
    window.dispatchEvent(new Event("blur"));
    window.dispatchEvent(new Event("pointermove"));
    expect(idle()).toBe(true);
  });

  it("keeps a single timer however much input arrives", () => {
    stop = startIdleTracking();
    for (let i = 0; i < 50; i += 1) window.dispatchEvent(new Event("pointermove"));
    expect(vi.getTimerCount()).toBe(1);
  });

  it("removes the mark and its listeners when stopped", () => {
    stop = startIdleTracking();
    window.dispatchEvent(new Event("blur"));
    stop();
    stop = undefined;
    expect(idle()).toBe(false);
    expect(vi.getTimerCount()).toBe(0);
    window.dispatchEvent(new Event("blur"));
    expect(idle()).toBe(false);
  });
});
