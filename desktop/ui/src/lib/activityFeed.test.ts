import { describe, expect, it } from "vitest";
import type { OrchestrationPlan, SessionStep } from "@/api";
import {
  buildActivityFeed,
  classifyTool,
  INTERRUPTED_ERROR,
  isRunLive,
  mergeSteps,
  toolTarget,
  type FeedItem,
  type FeedLane,
  type FeedRunInput,
} from "@/lib/activityFeed";

let counter = 0;
function step(step_type: string, payload: Record<string, unknown> | null = {}, runId = "run-1"): SessionStep {
  counter += 1;
  const id = String(counter).padStart(4, "0");
  const seconds = String(counter % 60).padStart(2, "0");
  const minutes = String(Math.floor(counter / 60)).padStart(2, "0");
  return { id, run_id: runId, step_type, payload, created_at: `2026-01-01T00:${minutes}:${seconds}.000Z` };
}

function run(steps: SessionStep[], live = false, runId = "run-1"): FeedRunInput {
  return { runId, live, steps };
}

const start = (tool: string, call_id: string, args: unknown = {}, extra: Record<string, unknown> = {}) =>
  step("tool_call_start", { tool, call_id, arguments: JSON.stringify(args), ...extra });
const result = (tool: string, call_id: string, content = "ok", is_error = false, extra: Record<string, unknown> = {}) =>
  step("tool_call_result", { tool, call_id, content, is_error, ...extra });

const body = (f: { items: FeedItem[] }) => f.items.filter((i) => i.kind !== "run");

function lanes(items: FeedItem[]): FeedLane[] {
  return items.flatMap((i) => (i.kind === "lane" ? [i.lane] : []));
}

describe("buildActivityFeed — sub-agents", () => {
  it("nests Claude Code sub-agent steps under their lane via parent_call_id", () => {
    const feed = buildActivityFeed([
      run([
        step("user_message", { content: "analyse auth" }),
        step("claude_code_session", { model: "opus" }),
        start("subagent", "sa1", { description: "find auth flow", subagent_type: "Explore", prompt: "p" }),
        step("assistant_message", { content: "inside sub-agent", parent_call_id: "sa1" }),
        start("read_file", "r1", { file_path: "/a/b/run.go" }, { parent_call_id: "sa1" }),
        result("read_file", "r1", "package run", false, { parent_call_id: "sa1" }),
        result("subagent", "sa1", "found it"),
        step("assistant_message", { content: "Done" }),
      ]),
    ]);

    expect(body(feed).map((i) => i.kind)).toEqual(["user", "lane", "say"]);
    const lane = lanes(body(feed))[0];
    expect(lane.source).toBe("subagent");
    expect(lane.title).toBe("find auth flow");
    expect(lane.agentName).toBe("Explore");
    expect(lane.status).toBe("completed");
    expect(lane.result).toBe("found it");
    expect(lane.items.map((i) => i.kind)).toEqual(["say", "tools"]);
    expect(lane.toolCount).toBe(1);
    expect(feed.stats).toMatchObject({ toolCalls: 1, subagents: 1, failures: 0, model: "opus" });
  });

  it("keeps sub-agent narration out of the top level", () => {
    const feed = buildActivityFeed([
      run([
        start("subagent", "sa1", { description: "d" }),
        step("assistant_message", { content: "child says hi", parent_call_id: "sa1" }),
        result("subagent", "sa1", "r"),
      ]),
    ]);
    expect(body(feed).map((i) => i.kind)).toEqual(["lane"]);
    expect(lanes(body(feed))[0].items.map((i) => i.kind)).toEqual(["say"]);
  });

  it("handles old runs without parent ids and the Task tool name", () => {
    const feed = buildActivityFeed([
      run([
        start("Task", "t1", { description: "legacy", subagent_type: "Plan" }),
        start("read_file", "r1", { path: "x.go" }),
        result("read_file", "r1"),
        result("Task", "t1", "legacy result"),
      ]),
    ]);
    expect(body(feed).map((i) => i.kind)).toEqual(["lane", "tools"]);
    expect(lanes(body(feed))[0]).toMatchObject({ title: "legacy", status: "completed", result: "legacy result" });
  });

  it("marks a failed sub-agent result", () => {
    const feed = buildActivityFeed([
      run([start("subagent", "s", { description: "d" }), result("subagent", "s", "boom", true)]),
    ]);
    expect(lanes(body(feed))[0]).toMatchObject({ status: "failed", error: "boom" });
  });
});

