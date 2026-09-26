import "@testing-library/jest-dom/vitest";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { useState } from "react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { SmokeCheck, SmokeGenerationJob, SmokeTestResponse } from "@/api";
import { SmokeChecksEditor } from "@/components/projects/repository/deploy/SmokeChecksEditor";
import { I18nProvider } from "@/hooks/useI18n";

if (!Element.prototype.scrollIntoView) {
  Element.prototype.scrollIntoView = () => {};
}

const { testSmokeChecks, generateSmokeChecks, getSmokeGeneration, cancelSmokeGeneration } = vi.hoisted(() => ({
  testSmokeChecks: vi.fn(),
  generateSmokeChecks: vi.fn(),
  getSmokeGeneration: vi.fn(),
  cancelSmokeGeneration: vi.fn(),
}));

vi.mock("@/api", async () => {
  const actual = await vi.importActual<typeof import("@/api")>("@/api");
  return {
    ...actual,
    api: { ...actual.api, testSmokeChecks, generateSmokeChecks, getSmokeGeneration, cancelSmokeGeneration },
  };
});

// No <Toaster/> is mounted in these tests, so a toast never reaches the DOM;
// asserting on the calls is the only way to see what it would have shown.
const { toastSuccess, toastError } = vi.hoisted(() => ({ toastSuccess: vi.fn(), toastError: vi.fn() }));

vi.mock("sonner", () => ({ toast: { success: toastSuccess, error: toastError } }));

function makeJob(overrides: Partial<SmokeGenerationJob> = {}): SmokeGenerationJob {
  return {
    job_id: "job-1",
    component_id: "comp-1",
    status: "running",
    agent_name: "release-engineer",
    started_at: "2024-01-01T00:00:00Z",
    checks: [],
    results: [],
    base_url: "",
    dropped: 0,
    error: "",
    ...overrides,
  };
}

function Wrapper({ initial, baseUrl }: { initial: SmokeCheck[]; baseUrl?: string }) {
  const [checks, setChecks] = useState<SmokeCheck[]>(initial);
  return (
    <I18nProvider>
      <SmokeChecksEditor checks={checks} onChange={setChecks} componentId="comp-1" baseUrl={baseUrl} />
    </I18nProvider>
  );
}

