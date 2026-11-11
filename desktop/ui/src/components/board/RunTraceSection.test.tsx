import "@testing-library/jest-dom/vitest";
import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { RunTraceEvent, RunTraceSummary } from "@/api";
import { RunTraceSection } from "@/components/board/RunTraceSection";
import { I18nProvider } from "@/hooks/useI18n";

const { getRunTraceSummary, listRunTrace, saveTextFile } = vi.hoisted(() => ({
  getRunTraceSummary: vi.fn(),
  listRunTrace: vi.fn(),
  saveTextFile: vi.fn(),
}));

vi.mock("@/api", async () => {
  const actual = await vi.importActual<typeof import("@/api")>("@/api");
  return { ...actual, api: { ...actual.api, getRunTraceSummary, listRunTrace } };
});

vi.mock("@/lib/runTrace", async () => {
  const actual = await vi.importActual<typeof import("@/lib/runTrace")>("@/lib/runTrace");
  return { ...actual, saveTextFile };
});

function ev(over: Partial<RunTraceEvent>): RunTraceEvent {
  return {
    run_id: "r1",
    seq: 1,
    at: "2026-01-01T10:00:00Z",
    duration_ms: 12,
    tool: "read_file",
    source: "loop",
    category: "read",
    status: "ok",
    ...over,
  };
}

const events: RunTraceEvent[] = [
  ev({ seq: 1, target: "src/a.go", detail: { path: "src/a.go", offset: 10, limit: 40 } }),
  ev({ seq: 2, tool: "edit_file", category: "change", target: "src/a.go", detail: { path: "src/a.go", hash_before: "aaaaaaaaaaaa", hash_after: "bbbbbbbbbbbb", size_delta: 2048 } }),
  ev({
    seq: 3,
    tool: "run_terminal",
    category: "exec",
    target: "go test ./...",
    status: "error",
    error: "exit status 2",
    detail: { command: "go test ./...", cwd: ".", exit_code: 2, output_head: ["FAIL pkg"], output_tail: ["done"], output_lines: 90 },
  }),
  ev({ seq: 4, tool: "http_request", category: "external", target: "api.example.com", detail: { host: "api.example.com", method: "POST", effect: "http_request" } }),
];

function summary(over: Partial<RunTraceSummary> = {}): RunTraceSummary {
  return {
    run: { run_id: "r1", base_sha: "0123456789abcdef", events: 4, dropped: 0, started_at: "2026-01-01T10:00:00Z" },
    counts: { read: 1, change: 1, exec: 1, external: 1, board: 0, undo: 0, failed: 1, total: 4, read_files: 14, changed_files: 3 },
    verification: {
      computed_at: "2026-01-01T10:05:00Z",
      base_sha: "0123456789abcdef",
      available: true,
      files: [{ path: "src/a.go", status: "M" }, { path: "shell.out", status: "?" }],
      unrecorded: ["shell.out"],
      recorded_only: [],
      tracked_files: 415,
      untouched: 412,
    },
    non_undoable: [{ seq: 4, tool: "http_request", kind: "http_request", target: "api.example.com" }],
    ...over,
  };
}

function renderTrace() {
  return render(
    <I18nProvider>
      <RunTraceSection repositoryId="repo" taskId="task" runId="r1" live={false} active prUrl="https://github.com/o/r/pull/3" />
    </I18nProvider>,
  );
}

describe("RunTraceSection", () => {
  beforeEach(() => {
    getRunTraceSummary.mockReset().mockResolvedValue({ recorded: true, summary: summary() });
    listRunTrace.mockReset().mockResolvedValue({ recorded: true, events, count: events.length });
    saveTextFile.mockReset();
  });
  afterEach(() => cleanup());

  it("shows the counts, the verified count of untouched files and the changed-outside-calls flag", async () => {
    renderTrace();
    expect(await screen.findByText(/Read 14 files · changed 3 · ran 1 commands · 1 external calls/)).toBeInTheDocument();
    expect(screen.getByText("412 other tracked files untouched, verified by git diff")).toBeInTheDocument();
    expect(screen.getByText("Changed outside recorded tool calls")).toBeInTheDocument();
    expect(screen.getAllByText("shell.out").length).toBeGreaterThan(0);
    expect(screen.getByText("Open the diff").closest("a")).toHaveAttribute("href", "https://github.com/o/r/pull/3/files");
    expect(screen.getByText("Not undone by a git reset")).toBeInTheDocument();
  });

  it("filters the timeline by category", async () => {
    renderTrace();
    await screen.findByText("run_terminal");
    expect(screen.getByText("edit_file")).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: /^Ran/ }));
    expect(screen.getByText("run_terminal")).toBeInTheDocument();
    expect(screen.queryByText("edit_file")).not.toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: /^All/ }));
    expect(screen.getByText("edit_file")).toBeInTheDocument();
  });

  it("expands a command row to the command, exit code and trimmed output", async () => {
    renderTrace();
    fireEvent.click(await screen.findByRole("button", { name: "run_terminal go test ./..." }));
    expect(screen.getByText(/Exit code 2/)).toBeInTheDocument();
    expect(screen.getByText("FAIL pkg")).toBeInTheDocument();
    expect(screen.getByText("done")).toBeInTheDocument();
    expect(screen.getByText(/lines in between are not stored/)).toBeInTheDocument();
  });

  it("explains a run with no trace", async () => {
    getRunTraceSummary.mockResolvedValue({ recorded: false });
    renderTrace();
    expect(await screen.findByText(/No trace was recorded for this run/)).toBeInTheDocument();
  });

  it("says why the git comparison is missing", async () => {
    getRunTraceSummary.mockResolvedValue({
      recorded: true,
      summary: summary({ verification: { computed_at: "x", base_sha: "", available: false, reason: "no_checkout", files: [], unrecorded: [], recorded_only: [], tracked_files: 0, untouched: 0 } }),
    });
    renderTrace();
    expect(await screen.findByText(/no git checkout on this computer/)).toBeInTheDocument();
  });

  it("exports JSON and an HTML page with escaped text", async () => {
    listRunTrace.mockResolvedValue({
      recorded: true,
      count: 1,
      events: [ev({ seq: 1, tool: "run_terminal", category: "exec", target: "echo <script>alert(1)</script>", detail: { command: "echo <script>alert(1)</script>" } })],
    });
    renderTrace();
    await screen.findByText("run_terminal");
    fireEvent.click(screen.getByRole("button", { name: "Export JSON" }));
    await waitFor(() => expect(saveTextFile).toHaveBeenCalledTimes(1));
    expect(saveTextFile.mock.calls[0][1]).toBe("application/json");
    expect(JSON.parse(saveTextFile.mock.calls[0][2]).events).toHaveLength(1);

    fireEvent.click(screen.getByRole("button", { name: "Export HTML" }));
    await waitFor(() => expect(saveTextFile).toHaveBeenCalledTimes(2));
    const html = saveTextFile.mock.calls[1][2] as string;
    expect(saveTextFile.mock.calls[1][1]).toBe("text/html");
    expect(html).not.toContain("<script>alert(1)");
    expect(html).toContain("&lt;script&gt;alert(1)&lt;/script&gt;");
    expect(html).not.toMatch(/<script/i);
    expect(html).toContain("prefers-color-scheme:dark");
  });
});
