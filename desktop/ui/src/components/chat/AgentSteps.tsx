import { AlertTriangle, Bot, CheckCircle2, ChevronDown, ChevronRight, Circle, Loader2, User, Wrench, XCircle } from "lucide-react";
import { useState } from "react";
import { AttachmentImage } from "@/components/attachments/AttachmentImage";
import { Badge } from "@/components/ui/badge";
import { useI18n } from "@/hooks/useI18n";
import { cn } from "@/lib/utils";
import type { GraphIteration, GraphLLMRequest, GraphNodeStatus, GraphToolCall } from "@/lib/sessionGraph";

// The pieces that render what an agent actually did inside one subtask: its
// iterations, the LLM request behind each, and every tool call with arguments
// and result. They live here, not inside SessionGraphView, because the plan
// view shows the same subtasks and needs the same detail — a card that only
// showed a title and a status left the run unexplainable.

export const graphPre =
  "max-h-40 max-w-full overflow-x-auto overflow-y-auto whitespace-pre-wrap break-words rounded bg-muted p-1.5 text-micro text-foreground [overflow-wrap:anywhere]";

export function StatusDot({ status }: { status: GraphNodeStatus }) {
  if (status === "running") return <Loader2 className="h-3.5 w-3.5 shrink-0 animate-spin text-warning" />;
  if (status === "completed") return <CheckCircle2 className="h-3.5 w-3.5 shrink-0 text-success" />;
  if (status === "failed") return <XCircle className="h-3.5 w-3.5 shrink-0 text-destructive" />;
  // Incomplete finished and the run went on — a warning, not a red cross and
  // not a spinner.
  if (status === "incomplete") return <AlertTriangle className="h-3.5 w-3.5 shrink-0 text-warning" />;
  return <Circle className="h-3.5 w-3.5 shrink-0 text-muted-foreground" />;
}

export function ExpandChevron({ expanded }: { expanded: boolean }) {
  return expanded ? (
    <ChevronDown className="h-3 w-3 shrink-0 text-muted-foreground" />
  ) : (
    <ChevronRight className="h-3 w-3 shrink-0 text-muted-foreground" />
  );
}

export function ContentPreview({ content }: { content: string }) {
  const { t } = useI18n();
  if (!content) return <p className="text-micro italic text-muted-foreground">{t("chatArea.chat.graph.emptyContent")}</p>;
  return <pre className={graphPre}>{content}</pre>;
}

export function ToolCallNode({ tool, compact }: { tool: GraphToolCall; compact?: boolean }) {
  const { t } = useI18n();
  const [expanded, setExpanded] = useState(false);

  return (
    <div
      className={cn(
        "min-w-0 max-w-full overflow-hidden rounded-md border border-border/70 bg-background/80",
        tool.status === "running" && "border-warning/40 bg-warning/5",
        tool.status === "failed" && "border-destructive/40 bg-destructive/5",
        tool.status === "completed" && "border-success/30 bg-success/5",
      )}
    >
      <button
        type="button"
        onClick={() => setExpanded((v) => !v)}
        className="flex w-full min-w-0 items-center gap-2 px-2 py-1.5 text-left"
      >
        <StatusDot status={tool.status} />
        <Wrench className="h-3 w-3 shrink-0 text-muted-foreground" />
        <span className="min-w-0 flex-1 truncate font-medium text-micro">{tool.name}</span>
        {!compact && (
          <Badge variant="secondary" className="shrink-0 text-micro">
            {tool.status}
          </Badge>
        )}
        <ExpandChevron expanded={expanded} />
      </button>
      {expanded && (
        <div className="min-w-0 max-w-full space-y-1 border-t border-border/60 px-2 py-1.5 text-micro text-muted-foreground">
          {tool.arguments && (
            <div className="min-w-0 max-w-full">
              <p className="mb-0.5 font-medium text-foreground">{t("chatArea.chat.graph.arguments")}</p>
              <pre className="max-h-24 max-w-full overflow-x-auto whitespace-pre-wrap break-words rounded bg-muted p-1.5 [overflow-wrap:anywhere]">
                {tool.arguments}
              </pre>
            </div>
          )}
          {tool.result && (
            <div className="min-w-0 max-w-full">
              <p className="mb-0.5 font-medium text-foreground">{t("chatArea.chat.graph.result")}</p>
              <pre
                className={cn(
                  "max-h-24 max-w-full overflow-x-auto whitespace-pre-wrap break-words rounded p-1.5 [overflow-wrap:anywhere]",
                  tool.isError ? "bg-destructive/10 text-destructive" : "bg-muted",
                )}
              >
                {tool.result}
              </pre>
            </div>
          )}
          {tool.imageIds && tool.imageIds.length > 0 && (
            <div className="min-w-0 max-w-full">
              {/* The screenshot the model was handed. Without it the reader had
                  the agent's word for what the screen looked like and nothing
                  to check it against. */}
              <p className="mb-0.5 font-medium text-foreground">{t("chatArea.chat.graph.screenshots")}</p>
              <div className="space-y-1">
                {tool.imageIds.map((id) => (
                  <AttachmentImage key={id} id={id} alt={tool.name} />
                ))}
              </div>
            </div>
          )}
        </div>
      )}
    </div>
  );
}

