import type { OrchestrationPlan, SessionStep } from "@/api";
import { stepLabel } from "@/lib/planUtils";
import { tStatic } from "@/hooks/useI18n";

// "incomplete" is a subtask that returned output without doing what it was for
// — it is finished and the run carried on, so it is neither still running nor a
// failure of the run. It exists as its own state because rendering it as either
// one lies: a spinner says an agent is working when it is not, and a red cross
// says the run broke when it is still going.
export type GraphNodeStatus = "pending" | "running" | "completed" | "failed" | "incomplete";

export interface GraphToolCall {
  id: string;
  name: string;
  arguments?: string;
  result?: string;
  isError?: boolean;
  /**
   * Attachment ids of the images this tool produced (browser/device
   * screenshots). Ids rather than the bytes: the steps endpoint is polled
   * every two seconds, and inlined base64 would be re-sent on every poll.
   */
  imageIds?: string[];
  status: GraphNodeStatus;
}

export interface GraphMessage {
  kind: "message";
  id: string;
  role: "user" | "assistant";
  content: string;
  createdAt: string;
  status: GraphNodeStatus;
}

export interface GraphLLMRequestToolCall {
  name: string;
  arguments: string;
}

export interface GraphLLMRequestMessage {
  role: string;
  content: string;
  // What the assistant turn asked the tools to do. Without it the request
  // trace showed only the results that came back, never the calls that
  // produced them.
  tool_calls?: GraphLLMRequestToolCall[];
}

export interface GraphLLMRequest {
  model: string;
  messageCount: number;
  toolCount: number;
  messages: GraphLLMRequestMessage[];
}

export interface GraphIteration {
  kind: "iteration";
  id: string;
  iteration: number;
  contextMessageCount: number;
  messages: Omit<GraphMessage, "kind">[];
  toolCalls: GraphToolCall[];
  llmRequest?: GraphLLMRequest;
  status: GraphNodeStatus;
}

export interface GraphEvent {
  kind: "event";
  id: string;
  stepType: string;
  label: string;
  detail?: string;
  payload: Record<string, unknown>;
  createdAt: string;
  status: GraphNodeStatus;
}

export interface GraphSubtask {
  kind: "subtask";
  id: string;
  taskKey: string;
  title?: string;
  agentName?: string;
  workingDir?: string;
  status: GraphNodeStatus;
  error?: string;
  result?: string;
  iterations: GraphIteration[];
}

export interface GraphPlanMarker {
  kind: "plan";
  id: string;
  summary: string;
  taskCount: number;
  status: GraphNodeStatus;
}

export type TimelineEntry = GraphMessage | GraphIteration | GraphEvent | GraphSubtask | GraphPlanMarker;

export type SessionGraphPhaseKind = "planning" | "execution" | "verification" | "messages";

export interface SessionGraphPhase {
  id: string;
  kind: SessionGraphPhaseKind;
  entries: TimelineEntry[];
}

const PLANNING_EVENT_TYPES = new Set([
  "goal_intake_start",
  "goal_intake_complete",
  "planner_start",
  "planner_complete",
]);

const VERIFICATION_EVENT_TYPES = new Set([
  "verification_start",
  "verification_complete",
  "verification_failed",
  "replan_created",
  "replan_iteration",
]);

// An event step that is neither planning nor verification chrome — the head
// and foot of a CLI session, the build gate, an unrecognized step type. It is
// still a real step (never dropped, see groupSessionGraphPhases), but showing
// it at full card width next to a subtask gave it the same visual weight as
// the work it is only bookkeeping for.
export function isLowSignalGraphEvent(entry: TimelineEntry): boolean {
  if (entry.kind !== "event") return false;
  return !PLANNING_EVENT_TYPES.has(entry.stepType) && !VERIFICATION_EVENT_TYPES.has(entry.stepType);
}

function classifyPhaseKind(entry: TimelineEntry): SessionGraphPhaseKind | null {
  if (entry.kind === "plan") return "planning";
  if (entry.kind === "message") return "messages";
  if (entry.kind === "event") {
    if (PLANNING_EVENT_TYPES.has(entry.stepType)) return "planning";
    if (VERIFICATION_EVENT_TYPES.has(entry.stepType)) return "verification";
  }
  return null;
}

