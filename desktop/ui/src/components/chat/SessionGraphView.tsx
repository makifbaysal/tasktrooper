import { Bot, GitBranch, User } from "lucide-react";
import { useRef, useState } from "react";
import type { OrchestrationPlan, SessionStep } from "@/api";
import { ContentPreview, ExpandChevron, IterationNode, StatusDot } from "@/components/chat/AgentSteps";
import { PlanView } from "@/components/chat/PlanView";
import { useI18n } from "@/hooks/useI18n";
import { cn } from "@/lib/utils";
import {
  buildSessionGraph,
  groupSessionGraphPhases,
  isLowSignalGraphEvent,
  type GraphIteration,
  type GraphMessage,
  type SessionGraphPhase,
  type SessionGraphPhaseKind,
  type TimelineEntry,
} from "@/lib/sessionGraph";

interface SessionGraphViewProps {
  steps: SessionStep[];
  plan: OrchestrationPlan | null;
  isLive: boolean;
  compact?: boolean;
}

const graphRow = "relative min-w-0 max-w-full pl-5";
const graphCard = "min-w-0 max-w-full overflow-hidden rounded-lg border border-border bg-card";
function MessageNode({ message, compact }: { message: GraphMessage; compact?: boolean }) {
  const { t } = useI18n();
  const [expanded, setExpanded] = useState(!compact);
  const Icon = message.role === "user" ? User : Bot;
  const label = message.role === "user" ? t("chatArea.chat.graph.userMessage") : t("chatArea.chat.graph.assistantReply");
  const preview = message.content.length > 80 ? `${message.content.slice(0, 80)}…` : message.content;

  return (
    <div className={graphRow}>
      <span
        className={cn(
          "absolute left-0 top-2 h-2.5 w-2.5 -translate-x-1/2 rounded-full border-2 border-background",
          message.role === "user" ? "bg-primary" : "bg-success",
        )}
      />
      <div className={graphCard}>
        <button
          type="button"
          onClick={() => setExpanded((v) => !v)}
          className="flex w-full min-w-0 items-start gap-2 p-2 text-left"
        >
          <StatusDot status={message.status} />
          <Icon className="mt-0.5 h-3 w-3 shrink-0 text-muted-foreground" />
          <div className="min-w-0 flex-1">
            <p className="text-xs font-semibold">{label}</p>
            {!expanded && preview && (
              <p className="mt-0.5 line-clamp-2 text-micro text-muted-foreground">{preview}</p>
            )}
          </div>
          <ExpandChevron expanded={expanded} />
        </button>
        {expanded && (
          <div className="min-w-0 max-w-full border-t border-border/60 px-2 py-1.5">
            <ContentPreview content={message.content} />
          </div>
        )}
      </div>
    </div>
  );
}

function EventNode({ entry }: { entry: Extract<TimelineEntry, { kind: "event" }> }) {
  const [expanded, setExpanded] = useState(false);
  const hasPayload = Object.keys(entry.payload).length > 0;

  return (
    <div className={graphRow}>
      <span className="absolute left-0 top-2 h-2.5 w-2.5 -translate-x-1/2 rounded-full border-2 border-background bg-muted-foreground/50" />
      <div className="min-w-0 max-w-full overflow-hidden rounded-lg border border-border/60 bg-card/80">
        <button
          type="button"
          onClick={() => hasPayload && setExpanded((v) => !v)}
          className="flex w-full min-w-0 items-center gap-2 px-2 py-1.5 text-left"
        >
          <StatusDot status={entry.status} />
          <Bot className="h-3 w-3 shrink-0 text-muted-foreground" />
          <div className="min-w-0 flex-1">
            <span className="text-xs">{entry.label}</span>
            {entry.detail && <p className="break-words text-micro text-muted-foreground">{entry.detail}</p>}
          </div>
          {hasPayload && <ExpandChevron expanded={expanded} />}
        </button>
        {expanded && hasPayload && (
          <div className="min-w-0 max-w-full border-t border-border/60 px-2 py-1.5">
            <pre className="max-h-32 max-w-full overflow-x-auto whitespace-pre-wrap break-words rounded bg-muted p-1.5 text-micro [overflow-wrap:anywhere]">
              {JSON.stringify(entry.payload, null, 2)}
            </pre>
          </div>
        )}
      </div>
    </div>
  );
}

