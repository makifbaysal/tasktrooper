import { Activity, FolderKanban, Inbox, Plus } from "lucide-react";
import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { Link, useNavigate, useSearchParams } from "react-router-dom";
import { toast } from "sonner";
import {
  api,
  type Agent,
  type BoardColumn,
  type BoardTask,
  type InitiativeProject,
  type Release,
  type Repository,
  type TaskColumn,
  type TaskTypeWorkflow,
  type WorkspaceConfig,
} from "@/api";
import { BoardLane } from "@/components/board/BoardLane";
import { BoardTaskCard } from "@/components/board/BoardTaskCard";
import { CreateTaskDialog } from "@/components/board/CreateTaskDialog";
import { ProjectScopeSelect } from "@/components/board/ProjectScopeSelect";
import { TaskDetailDrawer } from "@/components/board/TaskDetailDrawer";
import { NoRepositoriesNotice } from "@/components/workspace/NoProjectsNotice";
import { ActivityFeed } from "@/components/workspace/ActivityFeed";
import { PageHeader } from "@/components/admin/PageHeader";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card } from "@/components/ui/card";
import { Dialog, DialogContent, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import { EmptyState } from "@/components/ui/empty-state";
import { Skeleton } from "@/components/ui/skeleton";
import { useCachedState, useFirstLoad } from "@/hooks/useCachedState";
import { tStatic, useI18n } from "@/hooks/useI18n";
import { usePolling } from "@/hooks/usePolling";
import { useProjectScope } from "@/hooks/useProjectScope";
import { ACTIVITY_POLL, ALL_TASKS_POLL, useSharedPoll } from "@/hooks/useSharedPoll";
import {
  CACHE_AGENTS,
  CACHE_CONFIG,
  CACHE_PROJECTS,
  CACHE_REPOS,
  CACHE_TASKS,
  CACHE_WORKFLOWS,
  PROJECT_SCOPE_ALL,
  PROJECT_SCOPE_NONE,
  boardColumnsSplit,
  boardLanes,
  boardPollIntervals,
  filterTasksByScope,
  formatColumnAge,
  isProjectScope,
  mergeTaskList,
  projectScopeCounts,
  scopeShowingTask,
  taskCreateDefaults,
} from "@/lib/project-board";
import { BOARD_RELEASE_STATUSES, openReleaseByTask, verifyMinutesLeft } from "@/lib/release-board";
import { keepMap } from "@/lib/stableState";

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

// A release moves on the sweeper's 30s tick, so polling it as often as the
// cards would only repeat the same answer.
const RELEASE_POLL_MS = 15000;
// Cards are memoized and get the badge text, not this clock, so a tick
// re-renders only the cards whose column age or verify countdown now reads
// differently.
const CLOCK_TICK_MS = 60000;

const releaseBadgeKey = (release: Release) => `${release.id}:${release.status}:${release.verify_until ?? ""}`;

// The agent-activity poll runs every 2s during a run; handing React a fresh Set
// each time re-rendered the whole board — and an open task drawer with it — on
// every tick, even when nothing had changed.
function sameIds(a: Set<string>, b: Set<string>): boolean {
  if (a.size !== b.size) return false;
  for (const id of a) if (!b.has(id)) return false;
  return true;
}

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
  const [workflows, setWorkflows] = useCachedState<TaskTypeWorkflow[]>(CACHE_WORKFLOWS, []);
  const [loading, setLoading] = useFirstLoad(CACHE_TASKS, CACHE_CONFIG);
  const [dialogOpen, setDialogOpen] = useState(false);
  const [defaultRepositoryId, setDefaultRepositoryId] = useState("");
  const [dragTaskId, setDragTaskId] = useState<string | null>(null);
  const [dropColumn, setDropColumn] = useState<string | null>(null);
  const [selectedTask, setSelectedTask] = useState<BoardTask | null>(null);
  const [drawerOpen, setDrawerOpen] = useState(false);
  const [activeAgentTaskIds, setActiveAgentTaskIds] = useState<Set<string>>(new Set());
  const [releasesByTask, setReleasesByTask] = useState<Map<string, Release>>(new Map());
  const [activityOpen, setActivityOpen] = useState(false);
  const [fetched, setFetched] = useState(false);
  const [searchParams, setSearchParams] = useSearchParams();
  const navigate = useNavigate();
  const { scope, setScope } = useProjectScope(initiativeProjects);
  const projectScoped = isProjectScope(scope);

  // Keep the open drawer's task in sync with the latest board data: after an
  // edit (e.g. assigning an agent) load() refetches tasks, and the selected
  // task must be re-derived from the fresh list, otherwise the drawer shows
  // stale values until it is closed and reopened.
  useEffect(() => {
    setSelectedTask((prev) =>
      prev ? tasks.find((task) => task.id === prev.id) ?? prev : prev,
    );
  }, [tasks]);

  // A notification-center click (or the add-repository flow's "Open task")
  // lands here as `?task=<id>` — open that task's drawer once the board's own
  // task list has loaded, then drop the param so the URL doesn't keep
  // re-triggering it. A board painted from the cache can predate the task, so
  // a miss only counts once the first fetch is in; a task that still does not
  // exist (released/deleted since the link was made) then just clears
  // silently. A task outside the current project scope takes the scope with
  // it: the drawer must not open over a board its card is filtered out of.
  // Handled once per param: the navigation that drops it lands in a
  // transition, and a second pass over the same id in between would race it.
  const handledTaskParam = useRef<string | null>(null);
  useEffect(() => {
    const taskId = searchParams.get("task");
    if (!taskId) {
      handledTaskParam.current = null;
      return;
    }
    if (loading || handledTaskParam.current === taskId) return;
    const found = tasks.find((task) => task.id === taskId);
    if (!found && !fetched) return;
    handledTaskParam.current = taskId;
    if (found) {
      setSelectedTask(found);
      setDrawerOpen(true);
      const revealing = scopeShowingTask(found, scope, repositories);
      if (revealing) {
        setScope(revealing, ["task"]);
        return;
      }
    }
    setSearchParams(
      (prev) => {
        const next = new URLSearchParams(prev);
        next.delete("task");
        return next;
      },
      { replace: true },
    );
  }, [searchParams, setSearchParams, tasks, loading, fetched, scope, setScope, repositories]);

  const openTaskCreate = () => {
    if (repositories.length === 0) {
      navigate("/projects/new");
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
  // The shared poll forgets its cached list too, or a remount would be handed it.
  const boardVersion = useRef(0);
  const localChange = () => {
    boardVersion.current += 1;
    ALL_TASKS_POLL.invalidate();
  };

  const load = useCallback(async () => {
    const seen = boardVersion.current;
    const [taskData, cfg, repoData, projectData, agentData, workflowData] = await Promise.allSettled([
      api.listAllTasks(),
      api.getWorkspaceConfig(),
      api.listRepositories(),
      api.listInitiativeProjects(),
      api.listAgents(),
      api.listWorkflows(),
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
    if (workflowData.status === "fulfilled") setWorkflows(workflowData.value.workflows ?? []);

    const firstFailure = [taskData, cfg, repoData, projectData, agentData].find(
      (r) => r.status === "rejected",
    );
    if (firstFailure?.status === "rejected") {
      const reason = firstFailure.reason;
      toast.error(reason instanceof Error ? reason.message : tStatic("boardArea.board.loadFailed"));
    }
    setLoading(false);
    setFetched(true);
  }, [
    setTasks,
    setConfig,
    setRepositories,
    setInitiativeProjects,
    setAgents,
    setWorkflows,
    setLoading,
    setDefaultRepositoryId,
  ]);

  useEffect(() => {
    load();
  }, [load]);

  const pollIntervals = useMemo(
    () => boardPollIntervals(tasks, activeAgentTaskIds),
    [tasks, activeAgentTaskIds],
  );

  // The cheap half of load(): just the cards. The board is a shared surface:
  // agents move cards on their own, and this poll is how such a move shows up
  // without a reload. Shared with the header's notifications, so the two never
  // fetch the list twice. A blip keeps the last good board; the next tick (or
  // any user action) surfaces a real failure.
  useSharedPoll(ALL_TASKS_POLL, pollIntervals.tasksMs, !loading, {
    begin: () => boardVersion.current,
    onValue: (data, seen) => {
      if (boardVersion.current !== seen) return;
      setTasks((prev) => mergeTaskList(prev, data.tasks ?? []));
    },
  });

  useSharedPoll(ACTIVITY_POLL, pollIntervals.activityMs, !loading, {
    onValue: (data) => {
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
      setActiveAgentTaskIds((prev) => (sameIds(prev, ids) ? prev : ids));
    },
    onError: () => setActiveAgentTaskIds((prev) => (prev.size === 0 ? prev : new Set())),
  });

  const pollReleases = useCallback(async () => {
    try {
      const data = await api.listAllReleases({ statuses: BOARD_RELEASE_STATUSES, limit: 200 });
      const next = openReleaseByTask(data.releases ?? []);
      setReleasesByTask((prev) => keepMap(prev, next, releaseBadgeKey));
    } catch {
      // The badge is a hint; a failed poll keeps the last one.
    }
  }, []);

  usePolling(pollReleases, RELEASE_POLL_MS, !loading);

  const agentName = useCallback(
    (id?: string) => (id ? agents.find((a) => a.id === id)?.name : undefined),
    [agents],
  );

  // Per task type, the column slugs its workflow has a stage for — undefined
  // for a type with no curated stages at all (unscoped, matches the server's
  // own gate: every column stays a valid target). Mirrors validateStageConfigured
  // (server/internal/application/repository/service.go) client-side so a
  // doomed drag never even highlights before the server would reject it.
  const allowedColumnsByType = useMemo(() => {
    const map = new Map<string, Set<string>>();
    for (const wf of workflows) {
      if (wf.stages.length === 0) continue;
      map.set(wf.task_type, new Set(wf.stages.map((s) => s.column_slug)));
    }
    return map;
  }, [workflows]);

  const draggedTask = dragTaskId ? tasks.find((task) => task.id === dragTaskId) : undefined;
  const draggedAllowedColumns = draggedTask ? allowedColumnsByType.get(draggedTask.task_type) : undefined;
  const invalidStages = useMemo(() => {
    if (!draggedAllowedColumns) return undefined;
    const invalid = new Set<string>();
    for (const col of columns) {
      if (!draggedAllowedColumns.has(col.slug)) invalid.add(col.slug);
    }
    return invalid;
  }, [columns, draggedAllowedColumns]);

  const boardColumnTasks = useMemo(() => {
    const slugs = new Set(board.map((col) => col.slug));
    return tasks.filter((task) => slugs.has(task.column));
  }, [tasks, board]);

  const scopeCounts = useMemo(
    () => projectScopeCounts(boardColumnTasks, repositories),
    [boardColumnTasks, repositories],
  );

  const scopedTasks = useMemo(
    () => filterTasksByScope(boardColumnTasks, scope, repositories),
    [boardColumnTasks, scope, repositories],
  );

  const boardTasks = useMemo(() => {
    const grouped: Record<string, BoardTask[]> = {};
    for (const col of board) grouped[col.slug] = [];
    for (const task of scopedTasks) grouped[task.column].push(task);
    for (const col of board) {
      grouped[col.slug].sort((a, b) => a.position - b.position);
    }
    return grouped;
  }, [scopedTasks, board]);

  const createDefaults = useMemo(
    () => taskCreateDefaults(repositories, scope, defaultRepositoryId),
    [repositories, scope, defaultRepositoryId],
  );

  // The card lands in its new column on the drop, not a round-trip later. The
  // request still decides: the server answers with the task as it actually
  // stands (a review gate can refuse the move and hand back the old column),
  // and that answer replaces the optimistic one. A failure puts the card back.
  const moveTask = async (task: BoardTask, column: TaskColumn) => {
    if (task.column === column) return;
    localChange();
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
      localChange();
      // The PATCH answers with the task itself; the pipeline digest is added by
      // the list endpoint only, so carry the card's own over rather than
      // blanking its build icon until the next poll.
      setTasks((list) => list.map((item) => (item.id === updated.id ? withPipelineFrom(item, updated) : item)));
      // Everything else the move touched (pipeline status, assignee, the other
      // cards' positions) catches up in the background — the board is already
      // showing the result.
      void load();
    } catch (e) {
      localChange();
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
    localChange();
    const previous = tasks;
    setTasks((list) => list.filter((item) => item.id !== task.id));
    try {
      await api.deleteRepositoryTask(task.repository_id, task.id);
      localChange();
      void load();
      toast.success(t("boardArea.board.taskDeleted"));
    } catch (e) {
      localChange();
      setTasks(previous);
      toast.error(e instanceof Error ? e.message : t("boardArea.board.deleteFailed"));
    }
  };

  const openTask = useCallback((task: BoardTask) => {
    setSelectedTask(task);
    setDrawerOpen(true);
  }, []);
  const endDrag = useCallback(() => {
    setDragTaskId(null);
    setDropColumn(null);
  }, []);
  const deleteTaskRef = useRef(deleteTask);
  deleteTaskRef.current = deleteTask;
  const deleteCard = useCallback((task: BoardTask) => void deleteTaskRef.current(task), []);

  const [now, setNow] = useState(() => Date.now());
  usePolling(() => setNow(Date.now()), CLOCK_TICK_MS, true);

  const memberIds = useMemo(() => agents.filter((a) => a.enabled).map((a) => a.id), [agents]);
  const memberList = useMemo(() => memberIds.map((id) => ({ agent_id: id })), [memberIds]);

  const onDrop = (column: TaskColumn) => {
    if (!dragTaskId) return;
    const task = tasks.find((item) => item.id === dragTaskId);
    // BoardLane already refuses the drop when the zone is marked invalid, but
    // a task can be dropped via other paths (tests, future keyboard support),
    // so the same check is re-applied here as the real guard — the server's
    // own gate is still the source of truth either way.
    if (task && (!draggedAllowedColumns || draggedAllowedColumns.has(column))) {
      moveTask(task, column);
    }
    setDragTaskId(null);
    setDropColumn(null);
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

  return (
    <div className="flex h-full min-h-0 flex-1 flex-col">
      <div className="shrink-0 px-6 pt-6">
        <PageHeader
          title={t("boardArea.board.title")}
          action={
            <div className="flex flex-wrap gap-2">
              <ProjectScopeSelect
                projects={initiativeProjects}
                value={scope}
                onChange={setScope}
                counts={scopeCounts}
              />
              <Button variant="outline" className="gap-2" onClick={() => setActivityOpen(true)}>
                <Activity className="h-4 w-4" />
                {t("boardArea.board.activity")}
                {activeAgentTaskIds.size > 0 && (
                  <Badge variant="warning" className="h-5 min-w-5 justify-center px-1.5 text-micro">
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
          <NoRepositoriesNotice className="mb-4 border-amber-500/30 bg-amber-500/5 p-4" />
        )}
        {scope !== PROJECT_SCOPE_ALL && scopedTasks.length === 0 ? (
          <Card className="border-dashed">
            <EmptyState
              icon={FolderKanban}
              title={
                scope === PROJECT_SCOPE_NONE
                  ? t("boardArea.projectScope.emptyNoneTitle")
                  : t("boardArea.projectScope.emptyTitle")
              }
              description={t("boardArea.projectScope.boardEmptyHint")}
              action={
                <Button onClick={openTaskCreate} className="gap-2">
                  <Plus className="h-4 w-4" />
                  {t("boardArea.board.newTask")}
                </Button>
              }
            />
          </Card>
        ) : (
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
                  invalidStages={invalidStages}
                  renderStage={(stage) => {
                    const stageTasks = boardTasks[stage.slug] ?? [];
                    return stageTasks.length === 0 ? (
                      <p className="px-2 py-6 text-center text-xs text-muted-foreground">
                        {t("boardArea.board.emptyColumn")}
                      </p>
                    ) : (
                      stageTasks.map((task) => {
                        const release = task.column === "done" ? releasesByTask.get(task.id) : undefined;
                        return (
                          <BoardTaskCard
                            key={task.id}
                            task={task}
                            repositoryName={repositoryName(task.repository_id)}
                            assignee={agentName(task.assignee_agent_id)}
                            initiative={projectScoped ? undefined : initiativeName(task.initiative_project_id)}
                            agentRunning={task.agent_running === true || activeAgentTaskIds.has(task.id)}
                            release={release}
                            dragging={dragTaskId === task.id}
                            columnAge={task.column_entered_at ? formatColumnAge(task.column_entered_at, now) : undefined}
                            releaseVerifyMinutes={release ? verifyMinutesLeft(release, now) : undefined}
                            onDragStart={setDragTaskId}
                            onDragEnd={endDrag}
                            onOpen={openTask}
                            onDelete={deleteCard}
                          />
                        );
                      })
                    );
                  }}
                />
              ))}
            </div>
          </div>
        )}
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
            pollMs={pollIntervals.activityMs}
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
          repositories={createDefaults.repositories}
          initiativeProjects={initiativeProjects}
          columns={allColumns}
          agents={agents}
          memberAgentIds={memberIds}
          defaultRepositoryId={createDefaults.repositoryId}
          defaultInitiativeProjectId={createDefaults.initiativeProjectId}
          defaultColumn="todo"
          onCreated={load}
        />
      )}
    </div>
  );
}
