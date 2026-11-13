import {
  api,
  type AgentResponse,
  type ClarificationQuestion,
  type ClarificationRequest,
  type MessageMention,
  type SessionAction,
  type SessionMessage,
  type SessionStep,
} from "@/api";
import { tStatic } from "@/hooks/useI18n";
import { findOpenClarification } from "@/lib/clarification";
import { isAbortError } from "@/lib/errors";

const POLL_MS = 2000;
const MAX_WAIT_MS = 30 * 60 * 1000;
// How long to keep polling after the send request itself failed. The run may
// have started server-side before the connection dropped, so a short window is
// worth it — but only a short one: past this the composer must be handed back
// to the user with a real error instead of spinning for the full MAX_WAIT_MS.
const RECOVERY_WAIT_MS = 20 * 1000;

function sleep(ms: number): Promise<void> {
  return new Promise((resolve) => window.setTimeout(resolve, ms));
}

export function countAssistantMessages(messages: SessionMessage[]): number {
  return messages.filter((m) => m.role === "assistant").length;
}

function mergeResponseMessage(
  messages: SessionMessage[],
  response: AgentResponse,
  baselineAssistantCount: number,
): SessionMessage[] {
  if (countAssistantMessages(messages) > baselineAssistantCount) {
    return messages;
  }
  const content = response.message?.content?.trim();
  if (!content) return messages;
  const assistantMessage: SessionMessage = {
    id: "response-" + Date.now(),
    role: "assistant",
    content,
    created_at: new Date().toISOString(),
    clarification: response.clarification,
  };
  return [...messages, assistantMessage];
}

function isClarificationQuestion(value: unknown): value is ClarificationQuestion {
  if (!value || typeof value !== "object") return false;
  const q = value as ClarificationQuestion;
  return typeof q.id === "string" && typeof q.prompt === "string" && Array.isArray(q.options);
}

export function clarificationFromSteps(steps: SessionStep[]): ClarificationRequest | null {
  for (let i = steps.length - 1; i >= 0; i--) {
    const step = steps[i];
    if (step.step_type !== "clarification_requested") continue;
    const payload = (step.payload ?? {}) as Record<string, unknown>;
    const questions = payload.questions;
    if (!Array.isArray(questions) || questions.length === 0) continue;
    if (!questions.every(isClarificationQuestion)) continue;
    return {
      context: typeof payload.context === "string" ? payload.context : undefined,
      questions,
    };
  }
  return null;
}

type PollHit = {
  messages: SessionMessage[];
  actions: SessionAction[];
  clarification?: ClarificationRequest;
};

async function tryPollOnce(sessionId: string, baselineAssistantCount: number): Promise<PollHit | null> {
  const [session, activity] = await Promise.all([
    api.getSession(sessionId),
    api.sessionActivity(sessionId),
  ]);
  const messages = session.messages ?? [];
  if (countAssistantMessages(messages) <= baselineAssistantCount) {
    return null;
  }

  let clarification = findOpenClarification(messages) ?? undefined;

  const runs = [...(activity.runs ?? [])].sort(
    (a, b) => new Date(b.started_at).getTime() - new Date(a.started_at).getTime(),
  );
  const latestRun = runs[0];
  if (!clarification && latestRun) {
    const { steps } = await api.runSteps(latestRun.id);
    const parsed = clarificationFromSteps(steps ?? []);
    if (parsed) clarification = parsed;
  }

  return { messages, actions: session.actions ?? [], clarification };
}

async function pollUntilReply(
  sessionId: string,
  baselineAssistantCount: number,
  deadline: number,
  signal?: AbortSignal,
): Promise<PollHit> {
  // The signal check keeps an abandoned send from polling a session the user
  // left for the rest of the deadline — the race it belongs to is long settled.
  while (Date.now() < deadline && !signal?.aborted) {
    await sleep(POLL_MS);
    try {
      const hit = await tryPollOnce(sessionId, baselineAssistantCount);
      if (hit) return hit;
    } catch {
      /* retry */
    }
  }
  throw new Error(tStatic("chatArea.chat.send.timeout"));
}

/**
 * The run itself failed and the backend said so on the stream (the frame
 * carries `error`, see sessionMessageStream in handler.go). Distinct from a
 * dropped connection: the server is finished, so there is no late reply to poll
 * for, and the tokens received so far are the error text — never the agent's
 * answer, so they must not be committed as one.
 */
export class AgentStreamError extends Error {
  readonly type: string;

  constructor(message: string, type = "") {
    super(message);
    this.name = "AgentStreamError";
    this.type = type;
  }
}

/**
 * What the caller can paint while the turn is still running: the stretch being
 * written now, and the stretches of reasoning the run has already closed behind
 * it. The open stretch is not yet the answer: a tool call after it makes it
 * reasoning, and only the end of the stream makes it the reply — so a caller
 * that shows it as the reply shows the model's thinking as an answer first.
 */
export interface StreamProgress {
  /** The open stretch so far. Empty between a closed reasoning stretch and the next token. */
  pending: string;
  /** Finished reasoning stretches, oldest first. */
  reasoning: string[];
}

