import { act, renderHook } from "@testing-library/react";
import { StrictMode, type ReactNode } from "react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { useCachedState } from "@/hooks/useCachedState";
import { readCache, writeCache } from "@/lib/uiCache";

vi.mock("@/lib/uiCache", async () => {
  const actual = await vi.importActual<typeof import("@/lib/uiCache")>("@/lib/uiCache");
  return { ...actual, writeCache: vi.fn(actual.writeCache) };
});

const strict = ({ children }: { children: ReactNode }) => <StrictMode>{children}</StrictMode>;

describe("useCachedState", () => {
  beforeEach(() => {
    vi.mocked(writeCache).mockClear();
  });

  it("writes a changed value to the cache once, even when React runs the updater twice", () => {
    const { result } = renderHook(() => useCachedState<number[]>("test.changed", [1]), { wrapper: strict });
    act(() => result.current[1]((prev) => [...prev, 2]));
    expect(result.current[0]).toEqual([1, 2]);
    expect(writeCache).toHaveBeenCalledTimes(1);
    expect(readCache("test.changed")).toEqual([1, 2]);
  });

  it("skips the write when the updater hands back the previous value", () => {
    const { result } = renderHook(() => useCachedState<number[]>("test.same", [1]));
    const before = result.current[0];
    act(() => result.current[1]((prev) => prev));
    expect(result.current[0]).toBe(before);
    expect(writeCache).not.toHaveBeenCalled();
  });

  it("paints from the cache on the next mount", () => {
    const first = renderHook(() => useCachedState<string>("test.revisit", "fallback"));
    act(() => first.result.current[1]("fresh"));
    first.unmount();
    const second = renderHook(() => useCachedState<string>("test.revisit", "fallback"));
    expect(second.result.current[0]).toBe("fresh");
  });
});
