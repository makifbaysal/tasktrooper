import "@testing-library/jest-dom/vitest";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { DesignSystemFilesCard } from "@/components/projects/designsystem/DesignSystemFilesCard";
import { I18nProvider } from "@/hooks/useI18n";

const { getRepositoryDesignSystemFiles } = vi.hoisted(() => ({ getRepositoryDesignSystemFiles: vi.fn() }));

vi.mock("@/api", async () => {
  const actual = await vi.importActual<typeof import("@/api")>("@/api");
  return { ...actual, api: { ...actual.api, getRepositoryDesignSystemFiles } };
});

const files = [
  { path: "DESIGN.md", content: "# Acme design system\n" },
  { path: "design/tokens.css", content: ":root {\n  --color-accent: #2563eb;\n}\n" },
];

function renderCard() {
  return render(
    <I18nProvider>
      <DesignSystemFilesCard repositoryId="repo-1" />
    </I18nProvider>,
  );
}

const originalClipboard = Object.getOwnPropertyDescriptor(navigator, "clipboard");

describe("DesignSystemFilesCard", () => {
  beforeEach(() => {
    getRepositoryDesignSystemFiles.mockReset().mockResolvedValue({ files });
  });

  afterEach(() => {
    if (originalClipboard) Object.defineProperty(navigator, "clipboard", originalClipboard);
    else delete (navigator as { clipboard?: unknown }).clipboard;
    delete (document as { execCommand?: unknown }).execCommand;
  });

  it("loads the files only once opened and shows each path with its content", async () => {
    renderCard();
    expect(getRepositoryDesignSystemFiles).not.toHaveBeenCalled();

    fireEvent.click(screen.getByRole("button", { name: "Show files" }));

    expect(await screen.findByText("design/tokens.css")).toBeInTheDocument();
    expect(getRepositoryDesignSystemFiles).toHaveBeenCalledWith("repo-1");
    expect(screen.getByText("DESIGN.md")).toBeInTheDocument();
    expect(screen.getByText(/--color-accent: #2563eb;/)).toBeInTheDocument();
  });

  it("copies a file's content with the Clipboard API", async () => {
    const writeText = vi.fn().mockResolvedValue(undefined);
    Object.defineProperty(navigator, "clipboard", { value: { writeText }, configurable: true });
    renderCard();

    fireEvent.click(screen.getByRole("button", { name: "Show files" }));
    fireEvent.click(await screen.findByRole("button", { name: "Copy design/tokens.css" }));

    await waitFor(() => expect(writeText).toHaveBeenCalledWith(files[1].content));
  });

  it("falls back to a hidden textarea when the Clipboard API refuses", async () => {
    const writeText = vi.fn().mockRejectedValue(new Error("not allowed"));
    Object.defineProperty(navigator, "clipboard", { value: { writeText }, configurable: true });
    let copied = "";
    const execCommand = vi.fn(() => {
      copied = document.querySelector("textarea")?.value ?? "";
      return true;
    });
    Object.defineProperty(document, "execCommand", { value: execCommand, configurable: true });
    renderCard();

    fireEvent.click(screen.getByRole("button", { name: "Show files" }));
    fireEvent.click(await screen.findByRole("button", { name: "Copy DESIGN.md" }));

    await waitFor(() => expect(execCommand).toHaveBeenCalledWith("copy"));
    expect(copied).toBe(files[0].content);
    expect(document.querySelector("textarea")).toBeNull();
  });

  it("says so when the repository follows no design system", async () => {
    getRepositoryDesignSystemFiles.mockResolvedValue({ files: [] });
    renderCard();

    fireEvent.click(screen.getByRole("button", { name: "Show files" }));
    expect(await screen.findByText("No files yet: this repository follows no design system.")).toBeInTheDocument();
  });
});
