import type { OrchestrationPlan, SessionStep } from "@/api";

export type FeedStatus = "pending" | "running" | "completed" | "failed" | "incomplete";
export type ToolKind =
  | "read"
  | "search"
  | "edit"
  | "write"
  | "shell"
  | "browser"
  | "web"
  | "board"
  | "git"
  | "plan"
  | "subagent"
  | "other";

export interface FeedRunInput {
  runId: string;
  live: boolean;
  startedAt?: string;
  steps: SessionStep[];
  plan?: OrchestrationPlan | null;
}

export interface FeedTool {
  id: string;
  name: string;
  kind: ToolKind;
  target?: string;
  targetDetail?: string;
  arguments?: string;
  result?: string;
  isError: boolean;
  imageIds?: string[];
  status: FeedStatus;
  startedAt: string;
  endedAt?: string;
}

export interface FeedLane {
  id: string;
  source: "main" | "subtask" | "subagent";
  title: string;
  agentName?: string;
  model?: string;
  status: FeedStatus;
  items: FeedItem[];
  result?: string;
  error?: string;
  toolCount: number;
  startedAt: string;
  endedAt?: string;
  current: FeedCurrent | null;
}

export type FeedTone = "muted" | "info" | "success" | "warning" | "danger";

export type FeedItem =
  | { kind: "user"; id: string; content: string; at: string; runId: string }
  | { kind: "say"; id: string; content: string; at: string }
  | { kind: "tools"; id: string; toolKind: ToolKind; tools: FeedTool[]; status: FeedStatus; at: string }
  | { kind: "lane"; id: string; lane: FeedLane }
  | {
      kind: "event";
      id: string;
      stepType: string;
      tone: FeedTone;
      status: FeedStatus;
      detail?: string;
      at: string;
      payload: Record<string, unknown>;
      planId?: string;
    }
  | { kind: "run"; id: string; runId: string; at: string };

export interface FeedCurrent {
  kind: "tool" | "thinking" | "verifying" | "planning" | "waitingUser" | "starting";
  tool?: FeedTool;
  laneTitle?: string;
  laneAgent?: string;
  parallel?: number;
  since: string;
}

export interface ActivityFeed {
  items: FeedItem[];
  lanes: FeedLane[];
  current: FeedCurrent | null;
  stats: { toolCalls: number; failures: number; subagents: number; costUsd?: number; model?: string };
  plan: OrchestrationPlan | null;
}

// A lane error the view localizes: the run ended while the lane was still going.
export const INTERRUPTED_ERROR = "interrupted";

const TERMINAL_STEP_TYPES = new Set([
  "orchestration_complete",
  "verification_complete",
  "verification_failed",
  // A Claude Code run's own terminal steps. Without them a finished CLI run
  // whose row status we do not have (the board passes one, the chat page does
  // not) kept polling and kept the "Live" badge up forever, because its last
  // step is neither an assistant_message nor any of the loop's endings.
  "claude_code_result",
  "llm_provider_code_quota_park",
]);

// Statuses the run row itself reports as over. The step stream cannot be
// trusted alone: a run killed mid-flight (pod terminated, reconciler stale
// sweep) simply stops emitting steps, so its last step is an ordinary one and
// the graph showed "Live" with a spinning subtask forever.
const TERMINAL_RUN_STATUSES = new Set(["completed", "failed", "cancelled", "canceled", "error"]);

// Statuses that mean the run row itself is still in flight. The row is the
// authority when we have it: the orchestration plan settles as soon as the last
// subtask returns, but a board run keeps going after that — build/vet
// verification, up to N LLM fix rounds, the commit and push. Treating the
// settled plan as the end of the run stopped the poll mid-work and, because
// every still-running subtask is rewritten as "interrupted" once the run is not
// live, painted a working agent as failed.
const LIVE_RUN_STATUSES = new Set(["running", "pending", "queued", "in_progress"]);

