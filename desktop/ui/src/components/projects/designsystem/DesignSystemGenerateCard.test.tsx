import "@testing-library/jest-dom/vitest";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { MemoryRouter } from "react-router-dom";
import { describe, expect, it, vi } from "vitest";
import type { BoardTask, DesignSystemGenerateResult } from "@/api";
import { DesignSystemGenerateCard } from "@/components/projects/designsystem/DesignSystemGenerateCard";
import { I18nProvider } from "@/hooks/useI18n";

const task = {
  id: "t-9", key: "D-3", title: "Design system layer: web", repository_id: "r-1",
  column: "todo", task_type: "design",
} as BoardTask;

describe("DesignSystemGenerateCard", () => {
  it("says a task opened before any designer waits, and hands it over on the next press", async () => {
    const onGenerate = vi
      .fn<(notes: string) => Promise<DesignSystemGenerateResult>>()
      .mockResolvedValueOnce({ task, created: true, waiting_for_designer: true })
      .mockResolvedValueOnce({ task: { ...task, assignee_agent_id: "designer-1" }, created: false });
    render(
      <I18nProvider>
        <MemoryRouter>
          <DesignSystemGenerateCard scope="repository" mode="create" onGenerate={onGenerate} />
        </MemoryRouter>
      </I18nProvider>,
    );

    fireEvent.click(screen.getByRole("button", { name: "Create repository layer from code" }));
    expect(await screen.findByTestId("design-task-waiting")).toHaveTextContent("No agent holds the designer role yet");

    fireEvent.click(screen.getByRole("button", { name: "Hand it to the designer" }));
    await waitFor(() => expect(onGenerate).toHaveBeenCalledTimes(2));
    await waitFor(() => expect(screen.queryByTestId("design-task-waiting")).not.toBeInTheDocument());
  });
});