function SubtaskNode({ entry, compact }: { entry: Extract<TimelineEntry, { kind: "subtask" }>; compact?: boolean }) {
  const { t } = useI18n();
  const hasIterations = entry.iterations.length > 0;
  const hasOutcome = Boolean(entry.result || entry.error);
  const [expanded, setExpanded] = useState(entry.status === "running");

  return (
    <div className={graphRow}>
      <span className="absolute left-0 top-2 h-2.5 w-2.5 -translate-x-1/2 rounded-full border-2 border-background bg-accent" />
      <div
        className={cn(
          graphCard,
          entry.status === "running" && "border-warning/30 bg-warning/5",
          entry.status === "failed" && "border-destructive/30 bg-destructive/5",
          entry.status === "incomplete" && "border-warning/30 bg-warning/5",
          entry.status === "completed" && "border-success/30 bg-success/5",
        )}
      >
        <button
          type="button"
          onClick={() => setExpanded((v) => !v)}
          className="flex w-full min-w-0 items-start gap-2 p-2 text-left"
        >
          <StatusDot status={entry.status} />
          <GitBranch className="mt-0.5 h-3 w-3 shrink-0 text-muted-foreground" />
          <div className="min-w-0 flex-1">
            <p className="text-xs font-medium">{entry.title ?? entry.taskKey}</p>
            {entry.agentName && (
              <p className="mt-0.5 text-micro text-muted-foreground">
                <span className="font-medium text-foreground">{t("chatArea.chat.graph.agentLabel")}</span> {entry.agentName}
              </p>
            )}
            {entry.workingDir && (
              <p
                className="mt-0.5 break-all text-micro text-muted-foreground [overflow-wrap:anywhere]"
                title={entry.workingDir}
              >
                <span className="font-medium text-foreground">{t("chatArea.chat.graph.directoryLabel")}</span> {entry.workingDir}
              </p>
            )}
            {entry.status === "running" && hasIterations && (
              <p className="mt-1 text-micro font-medium text-warning">
                {entry.iterations[entry.iterations.length - 1]?.status === "running"
                  ? t("chatArea.chat.graph.agentWorking")
                  : t("chatArea.chat.graph.iterationsCount", { count: entry.iterations.length })}
              </p>
            )}
            {entry.error && !expanded && (
              <p className="mt-1 line-clamp-2 text-micro text-destructive">{entry.error}</p>
            )}
          </div>
          <ExpandChevron expanded={expanded} />
        </button>
        {expanded && (
          <div className="min-w-0 max-w-full space-y-2 border-t border-border/60 px-2 py-1.5">
            {hasIterations && (
              <div className="min-w-0 max-w-full space-y-1.5">
                <p className="text-micro font-medium uppercase tracking-wide text-muted-foreground">{t("chatArea.chat.graph.agentSteps")}</p>
                {entry.iterations.map((iteration) => (
                  <IterationNode key={iteration.id} entry={iteration} compact={compact} nested />
                ))}
              </div>
            )}
            {entry.error && (
              <div>
                <p className="mb-0.5 text-micro font-medium text-destructive">{t("chatArea.chat.graph.error")}</p>
                <ContentPreview content={entry.error} />
              </div>
            )}
            {entry.result && (
              <div>
                <p className="mb-0.5 text-micro font-medium text-foreground">{t("chatArea.chat.graph.result")}</p>
                <ContentPreview content={entry.result} />
              </div>
            )}
            {!hasIterations && !hasOutcome && entry.status === "running" && (
              <p className="text-micro text-muted-foreground">{t("chatArea.chat.graph.taskStarting")}</p>
            )}
          </div>
        )}
      </div>
    </div>
  );
}

