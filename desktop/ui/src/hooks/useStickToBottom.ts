import { useEffect, useLayoutEffect, useRef, type RefObject } from "react";

const NEAR_BOTTOM_PX = 48;

// Follows new content only while the reader is already at the bottom; scrolling
// up to read earlier steps must not be undone by the next poll.
export function useStickToBottom(ref: RefObject<HTMLElement | null>, deps: readonly unknown[]) {
  const stuck = useRef(true);
  const attached = useRef<HTMLElement | null>(null);

  const onScroll = useRef(() => {
    const el = attached.current;
    if (el) stuck.current = el.scrollHeight - el.scrollTop - el.clientHeight <= NEAR_BOTTOM_PX;
  });

  // The container can mount after the hook (behind a loading state), so the
  // listener is attached whenever the element changes rather than once.
  useLayoutEffect(() => {
    const el = ref.current;
    if (el !== attached.current) {
      attached.current?.removeEventListener("scroll", onScroll.current);
      el?.addEventListener("scroll", onScroll.current, { passive: true });
      attached.current = el;
      stuck.current = true;
    }
    if (el && stuck.current) el.scrollTop = el.scrollHeight;
  }, [ref, ...deps]);

  useEffect(
    () => () => {
      attached.current?.removeEventListener("scroll", onScroll.current);
      attached.current = null;
    },
    [],
  );
}