describe("buildActivityFeed — subtasks", () => {
  it("routes interleaved parallel subtasks by task_key", () => {
    const feed = buildActivityFeed([
      run([
        step("subtask_started", { task_key: "A", title: "Alpha", agent: "dev" }),
        step("subtask_started", { task_key: "B", title: "Beta" }),
        start("read_file", "a1", { path: "a.go" }, { task_key: "A" }),
        start("grep_code", "b1", { pattern: "foo" }, { task_key: "B" }),
        result("read_file", "a1", "ok", false, { task_key: "A" }),
        result("grep_code", "b1", "ok", false, { task_key: "B" }),
        step("subtask_completed", { task_key: "A", result: "A done" }),
        step("subtask_failed", { task_key: "B", error: "B broke" }),
      ]),
    ]);
    const [a, b] = lanes(body(feed));
    expect(a.title).toBe("Alpha");
    expect(a.agentName).toBe("dev");
    expect(a.items).toHaveLength(1);
    expect(a.items[0]).toMatchObject({ kind: "tools", toolKind: "read" });
    expect(a).toMatchObject({ status: "completed", result: "A done" });
    expect(b.items[0]).toMatchObject({ kind: "tools", toolKind: "search" });
    expect(b).toMatchObject({ status: "failed", error: "B broke" });
    expect(body(feed).filter((i) => i.kind === "tools")).toHaveLength(0);
  });

  it("falls back to position when exactly one subtask is open", () => {
    const feed = buildActivityFeed([
      run([
        step("subtask_started", { task_key: "A", title: "Alpha" }),
        start("bash", "c1", { command: "ls" }),
        result("bash", "c1"),
        step("subtask_incomplete", { task_key: "A", reason: "no output" }),
        start("bash", "c2", { command: "pwd" }),
        result("bash", "c2"),
      ]),
    ]);
    const lane = lanes(body(feed))[0];
    expect(lane.items).toHaveLength(1);
    expect(lane).toMatchObject({ status: "incomplete", error: "no output" });
    expect(body(feed).map((i) => i.kind)).toEqual(["lane", "tools"]);
  });

  it("does not guess when two subtasks are open", () => {
    const feed = buildActivityFeed([
      run([
        step("subtask_started", { task_key: "A", title: "A" }),
        step("subtask_started", { task_key: "B", title: "B" }),
        start("bash", "c1", { command: "ls" }),
      ]),
    ]);
    expect(body(feed).map((i) => i.kind)).toEqual(["lane", "lane", "tools"]);
  });

  it("reuses the lane when a subtask is retried with the same key", () => {
    const feed = buildActivityFeed([
      run([
        step("subtask_started", { task_key: "A", title: "A" }),
        step("subtask_failed", { task_key: "A", error: "x" }),
        step("subtask_started", { task_key: "A", title: "A again" }),
      ], true),
    ]);
    expect(lanes(body(feed))).toHaveLength(1);
    expect(lanes(body(feed))[0]).toMatchObject({ title: "A again", status: "running", error: undefined });
  });
});

