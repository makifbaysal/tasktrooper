import { render, screen } from "@testing-library/react";
import { MemoryRouter } from "react-router-dom";
import { describe, expect, it } from "vitest";
import type { BoardTask } from "@/api";
import { BoardTaskCard } from "@/components/board/BoardTaskCard";
import { I18nProvider } from "@/hooks/useI18n";

const stamp = "2026-01-01T00:00:00Z";

const task = (createdBy: string): BoardTask => ({
  id: "t-1",
  key: "SHOP-1",
  title: "Checkout button",
  repository_id: "repo-shop",
  task_number: 1,
  task_type: "task",
  description: "",
  technical_description: "",
  column: "todo",
  position: 0,
  priority: "medium",
  created_by: createdBy,
  created_at: stamp,
  updated_at: stamp,
});

function renderCard(
  createdBy: string,
  extra: { person?: string; assignee?: string } = {},
) {
  render(
    <I18nProvider>
      <MemoryRouter>
        <BoardTaskCard
          task={task(createdBy)}
          repositoryName="shop"
          agentRunning={false}
          dragging={false}
          onDragStart={() => {}}
          onDragEnd={() => {}}
          onOpen={() => {}}
          onDelete={() => {}}
          {...extra}
        />
      </MemoryRouter>
    </I18nProvider>,
  );
}

describe("BoardTaskCard person", () => {
  it("shows the assigned person", () => {
    renderCard("user", { person: "Ada Lovelace" });
    expect(screen.getByText("Ada Lovelace")).toBeTruthy();
    expect(screen.queryByText("user")).toBeNull();
  });

  it.each(["user", "User", "human"])(
    "shows nothing for a human creator (%s)",
    (createdBy) => {
      renderCard(createdBy);
      expect(screen.queryByText(createdBy)).toBeNull();
    },
  );

  it("keeps the creator for an agent-created task", () => {
    renderCard("agent");
    expect(screen.getByText("agent")).toBeTruthy();
  });

  it("shows the person and the agent together", () => {
    renderCard("user", { person: "Ada", assignee: "Builder" });
    expect(screen.getByText("Ada")).toBeTruthy();
    expect(screen.getByText("Builder")).toBeTruthy();
  });
});
