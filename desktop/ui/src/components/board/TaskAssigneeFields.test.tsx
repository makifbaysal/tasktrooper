import { fireEvent, render, screen } from "@testing-library/react";
import type { ComponentProps } from "react";
import { MemoryRouter, Outlet, Route, Routes } from "react-router-dom";
import { describe, expect, it, vi } from "vitest";
import { TaskAssigneeFields } from "@/components/board/TaskAssigneeFields";
import { I18nProvider } from "@/hooks/useI18n";

if (!Element.prototype.scrollIntoView) {
  Element.prototype.scrollIntoView = () => {};
}

const pm = { id: "pm", name: "Product Manager" };
const dev = { id: "dev", name: "Backend Developer" };
const qa = { id: "qa", name: "QA Engineer" };

function renderFields(props: Partial<ComponentProps<typeof TaskAssigneeFields>>) {
  return render(
    <I18nProvider>
      <MemoryRouter initialEntries={["/board"]}>
        <Routes>
          <Route element={<Outlet context={{ unassignableAgentIds: [pm.id] }} />}>
            <Route
              path="board"
              element={<TaskAssigneeFields agents={[pm, dev, qa]} onAgentChange={vi.fn()} {...props} />}
            />
          </Route>
        </Routes>
      </MemoryRouter>
    </I18nProvider>,
  );
}

describe("TaskAssigneeFields", () => {
  it("never offers the product manager", async () => {
    renderFields({});

    fireEvent.click(screen.getByRole("combobox"));

    expect(await screen.findByRole("option", { name: dev.name })).toBeInTheDocument();
    expect(screen.queryByRole("option", { name: pm.name })).not.toBeInTheDocument();
  });

  it("hides the picker when there is only one agent to choose", () => {
    renderFields({ agents: [pm, dev] });

    expect(screen.queryByRole("combobox")).not.toBeInTheDocument();
  });

  it("still names a product manager a card was assigned to before", () => {
    renderFields({ agentValue: pm.id, agentFallbackName: pm.name });

    expect(screen.getByRole("combobox")).toHaveTextContent(pm.name);
  });

  it("hides the picker when only the product manager is left", () => {
    renderFields({ agents: [pm] });

    expect(screen.queryByRole("combobox")).not.toBeInTheDocument();
  });
});
