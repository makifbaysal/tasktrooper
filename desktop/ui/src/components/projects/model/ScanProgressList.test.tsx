import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import type { ProjectScan } from "@/api";
import { type CloneProgress, ScanProgressList } from "@/components/projects/model/ScanProgressList";
import { I18nProvider } from "@/hooks/useI18n";

function renderScan(scan: ProjectScan | null, clone?: CloneProgress) {
  return render(
    <I18nProvider>
      <ScanProgressList scan={scan} clone={clone} />
    </I18nProvider>,
  );
}

function iconClass(container: HTMLElement, label: string) {
  const item = Array.from(container.querySelectorAll("li")).find((li) => li.textContent?.startsWith(label));
  return item?.querySelector("svg")?.getAttribute("class") ?? "";
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
    const { container } = renderScan(null, { state: "running" });
    const labels = Array.from(container.querySelectorAll("li")).map((li) => li.textContent);
    expect(labels).toEqual([
      "Clone", "Inventory", "Shape", "Components", "Stack", "Checks", "Links", "Deploy", "Match", "Notes",
    ]);
  });

  it("leaves the clone stage out of a scan that did not clone", () => {
    const { container } = renderScan(makeScan());
    const labels = Array.from(container.querySelectorAll("li")).map((li) => li.textContent);
    expect(labels[0]).toBe("Inventory");
  });

  it("drives the clone stage from the caller: spinner, tick, or the error", () => {
    const running = renderScan(null, { state: "running" });
    expect(iconClass(running.container, "Clone")).toContain("animate-spin");
    running.unmount();

    const done = renderScan(makeScan(), { state: "done" });
    expect(iconClass(done.container, "Clone")).toContain("text-success");
    done.unmount();

    const failed = renderScan(null, { state: "failed", error: "repository not found" });
    expect(iconClass(failed.container, "Clone")).toContain("text-destructive");
    expect(screen.getByText("repository not found")).toBeInTheDocument();
  });

  it("marks the stage a failed scan stopped on as failed, with the scan's error", () => {
    const scan = makeScan({
      status: "failed",
      stage: "shape",
      error: "walk: permission denied",
      events: [
        { stage: "inventory", done: true, summary: "12 files", at: "2026-01-01T00:00:01Z" },
        { stage: "shape", done: false, at: "2026-01-01T00:00:02Z" },
      ],
    });
    const { container } = renderScan(scan);
    expect(iconClass(container, "Shape")).toContain("text-destructive");
    expect(screen.getByText("walk: permission denied")).toBeInTheDocument();
    expect(iconClass(container, "Components")).not.toContain("text-destructive");
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
