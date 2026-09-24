import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import type { ProjectScan } from "@/api";
import { ScanProgressList } from "@/components/projects/model/ScanProgressList";
import { I18nProvider } from "@/hooks/useI18n";

function renderScan(scan: ProjectScan | null) {
  return render(
    <I18nProvider>
      <ScanProgressList scan={scan} />
    </I18nProvider>,
  );
}

function makeScan(overrides: Partial<ProjectScan> = {}): ProjectScan {
  return {
    id: "s1",
    repository_id: "r1",
    trigger: "import",
    status: "running",
    events: [],
    review_count: 0,
    started_at: "2026-01-01T00:00:00Z",
    ...overrides,
  };
}

describe("ScanProgressList", () => {
  it("renders every stage in the fixed pipeline order", () => {
    const { container } = renderScan(null);
    const labels = Array.from(container.querySelectorAll("li")).map((li) => li.textContent);
    expect(labels).toEqual([
      "Clone", "Inventory", "Shape", "Components", "Stack", "Checks", "Links", "Deploy", "Match", "Notes",
    ]);
  });

  it("shows a done stage's summary, but only for stages actually marked done", () => {
    const scan = makeScan({
      status: "running",
      stage: "shape",
      events: [
        { stage: "clone", done: true, summary: "Cloned main @ a1b2c3", at: "2026-01-01T00:00:01Z" },
        { stage: "inventory", done: true, summary: "1,204 files", at: "2026-01-01T00:00:02Z" },
      ],
    });
    renderScan(scan);
    expect(screen.getByText("Cloned main @ a1b2c3")).toBeInTheDocument();
    expect(screen.getByText("1,204 files")).toBeInTheDocument();
  });

  it("marks the scan's current stage as running and every later stage as pending", () => {
    const scan = makeScan({
      status: "running",
      stage: "components",
      events: [{ stage: "clone", done: true, summary: "done", at: "2026-01-01T00:00:01Z" }],
    });
    const { container } = renderScan(scan);
    const items = Array.from(container.querySelectorAll("li"));
    const componentsItem = items.find((li) => li.textContent?.startsWith("Components"));
    expect(componentsItem?.querySelector("svg")?.getAttribute("class")).toContain("animate-spin");
    const notesItem = items.find((li) => li.textContent?.startsWith("Notes"));
    expect(notesItem?.querySelector("span")?.className).toContain("text-muted-foreground");
  });
});