// Groups the flat, chronological TimelineEntry stream into labelled phases —
// Planning / Execution (one per subtask) / Verification / Messages — so the
// panel can render section headers instead of one undifferentiated list. A
// subtask always opens its own execution phase, even next to another subtask;
// everything else merges into the previous phase when it classifies the same
// way, or attaches to whatever phase is open when it does not classify on its
// own (a tool-chrome event, a top-level iteration) — never dropped.
export function groupSessionGraphPhases(entries: TimelineEntry[]): SessionGraphPhase[] {
  const phases: SessionGraphPhase[] = [];
  let current: SessionGraphPhase | null = null;

  for (const entry of entries) {
    if (entry.kind === "subtask") {
      current = { id: `execution-${entry.id}`, kind: "execution", entries: [entry] };
      phases.push(current);
      continue;
    }

    const kind = classifyPhaseKind(entry);
    if (kind) {
      if (current && current.kind === kind) {
        current.entries.push(entry);
      } else {
        current = { id: `${kind}-${entry.id}`, kind, entries: [entry] };
        phases.push(current);
      }
      continue;
    }

    if (current) {
      current.entries.push(entry);
    } else {
      current = { id: `execution-${entry.id}`, kind: "execution", entries: [entry] };
      phases.push(current);
    }
  }

  return phases;
}

interface ToolCallPlanned {
  id: string;
  name: string;
  arguments: string;
}

interface ToolCallStartPayload {
  tool: string;
  call_id: string;
  arguments: string;
}

interface ToolCallResultPayload {
  tool: string;
  call_id: string;
  content: string;
  is_error: boolean;
  image_attachment_ids?: string[];
}

interface IterationStartPayload {
  iteration: number;
  message_count: number;
}

interface OrchestrationPlanCreatedPayload {
  plan_id: string;
  summary: string;
  task_count: number;
}

interface SubtaskStartedPayload {
  task_key: string;
  title: string;
  agent?: string;
  agent_id?: string;
  working_dir?: string;
}

interface SubtaskCompletedPayload {
  task_key: string;
  result?: string;
}

interface SubtaskFailedPayload {
  task_key: string;
  error: string;
}

interface UserMessagePayload {
  content: string;
}

interface AssistantMessagePayload {
  content: string;
}

function asRecord(payload: unknown): Record<string, unknown> {
  if (typeof payload !== "object" || payload === null || Array.isArray(payload)) return {};
  return payload as Record<string, unknown>;
}

// Plan durumlari (pending/running/completed/failed/incomplete) dogrudan dugum
// durumuna karsilik gelir. Eskiden running disindaki her sey "completed"
// sayiliyordu: verifier'in reddettigi bir plan, PlanView kirmizi FAILED karti
// gosterirken varsayilan olarak kapali duran sohbet dugumunde yesil tik olarak
// goruluyordu. Ayni tuzak "incomplete" icin de gecerliydi — listede olmayan her
// deger bu default'tan yesil tik olarak cikar, yani dogrulanmamis bir kosu
// "tamamlandi" diye gorunur. Yeni bir plan durumu eklendiginde buraya da
// eklenmesi sart.
function planNodeStatus(status: string): GraphNodeStatus {
  const normalized = status.toLowerCase();
  if (
    normalized === "running" ||
    normalized === "failed" ||
    normalized === "pending" ||
    normalized === "incomplete"
  ) {
    return normalized;
  }
  return "completed";
}

function parseToolCallsPlanned(payload: Record<string, unknown>): ToolCallPlanned[] {
  const raw = payload.tool_calls;
  if (!Array.isArray(raw)) return [];
  return raw.filter(
    (item): item is ToolCallPlanned =>
      typeof item === "object" &&
      item !== null &&
      typeof (item as ToolCallPlanned).id === "string" &&
      typeof (item as ToolCallPlanned).name === "string",
  );
}

function findOrCreateIteration(
  entries: TimelineEntry[],
  activeSubtask: GraphSubtask | null,
  stepId: string,
  payload: IterationStartPayload,
): GraphIteration {
  const pool = activeSubtask ? activeSubtask.iterations : entries;
  const existing = pool.find(
    (e): e is GraphIteration => e.kind === "iteration" && e.iteration === payload.iteration,
  );
  if (existing) return existing;
  const node: GraphIteration = {
    kind: "iteration",
    id: stepId,
    iteration: payload.iteration,
    contextMessageCount: payload.message_count,
    messages: [],
    toolCalls: [],
    status: "running",
  };
  if (activeSubtask) {
    activeSubtask.iterations.push(node);
  } else {
    entries.push(node);
  }
  return node;
}

