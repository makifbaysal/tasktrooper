import { useCallback, useEffect, useLayoutEffect, useRef, type RefObject } from "react";

const NEAR_BOTTOM_PX = 48;
// A long session's list keeps growing for a few frames after a render — web
// fonts, markdown images, the streaming bubble. A single pin skips the settled
// bottom by exactly however much the list grows in the frames after it, and
// the next dep-driven pin then jumps over that gap, which reads as the
// viewport walking from the first message down to the last when a session
// mounts. Pinning every frame until two of them report the same scroll height
// lands the reader on the settled bottom instead.
const SETTLE_SAME_FRAMES = 2;

// jsdom (the renderer's test environment) has neither rAF nor cancelAnimationFrame
// unless pretendToBeVisual is on; degrade to timers rather than crash the hook.
const raf: (callback: FrameRequestCallback) => number =
  typeof requestAnimationFrame === "function" ? requestAnimationFrame : (callback) => setTimeout(() => callback(performance.now()), 0) as unknown as number;
const caf: (handle: number) => void =
  typeof cancelAnimationFrame === "function" ? cancelAnimationFrame : (handle) => clearTimeout(handle);

// Follows new content only while the reader is already at the bottom; scrolling
// up to read earlier steps must not be undone by the next poll. The returned
// `pin` re-sticks on purpose, e.g. when the reader sends something themselves.
export function useStickToBottom(ref: RefObject<HTMLElement | null>, deps: readonly unknown[]): () => void {
  const stuck = useRef(true);
  const attached = useRef<HTMLElement | null>(null);
  // One settle loop at a time: a burst of deps changes (poll tick + streamed
  // chunk) shares the same slot and converges to the final bottom instead of
  // stacking a jump per change.
  const frame = useRef<number | null>(null);
  const lastHeight = useRef(-1);
  const sameFrames = useRef(0);

  const onScroll = useRef(() => {
    const el = attached.current;
    if (el) stuck.current = el.scrollHeight - el.scrollTop - el.clientHeight <= NEAR_BOTTOM_PX;
  });

  // One settle frame: pin and, while the content is still moving, hand the
  // loop to the next frame. Direct `scrollTop` assignment is atomic — the
  // position is what the next paint draws, never an in-between frame — so the
  // reader only ever sees the pinned bottom, not a pass over the messages.
  function settleFrame(): void {
    frame.current = null;
    const el = attached.current;
    if (!el || !stuck.current) return;
    el.scrollTop = el.scrollHeight;
    const height = el.scrollHeight;
    if (height === lastHeight.current) sameFrames.current += 1;
    else sameFrames.current = 0;
    lastHeight.current = height;
    if (sameFrames.current < SETTLE_SAME_FRAMES) {
      frame.current = raf(settleFrame);
    }
  }

  function startSettle(): void {
    if (frame.current !== null) return;
    frame.current = raf(settleFrame);
  }

  // The container can mount after the hook (behind a loading state), so the
  // listener is attached whenever the element changes rather than once.
  useLayoutEffect(() => {
    const el = ref.current;
    if (el !== attached.current) {
      attached.current?.removeEventListener("scroll", onScroll.current);
      el?.addEventListener("scroll", onScroll.current, { passive: true });
      attached.current = el;
      stuck.current = true;
      lastHeight.current = -1;
      sameFrames.current = 0;
    }
    if (el && stuck.current) el.scrollTop = el.scrollHeight;
    // The layout-time pin above can still be short of the height the reader
    // would see (fonts/embeds land after layout); the settle loop lands on it.
    startSettle();
  }, [ref, ...deps]);

  useEffect(
    () => () => {
      if (frame.current !== null) caf(frame.current);
      frame.current = null;
      attached.current?.removeEventListener("scroll", onScroll.current);
      attached.current = null;
    },
    [],
  );

  return useCallback(() => {
    stuck.current = true;
    sameFrames.current = 0;
    const el = attached.current;
    if (el) el.scrollTop = el.scrollHeight;
    startSettle();
  }, []);
}