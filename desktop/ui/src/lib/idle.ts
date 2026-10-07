export const IDLE_ATTRIBUTE = "data-idle";
export const IDLE_AFTER_MS = 60_000;

const INPUT_EVENTS = ["pointerdown", "pointermove", "keydown", "wheel"] as const;

interface IdleTrackingOptions {
  idleAfterMs?: number;
  win?: Window;
  doc?: Document;
}

/**
 * globals.css pauses every infinite animation under `<html data-idle>`, so it
 * must be set only while nobody is watching: the window is blurred, the
 * document is hidden, or no pointer, keyboard or wheel input came for
 * `idleAfterMs`.
 */
export function startIdleTracking({
  idleAfterMs = IDLE_AFTER_MS,
  win = window,
  doc = document,
}: IdleTrackingOptions = {}): () => void {
  const root = doc.documentElement;
  let blurred = !doc.hasFocus();
  let hidden = doc.visibilityState === "hidden";
  let inactive = false;
  let lastInput = Date.now();
  let timer: ReturnType<typeof setTimeout> | undefined;

  const apply = () => {
    const idle = blurred || hidden || inactive;
    if (idle === root.hasAttribute(IDLE_ATTRIBUTE)) return;
    if (idle) root.setAttribute(IDLE_ATTRIBUTE, "");
    else root.removeAttribute(IDLE_ATTRIBUTE);
  };

  // pointermove fires per frame while the mouse moves, so input only stamps a
  // time; the timer checks the stamp when it fires instead of being re-armed
  // on every event.
  const arm = (delay: number) => {
    clearTimeout(timer);
    timer = setTimeout(check, delay);
  };

  function check() {
    timer = undefined;
    const quietFor = Date.now() - lastInput;
    if (quietFor >= idleAfterMs) {
      inactive = true;
      apply();
      return;
    }
    arm(idleAfterMs - quietFor);
  }

  const markActive = () => {
    lastInput = Date.now();
    if (timer === undefined) arm(idleAfterMs);
    if (inactive) {
      inactive = false;
      apply();
    }
  };

  const onBlur = () => {
    blurred = true;
    apply();
  };
  const onFocus = () => {
    blurred = false;
    markActive();
    apply();
  };
  const onVisibility = () => {
    hidden = doc.visibilityState === "hidden";
    if (!hidden) markActive();
    apply();
  };

  const listenerOptions: AddEventListenerOptions = { capture: true, passive: true };
  for (const type of INPUT_EVENTS) win.addEventListener(type, markActive, listenerOptions);
  win.addEventListener("blur", onBlur);
  win.addEventListener("focus", onFocus);
  doc.addEventListener("visibilitychange", onVisibility);
  arm(idleAfterMs);
  apply();

  return () => {
    clearTimeout(timer);
    timer = undefined;
    for (const type of INPUT_EVENTS) win.removeEventListener(type, markActive, listenerOptions);
    win.removeEventListener("blur", onBlur);
    win.removeEventListener("focus", onFocus);
    doc.removeEventListener("visibilitychange", onVisibility);
    root.removeAttribute(IDLE_ATTRIBUTE);
  };
}
