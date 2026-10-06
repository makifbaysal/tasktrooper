import { describe, expect, it } from "vitest";
import { layoutWorkflow } from "@/lib/workflowGraphLayout";

const slugs = [
  "backlog",
  "todo",
  "in_progress",
  "analiz_review",
  "code_review",
  "ready_for_qa",
  "in_qa",
  "need_revision",
  "pm_uat",
  "human_uat",
  "blocked",
  "done",
  "released",
];
const columns = slugs.map((slug, position) => ({ slug, position }));

const chain = [
  ["backlog", "todo"],
  ["todo", "in_progress"],
  ["in_progress", "analiz_review"],
  ["in_progress", "code_review"],
  ["code_review", "ready_for_qa"],
  ["ready_for_qa", "in_qa"],
  ["in_qa", "pm_uat"],
  ["pm_uat", "human_uat"],
  ["human_uat", "done"],
  ["analiz_review", "done"],
  ["done", "released"],
].map(([from, to]) => ({ from, to }));

const intoHubs = ["todo", "in_progress", "analiz_review", "code_review", "ready_for_qa", "in_qa", "pm_uat", "human_uat", "done"]
  .flatMap((from) => [
    { from, to: "blocked" },
    { from, to: "need_revision" },
  ])
  .filter((e) => !(e.from === "todo" && e.to === "need_revision"));

describe("layoutWorkflow", () => {
  it("places every column, and no two on the same spot", () => {
    const positions = layoutWorkflow(columns, [...chain, ...intoHubs]);

    expect(positions.size).toBe(slugs.length);
    const spots = new Set([...positions.values()].map((p) => `${p.x},${p.y}`));
    expect(spots.size).toBe(slugs.length);
  });

  it("keeps the main path in order and the hubs off it", () => {
    const positions = layoutWorkflow(columns, [...chain, ...intoHubs]);
    const p = (slug: string) => positions.get(slug)!;

    expect(p("todo").x).toBeGreaterThan(p("backlog").x);
    expect(p("in_progress").x).toBeGreaterThan(p("todo").x);
    expect(p("analiz_review").x).toBe(p("code_review").x);

    const hubRow = p("blocked").y;
    expect(p("need_revision").y).toBe(hubRow);
    expect(p("backlog").y).toBeLessThan(hubRow);
    expect(p("released").y).toBeGreaterThan(hubRow);
  });

  it("falls back to board order when no rule exists", () => {
    const positions = layoutWorkflow(columns.slice(0, 4), []);

    const xs = slugs.slice(0, 4).map((s) => positions.get(s)!.x);
    expect(xs).toEqual([...xs].sort((a, b) => a - b));
    expect(new Set(xs).size).toBe(4);
  });
});
