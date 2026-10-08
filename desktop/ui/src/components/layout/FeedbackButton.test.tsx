import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { FeedbackButton } from "@/components/layout/FeedbackButton";
import { I18nProvider } from "@/hooks/useI18n";
import { FEEDBACK_REPO_URL } from "@/lib/feedback";

function renderButton() {
  return render(
    <I18nProvider>
      <FeedbackButton />
    </I18nProvider>,
  );
}

afterEach(() => {
  delete window.__tasktrooperDesktop;
  vi.restoreAllMocks();
});

describe("FeedbackButton", () => {
  it("opens a pre-filled GitHub issue in the real browser through the shell", async () => {
    const openExternal = vi.fn().mockResolvedValue(true);
    window.__tasktrooperDesktop = {
      info: () => Promise.resolve({ app: "tasktrooper-desktop", version: "0.2.22", platform: "darwin" }),
      runner: { openExternal } as never,
    };
    renderButton();

    fireEvent.click(screen.getByRole("button", { name: "Send feedback" }));
    fireEvent.change(await screen.findByLabelText("Title"), { target: { value: "Bell does nothing" } });
    fireEvent.change(screen.getByLabelText("Details"), { target: { value: "In full screen." } });
    await screen.findByLabelText(/TaskTrooper 0\.2\.22, darwin/);
    fireEvent.click(screen.getByRole("button", { name: /continue on github/i }));

    await waitFor(() => expect(openExternal).toHaveBeenCalledTimes(1));
    const url = new URL(openExternal.mock.calls[0]![0] as string);
    expect(url.toString().startsWith(`${FEEDBACK_REPO_URL}/issues/new`)).toBe(true);
    expect(url.searchParams.get("title")).toBe("Bell does nothing");
    expect(url.searchParams.get("body")).toContain("In full screen.");
    expect(url.searchParams.get("body")).toContain("TaskTrooper version: 0.2.22");
    await waitFor(() => expect(screen.queryByLabelText("Title")).toBeNull());
  });

  it("does not open anything without a title", async () => {
    const openExternal = vi.fn().mockResolvedValue(true);
    window.__tasktrooperDesktop = { runner: { openExternal } as never };
    renderButton();

    fireEvent.click(screen.getByRole("button", { name: "Send feedback" }));
    const submit = await screen.findByRole("button", { name: /continue on github/i });
    expect((submit as HTMLButtonElement).disabled).toBe(true);
    expect(openExternal).not.toHaveBeenCalled();
  });

  it("uses a new tab in a plain browser", async () => {
    const open = vi.spyOn(window, "open").mockReturnValue(null);
    renderButton();

    fireEvent.click(screen.getByRole("button", { name: "Send feedback" }));
    fireEvent.change(await screen.findByLabelText("Title"), { target: { value: "Idea" } });
    fireEvent.click(screen.getByRole("button", { name: /continue on github/i }));

    await waitFor(() => expect(open).toHaveBeenCalledTimes(1));
    expect(open.mock.calls[0]![1]).toBe("_blank");
  });
});
