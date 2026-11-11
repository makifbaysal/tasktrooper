import "@testing-library/jest-dom/vitest";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { I18nProvider } from "@/hooks/useI18n";

const { audienceThread, sendAudienceMessage, followUpAudiencePersona, clearAudienceThread } = vi.hoisted(() => ({
  audienceThread: vi.fn(),
  sendAudienceMessage: vi.fn(),
  followUpAudiencePersona: vi.fn(),
  clearAudienceThread: vi.fn(),
}));

vi.mock("@/api", async () => {
  const actual = await vi.importActual<typeof import("@/api")>("@/api");
  return { ...actual, api: { ...actual.api, audienceThread, sendAudienceMessage, followUpAudiencePersona, clearAudienceThread } };
});
vi.mock("sonner", () => ({ toast: { success: vi.fn(), error: vi.fn() } }));

import { PersonaChatDialog } from "./PersonaChatDialog";

const ayse = { id: "p1", name: "Ayse Demir", role: "UX designer", segment: "Freelancers" };
const msg = (id: string, role: "user" | "persona", content: string) => ({ id, persona_id: "p1", role, content, created_at: "2026-11-11T10:00:00Z" });

function renderChat(props: Partial<React.ComponentProps<typeof PersonaChatDialog>> = {}) {
  return render(
    <I18nProvider>
      <PersonaChatDialog persona={ayse} onOpenChange={() => {}} {...props} />
    </I18nProvider>,
  );
}

beforeEach(() => {
  for (const f of [audienceThread, sendAudienceMessage, followUpAudiencePersona, clearAudienceThread]) f.mockReset();
  window.HTMLElement.prototype.scrollIntoView = vi.fn();
});

describe("PersonaChatDialog", () => {
  it("opens the saved conversation and says the person is invented", async () => {
    audienceThread.mockResolvedValue({ messages: [msg("m1", "user", "Why no?"), msg("m2", "persona", "Too expensive for me.")] });
    renderChat();
    expect(await screen.findByText("Too expensive for me.")).toBeInTheDocument();
    expect(screen.getByText("Talk to Ayse Demir")).toBeInTheDocument();
    expect(screen.getByText("An invented person")).toBeInTheDocument();
    expect(audienceThread).toHaveBeenCalledWith("p1");
  });

  it("invites a first question when the thread is empty", async () => {
    audienceThread.mockResolvedValue({ messages: [] });
    renderChat();
    expect(await screen.findByText(/Ask Ayse Demir anything/)).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Clear conversation" })).not.toBeInTheDocument();
  });

  it("sends a message, shows it at once and then the reply", async () => {
    audienceThread.mockResolvedValueOnce({ messages: [] });
    let finish: () => void = () => {};
    sendAudienceMessage.mockImplementation(() => new Promise<void>((resolve) => (finish = resolve)));
    renderChat();
    await screen.findByText(/Ask Ayse Demir anything/);
    fireEvent.change(screen.getByLabelText("Write a message"), { target: { value: "  At nine dollars?  " } });
    fireEvent.click(screen.getByRole("button", { name: "Send" }));

    expect(await screen.findByText("At nine dollars?")).toBeInTheDocument();
    expect(screen.getByText("Ayse Demir is thinking")).toBeInTheDocument();
    expect(sendAudienceMessage).toHaveBeenCalledWith("p1", "At nine dollars?");

    audienceThread.mockResolvedValueOnce({ messages: [msg("m1", "user", "At nine dollars?"), msg("m2", "persona", "Maybe, if it saves me an hour.")] });
    finish();
    expect(await screen.findByText("Maybe, if it saves me an hour.")).toBeInTheDocument();
    expect(screen.queryByText("Ayse Demir is thinking")).not.toBeInTheDocument();
  });

  it("asks as a follow-up of the study when one is given", async () => {
    audienceThread.mockResolvedValue({ messages: [] });
    followUpAudiencePersona.mockResolvedValue(msg("m2", "persona", "A free tier."));
    const onAnswered = vi.fn();
    renderChat({ studyId: "s1", onAnswered });
    await screen.findByText(/Ask Ayse Demir anything/);
    fireEvent.change(screen.getByLabelText("Write a message"), { target: { value: "What would change it?" } });
    fireEvent.click(screen.getByRole("button", { name: "Send" }));
    await waitFor(() => expect(followUpAudiencePersona).toHaveBeenCalledWith("s1", "p1", "What would change it?"));
    expect(sendAudienceMessage).not.toHaveBeenCalled();
    await waitFor(() => expect(onAnswered).toHaveBeenCalled());
  });

  it("keeps the text and says why when the person does not answer", async () => {
    audienceThread.mockResolvedValue({ messages: [] });
    sendAudienceMessage.mockRejectedValue(new Error("the model did not answer in time"));
    renderChat();
    await screen.findByText(/Ask Ayse Demir anything/);
    fireEvent.change(screen.getByLabelText("Write a message"), { target: { value: "Hello?" } });
    fireEvent.click(screen.getByRole("button", { name: "Send" }));
    expect(await screen.findByText("the model did not answer in time")).toBeInTheDocument();
    expect(screen.getByLabelText("Write a message")).toHaveValue("Hello?");
  });

  it("clears the conversation only after confirming", async () => {
    audienceThread.mockResolvedValue({ messages: [msg("m1", "user", "Hi"), msg("m2", "persona", "Hello")] });
    clearAudienceThread.mockResolvedValue(undefined);
    renderChat();
    fireEvent.click(await screen.findByRole("button", { name: "Clear conversation" }));
    expect(clearAudienceThread).not.toHaveBeenCalled();
    fireEvent.click((await screen.findAllByRole("button", { name: "Clear conversation" })).at(-1)!);
    await waitFor(() => expect(clearAudienceThread).toHaveBeenCalledWith("p1"));
  });

  it("sends with Enter and breaks the line with Shift+Enter", async () => {
    audienceThread.mockResolvedValue({ messages: [] });
    sendAudienceMessage.mockResolvedValue(msg("m2", "persona", "ok"));
    renderChat();
    await screen.findByText(/Ask Ayse Demir anything/);
    const box = screen.getByLabelText("Write a message");
    fireEvent.change(box, { target: { value: "Hi" } });
    fireEvent.keyDown(box, { key: "Enter", shiftKey: true });
    expect(sendAudienceMessage).not.toHaveBeenCalled();
    fireEvent.keyDown(box, { key: "Enter" });
    await waitFor(() => expect(sendAudienceMessage).toHaveBeenCalledWith("p1", "Hi"));
  });
});
