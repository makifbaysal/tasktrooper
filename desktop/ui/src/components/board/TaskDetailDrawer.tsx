import { Bot, Check, CircleStop, Clock, Cog, ExternalLink, FileText, FlaskConical, GitBranch, GitPullRequest, HelpCircle, History, Loader2, MessageSquare, MessagesSquare, Minus, Paperclip, Plus, Rocket, RotateCcw, User, X } from "lucide-react";
import { useCallback, useEffect, useMemo, useState, type MouseEvent } from "react";
import { Link, useNavigate } from "react-router-dom";
import { toast } from "sonner";
import {
  api,
  type AcceptanceCriterion,
  type Agent,
  type AttachmentMeta,
  type CriterionCheck,
  type CriterionReviewRole,
  type BoardTask,
  type InitiativeProject,
  type Repository,
  type SessionRun,
  type TaskAgentRun,
  type TaskComment,
  type TaskColumn,
  type TaskDocument,
  type TaskPriority,
  type TaskTestCase,
  type TestCaseStatus,
  type TaskType,
  type BoardColumn,
  type BoardMember,
} from "@/api";
import { MultiSelectPicker } from "@/components/admin/MultiSelectPicker";
import { ActivityPanel } from "@/components/chat/ActivityPanel";
import { AttachmentDropzone } from "@/components/attachments/AttachmentDropzone";
import { AttachmentList } from "@/components/attachments/AttachmentList";
import { PlanView } from "@/components/chat/PlanView";
import { PipelineSection } from "@/components/board/PipelineSection";
import { TaskAssigneeFields } from "@/components/board/TaskAssigneeFields";
import { TaskDocumentList } from "@/components/board/TaskDocumentList";
import { TaskHistory } from "@/components/board/TaskHistory";
import { MarkdownContent } from "@/components/markdown/MarkdownContent";
import { MarkdownField } from "@/components/markdown/MarkdownField";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Checkbox } from "@/components/ui/checkbox";
import { ConfirmDialog } from "@/components/ui/confirm-dialog";
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { ScrollArea } from "@/components/ui/scroll-area";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { Separator } from "@/components/ui/separator";
import { useI18n } from "@/hooks/useI18n";
import { usePolling } from "@/hooks/usePolling";
import { useRunActivity } from "@/hooks/useRunActivity";
import { desktopRunner } from "@/lib/desktop-bridge";
import { subtaskActivityByKey } from "@/lib/sessionGraph";
import {
  blockedResourceLabel,
  columnLabel,
  formatResumeIn,
  TASK_PRIORITY_OPTIONS,
  TASK_TYPE_OPTIONS,
  taskPriorityLabel,
  taskTypeLabel,
} from "@/lib/project-board";
import { formatDate, formatRelativeDate } from "@/lib/utils";
import { cn } from "@/lib/utils";

interface TaskDetailDrawerProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  repositoryId: string;
  task: BoardTask | null;
  columns: BoardColumn[];
  members: BoardMember[];
  agents: Agent[];
  initiativeProjects: InitiativeProject[];
  repositories: Repository[];
  onUpdated: () => void;
}

function runStatusVariant(status: string): "success" | "warning" | "destructive" | "secondary" {
  switch (status) {
    case "completed":
      return "success";
    case "running":
    case "pending":
      return "warning";
    case "failed":
      return "destructive";
    // Somebody stopped this run on purpose; it is history, not an incident, so
    // it must never borrow the failure colour.
    case "cancelled":
      return "secondary";
    default:
      return "secondary";
  }
}

// The server only accepts a stop while the run is still open, and a rerun only
// once it has settled — mirroring that here keeps a doomed request off the wire.
const STOPPABLE_RUN_STATUSES = new Set(["pending", "running"]);
const RERUNNABLE_RUN_STATUSES = new Set(["completed", "failed", "cancelled"]);

// prompt keeps the server's contract: it is the TOTAL prompt size, with the
// cache counters as subsets of it — total spend is prompt + completion.
interface TokenTally {
  prompt: number;
  completion: number;
  cacheRead: number;
  cacheWrite: number;
}

const formatTokenCount = (n: number) =>
  new Intl.NumberFormat(undefined, { notation: "compact", maximumFractionDigits: 1 }).format(n);

