import "@testing-library/jest-dom/vitest";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import type { AttachmentMeta } from "@/api";
import { AttachmentList } from "@/components/attachments/AttachmentList";
import { I18nProvider } from "@/hooks/useI18n";

const { fetchAttachmentBlob } = vi.hoisted(() => ({ fetchAttachmentBlob: vi.fn() }));

vi.mock("@/api", async () => {
  const actual = await vi.importActual<typeof import("@/api")>("@/api");
  return { ...actual, api: { ...actual.api, fetchAttachmentBlob } };
});

const meta = (id: string, filename: string, content_type: string): AttachmentMeta =>
  ({ id, filename, content_type, size_bytes: 2048 }) as AttachmentMeta;

function renderList(attachments: AttachmentMeta[]) {
  return render(
    <I18nProvider>
      <AttachmentList attachments={attachments} />
    </I18nProvider>,
  );
}

describe("AttachmentList", () => {
  beforeEach(() => {
    fetchAttachmentBlob.mockReset();
    fetchAttachmentBlob.mockResolvedValue(new Blob(["x"], { type: "image/png" }));
    URL.createObjectURL = vi.fn(() => "blob:shot");
    URL.revokeObjectURL = vi.fn();
  });

  it("opens an image at full size inside the app instead of a new window", async () => {
    const open = vi.spyOn(window, "open").mockImplementation(() => null);
    renderList([meta("a-1", "login-screen.png", "image/png")]);

    fireEvent.click(await screen.findByRole("button", { name: "login-screen.png" }));

    const dialog = await screen.findByRole("dialog");
    expect(dialog).toHaveTextContent("login-screen.png");
    expect(dialog.querySelector("img")).toHaveAttribute("src", "blob:shot");
    expect(screen.getByRole("link", { name: /Download/ })).toHaveAttribute("download", "login-screen.png");
    expect(open).not.toHaveBeenCalled();
    open.mockRestore();
  });

  it("downloads a file that is not an image", async () => {
    const click = vi.spyOn(HTMLAnchorElement.prototype, "click").mockImplementation(() => {});
    renderList([meta("a-2", "spec.pdf", "application/pdf")]);

    fireEvent.click(screen.getByRole("button", { name: /spec\.pdf/ }));

    await waitFor(() => expect(fetchAttachmentBlob).toHaveBeenCalledWith("a-2"));
    await waitFor(() => expect(click).toHaveBeenCalled());
    click.mockRestore();
  });
});
