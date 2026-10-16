import "@testing-library/jest-dom/vitest";
import { act, fireEvent, render, screen, within } from "@testing-library/react";
import { MemoryRouter } from "react-router-dom";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { I18nProvider } from "@/hooks/useI18n";

const { plots, FakePlot } = vi.hoisted(() => {
  const plots: FakePlotShape[] = [];
  interface FakePlotShape {
    opts: { series: { label?: string; stroke?: string; fill?: string }[]; cursor?: { sync?: { key: string } } };
    data: unknown[];
    destroyed: boolean;
    width: number;
  }
  class FakePlot implements FakePlotShape {
    static pxRatio = 1;
    static rangeNum = (min: number, max: number) => [min, max];
    opts: FakePlotShape["opts"];
    data: unknown[];
    destroyed = false;
    width: number;
    constructor(opts: FakePlotShape["opts"] & { width: number }, data: unknown[]) {
      this.opts = opts;
      this.data = data;
      this.width = opts.width;
      plots.push(this);
    }
    setData(data: unknown[]) {
      this.data = data;
    }
    setSize() {}
    redraw() {}
    destroy() {
      this.destroyed = true;
    }
  }
  return { plots, FakePlot };
});

vi.mock("uplot", () => ({ default: FakePlot }));

import { TimeSeriesChart, type ChartSeries } from "@/components/operations/charts/TimeSeriesChart";

const latency: ChartSeries = { id: "p95", label: "p95", points: [[1_000, 120], [2_000, 180]] };
const p50: ChartSeries = { id: "p50", label: "p50", points: [[1_000, 40], [2_000, 60]], dashed: true };

function renderChart(props: Partial<React.ComponentProps<typeof TimeSeriesChart>> = {}) {
  return render(
    <MemoryRouter>
      <I18nProvider>
        <TimeSeriesChart series={[latency]} ariaLabel="Latency" {...props} />
      </I18nProvider>
    </MemoryRouter>,
  );
}

beforeEach(() => {
  plots.length = 0;
  document.documentElement.classList.remove("dark");
});

describe("TimeSeriesChart", () => {
  it("draws one uPlot with the aligned data and no legend for a single series", () => {
    renderChart({ syncKey: "ops:c1", area: true });
    expect(screen.getByRole("img", { name: "Latency" })).toBeInTheDocument();
    expect(plots).toHaveLength(1);
    expect(plots[0].data).toEqual([[1_000, 2_000], [120, 180]]);
    expect(plots[0].opts.cursor?.sync?.key).toBe("ops:c1");
    expect(plots[0].opts.series[1].fill).toMatch(/^rgba\(/);
    expect(screen.queryByRole("list", { name: "Series" })).not.toBeInTheDocument();
  });

  it("shows a legend row from two series up", () => {
    renderChart({ series: [latency, p50] });
    const legend = screen.getByRole("list", { name: "Series" });
    expect(within(legend).getByText("p95")).toBeInTheDocument();
    expect(within(legend).getByText("p50")).toBeInTheDocument();
  });

  it("renders the empty state with its label and no plot", () => {
    renderChart({ series: [], emptyLabel: "No metrics source connected" });
    expect(screen.getByText("No metrics source connected")).toBeInTheDocument();
    expect(plots).toHaveLength(0);
  });

  it("falls back to the default empty label", () => {
    renderChart({ series: [{ id: "x", label: "X", points: [[1, null]] }] });
    expect(screen.getByText("No data in this range")).toBeInTheDocument();
  });

  it("shows a skeleton while the first load runs", () => {
    renderChart({ series: [], loading: true });
    expect(screen.getByRole("status", { name: "Loading chart" })).toBeInTheDocument();
    expect(screen.queryByText("No data in this range")).not.toBeInTheDocument();
  });

  it("swaps the canvas for a table of the same values and back", () => {
    renderChart({
      series: [latency, p50],
      unit: "ms",
      showTableToggle: true,
      deployMarkers: [{ at: 1_500, label: "v1.4.0" }],
      thresholds: [{ value: 150, label: "SLO" }],
    });
    const toggle = screen.getByRole("button", { name: "Show as table" });
    expect(toggle).toHaveAttribute("aria-pressed", "false");
    fireEvent.click(toggle);
    expect(toggle).toHaveAttribute("aria-pressed", "true");
    expect(plots[0].destroyed).toBe(true);
    const table = screen.getByRole("table", { name: "Latency" });
    expect(within(table).getByRole("columnheader", { name: "p95 (ms)" })).toBeInTheDocument();
    expect(within(table).getAllByRole("row")).toHaveLength(3);
    expect(within(table).getByText("180")).toBeInTheDocument();
    expect(screen.getByText("v1.4.0")).toBeInTheDocument();
    expect(screen.getByText("SLO")).toBeInTheDocument();

    fireEvent.click(screen.getByRole("button", { name: "Show as table" }));
    expect(toggle).toHaveAttribute("aria-pressed", "false");
    expect(screen.queryByRole("table")).not.toBeInTheDocument();
    expect(plots).toHaveLength(2);
  });

  it("feeds new points to the same plot", () => {
    const { rerender } = renderChart();
    rerender(
      <MemoryRouter>
        <I18nProvider>
          <TimeSeriesChart series={[{ ...latency, points: [[1_000, 120], [2_000, 180], [3_000, 90]] }]} ariaLabel="Latency" />
        </I18nProvider>
      </MemoryRouter>,
    );
    expect(plots).toHaveLength(1);
    expect(plots[0].data).toEqual([[1_000, 2_000, 3_000], [120, 180, 90]]);
  });

  it("rebuilds the plot when the theme flips and destroys it on unmount", async () => {
    const { unmount } = renderChart();
    await act(async () => {
      document.documentElement.style.setProperty("--chart-1", "#3987e5");
      document.documentElement.classList.add("dark");
      await Promise.resolve();
    });
    expect(plots).toHaveLength(2);
    expect(plots[0].destroyed).toBe(true);
    expect(plots[1].opts.series[1].stroke).toBe("#3987e5");
    unmount();
    expect(plots[1].destroyed).toBe(true);
    document.documentElement.style.removeProperty("--chart-1");
  });
});