function TimelineNode({
  entry,
  plan,
  compact,
  activityByTaskKey,
}: {
  entry: TimelineEntry;
  plan: OrchestrationPlan | null;
  compact?: boolean;
  activityByTaskKey: Record<string, GraphIteration[]>;
}) {
  const { t } = useI18n();
  const [showPlan, setShowPlan] = useState(false);

  if (entry.kind === "message") {
    return <MessageNode message={entry} compact={compact} />;
  }

  if (entry.kind === "iteration") {
    return <IterationNode entry={entry} compact={compact} />;
  }

  if (entry.kind === "event") {
    return <EventNode entry={entry} />;
  }

  if (entry.kind === "subtask") {
    return <SubtaskNode entry={entry} compact={compact} />;
  }

  if (entry.kind === "plan") {
    return (
      <div className={graphRow}>
        <span className="absolute left-0 top-2 h-2.5 w-2.5 -translate-x-1/2 rounded-full border-2 border-background bg-warning" />
        <div className="min-w-0 max-w-full overflow-hidden rounded-lg border border-warning/30 bg-warning/5 p-2">
          <button
            type="button"
            onClick={() => setShowPlan((v) => !v)}
            className="flex w-full min-w-0 items-start gap-2 text-left"
          >
            <StatusDot status={entry.status} />
            <div className="min-w-0 flex-1">
              <p className="text-xs font-semibold">{t("chatArea.chat.graph.orchestrationPlan")}</p>
              <p className="mt-0.5 break-words text-micro text-muted-foreground">{entry.summary}</p>
              <p className="mt-1 text-micro text-muted-foreground">{t("chatArea.chat.graph.tasksCount", { count: entry.taskCount })}</p>
            </div>
            <ExpandChevron expanded={showPlan} />
          </button>
          {showPlan && plan && (
            <div className="mt-2 min-w-0 max-w-full border-t border-border/60 pt-2">
              <PlanView plan={plan} activityByTaskKey={activityByTaskKey} />
            </div>
          )}
        </div>
      </div>
    );
  }

  return null;
}

function phaseLabel(t: (key: string, params?: Record<string, string | number>) => string, kind: SessionGraphPhaseKind): string {
  if (kind === "planning") return t("chatArea.chat.graph.phasePlanning");
  if (kind === "execution") return t("chatArea.chat.graph.phaseExecution");
  if (kind === "verification") return t("chatArea.chat.graph.phaseVerification");
  return t("chatArea.chat.graph.phaseMessages");
}

// A phase opens already expanded only when a subtask inside it is still
// running or has failed — the one thing a returning reader actually needs to
// see without clicking. Everything settled starts closed so a long run reads
// as a list of headers, not a wall of expanded cards.
function phaseNeedsAttention(phase: SessionGraphPhase): boolean {
  return (
    phase.kind === "execution" &&
    phase.entries.some((e) => e.kind === "subtask" && (e.status === "running" || e.status === "failed"))
  );
}

function PhaseSection({
  phase,
  plan,
  compact,
  activityByTaskKey,
}: {
  phase: SessionGraphPhase;
  plan: OrchestrationPlan | null;
  compact?: boolean;
  activityByTaskKey: Record<string, GraphIteration[]>;
}) {
  const { t } = useI18n();
  const [expanded, setExpanded] = useState(() => phaseNeedsAttention(phase));
  const [showInternal, setShowInternal] = useState(false);

  const visibleEntries = phase.entries.filter((entry) => !isLowSignalGraphEvent(entry));
  const internalEntries = phase.entries.filter(isLowSignalGraphEvent);

  return (
    <div className="min-w-0 max-w-full">
      <button
        type="button"
        onClick={() => setExpanded((v) => !v)}
        className="flex w-full min-w-0 items-center gap-2 rounded-md border border-border/60 bg-background/60 px-2 py-1.5 text-left"
      >
        <span className="min-w-0 flex-1 truncate text-micro font-semibold uppercase tracking-wide text-muted-foreground">
          {phaseLabel(t, phase.kind)}
        </span>
        <span className="shrink-0 text-micro text-muted-foreground">
          {t("chatArea.chat.graph.phaseEntriesCount", { count: phase.entries.length })}
        </span>
        <ExpandChevron expanded={expanded} />
      </button>
      {expanded && (
        <div className="mt-2 min-w-0 max-w-full space-y-2 pl-2">
          {visibleEntries.map((entry) => (
            <TimelineNode key={entry.id} entry={entry} plan={plan} compact={compact} activityByTaskKey={activityByTaskKey} />
          ))}
          {internalEntries.length > 0 && (
            <div className="min-w-0 max-w-full">
              <button
                type="button"
                onClick={() => setShowInternal((v) => !v)}
                className="text-micro font-medium text-muted-foreground underline decoration-dotted underline-offset-2"
              >
                {showInternal
                  ? t("chatArea.chat.graph.hideInternalEvents")
                  : t("chatArea.chat.graph.showInternalEvents", { count: internalEntries.length })}
              </button>
              {showInternal && (
                <div className="mt-1.5 min-w-0 max-w-full space-y-1.5">
                  {internalEntries.map((entry) => (
                    <TimelineNode key={entry.id} entry={entry} plan={plan} compact={compact} activityByTaskKey={activityByTaskKey} />
                  ))}
                </div>
              )}
            </div>
          )}
        </div>
      )}
    </div>
  );
}

