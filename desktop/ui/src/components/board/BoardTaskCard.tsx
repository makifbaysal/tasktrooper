import { Bot, Clock, GitMerge, GripVertical, HelpCircle, PackageCheck, Trash2, User } from "lucide-react";
import { memo } from "react";
import { Link } from "react-router-dom";
import type { BoardTask, Release } from "@/api";
import { RELEASE_STATUS_VARIANT } from "@/components/projects/repository/deploy/ReleaseDrawer";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card } from "@/components/ui/card";
import { useI18n } from "@/hooks/useI18n";
import { analysisReviewPath } from "@/lib/analysis-review";
import {
  blockedResourceLabel,
  deployOrderBlockerLabel,
  formatResumeIn,
  isMergeHold,
  pipelineGateReasonLabel,
  taskPipelineCardIcon,
  taskPriorityLabel,
  taskTypeLabel,
  workOrderBlockerLabel,
} from "@/lib/project-board";
import { cn, formatDate } from "@/lib/utils";

// The clarification chat lives under the agent that asked, so both ids are
// needed to link to it; an older blocked task may predate either.
const blockedChatPath = (task: BoardTask): string | null =>
  task.blocked_session_id && task.assignee_agent_id
    ? `/agents/${task.assignee_agent_id}/chat/${task.blocked_session_id}`
    : null;

interface BoardTaskCardProps {
  task: BoardTask;
  repositoryName: string;
  /** The assigned agent's name, if it has one. */
  assignee?: string;
  /** The person the task is assigned to, if any. */
  person?: string;
  /** The task's project; left out under a project scope, where it would repeat the picker. */
  initiative?: string;
  agentRunning: boolean;
  /** The open release carrying this task, shown on a done card. */
  release?: Release;
  dragging: boolean;
  /**
   * The column-age badge's text (lib/project-board formatColumnAge) and the
   * verify window's minutes left, computed by the board from its minute clock:
   * passing the clock itself re-rendered every memoized card once a minute.
   */
  columnAge?: string;
  releaseVerifyMinutes?: number;
  onDragStart: (taskId: string) => void;
  onDragEnd: () => void;
  onOpen: (task: BoardTask) => void;
  onDelete: (task: BoardTask) => void;
}

const HUMAN_CREATORS = new Set(["user", "human"]);

const isHumanCreator = (createdBy: string | undefined): boolean =>
  !createdBy || HUMAN_CREATORS.has(createdBy.trim().toLowerCase());

