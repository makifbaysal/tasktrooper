import { act, fireEvent, render, screen } from "@testing-library/react";
import { useEffect, useRef } from "react";
import { describe, expect, it } from "vitest";
import { useStickToBottom } from "@/hooks/useStickToBottom";

// jsdom reports scrollHeight 0 for every element, so the scroll metrics the
// hook reads are installed on the harness element as plain accessors over a
// mutable state object the test drives.
interface ScrollState {
  scrollHeight: number;
  clientHeight: number;
  scrollTop: number;
}

function installScroll(el: HTMLElement): ScrollState {
  const state: ScrollState = { scrollHeight: 0, clientHeight: 100, scrollTop: 0 };
  Object.defineProperty(el, "scrollHeight", { configurable: true, get: () => state.scrollHeight });
  Object.defineProperty(el, "clientHeight", { configurable: true, get: () => state.clientHeight });
  Object.defineProperty(el, "scrollTop", {
    configurable: true,
    get: () => state.scrollTop,
    set: (value: number) => {
      state.scrollTop = value;
    },
  });
  return state;
}

function Harness({ rise, onReady }: { rise: number; onReady: (el: HTMLElement, state: ScrollState) => void }) {
  const ref = useRef<HTMLDivElement>(null);
  const pin = useStickToBottom(ref, [rise]);
  const installed = useRef(false);
  useEffect(() => {
    const el = ref.current;
    if (el && !installed.current) {
      installed.current = true;
      onReady(el, installScroll(el));
    }
  }, [onReady]);
  return (
    <div>
      <div data-testid="list" ref={ref} style={{ height: 100, overflowY: "auto" }} />
      <button type="button" onClick={() => pin()}>
        pin
      </button>
    </div>
  );
}

async function flushSettle(): Promise<void> {
  // The settle loop is rAF/timer driven; a tick or two lets it run to the two
  // same-height frames that stop it.
  await act(async () => {
    await new Promise((resolve) => setTimeout(resolve, 50));
  });
}

describe("useStickToBottom", () => {
  it("pins a just-mounting session to the bottom once its content arrives", async () => {
    let seen: { el: HTMLElement; state: ScrollState } | null = null;
    const { rerender } = render(<Harness rise={0} onReady={(el, state) => (seen = { el, state })} />);
    act(() => {
      seen!.state.scrollHeight = 600;
    });
    rerender(<Harness rise={1} onReady={(el, state) => (seen = { el, state })} />);
    await flushSettle();
    expect(seen!.state.scrollTop).toBe(600);
  });

  it("follows new content while the reader stays at the bottom", async () => {
    let seen: { el: HTMLElement; state: ScrollState } | null = null;
    const { rerender } = render(<Harness rise={0} onReady={(el, state) => (seen = { el, state })} />);
    act(() => {
      seen!.state.scrollHeight = 600;
    });
    rerender(<Harness rise={1} onReady={(el, state) => (seen = { el, state })} />);
    await flushSettle();
    act(() => {
      seen!.state.scrollHeight = 800;
    });
    rerender(<Harness rise={2} onReady={(el, state) => (seen = { el, state })} />);
    await flushSettle();
    expect(seen!.state.scrollTop).toBe(800);
  });

  it("does not yank the reader back to the bottom after they scrolled up", async () => {
    let seen: { el: HTMLElement; state: ScrollState } | null = null;
    const { rerender } = render(<Harness rise={0} onReady={(el, state) => (seen = { el, state })} />);
    act(() => {
      seen!.state.scrollHeight = 600;
    });
    rerender(<Harness rise={1} onReady={(el, state) => (seen = { el, state })} />);
    await flushSettle();
    // Reader scrolls up into the history; the next content must not move it.
    act(() => {
      seen!.state.scrollTop = 200;
    });
    fireEvent.scroll(seen!.el);
    act(() => {
      seen!.state.scrollHeight = 800;
    });
    rerender(<Harness rise={2} onReady={(el, state) => (seen = { el, state })} />);
    await flushSettle();
    expect(seen!.state.scrollTop).toBe(200);
  });

  it("re-sticks on pin() even after the reader scrolled away", async () => {
    let seen: { el: HTMLElement; state: ScrollState } | null = null;
    const { rerender } = render(<Harness rise={0} onReady={(el, state) => (seen = { el, state })} />);
    act(() => {
      seen!.state.scrollHeight = 600;
    });
    rerender(<Harness rise={1} onReady={(el, state) => (seen = { el, state })} />);
    await flushSettle();
    act(() => {
      seen!.state.scrollTop = 120;
    });
    fireEvent.scroll(seen!.el);
    fireEvent.click(screen.getByRole("button", { name: "pin" }));
    await flushSettle();
    expect(seen!.state.scrollTop).toBe(600);
  });
});