// A trace that records no iteration_start still has rounds — it just does not
// say so. This makes one up so the steps inside it have somewhere to hang.
//
// It exists because the whole timeline used to hinge on a step type only the
// in-process agent loop emits. A Claude Code run records the same
// assistant_message / tool_call_start / tool_call_result steps, and every one of
// its tool calls was DROPPED here for want of an enclosing round: the panel
// showed the narration and nothing else, which is exactly the "I can't see what
// it did" this function answers. Runs recorded before the executor started
// bracketing its turns still have no iteration_start at all, and they render
// correctly through this path.
function openImplicitIteration(
  entries: TimelineEntry[],
  activeSubtask: GraphSubtask | null,
  stepId: string,
): GraphIteration {
  const node: GraphIteration = {
    kind: "iteration",
    id: `${stepId}-turn`,
    iteration: collectIterations(entries).length + 1,
    contextMessageCount: 0,
    messages: [],
    toolCalls: [],
    status: "running",
  };
  if (activeSubtask) {
    activeSubtask.iterations.push(node);
  } else {
    entries.push(node);
  }
  return node;
}

function ensureToolCall(iteration: GraphIteration, callId: string, name: string): GraphToolCall {
  const existing = iteration.toolCalls.find((tc) => tc.id === callId);
  if (existing) return existing;
  const node: GraphToolCall = { id: callId, name, status: "pending" };
  iteration.toolCalls.push(node);
  return node;
}

function currentIteration(entries: TimelineEntry[], activeSubtask: GraphSubtask | null): GraphIteration | undefined {
  if (activeSubtask && activeSubtask.iterations.length > 0) {
    return activeSubtask.iterations[activeSubtask.iterations.length - 1];
  }
  for (let i = entries.length - 1; i >= 0; i--) {
    const entry = entries[i];
    if (entry.kind === "iteration") return entry;
  }
  return undefined;
}

function collectIterations(entries: TimelineEntry[]): GraphIteration[] {
  const iterations: GraphIteration[] = [];
  for (const entry of entries) {
    if (entry.kind === "iteration") iterations.push(entry);
    if (entry.kind === "subtask") iterations.push(...entry.iterations);
  }
  return iterations;
}

function finalizeIterationStatuses(entries: TimelineEntry[], isLive: boolean) {
  const iterations = collectIterations(entries);
  iterations.forEach((iteration, index) => {
    const isLast = index === iterations.length - 1;
    if (iteration.toolCalls.length === 0 && iteration.messages.length === 0) {
      iteration.status = isLast && isLive ? "running" : "completed";
      return;
    }
    const hasFailed = iteration.toolCalls.some((tc) => tc.status === "failed");
    const hasRunning = iteration.toolCalls.some((tc) => tc.status === "running" || tc.status === "pending");
    if (hasFailed) iteration.status = "failed";
    else if (hasRunning) {
      // A tool call with no result in a run that is over did not complete — the
      // process died mid-call. Showing it as completed hid exactly that.
      iteration.status = isLast && isLive ? "running" : "failed";
    } else iteration.status = "completed";
  });

  // Same for a subtask whose start was recorded but whose completion never was.
  if (!isLive) {
    for (const entry of entries) {
      if (entry.kind === "subtask" && entry.status === "running") {
        entry.status = "failed";
        entry.error ??= tStatic("chatArea.chat.activityPanel.subtaskInterrupted");
      }
      if (entry.kind === "plan" && entry.status === "running") entry.status = "failed";
      if (entry.kind === "event" && entry.status === "running") entry.status = "completed";
    }
  }
}

function resolveTaskAgent(plan: OrchestrationPlan | null, taskKey: string, fallback?: string): string | undefined {
  const task = plan?.tasks?.find((t) => t.task_key === taskKey);
  if (task?.agent_id && plan) {
    return fallback ?? task.agent_id;
  }
  return fallback;
}

function findActiveSubtaskPayload(steps: SessionStep[]): SubtaskStartedPayload | null {
  let active: SubtaskStartedPayload | null = null;
  for (const step of steps) {
    if (step.step_type === "subtask_started") {
      active = asRecord(step.payload) as unknown as SubtaskStartedPayload;
    }
    if (
      step.step_type === "subtask_completed" ||
      step.step_type === "subtask_failed" ||
      step.step_type === "subtask_incomplete"
    ) {
      active = null;
    }
  }
  return active;
}

