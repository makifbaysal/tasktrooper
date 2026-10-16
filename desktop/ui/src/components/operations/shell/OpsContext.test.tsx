import "@testing-library/jest-dom/vitest";
import { act, fireEvent, render, screen } from "@testing-library/react";
import { MemoryRouter, Route, Routes, useLocation } from "react-router-dom";
import { afterEach, describe, expect, it, vi } from "vitest";
import {
  OPS_AUTO_ADVANCE_MS,
  OPS_PRESET_MS,
  OpsProvider,
  previousPeriod,
  readOpsParams,
  resolveRange,
  useOpsContext,
  useOpsHref,
  withOpsParams,
  writeOpsParams,
} from "@/components/operations/shell/OpsContext";

describe("URL state helpers", () => {
  it("defaults to production and the last hour", () => {
    expect(readOpsParams(new URLSearchParams())).toEqual({ preset: "1h", env: "production", compare: false });
  });

  it("reads presets, a custom window, the environment and compare", () => {
    expect(readOpsParams(new URLSearchParams("range=7d&env=staging&compare=1"))).toEqual({ preset: "7d", env: "staging", compare: true });
    expect(readOpsParams(new URLSearchParams("range=custom&from=1000&to=5000"))).toMatchObject({ preset: "custom", from: 1000, to: 5000 });
  });

  it("falls back to the defaults for anything it cannot use", () => {
    expect(readOpsParams(new URLSearchParams("range=2w&env=moon"))).toEqual({ preset: "1h", env: "production", compare: false });
    expect(readOpsParams(new URLSearchParams("range=custom&from=9&to=3")).preset).toBe("1h");
    expect(readOpsParams(new URLSearchParams("range=custom&from=abc")).preset).toBe("1h");
  });

  it("writes only what differs from the defaults and keeps the page's own params", () => {
    const prev = new URLSearchParams("tab=recipes&range=custom&from=1&to=2");
    expect(writeOpsParams(prev, { range: { preset: "1h" } }).toString()).toBe("tab=recipes");
    expect(writeOpsParams(prev, { range: { preset: "6h" }, env: "preview", compare: true }).toString()).toBe(
      "tab=recipes&range=6h&env=preview&compare=1",
    );
    expect(writeOpsParams(new URLSearchParams("env=staging&compare=1"), { env: "production", compare: false }).toString()).toBe("");
  });

  it("slides a preset window to now and keeps a custom one fixed", () => {
    const now = 10_000_000;
    expect(resolveRange({ preset: "3h" }, now)).toEqual({ preset: "3h", from: now - OPS_PRESET_MS["3h"], to: now });
    expect(resolveRange({ preset: "custom", from: 5, to: 9 }, now)).toEqual({ preset: "custom", from: 5, to: 9 });
    expect(previousPeriod({ from: 100, to: 160 })).toEqual({ from: 40, to: 100 });
  });

  it("carries the console's params onto links, never another page's", () => {
    const current = new URLSearchParams("range=24h&env=staging&compare=1&tab=recipes&q=api");
    expect(withOpsParams("/operations/errors", current)).toBe("/operations/errors?range=24h&env=staging&compare=1");
    expect(withOpsParams("/operations/issues?entity=c-1", current)).toBe("/operations/issues?entity=c-1&range=24h&env=staging&compare=1");
    expect(withOpsParams("/operations?env=preview", current)).toBe("/operations?env=preview&range=24h&compare=1");
    expect(withOpsParams("/operations", new URLSearchParams())).toBe("/operations");
  });
});

function Probe() {
  const ops = useOpsContext();
  const href = useOpsHref();
  const location = useLocation();
  return (
    <div>
      <p data-testid="search">{location.search}</p>
      <p data-testid="range">{`${ops.range.preset}:${ops.range.to - ops.range.from}`}</p>
      <p data-testid="env">{ops.env}</p>
      <p data-testid="compare">{String(ops.compare)}</p>
      <p data-testid="key">{ops.refreshKey}</p>
      <p data-testid="to">{ops.range.to}</p>
      <p data-testid="link">{href("/operations/logs")}</p>
      <button onClick={() => ops.setPreset("24h")}>24h</button>
      <button onClick={() => ops.setPreset("1h")}>1h</button>
      <button onClick={() => ops.setCustom(9_000, 3_000)}>custom</button>
      <button onClick={() => ops.setEnv("staging")}>staging</button>
      <button onClick={() => ops.setCompare(true)}>compare</button>
      <button onClick={() => ops.refresh()}>refresh</button>
    </div>
  );
}

function renderAt(path: string) {
  return render(
    <MemoryRouter initialEntries={[path]}>
      <Routes>
        <Route
          path="/operations/*"
          element={
            <OpsProvider>
              <Probe />
            </OpsProvider>
          }
        />
      </Routes>
    </MemoryRouter>,
  );
}

const text = (id: string) => screen.getByTestId(id).textContent;

afterEach(() => {
  vi.useRealTimers();
});

describe("OpsProvider", () => {
  it("reads its state from the URL", () => {
    renderAt("/operations/web?range=12h&env=preview&compare=1");
    expect(text("range")).toBe(`12h:${OPS_PRESET_MS["12h"]}`);
    expect(text("env")).toBe("preview");
    expect(text("compare")).toBe("true");
    expect(text("link")).toBe("/operations/logs?range=12h&env=preview&compare=1");
  });

  it("writes every change back to the URL, defaults left out", () => {
    renderAt("/operations?q=api");
    fireEvent.click(screen.getByText("24h"));
    expect(text("search")).toBe("?q=api&range=24h");
    fireEvent.click(screen.getByText("staging"));
    fireEvent.click(screen.getByText("compare"));
    expect(text("search")).toBe("?q=api&range=24h&env=staging&compare=1");
    fireEvent.click(screen.getByText("1h"));
    expect(text("search")).toBe("?q=api&env=staging&compare=1");
  });

  it("orders a custom window and keeps it in the URL", () => {
    renderAt("/operations");
    fireEvent.click(screen.getByText("custom"));
    expect(text("search")).toBe("?range=custom&from=3000&to=9000");
    expect(text("range")).toBe("custom:6000");
  });

  it("bumps refreshKey and moves `to` to now on refresh", () => {
    vi.useFakeTimers({ toFake: ["Date"] });
    vi.setSystemTime(1_000_000);
    renderAt("/operations");
    expect(text("key")).toBe("0");
    expect(text("to")).toBe("1000000");
    vi.setSystemTime(1_005_000);
    fireEvent.click(screen.getByText("refresh"));
    expect(text("key")).toBe("1");
    expect(text("to")).toBe("1005000");
  });

  it("slides a preset window on its own, without a refetch signal", () => {
    vi.useFakeTimers();
    vi.setSystemTime(2_000_000);
    renderAt("/operations");
    act(() => {
      vi.advanceTimersByTime(OPS_AUTO_ADVANCE_MS);
    });
    expect(Number(text("to"))).toBeGreaterThanOrEqual(2_000_000 + OPS_AUTO_ADVANCE_MS);
    expect(text("key")).toBe("0");
  });
});
