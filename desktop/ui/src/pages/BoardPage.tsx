import { Activity, Bot, Clock, GripVertical, HelpCircle, Inbox, Loader2, Plus, Trash2 } from "lucide-react";
import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { Link } from "react-router-dom";
import { toast } from "sonner";
import {
  api,
  type Agent,
  type BoardColumn,
  type BoardTask,
  type InitiativeProject,
  type Repository,
  type TaskColumn,
  type WorkspaceConfig,
} from "@/api";
import { BoardLane } from "@/components/board/BoardLane";
import { CreateTaskDialog } from "@/components/board/CreateTaskDialog";
import { TaskDetailDrawer } from "@/components/board/TaskDetailDrawer";
import { NoRepositoriesNotice } from "@/components/workspace/NoProjectsNotice";
import { ActivityFeed } from "@/components/workspace/ActivityFeed";
import { PageHeader } from "@/components/admin/PageHeader";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card } from "@/components/ui/card";
import { Dialog, DialogContent, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import { Skeleton } from "@/components/ui/skeleton";
import { useCachedState, useFirstLoad } from "@/hooks/useCachedState";
import { tStatic, useI18n } from "@/hooks/useI18n";
import { usePolling } from "@/hooks/usePolling";
import {
  CACHE_AGENTS,
  CACHE_CONFIG,
  CACHE_PROJECTS,
  CACHE_REPOS,
  CACHE_TASKS,
  blockedResourceLabel,
  boardColumnsSplit,
  boardLanes,
  formatResumeIn,
  mergeTaskList,
  pipelineGateReasonLabel,
  taskPipelineCardIcon,
  taskPriorityLabel,
  taskTypeLabel,
  workOrderBlockerLabel,
} from "@/lib/project-board";
import { cn, formatDate } from "@/lib/utils";

// Coarse on purpose: the badge answers "is this stuck?", and a minute-accurate
// figure on a card that re-renders on every poll only adds noise.
const formatColumnAge = (iso: string): string => {
  const minutes = Math.max(0, Math.round((Date.now() - new Date(iso).getTime()) / 60000));
  if (minutes < 60) return `${minutes}m`;
  const hours = Math.round(minutes / 60);
  return hours < 48 ? `${hours}h` : `${Math.round(hours / 24)}d`;
};

// The board's own view of a card carries two fields the single-task endpoints
// never return (they are filled in by the list query). Keep them when a
// mutation's response replaces the card.
function withPipelineFrom(previous: BoardTask, updated: BoardTask): BoardTask {
  return {
    ...updated,
    latest_pipeline_status: updated.latest_pipeline_status ?? previous.latest_pipeline_status,
    latest_pipeline_gate_reason:
      updated.latest_pipeline_gate_reason ?? previous.latest_pipeline_gate_reason,
  };
}

// The board is a shared surface: agents move cards on their own, and until this
// poll existed the only way to see that was to reload the page.
const TASK_POLL_MS = 5000;

export function BoardPage() {
  const { t } = useI18n();
  // Cached across navigations: coming back to the board paints the last known
  // cards immediately and refreshes behind them, instead of showing the
  // full-page skeleton on every click through the sidebar.
  const [tasks, setTasks] = useCachedState<BoardTask[]>(CACHE_TASKS, []);
  const [repositories, setRepositories] = useCachedState<Repository[]>(CACHE_REPOS, []);
  const [initiativeProjects, setInitiativeProjects] = useCachedState<InitiativeProject[]>(
    CACHE_PROJECTS,
    [],
  );
  const [config, setConfig] = useCachedState<WorkspaceConfig | null>(CACHE_CONFIG, null);
  const [agents, setAgents] = useCachedState<Agent[]>(CACHE_AGENTS, []);
  const [loading, setLoading] = useFirstLoad(CACHE_TASKS, CACHE_CONFIG);
  const [dialogOpen, setDialogOpen] = useState(false);
  const [defaultRepositoryId, setDefaultRepositoryId] = useState("");
  const [dragTaskId, setDragTaskId] = useState<string | null>(null);
  const [dropColumn, setDropColumn] = useState<string | null>(null);
  const [selectedTask, setSelectedTask] = useState<BoardTask | null>(null);
  const [drawerOpen, setDrawerOpen] = useState(false);
  const [repositoryAutoOpen, setRepositoryAutoOpen] = useState<"create" | "open" | null>(null);
  const [activeAgentTaskIds, setActiveAgentTaskIds] = useState<Set<string>>(new Set());
  const [activityOpen, setActivityOpen] = useState(false);

  // Keep the open drawer's task in sync with the latest board data: after an
  // edit (e.g. assigning an agent) load() refetches tasks, and the selected
  // task must be re-derived from the fresh list, otherwise the drawer shows
  // stale values until it is closed and reopened.
  useEffect(() => {
    setSelectedTask((prev) =>
      prev ? tasks.find((task) => task.id === prev.id) ?? prev : prev,
    );
  }, [tasks]);

  const openTaskCreate = () => {
    if (repositories.length === 0) {
      setRepositoryAutoOpen("create");
      return;
    }
    setDialogOpen(true);
  };

  const columns = config?.columns ?? [];
  const { board } = useMemo(() => boardColumnsSplit(columns), [columns]);
  // Lanes, not columns, are what the board renders: the pairing lives in
  // BOARD_STACKED_LANES (lib/project-board) and everything else keeps a lane of
  // its own, so a custom column still shows up.
  const lanes = useMemo(() => boardLanes(board), [board]);

  const repositoryName = useCallback(
    (id: string) => repositories.find((r) => r.id === id)?.name ?? t("boardArea.board.repoFallback"),
    [repositories, t],
  );

  const initiativeName = useCallback(
    (id?: string) =>
      id ? initiativeProjects.find((p) => p.id === id)?.name : undefined,
    [initiativeProjects],
  );

  // Settled per call, not all-or-nothing: a hiccup in any one of these used to
  // leave config null, which silently disabled the New Task dialog and rendered
  // a columnless board. Each slice now keeps its last good value instead.
  //
  // The full-page skeleton is shown only until the first load resolves.
  // Refreshes (after a drag, a delete, a drawer edit) must not unmount the
  // board, or the open task drawer is torn down and its unsaved input is lost.
  // Every local change to a card bumps this. A refresh that was already in
  // flight when it happened is discarded rather than applied, so a poll (or the
  // full reload behind a move) cannot put a card back where it was dragged from.
  const boardVersion = useRef(0);

  const load = useCallback(async () => {
    const seen = boardVersion.current;
    const [taskData, cfg, repoData, projectData, agentData] = await Promise.allSettled([
      api.listAllTasks(),
      api.getWorkspaceConfig(),
      api.listRepositories(),
      api.listInitiativeProjects(),
      api.listAgents(),
    ]);
    // Cards only: a local change made while this was in flight (a second drag,
    // a delete) wins over what the server said before it happened.
    if (taskData.status === "fulfilled" && boardVersion.current === seen) {
      setTasks((prev) => mergeTaskList(prev, taskData.value.tasks ?? []));
    }
    if (cfg.status === "fulfilled") setConfig(cfg.value);
    if (repoData.status === "fulfilled") {
      const list = repoData.value.repositories ?? [];
      setRepositories(list);
      if (list.length > 0) {
        setDefaultRepositoryId((prev) => prev || list[0].id);
      }
    }
    if (projectData.status === "fulfilled") setInitiativeProjects(projectData.value.projects ?? []);
    if (agentData.status === "fulfilled") setAgents(agentData.value.agents ?? []);

    const firstFailure = [taskData, cfg, repoData, projectData, agentData].find(
      (r) => r.status === "rejected",
    );
    if (firstFailure?.status === "rejected") {
      const reason = firstFailure.reason;
      toast.error(reason instanceof Error ? reason.message : tStatic("boardArea.board.loadFailed"));
    }
    setLoading(false);
  }, [setTasks, setConfig, setRepositories, setInitiativeProjects, setAgents, setLoading, setDefaultRepositoryId]);

  useEffect(() => {
    load();
  }, [load]);

  // The cheap half of load(): just the cards. Runs on a timer so a move an
  // agent (or another browser) made shows up without a reload.
  const refreshTasks = useCallback(async () => {
    const seen = boardVersion.current;
    try {
      const data = await api.listAllTasks();
      if (boardVersion.current !== seen) return;
      setTasks((prev) => mergeTaskList(prev, data.tasks ?? []));
    } catch {
      // A blip on a background poll keeps the last good board; the next tick
      // (or any user action) surfaces a real failure.
    }
  }, [setTasks]);

  usePolling(refreshTasks, TASK_POLL_MS, !loading);

  const pollAgentActivity = useCallback(async () => {
    try {
      const data = await api.listActivity(100);
      const ids = new Set<string>();
      for (const item of data.items ?? []) {
        // Only a run that has actually started. A pending run is still waiting
        // to be claimed and nothing is happening on the card yet, so
        // badging it "agent running" made the board claim work it was not
        // doing — and hid the real reason the task was sitting still.
        if (item.kind === "agent_run" && item.task_id && item.status === "running") {
          ids.add(item.task_id);
        }
      }
      setActiveAgentTaskIds(ids);
    } catch {
      setActiveAgentTaskIds(new Set());
    }
  }, []);

  usePolling(pollAgentActivity, 2000, !loading);

  const agentName = useCallback(
    (id?: string) => (id ? agents.find((a) => a.id === id)?.name : undefined),
    [agents],
  );

  const boardTasks = useMemo(() => {
    const grouped: Record<string, BoardTask[]> = {};
    for (const col of board) grouped[col.slug] = [];
    for (const task of tasks) {
      if (grouped[task.column]) grouped[task.column].push(task);
    }
    for (const col of board) {
      grouped[col.slug].sort((a, b) => a.position - b.position);
    }
    return grouped;
  }, [tasks, board]);

  // The card lands in its new column on the drop, not a round-trip later. The
  // request still decides: the server answers with the task as it actually
  // stands (a review gate can refuse the move and hand back the old column),
  // and that answer replaces the optimistic one. A failure puts the card back.
  const moveTask = async (task: BoardTask, column: TaskColumn) => {
    if (task.column === column) return;
    boardVersion.current += 1;
    const previousColumn = task.column;
    const previousEnteredAt = task.column_entered_at;
    setTasks((list) =>
      list.map((item) =>
        item.id === task.id
          ? { ...item, column, column_entered_at: new Date().toISOString() }
          : item,
      ),
    );
    try {
      const updated = await api.updateRepositoryTask(task.repository_id, task.id, { column });
      boardVersion.current += 1;
      // The PATCH answers with the task itself; the pipeline digest is added by
      // the list endpoint only, so carry the card's own over rather than
      // blanking its build icon until the next poll.
      setTasks((list) => list.map((item) => (item.id === updated.id ? withPipelineFrom(item, updated) : item)));
      // Everything else the move touched (pipeline status, assignee, the other
      // cards' positions) catches up in the background — the board is already
      // showing the result.
      void load();
    } catch (e) {
      boardVersion.current += 1;
      setTasks((list) =>
        list.map((item) =>
          item.id === task.id
            ? { ...item, column: previousColumn, column_entered_at: previousEnteredAt }
            : item,
        ),
      );
      toast.error(e instanceof Error ? e.message : t("boardArea.board.moveFailed"));
    }
  };

  const deleteTask = async (task: BoardTask) => {
    boardVersion.current += 1;
    const previous = tasks;
    setTasks((list) => list.filter((item) => item.id !== task.id));
    try {
      await api.deleteRepositoryTask(task.repository_id, task.id);
      boardVersion.current += 1;
      void load();
      toast.success(t("boardArea.board.taskDeleted"));
    } catch (e) {
      boardVersion.current += 1;
      setTasks(previous);
      toast.error(e instanceof Error ? e.message : t("boardArea.board.deleteFailed"));
    }
  };

  const openTask = (task: BoardTask) => {
    setSelectedTask(task);
    setDrawerOpen(true);
  };

  const onDrop = (column: TaskColumn) => {
    if (!dragTaskId) return;
    const task = tasks.find((item) => item.id === dragTaskId);
    if (task) moveTask(task, column);
    setDragTaskId(null);
    setDropColumn(null);
  };

  // The clarification chat lives under the agent that asked, so both ids are
  // needed to link to it; an older blocked task may predate either.
  const blockedChatPath = (task: BoardTask): string | null =>
    task.blocked_session_id && task.assignee_agent_id
      ? `/agents/${task.assignee_agent_id}/chat/${task.blocked_session_id}`
      : null;

  const TaskCard = ({ task }: { task: BoardTask }) => {
    const assignee = agentName(task.assignee_agent_id);
    const initiative = initiativeName(task.initiative_project_id);
    const agentRunning = activeAgentTaskIds.has(task.id);
    const pipelineIcon = taskPipelineCardIcon(task.latest_pipeline_status, task.latest_pipeline_gate_reason);
    // A skipped gate explains itself; an ordinary pipeline just names its status.
    const pipelineGateNote = pipelineGateReasonLabel(task.latest_pipeline_gate_reason);
    return (
      <Card
        key={task.id}
        draggable
        onDragStart={() => setDragTaskId(task.id)}
        onDragEnd={() => {
          setDragTaskId(null);
          setDropColumn(null);
        }}
        onClick={() => openTask(task)}
        className={cn(
          "mb-2 cursor-grab overflow-hidden border-border/80 p-3 transition-shadow active:cursor-grabbing",
          dragTaskId === task.id && "opacity-50 ring-2 ring-primary/30",
          "hover:shadow-[var(--shadow-overlay)]",
        )}
      >
        <div className="flex items-start gap-2">
          <GripVertical className="mt-0.5 h-4 w-4 shrink-0 text-muted-foreground/60" />
          <div className="min-w-0 flex-1">
            <div className="mb-2 flex flex-wrap items-center gap-1">
              <Badge variant="outline" className="font-mono text-[10px]">
                {task.key}
              </Badge>
              {agentRunning && (
                <Badge variant="info" className="gap-1 text-[10px]">
                  <Loader2 className="h-3 w-3 animate-spin" />
                  {t("boardArea.board.agentRunning")}
                </Badge>
              )}
              <Badge variant="secondary" className="text-[10px]">
                {taskTypeLabel(task.task_type)}
              </Badge>
              <Badge variant="outline" className="text-[10px]">
                {taskPriorityLabel(task.priority)}
              </Badge>
            </div>
            <p className="break-words text-sm font-medium leading-snug">{task.title}</p>
            {task.description && (
              <p className="mt-1 line-clamp-2 break-words text-xs text-muted-foreground">{task.description}</p>
            )}
            <div className="mt-2.5 flex flex-wrap items-center gap-1.5">
              <Badge variant="outline" className="max-w-[9rem] truncate text-[10px]">
                {repositoryName(task.repository_id)}
              </Badge>
              {initiative && (
                <Badge variant="outline" className="max-w-[9rem] truncate text-[10px]">
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
              {task.blocked_resource && (
                <Badge
                  variant="outline"
                  className="gap-1 border-amber-500/40 bg-amber-500/10 text-[10px] text-amber-600 dark:text-amber-400"
                  title={
                    // Unlike every other resource, no sweeper ever releases a
                    // human_decision park — say so instead of promising a
                    // pickup that will never come.
                    task.blocked_resource === "human_decision"
                      ? t("boardArea.board.blockedHumanDecisionTitle", {
                          reason: task.blocked_question || blockedResourceLabel(task.blocked_resource),
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
                  {task.blocked_resource === "work_order"
                    ? workOrderBlockerLabel(task.blocked_question || "")
                    : blockedResourceLabel(task.blocked_resource)}
                  {task.blocked_resume_at ? ` · ~${formatResumeIn(task.blocked_resume_at)}` : ""}
                </Badge>
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
                      className="gap-1 border-amber-500/40 bg-amber-500/10 text-[10px] text-amber-600 hover:bg-amber-500/20 dark:text-amber-400"
                    >
                      <HelpCircle className="h-3 w-3" />
                      {t("boardArea.board.answerQuestion")}
                    </Badge>
                  </Link>
                ) : (
                  <Badge
                    variant="outline"
                    className="gap-1 border-amber-500/40 bg-amber-500/10 text-[10px] text-amber-600 dark:text-amber-400"
                    title={task.blocked_question || undefined}
                  >
                    <HelpCircle className="h-3 w-3" />
                    {t("boardArea.board.awaitingAnswer")}
                  </Badge>
                ))}
              {task.column_entered_at && (
                <Badge
                  variant="outline"
                  className="text-[10px]"
                  title={t("boardArea.board.columnAge", {
                    value: formatColumnAge(task.column_entered_at),
                  })}
                >
                  {formatColumnAge(task.column_entered_at)}
                </Badge>
              )}
            </div>
            <div className="mt-2 flex items-center justify-between gap-2">
              <div className="flex min-w-0 flex-wrap items-center gap-1.5">
                {assignee && (
                  <Badge variant="outline" className="gap-1 text-[10px]">
                    <Bot className="h-3 w-3" />
                    <span className="max-w-[6rem] truncate">{assignee}</span>
                  </Badge>
                )}
                {!assignee && (
                  <span className="text-[10px] text-muted-foreground">{task.created_by}</span>
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
                    deleteTask(task);
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
  };

  if (loading) {
    return (
      <div className="flex h-full min-h-0 flex-1 flex-col p-4">
        <Skeleton className="mb-4 h-10 w-64" />
        <Skeleton className="h-full min-h-0 flex-1 rounded-xl" />
      </div>
    );
  }

  const allColumns: BoardColumn[] = columns.length ? columns : [];
  const memberIds = agents.filter((a) => a.enabled).map((a) => a.id);
  const memberList = memberIds.map((id) => ({ agent_id: id }));

  return (
    <div className="flex h-full min-h-0 flex-1 flex-col">
      <div className="shrink-0 px-6 pt-6">
        <PageHeader
          title={t("boardArea.board.title")}
          description={t("boardArea.board.description")}
          action={
            <div className="flex flex-wrap gap-2">
              <Button variant="outline" className="gap-2" onClick={() => setActivityOpen(true)}>
                <Activity className="h-4 w-4" />
                {t("boardArea.board.activity")}
                {activeAgentTaskIds.size > 0 && (
                  <Badge variant="warning" className="h-5 min-w-5 justify-center px-1.5 text-[10px]">
                    {activeAgentTaskIds.size}
                  </Badge>
                )}
              </Button>
              <Button variant="outline" asChild className="gap-2">
                <Link to="/backlog">
                  <Inbox className="h-4 w-4" />
                  {t("boardArea.board.backlog")}
                </Link>
              </Button>
              <Button onClick={openTaskCreate} className="gap-2">
                <Plus className="h-4 w-4" />
                {t("boardArea.board.newTask")}
              </Button>
            </div>
          }
        />
      </div>

      <div className="flex min-h-0 flex-1 flex-col p-4 pt-2">
        {repositories.length === 0 && (
          <NoRepositoriesNotice
            className="mb-4 border-amber-500/30 bg-amber-500/5 p-4"
            autoOpen={repositoryAutoOpen}
            onAutoOpenHandled={() => setRepositoryAutoOpen(null)}
            onRepositoryAdded={load}
            onReadyForTask={() => setDialogOpen(true)}
          />
        )}
        <p className="mb-4 text-sm text-muted-foreground">
          {t("boardArea.board.summary", {
            tasks: tasks.filter((task) => board.some((c) => c.slug === task.column)).length,
            repos: repositories.length,
          })}
        </p>

        <div className="flex min-h-0 flex-1 overflow-x-auto pb-1">
          <div className="flex h-full min-h-0 min-w-max gap-3">
            {lanes.map((lane) => (
              <BoardLane
                key={lane.key}
                className="w-60"
                stages={lane.columns.map((column) => ({
                  slug: column.slug,
                  label: column.label,
                  count: (boardTasks[column.slug] ?? []).length,
                }))}
                dragging={dragTaskId !== null}
                dropColumn={dropColumn}
                onDropColumnChange={(slug) => setDropColumn((c) => (c === slug ? c : slug))}
                onDropTask={onDrop}
                renderStage={(stage) => {
                  const stageTasks = boardTasks[stage.slug] ?? [];
                  return stageTasks.length === 0 ? (
                    <p className="px-2 py-6 text-center text-xs text-muted-foreground">{t("boardArea.board.emptyColumn")}</p>
                  ) : (
                    stageTasks.map((task) => TaskCard({ task }))
                  );
                }}
              />
            ))}
          </div>
        </div>
      </div>

      {config && selectedTask && (
        <TaskDetailDrawer
          open={drawerOpen}
          onOpenChange={setDrawerOpen}
          repositoryId={selectedTask.repository_id}
          task={selectedTask}
          columns={allColumns}
          members={memberList}
          agents={agents}
          initiativeProjects={initiativeProjects}
          repositories={repositories}
          onUpdated={load}
        />
      )}

      <Dialog open={activityOpen} onOpenChange={setActivityOpen}>
        <DialogContent className="flex h-[80vh] max-w-lg flex-col gap-0 overflow-hidden p-0 pt-9">
          <DialogHeader className="sr-only">
            <DialogTitle>{t("boardArea.board.activityDialogTitle")}</DialogTitle>
          </DialogHeader>
          <ActivityFeed
            className="min-h-0 flex-1 rounded-none border-0"
            agents={agents}
            tasks={tasks}
            columns={allColumns}
          />
        </DialogContent>
      </Dialog>

      {config && (
        <CreateTaskDialog
          open={dialogOpen}
          onOpenChange={setDialogOpen}
          repositories={repositories}
          initiativeProjects={initiativeProjects}
          columns={allColumns}
          agents={agents}
          memberAgentIds={memberIds}
          defaultRepositoryId={defaultRepositoryId}
          defaultColumn="todo"
          onCreated={load}
        />
      )}
    </div>
  );
}
