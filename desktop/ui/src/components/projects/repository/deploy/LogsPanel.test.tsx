import "@testing-library/jest-dom/vitest";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { LogsPanel, mergeLiveTail } from "@/components/projects/repository/deploy/LogsPanel";
import { I18nProvider } from "@/hooks/useI18n";

// jsdom has no layout engine, so Radix Select's scroll-into-view-on-open
// crashes without this — same stub ChecksTab.test.tsx and friends use.
if (!Element.prototype.scrollIntoView) {
  Element.prototype.scrollIntoView = () => {};
}

const { getEnvironmentLogs } = vi.hoisted(() => ({ getEnvironmentLogs: vi.fn() }));

vi.mock("@/api", async () => {
  const actual = await vi.importActual<typeof import("@/api")>("@/api");
  return { ...actual, api: { ...actual.api, getEnvironmentLogs } };
});

function renderPanel() {
  return render(
    <I18nProvider>
      <LogsPanel envId="env-1" />
    </I18nProvider>,
  );
}

describe("LogsPanel", () => {
  beforeEach(() => {
    getEnvironmentLogs.mockReset().mockResolvedValue({ entries: [] });
  });

  afterEach(() => {
    vi.useRealTimers();
  });

  it("loads the default window on mount", async () => {
    renderPanel();
    await waitFor(() => expect(getEnvironmentLogs).toHaveBeenCalledTimes(1));
    const [envId, query] = getEnvironmentLogs.mock.calls[0];
    expect(envId).toBe("env-1");
    expect(query.since).toBeTruthy();
    expect(query.min_severity).toBeUndefined();
  });

  it("changing severity and typing a search term refetches with min_severity and text", async () => {
    renderPanel();
    await waitFor(() => expect(getEnvironmentLogs).toHaveBeenCalledTimes(1));

    fireEvent.click(await screen.findByLabelText(/severity/i));
    fireEvent.click(await screen.findByRole("option", { name: /Error/i }));
    await waitFor(() => expect(getEnvironmentLogs).toHaveBeenCalledTimes(2));
    expect(getEnvironmentLogs.mock.calls[1][1].min_severity).toBe("error");

    fireEvent.change(screen.getByPlaceholderText("Search logs…"), { target: { value: "timeout" } });
    await waitFor(() => expect(getEnvironmentLogs.mock.calls.at(-1)?.[1].text).toBe("timeout"), { timeout: 1000 });
  });

  it("the Live toggle polls every 5s and stops as soon as it is switched off", async () => {
    vi.useFakeTimers();
    renderPanel();
    await vi.waitFor(() => expect(getEnvironmentLogs).toHaveBeenCalledTimes(1));

    fireEvent.click(screen.getByRole("switch", { name: "Live" }));
    await vi.waitFor(() => expect(getEnvironmentLogs).toHaveBeenCalledTimes(2));

    await vi.advanceTimersByTimeAsync(5000);
    expect(getEnvironmentLogs).toHaveBeenCalledTimes(3);

    fireEvent.click(screen.getByRole("switch", { name: "Live" }));
    await vi.advanceTimersByTimeAsync(10000);
    expect(getEnvironmentLogs).toHaveBeenCalledTimes(3);
  });
});

describe("LogsPanel live tail", () => {
  const line = (timestamp: string, message: string) => ({ timestamp, severity: "info" as const, message });

  beforeEach(() => {
    getEnvironmentLogs.mockReset();
  });

  afterEach(() => {
    vi.useRealTimers();
  });

  it("adds new lines on top without a skeleton, keeping the pages already loaded", async () => {
    getEnvironmentLogs
      .mockResolvedValueOnce({ entries: [line("2026-01-01T10:00:02Z", "second"), line("2026-01-01T10:00:01Z", "first")], next_cursor: "c2" })
      .mockResolvedValueOnce({ entries: [line("2026-01-01T10:00:00Z", "older page")] })
      .mockResolvedValue({ entries: [line("2026-01-01T10:00:03Z", "newest"), line("2026-01-01T10:00:02Z", "second")] });
    vi.useFakeTimers();
    renderPanel();
    await vi.waitFor(() => expect(screen.getByText("second")).toBeInTheDocument());

    fireEvent.click(screen.getByRole("button", { name: "Load more" }));
    await vi.waitFor(() => expect(screen.getByText("older page")).toBeInTheDocument());

    fireEvent.click(screen.getByRole("switch", { name: "Live" }));
    await vi.waitFor(() => expect(screen.getByText("newest")).toBeInTheDocument());
    expect(screen.getByText("older page")).toBeInTheDocument();
    expect(screen.getAllByText("second")).toHaveLength(1);
  });

  it("reloads the first page instead of leaving a hole when a tick brings a full page of new lines", async () => {
    const burst = Array.from({ length: 200 }, (_, i) => line(`2026-01-01T11:00:${String(i % 60).padStart(2, "0")}Z`, `burst ${i}`));
    getEnvironmentLogs
      .mockResolvedValueOnce({ entries: [line("2026-01-01T10:00:01Z", "before the burst")], next_cursor: "old" })
      .mockResolvedValue({ entries: burst, next_cursor: "after-burst" });
    vi.useFakeTimers();
    renderPanel();
    await vi.waitFor(() => expect(screen.getByText("before the burst")).toBeInTheDocument());

    fireEvent.click(screen.getByRole("switch", { name: "Live" }));
    await vi.waitFor(() => expect(screen.getByText("burst 0")).toBeInTheDocument());
    expect(screen.queryByText("before the burst")).toBeNull();

    fireEvent.click(screen.getByRole("button", { name: "Load more" }));
    await vi.waitFor(() => expect(getEnvironmentLogs.mock.calls.at(-1)?.[1].cursor).toBe("after-burst"));
  });
});

describe("mergeLiveTail", () => {
  const line = (timestamp: string, message: string) => ({ timestamp, severity: "info" as const, message });

  it("returns the shown list itself when the head brought nothing new", () => {
    const prev = [line("t2", "b"), line("t1", "a")];
    expect(mergeLiveTail(prev, [line("t2", "b"), line("t1", "a")])).toBe(prev);
  });

  it("prepends only the lines it has not shown yet", () => {
    const prev = [line("t2", "b"), line("t1", "a")];
    expect(mergeLiveTail(prev, [line("t3", "c"), line("t2", "b")])?.map((e) => e.message)).toEqual(["c", "b", "a"]);
  });

  it("refuses to merge a full page that shares no line with what is shown", () => {
    const prev = [line("t2", "b"), line("t1", "a")];
    expect(mergeLiveTail(prev, [line("t4", "d"), line("t3", "c")], true)).toBeNull();
    expect(mergeLiveTail(null, [line("t4", "d"), line("t3", "c")], true)).toBeNull();
    expect(mergeLiveTail(prev, [line("t3", "c"), line("t2", "b")], true)?.map((e) => e.message)).toEqual(["c", "b", "a"]);
    expect(mergeLiveTail(prev, [line("t4", "d"), line("t3", "c")], false)?.map((e) => e.message)).toEqual(["d", "c", "b", "a"]);
  });
});