describe("buildActivityFeed — tools and say", () => {
  it("merges consecutive same-kind tools, and a say breaks the group", () => {
    const feed = buildActivityFeed([
      run([
        ...["a", "b", "c", "d"].flatMap((n) => [start("read_file", n, { path: `${n}.go` }), result("read_file", n)]),
        step("assistant_message", { content: "thinking out loud" }),
        start("read_file", "e", { path: "e.go" }),
        result("read_file", "e"),
      ]),
    ]);
    expect(body(feed).map((i) => i.kind)).toEqual(["tools", "say", "tools"]);
    const first = body(feed)[0];
    expect(first.kind === "tools" && first.tools).toHaveLength(4);
    expect(first.kind === "tools" && first.status).toBe("completed");
  });

  it("starts a new group when the kind changes", () => {
    const feed = buildActivityFeed([
      run([start("read_file", "a", { path: "a" }), result("read_file", "a"), start("grep_code", "b", { pattern: "p" }), result("grep_code", "b")]),
    ]);
    expect(body(feed).map((i) => (i.kind === "tools" ? i.toolKind : i.kind))).toEqual(["read", "search"]);
  });

  it("turns tool_calls_planned into a say plus pending tools that start and finish", () => {
    const planned = step("tool_calls_planned", {
      content: "Let me look",
      tool_calls: [{ id: "p1", name: "read_file", arguments: '{"path":"x.go"}' }],
    });
    const feed = buildActivityFeed([run([planned], true)]);
    expect(body(feed).map((i) => i.kind)).toEqual(["say", "tools"]);
    const group = body(feed)[1];
    expect(group.kind === "tools" && group.tools[0].status).toBe("pending");

    const done = buildActivityFeed([
      run([planned, start("read_file", "p1", { path: "x.go" }), result("read_file", "p1", "x", false, { image_attachment_ids: ["img"] })]),
    ]);
    const g = body(done)[1];
    expect(g.kind === "tools" && g.tools[0]).toMatchObject({ status: "completed", target: "x.go", imageIds: ["img"] });
    expect(g.kind === "tools" && g.status).toBe("completed");
  });

  it("group status is failed when any tool errored and running while one runs", () => {
    const feed = buildActivityFeed([
      run([start("bash", "1", { command: "a" }), result("bash", "1", "no", true), start("bash", "2", { command: "b" })], true),
    ]);
    const g = body(feed)[0];
    expect(g.kind === "tools" && g.status).toBe("running");
    expect(feed.stats.failures).toBe(1);
  });
});

describe("buildActivityFeed — events", () => {
  it("collapses start/end pairs into one updated item", () => {
    const feed = buildActivityFeed([
      run([
        step("build_verification_start", { attempt: 1 }),
        step("build_verification_failed", { report: "go vet failed" }),
        step("build_verification_start", { attempt: 2 }),
        step("build_verification_passed"),
        step("planner_start"),
        step("planner_complete", { task_count: 3 }),
      ]),
    ]);
    expect(body(feed)).toHaveLength(3);
    expect(body(feed)[0]).toMatchObject({ kind: "event", stepType: "build_verification_failed", tone: "danger", status: "failed", detail: "go vet failed" });
    expect(body(feed)[1]).toMatchObject({ stepType: "build_verification_passed", tone: "success", status: "completed" });
    expect(body(feed)[2]).toMatchObject({ stepType: "planner_complete", tone: "info" });
  });

  it("keeps an unfinished pair running while live", () => {
    const feed = buildActivityFeed([run([step("verification_start")], true)]);
    expect(body(feed)[0]).toMatchObject({ status: "running", stepType: "verification_start" });
  });

  it("hides bookkeeping steps and only surfaces a failed claude_code_result", () => {
    const quiet = buildActivityFeed([
      run([
        step("iteration_start", { iteration: 1 }),
        step("llm_request", { model: "m" }),
        step("history_trimmed"),
        step("claude_code_result", { subtype: "success", cost_usd: 0.12, model: "opus" }),
        step("orchestration_complete"),
      ]),
    ]);
    expect(quiet.items.map((i) => i.kind)).toEqual(["run"]);
    expect(quiet.stats.costUsd).toBeCloseTo(0.12);

    const failed = buildActivityFeed([run([step("claude_code_result", { subtype: "error_during_execution" })])]);
    expect(failed.items[1]).toMatchObject({ kind: "event", tone: "danger" });
  });

  it("maps tones and the plan id", () => {
    const feed = buildActivityFeed([
      run([
        step("orchestration_plan_created", { plan_id: "p9", summary: "do it", task_count: 2 }),
        step("clarification_requested", { questions: [{ question: "Which db?" }] }),
        step("loop_guard_repeat", { tool: "bash" }),
        step("something_new", {}),
      ]),
    ]);
    const events = body(feed).filter((i) => i.kind === "event");
    expect(events[0]).toMatchObject({ tone: "info", planId: "p9", detail: "do it" });
    expect(events[1]).toMatchObject({ tone: "warning", detail: "Which db?" });
    expect(events[2]).toMatchObject({ tone: "warning" });
    expect(events[3]).toMatchObject({ tone: "muted", stepType: "something_new" });
  });
});

