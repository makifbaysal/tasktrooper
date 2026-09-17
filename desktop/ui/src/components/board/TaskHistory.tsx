import { ArrowRight, History } from "lucide-react";
import { useCallback, useEffect, useState } from "react";
import { api, type BoardColumn, type PipelineGateReason, type TaskEvent } from "@/api";
import { Badge } from "@/components/ui/badge";
import { EmptyState } from "@/components/ui/empty-state";
import { Spinner } from "@/components/ui/spinner";
import { useI18n } from "@/hooks/useI18n";
import { columnLabel, pipelineGateReasonLabel } from "@/lib/project-board";
import { formatRelativeDate } from "@/lib/utils";

interface TaskHistoryProps {
  repositoryId: string;
  taskId: string;
  columns: BoardColumn[];
  /** agent id → display name, for resolving who moved/was assigned. */
  agentNameMap: Record<string, string>;
}

function payloadString(payload: Record<string, unknown> | undefined, key: string): string {
  const value = payload?.[key];
  return typeof value === "string" ? value : "";
}

/**
 * isSystemMove reports whether the control plane itself produced this move
 * (pipeline hand-off, verification bounce, reconciler) rather than a human or
 * an agent. New events carry actor="system"; the `pipeline`/`reconciled` keys
 * are the fallback that keeps events written before that key was added from
 * being attributed to the user.
 */
function isSystemMove(payload: Record<string, unknown> | undefined): boolean {
  if (payloadString(payload, "actor") === "system") return true;
  return payload?.pipeline !== undefined || payload?.reconciled !== undefined;
}

/**
 * TaskHistory renders a task's board events oldest-first: which columns it
 * moved between, who moved it, and how the assignment changed — the audit
 * trail behind the card's current state.
 */
