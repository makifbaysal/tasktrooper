import { Archive, ArrowRight, Inbox, Plus, Trash2 } from "lucide-react";
import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { Link, useNavigate } from "react-router-dom";
import { toast } from "sonner";
import {
  api,
  type Agent,
  type BoardTask,
  type InitiativeProject,
  type Repository,
  type TaskColumn,
  type WorkspaceConfig,
} from "@/api";
import { CreateTaskDialog } from "@/components/board/CreateTaskDialog";
import { TaskDetailDrawer } from "@/components/board/TaskDetailDrawer";
import { NoRepositoriesNotice } from "@/components/workspace/NoProjectsNotice";
import { PageHeader } from "@/components/admin/PageHeader";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card } from "@/components/ui/card";
import { EmptyState } from "@/components/ui/empty-state";
import { ScrollArea } from "@/components/ui/scroll-area";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Skeleton } from "@/components/ui/skeleton";
import { useCachedState, useFirstLoad } from "@/hooks/useCachedState";
import { useI18n } from "@/hooks/useI18n";
import { usePolling } from "@/hooks/usePolling";
import {
  CACHE_AGENTS,
  CACHE_CONFIG,
  CACHE_PROJECTS,
  CACHE_REPOS,
  CACHE_TASKS,
  boardColumnsSplit,
  mergeTaskList,
  taskPriorityLabel,
  taskTypeLabel,
} from "@/lib/project-board";
import { cn, formatRelativeDate } from "@/lib/utils";

// Slower than the board's own poll: the backlog changes when a human puts
// something in it, not while an agent works.
const TASK_POLL_MS = 8000;

