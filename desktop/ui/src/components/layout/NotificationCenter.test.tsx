import "@testing-library/jest-dom/vitest";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import type { ActivityItem, BoardTask } from "@/api";
import { NotificationCenter } from "@/components/layout/NotificationCenter";
import { I18nProvider } from "@/hooks/useI18n";

const { listActivity, listAllTasks, navigate } = vi.hoisted(() => ({
  listActivity: vi.fn(),
  listAllTasks: vi.fn(),
  navigate: vi.fn(),
}));

vi.mock("@/api", async () => {
  const actual = await vi.importActual<typeof import("@/api")>("@/api");
  return {
    ...actual,
    api: {
      ...actual.api,
      listActivity,
      listAllTasks,
    },
  };
});

vi.mock("react-router-dom", async () => {
  const actual = await vi.importActual<typeof import("react-router-dom")>("react-router-dom");
  return { ...actual, useNavigate: () => navigate };
});

const READ_CURSOR_KEY = "tt.notificationCenter.readCursor";

function task(overrides: Partial<BoardTask> = {}): BoardTask {
  return {
    id: "task-1",
    repository_id: "repo-1",
    key: "T-1",
    task_number: 1,
    title: "Fix the thing",
    task_type: "task",
    description: "",
    technical_description: "",
    column: "in_progress",
    position: 0,
    priority: "medium",
    created_by: "human",
    created_at: "2026-09-01T00:00:00Z",
    updated_at: "2026-09-01T00:00:00Z",
    ...overrides,
  };
}

function commentItem(overrides: Partial<ActivityItem> = {}): ActivityItem {
  return {
    id: "evt-1",
    kind: "board_event",
    task_id: "task-1",
    event_type: "task.commented",
    payload: { author_type: "agent", author_id: "agent-1", author_name: "Rex", content: "Done." },
    created_at: "2026-09-18T10:00:00Z",
    ...overrides,
  };
}

function renderCenter(onAgentSeen = vi.fn()) {
  render(
    <I18nProvider>
      <NotificationCenter onAgentSeen={onAgentSeen} />
    </I18nProvider>,
  );
  return { onAgentSeen };
}

function seedCursor(lastEventAt: string, lastEventId = "evt-seed") {
  window.localStorage.setItem(READ_CURSOR_KEY, JSON.stringify({ lastEventId, lastEventAt }));
}

beforeEach(() => {
  window.localStorage.clear();
  navigate.mockClear();
  listActivity.mockResolvedValue({ items: [] });
  listAllTasks.mockResolvedValue({ tasks: [] });
});

describe("NotificationCenter badge", () => {
  it("renders no unread badge with an empty feed", async () => {
    renderCenter();
    await waitFor(() => expect(listActivity).toHaveBeenCalled());
    expect(screen.queryByLabelText(/unread/i)).not.toBeInTheDocument();
  });

  it("treats every pre-existing item as read on a first install (seed)", async () => {
    listActivity.mockResolvedValue({ items: [commentItem()] });
    listAllTasks.mockResolvedValue({ tasks: [task()] });
    renderCenter();

    await waitFor(() => expect(listActivity).toHaveBeenCalled());
    expect(screen.queryByLabelText(/unread/i)).not.toBeInTheDocument();
    expect(window.localStorage.getItem(READ_CURSOR_KEY)).not.toBeNull();
  });

  it("shows an unread badge for an agent comment newer than the read cursor", async () => {
    seedCursor("2026-09-18T09:00:00Z");
    listActivity.mockResolvedValue({ items: [commentItem()] });
    listAllTasks.mockResolvedValue({ tasks: [task()] });
    renderCenter();

    await waitFor(() => expect(screen.getByLabelText("1 unread")).toBeInTheDocument());
  });

  it("excludes a human comment and a routine task.moved from the badge count", async () => {
    seedCursor("2026-09-18T09:00:00Z");
    const humanComment = commentItem({ id: "evt-human", payload: { author_type: "user", content: "hi" } });
    const routineMove: ActivityItem = {
      id: "evt-move",
      kind: "board_event",
      task_id: "task-1",
      event_type: "task.moved",
      payload: { from_column: "todo", to_column: "in_progress" },
      created_at: "2026-09-18T10:00:00Z",
    };
    listActivity.mockResolvedValue({ items: [humanComment, routineMove] });
    listAllTasks.mockResolvedValue({ tasks: [task()] });
    renderCenter();

    await waitFor(() => expect(listActivity).toHaveBeenCalled());
    expect(screen.queryByLabelText(/unread/i)).not.toBeInTheDocument();
  });
});

// One test opens the Radix dropdown for the whole file: its dismissable-layer
// module keeps a document-wide singleton (`originalBodyPointerEvents` and the
// default layer Set in @radix-ui/react-dismissable-layer) that a second
// independent open/close cycle in the same jsdom document does not tear down
// cleanly, so every panel-content assertion is exercised here in one
// continuous open session instead of being spread across separate `it`s.
describe("NotificationCenter panel (single continuous session)", () => {
  it("lists newest-first, routes a click, marks the agent seen, and mark-all-read clears the rest", async () => {
    seedCursor("2026-09-18T09:00:00Z");
    const older = commentItem({
      id: "evt-older",
      task_id: "task-1",
      payload: { author_type: "agent", author_id: "agent-1", author_name: "Rex", content: "First." },
      created_at: "2026-09-18T10:00:00Z",
    });
    const newer = commentItem({
      id: "evt-newer",
      task_id: "task-2",
      payload: { author_type: "agent", author_id: "agent-2", author_name: "Ada", content: "Second." },
      created_at: "2026-09-18T11:00:00Z",
    });
    listActivity.mockResolvedValue({ items: [older, newer] });
    listAllTasks.mockResolvedValue({
      tasks: [task({ id: "task-1", key: "T-1" }), task({ id: "task-2", key: "T-2" })],
    });
    const { onAgentSeen } = renderCenter();

    await waitFor(() => expect(screen.getByLabelText("2 unread")).toBeInTheDocument());

    fireEvent.pointerDown(screen.getByTitle("Notifications"), { button: 0 });
    const rows = await screen.findAllByText(/— New comment$/);
    expect(rows.map((el) => el.textContent)).toEqual(["T-2 — New comment", "T-1 — New comment"]);

    // Click the older (second) row: the read cursor is a single "read up to
    // here" watermark (mirrors the desktop watcher's cursor), so clicking it
    // must not also mark the still-newer row (rows[0]) as read.
    fireEvent.click(rows[1]);
    expect(navigate).toHaveBeenCalledWith("/board?task=task-1");
    expect(onAgentSeen).toHaveBeenCalledWith("agent-1", "2026-09-18T10:00:00Z");
    await waitFor(() => expect(screen.getByLabelText("1 unread")).toBeInTheDocument());
    // Selecting a DropdownMenuItem auto-closes the panel; reopening it here is
    // the same live component (no unmount in between), unlike a fresh `it()`.
    await waitFor(() => expect(screen.queryByRole("menu")).not.toBeInTheDocument());

    fireEvent.pointerDown(screen.getByTitle("Notifications"), { button: 0 });
    fireEvent.click(await screen.findByText("Mark all as read"));
    await waitFor(() => expect(screen.queryByLabelText(/unread/i)).not.toBeInTheDocument());
  });
});
