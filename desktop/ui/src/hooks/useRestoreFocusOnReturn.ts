import { useEffect, useRef } from "react";

/**
 * Alt-tabbing away from the window drops document focus — Chromium moves it to
 * <body> — and the field the user was typing in is unreadable when they switch
 * back. Remember the last editable element while the window is away and hand
 * focus back to it the next time the window returns.
 *
 * Capture is belt-and-braces, because browsers disagree about what fires on a
 * window switch:
 *  - `focusout` with a null relatedTarget (focus left the document entirely);
 *  - the window's own `blur`, whatever the active element happens to be then;
 *  - `visibilitychange` → hidden (Electron hides the page for an occluded or
 *    minimized window, and neither element nor window focus events may reach an
 *    occluded page at all).
 *
 * Restore runs on window focus, document focus and `visibilitychange` → visible,
 * and is re-applied once more after a task turn — the activation sequence can
 * clear a freshly-restored caret a moment later.
 *
 * Conservative on purpose: only editable fields are restored, only while they
 * are still mounted, and only when nothing else in the page already owns focus
 * (a dialog or a field the user clicked meanwhile is left strictly alone).
 */
export function useRestoreFocusOnReturn(): void {
  const last = useRef<HTMLElement | null>(null);
  const retryTimer = useRef<number | null>(null);

  useEffect(() => {
    const editable = (el: HTMLElement): boolean => {
      if (el instanceof HTMLInputElement || el instanceof HTMLTextAreaElement || el instanceof HTMLSelectElement) {
        return !el.disabled;
      }
      return el.isContentEditable || el instanceof HTMLButtonElement;
    };

    const active = (): HTMLElement | null =>
      document.activeElement instanceof HTMLElement ? document.activeElement : null;

    const save = (el: HTMLElement | null): void => {
      if (el && el !== document.body && el !== document.documentElement && editable(el)) {
        last.current = el;
      }
    };

    const restore = () => {
      if (retryTimer.current !== null) {
        clearTimeout(retryTimer.current);
        retryTimer.current = null;
      }
      const el = last.current;
      if (!el || !el.isConnected || !editable(el)) return;
      const focused = active();
      // Someone else already owns focus in the page — a dialog, or the user
      // clicked another field. Not ours to move.
      if (focused && focused !== document.body && focused !== document.documentElement && focused !== el) return;
      el.focus({ preventScroll: true });
      last.current = null;
    };

    const onReturn = () => {
      restore();
      // Refocus once more after the activation settles: Chromium can clear the
      // caret we just set the moment the window's own focus pass finishes.
      retryTimer.current = window.setTimeout(restore, 0);
    };

    const onWindowBlur = () => save(active());

    const onFocusOut = (event: FocusEvent) => {
      if (event.relatedTarget !== null) return;
      const target = event.target;
      if (target instanceof HTMLElement) save(target);
    };

    const onVisibility = () => {
      if (document.visibilityState === "hidden") save(active());
      else onReturn();
    };

    window.addEventListener("blur", onWindowBlur);
    window.addEventListener("focus", onReturn);
    document.addEventListener("focusout", onFocusOut);
    document.addEventListener("focus", onReturn);
    document.addEventListener("visibilitychange", onVisibility);
    return () => {
      if (retryTimer.current !== null) clearTimeout(retryTimer.current);
      window.removeEventListener("blur", onWindowBlur);
      window.removeEventListener("focus", onReturn);
      document.removeEventListener("focusout", onFocusOut);
      document.removeEventListener("focus", onReturn);
      document.removeEventListener("visibilitychange", onVisibility);
    };
  }, []);
}