export function LLMRequestView({ req }: { req: GraphLLMRequest }) {
  const { t } = useI18n();
  const [expanded, setExpanded] = useState(false);
  return (
    <div className="min-w-0 max-w-full overflow-hidden rounded-md border border-border/60 bg-background/60">
      <button
        type="button"
        onClick={() => setExpanded((v) => !v)}
        className="flex w-full min-w-0 items-center gap-2 px-2 py-1.5 text-left"
      >
        <Bot className="h-3 w-3 shrink-0 text-muted-foreground" />
        <span className="min-w-0 flex-1 truncate text-micro font-medium">
          {t("chatArea.chat.graph.llmRequest", { model: req.model })}
        </span>
        <span className="shrink-0 text-micro text-muted-foreground">
          {t("chatArea.chat.graph.messagesToolsCount", { messages: req.messageCount, tools: req.toolCount })}
        </span>
        <ExpandChevron expanded={expanded} />
      </button>
      {expanded && (
        <div className="min-w-0 max-w-full space-y-1 border-t border-border/60 px-2 py-1.5">
          {req.messages.map((msg, i) => (
            <div key={i} className="min-w-0 max-w-full">
              <span className="text-micro font-medium text-muted-foreground">{msg.role}: </span>
              <pre className="inline whitespace-pre-wrap break-words text-micro text-foreground [overflow-wrap:anywhere]">
                {msg.content || <span className="italic text-muted-foreground">{t("chatArea.chat.graph.empty")}</span>}
              </pre>
              {/* The calls this turn made, not just the results that came back. */}
              {msg.tool_calls?.map((call, j) => (
                <div key={j} className="mt-0.5 flex min-w-0 max-w-full items-start gap-1 pl-3">
                  <Wrench className="mt-[3px] h-2.5 w-2.5 shrink-0 text-muted-foreground" />
                  <pre className="min-w-0 flex-1 whitespace-pre-wrap break-words text-micro text-muted-foreground [overflow-wrap:anywhere]">
                    <span className="font-medium text-foreground">{call.name}</span>
                    {call.arguments ? ` ${call.arguments}` : ""}
                  </pre>
                </div>
              ))}
            </div>
          ))}
        </div>
      )}
    </div>
  );
}

export function IterationBody({ entry, compact }: { entry: GraphIteration; compact?: boolean }) {
  const { t } = useI18n();
  return (
    <div className="min-w-0 max-w-full space-y-2">
      {entry.llmRequest && <LLMRequestView req={entry.llmRequest} />}
      {entry.messages.length === 0 && entry.toolCalls.length === 0 && (
        <p className="break-words text-micro text-muted-foreground [overflow-wrap:anywhere]">
          {entry.status === "running"
            ? t("chatArea.chat.graph.waitingForLlmContext", { count: entry.contextMessageCount })
            : t("chatArea.chat.graph.contextNoOutput", { count: entry.contextMessageCount })}
        </p>
      )}
      {entry.messages.length > 0 && (
        <div className="min-w-0 max-w-full space-y-1.5">
          <p className="text-micro font-medium uppercase tracking-wide text-muted-foreground">{t("chatArea.chat.graph.messages")}</p>
          {entry.messages.map((msg) => (
            <div key={msg.id} className="min-w-0 max-w-full rounded-md border border-border/60 bg-background/60 p-1.5">
              <div className="mb-1 flex items-center gap-1.5">
                {msg.role === "user" ? (
                  <User className="h-3 w-3 text-muted-foreground" />
                ) : (
                  <Bot className="h-3 w-3 text-muted-foreground" />
                )}
                <span className="text-micro font-medium">
                  {msg.role === "user" ? t("chatArea.chat.graph.user") : t("chatArea.chat.graph.assistant")}
                </span>
              </div>
              <ContentPreview content={msg.content} />
            </div>
          ))}
        </div>
      )}
      {entry.toolCalls.length > 0 && (
        <div className="min-w-0 max-w-full space-y-1.5 border-l-2 border-dashed border-border/80 pl-3">
          <p className="text-micro font-medium uppercase tracking-wide text-muted-foreground">{t("chatArea.chat.graph.toolCalls")}</p>
          {entry.toolCalls.map((tool) => (
            <ToolCallNode key={tool.id} tool={tool} compact={compact} />
          ))}
        </div>
      )}
    </div>
  );
}