export function BacklogPage() {
  const { t } = useI18n();
  // Same cached payloads the board renders (see BoardPage): switching between
  // the two pages shows data at once and refreshes behind it.
  const [tasks, setTasks] = useCachedState<BoardTask[]>(CACHE_TASKS, []);
  const [repositories, setRepositories] = useCachedState<Repository[]>(CACHE_REPOS, []);
  const [initiativeProjects, setInitiativeProjects] = useCachedState<InitiativeProject[]>(
    CACHE_PROJECTS,
    [],
  );
  const [config, setConfig] = useCachedState<WorkspaceConfig | null>(CACHE_CONFIG, null);
  const [agents, setAgents] = useCachedState<Agent[]>(CACHE_AGENTS, []);
  const [loading, setLoading] = useFirstLoad(CACHE_TASKS, CACHE_CONFIG);
  const [selectedId, setSelectedId] = useState<string | null>(null);
  const [moving, setMoving] = useState(false);
  // Drag-to-move: the panel on the right is a drop target for the first board
  // column, so the common case (backlog -> todo) is one gesture instead of
  // click, read the panel, pick a column.
  const [dragTaskId, setDragTaskId] = useState<string | null>(null);
  const [dropActive, setDropActive] = useState(false);
  const [dialogOpen, setDialogOpen] = useState(false);
  const [drawerOpen, setDrawerOpen] = useState(false);
  const [defaultRepositoryId, setDefaultRepositoryId] = useState("");
  const [projectFilter, setProjectFilter] = useState("all");
  const navigate = useNavigate();

  const openTaskCreate = () => {
    if (repositories.length === 0) {
      navigate("/projects/new");
      return;
    }
    setDialogOpen(true);
  };

  const columns = config?.columns ?? [];
  const { backlog, board } = useMemo(() => boardColumnsSplit(columns), [columns]);
  const backlogSlug = backlog?.slug ?? "backlog";
  // Where a dropped card lands. `todo` by name when the board has it (every
  // default board does); otherwise the first board column, which is the one a
  // custom board starts with. Never the backlog itself.
  const dropColumn = useMemo(
    () => board.find((col) => col.slug === "todo") ?? board[0],
    [board],
  );

  const repositoryName = useCallback(
    (id: string) => repositories.find((r) => r.id === id)?.name ?? t("boardArea.backlog.repoFallback"),
    [repositories, t],
  );

  // Bumped by every local change, so a refresh already in flight when a task
  // was moved out of the backlog is discarded instead of putting it back.
  const backlogVersion = useRef(0);

  // No setLoading(true) here: a refresh after a move (or on a poll tick) must
  // not tear the page down to a skeleton — the list is already on screen and
  // the new data replaces it in place. The skeleton belongs to the very first
  // visit only, which is what useFirstLoad decides.
  const load = useCallback(async () => {
    const seen = backlogVersion.current;
    try {
      const [taskData, cfg, repoData, projectData, agentData] = await Promise.all([
        api.listAllTasks(),
        api.getWorkspaceConfig(),
        api.listRepositories(),
        api.listInitiativeProjects(),
        api.listAgents(),
      ]);
      if (backlogVersion.current === seen) {
        setTasks((prev) => mergeTaskList(prev, taskData.tasks ?? []));
      }
      setConfig(cfg);
      const list = repoData.repositories ?? [];
      setRepositories(list);
      setInitiativeProjects(projectData.projects ?? []);
      if (list.length > 0) {
        setDefaultRepositoryId((prev) => prev || list[0].id);
      }
      setAgents(agentData.agents ?? []);
    } catch (e) {
      toast.error(e instanceof Error ? e.message : t("boardArea.backlog.loadFailed"));
    } finally {
      setLoading(false);
    }
  }, [t, setTasks, setConfig, setRepositories, setInitiativeProjects, setAgents, setLoading]);

  useEffect(() => {
    load();
  }, [load]);

  const refreshTasks = useCallback(async () => {
    const seen = backlogVersion.current;
    try {
      const data = await api.listAllTasks();
      if (backlogVersion.current !== seen) return;
      setTasks((prev) => mergeTaskList(prev, data.tasks ?? []));
    } catch {
      /* keep the last good list; a poll blip is not worth a toast */
    }
  }, [setTasks]);

  usePolling(refreshTasks, TASK_POLL_MS, !loading);

  const backlogTasks = useMemo(
    () =>
      tasks
        .filter((task) => task.column === backlogSlug)
        .filter((task) => {
          if (projectFilter === "all") return true;
          if (projectFilter === "none") return !task.initiative_project_id;
          return task.initiative_project_id === projectFilter;
        })
        .sort((a, b) => a.position - b.position),
    [tasks, backlogSlug, projectFilter],
  );

  // Looked up against every task, not just the backlog slice: changing the
  // column from inside the detail drawer drops the task out of `backlogTasks`,
  // which would unmount the drawer mid-edit.
  const selectedTask = useMemo(
    () => tasks.find((task) => task.id === selectedId) ?? null,
    [tasks, selectedId],
  );

  const openTask = (task: BoardTask) => {
    setSelectedId(task.id);
    setDrawerOpen(true);
  };

  // The card leaves the backlog on the click and the full refresh runs behind
  // it. Nothing here waits on the board's five reloads, which is what used to
  // make "move to board" feel like it had hung.
  const moveToBoard = async (task: BoardTask, column: TaskColumn) => {
    setMoving(true);
    backlogVersion.current += 1;
    const previousColumn = task.column;
    setTasks((list) =>
      list.map((item) => (item.id === task.id ? { ...item, column } : item)),
    );
    setSelectedId(null);
    try {
      const updated = await api.updateRepositoryTask(task.repository_id, task.id, { column });
      backlogVersion.current += 1;
      setTasks((list) =>
        list.map((item) =>
          item.id === updated.id
            ? {
                ...updated,
                // Filled in by the list endpoint only — see BoardPage.
                latest_pipeline_status: updated.latest_pipeline_status ?? item.latest_pipeline_status,
                latest_pipeline_gate_reason:
                  updated.latest_pipeline_gate_reason ?? item.latest_pipeline_gate_reason,
              }
            : item,
        ),
      );
      toast.success(t("boardArea.backlog.movedToBoard", { key: task.key }));
      void load();
    } catch (e) {
      backlogVersion.current += 1;
      setTasks((list) =>
        list.map((item) => (item.id === task.id ? { ...item, column: previousColumn } : item)),
      );
      toast.error(e instanceof Error ? e.message : t("boardArea.backlog.moveFailed"));
    } finally {
      setMoving(false);
    }
  };

  const onDropTask = () => {
    setDropActive(false);
    const task = tasks.find((item) => item.id === dragTaskId);
    setDragTaskId(null);
    if (!task || !dropColumn) return;
    void moveToBoard(task, dropColumn.slug as TaskColumn);
  };

  const deleteTask = async (task: BoardTask) => {
    backlogVersion.current += 1;
    const previous = tasks;
    setTasks((list) => list.filter((item) => item.id !== task.id));
    if (selectedId === task.id) {
      setDrawerOpen(false);
      setSelectedId(null);
    }
    try {
      await api.deleteRepositoryTask(task.repository_id, task.id);
      backlogVersion.current += 1;
      void load();
      toast.success(t("boardArea.backlog.taskDeleted"));
    } catch (e) {
      backlogVersion.current += 1;
      setTasks(previous);
      toast.error(e instanceof Error ? e.message : t("boardArea.backlog.deleteFailed"));
    }
  };

  if (loading) {
    return (
      <div className="flex h-full min-h-0 flex-1 gap-4 p-6">
        <Skeleton className="h-full flex-1 rounded-xl" />
        <Skeleton className="hidden h-full w-80 rounded-xl lg:block" />
      </div>
    );
  }

  const memberIds = agents.filter((a) => a.enabled).map((a) => a.id);
  const memberList = memberIds.map((id) => ({ agent_id: id }));

  return (
    <div className="flex h-full min-h-0 flex-1 flex-col">
      <div className="shrink-0 border-b border-border px-6 py-4">
        <PageHeader
          title={t("boardArea.backlog.title")}
          description={t("boardArea.backlog.description")}
          action={
            <div className="flex flex-wrap gap-2">
              {initiativeProjects.length > 0 && (
                <Select value={projectFilter} onValueChange={setProjectFilter}>
                  <SelectTrigger className="w-44">
                    <SelectValue />
                  </SelectTrigger>
                  <SelectContent>
                    <SelectItem value="all">{t("boardArea.backlog.filterAllProjects")}</SelectItem>
                    <SelectItem value="none">{t("boardArea.backlog.filterNoProject")}</SelectItem>
                    {initiativeProjects.map((project) => (
                      <SelectItem key={project.id} value={project.id}>
                        {project.name}
                      </SelectItem>
                    ))}
                  </SelectContent>
                </Select>
              )}
              <Button variant="outline" asChild className="gap-2">
                <Link to="/released">
                  <Archive className="h-4 w-4" />
                  {t("boardArea.backlog.releasedArchive")}
                </Link>
              </Button>
              <Button variant="outline" asChild className="gap-2">
                <Link to="/board">
                  <ArrowRight className="h-4 w-4" />
                  {t("boardArea.backlog.board")}
                </Link>
              </Button>
              <Button onClick={openTaskCreate} className="gap-2">
                <Plus className="h-4 w-4" />
                {t("boardArea.backlog.newTask")}
              </Button>
            </div>
          }
        />
      </div>

      <div className="flex min-h-0 flex-1 overflow-hidden">
        <ScrollArea className="min-h-0 flex-1">
          <div className="p-6">
            {repositories.length === 0 && (
              <NoRepositoriesNotice className="mb-6 border-amber-500/30 bg-amber-500/5 p-4" />
            )}
            {backlogTasks.length === 0 ? (
              <Card className="border-dashed">
                <EmptyState
                  icon={Inbox}
                  title={t("boardArea.backlog.emptyTitle")}
                  description={
                    repositories.length === 0
                      ? t("boardArea.backlog.emptyNoRepo")
                      : t("boardArea.backlog.emptyWithRepo")
                  }
                  action={
                    <Button onClick={openTaskCreate}>
                      {repositories.length === 0
                        ? t("boardArea.backlog.addRepo")
                        : t("boardArea.backlog.addTask")}
                    </Button>
                  }
                  className="py-16"
                />
              </Card>
            ) : (
              <div className="grid gap-3 sm:grid-cols-2 xl:grid-cols-3">
                {backlogTasks.map((task) => (
                  <Card
                    key={task.id}
                    draggable
                    onDragStart={(e) => {
                      e.dataTransfer.effectAllowed = "move";
                      setDragTaskId(task.id);
                    }}
                    onDragEnd={() => {
                      setDragTaskId(null);
                      setDropActive(false);
                    }}
                    className={cn(
                      "cursor-grab p-4 transition-all hover:border-primary/30 hover:shadow-[var(--shadow-overlay)] active:cursor-grabbing",
                      selectedId === task.id && "border-primary ring-2 ring-primary/20",
                      dragTaskId === task.id && "opacity-50 ring-2 ring-primary/30",
                    )}
                    onClick={() => openTask(task)}
                  >
                    <div className="flex items-start justify-between gap-2">
                      <div className="flex flex-wrap gap-1">
                        <Badge variant="outline" className="font-mono text-micro">
                          {task.key}
                        </Badge>
                        <Badge variant="secondary" className="text-micro">
                          {taskTypeLabel(task.task_type)}
                        </Badge>
                        <Badge variant="outline" className="text-micro">
                          {taskPriorityLabel(task.priority)}
                        </Badge>
                      </div>
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
                    <h3 className="mt-2 font-semibold leading-snug">{task.title}</h3>
                    {task.description && (
                      <p className="mt-1 line-clamp-2 text-sm text-muted-foreground">{task.description}</p>
                    )}
                    <Badge variant="outline" className="mt-2 text-micro">
                      {repositoryName(task.repository_id)}
                    </Badge>
                    <p className="mt-3 text-micro text-muted-foreground">
                      {formatRelativeDate(task.updated_at)}
                    </p>
                  </Card>
                ))}
              </div>
            )}
          </div>
        </ScrollArea>

        <aside
          onDragOver={(e) => {
            if (!dragTaskId || !dropColumn) return;
            // Without preventDefault the browser refuses the drop outright.
            e.preventDefault();
            e.dataTransfer.dropEffect = "move";
            setDropActive(true);
          }}
          onDragLeave={(e) => {
            // Moving over a child fires dragleave on the panel; only a pointer
            // that actually left the panel's box counts.
            if (e.currentTarget.contains(e.relatedTarget as Node | null)) return;
            setDropActive(false);
          }}
          onDrop={(e) => {
            e.preventDefault();
            onDropTask();
          }}
          className={cn(
            "relative flex w-full shrink-0 flex-col border-l border-border bg-muted/10 lg:w-80",
            !selectedTask && "hidden lg:flex",
            dragTaskId && "border-primary/40",
            dropActive && "bg-primary/5 ring-2 ring-inset ring-primary/40",
          )}
        >
          {/* Covers the panel while a card is in the air: the buttons behind it
              are unreachable mid-drag anyway, and the target has to say which
              column the drop means. */}
          {dragTaskId && dropColumn && (
            <div className="absolute inset-0 z-10 flex items-center justify-center rounded-none border-2 border-dashed border-primary/50 bg-background/80 p-6 text-center text-sm font-medium text-primary">
              {t("boardArea.backlog.dropToColumn", { column: dropColumn.label })}
            </div>
          )}
          {selectedTask ? (
            <>
              <div className="border-b border-border p-4">
                <div className="flex flex-wrap gap-1">
                  <Badge variant="outline" className="font-mono text-micro">
                    {selectedTask.key}
                  </Badge>
                  <Badge variant="secondary" className="text-micro">
                    {taskTypeLabel(selectedTask.task_type)}
                  </Badge>
                </div>
                <h2 className="mt-2 font-semibold leading-snug">{selectedTask.title}</h2>
                {selectedTask.description && (
                  <p className="mt-2 line-clamp-4 text-sm text-muted-foreground">{selectedTask.description}</p>
                )}
                <Badge variant="secondary" className="mt-3">
                  {repositoryName(selectedTask.repository_id)}
                </Badge>
              </div>

              <div className="flex-1 space-y-4 overflow-y-auto p-4">
                <div>
                  <p className="mb-2 text-xs font-medium tracking-wide text-muted-foreground uppercase">
                    {t("boardArea.backlog.moveToBoardLabel")}
                  </p>
                  <div className="flex flex-col gap-2">
                    {board.map((col) => (
                      <Button
                        key={col.slug}
                        variant="outline"
                        className="justify-start"
                        disabled={moving}
                        onClick={() => moveToBoard(selectedTask, col.slug as TaskColumn)}
                      >
                        <ArrowRight className="mr-2 h-4 w-4 shrink-0" />
                        {col.label}
                      </Button>
                    ))}
                  </div>
                </div>

                <Button variant="secondary" className="w-full" onClick={() => setDrawerOpen(true)}>
                  {t("boardArea.backlog.detailsAndComments")}
                </Button>
              </div>

              <div className="border-t border-border p-4 lg:hidden">
                <Button variant="ghost" className="w-full" onClick={() => setSelectedId(null)}>
                  {t("boardArea.backlog.close")}
                </Button>
              </div>
            </>
          ) : (
            <div className="flex flex-1 items-center justify-center p-6 text-center text-sm text-muted-foreground">
              {t("boardArea.backlog.selectPrompt", { column: dropColumn?.label ?? "" })}
            </div>
          )}
        </aside>
      </div>

      {config && selectedTask && (
        <TaskDetailDrawer
          open={drawerOpen}
          onOpenChange={setDrawerOpen}
          repositoryId={selectedTask.repository_id}
          task={selectedTask}
          columns={columns}
          members={memberList}
          agents={agents}
          initiativeProjects={initiativeProjects}
          repositories={repositories}
          onUpdated={load}
        />
      )}

      {config && (
        <CreateTaskDialog
          open={dialogOpen}
          onOpenChange={setDialogOpen}
          repositories={repositories}
          initiativeProjects={initiativeProjects}
          columns={columns}
          agents={agents}
          memberAgentIds={memberIds}
          defaultRepositoryId={defaultRepositoryId}
          defaultColumn={backlogSlug}
          title={t("boardArea.backlog.createDialogTitle")}
          onCreated={load}
        />
      )}
    </div>
  );
}
