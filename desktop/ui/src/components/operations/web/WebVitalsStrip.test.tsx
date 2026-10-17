import "@testing-library/jest-dom/vitest";
import { fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { MemoryRouter } from "react-router-dom";
import { beforeEach, describe, expect, it, vi } from "vitest";
import type { SignalSeries } from "@/api";
import { MobileVitals } from "@/components/operations/releases/MobileVitals";
import { WebVitalsStrip } from "@/components/operations/web/WebVitalsStrip";
import { I18nProvider } from "@/hooks/useI18n";
import { writeCache } from "@/lib/uiCache";
import { en } from "@/locales/en";

vi.mock("uplot", () => ({
  default: class {
    static pxRatio = 1;
    static rangeNum = (min: number, max: number) => [min, max];
    setData() {}
    setSize() {}
    redraw() {}
    destroy() {}
  },
}));

const { getComponentSeries, getComponentSignals } = vi.hoisted(() => ({ getComponentSeries: vi.fn(), getComponentSignals: vi.fn() }));

vi.mock("@/api", async () => {
  const actual = await vi.importActual<typeof import("@/api")>("@/api");
  return { ...actual, api: { ...actual.api, getComponentSeries, getComponentSignals } };
});

const NOW = Date.now();

function bound(signal: string, unit: string, series: Array<{ name: string; value: number }>): SignalSeries {
  return {
    signal,
    unit,
    bound: true,
    series: series.map((s) => ({ name: s.name, labels: {}, points: [[NOW - 60_000, s.value * 0.9], [NOW, s.value]] })),
    previous: [],
    error: null,
  };
}

function unbound(signal: string, unit: string): SignalSeries {
  return { signal, unit, bound: false, series: [], previous: [], error: null };
}

function answer(signals: SignalSeries[]) {
  return { from: "", to: "", step_seconds: 60, signals };
}

const t = (key: string, params: Record<string, string | number> = {}) => {
  const value = key.split(".").reduce<unknown>((o, k) => (o as Record<string, unknown> | undefined)?.[k], en);
  return typeof value === "string" ? value.replace(/\{(\w+)\}/g, (m, n: string) => (n in params ? String(params[n]) : m)) : key;
};

beforeEach(() => {
  sessionStorage.clear();
  localStorage.clear();
  writeCache("operations.series.c-web.production.1h.plain.web.lcp,web.inp,web.cls,web.page_views,web.sessions,web.visitors", undefined);
  getComponentSeries.mockReset();
  getComponentSignals.mockReset().mockResolvedValue({ component_id: "c-ios", role: "mobile", environment: "production", signals: [] });
  writeCache("operations.signals.c-ios.production", undefined);
});

describe("WebVitalsStrip", () => {
  function renderStrip(onBind = vi.fn(), onEdit = vi.fn()) {
    render(
      <MemoryRouter>
        <I18nProvider>
          <WebVitalsStrip
            componentId="c-web"
            env="production"
            range={{ preset: "1h", from: NOW - 3_600_000, to: NOW }}
            formatCtx={{ locale: "en", t }}
            onBind={onBind}
            onEdit={onEdit}
          />
        </I18nProvider>
      </MemoryRouter>,
    );
    return { onBind, onEdit };
  }

  it("rates LCP, INP and CLS at p75 against Google's bands, and shows the traffic", async () => {
    getComponentSeries.mockResolvedValue(
      answer([
        bound("web.lcp", "ms", [
          { name: "p50", value: 1200 },
          { name: "p75", value: 3100 },
          { name: "p95", value: 5200 },
          { name: "p99", value: 6000 },
        ]),
        bound("web.inp", "ms", [{ name: "", value: 150 }]),
        bound("web.cls", "score", [{ name: "p75", value: 0.31 }]),
        bound("web.page_views", "views/s", [{ name: "", value: 4.2 }]),
        unbound("web.sessions", "count"),
        { ...unbound("web.visitors", "count"), bound: true, error: { code: "needs_params", message: "no value for host" } },
      ]),
    );
    const { onBind, onEdit } = renderStrip();
    const lcp = (await screen.findByText("LCP", { selector: "p" })).closest("div") as HTMLElement;
    await waitFor(() => expect(within(lcp).getByText("3.1")).toBeInTheDocument());
    expect(within(lcp).getByText("Needs improvement")).toBeInTheDocument();
    expect(within(lcp).getByText("p75")).toBeInTheDocument();
    expect(within(lcp).queryByText("5.2")).toBeNull();
    expect(within(lcp).getByRole("img", { name: "Needs improvement: good up to 2.5 s, poor past 4 s" })).toBeInTheDocument();

    const inp = screen.getByText("INP", { selector: "p" }).closest("div") as HTMLElement;
    expect(within(inp).getByText("Good")).toBeInTheDocument();
    expect(within(inp).getByText("p75")).toBeInTheDocument();
    expect(within(inp).getByText("Good up to 200 ms, poor past 500 ms")).toBeInTheDocument();

    const cls = screen.getByText("CLS", { selector: "p" }).closest("div") as HTMLElement;
    expect(within(cls).getByText("Poor")).toBeInTheDocument();
    expect(within(cls).getByText("0.31")).toBeInTheDocument();
    expect(within(cls).getByText("Critical")).toBeInTheDocument();

    expect(screen.getByText("Page views", { selector: "p" })).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "Bind a data source" }));
    expect(onBind).toHaveBeenCalledWith("web.sessions", "count");
    fireEvent.click(screen.getByRole("button", { name: "Fill in parameters" }));
    expect(onEdit).toHaveBeenCalledWith("web.visitors", "count");
  });
});

