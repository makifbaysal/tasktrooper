import "@testing-library/jest-dom/vitest";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import type { DesignFileManifest } from "@/api";
import { FigCraftFileCard } from "@/components/board/FigCraftFileCard";
import { I18nProvider } from "@/hooks/useI18n";

const { getDesignFile, fetchDesignFilePreview, openDesignFile, revealDesignFile } = vi.hoisted(() => ({
  getDesignFile: vi.fn(),
  fetchDesignFilePreview: vi.fn(),
  openDesignFile: vi.fn(),
  revealDesignFile: vi.fn(),
}));

vi.mock("@/api", async () => {
  const actual = await vi.importActual<typeof import("@/api")>("@/api");
  return { ...actual, api: { ...actual.api, getDesignFile, fetchDesignFilePreview, openDesignFile, revealDesignFile } };
});

const manifest: DesignFileManifest = {
  path: "/Users/me/Documents/TaskTrooper Designs/tasks/t1.figcraft",
  exists: true,
  installed: true,
  name: "Pishio — Onboarding",
  modified: 1,
  pages: [
    { id: 1, name: "00 · Foundations", frames: [{ id: 16, name: "Colors", width: 1760, height: 774 }] },
    { id: 8, name: "01 · Onboarding", frames: [] },
  ],
};

function renderCard(props: Partial<Parameters<typeof FigCraftFileCard>[0]> = {}) {
  return render(
    <I18nProvider>
      <FigCraftFileCard scope="task" id="t1" {...props} />
    </I18nProvider>,
  );
}

describe("FigCraftFileCard", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    Object.defineProperty(URL, "createObjectURL", { value: vi.fn(() => "blob:page"), configurable: true });
    Object.defineProperty(URL, "revokeObjectURL", { value: vi.fn(), configurable: true });
    fetchDesignFilePreview.mockResolvedValue(new Blob(["png"]));
    openDesignFile.mockResolvedValue(undefined);
  });

  it("shows nothing until the design file exists", async () => {
    getDesignFile.mockResolvedValue({ ...manifest, exists: false, pages: [] });
    const { container } = renderCard();
    await waitFor(() => expect(getDesignFile).toHaveBeenCalledWith("task", "t1"));
    expect(container).toBeEmptyDOMElement();
  });

  it("renders the first page and opens the file in FigCraft", async () => {
    getDesignFile.mockResolvedValue(manifest);
    renderCard();
    expect(await screen.findByRole("img", { name: "00 · Foundations" })).toHaveAttribute("src", "blob:page");
    expect(fetchDesignFilePreview).toHaveBeenCalledWith("task", "t1", 1, 1);

    fireEvent.click(screen.getByRole("tab", { name: /01 · Onboarding/ }));
    await waitFor(() => expect(fetchDesignFilePreview).toHaveBeenCalledWith("task", "t1", 8, 1));

    fireEvent.click(screen.getByRole("button", { name: /Open in FigCraft/ }));
    expect(openDesignFile).toHaveBeenCalledWith("task", "t1");
  });

  it("says FigCraft is missing instead of rendering", async () => {
    getDesignFile.mockResolvedValue({ ...manifest, installed: false });
    renderCard();
    expect(await screen.findByText(/FigCraft is not installed/)).toBeInTheDocument();
    expect(fetchDesignFilePreview).not.toHaveBeenCalled();
  });
});
