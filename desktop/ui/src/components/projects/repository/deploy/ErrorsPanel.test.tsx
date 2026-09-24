import "@testing-library/jest-dom/vitest";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { MemoryRouter } from "react-router-dom";
import { beforeEach, describe, expect, it, vi } from "vitest";
import type { RuntimeErrorGroup } from "@/api";
import { ErrorsPanel } from "@/components/projects/repository/deploy/ErrorsPanel";
import { I18nProvider } from "@/hooks/useI18n";

const { getEnvironmentErrors, createErrorTask } = vi.hoisted(() => ({
  getEnvironmentErrors: vi.fn(),
  createErrorTask: vi.fn(),
}));

vi.mock("@/api", async () => {
  const actual = await vi.importActual<typeof import("@/api")>("@/api");
  return { ...actual, api: { ...actual.api, getEnvironmentErrors, createErrorTask } };
});

const group: RuntimeErrorGroup = {
  fingerprint: "fp-1",
  message: 'panic: assignment to entry in nil map',
  count: 3,
  first_seen: "2024-01-01T00:00:00Z",
  last_seen: "2024-01-01T01:00:00Z",
  new: true,
};

function renderPanel() {
  return render(
    <MemoryRouter>
      <I18nProvider>
        <ErrorsPanel envId="env-1" />
      </I18nProvider>
    </MemoryRouter>,
  );
}

describe("ErrorsPanel", () => {
  beforeEach(() => {
    getEnvironmentErrors.mockReset().mockResolvedValue({ errors: [group] });
    createErrorTask.mockReset().mockResolvedValue({ id: "task-1", key: "BUG-42" });
  });

  it("defaults to the 24h window", async () => {
    renderPanel();
    await waitFor(() => expect(getEnvironmentErrors).toHaveBeenCalledTimes(1));
    expect(getEnvironmentErrors.mock.calls[0][0]).toBe("env-1");
  });

  it("Create task calls createErrorTask with the envId and the error group", async () => {
    renderPanel();
    await screen.findByText(group.message);

    fireEvent.click(screen.getByRole("button", { name: "Create task" }));

    await waitFor(() => expect(createErrorTask).toHaveBeenCalledWith("env-1", group));
  });

  it("switching the range refetches errors", async () => {
    renderPanel();
    await waitFor(() => expect(getEnvironmentErrors).toHaveBeenCalledTimes(1));

    fireEvent.click(screen.getByRole("button", { name: "1h" }));
    await waitFor(() => expect(getEnvironmentErrors).toHaveBeenCalledTimes(2));
  });
});