// Memoized with plain props: the board re-renders on every poll that changed
// anything, and only the cards whose own data changed need to follow.
export const BoardTaskCard = memo(function BoardTaskCard({
  task,
  repositoryName,
  assignee,
  person,
  initiative,
  agentRunning,
  release,
  dragging,
  columnAge,
  releaseVerifyMinutes,
  onDragStart,
  onDragEnd,
  onOpen,
  onDelete,
}: BoardTaskCardProps) {
  const { t } = useI18n();
  const pipelineIcon = taskPipelineCardIcon(task.latest_pipeline_status, task.latest_pipeline_gate_reason);
  // A skipped gate explains itself; an ordinary pipeline just names its status.
  const pipelineGateNote = pipelineGateReasonLabel(task.latest_pipeline_gate_reason);
  return (
    <Card
      draggable
      onDragStart={() => onDragStart(task.id)}
      onDragEnd={onDragEnd}
      onClick={() => onOpen(task)}
      className={cn(
        "mb-2 cursor-grab overflow-hidden border-border/80 p-3 transition-shadow active:cursor-grabbing",
        dragging && "opacity-50 ring-2 ring-primary/30",
        "hover:shadow-[var(--shadow-overlay)]",
      )}
    >
      <div className="flex items-start gap-2">
        <GripVertical className="mt-0.5 h-4 w-4 shrink-0 text-muted-foreground/60" />
        <div className="min-w-0 flex-1">
          <div className="mb-2 flex flex-wrap items-center gap-1">
            <Badge variant="outline" className="font-mono text-micro">
              {task.key}
            </Badge>
            {agentRunning && (
              <Badge variant="info" className="gap-1 text-micro">
                <span aria-hidden className="h-1.5 w-1.5 shrink-0 rounded-full bg-info" />
                {t("boardArea.board.agentRunning")}
              </Badge>
            )}
            <Badge variant="secondary" className="text-micro">
              {taskTypeLabel(task.task_type)}
            </Badge>
            <Badge variant="outline" className="text-micro">
              {taskPriorityLabel(task.priority)}
            </Badge>
          </div>
          <p className="break-words text-sm font-medium leading-snug">{task.title}</p>
          {task.description && (
            <p className="mt-1 line-clamp-2 break-words text-xs text-muted-foreground">{task.description}</p>
          )}
          <div className="mt-2.5 flex flex-wrap items-center gap-1.5">
            <Badge variant="outline" className="max-w-[9rem] truncate text-micro">
              {repositoryName}
            </Badge>
            {initiative && (
              <Badge variant="outline" className="max-w-[9rem] truncate text-micro">
                {initiative}
              </Badge>
            )}
            {/* A resource park is not a question: nobody answers it, a
                sweeper releases it, and blocked_question carries the
                resource's detail line rather than something to reply to. So
                it gets its own clock badge and takes the question badge's
                place — showing "Awaiting answer" on a task waiting out the
                Claude usage limit sent people hunting for a chat that does
                not exist. */}
            {task.blocked_resource === "work_order" ? (
              <Badge
                variant="outline"
                className="max-w-[9rem] truncate border-amber-500/40 bg-amber-500/10 text-micro text-amber-600 dark:text-amber-400"
                title={t("boardArea.board.blockedResourceTitle", {
                  reason: task.blocked_question || blockedResourceLabel(task.blocked_resource),
                })}
              >
                {workOrderBlockerLabel(task.blocked_question || "")}
              </Badge>
            ) : task.blocked_resource === "analysis_questions" ? (
              // Unlike every other resource park, this one is answerable —
              // same clickable treatment as the plain clarification badge
              // below, just pointed at the analysis report instead of chat.
              <Link
                to={analysisReviewPath(task.repository_id, task.id)}
                onClick={(e) => e.stopPropagation()}
                title={task.blocked_question || blockedResourceLabel(task.blocked_resource)}
              >
                <Badge
                  variant="outline"
                  className="gap-1 border-amber-500/40 bg-amber-500/10 text-micro text-amber-600 hover:bg-amber-500/20 dark:text-amber-400"
                >
                  <HelpCircle className="h-3 w-3" />
                  {t("boardArea.board.answerQuestions")}
                </Badge>
              </Link>
            ) : isMergeHold(task.blocked_resource) ? (
              <Badge
                variant="outline"
                className="max-w-[12rem] gap-1 truncate border-amber-500/40 bg-amber-500/10 text-micro text-amber-600 dark:text-amber-400"
                title={t(`boardArea.board.mergeHoldTitle.${task.blocked_resource}`, {
                  detail: task.blocked_question ?? "",
                })}
              >
                <GitMerge className="h-3 w-3 shrink-0" />
                {task.blocked_resource === "deploy_order"
                  ? deployOrderBlockerLabel(task.blocked_question ?? "")
                  : blockedResourceLabel(task.blocked_resource)}
              </Badge>
            ) : (
              task.blocked_resource && (
                <Badge
                  variant="outline"
                  className="gap-1 border-amber-500/40 bg-amber-500/10 text-micro text-amber-600 dark:text-amber-400"
                  title={
                    // Unlike every other resource, no sweeper ever releases a
                    // human_decision park — say so instead of promising a
                    // pickup that will never come.
                    task.blocked_resource === "human_decision"
                      ? t("boardArea.board.blockedHumanDecisionTitle", {
                          reason: task.blocked_question || t("boardArea.board.blockedHumanDecisionReason"),
                        })
                      : task.blocked_resume_at
                      ? t("boardArea.board.blockedResumeTitle", {
                          reason: task.blocked_question || blockedResourceLabel(task.blocked_resource),
                          value: formatDate(task.blocked_resume_at),
                        })
                      : t("boardArea.board.blockedResourceTitle", {
                          reason: task.blocked_question || blockedResourceLabel(task.blocked_resource),
                        })
                  }
                >
                  <Clock className="h-3 w-3" />
                  {blockedResourceLabel(task.blocked_resource)}
                  {task.blocked_resume_at ? ` · ~${formatResumeIn(task.blocked_resume_at)}` : ""}
                </Badge>
              )
            )}
            {task.blocked_at &&
              !task.blocked_resource &&
              (blockedChatPath(task) ? (
                // The badge is the only route to the question: without it the
                // user has to hunt for the clarification chat among sessions.
                <Link
                  to={blockedChatPath(task)!}
                  onClick={(e) => e.stopPropagation()}
                  title={task.blocked_question || undefined}
                >
                  <Badge
                    variant="outline"
                    className="gap-1 border-amber-500/40 bg-amber-500/10 text-micro text-amber-600 hover:bg-amber-500/20 dark:text-amber-400"
                  >
                    <HelpCircle className="h-3 w-3" />
                    {t("boardArea.board.answerQuestion")}
                  </Badge>
                </Link>
              ) : (
                <Badge
                  variant="outline"
                  className="gap-1 border-amber-500/40 bg-amber-500/10 text-micro text-amber-600 dark:text-amber-400"
                  title={task.blocked_question || undefined}
                >
                  <HelpCircle className="h-3 w-3" />
                  {t("boardArea.board.awaitingAnswer")}
                </Badge>
              ))}
            {release && (
              <Badge
                variant={RELEASE_STATUS_VARIANT[release.status]}
                className="gap-1 text-micro"
                title={
                  release.status === "verifying"
                    ? t("boardArea.board.releaseTitle.verifying", {
                        time: release.verify_until ? formatDate(release.verify_until) : "",
                      })
                    : release.status === "failed"
                    ? t("boardArea.board.releaseTitle.failed", { reason: release.failure_reason ?? "" })
                    : t(`boardArea.board.releaseTitle.${release.status}`)
                }
              >
                <PackageCheck className="h-3 w-3" />
                {releaseVerifyMinutes !== undefined
                  ? t("boardArea.board.releaseBadgeVerifying", {
                      status: t(`release.statuses.${release.status}`),
                      minutes: releaseVerifyMinutes,
                    })
                  : t(`release.statuses.${release.status}`)}
              </Badge>
            )}
            {columnAge && (
              <Badge
                variant="outline"
                className="text-micro"
                title={t("boardArea.board.columnAge", { value: columnAge })}
              >
                {columnAge}
              </Badge>
            )}
          </div>
          <div className="mt-2 flex items-center justify-between gap-2">
            <div className="flex min-w-0 flex-wrap items-center gap-1.5">
              {person && (
                <Badge variant="outline" className="gap-1 text-micro" title={person}>
                  <User className="h-3 w-3" />
                  <span className="max-w-[6rem] truncate">{person}</span>
                </Badge>
              )}
              {assignee && (
                <Badge variant="outline" className="gap-1 text-micro">
                  <Bot className="h-3 w-3" />
                  <span className="max-w-[6rem] truncate">{assignee}</span>
                </Badge>
              )}
              {!assignee && !person && !isHumanCreator(task.created_by) && (
                <span className="text-micro text-muted-foreground">{task.created_by}</span>
              )}
            </div>
            <div className="flex shrink-0 items-center gap-1">
              {pipelineIcon && (
                <span
                  title={
                    pipelineGateNote ||
                    t("boardArea.board.pipelineTitle", { status: task.latest_pipeline_status ?? "" })
                  }
                >
                  <pipelineIcon.Icon className={cn("h-3.5 w-3.5", pipelineIcon.className)} />
                </span>
              )}
              <Button
                variant="ghost"
                size="icon"
                className="h-7 w-7 shrink-0 text-muted-foreground hover:text-destructive"
                onClick={(e) => {
                  e.stopPropagation();
                  onDelete(task);
                }}
              >
                <Trash2 className="h-3.5 w-3.5" />
              </Button>
            </div>
          </div>
        </div>
      </div>
    </Card>
  );
});