function isRunComplete(steps: SessionStep[], plan: OrchestrationPlan | null): boolean {
  if (plan) {
    const status = plan.status.toLowerCase();
    if (status === "completed" || status === "failed") return true;
  }
  if (steps.length === 0) return false;
  const lastType = steps[steps.length - 1].step_type;
  if (TERMINAL_STEP_TYPES.has(lastType)) return true;
  if (!plan && lastType === "assistant_message") return true;
  return false;
}

export function isRunLive(
  runStatus: string | null | undefined,
  steps: SessionStep[],
  plan: OrchestrationPlan | null,
): boolean {
  const status = (runStatus ?? "").toLowerCase();
  if (TERMINAL_RUN_STATUSES.has(status)) return false;
  // A run row that says it is still running outranks anything the step stream
  // or the settled plan suggests — see LIVE_RUN_STATUSES.
  if (LIVE_RUN_STATUSES.has(status)) return true;
  return !isRunComplete(steps, plan);
}

// Steps arrive from an incremental poll whose `since` is inclusive, so the
// boundary step comes back every time; ids make that harmless.
export function mergeSteps(prev: SessionStep[], incoming: SessionStep[]): SessionStep[] {
  if (incoming.length === 0) return prev;
  const seen = new Set(prev.map((s) => s.id));
  const fresh = incoming.filter((s) => !seen.has(s.id));
  if (fresh.length === 0) return prev;
  const merged = [...prev, ...fresh];
  const time = new Map(merged.map((s) => [s.id, Date.parse(s.created_at) || 0]));
  return merged
    .map((s, index) => ({ s, index }))
    .sort((a, b) => (time.get(a.s.id)! - time.get(b.s.id)!) || a.index - b.index)
    .map((x) => x.s);
}

const SUBAGENT_NAMES = new Set(["subagent", "task", "agent"]);
const PLAN_NAMES = new Set(["todowrite", "todo_write", "update_plan", "update_todos"]);
const EDIT_NAMES = new Set(["edit_file", "edit", "multiedit", "notebookedit", "apply_patch", "replace_in_file"]);
const WRITE_NAMES = new Set(["write_file", "write", "create_file"]);
const READ_NAMES = new Set(["read_file", "read", "view", "cat", "open_file"]);
const SEARCH_NAMES = new Set([
  "grep_code",
  "grep",
  "glob",
  "get_repo_tree",
  "ls",
  "list_dir",
  "list_files",
  "codebase_search",
]);
const SHELL_NAMES = new Set([
  "run_terminal",
  "bash",
  "shell",
  "exec",
  "run_command",
  "run_terminal_cmd",
  "bashoutput",
  "killshell",
]);
const WEB_NAMES = new Set(["fetch_url", "web_search", "webfetch", "websearch", "http_request", "fetch"]);

export function stripMcpPrefix(name: string): string {
  const match = /^mcp__.+?__(.+)$/i.exec(name);
  return match ? match[1] : name;
}

export function classifyTool(name: string): ToolKind {
  const n = stripMcpPrefix(name).toLowerCase();
  if (SUBAGENT_NAMES.has(n)) return "subagent";
  if (PLAN_NAMES.has(n)) return "plan";
  if (EDIT_NAMES.has(n) || n.startsWith("str_replace")) return "edit";
  if (WRITE_NAMES.has(n)) return "write";
  if (READ_NAMES.has(n) || n.startsWith("get_file") || n.startsWith("read_")) return "read";
  if (SEARCH_NAMES.has(n) || n.startsWith("search") || n.startsWith("find")) return "search";
  if (SHELL_NAMES.has(n)) return "shell";
  if (
    n.startsWith("browser_") ||
    n.startsWith("playwright") ||
    n.startsWith("puppeteer") ||
    n.startsWith("mobile_") ||
    n.includes("screenshot")
  )
    return "browser";
  if (WEB_NAMES.has(n)) return "web";
  if (n.startsWith("git_") || n.startsWith("gh_") || n.includes("pull_request") || n.includes("commit") || n.includes("merge"))
    return "git";
  if (
    n.includes("task") ||
    n.includes("board") ||
    n.includes("comment") ||
    n.includes("column") ||
    n.includes("criteri") ||
    n.includes("document") ||
    n.includes("clarification") ||
    n.includes("release")
  )
    return "board";
  return "other";
}