describe("buildActivityFeed — liveness", () => {
  it("marks everything unfinished as failed when the run is not live", () => {
    const feed = buildActivityFeed([
      run([
        start("subagent", "s", { description: "d" }),
        start("bash", "c", { command: "sleep" }, { parent_call_id: "s" }),
        step("planner_start"),
      ], false),
    ]);
    const lane = lanes(body(feed))[0];
    expect(lane).toMatchObject({ status: "failed", error: INTERRUPTED_ERROR, current: null });
    expect(lane.items[0]).toMatchObject({ kind: "tools", status: "failed" });
    expect(body(feed)[1]).toMatchObject({ kind: "event", status: "failed" });
    expect(feed.current).toBeNull();
  });
});

describe("buildActivityFeed — current", () => {
  it("reports the running tool", () => {
    const feed = buildActivityFeed([run([step("user_message", { content: "go" }), start("read_file", "r", { path: "/x/run.go" })], true)]);
    expect(feed.current).toMatchObject({ kind: "tool", tool: { name: "read_file", target: "run.go" } });
  });

  it("is thinking after a tool result or iteration start", () => {
    const afterResult = buildActivityFeed([run([start("bash", "c", { command: "ls" }), result("bash", "c")], true)]);
    expect(afterResult.current?.kind).toBe("thinking");
    const afterIter = buildActivityFeed([run([step("iteration_start", { iteration: 2 })], true)]);
    expect(afterIter.current?.kind).toBe("thinking");
  });

  it("is starting with no steps", () => {
    expect(buildActivityFeed([{ runId: "r", live: true, startedAt: "2026-01-01T00:00:00Z", steps: [] }]).current?.kind).toBe("starting");
  });

  it("is verifying / planning / waitingUser", () => {
    expect(buildActivityFeed([run([step("build_verification_start")], true)]).current?.kind).toBe("verifying");
    expect(buildActivityFeed([run([step("planner_start")], true)]).current?.kind).toBe("planning");
    expect(buildActivityFeed([run([step("clarification_requested", { questions: ["q"] })], true)]).current?.kind).toBe("waitingUser");
  });

  it("points into the running sub-agent and counts parallel lanes", () => {
    const feed = buildActivityFeed([
      run([
        start("subagent", "s1", { description: "one", subagent_type: "Explore" }),
        start("subagent", "s2", { description: "two" }),
        start("grep_code", "g", { pattern: "x" }, { parent_call_id: "s1" }),
      ], true),
    ]);
    expect(feed.current).toMatchObject({ kind: "tool", laneTitle: "one", laneAgent: "Explore", parallel: 1 });
    expect(lanes(body(feed))[0].current?.kind).toBe("tool");
  });

  it("is null when the newest run is not live", () => {
    expect(buildActivityFeed([run([start("bash", "c")], true), run([step("assistant_message", { content: "x" })], false, "run-2")]).current).toBeNull();
  });
});

describe("buildActivityFeed — multiple runs", () => {
  it("appends runs into one feed with a boundary only for runs without a user message", () => {
    const feed = buildActivityFeed([
      run([step("user_message", { content: "first" }), step("assistant_message", { content: "a" })], false, "r1"),
      run([step("quota_resume", { resumed: true }), step("assistant_message", { content: "b" })], false, "r2"),
      run([step("user_message", { content: "third" })], true, "r3"),
    ]);
    expect(feed.items.map((i) => i.kind)).toEqual(["user", "say", "run", "event", "say", "user"]);
    expect(feed.items[2]).toMatchObject({ kind: "run", runId: "r2" });
  });

  it("keeps lane ids unique when two runs use the same task key", () => {
    const sub = (runId: string) =>
      run([step("subtask_started", { task_key: "T", title: "T" }), step("subtask_completed", { task_key: "T" })], false, runId);
    const feed = buildActivityFeed([sub("r1"), sub("r2")]);
    expect(new Set(feed.lanes.map((l) => l.id)).size).toBe(2);
  });

  it("carries the newest plan", () => {
    const plan = { id: "p", run_id: "r", status: "completed", summary: "s", tasks: [] } as OrchestrationPlan;
    expect(buildActivityFeed([{ runId: "r", live: false, steps: [], plan }]).plan).toBe(plan);
  });
});