export function buildSessionGraph(steps: SessionStep[], plan: OrchestrationPlan | null, isLive: boolean): TimelineEntry[] {
  const entries: TimelineEntry[] = [];
  const subtasks = new Map<string, GraphSubtask>();
  let activeSubtask: GraphSubtask | null = null;
  // Rounds this builder invented, for a trace that declares none. Membership is
  // what tells an invented round from one the producer declared — a closed
  // invented round must not swallow the next turn's tool calls the way a
  // declared one legitimately does.
  const invented = new WeakSet<GraphIteration>();
  // The invented round still taking steps, or null when the last one was closed
  // by narration — which is where one turn ends and the next begins.
  let openInvented: GraphIteration | null = null;

  const declaredIteration = (): GraphIteration | undefined => {
    const current = currentIteration(entries, activeSubtask);
    return current && !invented.has(current) ? current : undefined;
  };

  // The round a tool call belongs to: the declared one when there is one, else
  // an invented one. Never undefined, because a dropped tool call is the bug.
  const iterationForToolCall = (stepId: string): GraphIteration => {
    const declared = declaredIteration();
    if (declared) return declared;
    if (openInvented) return openInvented;
    openInvented = openImplicitIteration(entries, activeSubtask, stepId);
    invented.add(openInvented);
    return openInvented;
  };

  for (const step of steps) {
    const payload = asRecord(step.payload);

    switch (step.step_type) {
      case "user_message": {
        const data = payload as unknown as UserMessagePayload;
        entries.push({
          kind: "message",
          id: step.id,
          role: "user",
          content: data.content ?? "",
          createdAt: step.created_at,
          status: "completed",
        });
        break;
      }
      case "assistant_message": {
        const data = payload as unknown as AssistantMessagePayload;
        const message = {
          id: step.id,
          role: "assistant" as const,
          content: data.content ?? "",
          createdAt: step.created_at,
          status: "completed" as const,
        };
        const declared = declaredIteration();
        if (declared) {
          declared.messages.push(message);
        } else {
          // Narration that follows an invented round's tool calls is the NEXT
          // turn starting, so the round is closed rather than grown: folding it
          // in would move it behind tool calls that came before it, and the
          // whole point of this timeline is that it is chronological.
          openInvented = null;
          entries.push({ kind: "message", ...message });
        }
        break;
      }
      case "iteration_start": {
        const data = payload as unknown as IterationStartPayload;
        openInvented = null;
        findOrCreateIteration(entries, activeSubtask, step.id, {
          iteration: data.iteration,
          // Absent on a producer that has no history to count (a CLI session
          // brackets its turns but holds its own context). Left as 0 rather than
          // undefined, which rendered as "undefined messages of context".
          message_count: typeof data.message_count === "number" ? data.message_count : 0,
        });
        break;
      }
      case "llm_request": {
        const iteration = currentIteration(entries, activeSubtask);
        if (!iteration) break;
        iteration.llmRequest = {
          model: typeof payload.model === "string" ? payload.model : "",
          messageCount: typeof payload.message_count === "number" ? payload.message_count : 0,
          toolCount: typeof payload.tool_count === "number" ? payload.tool_count : 0,
          messages: Array.isArray(payload.messages)
            ? (payload.messages as GraphLLMRequestMessage[])
            : [],
        };
        break;
      }
      case "tool_calls_planned": {
        const calls = parseToolCallsPlanned(payload);
        const iteration = currentIteration(entries, activeSubtask);
        if (!iteration) break;
        // What the model said before it called the tools. A turn that ends in
        // tool calls records no assistant_message, so this is the only place
        // the reasoning behind a call is available to show.
        const said = typeof payload.content === "string" ? payload.content.trim() : "";
        if (said) {
          iteration.messages.push({
            id: `${step.id}-content`,
            role: "assistant",
            content: said,
            createdAt: step.created_at,
            status: "completed",
          });
        }
        for (const call of calls) {
          const tc = ensureToolCall(iteration, call.id, call.name);
          tc.arguments = call.arguments;
        }
        break;
      }
      case "tool_call_start": {
        const data = payload as unknown as ToolCallStartPayload;
        const iteration = iterationForToolCall(step.id);
        const tc = ensureToolCall(iteration, data.call_id, data.tool);
        tc.arguments = data.arguments;
        tc.status = "running";
        break;
      }
      case "tool_call_result": {
        const data = payload as unknown as ToolCallResultPayload;
        const iteration = iterationForToolCall(step.id);
        const tc = ensureToolCall(iteration, data.call_id, data.tool);
        tc.result = data.content;
        tc.isError = data.is_error;
        // Absent on every step written before screenshots were archived, and on
        // every tool that produces none — so an empty list stays undefined
        // rather than rendering an empty gallery.
        tc.imageIds = data.image_attachment_ids?.length ? data.image_attachment_ids : undefined;
        tc.status = data.is_error ? "failed" : "completed";
        break;
      }
      case "planner_start": {
        const model = typeof payload.model === "string" ? payload.model : undefined;
        entries.push({
          kind: "event",
          id: step.id,
          stepType: step.step_type,
          label: stepLabel(step.step_type),
          detail: model ? `Model: ${model}` : undefined,
          payload,
          createdAt: step.created_at,
          status: "completed",
        });
        break;
      }
      case "planner_complete": {
        const taskCount = typeof payload.task_count === "number" ? payload.task_count : undefined;
        entries.push({
          kind: "event",
          id: step.id,
          stepType: step.step_type,
          label: stepLabel(step.step_type),
          detail: taskCount !== undefined ? `${taskCount} tasks planned` : undefined,
          payload,
          createdAt: step.created_at,
          status: "completed",
        });
        break;
      }
      case "goal_intake_start":
      case "replan_iteration": {
        entries.push({
          kind: "event",
          id: step.id,
          stepType: step.step_type,
          label: stepLabel(step.step_type),
          payload,
          createdAt: step.created_at,
          status: isLive ? "running" : "completed",
        });
        break;
      }
      case "goal_intake_complete": {
        const goal = typeof payload.goal === "string" ? payload.goal : undefined;
        const purpose = typeof payload.purpose === "string" ? payload.purpose : undefined;
        entries.push({
          kind: "event",
          id: step.id,
          stepType: step.step_type,
          label: stepLabel(step.step_type),
          detail: goal ?? purpose,
          payload,
          createdAt: step.created_at,
          status: "completed",
        });
        break;
      }
      case "verification_start": {
        entries.push({
          kind: "event",
          id: step.id,
          stepType: step.step_type,
          label: stepLabel(step.step_type),
          payload,
          createdAt: step.created_at,
          status: isLive ? "running" : "completed",
        });
        break;
      }
      case "verification_complete": {
        const passed = payload.passed === true;
        const summary = typeof payload.summary === "string" ? payload.summary : undefined;
        entries.push({
          kind: "event",
          id: step.id,
          stepType: step.step_type,
          label: stepLabel(step.step_type),
          detail: summary,
          payload,
          createdAt: step.created_at,
          status: passed ? "completed" : "failed",
        });
        break;
      }
      case "verification_failed": {
        const summary = typeof payload.summary === "string" ? payload.summary : undefined;
        entries.push({
          kind: "event",
          id: step.id,
          stepType: step.step_type,
          label: stepLabel(step.step_type),
          detail: summary,
          payload,
          createdAt: step.created_at,
          status: "failed",
        });
        break;
      }
      case "replan_created": {
        const iteration = typeof payload.iteration === "number" ? payload.iteration : undefined;
        entries.push({
          kind: "event",
          id: step.id,
          stepType: step.step_type,
          label: stepLabel(step.step_type),
          detail: iteration !== undefined ? `Iteration ${iteration}` : undefined,
          payload,
          createdAt: step.created_at,
          status: "completed",
        });
        break;
      }
      case "orchestration_plan_created": {
        const data = payload as unknown as OrchestrationPlanCreatedPayload;
        entries.push({
          kind: "plan",
          id: step.id,
          summary: data.summary,
          taskCount: data.task_count,
          status: plan ? planNodeStatus(plan.status) : isLive ? "running" : "completed",
        });
        break;
      }
      case "subtask_started": {
        const data = payload as unknown as SubtaskStartedPayload;
        const node: GraphSubtask = {
          kind: "subtask",
          id: step.id,
          taskKey: data.task_key,
          title: data.title,
          agentName: data.agent ?? resolveTaskAgent(plan, data.task_key, data.agent_id),
          workingDir: data.working_dir,
          status: "running",
          iterations: [],
        };
        subtasks.set(data.task_key, node);
        activeSubtask = node;
        entries.push(node);
        break;
      }
      case "subtask_completed": {
        const data = payload as unknown as SubtaskCompletedPayload;
        const node = subtasks.get(data.task_key);
        if (node) {
          node.status = "completed";
          node.result = data.result;
          if (activeSubtask?.taskKey === data.task_key) activeSubtask = null;
        }
        break;
      }
      case "subtask_failed": {
        const data = payload as unknown as SubtaskFailedPayload;
        const node = subtasks.get(data.task_key);
        if (node) {
          node.status = "failed";
          node.error = data.error;
          if (activeSubtask?.taskKey === data.task_key) activeSubtask = null;
        }
        break;
      }
      // A subtask that produced output but did not finish what it was for (the
      // classic case: it claimed the task, moved it, and stopped). The run does
      // NOT stop — the result is kept and the plan carries on — so this is a
      // terminal state for the node, not a failure of the run. Without this case
      // the step fell through to `default` and the node was left "running"
      // forever: a spinner while the agent was already on the next subtask, then
      // rewritten to "interrupted" the moment the run stopped being live.
      case "subtask_incomplete": {
        const data = payload as unknown as SubtaskFailedPayload & { reason?: string };
        const node = subtasks.get(data.task_key);
        if (node) {
          node.status = "incomplete";
          node.error = data.reason ?? data.error;
          if (activeSubtask?.taskKey === data.task_key) activeSubtask = null;
        }
        break;
      }
      // The head and foot of a Claude Code run. They fell through to `default`,
      // which shows a label and nothing else — so the two steps that say WHICH
      // session this was, on which model, for how many turns and at what cost
      // read as two anonymous chips. Both are terminal facts, never "running".
      case "claude_code_session": {
        const model = typeof payload.model === "string" ? payload.model : "";
        const sessionId = typeof payload.cli_session_id === "string" ? payload.cli_session_id : "";
        entries.push({
          kind: "event",
          id: step.id,
          stepType: step.step_type,
          label: stepLabel(step.step_type),
          detail: [model, sessionId].filter(Boolean).join(" · ") || undefined,
          payload,
          createdAt: step.created_at,
          status: "completed",
        });
        break;
      }
      case "claude_code_result": {
        const num = (key: string): number | undefined =>
          typeof payload[key] === "number" ? (payload[key] as number) : undefined;
        const turns = num("turns") ?? num("num_turns");
        const toolCalls = num("tool_calls");
        const failures = num("tool_failures");
        const cost = num("cost_usd");
        const parts: string[] = [];
        if (turns !== undefined) parts.push(tStatic("chatArea.chat.graph.cliTurns", { count: turns }));
        if (toolCalls !== undefined) parts.push(tStatic("chatArea.chat.graph.toolsCount", { count: toolCalls }));
        if (failures) parts.push(tStatic("chatArea.chat.graph.cliFailures", { count: failures }));
        if (cost) parts.push(`$${cost.toFixed(4)}`);
        entries.push({
          kind: "event",
          id: step.id,
          stepType: step.step_type,
          label: stepLabel(step.step_type),
          detail: parts.join(" · ") || undefined,
          payload,
          createdAt: step.created_at,
          // A session that stopped at its turn budget still did the work; only a
          // hard error is a red foot to the trace.
          status: payload.subtype === "success" || payload.subtype === "error_max_turns" ? "completed" : "failed",
        });
        break;
      }
      // The subscription behind the CLI is spent. The run stops with nothing
      // wrong with it, so this is a warning, not a failure of the work.
      case "claude_code_quota_park": {
        const resumeAt = typeof payload.resume_at === "string" ? payload.resume_at : undefined;
        entries.push({
          kind: "event",
          id: step.id,
          stepType: step.step_type,
          label: stepLabel(step.step_type),
          detail: resumeAt ? tStatic("chatArea.chat.graph.cliResumesAt", { at: resumeAt }) : undefined,
          payload,
          createdAt: step.created_at,
          status: "incomplete",
        });
        break;
      }
      // The build gate the runner puts every board run through after the agent
      // stops talking. It is the one phase that can take minutes with nothing
      // else to show, so it gets its own rows instead of falling through to
      // `default` — where a finished check would have kept a spinner.
      case "build_verification_start":
      case "build_verification_passed":
      case "build_verification_failed": {
        entries.push({
          kind: "event",
          id: step.id,
          stepType: step.step_type,
          label: stepLabel(step.step_type),
          detail: typeof payload.report === "string" ? payload.report : undefined,
          payload,
          createdAt: step.created_at,
          status:
            step.step_type === "build_verification_failed"
              ? "failed"
              : step.step_type === "build_verification_passed"
                ? "completed"
                : isLive
                  ? "running"
                  : "completed",
        });
        break;
      }
      default: {
        entries.push({
          kind: "event",
          id: step.id,
          stepType: step.step_type,
          label: stepLabel(step.step_type),
          payload,
          createdAt: step.created_at,
          status: step.step_type === "orchestration_complete" ? "completed" : isLive ? "running" : "completed",
        });
      }
    }
  }

  finalizeIterationStatuses(entries, isLive);
  return entries;
}

