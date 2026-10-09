import "@testing-library/jest-dom/vitest";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import type { MCPServerView } from "@/api";
import { MCPServerPicker } from "@/components/admin/MCPServerPicker";
import { I18nProvider } from "@/hooks/useI18n";

const { listMCPServers } = vi.hoisted(() => ({ listMCPServers: vi.fn() }));

vi.mock("@/api", async () => {
  const actual = await vi.importActual<typeof import("@/api")>("@/api");
  return { ...actual, api: { ...actual.api, listMCPServers } };
});

function server(id: string): MCPServerView {
  return {
    id,
    enabled: true,
    transport: "http",
    url: "https://mcp.example.com/mcp",
    access: "listed",
    created_at: "2026-10-01T00:00:00Z",
    connected: true,
    tool_count: 0,
    status: "connected",
    auth: "none",
  };
}

async function renderPicker(selected: string[], onChange = vi.fn()) {
  render(
    <I18nProvider>
      <MCPServerPicker selected={selected} onChange={onChange} />
    </I18nProvider>,
  );
  await waitFor(() => expect(screen.getByRole("button", { name: /select/i })).toBeInTheDocument());
  fireEvent.click(screen.getByRole("button", { name: /select/i }));
  return onChange;
}

describe("MCPServerPicker", () => {
  beforeEach(() => {
    listMCPServers.mockReset();
    listMCPServers.mockResolvedValue({ servers: [server("gitlab")] });
  });

  it("lists TaskTrooper first, checked and locked", async () => {
    await renderPicker([]);
    const boxes = await screen.findAllByRole("checkbox");
    expect(boxes[0]).toBeChecked();
    expect(boxes[0]).toBeDisabled();
    expect(boxes[0].closest("label")).toHaveTextContent("TaskTrooper");
    expect(boxes[1].closest("label")).toHaveTextContent("gitlab");
    expect(boxes[1]).not.toBeChecked();
  });

  it("never offers TaskTrooper a remove button", async () => {
    await renderPicker([]);
    expect(screen.queryByRole("button", { name: /remove TaskTrooper/i })).toBeNull();
  });

  it("never emits tasktrooper when another server is picked", async () => {
    const onChange = await renderPicker([]);
    fireEvent.click((await screen.findAllByRole("checkbox"))[1]);
    expect(onChange).toHaveBeenCalledWith(["gitlab"]);
  });

  it("drops a stored tasktrooper on the next change", async () => {
    const onChange = await renderPicker(["tasktrooper", "gitlab"]);
    fireEvent.click((await screen.findAllByRole("checkbox"))[1]);
    expect(onChange).toHaveBeenCalledWith([]);
  });

  it("keeps a selected id the server does not know visible and removable", async () => {
    const onChange = await renderPicker(["gitlab", "notes"]);
    const boxes = await screen.findAllByRole("checkbox");
    const notes = boxes.find((box) => box.closest("label")?.textContent?.includes("notes"));
    expect(notes).toBeChecked();
    expect(notes?.closest("label")).toHaveAttribute("title", "Not configured here");
    fireEvent.click(notes as HTMLElement);
    expect(onChange).toHaveBeenCalledWith(["gitlab"]);
  });
});