/**
 * Drives the SSE transport and reports the reply as it grows. Resolves to the
 * same AgentResponse shape the non-streaming POST returns, so everything
 * downstream — the recovery race, mergeResponseMessage, the page's commit — is
 * shared between the two paths instead of forked.
 */
async function streamSessionMessage(
  sessionId: string,
  content: string,
  options: {
    fileIds?: string[];
    orchestrate?: boolean;
    mentions?: MessageMention[];
    attachmentIds?: string[];
    signal?: AbortSignal;
  },
  onProgress: (progress: StreamProgress) => void,
): Promise<AgentResponse> {
  // Two accumulators, not one. A run that calls tools streams the reasoning in
  // front of each call over the same token stream as the answer, and a single
  // buffer makes the two one message — which is why that reasoning was rendered
  // as the reply and then vanished when the real reply was committed.
  // `reasoning_end` closes a stretch; whatever is being written when the stream
  // stops is the answer, because the answering turn is by definition the one
  // with no tool call after it.
  let text = "";
  let reasoning: string[] = [];
  let finished = false;
  for await (const event of api.sendMessageStream(sessionId, content, options)) {
    if (event.kind === "token") {
      text += event.token;
      onProgress({ pending: text, reasoning });
    } else if (event.kind === "reasoning_end") {
      const closed = text.trim();
      if (closed) reasoning = [...reasoning, closed];
      text = "";
      onProgress({ pending: text, reasoning });
    } else if (event.kind === "finished") {
      finished = true;
    } else if (event.kind === "error") {
      throw new AgentStreamError(
        event.message || tStatic("chatArea.chat.send.failed"),
        event.type,
      );
    }
  }
  // Only the "stop" frame proves the reply is whole — a body that just stopped
  // arriving would otherwise be committed as a complete answer, silently cut
  // off. A plain Error here (not AgentStreamError) is what hands the turn to
  // the recovery poll, since the run may well have finished server-side.
  if (!finished) {
    throw new Error(tStatic("chatArea.chat.send.failed"));
  }
  return { message: { role: "assistant", content: text } };
}

export type SendSessionMessageResult =
  | {
      status: "response";
      response: AgentResponse;
      messages: SessionMessage[];
      actions: SessionAction[];
    }
  | {
      status: "recovered";
      messages: SessionMessage[];
      actions: SessionAction[];
      clarification?: ClarificationRequest;
    };

export async function sendSessionMessageWithRecovery(
  sessionId: string,
  content: string,
  options: {
    fileIds?: string[];
    orchestrate?: boolean;
    baselineAssistantCount: number;
    mentions?: MessageMention[];
    /** Binary attachment ids to link onto the persisted user message. */
    attachmentIds?: string[];
    /**
     * Present when the caller can render a partial reply. It switches the send
     * to the SSE endpoint and is called on every token with the reply so far and
     * the reasoning already closed behind it; leaving it out keeps the plain
     * one-shot POST.
     */
    onProgress?: (progress: StreamProgress) => void;
    /** Cancels the send — and, on the streaming path, the run behind it. */
    signal?: AbortSignal;
  },
): Promise<SendSessionMessageResult> {
  const { fileIds, orchestrate, baselineAssistantCount, mentions, attachmentIds, onProgress, signal } = options;
  const deadline = Date.now() + MAX_WAIT_MS;

  const send = onProgress
    ? streamSessionMessage(sessionId, content, { fileIds, orchestrate, mentions, attachmentIds, signal }, onProgress)
    : api.sendMessage(sessionId, content, fileIds, orchestrate, mentions, attachmentIds);
  const sendTask = send.then((response) => ({
    kind: "send" as const,
    response,
  }));

  const pollTask = pollUntilReply(sessionId, baselineAssistantCount, deadline, signal).then(
    (hit) => ({
      kind: "poll" as const,
      hit,
    }),
  );

  try {
    const winner = await Promise.race([sendTask, pollTask]);
    if (winner.kind === "send") {
      const data = await api.getSession(sessionId);
      const messages = mergeResponseMessage(
        data.messages ?? [],
        winner.response,
        baselineAssistantCount,
      );
      return {
        status: "response",
        response: winner.response,
        messages,
        actions: data.actions ?? [],
      };
    }
    return {
      status: "recovered",
      messages: winner.hit.messages,
      actions: winner.hit.actions,
      clarification: winner.hit.clarification,
    };
  } catch (sendErr) {
    // Neither of these is a lost connection, so the recovery window below would
    // only stall: a stream error means the server already finished (and wrote
    // its own "**Error:**" line into the transcript), and an abort means the
    // caller no longer wants the answer at all.
    if (sendErr instanceof AgentStreamError || isAbortError(sendErr)) {
      throw sendErr;
    }
    try {
      const recoveryDeadline = Math.min(deadline, Date.now() + RECOVERY_WAIT_MS);
      const hit = await pollUntilReply(sessionId, baselineAssistantCount, recoveryDeadline, signal);
      return {
        status: "recovered",
        messages: hit.messages,
        actions: hit.actions,
        clarification: hit.clarification,
      };
    } catch {
      throw sendErr instanceof Error ? sendErr : new Error(tStatic("chatArea.chat.send.failed"));
    }
  }
}