function clip(text: string, max: number): string {
  const flat = text.trim();
  return flat.length > max ? `${flat.slice(0, max - 1)}…` : flat;
}

function basename(path: string): string {
  const parts = path.split(/[\\/]/).filter(Boolean);
  return parts.length > 0 ? parts[parts.length - 1] : path;
}

function parseObject(json: unknown): Record<string, unknown> | null {
  if (json && typeof json === "object" && !Array.isArray(json)) return json as Record<string, unknown>;
  if (typeof json !== "string" || !json.trim()) return null;
  try {
    const parsed: unknown = JSON.parse(json);
    return parsed && typeof parsed === "object" && !Array.isArray(parsed) ? (parsed as Record<string, unknown>) : null;
  } catch {
    return null;
  }
}

function firstString(obj: Record<string, unknown>, keys: string[]): string | undefined {
  for (const key of keys) {
    const value = obj[key];
    if (typeof value === "string" && value.trim()) return value;
  }
  return undefined;
}

const PATH_KEYS = ["file_path", "path", "filePath", "target_file", "notebook_path", "file"];

export function toolTarget(kind: ToolKind, argumentsJson?: string): { target?: string; detail?: string } {
  const args = parseObject(argumentsJson);
  if (!args) return {};
  switch (kind) {
    case "read":
    case "edit":
    case "write": {
      const path = firstString(args, PATH_KEYS);
      return path ? { target: basename(path), detail: path } : {};
    }
    case "search": {
      const pattern = firstString(args, ["pattern", "query", "q", "regex", "glob"]);
      const where = firstString(args, ["path", "dir", "directory", "root"]);
      if (pattern) return { target: `"${clip(pattern, 40)}"`, detail: where };
      return where ? { target: basename(where), detail: where } : {};
    }
    case "shell": {
      const command = firstString(args, ["command", "cmd"]);
      if (!command) return {};
      const line = command.trim().split("\n")[0];
      return { target: clip(line, 60), detail: command };
    }
    case "web": {
      const value = firstString(args, ["url", "query"]);
      return value ? { target: clip(value.replace(/^https?:\/\//, ""), 50), detail: value } : {};
    }
    case "browser": {
      const value = firstString(args, ["url", "selector", "action"]);
      return value ? { target: clip(value.replace(/^https?:\/\//, ""), 50), detail: value } : {};
    }
    case "board":
    case "git": {
      const value = firstString(args, ["title", "task_key", "key", "branch", "message"]);
      return value ? { target: clip(value, 40), detail: value } : {};
    }
    case "subagent": {
      const value = firstString(args, ["description"]);
      return value ? { target: clip(value, 40), detail: value } : {};
    }
    case "plan":
      return {};
    default: {
      for (const value of Object.values(args)) {
        if (typeof value === "string" && value.trim() && value.length <= 40) return { target: value.trim() };
      }
      return {};
    }
  }
}

function asRecord(payload: unknown): Record<string, unknown> {
  return payload && typeof payload === "object" && !Array.isArray(payload) ? (payload as Record<string, unknown>) : {};
}

function str(value: unknown): string | undefined {
  return typeof value === "string" && value !== "" ? value : undefined;
}

function argsString(value: unknown): string | undefined {
  if (typeof value === "string") return value || undefined;
  if (value && typeof value === "object") return JSON.stringify(value);
  return undefined;
}

const SUCCESS_EVENTS = new Set(["build_verification_passed"]);
const DANGER_EVENTS = new Set([
  "build_verification_failed",
  "verification_failed",
  "budget_exhausted",
  "token_budget_exhausted",
]);
const WARNING_EVENTS = new Set([
  "clarification_requested",
  "llm_provider_code_quota_park",
  "resource_blocked",
  "budget_warning",
  "token_budget_warning",
  "tool_call_skipped",
  "clarification_refused",
]);
const INFO_EVENTS = new Set([
  "goal_intake_complete",
  "planner_complete",
  "orchestration_plan_created",
  "replan_created",
  "criteria_sweep_start",
  "criteria_sweep_settled",
]);
const HIDDEN_STEPS = new Set([
  "iteration_start",
  "llm_request",
  "orchestration_complete",
  "empty_turn_retry",
  "history_trimmed",
  "history_summarized",
  "claude_code_slot_wait",
  "claude_code_result",
]);

const PAIR_OF: Record<string, string> = {
  build_verification_start: "build_verification",
  build_verification_passed: "build_verification",
  build_verification_failed: "build_verification",
  verification_start: "verification",
  verification_complete: "verification",
  verification_failed: "verification",
  planner_start: "planner",
  planner_complete: "planner",
  goal_intake_start: "goal_intake",
  goal_intake_complete: "goal_intake",
};

const PAIR_STARTS = new Set(["build_verification_start", "verification_start", "planner_start", "goal_intake_start"]);

// Steps that belong to "the agent working" and so follow a lone open subtask
// when they carry no key of their own.
const POSITIONAL_TYPES = new Set([
  "iteration_start",
  "llm_request",
  "tool_calls_planned",
  "tool_call_start",
  "tool_call_result",
  "assistant_message",
]);

function eventTone(type: string, payload: Record<string, unknown>): FeedTone {
  if (type === "verification_complete") return payload.passed === false ? "danger" : "success";
  if (SUCCESS_EVENTS.has(type)) return "success";
  if (DANGER_EVENTS.has(type)) return "danger";
  if (WARNING_EVENTS.has(type) || type.startsWith("loop_guard_")) return "warning";
  if (INFO_EVENTS.has(type)) return "info";
  return "muted";
}

function questionText(q: unknown): string | undefined {
  if (typeof q === "string") return q;
  const rec = asRecord(q);
  return str(rec.question) ?? str(rec.text) ?? str(rec.prompt);
}

function eventDetail(type: string, payload: Record<string, unknown>): string | undefined {
  switch (type) {
    case "build_verification_failed":
      return str(payload.report);
    case "goal_intake_complete":
      return str(payload.goal) ?? str(payload.purpose);
    case "orchestration_plan_created":
      return str(payload.summary);
    case "budget_exhausted":
    case "token_budget_exhausted":
      return str(payload.summary);
    case "verification_complete":
      return str(payload.summary);
    case "resource_blocked":
      return [str(payload.resource), str(payload.detail)].filter(Boolean).join(" — ") || undefined;
    case "clarification_requested": {
      const qs = Array.isArray(payload.questions) ? payload.questions.map(questionText).filter(Boolean) : [];
      return qs.length > 0 ? qs.join("\n") : undefined;
    }
    case "llm_provider_code_quota_park":
      return str(payload.resume_at);
    default:
      return str(payload.detail) ?? str(payload.reason) ?? str(payload.error);
  }
}

interface Scope {
  items: FeedItem[];
  lane?: FeedLane;
  tools: FeedTool[];
  pairs: Map<string, { item: Extract<FeedItem, { kind: "event" }>; at: string }>;
  lastStep: SessionStep | null;
  activeSeq: number;
  stepCount: number;
}

function newScope(items: FeedItem[], lane?: FeedLane): Scope {
  return { items, lane, tools: [], pairs: new Map(), lastStep: null, activeSeq: 0, stepCount: 0 };
}

function groupStatus(tools: FeedTool[]): FeedStatus {
  if (tools.some((t) => t.status === "running" || t.status === "pending")) return "running";
  if (tools.some((t) => t.status === "failed")) return "failed";
  return "completed";
}

interface StatsAcc {
  costUsd?: number;
  model?: string;
}

class RunBuilder {
  top: Scope;
  scopes: Scope[] = [];
  subLanes = new Map<string, Scope>();
  taskLanes = new Map<string, Scope>();
  toolById = new Map<string, FeedTool>();
  toolScope = new Map<string, Scope>();
  hasUserMessage = false;
  seq = 0;

  constructor(
    readonly input: FeedRunInput,
    items: FeedItem[],
    readonly allLanes: FeedLane[],
    readonly usedLaneIds: Set<string>,
    readonly acc: StatsAcc,
  ) {
    this.top = newScope(items);
    this.scopes.push(this.top);
  }

  private laneId(base: string): string {
    const id = this.usedLaneIds.has(base) ? `${base}@${this.input.runId}` : base;
    this.usedLaneIds.add(id);
    return id;
  }

  private createLane(
    base: string,
    source: FeedLane["source"],
    at: string,
    fields: Partial<FeedLane>,
  ): Scope {
    const lane: FeedLane = {
      id: this.laneId(base),
      source,
      title: "",
      status: "running",
      items: [],
      toolCount: 0,
      startedAt: at,
      current: null,
      ...fields,
    };
    this.allLanes.push(lane);
    const scope = newScope(lane.items, lane);
    this.scopes.push(scope);
    return scope;
  }

  private subScope(callId: string, at: string, into: Scope | null): Scope {
    const existing = this.subLanes.get(callId);
    if (existing) return existing;
    const scope = this.createLane(`sub:${callId}`, "subagent", at, {});
    this.subLanes.set(callId, scope);
    (into ?? this.top).items.push({ kind: "lane", id: `lane:${scope.lane!.id}`, lane: scope.lane! });
    return scope;
  }

  private taskScope(key: string, at: string): Scope {
    const existing = this.taskLanes.get(key);
    if (existing) return existing;
    const scope = this.createLane(`task:${key}`, "subtask", at, {});
    this.taskLanes.set(key, scope);
    this.top.items.push({ kind: "lane", id: `lane:${scope.lane!.id}`, lane: scope.lane! });
    return scope;
  }

  private route(step: SessionStep, payload: Record<string, unknown>): Scope {
    const parent = str(payload.parent_call_id);
    if (parent) return this.subScope(parent, step.created_at, null);
    const type = step.step_type;
    const key = str(payload.task_key);
    if (key && !type.startsWith("subtask_") && !type.endsWith("_session")) {
      const scope = this.taskLanes.get(key);
      if (scope) return scope;
    }
    if (POSITIONAL_TYPES.has(type)) {
      const open = [...this.taskLanes.values()].filter((s) => s.lane!.status === "running");
      if (open.length === 1) return open[0];
    }
    return this.top;
  }

  private addTool(scope: Scope, tool: FeedTool, step: SessionStep) {
    scope.tools.push(tool);
    if (scope.lane) scope.lane.toolCount += 1;
    const last = scope.items[scope.items.length - 1];
    if (last && last.kind === "tools" && last.toolKind === tool.kind) {
      last.tools.push(tool);
    } else {
      scope.items.push({
        kind: "tools",
        id: `tools:${step.id}`,
        toolKind: tool.kind,
        tools: [tool],
        status: "pending",
        at: step.created_at,
      });
    }
  }

  private makeTool(id: string, name: string, args: string | undefined, status: FeedStatus, at: string): FeedTool {
    const kind = classifyTool(name);
    const { target, detail } = toolTarget(kind, args);
    return {
      id,
      name,
      kind,
      target,
      targetDetail: detail,
      arguments: args,
      isError: false,
      status,
      startedAt: at,
    };
  }

  private startSubagent(step: SessionStep, callId: string, scope: Scope, args: string | undefined) {
    const parsed = parseObject(args) ?? {};
    const created = !this.subLanes.has(callId);
    const sub = this.subScope(callId, step.created_at, scope);
    const lane = sub.lane!;
    lane.title = str(parsed.description) ?? lane.title;
    lane.agentName = str(parsed.subagent_type) ?? lane.agentName;
    if (!created) lane.startedAt = step.created_at;
  }

  private say(scope: Scope, step: SessionStep, content: string | undefined) {
    if (!content || !content.trim()) return;
    scope.items.push({ kind: "say", id: `say:${step.id}`, content, at: step.created_at });
  }

  private pushEvent(scope: Scope, step: SessionStep, payload: Record<string, unknown>) {
    const type = step.step_type;
    const pair = PAIR_OF[type];
    const tone = eventTone(type, payload);
    const planId = type === "orchestration_plan_created" ? str(payload.plan_id) : undefined;
    const detail = eventDetail(type, payload);

    if (pair && !PAIR_STARTS.has(type)) {
      const open = scope.pairs.get(pair);
      if (open) {
        scope.pairs.delete(pair);
        Object.assign(open.item, {
          stepType: type,
          tone,
          status: tone === "danger" ? "failed" : "completed",
          detail,
          payload,
          planId,
        });
        return;
      }
    }

    const item: Extract<FeedItem, { kind: "event" }> = {
      kind: "event",
      id: `ev:${step.id}`,
      stepType: type,
      tone,
      status: PAIR_STARTS.has(type) ? "running" : tone === "danger" ? "failed" : "completed",
      detail,
      at: step.created_at,
      payload,
      planId,
    };
    scope.items.push(item);
    if (pair && PAIR_STARTS.has(type)) scope.pairs.set(pair, { item, at: step.created_at });
  }

  private finishLane(callId: string, step: SessionStep, payload: Record<string, unknown>) {
    const scope = this.subLanes.get(callId);
    if (!scope?.lane) return;
    const lane = scope.lane;
    const content = str(payload.content);
    if (payload.is_error === true) {
      lane.status = "failed";
      lane.error = content;
    } else {
      lane.status = "completed";
      lane.result = content;
    }
    lane.endedAt = step.created_at;
  }

  private finishTask(step: SessionStep, payload: Record<string, unknown>) {
    const key = str(payload.task_key);
    if (!key) return;
    const scope = this.taskScope(key, step.created_at);
    const lane = scope.lane!;
    lane.endedAt = step.created_at;
    if (step.step_type === "subtask_completed") {
      lane.status = "completed";
      lane.result = str(payload.result);
    } else if (step.step_type === "subtask_failed") {
      lane.status = "failed";
      lane.error = str(payload.error);
    } else {
      lane.status = "incomplete";
      lane.error = str(payload.reason);
    }
  }

  handle(step: SessionStep, seq: number) {
    const payload = asRecord(step.payload);
    const type = step.step_type;
    const at = step.created_at;

    if (type === "subtask_started") {
      const key = str(payload.task_key);
      if (key) {
        const scope = this.taskScope(key, at);
        const lane = scope.lane!;
        lane.title = str(payload.title) ?? lane.title;
        lane.agentName = str(payload.agent) ?? str(payload.agent_id) ?? lane.agentName;
        lane.model = str(payload.model) ?? lane.model;
        lane.status = "running";
        lane.startedAt = at;
        lane.endedAt = undefined;
        lane.error = undefined;
        lane.result = undefined;
        scope.activeSeq = seq;
        scope.lastStep = step;
        scope.stepCount += 1;
      }
      return;
    }
    if (type === "subtask_completed" || type === "subtask_failed" || type === "subtask_incomplete") {
      this.finishTask(step, payload);
      const key = str(payload.task_key);
      const scope = key ? this.taskLanes.get(key) : undefined;
      if (scope) {
        scope.activeSeq = seq;
        scope.lastStep = step;
      }
      return;
    }

    const scope = this.route(step, payload);
    scope.lastStep = step;
    scope.activeSeq = seq;
    scope.stepCount += 1;

    if (type.endsWith("_session")) {
      const model = str(payload.model);
      if (model) this.acc.model = model;
      return;
    }

    switch (type) {
      case "user_message": {
        this.hasUserMessage = true;
        this.top.items.push({
          kind: "user",
          id: `user:${step.id}`,
          content: str(payload.content) ?? "",
          at,
          runId: this.input.runId,
        });
        return;
      }
      case "assistant_message":
        this.say(scope, step, str(payload.content));
        return;
      case "tool_calls_planned": {
        this.say(scope, step, str(payload.content));
        const calls = Array.isArray(payload.tool_calls) ? payload.tool_calls : [];
        for (const raw of calls) {
          const call = asRecord(raw);
          const name = str(call.name);
          const id = str(call.id);
          if (!name || !id || classifyTool(name) === "subagent") continue;
          const tool = this.makeTool(id, name, argsString(call.arguments), "pending", at);
          this.toolById.set(id, tool);
          this.toolScope.set(id, scope);
          this.addTool(scope, tool, step);
        }
        return;
      }
      case "tool_call_start": {
        const name = str(payload.tool) ?? "tool";
        const callId = str(payload.call_id) ?? step.id;
        const args = argsString(payload.arguments);
        if (classifyTool(name) === "subagent") {
          this.startSubagent(step, callId, scope, args);
          return;
        }
        const existing = this.toolById.get(callId);
        if (existing) {
          existing.status = "running";
          existing.startedAt = at;
          existing.arguments = args ?? existing.arguments;
          const { target, detail } = toolTarget(existing.kind, existing.arguments);
          existing.target = target ?? existing.target;
          existing.targetDetail = detail ?? existing.targetDetail;
          return;
        }
        const tool = this.makeTool(callId, name, args, "running", at);
        this.toolById.set(callId, tool);
        this.toolScope.set(callId, scope);
        this.addTool(scope, tool, step);
        return;
      }
      case "tool_call_result": {
        const name = str(payload.tool) ?? "tool";
        const callId = str(payload.call_id) ?? step.id;
        if (this.subLanes.has(callId) || classifyTool(name) === "subagent") {
          this.finishLane(callId, step, payload);
          return;
        }
        let tool = this.toolById.get(callId);
        if (!tool) {
          tool = this.makeTool(callId, name, undefined, "running", at);
          this.toolById.set(callId, tool);
          this.toolScope.set(callId, scope);
          this.addTool(scope, tool, step);
        }
        const isError = payload.is_error === true;
        tool.isError = isError;
        tool.status = isError ? "failed" : "completed";
        tool.result = str(payload.content);
        tool.endedAt = at;
        const images = payload.image_attachment_ids;
        if (Array.isArray(images)) {
          const ids = images.filter((x): x is string => typeof x === "string");
          if (ids.length > 0) tool.imageIds = ids;
        }
        return;
      }
      case "tool_call_skipped": {
        const callId = str(payload.call_id);
        const tool = callId ? this.toolById.get(callId) : undefined;
        if (tool && (tool.status === "pending" || tool.status === "running")) {
          tool.status = "incomplete";
          tool.endedAt = at;
        }
        break;
      }
      case "claude_code_result": {
        const cost = Number(payload.cost_usd);
        if (Number.isFinite(cost) && cost > 0) this.acc.costUsd = (this.acc.costUsd ?? 0) + cost;
        const model = str(payload.model);
        if (model) this.acc.model = model;
        const subtype = str(payload.subtype);
        if (subtype && subtype !== "success" && subtype !== "error_max_turns") {
          scope.items.push({
            kind: "event",
            id: `ev:${step.id}`,
            stepType: type,
            tone: "danger",
            status: "failed",
            detail: subtype,
            at,
            payload,
          });
        }
        return;
      }
      default:
        break;
    }

    if (HIDDEN_STEPS.has(type)) return;
    this.pushEvent(scope, step, payload);
  }

  currentOf(scope: Scope, live: boolean): FeedCurrent | null {
    if (!live) return null;
    const since = scope.lastStep?.created_at ?? this.input.startedAt ?? "";
    const running = scope.tools.filter((t) => t.status === "running");
    const base = scope.lane ? { laneTitle: scope.lane.title, laneAgent: scope.lane.agentName } : {};
    if (running.length > 0) {
      const tool = running[running.length - 1];
      return { kind: "tool", tool, since: tool.startedAt, ...base, parallel: running.length > 1 ? running.length - 1 : undefined };
    }
    const open = [...scope.pairs.entries()].sort((a, b) => b[1].at.localeCompare(a[1].at))[0];
    if (open) {
      const kind = open[0] === "planner" || open[0] === "goal_intake" ? "planning" : "verifying";
      return { kind, since: open[1].at, ...base };
    }
    if (scope.lastStep?.step_type === "clarification_requested") {
      return { kind: "waitingUser", since, ...base };
    }
    if (scope.stepCount === 0 && !scope.lane && this.input.steps.length === 0) {
      return { kind: "starting", since, ...base };
    }
    if (scope.stepCount === 0 && scope.lane) return { kind: "starting", since: scope.lane.startedAt, ...base };
    return { kind: "thinking", since, ...base };
  }

  settle() {
    const live = this.input.live;
    for (const scope of this.scopes) {
      for (const item of scope.items) {
        if (item.kind === "tools") item.status = groupStatus(item.tools);
        if (item.kind === "event" && item.status === "running" && !live) item.status = "failed";
      }
      if (live) continue;
      for (const tool of scope.tools) {
        if (tool.status === "running" || tool.status === "pending") tool.status = "failed";
      }
      for (const item of scope.items) {
        if (item.kind === "tools") item.status = groupStatus(item.tools);
      }
      if (scope.lane && (scope.lane.status === "running" || scope.lane.status === "pending")) {
        scope.lane.status = "failed";
        scope.lane.error = scope.lane.error ?? INTERRUPTED_ERROR;
      }
    }
    for (const scope of this.scopes) {
      if (scope.lane) scope.lane.current = scope.lane.status === "running" ? this.currentOf(scope, live) : null;
    }
  }

  topCurrent(): FeedCurrent | null {
    if (!this.input.live) return null;
    const running = this.scopes.filter((s) => s.lane && s.lane.status === "running");
    if (running.length === 0) return this.currentOf(this.top, true);
    const chosen = running.reduce((a, b) => (b.activeSeq >= a.activeSeq ? b : a));
    const current = this.currentOf(chosen, true);
    if (!current) return null;
    const others = running.length - 1 + (current.parallel ?? 0);
    return { ...current, parallel: others > 0 ? others : undefined };
  }
}

export function buildActivityFeed(runs: FeedRunInput[]): ActivityFeed {
  const items: FeedItem[] = [];
  const lanes: FeedLane[] = [];
  const usedLaneIds = new Set<string>();
  const acc: StatsAcc = {};
  let plan: OrchestrationPlan | null = null;
  let toolCalls = 0;
  let failures = 0;
  let current: FeedCurrent | null = null;
  let seq = 0;

  runs.forEach((input, runIndex) => {
    if (input.plan) plan = input.plan;
    const builder = new RunBuilder(input, items, lanes, usedLaneIds, acc);
    const hasUser = input.steps.some((s) => s.step_type === "user_message");
    if (!hasUser) {
      items.push({
        kind: "run",
        id: `run:${input.runId}`,
        runId: input.runId,
        at: input.steps[0]?.created_at ?? input.startedAt ?? "",
      });
    }
    for (const step of input.steps) builder.handle(step, ++seq);
    builder.settle();
    for (const scope of builder.scopes) {
      for (const tool of scope.tools) {
        toolCalls += 1;
        if (tool.isError) failures += 1;
      }
    }
    if (runIndex === runs.length - 1) current = builder.topCurrent();
  });

  const subagents = lanes.filter((l) => l.source === "subagent").length;
  return {
    items,
    lanes,
    current,
    stats: { toolCalls, failures, subagents, costUsd: acc.costUsd, model: acc.model },
    plan,
  };
}