export function TaskDetailDrawer({
  open,
  onOpenChange,
  repositoryId,
  task,
  columns,
  members,
  agents,
  initiativeProjects,
  repositories,
  onUpdated,
}: TaskDetailDrawerProps) {
  const { t } = useI18n();
  const navigate = useNavigate();
  const [comments, setComments] = useState<TaskComment[]>([]);
  const [runs, setRuns] = useState<TaskAgentRun[]>([]);
  const [documents, setDocuments] = useState<TaskDocument[]>([]);
  const [attachments, setAttachments] = useState<AttachmentMeta[]>([]);
  const [criteria, setCriteria] = useState<AcceptanceCriterion[]>([]);
  const [testCases, setTestCases] = useState<TaskTestCase[]>([]);
  const [commentText, setCommentText] = useState("");
  const [selectedRunId, setSelectedRunId] = useState<string | null>(null);
  const [submittingComment, setSubmittingComment] = useState(false);
  const [saving, setSaving] = useState(false);
  const [editingDescription, setEditingDescription] = useState(false);
  const [editingTechnical, setEditingTechnical] = useState(false);
  const [descriptionDraft, setDescriptionDraft] = useState("");
  const [technicalDraft, setTechnicalDraft] = useState("");
  const [newDocTitle, setNewDocTitle] = useState("");
  const [newDocContent, setNewDocContent] = useState("");
  const [addingDoc, setAddingDoc] = useState(false);
  const [runActionId, setRunActionId] = useState<string | null>(null);
  const [stopRunId, setStopRunId] = useState<string | null>(null);
  const [openingChat, setOpeningChat] = useState(false);
  // Deploy runbook: three inline-editable fields, same pattern as description
  // and technical description above.
  const [editingBeforeDeploy, setEditingBeforeDeploy] = useState(false);
  const [editingAfterDeploy, setEditingAfterDeploy] = useState(false);
  const [editingRollback, setEditingRollback] = useState(false);
  const [beforeDeployDraft, setBeforeDeployDraft] = useState("");
  const [afterDeployDraft, setAfterDeployDraft] = useState("");
  const [rollbackDraft, setRollbackDraft] = useState("");
  // Deploy dependencies. repoTasks backs both the picker and the status badge
  // next to each dependency — task.relations carries the key but not the
  // column, and "which of these has actually shipped" is the whole question.
  const [editingDeps, setEditingDeps] = useState(false);
  const [depsDraft, setDepsDraft] = useState<string[]>([]);
  const [repoTasks, setRepoTasks] = useState<BoardTask[]>([]);

  const memberAgents = agents.filter((a) => members.some((m) => m.agent_id === a.id));
  const agentNameMap = useMemo(
    () => Object.fromEntries(agents.map((agent) => [agent.id, agent.name])),
    [agents],
  );
  const assigneeName = task?.assignee_agent_id
    ? agents.find((a) => a.id === task.assignee_agent_id)?.name
    : null;
  const repositoryName = repositories.find((r) => r.id === repositoryId)?.name;
  const initiativeName = task?.initiative_project_id
    ? initiativeProjects.find((p) => p.id === task.initiative_project_id)?.name
    : null;

  // Deploy dependencies get their own section (they are enforced at release
  // time); everything else stays in the descriptive relations list, so neither
  // is shown twice.
  const deployDependencies = useMemo(
    () => (task?.relations ?? []).filter((rel) => rel.relation_type === "deploy_depends_on"),
    [task],
  );
  const otherRelations = useMemo(
    () => (task?.relations ?? []).filter((rel) => rel.relation_type !== "deploy_depends_on"),
    [task],
  );
  const columnByTaskId = useMemo(
    () => new Map(repoTasks.map((item) => [item.id, item.column])),
    [repoTasks],
  );

  // Criteria are fetched, not read off the task: the board lists tasks via
  // listAllTasks, which does not populate acceptance_criteria, so the drawer
  // used to show "0" for every task and the 2s poll reverted each tick.
  // Settled per call so one failing section doesn't blank the others.
  const loadDetails = useCallback(async () => {
    if (!task || !repositoryId) return;
    const [c, r, d, cr, at, tc] = await Promise.allSettled([
      api.listTaskComments(repositoryId, task.id),
      api.listTaskAgentRuns(repositoryId, task.id),
      api.listTaskDocuments(repositoryId, task.id),
      api.listAcceptanceCriteria(repositoryId, task.id),
      api.listTaskAttachments(repositoryId, task.id),
      api.listTestCases(repositoryId, task.id),
    ]);
    if (c.status === "fulfilled") setComments(c.value.comments ?? []);
    if (r.status === "fulfilled") setRuns(r.value.runs ?? []);
    if (d.status === "fulfilled") setDocuments(d.value.documents ?? []);
    if (cr.status === "fulfilled") setCriteria(cr.value.items ?? []);
    if (at.status === "fulfilled") setAttachments(at.value.attachments ?? []);
    if (tc.status === "fulfilled") setTestCases(tc.value.items ?? []);
  }, [repositoryId, task]);

  // Reset the editing state only when a different task is shown. The board polls
  // and re-derives `task` from the fresh list, so a `task`-identity dependency
  // fired every two seconds — closing the open editor, discarding whatever the
  // user had typed, and collapsing the selected run.
  const taskID = task?.id ?? null;
  useEffect(() => {
    if (!open || !task) return;
    setSelectedRunId(null);
    setEditingDescription(false);
    setEditingTechnical(false);
    setDescriptionDraft(task.description);
    setTechnicalDraft(task.technical_description);
    setEditingBeforeDeploy(false);
    setEditingAfterDeploy(false);
    setEditingRollback(false);
    setEditingDeps(false);
    setBeforeDeployDraft(task.before_deploy ?? "");
    setAfterDeployDraft(task.after_deploy ?? "");
    setRollbackDraft(task.rollback_plan ?? "");
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [open, taskID]);

  // Loaded once per opened task rather than in the 2s poll: this is only used
  // to name and badge the deploy dependencies, which change when someone edits
  // them, not every tick.
  useEffect(() => {
    if (!open || !repositoryId) return;
    let cancelled = false;
    api
      .listRepositoryTasks(repositoryId)
      .then((data) => {
        if (!cancelled) setRepoTasks(data.tasks ?? []);
      })
      .catch(() => {
        if (!cancelled) setRepoTasks([]);
      });
    return () => {
      cancelled = true;
    };
  }, [open, repositoryId, taskID]);

  useEffect(() => {
    if (open && task) loadDetails();
  }, [open, task, loadDetails]);

  usePolling(loadDetails, 2000, open && !!task);

  // "What is the agent doing right now" is the first question the card is opened
  // with, and the activity below the run list only renders for a selected run —
  // so a live run selects itself as soon as the list arrives. Guarded on the
  // current selection, never on the runs themselves: picking another run (or
  // deselecting) must survive the 2s poll.
  useEffect(() => {
    if (!open || selectedRunId) return;
    const live = runs.find((r) => r.session_run_id && STOPPABLE_RUN_STATUSES.has(r.status));
    if (live?.session_run_id) setSelectedRunId(live.session_run_id);
  }, [open, runs, selectedRunId]);

  const selectedTaskRun = runs.find((run) => run.session_run_id === selectedRunId) ?? null;

  // Per-agent token totals across every run of this task, for the summary
  // block above the run list. Runs with no recorded usage (pre-migration rows,
  // runs that died before their first LLM call) contribute nothing.
  const agentTokenTotals = useMemo(() => {
    const byAgent = new Map<string, TokenTally>();
    for (const r of runs) {
      const prompt = r.prompt_tokens ?? 0;
      const completion = r.completion_tokens ?? 0;
      if (prompt + completion === 0) continue;
      const acc = byAgent.get(r.agent_id) ?? { prompt: 0, completion: 0, cacheRead: 0, cacheWrite: 0 };
      acc.prompt += prompt;
      acc.completion += completion;
      acc.cacheRead += r.cache_read_tokens ?? 0;
      acc.cacheWrite += r.cache_write_tokens ?? 0;
      byAgent.set(r.agent_id, acc);
    }
    return [...byAgent.entries()];
  }, [runs]);

  const tokenLine = useCallback(
    (u: TokenTally) => {
      const parts = [
        `${formatTokenCount(u.prompt)} ${t("boardArea.components.taskDetail.tokenIn")}`,
        `${formatTokenCount(u.completion)} ${t("boardArea.components.taskDetail.tokenOut")}`,
      ];
      if (u.cacheRead > 0) parts.push(`${formatTokenCount(u.cacheRead)} ${t("boardArea.components.taskDetail.tokenCacheRead")}`);
      if (u.cacheWrite > 0) parts.push(`${formatTokenCount(u.cacheWrite)} ${t("boardArea.components.taskDetail.tokenCacheWrite")}`);
      parts.push(`${formatTokenCount(u.prompt + u.completion)} ${t("boardArea.components.taskDetail.tokenTotal")}`);
      return parts.join(" · ");
    },
    [t],
  );
  const {
    steps: runSteps,
    plan: runPlan,
    liveSummary,
    isLive: runIsLive,
    loading: runActivityLoading,
  } = useRunActivity(selectedRunId, open && !!selectedRunId, selectedTaskRun?.status);

  // The plan says which subtasks ran; the step stream says what happened inside
  // each one. The card is only readable with both.
  const runSubtaskActivity = useMemo(
    () => subtaskActivityByKey(runSteps, runPlan, runIsLive),
    [runSteps, runPlan, runIsLive],
  );

  const selectedSessionRun: SessionRun | null = selectedRunId
    ? {
        id: selectedRunId,
        request_id: selectedTaskRun?.id ?? "",
        status: selectedTaskRun?.status ?? "unknown",
        started_at: selectedTaskRun?.created_at ?? new Date().toISOString(),
      }
    : null;

  const patchTask = async (data: Parameters<typeof api.updateRepositoryTask>[2]) => {
    if (!task) return;
    setSaving(true);
    try {
      await api.updateRepositoryTask(repositoryId, task.id, data);
      onUpdated();
      toast.success(t("boardArea.components.taskDetail.updated"));
    } catch (e) {
      toast.error(e instanceof Error ? e.message : t("boardArea.components.taskDetail.updateFailed"));
    } finally {
      setSaving(false);
    }
  };

  // Unassigning sends `null`, which the server honours only since it moved the
  // assignee onto domain.Nullable — before that this control was a silent
  // no-op, the request going out and nothing being cleared. See
  // UpdateBoardTaskInput for the three spellings.
  const handleAssignee = (agentId: string) => {
    patchTask({ assignee_agent_id: agentId || null });
  };

  const handleColumn = (column: TaskColumn) => {
    patchTask({ column });
  };

  // Desktop only: `target="_blank"` normally hands this off to the OS, but a
  // PR link asks the hosted view's own navigation to decide first — which is
  // what used to leave the shell on its loading screen with no way back (see
  // desktop/src/main/window.ts). Asking the shell directly, before the anchor
  // gets a chance to navigate anywhere, is what makes that impossible: there
  // is no in-between state for this click to get stuck in. In a plain browser
  // there is no bridge, so the anchor's own `target="_blank"` still applies.
  const handlePrClick = (event: MouseEvent<HTMLAnchorElement>) => {
    const url = task?.pr_url;
    const runner = desktopRunner();
    if (!url || !runner) return;
    event.preventDefault();
    void runner.openExternal(url).then((opened) => {
      if (!opened) toast.error(t("boardArea.components.taskDetail.pullRequestLinkFailed"));
    });
  };

  const handleType = (taskType: TaskType) => {
    patchTask({ task_type: taskType });
  };

  const handlePriority = (priority: TaskPriority) => {
    patchTask({ priority });
  };

  const handleInitiative = (value: string) => {
    patchTask({ initiative_project_id: value === "none" ? null : value });
  };

  const saveDescription = async () => {
    await patchTask({ description: descriptionDraft });
    setEditingDescription(false);
  };

  const saveTechnical = async () => {
    await patchTask({ technical_description: technicalDraft });
    setEditingTechnical(false);
  };

  const saveBeforeDeploy = async () => {
    await patchTask({ before_deploy: beforeDeployDraft });
    setEditingBeforeDeploy(false);
  };

  const saveAfterDeploy = async () => {
    await patchTask({ after_deploy: afterDeployDraft });
    setEditingAfterDeploy(false);
  };

  const saveRollbackPlan = async () => {
    await patchTask({ rollback_plan: rollbackDraft });
    setEditingRollback(false);
  };

  // deploy_depends_on is replaced wholesale; the backend leaves every other
  // relation type on the task untouched.
  const saveDeployDependencies = async (targetIds: string[]) => {
    await patchTask({
      deploy_depends_on: targetIds.map((id) => ({
        target_task_id: id,
        relation_type: "deploy_depends_on" as const,
      })),
    });
    setEditingDeps(false);
  };

  const toggleCriterion = async (criterion: AcceptanceCriterion) => {
    if (!task) return;
    try {
      const updated = await api.updateAcceptanceCriterion(
        repositoryId,
        task.id,
        criterion.id,
        !criterion.completed,
      );
      setCriteria((prev) => prev.map((c) => (c.id === updated.id ? updated : c)));
    } catch (e) {
      toast.error(e instanceof Error ? e.message : t("boardArea.components.taskDetail.criterionUpdateFailed"));
    }
  };

  const handleComment = async () => {
    if (!task || !commentText.trim()) return;
    setSubmittingComment(true);
    try {
      await api.createTaskComment(repositoryId, task.id, commentText.trim());
      setCommentText("");
      await loadDetails();
      onUpdated();
      toast.success(t("boardArea.components.taskDetail.commentAdded"));
    } catch (e) {
      toast.error(e instanceof Error ? e.message : t("boardArea.components.taskDetail.commentFailed"));
    } finally {
      setSubmittingComment(false);
    }
  };

  const addDocument = async () => {
    if (!task || !newDocTitle.trim()) return;
    setAddingDoc(true);
    try {
      await api.createTaskDocument(repositoryId, task.id, {
        title: newDocTitle.trim(),
        content: newDocContent.trim(),
        position: documents.length,
      });
      setNewDocTitle("");
      setNewDocContent("");
      await loadDetails();
      toast.success(t("boardArea.components.taskDetail.docAdded"));
    } catch (e) {
      toast.error(e instanceof Error ? e.message : t("boardArea.components.taskDetail.docFailed"));
    } finally {
      setAddingDoc(false);
    }
  };

  const deleteDocument = async (docId: string) => {
    if (!task) return;
    try {
      await api.deleteTaskDocument(repositoryId, task.id, docId);
      await loadDetails();
      toast.success(t("boardArea.components.taskDetail.docDeleted"));
    } catch (e) {
      toast.error(e instanceof Error ? e.message : t("boardArea.components.taskDetail.deleteFailed"));
    }
  };

  // The dropzone already uploaded the bytes; here each fresh id is linked to
  // the task, then the list is re-read from the server as the source of truth.
  const linkUploadedAttachments = async (metas: AttachmentMeta[]) => {
    if (!task) return;
    try {
      for (const meta of metas) {
        await api.linkTaskAttachment(repositoryId, task.id, meta.id);
      }
      await loadDetails();
      toast.success(t("boardArea.components.taskDetail.attachmentAdded"));
    } catch (e) {
      toast.error(e instanceof Error ? e.message : t("boardArea.components.taskDetail.attachmentFailed"));
    }
  };

  const unlinkAttachment = async (meta: AttachmentMeta) => {
    if (!task) return;
    try {
      await api.unlinkTaskAttachment(repositoryId, task.id, meta.id);
      await loadDetails();
      toast.success(t("boardArea.components.taskDetail.attachmentRemoved"));
    } catch (e) {
      toast.error(e instanceof Error ? e.message : t("boardArea.components.taskDetail.deleteFailed"));
    }
  };

  // Both actions reload in `finally`: a 409 means the run settled while the
  // button was still on screen, so the refreshed list is the answer to the
  // error just as much as it is to the success.
  const stopRun = async () => {
    if (!task || !stopRunId) return;
    setRunActionId(stopRunId);
    try {
      await api.cancelTaskAgentRun(repositoryId, task.id, stopRunId);
      toast.success(t("boardArea.components.taskDetail.runStopped"));
      // Stopping parks the task as blocked, so the task itself is stale too.
      onUpdated();
    } catch (e) {
      toast.error(e instanceof Error ? e.message : t("boardArea.components.taskDetail.runStopFailed"));
    } finally {
      await loadDetails();
      setRunActionId(null);
      setStopRunId(null);
    }
  };

  // The thread is opened server-side (it is seeded with the task and its pull
  // request), and it lives under the agent the endpoint hands back — which is
  // not necessarily the assignee, so the id from the response is the only one
  // that can be trusted for the deep link.
  const discussTask = async () => {
    if (!task || openingChat) return;
    setOpeningChat(true);
    try {
      const { session_id, agent_id } = await api.openTaskChat(repositoryId, task.id);
      onOpenChange(false);
      navigate(`/agents/${agent_id}/chat/${session_id}`);
    } catch (e) {
      toast.error(e instanceof Error ? e.message : t("boardArea.components.taskDetail.discussFailed"));
    } finally {
      setOpeningChat(false);
    }
  };

  const rerunRun = async (runId: string) => {
    if (!task) return;
    setRunActionId(runId);
    try {
      await api.rerunTaskAgentRun(repositoryId, task.id, runId);
      toast.success(t("boardArea.components.taskDetail.runRerunStarted"));
    } catch (e) {
      toast.error(e instanceof Error ? e.message : t("boardArea.components.taskDetail.runRerunFailed"));
    } finally {
      await loadDetails();
      setRunActionId(null);
    }
  };

  return (
    <>
      <Dialog open={open} onOpenChange={onOpenChange}>
        <DialogContent className="flex h-[90vh] w-[95vw] max-w-6xl flex-col gap-0 overflow-hidden p-0">
          <DialogHeader className="border-b border-border px-6 py-4">
            <div className="flex flex-wrap items-center gap-2 pr-6">
              {task && (
                <Badge variant="outline" className="font-mono">
                  {task.key}
                </Badge>
              )}
              <DialogTitle className="text-left">{task?.title ?? t("boardArea.components.taskDetail.taskFallback")}</DialogTitle>
              {task && (
                <Badge variant="outline">{columnLabel(task.column, columns)}</Badge>
              )}
            </div>
            <div className="mt-2 flex flex-wrap gap-2 text-left">
              {repositoryName && <Badge variant="secondary">{repositoryName}</Badge>}
              {initiativeName && <Badge variant="outline">{initiativeName}</Badge>}
              {task && (
                <>
                  <Badge variant="outline">{taskTypeLabel(task.task_type)}</Badge>
                  <Badge variant="outline">{taskPriorityLabel(task.priority)}</Badge>
                </>
              )}
            </div>
          </DialogHeader>

          {task && (
            <div className="flex min-h-0 flex-1 flex-col lg:flex-row">
            <ScrollArea className="min-h-0 flex-1">
              <div className="space-y-6 px-6 py-5">
                {/* A parked task is waiting on a human, so the question outranks
                    every other field here — it is what unblocks the work. */}
                {task.blocked_at && (
                  <section className="space-y-2 rounded-lg border border-amber-500/40 bg-amber-500/10 p-3">
                    <div className="flex items-center gap-2 text-amber-600 dark:text-amber-400">
                      {/* A resource park waits on a sweeper, not on the reader,
                          so it says what it is waiting for instead of asking
                          for an answer that would never be read. */}
                      {task.blocked_resource ? (
                        <Clock className="h-4 w-4 shrink-0" />
                      ) : (
                        <HelpCircle className="h-4 w-4 shrink-0" />
                      )}
                      <span className="text-sm font-medium">
                        {task.blocked_resource
                          ? blockedResourceLabel(task.blocked_resource)
                          : t("boardArea.components.taskDetail.blockedTitle")}
                      </span>
                    </div>
                    {/* The parked question carries the agent's context line and
                        every question it asked, one per line — keep the breaks.
                        For a resource park the same field holds the block's
                        detail ("Claude AI usage limit reached"), so it reads the
                        same way. */}
                    {task.blocked_question && (
                      <p className="whitespace-pre-line text-sm text-foreground">
                        {task.blocked_question}
                      </p>
                    )}
                    {task.blocked_resource && (
                      <p className="text-xs text-amber-700 dark:text-amber-400">
                        {task.blocked_resource === "human_decision"
                          ? t("boardArea.components.taskDetail.blockedHumanDecision")
                          : task.blocked_resume_at
                          ? t("boardArea.components.taskDetail.blockedResumeAt", {
                              relative: formatResumeIn(task.blocked_resume_at),
                              absolute: formatDate(task.blocked_resume_at),
                            })
                          : t("boardArea.components.taskDetail.blockedNoResume")}
                      </p>
                    )}
                    {task.blocked_session_id && task.assignee_agent_id && (
                      <Button asChild size="sm" variant="outline">
                        <Link to={`/agents/${task.assignee_agent_id}/chat/${task.blocked_session_id}`}>
                          {t("boardArea.components.taskDetail.blockedOpenChat")}
                        </Link>
                      </Button>
                    )}
                  </section>
                )}
                {/* A schema change cannot reach production until a stage deploy
                    has actually applied it, so the gate's state belongs next to
                    the task, not only in the release error. */}
                {task.has_migration && (
                  <section className="flex flex-wrap items-center gap-2 rounded-lg border border-border p-3">
                    <Badge variant="warning">{t("projectAdmin.prodOps.migrationBadge")}</Badge>
                    {task.stage_verified_at ? (
                      <Badge variant="success">{t("projectAdmin.prodOps.migrationGateOpen")}</Badge>
                    ) : (
                      <Badge variant="destructive">{t("projectAdmin.prodOps.migrationGateBlocked")}</Badge>
                    )}
                  </section>
                )}
                <section className="space-y-2">
                  <div className="flex items-center justify-between">
                    <Label>{t("boardArea.components.taskDetail.description")}</Label>
                    <Button
                      variant="ghost"
                      size="sm"
                      onClick={() => {
                        if (editingDescription) saveDescription();
                        else setEditingDescription(true);
                      }}
                      disabled={saving}
                    >
                      {editingDescription ? t("common.save") : t("boardArea.components.taskDetail.edit")}
                    </Button>
                  </div>
                  {editingDescription ? (
                    <MarkdownField value={descriptionDraft} onChange={setDescriptionDraft} />
                  ) : (
                    <MarkdownContent content={task.description} />
                  )}
                </section>

                <section className="space-y-2">
                  <div className="flex items-center justify-between">
                    <Label>{t("boardArea.components.taskDetail.technical")}</Label>
                    <Button
                      variant="ghost"
                      size="sm"
                      onClick={() => {
                        if (editingTechnical) saveTechnical();
                        else setEditingTechnical(true);
                      }}
                      disabled={saving}
                    >
                      {editingTechnical ? t("common.save") : t("boardArea.components.taskDetail.edit")}
                    </Button>
                  </div>
                  {editingTechnical ? (
                    <MarkdownField value={technicalDraft} onChange={setTechnicalDraft} />
                  ) : (
                    <MarkdownContent content={task.technical_description} />
                  )}
                </section>

                <Separator />

                <section className="space-y-3">
                  <Label className="flex items-center gap-2 text-muted-foreground">
                    <Check className="h-3.5 w-3.5" />
                    {t("boardArea.components.taskDetail.criteria", { count: criteria.length })}
                  </Label>
                  {criteria.length === 0 ? (
                    <p className="text-xs text-muted-foreground">{t("boardArea.components.taskDetail.noCriteria")}</p>
                  ) : (
                    <div className="space-y-2">
                      {criteria.map((c) => {
                        const rejections = (c.checks ?? []).filter((ch) => !ch.approved && ch.note);
                        return (
                          <div key={c.id} className="rounded-md border border-border px-3 py-2">
                            <div className="flex items-start gap-2">
                              <label className="flex flex-1 cursor-pointer items-start gap-2">
                                <Checkbox
                                  checked={c.completed}
                                  onCheckedChange={() => toggleCriterion(c)}
                                  className="mt-0.5"
                                />
                                <span
                                  className={cn(
                                    "text-sm",
                                    (c.completed || c.canceled) && "text-muted-foreground line-through",
                                  )}
                                >
                                  {c.text}
                                </span>
                              </label>
                              <div className="flex shrink-0 items-center gap-1">
                                {c.canceled && (
                                  <Badge variant="outline" className="text-[10px] text-muted-foreground">
                                    {t("boardArea.components.taskDetail.criterionCanceled")}
                                  </Badge>
                                )}
                                <CriterionDevBadge completed={c.completed} t={t} />
                                <CriterionCheckBadge role="qa" checks={c.checks} t={t} />
                                <CriterionCheckBadge role="pm" checks={c.checks} t={t} />
                              </div>
                            </div>
                            {/* The reason a criterion was dropped belongs next to the
                                criterion; without it a struck-through line reads as
                                "someone gave up here". */}
                            {c.canceled && c.cancel_reason && (
                              <p className="mt-1.5 pl-6 text-xs text-muted-foreground">
                                {t("boardArea.components.taskDetail.criterionCancelReason", {
                                  reason: c.cancel_reason,
                                })}
                              </p>
                            )}
                            {rejections.map((ch) => (
                              <p key={ch.id} className="mt-1.5 pl-6 text-xs text-destructive">
                                {roleLabel(ch.role, t)}: {ch.note}
                              </p>
                            ))}
                          </div>
                        );
                      })}
                    </div>
                  )}
                </section>

                <Separator />

                {/* The test round sits under the criteria on purpose: the criteria
                    say what was asked for, this says what was actually tried. */}
                <section className="space-y-3">
                  <Label className="flex items-center gap-2 text-muted-foreground">
                    <FlaskConical className="h-3.5 w-3.5" />
                    {t("boardArea.components.taskDetail.testCases", { count: testCases.length })}
                  </Label>
                  <TestCaseList items={testCases} t={t} />
                </section>

                <Separator />

                <section className="space-y-4">
                  <Label className="flex items-center gap-2 text-muted-foreground">
                    <Rocket className="h-3.5 w-3.5" />
                    {t("boardArea.components.taskDetail.deploy")}
                  </Label>

                  <div className="space-y-2">
                    <div className="flex items-center justify-between">
                      <Label>{t("boardArea.components.taskDetail.deployDependsOn")}</Label>
                      <Button
                        variant="ghost"
                        size="sm"
                        onClick={() => {
                          if (editingDeps) {
                            saveDeployDependencies(depsDraft);
                          } else {
                            setDepsDraft(deployDependencies.map((rel) => rel.target_task_id));
                            setEditingDeps(true);
                          }
                        }}
                        disabled={saving}
                      >
                        {editingDeps ? t("common.save") : t("boardArea.components.taskDetail.edit")}
                      </Button>
                    </div>
                    {editingDeps ? (
                      <MultiSelectPicker
                        label={t("boardArea.components.taskDetail.deployDependsOn")}
                        options={repoTasks
                          .filter((item) => item.id !== task.id)
                          .map((item) => ({
                            value: item.id,
                            label: `${item.key} · ${item.title}`,
                            description: columnLabel(item.column, columns),
                          }))}
                        selected={depsDraft}
                        onChange={setDepsDraft}
                        emptyText={t("boardArea.components.taskDetail.deployDependsOnEmpty")}
                      />
                    ) : deployDependencies.length === 0 ? (
                      <p className="text-xs text-muted-foreground">
                        {t("boardArea.components.taskDetail.deployDependsOnHint")}
                      </p>
                    ) : (
                      <div className="space-y-1">
                        {deployDependencies.map((rel) => {
                          const depColumn = columnByTaskId.get(rel.target_task_id);
                          return (
                            <div
                              key={rel.id}
                              className="flex items-center justify-between gap-2 rounded-md border border-border px-3 py-2 text-sm"
                            >
                              <span className="font-mono">{rel.target_key ?? rel.target_task_id}</span>
                              <Badge variant={depColumn === "released" ? "success" : "outline"}>
                                {depColumn ? columnLabel(depColumn, columns) : t("boardArea.components.taskDetail.deployDependencyUnknown")}
                              </Badge>
                            </div>
                          );
                        })}
                      </div>
                    )}
                  </div>

                  <div className="space-y-2">
                    <div className="flex items-center justify-between">
                      <Label>{t("boardArea.components.taskDetail.beforeDeploy")}</Label>
                      <Button
                        variant="ghost"
                        size="sm"
                        onClick={() => {
                          if (editingBeforeDeploy) saveBeforeDeploy();
                          else setEditingBeforeDeploy(true);
                        }}
                        disabled={saving}
                      >
                        {editingBeforeDeploy ? t("common.save") : t("boardArea.components.taskDetail.edit")}
                      </Button>
                    </div>
                    {editingBeforeDeploy ? (
                      <MarkdownField value={beforeDeployDraft} onChange={setBeforeDeployDraft} rows={3} />
                    ) : task.before_deploy ? (
                      <MarkdownContent content={task.before_deploy} />
                    ) : (
                      <p className="text-xs text-muted-foreground">
                        {t("boardArea.components.taskDetail.beforeDeployHint")}
                      </p>
                    )}
                  </div>

                  <div className="space-y-2">
                    <div className="flex items-center justify-between">
                      <Label>{t("boardArea.components.taskDetail.afterDeploy")}</Label>
                      <Button
                        variant="ghost"
                        size="sm"
                        onClick={() => {
                          if (editingAfterDeploy) saveAfterDeploy();
                          else setEditingAfterDeploy(true);
                        }}
                        disabled={saving}
                      >
                        {editingAfterDeploy ? t("common.save") : t("boardArea.components.taskDetail.edit")}
                      </Button>
                    </div>
                    {editingAfterDeploy ? (
                      <MarkdownField value={afterDeployDraft} onChange={setAfterDeployDraft} rows={3} />
                    ) : task.after_deploy ? (
                      <MarkdownContent content={task.after_deploy} />
                    ) : (
                      <p className="text-xs text-muted-foreground">
                        {t("boardArea.components.taskDetail.afterDeployHint")}
                      </p>
                    )}
                  </div>

                  <div className="space-y-2">
                    <div className="flex items-center justify-between">
                      <Label>{t("boardArea.components.taskDetail.rollbackPlan")}</Label>
                      <Button
                        variant="ghost"
                        size="sm"
                        onClick={() => {
                          if (editingRollback) saveRollbackPlan();
                          else setEditingRollback(true);
                        }}
                        disabled={saving}
                      >
                        {editingRollback ? t("common.save") : t("boardArea.components.taskDetail.edit")}
                      </Button>
                    </div>
                    {editingRollback ? (
                      <MarkdownField value={rollbackDraft} onChange={setRollbackDraft} rows={3} />
                    ) : task.rollback_plan ? (
                      <MarkdownContent content={task.rollback_plan} />
                    ) : (
                      <p className="text-xs text-muted-foreground">
                        {t("boardArea.components.taskDetail.rollbackPlanHint")}
                      </p>
                    )}
                  </div>
                </section>

                {otherRelations.length > 0 && (
                  <section className="space-y-2">
                    <Label className="text-muted-foreground">{t("boardArea.components.taskDetail.relations")}</Label>
                    <div className="space-y-1">
                      {otherRelations.map((rel) => (
                        <div key={rel.id} className="rounded-md border border-border px-3 py-2 text-sm">
                          <span className="text-muted-foreground">{rel.relation_type}</span>
                          {" → "}
                          <span className="font-mono">{rel.target_key ?? rel.target_task_id}</span>
                        </div>
                      ))}
                    </div>
                  </section>
                )}

                <Separator />

                <section className="space-y-3">
                  <Label className="flex items-center gap-2 text-muted-foreground">
                    <FileText className="h-3.5 w-3.5" />
                    {t("boardArea.components.taskDetail.documents", { count: documents.length })}
                  </Label>
                  <TaskDocumentList
                    documents={documents}
                    agentNameMap={agentNameMap}
                    onDelete={deleteDocument}
                  />
                  <div className="space-y-2 rounded-lg border border-dashed border-border p-3">
                    <Input
                      value={newDocTitle}
                      onChange={(e) => setNewDocTitle(e.target.value)}
                      placeholder={t("boardArea.components.taskDetail.docTitlePlaceholder")}
                    />
                    <MarkdownField
                      value={newDocContent}
                      onChange={setNewDocContent}
                      placeholder={t("boardArea.components.taskDetail.docContentPlaceholder")}
                      rows={3}
                    />
                    <Button size="sm" className="gap-1" onClick={addDocument} disabled={addingDoc || !newDocTitle.trim()}>
                      {addingDoc ? <Loader2 className="h-3.5 w-3.5 animate-spin" /> : <Plus className="h-3.5 w-3.5" />}
                      {t("boardArea.components.taskDetail.addDoc")}
                    </Button>
                  </div>
                </section>

                <Separator />

                <section className="space-y-3">
                  <Label className="flex items-center gap-2 text-muted-foreground">
                    <Paperclip className="h-3.5 w-3.5" />
                    {t("boardArea.components.taskDetail.attachments", { count: attachments.length })}
                  </Label>
                  <AttachmentList attachments={attachments} onRemove={unlinkAttachment} />
                  <AttachmentDropzone
                    repositoryId={repositoryId}
                    onUploaded={(metas) => void linkUploadedAttachments(metas)}
                  />
                </section>

                <Separator />

                <section className="space-y-3">
                  <Label className="text-muted-foreground">{t("boardArea.components.taskDetail.agentRuns", { count: runs.length })}</Label>
                  {agentTokenTotals.length > 0 && (
                    <div className="space-y-1 rounded-lg border border-border bg-muted/10 px-3 py-2">
                      <p className="text-[11px] font-medium text-muted-foreground">{t("boardArea.components.taskDetail.tokenUsage")}</p>
                      {agentTokenTotals.map(([agentId, tally]) => (
                        <div key={agentId} className="flex flex-wrap items-baseline justify-between gap-x-2 text-[11px]">
                          <span className="font-medium">
                            {agents.find((a) => a.id === agentId)?.name ?? t("boardArea.components.taskDetail.agentFallback")}
                          </span>
                          <span className="text-muted-foreground">{tokenLine(tally)}</span>
                        </div>
                      ))}
                    </div>
                  )}
                  {runs.length === 0 ? (
                    <p className="text-xs text-muted-foreground">{t("boardArea.components.taskDetail.noRuns")}</p>
                  ) : (
                    <div className="space-y-2">
                      {runs.map((r) => {
                        const agentName = agents.find((a) => a.id === r.agent_id)?.name ?? t("boardArea.components.taskDetail.agentFallback");
                        const active = selectedRunId === r.session_run_id;
                        const busy = runActionId === r.id;
                        const canStop = STOPPABLE_RUN_STATUSES.has(r.status);
                        // A blocked task is waiting on the human, so handing it a
                        // fresh run would only bounce off the server's gate.
                        const canRerun = !task.blocked_at && RERUNNABLE_RUN_STATUSES.has(r.status);
                        return (
                          <div
                            key={r.id}
                            className={cn(
                              "flex items-start gap-1 rounded-lg border transition-colors",
                              active ? "border-primary bg-primary/5" : "border-border hover:bg-muted/30",
                            )}
                          >
                            <button
                              type="button"
                              className="min-w-0 flex-1 px-3 py-2.5 text-left"
                              onClick={() => setSelectedRunId(r.session_run_id ?? null)}
                              disabled={!r.session_run_id}
                            >
                              <div className="flex items-center justify-between gap-2">
                                <span className="text-sm font-medium">{agentName}</span>
                                <Badge variant={runStatusVariant(r.status)}>{r.status}</Badge>
                              </div>
                              {r.summary && (
                                <p className="mt-1 line-clamp-2 text-xs text-muted-foreground">{r.summary}</p>
                              )}
                              <p className="mt-1 text-[11px] text-muted-foreground">{formatRelativeDate(r.created_at)}</p>
                              {(r.prompt_tokens ?? 0) + (r.completion_tokens ?? 0) > 0 && (
                                <p className="mt-1 text-[11px] text-muted-foreground">
                                  {tokenLine({
                                    prompt: r.prompt_tokens ?? 0,
                                    completion: r.completion_tokens ?? 0,
                                    cacheRead: r.cache_read_tokens ?? 0,
                                    cacheWrite: r.cache_write_tokens ?? 0,
                                  })}
                                </p>
                              )}
                            </button>
                            {canStop && (
                              <Button
                                variant="ghost"
                                size="sm"
                                className="mt-1.5 mr-1.5 shrink-0 gap-1"
                                disabled={busy}
                                onClick={(e) => {
                                  e.stopPropagation();
                                  setStopRunId(r.id);
                                }}
                              >
                                {busy ? (
                                  <Loader2 className="h-3.5 w-3.5 animate-spin" />
                                ) : (
                                  <CircleStop className="h-3.5 w-3.5" />
                                )}
                                {t("boardArea.components.taskDetail.runStop")}
                              </Button>
                            )}
                            {canRerun && (
                              <Button
                                variant="ghost"
                                size="sm"
                                className="mt-1.5 mr-1.5 shrink-0 gap-1"
                                disabled={busy}
                                onClick={(e) => {
                                  e.stopPropagation();
                                  rerunRun(r.id);
                                }}
                              >
                                {busy ? (
                                  <Loader2 className="h-3.5 w-3.5 animate-spin" />
                                ) : (
                                  <RotateCcw className="h-3.5 w-3.5" />
                                )}
                                {t("boardArea.components.taskDetail.runRerun")}
                              </Button>
                            )}
                          </div>
                        );
                      })}
                    </div>
                  )}
                  {selectedRunId && (
                    <div className="space-y-3 rounded-lg border border-border bg-muted/10 p-3">
                      {runIsLive && liveSummary && (
                        <Badge variant="warning" className="gap-1">
                          <Loader2 className="h-3 w-3 animate-spin" />
                          {liveSummary}
                        </Badge>
                      )}
                      {runActivityLoading && !runSteps.length ? (
                        <div className="flex items-center gap-2 text-xs text-muted-foreground">
                          <Loader2 className="h-3.5 w-3.5 animate-spin" />
                          {t("boardArea.components.taskDetail.runLoading")}
                        </div>
                      ) : (
                        <>
                          <ActivityPanel
                            embedded
                            activeRuns={runIsLive && selectedSessionRun ? [selectedSessionRun] : []}
                            runs={selectedSessionRun ? [selectedSessionRun] : []}
                            selectedRunId={selectedRunId}
                            onSelectRun={setSelectedRunId}
                            steps={runSteps}
                            plan={runPlan}
                            isLive={runIsLive}
                          />
                          {runPlan && (
                            <div className="rounded-lg border border-border bg-card p-3">
                              <p className="mb-2 text-xs font-medium text-muted-foreground">{t("boardArea.components.taskDetail.orchestrationPlan")}</p>
                              <PlanView
                                plan={runPlan}
                                agentNameMap={agentNameMap}
                                activityByTaskKey={runSubtaskActivity}
                              />
                            </div>
                          )}
                        </>
                      )}
                    </div>
                  )}
                </section>

                <Separator />

                <section className="space-y-3">
                  <Label className="flex items-center gap-2 text-muted-foreground">
                    <GitBranch className="h-3.5 w-3.5" />
                    {t("boardArea.components.taskDetail.pipeline")}
                  </Label>
                  <PipelineSection repositoryId={repositoryId} taskId={task.id} />
                </section>

                <Separator />

                <section className="space-y-3">
                  <Label className="flex items-center gap-2 text-muted-foreground">
                    <History className="h-3.5 w-3.5" />
                    {t("boardArea.components.taskHistory.title")}
                  </Label>
                  <TaskHistory
                    repositoryId={repositoryId}
                    taskId={task.id}
                    columns={columns}
                    agentNameMap={agentNameMap}
                  />
                </section>
              </div>
            </ScrollArea>

            {/* Sidebar: the task's properties and its conversation. Fields sit
                on top Jira-style; the comment thread gets the rest of the
                column so it is visible without scrolling past the content. */}
            <div className="flex min-h-0 flex-1 flex-col border-t border-border lg:max-w-sm lg:shrink-0 lg:border-t-0 lg:border-l">
              <ScrollArea className="min-h-0 flex-1">
                <div className="space-y-5 px-5 py-5">
                  <section className="grid grid-cols-2 gap-3">
                    <div className="space-y-1.5">
                      <Label className="text-xs text-muted-foreground">{t("boardArea.components.taskDetail.status")}</Label>
                      <Select value={task.column} onValueChange={(v) => handleColumn(v as TaskColumn)} disabled={saving}>
                        <SelectTrigger>
                          <SelectValue />
                        </SelectTrigger>
                        <SelectContent>
                          {columns.map((col) => (
                            <SelectItem key={col.slug} value={col.slug}>
                              {col.label}
                            </SelectItem>
                          ))}
                        </SelectContent>
                      </Select>
                    </div>
                    <div className="space-y-1.5">
                      <Label className="text-xs text-muted-foreground">{t("boardArea.components.taskDetail.type")}</Label>
                      <Select value={task.task_type} onValueChange={(v) => handleType(v as TaskType)} disabled={saving}>
                        <SelectTrigger>
                          <SelectValue />
                        </SelectTrigger>
                        <SelectContent>
                          {TASK_TYPE_OPTIONS.map((o) => (
                            <SelectItem key={o.value} value={o.value}>
                              {t(o.labelKey)}
                            </SelectItem>
                          ))}
                        </SelectContent>
                      </Select>
                    </div>
                    <div className="space-y-1.5">
                      <Label className="text-xs text-muted-foreground">{t("boardArea.components.taskDetail.priority")}</Label>
                      <Select
                        value={task.priority}
                        onValueChange={(v) => handlePriority(v as TaskPriority)}
                        disabled={saving}
                      >
                        <SelectTrigger>
                          <SelectValue />
                        </SelectTrigger>
                        <SelectContent>
                          {TASK_PRIORITY_OPTIONS.map((o) => (
                            <SelectItem key={o.value} value={o.value}>
                              {t(o.labelKey)}
                            </SelectItem>
                          ))}
                        </SelectContent>
                      </Select>
                    </div>
                    {initiativeProjects.length > 0 && (
                      <div className="space-y-1.5">
                        <Label className="text-xs text-muted-foreground">{t("boardArea.components.taskDetail.project")}</Label>
                        <Select
                          value={task.initiative_project_id ?? "none"}
                          onValueChange={handleInitiative}
                          disabled={saving}
                        >
                          <SelectTrigger>
                            <SelectValue />
                          </SelectTrigger>
                          <SelectContent>
                            <SelectItem value="none">{t("boardArea.components.taskDetail.none")}</SelectItem>
                            {initiativeProjects.map((p) => (
                              <SelectItem key={p.id} value={p.id}>
                                {p.name}
                              </SelectItem>
                            ))}
                          </SelectContent>
                        </Select>
                      </div>
                    )}
                    <div className="col-span-2">
                      <TaskAssigneeFields
                        agents={memberAgents}
                        agentValue={task.assignee_agent_id}
                        onAgentChange={handleAssignee}
                        agentFallbackName={assigneeName}
                        disabled={saving}
                      />
                    </div>
                  </section>

                  {/* The pull request lives HERE, with the task's other
                      properties, and not in the content column beside the
                      pipeline. It is a fact about the card — the same kind of
                      thing as its status or its assignee — and putting it in
                      the sidebar is also what lets the agents stop repeating
                      the link in comments: there is one place to look for it.
                      The link opens in a new tab in a plain browser; inside
                      the desktop shell, handlePrClick intercepts the click and
                      routes it through the bridge instead of letting the
                      anchor's own navigation run — that native path is what
                      used to leave the shell stuck on its loading screen. */}
                  {task.pr_url && (
                    <section className="space-y-2">
                      <Label className="flex items-center gap-2 text-xs text-muted-foreground">
                        <GitPullRequest className="h-3.5 w-3.5" />
                        {t("boardArea.components.taskDetail.pullRequest")}
                      </Label>
                      <a
                        href={task.pr_url}
                        target="_blank"
                        rel="noreferrer"
                        onClick={handlePrClick}
                        className="inline-flex items-center gap-1 text-sm text-primary hover:underline"
                      >
                        <ExternalLink className="h-3 w-3" />
                        {task.pr_number
                          ? t("boardArea.components.taskDetail.pullRequestNumber", { number: task.pr_number })
                          : t("boardArea.components.taskDetail.pullRequestOpen")}
                      </a>
                      {/* The merge is shown, not commented: the merge tool
                          records the commit on the task and this is where a
                          person reads it back. */}
                      {task.merge_commit_sha && (
                        <p className="text-xs text-muted-foreground">
                          {t("boardArea.components.taskDetail.pullRequestMerged", {
                            sha: task.merge_commit_sha.slice(0, 12),
                          })}
                        </p>
                      )}
                    </section>
                  )}

                  {/* Talking to the agent about this task — and about the pull
                      request opened for it — never depends on a parked question
                      or a finished run, so the action is always on offer. */}
                  <Button
                    size="sm"
                    variant="outline"
                    className="w-full gap-2"
                    onClick={discussTask}
                    disabled={openingChat}
                    title={t("boardArea.components.taskDetail.discussHint")}
                  >
                    {openingChat ? (
                      <Loader2 className="h-3.5 w-3.5 animate-spin" />
                    ) : (
                      <MessagesSquare className="h-3.5 w-3.5" />
                    )}
                    {openingChat
                      ? t("boardArea.components.taskDetail.discussOpening")
                      : t("boardArea.components.taskDetail.discuss")}
                  </Button>

                  <Separator />

                  <section className="space-y-3">
                    <Label className="flex items-center gap-2 text-muted-foreground">
                      <MessageSquare className="h-3.5 w-3.5" />
                      {t("boardArea.components.taskDetail.comments", { count: comments.length })}
                    </Label>
                    <div className="space-y-2">
                      {comments.length === 0 ? (
                        <p className="rounded-lg border border-dashed border-border px-2 py-4 text-center text-xs text-muted-foreground">{t("boardArea.components.taskDetail.noComments")}</p>
                      ) : (
                        comments.map((c) => {
                          const isAgent = c.author_type === "agent";
                          // System comments (verification bounce, clarification
                          // answer, gate block) are not the human's words —
                          // showing them as "User" made the board claim the
                          // human wrote a build report they never saw.
                          const isSystem = c.author_type === "system";
                          const authorName = isAgent
                            ? c.author_name ?? agentNameMap[c.author_id] ?? t("boardArea.components.taskDetail.agentFallback")
                            : isSystem
                              ? t("boardArea.components.taskDetail.systemAuthor")
                              : t("boardArea.components.taskDetail.userAuthor");
                          return (
                            <div key={c.id} className="rounded-md border border-border bg-card px-3 py-2 shadow-sm">
                              <div className="flex items-center gap-2 text-xs text-muted-foreground">
                                {isAgent ? <Bot className="h-3 w-3" /> : isSystem ? <Cog className="h-3 w-3" /> : <User className="h-3 w-3" />}
                                <span className="font-medium text-foreground">{authorName}</span>
                                {isAgent && (
                                  <Badge variant="outline" className="h-4 px-1 text-[10px] font-normal">
                                    {t("boardArea.components.taskDetail.agentFallback")}
                                  </Badge>
                                )}
                                <span>·</span>
                                <span>{formatRelativeDate(c.created_at)}</span>
                              </div>
                              <MarkdownContent content={c.content} className="mt-1.5 text-sm leading-relaxed" />
                            </div>
                          );
                        })
                      )}
                    </div>
                    <MarkdownField
                      value={commentText}
                      onChange={setCommentText}
                      placeholder={t("boardArea.components.taskDetail.commentPlaceholder")}
                      rows={3}
                    />
                    <Button
                      size="sm"
                      onClick={handleComment}
                      disabled={submittingComment || !commentText.trim()}
                      className="gap-2"
                    >
                      {submittingComment && <Loader2 className="h-3.5 w-3.5 animate-spin" />}
                      {t("boardArea.components.taskDetail.addComment")}
                    </Button>
                  </section>
                </div>
              </ScrollArea>
            </div>
            </div>
          )}
        </DialogContent>
      </Dialog>

      <ConfirmDialog
        open={stopRunId !== null}
        onOpenChange={(o) => !o && setStopRunId(null)}
        title={t("boardArea.components.taskDetail.runStopTitle")}
        description={t("boardArea.components.taskDetail.runStopDescription")}
        confirmLabel={t("boardArea.components.taskDetail.runStopConfirm")}
        loading={stopRunId !== null && runActionId === stopRunId}
        onConfirm={stopRun}
      />
    </>
  );
}

type TranslateFn = (key: string, params?: Record<string, string | number>) => string;

// TestCaseList renders the round, not a checklist: nothing here is toggled from
// the UI, because a verdict is only worth reading if the thing that executed the
// case wrote it. Order is failed → not run → passed → invalid, so the two rows
// that need a person come first and the shelf of passing cases sits below them.
const testCaseStatusOrder: TestCaseStatus[] = ["failed", "planned", "skipped", "passed", "invalid"];

function TestCaseList({ items, t }: { items: TaskTestCase[]; t: TranslateFn }) {
  if (items.length === 0) {
    return <p className="text-xs text-muted-foreground">{t("boardArea.components.taskDetail.noTestCases")}</p>;
  }
  const counts = {
    passed: items.filter((c) => c.status === "passed").length,
    failed: items.filter((c) => c.status === "failed").length,
    skipped: items.filter((c) => c.status === "skipped" || c.status === "planned").length,
    invalid: items.filter((c) => c.status === "invalid").length,
  };
  const sorted = [...items].sort(
    (a, b) => testCaseStatusOrder.indexOf(a.status) - testCaseStatusOrder.indexOf(b.status) || a.position - b.position,
  );
  return (
    <div className="space-y-2">
      <p className="text-xs text-muted-foreground">
        {t("boardArea.components.taskDetail.testCaseSummary", counts)}
      </p>
      {sorted.map((c) => (
        <div key={c.id} className="rounded-md border border-border px-3 py-2">
          <div className="flex items-start justify-between gap-2">
            <span className={cn("text-sm", c.status === "invalid" && "text-muted-foreground line-through")}>
              {c.title}
            </span>
            <div className="flex shrink-0 items-center gap-1">
              <Badge variant="outline" className="text-[10px] text-muted-foreground">
                {t(`boardArea.components.taskDetail.testCaseCategory${pascal(c.category)}`)}
              </Badge>
              <Badge variant={testCaseBadgeVariant(c.status)} className="text-[10px]">
                {t(`boardArea.components.taskDetail.testCaseStatus${pascal(c.status)}`)}
              </Badge>
            </div>
          </div>
          {c.expected && (
            <p className="mt-1.5 text-xs text-muted-foreground">
              {t("boardArea.components.taskDetail.testCaseExpected")}: {c.expected}
            </p>
          )}
          {c.actual && (
            <p className="mt-1 text-xs text-destructive">
              {t("boardArea.components.taskDetail.testCaseActual")}: {c.actual}
            </p>
          )}
          {c.notes && (
            <p className="mt-1 text-xs text-muted-foreground">
              {t("boardArea.components.taskDetail.testCaseNotes")}: {c.notes}
            </p>
          )}
          {c.evidence && (
            <p className="mt-1 whitespace-pre-wrap break-words font-mono text-[11px] text-muted-foreground">
              {t("boardArea.components.taskDetail.testCaseEvidence")}: {c.evidence}
            </p>
          )}
        </div>
      ))}
    </div>
  );
}

function testCaseBadgeVariant(status: TestCaseStatus): "default" | "secondary" | "destructive" | "outline" {
  switch (status) {
    case "passed":
      return "default";
    case "failed":
      return "destructive";
    case "planned":
      return "secondary";
    default:
      return "outline";
  }
}

// pascal turns a snake_case enum value into the suffix its locale key uses
// (happy_path → HappyPath), so the status and category labels stay one key each
// instead of a switch per enum.
function pascal(value: string): string {
  return value
    .split("_")
    .map((part) => part.charAt(0).toUpperCase() + part.slice(1))
    .join("");
}

function roleLabel(role: CriterionReviewRole, t: TranslateFn): string {
  return role === "qa"
    ? t("boardArea.components.taskDetail.checkRoleQa")
    : t("boardArea.components.taskDetail.checkRolePm");
}

// The developer's own tick, rendered as a badge in the same row as QA and PM.
// The checkbox alone read as a personal to-do marker, so a criterion could sit
// unticked with nobody noticing — and an unticked criterion is exactly what
// makes the board refuse the automatic hand-off to code review. As a badge next
// to the two review verdicts it reads as what it is: the first of three states
// the criterion has to pass through.
function CriterionDevBadge({ completed, t }: { completed: boolean; t: TranslateFn }) {
  const label = t("boardArea.components.taskDetail.checkRoleDev");
  if (completed) {
    return (
      <Badge variant="success" className="gap-1" title={t("boardArea.components.taskDetail.checkDevDone")}>
        <Check className="h-3 w-3" />
        {label}
      </Badge>
    );
  }
  return (
    <Badge
      variant="outline"
      className="gap-1 text-muted-foreground"
      title={t("boardArea.components.taskDetail.checkDevPending")}
    >
      <Minus className="h-3 w-3" />
      {label}
    </Badge>
  );
}

// The implementer's checkbox is a claim; the QA and PM verdicts render next to
// it separately, so the card tells who has actually verified each criterion.
function CriterionCheckBadge({
  role,
  checks,
  t,
}: {
  role: CriterionReviewRole;
  checks?: CriterionCheck[];
  t: TranslateFn;
}) {
  const check = (checks ?? []).find((ch) => ch.role === role);
  const label = roleLabel(role, t);
  if (!check) {
    return (
      <Badge
        variant="outline"
        className="gap-1 text-muted-foreground"
        title={t("boardArea.components.taskDetail.checkPending", { role: label })}
      >
        <Minus className="h-3 w-3" />
        {label}
      </Badge>
    );
  }
  if (check.approved) {
    return (
      <Badge variant="success" className="gap-1" title={t("boardArea.components.taskDetail.checkApproved", { role: label })}>
        <Check className="h-3 w-3" />
        {label}
      </Badge>
    );
  }
  return (
    <Badge
      variant="destructive"
      className="gap-1"
      title={check.note || t("boardArea.components.taskDetail.checkRejected", { role: label })}
    >
      <X className="h-3 w-3" />
      {label}
    </Badge>
  );
}