describe("SmokeChecksEditor", () => {
  beforeEach(() => {
    testSmokeChecks.mockReset();
    generateSmokeChecks.mockReset();
    getSmokeGeneration.mockReset();
    cancelSmokeGeneration.mockReset().mockResolvedValue(undefined);
    toastSuccess.mockReset();
    toastError.mockReset();
  });

  afterEach(() => {
    vi.useRealTimers();
  });

  it("labels every field of a check", () => {
    render(<Wrapper initial={[{ method: "GET", path: "/" }]} />);
    expect(screen.getByLabelText("Name (optional)")).toBeInTheDocument();
    expect(screen.getByLabelText("Method")).toBeInTheDocument();
    expect(screen.getByLabelText("Path or URL")).toBeInTheDocument();
    expect(screen.getByLabelText("Expected status code")).toBeInTheDocument();
    expect(screen.getByLabelText("Body must contain")).toBeInTheDocument();
    expect(screen.getByLabelText("Max latency (ms)")).toBeInTheDocument();
  });

  it("shows a short empty state with the preset menu when there are no checks", () => {
    render(<Wrapper initial={[]} />);
    expect(screen.getByText(/No smoke checks yet/)).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Add check" })).toBeInTheDocument();
  });

  it("adds a filled-in check from a preset", async () => {
    render(<Wrapper initial={[]} />);
    fireEvent.pointerDown(screen.getByRole("button", { name: "Add check" }), { button: 0 });
    fireEvent.click(await screen.findByRole("menuitem", { name: "Homepage loads" }));

    expect(await screen.findByText("Ana sayfa")).toBeInTheDocument();
    expect(screen.getByLabelText("Path or URL")).toHaveValue("/");
    expect(screen.getByLabelText("Expected status code")).toHaveValue(200);
  });

  it("disables and clears the body-contains field once the method is HEAD", async () => {
    render(<Wrapper initial={[{ method: "GET", path: "/health", contains: "ok" }]} />);
    expect(screen.getByLabelText("Body must contain")).not.toBeDisabled();

    fireEvent.click(screen.getByLabelText("Method"));
    fireEvent.click(await screen.findByRole("option", { name: "HEAD" }));

    const contains = screen.getByLabelText("Body must contain");
    expect(contains).toBeDisabled();
    expect(contains).toHaveValue("");
  });

  it("shows the PROD base URL as a prefix and the resolved URL preview for a relative path", () => {
    render(<Wrapper initial={[{ method: "GET", path: "/health" }]} baseUrl="https://example.com" />);
    expect(screen.getByText("https://example.com")).toBeInTheDocument();
    expect(screen.getByText("https://example.com/health")).toBeInTheDocument();
  });

  it("warns when the path is relative and no PROD environment is bound", () => {
    render(<Wrapper initial={[{ method: "GET", path: "/health" }]} />);
    expect(screen.getByText("PROD environment isn't bound — the relative path is skipped after a deploy")).toBeInTheDocument();
  });

  it("runs the draft checks, shows pass/fail badges and a summary, then clears on edit", async () => {
    const checks: SmokeCheck[] = [
      { method: "GET", path: "/health", expect_status: 200 },
      { method: "GET", path: "/broken", expect_status: 200 },
    ];
    const response: SmokeTestResponse = {
      base_url: "https://example.com",
      results: [
        { check: checks[0], url: "https://example.com/health", at: "2024-01-01T00:00:00Z", status: 200, ok: true, latency_ms: 123 },
        { check: checks[1], url: "https://example.com/broken", at: "2024-01-01T00:00:00Z", status: 500, ok: false, error: "boom" },
      ],
    };
    testSmokeChecks.mockResolvedValue(response);

    render(<Wrapper initial={checks} />);
    fireEvent.click(screen.getByRole("button", { name: "Test now" }));

    await waitFor(() => expect(testSmokeChecks).toHaveBeenCalledWith("comp-1", checks));
    expect(await screen.findByText("✓ 200 · 123 ms")).toBeInTheDocument();
    expect(screen.getByText("✗ 500")).toBeInTheDocument();
    expect(screen.getByText("boom")).toBeInTheDocument();
    expect(screen.getByText(/1\/2 passed/)).toBeInTheDocument();

    fireEvent.change(screen.getAllByLabelText("Name (optional)")[0], { target: { value: "Renamed" } });
    expect(screen.queryByText("✓ 200 · 123 ms")).not.toBeInTheDocument();
  });

  it("shows the server's error message when the test request fails", async () => {
    testSmokeChecks.mockRejectedValue(new Error("component has no production environment"));
    render(<Wrapper initial={[{ method: "GET", path: "/health" }]} />);
    fireEvent.click(screen.getByRole("button", { name: "Test now" }));
    expect(await screen.findByText("component has no production environment")).toBeInTheDocument();
  });

  describe("AI generation", () => {
    it("starts a generation with the current checks as existing", async () => {
      const checks: SmokeCheck[] = [{ method: "GET", path: "/health" }];
      generateSmokeChecks.mockResolvedValue(makeJob({ status: "running" }));
      render(<Wrapper initial={checks} />);

      fireEvent.click(screen.getByRole("button", { name: "Generate with AI" }));
      await waitFor(() => expect(generateSmokeChecks).toHaveBeenCalledWith("comp-1", checks));
    });

    it("shows a running notice with the agent name, and Stop cancels the job", async () => {
      generateSmokeChecks.mockResolvedValue(makeJob({ status: "running" }));
      render(<Wrapper initial={[{ method: "GET", path: "/health" }]} />);

      fireEvent.click(screen.getByRole("button", { name: "Generate with AI" }));
      expect(await screen.findByText(/reading the code/)).toBeInTheDocument();
      expect(screen.getByText(/release-engineer/)).toBeInTheDocument();

      fireEvent.click(screen.getByRole("button", { name: "Stop" }));
      await waitFor(() => expect(cancelSmokeGeneration).toHaveBeenCalledWith("job-1"));
      expect(screen.queryByText(/reading the code/)).not.toBeInTheDocument();
    });

    it("polls until done, appends the suggested checks with badges and seeded results, and toasts a count", async () => {
      const existing: SmokeCheck[] = [{ method: "GET", path: "/health" }];
      const suggested: SmokeCheck = { method: "GET", path: "/api/health", expect_status: 200, name: "API health" };
      generateSmokeChecks.mockResolvedValue(makeJob({ status: "running" }));
      getSmokeGeneration.mockResolvedValue(
        makeJob({
          status: "done",
          checks: [suggested],
          results: [{ check: suggested, url: "https://example.com/api/health", at: "2024-01-01T00:00:00Z", status: 200, ok: true, latency_ms: 42 }],
          base_url: "https://example.com",
          dropped: 2,
        }),
      );

      let latest: SmokeCheck[] = existing;
      render(
        <I18nProvider>
          <SmokeChecksEditor checks={existing} onChange={(next) => (latest = next)} componentId="comp-1" />
        </I18nProvider>,
      );

      fireEvent.click(screen.getByRole("button", { name: "Generate with AI" }));

      await waitFor(() => expect(latest).toEqual([existing[0], suggested]), { timeout: 5000 });
      expect(getSmokeGeneration).toHaveBeenCalledWith("job-1");
      expect(toastSuccess).toHaveBeenCalledWith("1 checks suggested · 2 invalid suggestions dropped");
    });

    it("appends suggested checks and renders their AI badge and pass/fail badge", async () => {
      const existing: SmokeCheck[] = [{ method: "GET", path: "/health" }];
      const suggested: SmokeCheck = { method: "GET", path: "/api/health", expect_status: 200, name: "API health" };
      generateSmokeChecks.mockResolvedValue(
        makeJob({
          status: "done",
          checks: [suggested],
          results: [{ check: suggested, url: "https://example.com/api/health", at: "2024-01-01T00:00:00Z", status: 200, ok: true, latency_ms: 42 }],
          base_url: "https://example.com",
          dropped: 1,
        }),
      );

      render(<Wrapper initial={existing} />);
      fireEvent.click(screen.getByRole("button", { name: "Generate with AI" }));

      expect(await screen.findByText("API health")).toBeInTheDocument();
      expect(screen.getByText("AI suggestion")).toBeInTheDocument();
      expect(screen.getByText("✓ 200 · 42 ms")).toBeInTheDocument();
      await waitFor(() => expect(toastSuccess).toHaveBeenCalledWith("1 checks suggested · 1 invalid suggestions dropped"));
    });

    it("shows a warning notice when the job finishes with zero checks", async () => {
      generateSmokeChecks.mockResolvedValue(
        makeJob({ status: "done", checks: [], error: "the repository has no HTTP routes to check" }),
      );
      render(<Wrapper initial={[{ method: "GET", path: "/health" }]} />);

      fireEvent.click(screen.getByRole("button", { name: "Generate with AI" }));
      expect(await screen.findByText("The AI found no checks to suggest")).toBeInTheDocument();
      expect(screen.getByText("the repository has no HTTP routes to check")).toBeInTheDocument();
    });

    it("shows a destructive notice with retry when the job fails", async () => {
      generateSmokeChecks.mockResolvedValue(makeJob({ status: "running" }));
      getSmokeGeneration.mockResolvedValue(makeJob({ status: "failed", error: "no runnable release-engineer agent" }));
      render(<Wrapper initial={[]} />);

      fireEvent.click(screen.getAllByRole("button", { name: "Generate with AI" })[0]);
      expect(await screen.findByText(/reading the code/)).toBeInTheDocument();

      expect(await screen.findByText("Generation failed", {}, { timeout: 5000 })).toBeInTheDocument();
      expect(screen.getByText("no runnable release-engineer agent")).toBeInTheDocument();

      generateSmokeChecks.mockResolvedValue(makeJob({ status: "running" }));
      fireEvent.click(screen.getByRole("button", { name: "Try again" }));
      await waitFor(() => expect(generateSmokeChecks).toHaveBeenCalledTimes(2));
    });

    it("resets silently when the job is cancelled", async () => {
      vi.useFakeTimers();
      generateSmokeChecks.mockResolvedValue(makeJob({ status: "running" }));
      getSmokeGeneration.mockResolvedValue(makeJob({ status: "cancelled" }));
      render(<Wrapper initial={[{ method: "GET", path: "/health" }]} />);

      fireEvent.click(screen.getByRole("button", { name: "Generate with AI" }));
      await vi.advanceTimersByTimeAsync(0);
      expect(screen.getByText(/reading the code/)).toBeInTheDocument();

      await vi.advanceTimersByTimeAsync(2000);
      expect(screen.queryByText(/reading the code/)).not.toBeInTheDocument();
      expect(screen.queryByText("Generation failed")).not.toBeInTheDocument();
      expect(screen.queryByText("The AI found no checks to suggest")).not.toBeInTheDocument();
    });

    it("toasts and shows a notice when starting the generation fails outright", async () => {
      generateSmokeChecks.mockRejectedValue(new Error("no runnable release-engineer agent"));
      render(<Wrapper initial={[{ method: "GET", path: "/health" }]} />);

      fireEvent.click(screen.getByRole("button", { name: "Generate with AI" }));
      expect(await screen.findByText("Could not start generation")).toBeInTheDocument();
      expect(screen.getByText("no runnable release-engineer agent")).toBeInTheDocument();
      expect(toastError).toHaveBeenCalledWith("no runnable release-engineer agent");
    });

    it("still allows a manual preset to be added after AI suggestions land", async () => {
      const suggested: SmokeCheck = { method: "GET", path: "/api/health" };
      generateSmokeChecks.mockResolvedValue(makeJob({ status: "done", checks: [suggested], results: [] }));
      render(<Wrapper initial={[]} />);

      fireEvent.click(screen.getAllByRole("button", { name: "Generate with AI" })[0]);
      await screen.findByText("AI suggestion");

      const addTrigger = screen.getByRole("button", { name: "Add check" });
      addTrigger.focus();
      fireEvent.keyDown(addTrigger, { key: "Enter" });
      fireEvent.click(await screen.findByRole("menuitem", { name: "Homepage loads" }));
      expect(await screen.findByText("Ana sayfa")).toBeInTheDocument();
      // The AI-suggested check is still there, unaffected by the manual add.
      expect(screen.getByText("AI suggestion")).toBeInTheDocument();
    });

    it("disables the button while running and once MAX_SMOKE_CHECKS is reached", async () => {
      generateSmokeChecks.mockResolvedValue(makeJob({ status: "running" }));
      const checks: SmokeCheck[] = Array.from({ length: 20 }, (_, i) => ({ method: "GET", path: `/p${i}` }));
      render(<Wrapper initial={checks} />);
      expect(screen.getByRole("button", { name: "Generate with AI" })).toBeDisabled();
    });

    it("cancels the running job when the editor unmounts", async () => {
      generateSmokeChecks.mockResolvedValue(makeJob({ status: "running" }));
      const { unmount } = render(<Wrapper initial={[{ method: "GET", path: "/health" }]} />);

      fireEvent.click(screen.getByRole("button", { name: "Generate with AI" }));
      await screen.findByText(/reading the code/);

      unmount();
      await waitFor(() => expect(cancelSmokeGeneration).toHaveBeenCalledWith("job-1"));
    });
  });
});