// subtaskActivityByKey indexes each subtask's iterations by its plan task key,
// so the plan view can show what an agent actually did inside a subtask — the
// LLM turns, and every tool call with its arguments and result. The plan alone
// carries only a title, a status and a final result string, which is not enough
// to tell a subtask that worked from one that spun.
export function subtaskActivityByKey(
  steps: SessionStep[],
  plan: OrchestrationPlan | null,
  isLive: boolean,
): Record<string, GraphIteration[]> {
  const byKey: Record<string, GraphIteration[]> = {};
  for (const entry of buildSessionGraph(steps, plan, isLive)) {
    if (entry.kind !== "subtask" || entry.iterations.length === 0) continue;
    // A retried subtask starts again under the same key; keep every attempt.
    byKey[entry.taskKey] = [...(byKey[entry.taskKey] ?? []), ...entry.iterations];
  }
  return byKey;
}

export function getLiveStepSummary(steps: SessionStep[], isLive: boolean): string | null {
  if (steps.length === 0) return isLive ? "Waiting for response…" : null;
  const last = steps[steps.length - 1];
  const activeSubtask = findActiveSubtaskPayload(steps);

  if (last.step_type === "tool_call_start") {
    const payload = asRecord(last.payload) as unknown as ToolCallStartPayload;
    const prefix = activeSubtask
      ? `${activeSubtask.title || activeSubtask.task_key}${activeSubtask.agent ? ` · ${activeSubtask.agent}` : ""}: `
      : "";
    return `${prefix}${payload.tool} running…`;
  }
  if (last.step_type === "tool_call_result") {
    const payload = asRecord(last.payload) as unknown as ToolCallResultPayload;
    const prefix = activeSubtask
      ? `${activeSubtask.title || activeSubtask.task_key}${activeSubtask.agent ? ` · ${activeSubtask.agent}` : ""}: `
      : "";
    return `${prefix}${payload.tool} completed`;
  }
  if (last.step_type === "iteration_start") {
    const payload = asRecord(last.payload) as unknown as IterationStartPayload;
    if (activeSubtask) {
      const agent = activeSubtask.agent ? ` · ${activeSubtask.agent}` : "";
      return `${activeSubtask.title || activeSubtask.task_key}${agent} — Iteration ${payload.iteration}, waiting for LLM response…`;
    }
    return `Iteration ${payload.iteration} — waiting for LLM response…`;
  }
  if (last.step_type === "subtask_started") {
    const payload = asRecord(last.payload) as unknown as SubtaskStartedPayload;
    const agent = payload.agent ? ` · ${payload.agent}` : "";
    return `${payload.title || payload.task_key}${agent} running…`;
  }
  if (last.step_type === "planner_start") {
    return "Planner running…";
  }
  if (last.step_type === "planner_complete") {
    return "Executing tasks…";
  }
  if (last.step_type === "goal_intake_start") {
    return "Capturing goal…";
  }
  if (last.step_type === "goal_intake_complete") {
    return "Goal set — planning…";
  }
  if (last.step_type === "verification_start") {
    return "Verifying result…";
  }
  if (last.step_type === "verification_complete") {
    return "Verification complete";
  }
  if (last.step_type === "verification_failed") {
    return "Verification failed";
  }
  if (last.step_type === "replan_created" || last.step_type === "replan_iteration") {
    return "Replanning…";
  }
  if (last.step_type === "clarification_requested") {
    return "Waiting for your clarification…";
  }
  if (last.step_type === "claude_code_session") {
    return "Claude Code session started…";
  }
  if (last.step_type === "claude_code_result") {
    return "Claude Code session finished";
  }
  if (last.step_type === "claude_code_quota_park") {
    return "Claude Code usage limit reached — task parked";
  }
  return stepLabel(last.step_type);
}