export function TaskHistory({ repositoryId, taskId, columns, agentNameMap }: TaskHistoryProps) {
  const { t } = useI18n();
  const [events, setEvents] = useState<TaskEvent[]>([]);
  const [loading, setLoading] = useState(true);

  const load = useCallback(async () => {
    try {
      const res = await api.listTaskEvents(repositoryId, taskId);
      setEvents(res.events ?? []);
    } catch {
      setEvents([]);
    } finally {
      setLoading(false);
    }
  }, [repositoryId, taskId]);

  useEffect(() => {
    setLoading(true);
    load();
  }, [load]);

  if (loading && events.length === 0) {
    return (
      <div className="flex items-center justify-center py-4">
        <Spinner />
      </div>
    );
  }

  if (events.length === 0) {
    return <EmptyState icon={History} title={t("boardArea.components.taskHistory.empty")} />;
  }

  const agentLabel = (id: string) => agentNameMap[id] ?? t("boardArea.components.taskHistory.unknownAgent");

  const eventTitle = (event: TaskEvent) => {
    switch (event.event_type) {
      case "task.created":
        return t("boardArea.components.taskHistory.event.created");
      case "task.moved":
        return t("boardArea.components.taskHistory.event.moved");
      case "task.assigned":
        return t("boardArea.components.taskHistory.event.assigned");
      case "task.commented":
        return t("boardArea.components.taskHistory.event.commented");
      default:
        return event.event_type;
    }
  };

  const eventActor = (event: TaskEvent) => {
    const payload = event.payload;
    switch (event.event_type) {
      case "task.moved": {
        const actor = payloadString(payload, "actor_agent_id");
        if (actor) return agentLabel(actor);
        if (isSystemMove(payload)) return t("boardArea.components.taskHistory.system");
        return t("boardArea.components.taskHistory.human");
      }
      case "task.commented": {
        const name = payloadString(payload, "author_name");
        if (name) return name;
        const authorType = payloadString(payload, "author_type");
        const authorID = payloadString(payload, "author_id");
        if (authorType === "agent") {
          return authorID ? agentLabel(authorID) : t("boardArea.components.taskHistory.unknownAgent");
        }
        // Verification bounces, clarification answers and gate blocks are all
        // written by the control plane; attributing them to the human made the
        // history claim they commented on their own task.
        if (authorType === "system") return t("boardArea.components.taskHistory.system");
        return t("boardArea.components.taskHistory.human");
      }
      default:
        return "";
    }
  };

  return (
    <ol className="space-y-2">
      {events.map((event) => {
        const payload = event.payload;
        const from =
          event.event_type === "task.moved"
            ? columnLabel(payloadString(payload, "from_column"), columns)
            : "";
        const to =
          event.event_type === "task.moved"
            ? columnLabel(payloadString(payload, "to_column"), columns)
            : event.event_type === "task.created"
              ? columnLabel(payloadString(payload, "column"), columns)
              : "";
        const fromAgent = payloadString(payload, "from_agent_id");
        const toAgent = payloadString(payload, "to_agent_id");
        const actor = eventActor(event);
        // Why the system moved it. The pipeline hand-off carries no columns at
        // all (it hands the task to a reviewer without changing column), so
        // without this the row rendered as two empty badges and an arrow.
        const reasonKey =
          payloadString(payload, "system_reason") ||
          (payload?.pipeline !== undefined
            ? "pipeline_passed"
            : payload?.reconciled !== undefined
              ? "reconciled"
              : "");
        const reasonI18nKey = `boardArea.components.taskHistory.reason.${reasonKey}`;
        const reasonText = reasonKey ? t(reasonI18nKey) : "";
        // An unknown reason resolves to its own key — show nothing rather than
        // a dotted path.
        let reason = reasonText === reasonI18nKey ? "" : reasonText;
        // When the code-review gate was opened WITHOUT a build behind it, the
        // payload also names which of the four reasons it was. Appending it here
        // is what makes the history row diagnostic rather than merely honest:
        // "no build result arrived" and "CI is out of quota" call for different
        // actions from the person reading the card.
        const gateReason = payloadString(payload, "pipeline_gate");
        if (gateReason) {
          const gateLabel = pipelineGateReasonLabel(gateReason as PipelineGateReason);
          if (gateLabel) reason = reason ? `${reason} (${gateLabel})` : gateLabel;
        }
        const showColumns = event.event_type === "task.moved" && Boolean(from || to);

        return (
          <li key={event.id} className="rounded-md border border-border bg-card px-default py-compact">
            <div className="flex flex-wrap items-center gap-2 text-xs">
              <span className="font-medium">{eventTitle(event)}</span>
              <span className="text-muted-foreground">{formatRelativeDate(event.created_at)}</span>
            </div>

            {showColumns && (
              <div className="mt-1.5 flex flex-wrap items-center gap-1.5 text-xs">
                <Badge variant="outline" className="font-normal">
                  {from}
                </Badge>
                <ArrowRight className="h-3 w-3 text-muted-foreground" />
                <Badge variant="secondary" className="font-normal">
                  {to}
                </Badge>
              </div>
            )}

            {reason && <p className="mt-1.5 text-xs text-muted-foreground">{reason}</p>}

            {event.event_type === "task.assigned" && (fromAgent || toAgent) && (
              <div className="mt-1.5 flex flex-wrap items-center gap-1.5 text-xs">
                <Badge variant="outline" className="font-normal">
                  {fromAgent ? agentLabel(fromAgent) : t("boardArea.components.taskHistory.unassigned")}
                </Badge>
                <ArrowRight className="h-3 w-3 text-muted-foreground" />
                <Badge variant="secondary" className="font-normal">
                  {toAgent ? agentLabel(toAgent) : t("boardArea.components.taskHistory.unassigned")}
                </Badge>
              </div>
            )}

            {event.event_type === "task.created" && to && (
              <div className="mt-1.5 flex flex-wrap items-center gap-1.5 text-xs">
                <Badge variant="secondary" className="font-normal">
                  {to}
                </Badge>
              </div>
            )}

            {actor && (
              <p className="mt-1 text-[11px] text-muted-foreground">
                {t("boardArea.components.taskHistory.by", { name: actor })}
              </p>
            )}
          </li>
        );
      })}
    </ol>
  );
}