describe("MobileVitals", () => {
  it("shows the last 7 days' vitals, a daily one Play has not published yet, and the way to bind the rest", async () => {
    getComponentSignals.mockResolvedValue({
      component_id: "c-ios",
      role: "mobile",
      environment: "production",
      signals: [
        { signal: "mobile.crash_rate", kind: "metric", unit: "%", golden: false, status: "bound", binding: { data_source_kind: "google_play_vitals" }, suggestions: [] },
      ],
    });
    getComponentSeries.mockResolvedValue(
      answer([
        bound("mobile.crash_free_users", "%", [{ name: "", value: 99.4 }]),
        { ...unbound("mobile.crash_rate", "%"), bound: true },
        unbound("mobile.anr_rate", "%"),
        { ...unbound("mobile.cold_start", "ms"), bound: true, error: { code: "timeout", message: "slow" } },
      ]),
    );
    render(
      <MemoryRouter>
        <I18nProvider>
          <MobileVitals componentId="c-ios" env="production" />
        </I18nProvider>
      </MemoryRouter>,
    );
    expect(await screen.findByText("99.4")).toBeInTheDocument();
    const [, query] = getComponentSeries.mock.calls[0];
    expect(query.signals).toEqual(["mobile.crash_free_users", "mobile.crash_rate", "mobile.anr_rate", "mobile.cold_start"]);
    expect(query.to - query.from).toBe(7 * 86_400_000);
    expect(screen.getByText("Not published yet")).toBeInTheDocument();
    expect(screen.getByText(/Google Play's data runs 1–2 days behind/)).toBeInTheDocument();
    expect(screen.getByRole("link", { name: "Bind on Signals" })).toHaveAttribute("href", "/operations/entity/c-ios/signals");
    expect(screen.getByText("The data source didn't answer in time")).toBeInTheDocument();
  });

  it("keeps the slots waiting for a source where no component carries the app", () => {
    render(
      <MemoryRouter>
        <I18nProvider>
          <MobileVitals />
        </I18nProvider>
      </MemoryRouter>,
    );
    expect(screen.getAllByText("No data source bound")).toHaveLength(4);
    expect(getComponentSeries).not.toHaveBeenCalled();
  });
});

describe("MobileVitals with another source", () => {
  it("says no data, not a Play delay, for a vital bound to Sentry", async () => {
    getComponentSignals.mockResolvedValue({
      component_id: "c-ios",
      role: "mobile",
      environment: "production",
      signals: [{ signal: "mobile.crash_rate", kind: "metric", unit: "%", golden: false, status: "bound", binding: { data_source_kind: "sentry" }, suggestions: [] }],
    });
    getComponentSeries.mockResolvedValue(answer([{ ...unbound("mobile.crash_rate", "%"), bound: true }]));
    render(
      <MemoryRouter>
        <I18nProvider>
          <MobileVitals componentId="c-ios" env="production" />
        </I18nProvider>
      </MemoryRouter>,
    );
    expect(await screen.findByText("No data in the last 7 days")).toBeInTheDocument();
    expect(screen.queryByText("Not published yet")).not.toBeInTheDocument();
    expect(screen.queryByText(/Google Play's data runs/)).not.toBeInTheDocument();
  });
});