describe("classifyTool", () => {
  it.each([
    ["read_file", "read"],
    ["Read", "read"],
    ["get_file_contents", "read"],
    ["grep_code", "search"],
    ["Glob", "search"],
    ["search_board_tasks", "search"],
    ["get_repo_tree", "search"],
    ["edit_file", "edit"],
    ["MultiEdit", "edit"],
    ["str_replace_based_edit", "edit"],
    ["write_file", "write"],
    ["run_terminal", "shell"],
    ["Bash", "shell"],
    ["BashOutput", "shell"],
    ["browser_click", "browser"],
    ["mobile_tap", "browser"],
    ["take_screenshot", "browser"],
    ["web_search", "web"],
    ["http_request", "web"],
    ["git_push", "git"],
    ["create_pull_request", "git"],
    ["update_board_task", "board"],
    ["add_task_comment", "board"],
    ["attach_task_document", "board"],
    ["TodoWrite", "plan"],
    ["subagent", "subagent"],
    ["Task", "subagent"],
    ["Agent", "subagent"],
    ["mcp__github__create_issue", "other"],
    ["mcp__srv__read_file", "read"],
    ["mystery", "other"],
  ])("%s -> %s", (name, kind) => {
    expect(classifyTool(name)).toBe(kind);
  });
});

describe("toolTarget", () => {
  it("never throws on bad input", () => {
    expect(toolTarget("read", "{not json")).toEqual({});
    expect(toolTarget("read", undefined)).toEqual({});
    expect(toolTarget("other", "[1,2]")).toEqual({});
  });

  it.each([
    ["read", { file_path: "/a/b/c.go" }, "c.go", "/a/b/c.go"],
    ["edit", { filePath: "x/y.ts" }, "y.ts", "x/y.ts"],
    ["write", { notebook_path: "n.ipynb" }, "n.ipynb", "n.ipynb"],
    ["search", { pattern: "SessionToken", path: "internal" }, '"SessionToken"', "internal"],
    ["shell", { command: "go test ./...\nsecond line" }, "go test ./...", "go test ./...\nsecond line"],
    ["web", { url: "https://example.com/a" }, "example.com/a", "https://example.com/a"],
    ["browser", { selector: "#go" }, "#go", "#go"],
    ["board", { task_key: "TT-12" }, "TT-12", "TT-12"],
    ["git", { branch: "feat/x" }, "feat/x", "feat/x"],
    ["other", { n: 1, label: "short" }, "short", undefined],
  ] as const)("%s", (kind, args, target, detail) => {
    expect(toolTarget(kind, JSON.stringify(args))).toEqual(detail === undefined ? { target } : { target, detail });
  });

  it("clips long values", () => {
    const out = toolTarget("shell", JSON.stringify({ command: "x".repeat(100) }));
    expect(out.target).toHaveLength(60);
    expect(out.target?.endsWith("…")).toBe(true);
    expect(toolTarget("search", JSON.stringify({ pattern: "y".repeat(100) })).target).toHaveLength(42);
  });
});

describe("isRunLive", () => {
  const plan = (status: string) => ({ id: "p", run_id: "r", status, summary: "", tasks: [] }) as OrchestrationPlan;
  it("lets the run row outrank the steps and the plan", () => {
    expect(isRunLive("running", [], plan("completed"))).toBe(true);
    expect(isRunLive("completed", [], null)).toBe(false);
    expect(isRunLive("cancelled", [], null)).toBe(false);
  });
  it("falls back to the step stream without a row status", () => {
    expect(isRunLive(undefined, [], null)).toBe(true);
    expect(isRunLive(undefined, [step("assistant_message")], null)).toBe(false);
    expect(isRunLive(undefined, [step("claude_code_result")], null)).toBe(false);
    expect(isRunLive(undefined, [step("assistant_message")], plan("running"))).toBe(true);
  });
});

describe("mergeSteps", () => {
  it("dedupes by id, keeps the previous array when nothing is new, and sorts by time", () => {
    const a = step("a");
    const b = step("b");
    const c = step("c");
    const prev = [a, c];
    expect(mergeSteps(prev, [c])).toBe(prev);
    expect(mergeSteps(prev, [b, c]).map((s) => s.id)).toEqual([a.id, b.id, c.id]);
  });
});
