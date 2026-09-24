import "@testing-library/jest-dom/vitest";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { LogsPanel } from "@/components/projects/repository/deploy/LogsPanel";
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
