import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { createSharedPoll } from "@/lib/sharedPoll";

function deferred<T>() {
  let resolve!: (value: T) => void;
  let reject!: (error: unknown) => void;
  const promise = new Promise<T>((res, rej) => {
    resolve = res;
    reject = rej;
  });
  return { promise, resolve, reject };
}

let visibility: DocumentVisibilityState = "visible";

beforeEach(() => {
  vi.useFakeTimers();
  visibility = "visible";
  Object.defineProperty(document, "visibilityState", { configurable: true, get: () => visibility });
});

afterEach(() => {
  vi.useRealTimers();
});

describe("createSharedPoll", () => {
  it("serves every subscriber from one request, at the shortest interval asked for", async () => {
    const fetcher = vi.fn().mockResolvedValue("v");
    const poll = createSharedPoll(fetcher);
    const slow = vi.fn();
    const fast = vi.fn();
    poll.subscribe({ intervalMs: 15000, onValue: slow });
    poll.subscribe({ intervalMs: 5000, onValue: fast });
    await vi.advanceTimersByTimeAsync(0);
    expect(fetcher).toHaveBeenCalledTimes(1);
    expect(slow).toHaveBeenCalledWith("v", 0);
    expect(fast).toHaveBeenCalledWith("v", 0);

    await vi.advanceTimersByTimeAsync(5000);
    expect(fetcher).toHaveBeenCalledTimes(2);
    expect(slow).toHaveBeenCalledTimes(2);
  });

  it("never has two requests in flight, however slow the answer", async () => {
    const pending = deferred<string>();
    const fetcher = vi.fn().mockReturnValueOnce(pending.promise).mockResolvedValue("later");
    const poll = createSharedPoll(fetcher);
    const onValue = vi.fn();
    poll.subscribe({ intervalMs: 1000, onValue });
    await vi.advanceTimersByTimeAsync(5000);
    expect(fetcher).toHaveBeenCalledTimes(1);

    pending.resolve("first");
    await vi.advanceTimersByTimeAsync(0);
    expect(onValue).toHaveBeenCalledWith("first", 0);
    await vi.advanceTimersByTimeAsync(0);
    expect(fetcher).toHaveBeenCalledTimes(2);
  });

  it("hands each subscriber back the ticket it had when the read began", async () => {
    const pending = deferred<string>();
    const poll = createSharedPoll(() => pending.promise);
    let version = 1;
    const onValue = vi.fn();
    poll.subscribe({ intervalMs: 1000, begin: () => version, onValue });
    version = 2;
    pending.resolve("v");
    await vi.advanceTimersByTimeAsync(0);
    expect(onValue).toHaveBeenCalledWith("v", 1);
  });

  it("gives a late subscriber the cached answer at once and the request in flight when it lands", async () => {
    const fetcher = vi.fn().mockResolvedValue("cached");
    const poll = createSharedPoll(fetcher);
    poll.subscribe({ intervalMs: 10000, onValue: vi.fn() });
    await vi.advanceTimersByTimeAsync(0);

    const late = vi.fn();
    poll.subscribe({ intervalMs: 10000, onValue: late });
    expect(late).toHaveBeenCalledWith("cached", 0);
    expect(fetcher).toHaveBeenCalledTimes(1);

    const pending = deferred<string>();
    fetcher.mockReturnValueOnce(pending.promise);
    await vi.advanceTimersByTimeAsync(10000);
    const joiner = vi.fn();
    poll.subscribe({ intervalMs: 10000, begin: () => 7, onValue: joiner });
    pending.resolve("fresh");
    await vi.advanceTimersByTimeAsync(0);
    expect(joiner).toHaveBeenLastCalledWith("fresh", 7);
  });

  it("hands a late subscriber no cached answer older than the shortest interval, and reads at once instead", async () => {
    const fetcher = vi.fn().mockResolvedValueOnce("old").mockResolvedValue("new");
    const poll = createSharedPoll(fetcher);
    poll.subscribe({ intervalMs: 15000, onValue: vi.fn() });
    await vi.advanceTimersByTimeAsync(6000);
    expect(fetcher).toHaveBeenCalledTimes(1);

    const late = vi.fn();
    poll.subscribe({ intervalMs: 5000, onValue: late });
    expect(late).not.toHaveBeenCalled();
    expect(fetcher).toHaveBeenCalledTimes(2);
    await vi.advanceTimersByTimeAsync(0);
    expect(late).toHaveBeenCalledTimes(1);
    expect(late).toHaveBeenCalledWith("new", 0);
  });

  it("never hands a later subscriber what was cached before a local change", async () => {
    const fetcher = vi.fn().mockResolvedValueOnce("before the move").mockResolvedValue("after the move");
    const poll = createSharedPoll(fetcher);
    poll.subscribe({ intervalMs: 10000, onValue: vi.fn() });
    await vi.advanceTimersByTimeAsync(0);

    poll.invalidate();
    const late = vi.fn();
    poll.subscribe({ intervalMs: 10000, onValue: late });
    expect(late).not.toHaveBeenCalled();
    await vi.advanceTimersByTimeAsync(0);
    expect(late).toHaveBeenCalledWith("after the move", 0);
  });

  it("does not let a later subscriber join a read that began before a local change", async () => {
    const pending = deferred<string>();
    const fetcher = vi.fn().mockReturnValueOnce(pending.promise).mockResolvedValue("after the move");
    const poll = createSharedPoll(fetcher);
    const early = vi.fn();
    poll.subscribe({ intervalMs: 10000, onValue: early });
    poll.invalidate();

    const late = vi.fn();
    poll.subscribe({ intervalMs: 10000, onValue: late });
    pending.resolve("before the move");
    await vi.advanceTimersByTimeAsync(0);
    expect(early).toHaveBeenCalledWith("before the move", 0);
    expect(late).not.toHaveBeenCalledWith("before the move", expect.anything());
    expect(fetcher).toHaveBeenCalledTimes(2);
    await vi.advanceTimersByTimeAsync(0);
    expect(late).toHaveBeenCalledWith("after the move", 0);

    const another = vi.fn();
    poll.subscribe({ intervalMs: 10000, onValue: another });
    expect(another).toHaveBeenCalledWith("after the move", 0);
  });

  it("stops with the last subscriber and never delivers that abandoned request later", async () => {
    const pending = deferred<string>();
    const fetcher = vi.fn().mockReturnValueOnce(pending.promise).mockResolvedValue("new");
    const poll = createSharedPoll(fetcher);
    const gone = vi.fn();
    const unsubscribe = poll.subscribe({ intervalMs: 1000, onValue: gone });
    unsubscribe();

    const next = vi.fn();
    poll.subscribe({ intervalMs: 1000, onValue: next });
    pending.resolve("stale");
    await vi.advanceTimersByTimeAsync(0);
    expect(gone).not.toHaveBeenCalled();
    expect(next).toHaveBeenCalledTimes(1);
    expect(next).toHaveBeenCalledWith("new", 0);
  });

  it("reports a failed read and keeps polling", async () => {
    const fetcher = vi.fn().mockRejectedValueOnce(new Error("blip")).mockResolvedValue("ok");
    const poll = createSharedPoll(fetcher);
    const onError = vi.fn();
    const onValue = vi.fn();
    poll.subscribe({ intervalMs: 1000, onValue, onError });
    await vi.advanceTimersByTimeAsync(0);
    expect(onError).toHaveBeenCalledTimes(1);
    await vi.advanceTimersByTimeAsync(1000);
    expect(onValue).toHaveBeenCalledWith("ok", 0);
  });

  it("pauses on a hidden tab and reads once the tab is shown again", async () => {
    const fetcher = vi.fn().mockResolvedValue("v");
    const poll = createSharedPoll(fetcher);
    poll.subscribe({ intervalMs: 1000, onValue: vi.fn() });
    await vi.advanceTimersByTimeAsync(0);

    visibility = "hidden";
    document.dispatchEvent(new Event("visibilitychange"));
    await vi.advanceTimersByTimeAsync(10000);
    expect(fetcher).toHaveBeenCalledTimes(1);

    visibility = "visible";
    document.dispatchEvent(new Event("visibilitychange"));
    await vi.advanceTimersByTimeAsync(0);
    expect(fetcher).toHaveBeenCalledTimes(2);
  });
});
