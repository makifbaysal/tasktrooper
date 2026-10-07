import "@testing-library/jest-dom/vitest";
import { fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { MemoryRouter, Route, Routes, useLocation } from "react-router-dom";
import { beforeEach, describe, expect, it, vi } from "vitest";
import type { BoardColumn, BoardTask, InitiativeProject, Release, Repository, WorkspaceConfig } from "@/api";
import { I18nProvider } from "@/hooks/useI18n";
import { PROJECT_SCOPE_STORAGE_KEY } from "@/hooks/useProjectScope";
import { ACTIVITY_POLL, ALL_TASKS_POLL } from "@/hooks/useSharedPoll";
import {
  CACHE_AGENTS,
  CACHE_CONFIG,
  CACHE_PROJECTS,
  CACHE_REPOS,
  CACHE_TASKS,
  CACHE_WORKFLOWS,
} from "@/lib/project-board";
import { writeCache } from "@/lib/uiCache";
import { BoardPage } from "@/pages/BoardPage";

// jsdom has no layout, so Radix Select's scroll-into-view-on-open crashes without it.
if (!Element.prototype.scrollIntoView) {
  Element.prototype.scrollIntoView = () => {};
}

const api = vi.hoisted(() => ({
  listAllTasks: vi.fn(),
  getWorkspaceConfig: vi.fn(),
  listRepositories: vi.fn(),
  listInitiativeProjects: vi.fn(),
  listAgents: vi.fn(),
  listWorkflows: vi.fn(),
  listActivity: vi.fn(),
  listAllReleases: vi.fn(),
  listTaskTypes: vi.fn(),
  createRepositoryTask: vi.fn(),
}));

vi.mock("@/api", async () => {
  const actual = await vi.importActual<typeof import("@/api")>("@/api");
  return { ...actual, api: { ...actual.api, ...api } };
});

vi.mock("@/components/board/TaskDetailDrawer", () => ({
  TaskDetailDrawer: ({ open, task }: { open: boolean; task: BoardTask }) =>
    open ? <div data-testid="task-drawer">{task.key}</div> : null,
}));

const stamp = "2026-01-01T00:00:00Z";

const projects: InitiativeProject[] = [
  { id: "proj-shop", name: "Shop", description: "", created_at: stamp, updated_at: stamp },
  { id: "proj-ops", name: "Ops", description: "", created_at: stamp, updated_at: stamp },
  { id: "proj-empty", name: "Quiet", description: "", created_at: stamp, updated_at: stamp },
];

const repo = (id: string, name: string, project_ids: string[]): Repository => ({
  id,
  name,
  description: "",
  root_path: `/repos/${name}`,
  project_ids,
  created_at: stamp,
  updated_at: stamp,
});

const repositories = [
  repo("repo-shop", "shop-web", ["proj-shop"]),
  repo("repo-ops", "ops-infra", ["proj-ops"]),
  repo("repo-loose", "scratch", []),
];

const column = (slug: string, position: number, is_backlog = false): BoardColumn => ({
  id: slug,
  slug,
  label: slug,
  position,
  is_backlog,
});

const config = {
  columns: [column("backlog", 0, true), column("todo", 1), column("in_progress", 2)],
} as WorkspaceConfig;

const task = (
  id: string,
  key: string,
  title: string,
  repository_id: string,
  extra: Partial<BoardTask> = {},
): BoardTask => ({
  id,
  key,
  title,
  repository_id,
  task_number: 1,
  task_type: "task",
  description: "",
  technical_description: "",
  column: "todo",
  position: 0,
  priority: "medium",
  created_by: "me",
  created_at: stamp,
  updated_at: stamp,
  ...extra,
});

const tasks = [
  task("t-1", "SHOP-1", "Checkout button", "repo-shop"),
  task("t-2", "OPS-1", "Rotate keys", "repo-ops", { column: "in_progress" }),
  task("t-3", "SHOP-2", "Ops tweak in the shop repo", "repo-shop", { initiative_project_id: "proj-ops" }),
  task("t-4", "SCR-1", "Loose end", "repo-loose"),
  task("t-5", "SHOP-3", "Backlog idea", "repo-shop", { column: "backlog" }),
];

function seedCache(cachedTasks: BoardTask[] = tasks) {
  writeCache(CACHE_TASKS, cachedTasks);
  writeCache(CACHE_REPOS, repositories);
  writeCache(CACHE_PROJECTS, projects);
  writeCache(CACHE_CONFIG, config);
  writeCache(CACHE_AGENTS, []);
  writeCache(CACHE_WORKFLOWS, []);
}

let location = "";
function LocationProbe() {
  const current = useLocation();
  location = current.pathname + current.search;
  return null;
}

function renderBoard(url: string) {
  return render(
    <I18nProvider>
      <MemoryRouter initialEntries={[url]}>
        <Routes>
          <Route path="/board" element={<BoardPage />} />
          <Route path="/projects" element={null} />
        </Routes>
        <LocationProbe />
      </MemoryRouter>
    </I18nProvider>,
  );
}

const cardOf = (title: string) => screen.getByText(title).closest("[draggable]") as HTMLElement;

async function pickScope(option: RegExp) {
  fireEvent.click(screen.getByRole("combobox", { name: "Project" }));
  fireEvent.click(await screen.findByRole("option", { name: option }));
}

describe("BoardPage project scope", () => {
  beforeEach(() => {
    window.localStorage.clear();
    seedCache();
    api.listAllTasks.mockReset().mockResolvedValue({ tasks });
    api.getWorkspaceConfig.mockReset().mockResolvedValue(config);
    api.listRepositories.mockReset().mockResolvedValue({ repositories });
    api.listInitiativeProjects.mockReset().mockResolvedValue({ projects });
    api.listAgents.mockReset().mockResolvedValue({ agents: [] });
    api.listWorkflows.mockReset().mockResolvedValue({ workflows: [] });
    api.listActivity.mockReset().mockResolvedValue({ items: [] });
    api.listAllReleases.mockReset().mockResolvedValue({ releases: [] });
    api.listTaskTypes.mockReset().mockResolvedValue({ task_types: [] });
    api.createRepositoryTask.mockReset().mockResolvedValue(tasks[0]);
  });

  it("shows only the chosen project's cards, then every card again under All projects", async () => {
    renderBoard("/board?project=proj-ops");

    expect(await screen.findByText("Rotate keys")).toBeInTheDocument();
    expect(screen.getByText("Ops tweak in the shop repo")).toBeInTheDocument();
    expect(screen.queryByText("Checkout button")).not.toBeInTheDocument();
    expect(screen.queryByText("Loose end")).not.toBeInTheDocument();
    expect(within(cardOf("Ops tweak in the shop repo")).queryByText("Ops")).not.toBeInTheDocument();
    expect(within(cardOf("Ops tweak in the shop repo")).getByText("shop-web")).toBeInTheDocument();

    await pickScope(/All projects/);

    expect(await screen.findByText("Checkout button")).toBeInTheDocument();
    expect(screen.getByText("Loose end")).toBeInTheDocument();
    expect(screen.queryByText("Backlog idea")).not.toBeInTheDocument();
    expect(within(cardOf("Ops tweak in the shop repo")).getByText("Ops")).toBeInTheDocument();
    expect(location).toBe("/board");
    expect(window.localStorage.getItem(PROJECT_SCOPE_STORAGE_KEY)).toBe("all");
  });

  it("counts only board cards per project and offers the project-less slice", async () => {
    renderBoard("/board");
    await screen.findByText("Checkout button");

    fireEvent.click(screen.getByRole("combobox", { name: "Project" }));
    expect(within(await screen.findByRole("option", { name: /Shop/ })).getByText("1")).toBeInTheDocument();
    expect(within(screen.getByRole("option", { name: /Ops/ })).getByText("2")).toBeInTheDocument();
    expect(within(screen.getByRole("option", { name: /Quiet/ })).getByText("0")).toBeInTheDocument();
    fireEvent.click(screen.getByRole("option", { name: /No project/ }));

    expect(await screen.findByText("Loose end")).toBeInTheDocument();
    expect(screen.queryByText("Checkout button")).not.toBeInTheDocument();
    expect(location).toBe("/board?project=none");
  });

  it("uses the scope saved on the backlog when the URL has none", async () => {
    window.localStorage.setItem(PROJECT_SCOPE_STORAGE_KEY, "proj-shop");
    renderBoard("/board");

    expect(await screen.findByText("Checkout button")).toBeInTheDocument();
    expect(screen.queryByText("Rotate keys")).not.toBeInTheDocument();
  });

  it("says so when the project has nothing on the board, and still offers a new task", async () => {
    renderBoard("/board?project=proj-empty");

    expect(await screen.findByText("No tasks in this project yet")).toBeInTheDocument();
    expect(screen.getAllByRole("button", { name: "New Task" })).toHaveLength(2);
    expect(screen.queryByText("Checkout button")).not.toBeInTheDocument();
  });

  it("opens ?task= even when the saved scope is another project, taking the scope to the task's project", async () => {
    window.localStorage.setItem(PROJECT_SCOPE_STORAGE_KEY, "proj-shop");
    renderBoard("/board?task=t-2");

    expect(await screen.findByTestId("task-drawer")).toHaveTextContent("OPS-1");
    await waitFor(() => expect(location).toBe("/board?project=proj-ops"));
    expect(screen.getByText("Rotate keys")).toBeInTheDocument();
    expect(screen.queryByText("Checkout button")).not.toBeInTheDocument();
    expect(window.localStorage.getItem(PROJECT_SCOPE_STORAGE_KEY)).toBe("proj-ops");
  });

  it("waits for the fetch when ?task= names a task the cached board predates", async () => {
    seedCache(tasks.slice(0, 4));
    api.listAllTasks.mockResolvedValue({
      tasks: [...tasks, task("t-9", "SCR-2", "Set up the new repository", "repo-loose")],
    });
    renderBoard("/board?task=t-9");

    expect(await screen.findByTestId("task-drawer")).toHaveTextContent("SCR-2");
    await waitFor(() => expect(location).toBe("/board"));
  });

  it("keeps the picker with no project at all, offering only every task and a way to create one", async () => {
    writeCache(CACHE_PROJECTS, []);
    api.listInitiativeProjects.mockResolvedValue({ projects: [] });
    window.localStorage.setItem(PROJECT_SCOPE_STORAGE_KEY, "none");
    renderBoard("/board");

    const picker = await screen.findByRole("combobox", { name: "Project" });
    expect(picker).toHaveTextContent("All projects");
    expect(screen.getByText("Checkout button")).toBeInTheDocument();

    fireEvent.click(picker);
    expect(await screen.findAllByRole("option")).toHaveLength(2);
    expect(within(screen.getByRole("option", { name: /All projects/ })).getByText("4")).toBeInTheDocument();
    expect(screen.queryByRole("option", { name: /No project/ })).not.toBeInTheDocument();

    fireEvent.click(screen.getByRole("option", { name: "Create a project" }));
    await waitFor(() => expect(location).toBe("/projects"));
  });

  it("opens New Task on the chosen project and its repository", async () => {
    renderBoard("/board?project=proj-shop");
    await screen.findByText("Checkout button");

    fireEvent.click(screen.getAllByRole("button", { name: "New Task" })[0]);
    const dialog = await screen.findByRole("dialog");
    const pickers = within(dialog).getAllByRole("combobox");
    expect(pickers[0]).toHaveTextContent("shop-web");
    expect(pickers[pickers.length - 1]).toHaveTextContent("Shop");

    fireEvent.change(within(dialog).getByLabelText("Title"), { target: { value: "Gift cards" } });
    fireEvent.click(within(dialog).getByRole("button", { name: "Create" }));

    await waitFor(() =>
      expect(api.createRepositoryTask).toHaveBeenCalledWith(
        "repo-shop",
        expect.objectContaining({ title: "Gift cards", initiative_project_id: "proj-shop" }),
      ),
    );
  });
});

describe("BoardPage release badge", () => {
  const doneConfig = {
    columns: [column("backlog", 0, true), column("todo", 1), column("done", 2)],
  } as WorkspaceConfig;
  const shipped = task("t-9", "SHOP-9", "Press card crop", "repo-shop", { column: "done" });
  const verifying: Release = {
    id: "rel-1",
    repository_id: "repo-shop",
    version: "b53ee526cc18",
    mode: "on_merge",
    status: "verifying",
    profile: { mode: "on_merge" } as Release["profile"],
    checks: {} as Release["checks"],
    tasks: [{ id: "t-9" }, { id: "t-1" }],
    created_at: stamp,
    updated_at: stamp,
    verify_until: new Date(Date.now() + 7 * 60000 + 5000).toISOString(),
  };

  beforeEach(() => {
    window.localStorage.clear();
    seedCache([shipped, tasks[0]]);
    writeCache(CACHE_CONFIG, doneConfig);
    api.listAllTasks.mockReset().mockResolvedValue({ tasks: [shipped, tasks[0]] });
    api.getWorkspaceConfig.mockReset().mockResolvedValue(doneConfig);
    api.listRepositories.mockReset().mockResolvedValue({ repositories });
    api.listInitiativeProjects.mockReset().mockResolvedValue({ projects });
    api.listAgents.mockReset().mockResolvedValue({ agents: [] });
    api.listWorkflows.mockReset().mockResolvedValue({ workflows: [] });
    api.listActivity.mockReset().mockResolvedValue({ items: [] });
    api.listAllReleases.mockReset().mockResolvedValue({ releases: [verifying] });
  });

  it("says a merged card in done is verifying and when it moves on, but only on the done card", async () => {
    renderBoard("/board");

    const badge = await within(cardOf("Press card crop")).findByText("Verifying · ~8m");
    expect(badge.closest("[title]")?.getAttribute("title")).toMatch(/moves to Released by itself/);
    expect(within(cardOf("Checkout button")).queryByText(/Verifying/)).not.toBeInTheDocument();
    expect(api.listAllReleases).toHaveBeenCalledWith(
      expect.objectContaining({ statuses: expect.arrayContaining(["deploying", "verifying", "awaiting_verdict"]) }),
    );
  });
});

describe("BoardPage polling", () => {
  const quiet = [task("t-1", "SHOP-1", "Checkout button", "repo-shop"), task("t-4", "SCR-1", "Loose end", "repo-loose")];
  const lastInterval = (spy: { mock: { calls: [{ intervalMs: number }][] } }) =>
    spy.mock.calls[spy.mock.calls.length - 1]?.[0].intervalMs;

  beforeEach(() => {
    window.localStorage.clear();
    seedCache(quiet);
    api.listAllTasks.mockReset().mockResolvedValue({ tasks: quiet });
    api.getWorkspaceConfig.mockReset().mockResolvedValue(config);
    api.listRepositories.mockReset().mockResolvedValue({ repositories });
    api.listInitiativeProjects.mockReset().mockResolvedValue({ projects });
    api.listAgents.mockReset().mockResolvedValue({ agents: [] });
    api.listWorkflows.mockReset().mockResolvedValue({ workflows: [] });
    api.listActivity.mockReset().mockResolvedValue({ items: [] });
    api.listAllReleases.mockReset().mockResolvedValue({ releases: [] });
  });

  it("polls a quiet board every 15s, cards and activity alike", async () => {
    const tasksPoll = vi.spyOn(ALL_TASKS_POLL, "subscribe");
    const activityPoll = vi.spyOn(ACTIVITY_POLL, "subscribe");
    renderBoard("/board");
    await screen.findByText("Checkout button");

    await waitFor(() => expect(lastInterval(activityPoll)).toBe(15000));
    expect(lastInterval(tasksPoll)).toBe(15000);
  });

  it("speeds both up while the task list says an agent is running, with a static badge on the card", async () => {
    const running = [{ ...quiet[0], agent_running: true }, quiet[1]];
    seedCache(running);
    api.listAllTasks.mockResolvedValue({ tasks: running });
    const tasksPoll = vi.spyOn(ALL_TASKS_POLL, "subscribe");
    const activityPoll = vi.spyOn(ACTIVITY_POLL, "subscribe");
    renderBoard("/board");

    await within(cardOf("Checkout button")).findByText("Agent running");
    expect(cardOf("Checkout button").querySelector("[class*='animate-']")).toBeNull();
    await waitFor(() => expect(lastInterval(activityPoll)).toBe(2000));
    expect(lastInterval(tasksPoll)).toBe(5000);
  });

  it("speeds up once the activity feed reports a running agent", async () => {
    api.listActivity.mockResolvedValue({
      items: [{ id: "run-1", kind: "agent_run", task_id: "t-1", status: "running", created_at: stamp }],
    });
    const activityPoll = vi.spyOn(ACTIVITY_POLL, "subscribe");
    renderBoard("/board");

    await within(cardOf("Checkout button")).findByText("Agent running");
    await waitFor(() => expect(lastInterval(activityPoll)).toBe(2000));
  });
});
