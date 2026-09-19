import "@testing-library/jest-dom/vitest";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { describe, expect, it, vi, beforeEach } from "vitest";
import { OpenRepositoryDialog } from "./RepositoryDialogs";
import { I18nProvider } from "@/hooks/useI18n";
import type { DesktopRunnerHost } from "@/lib/desktop-bridge";

const { listInitiativeProjects } = vi.hoisted(() => ({
  listInitiativeProjects: vi.fn(),
}));

vi.mock("@/api", async () => {
  const actual = await vi.importActual<typeof import("@/api")>("@/api");
  return {
    ...actual,
    api: { ...actual.api, listInitiativeProjects },
  };
});

describe("OpenRepositoryDialog folder picker", () => {
  beforeEach(() => {
    listInitiativeProjects.mockReset();
    listInitiativeProjects.mockResolvedValue({ projects: [] });
    delete (window as unknown as { __tasktrooperDesktop?: unknown }).__tasktrooperDesktop;
  });

  it("renders browse button and fills input when folder is chosen via desktop runner", async () => {
    const chooseDirectory = vi.fn().mockResolvedValue("C:\\test\\my-project");
    (window as unknown as { __tasktrooperDesktop: { runner: Partial<DesktopRunnerHost> } }).__tasktrooperDesktop = {
      runner: {
        chooseDirectory,
      },
    };

    render(
      <I18nProvider>
        <OpenRepositoryDialog open={true} onOpenChange={() => {}} onSuccess={() => {}} />
      </I18nProvider>,
    );

    const browseBtn = screen.getByRole("button", { name: /Browse/i });
    expect(browseBtn).toBeInTheDocument();

    const input = screen.getByPlaceholderText("/path/to/project") as HTMLInputElement;
    expect(input.value).toBe("");

    fireEvent.click(browseBtn);

    await waitFor(() => {
      expect(chooseDirectory).toHaveBeenCalledWith({
        title: "Choose Repository Folder",
        defaultPath: undefined,
      });
      expect(input.value).toBe("C:\\test\\my-project");
    });
  });

  it("does not render browse button in browser-only mode without desktop runner", () => {
    render(
      <I18nProvider>
        <OpenRepositoryDialog open={true} onOpenChange={() => {}} onSuccess={() => {}} />
      </I18nProvider>,
    );

    expect(screen.queryByRole("button", { name: /Browse/i })).not.toBeInTheDocument();
    expect(screen.getByPlaceholderText("/path/to/project")).toBeInTheDocument();
  });
});