export function SessionGraphView({ steps, plan, isLive, compact = false }: SessionGraphViewProps) {
  const { t } = useI18n();
  const entries = buildSessionGraph(steps, plan, isLive);
  const sectionRefs = useRef<Record<string, HTMLDivElement | null>>({});
  // Same subtasks, indexed for the plan card so it can show the tool calls the
  // timeline already shows.
  const activityByTaskKey: Record<string, GraphIteration[]> = {};
  for (const entry of entries) {
    if (entry.kind === "subtask" && entry.iterations.length > 0) {
      activityByTaskKey[entry.taskKey] = [...(activityByTaskKey[entry.taskKey] ?? []), ...entry.iterations];
    }
  }

  if (entries.length === 0) {
    return (
      <p className="py-4 text-center text-xs text-muted-foreground">
        {isLive ? t("chatArea.chat.graph.sessionActivityStarting") : t("chatArea.chat.graph.noSteps")}
      </p>
    );
  }

  const phases = groupSessionGraphPhases(entries);
  const subtaskPhases = phases.filter(
    (phase): phase is SessionGraphPhase & { entries: [Extract<TimelineEntry, { kind: "subtask" }>, ...TimelineEntry[]] } =>
      phase.kind === "execution" && phase.entries[0]?.kind === "subtask",
  );

  return (
    <div className="min-w-0 max-w-full">
      {subtaskPhases.length >= 2 && (
        <nav
          data-testid="session-graph-outline"
          aria-label={t("chatArea.chat.graph.outlineLabel")}
          className="sticky top-0 z-10 mb-3 min-w-0 max-w-full rounded-md border border-border/60 bg-background/95 p-2"
        >
          <p className="mb-1.5 text-micro font-medium uppercase tracking-wide text-muted-foreground">
            {t("chatArea.chat.graph.outlineLabel")}
          </p>
          <ul className="flex flex-wrap gap-1.5">
            {subtaskPhases.map((phase) => {
              const subtask = phase.entries[0];
              return (
                <li key={phase.id} className="min-w-0">
                  <button
                    type="button"
                    onClick={() => sectionRefs.current[phase.id]?.scrollIntoView({ behavior: "smooth", block: "nearest" })}
                    className="flex max-w-full items-center gap-1.5 rounded-full border border-border/60 bg-card px-2 py-1 text-micro"
                  >
                    <StatusDot status={subtask.status} />
                    <span className="max-w-32 truncate">{subtask.title ?? subtask.taskKey}</span>
                  </button>
                </li>
              );
            })}
          </ul>
        </nav>
      )}
      <div data-testid="session-graph-timeline" className="relative min-w-0 max-w-full overflow-hidden">
        <div className="absolute bottom-2 left-0 top-2 w-px bg-border" aria-hidden />
        <div className="space-y-3 pl-5">
          {phases.map((phase) => (
            <div key={phase.id} ref={(el) => { sectionRefs.current[phase.id] = el; }}>
              <PhaseSection phase={phase} plan={plan} compact={compact} activityByTaskKey={activityByTaskKey} />
            </div>
          ))}
          {isLive && (
            <div className="relative min-w-0 max-w-full">
              <p className="break-words text-micro text-muted-foreground">{t("chatArea.chat.graph.liveWaiting")}</p>
            </div>
          )}
        </div>
      </div>
    </div>
  );
}
