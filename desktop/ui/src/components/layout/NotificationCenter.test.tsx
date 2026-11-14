import "@testing-library/jest-dom/vitest";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import type { ActivityItem, Agent, BoardTask } from "@/api";
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

function chatItem(overrides: Partial<ActivityItem> = {}): ActivityItem {
  return {
    id: "evt-chat-1",
    kind: "chat_message",
    agent_id: "agent-1",
    session_id: "session-1",
    summary: "Here's the update.",
    created_at: "2026-09-18T10:00:00Z",
    ...overrides,
  };
}

const agents: Agent[] = [{ id: "agent-1", name: "Rex", enabled: true } as Agent];

function renderCenter(onAgentSeen = vi.fn(), agentList: Agent[] = agents) {
  render(
    <I18nProvider>
      <NotificationCenter onAgentSeen={onAgentSeen} agents={agentList} />
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
  it(
    "lists newest-first with the sender's avatar+name and a localized system title, " +
      "routes a click, marks the agent seen, and mark-all-read clears the rest",
    async () => {
      seedCursor("2026-09-18T09:00:00Z");
      const older = commentItem({
        id: "evt-older",
        task_id: "task-1",
        payload: { author_type: "agent", author_id: "agent-1", author_name: "Rex", content: "First." },
        created_at: "2026-09-18T10:00:00Z",
      });
      const newer = chatItem({
        id: "evt-newer",
        agent_id: "agent-2",
        session_id: "session-2",
        summary: "Second.",
        created_at: "2026-09-18T11:00:00Z",
      });
      const uatMove: ActivityItem = {
        id: "evt-uat",
        kind: "board_event",
        task_id: "task-2",
        event_type: "task.moved",
        payload: { from_column: "in_progress", to_column: "human_uat" },
        created_at: "2026-09-18T12:00:00Z",
      };
      listActivity.mockResolvedValue({ items: [older, newer, uatMove] });
      listAllTasks.mockResolvedValue({
        tasks: [task({ id: "task-1", key: "T-1" }), task({ id: "task-2", key: "T-2" })],
      });
      const { onAgentSeen } = renderCenter(vi.fn(), [
        { id: "agent-1", name: "Rex", enabled: true } as Agent,
        { id: "agent-2", name: "Ada", enabled: true } as Agent,
      ]);

      await waitFor(() => expect(screen.getByLabelText("3 unread")).toBeInTheDocument());

      fireEvent.pointerDown(screen.getByTitle("Notifications"), { button: 0 });
      const rows = await screen.findAllByRole("menuitem");
      expect(rows).toHaveLength(3);

      // Newest first: the UAT system item, then the chat message, then the comment.
      expect(rows[0].textContent).toContain("T-2 is waiting for your UAT");
      expect(rows[1].textContent).toContain("Ada");
      expect(rows[1].textContent).toContain("Second.");
      expect(rows[1].querySelector("[aria-hidden]")).not.toBeNull();
      expect(rows[2].textContent).toContain("Rex");
      expect(rows[2].textContent).toContain("First.");
      expect(rows[2].querySelector("[aria-hidden]")).not.toBeNull();

      // Click the comment row: the read cursor is a single "read up to here"
      // watermark (mirrors the desktop watcher's cursor), so clicking it must
      // not also mark the still-newer rows as read.
      fireEvent.click(rows[2]);
      expect(navigate).toHaveBeenCalledWith("/board?task=task-1");
      expect(onAgentSeen).toHaveBeenCalledWith("agent-1", "2026-09-18T10:00:00Z");
      await waitFor(() => expect(screen.getByLabelText("2 unread")).toBeInTheDocument());
      // Selecting a DropdownMenuItem auto-closes the panel; reopening it here is
      // the same live component (no unmount in between), unlike a fresh `it()`.
      await waitFor(() => expect(screen.queryByRole("menu")).not.toBeInTheDocument());

      fireEvent.pointerDown(screen.getByTitle("Notifications"), { button: 0 });
      fireEvent.click(await screen.findByText("Mark all as read"));
      await waitFor(() => expect(screen.queryByLabelText(/unread/i)).not.toBeInTheDocument());
    },
  );
});