export function IterationNode({ entry, compact, nested }: { entry: GraphIteration; compact?: boolean; nested?: boolean }) {
  const { t } = useI18n();
  const isRunning = entry.status === "running";
  const [expanded, setExpanded] = useState(isRunning);

  const summary =
    entry.messages.length > 0
      ? `${t("chatArea.chat.graph.repliesCount", { count: entry.messages.length })}${
          entry.toolCalls.length > 0 ? ` · ${t("chatArea.chat.graph.toolsCount", { count: entry.toolCalls.length })}` : ""
        }`
      : entry.toolCalls.length > 0
        ? t("chatArea.chat.graph.toolsCount", { count: entry.toolCalls.length })
        : isRunning
          ? t("chatArea.chat.graph.waitingForLlm")
          : t("chatArea.chat.graph.contextCount", { count: entry.contextMessageCount });

  const inner = (
    <>
      <button
        type="button"
        onClick={() => setExpanded((v) => !v)}
        className="flex w-full min-w-0 items-start gap-2 p-2 text-left"
      >
        <StatusDot status={entry.status} />
        <div className="min-w-0 flex-1">
          <p className="text-xs font-semibold">{t("chatArea.chat.graph.iteration", { number: entry.iteration })}</p>
          <p className="break-words text-micro text-muted-foreground [overflow-wrap:anywhere]">{summary}</p>
        </div>
        <ExpandChevron expanded={expanded} />
      </button>
      {expanded && (
        <div className="min-w-0 max-w-full border-t border-border/60 p-2">
          <IterationBody entry={entry} compact={compact} />
        </div>
      )}
    </>
  );

  if (nested) {
    return <div className="min-w-0 max-w-full overflow-hidden rounded-md border border-border/70 bg-background/80">{inner}</div>;
  }

  return (
    <div className="relative min-w-0 max-w-full pl-5">
      <span className="absolute left-0 top-2 h-2.5 w-2.5 -translate-x-1/2 rounded-full border-2 border-background bg-primary" />
      <div className="min-w-0 max-w-full overflow-hidden rounded-lg border border-border bg-card">{inner}</div>
    </div>
  );
}

// AgentStepList is the whole "what happened inside this subtask" block: one
// collapsible node per iteration, each holding its LLM request, replies and
// tool calls. Collapsed by default so a plan with many subtasks stays readable.
export function AgentStepList({
  iterations,
  compact,
  defaultExpanded = false,
}: {
  iterations: GraphIteration[];
  compact?: boolean;
  defaultExpanded?: boolean;
}) {
  const { t } = useI18n();
  const [expanded, setExpanded] = useState(defaultExpanded);

  if (iterations.length === 0) return null;

  const toolCount = iterations.reduce((total, it) => total + it.toolCalls.length, 0);

  return (
    <div className="mt-2 min-w-0 max-w-full">
      <button
        type="button"
        onClick={() => setExpanded((v) => !v)}
        className="flex w-full min-w-0 items-center gap-2 rounded-md border border-border/60 bg-background/60 px-2 py-1.5 text-left"
      >
        <Wrench className="h-3 w-3 shrink-0 text-muted-foreground" />
        <span className="min-w-0 flex-1 truncate text-micro font-medium uppercase tracking-wide text-muted-foreground">
          {t("chatArea.chat.graph.agentSteps")}
        </span>
        <span className="shrink-0 text-micro text-muted-foreground">
          {t("chatArea.chat.graph.iterationsCount", { count: iterations.length })}
          {toolCount > 0 ? ` · ${t("chatArea.chat.graph.toolsCount", { count: toolCount })}` : ""}
        </span>
        <ExpandChevron expanded={expanded} />
      </button>
      {expanded && (
        <div className="mt-1.5 min-w-0 max-w-full space-y-1.5">
          {iterations.map((iteration) => (
            <IterationNode key={iteration.id} entry={iteration} compact={compact} nested />
          ))}
        </div>
      )}
    </div>
  );
}
