import { render } from "@testing-library/react";
import { MemoryRouter, Route, Routes, useNavigate } from "react-router-dom";
import { afterEach, describe, expect, it, vi } from "vitest";
import { act } from "react";
import {
  agentRole,
  hashUserId,
  identifyUser,
  logEvent,
  screenFor,
  setAnalyticsSink,
  useScreenTracking,
  type AnalyticsSink,
} from "@/lib/analytics";

function stubShell() {
  const analytics = {
    get: vi.fn(),
    set: vi.fn(),
    track: vi.fn().mockResolvedValue(undefined),
    identify: vi.fn().mockResolvedValue(undefined),
  };
  window.__tasktrooperDesktop = { analytics };
  return analytics;
}

function stubSink(): AnalyticsSink & { track: ReturnType<typeof vi.fn>; identify: ReturnType<typeof vi.fn> } {
  const sink = { track: vi.fn(), identify: vi.fn() };
  setAnalyticsSink(sink);
  return sink;
}

afterEach(() => {
  delete window.__tasktrooperDesktop;
  setAnalyticsSink(null);
});

describe("screenFor", () => {
  it.each([
    ["/board", "board", "/board"],
    ["/projects/new", "repo_add", "/projects/new"],
    ["/projects/3f2c", "project_detail", "/projects/:projectId"],
    ["/agents/pm/chat/s-1", "agent_chat", "/agents/:agentId/chat/:sessionId"],
    ["/repositories/r1/tasks/t9/analysis", "analysis_review", "/repositories/:repositoryId/tasks/:taskId/analysis"],
    ["/settings", "settings_general", "/settings"],
    ["/settings/llm", "settings_llm", "/settings/llm"],
    ["/operations", "ops_explorer", "/operations"],
    ["/operations/entity/c-1/errors", "ops_entity_errors", "/operations/entity/:componentId/errors"],
    ["/operations/mobile/app/r-1", "ops_store_apps", "/operations/mobile/app/:repositoryId"],
    ["/operations/alerts", "ops_alerts", "/operations/alerts"],
    ["/operations/entity/c-1/alerts", "ops_entity_alerts", "/operations/entity/:componentId/alerts"],
  ])("names %s %s and sends only the template", (path, name, template) => {
    expect(screenFor(path)).toEqual({ name, template });
  });

  it.each(["/", "/teams/x", "/nowhere", "/operations/deployments", "/operations/incidents", "/operations/entity/c-1"])("does not count %s", (path) => {
    expect(screenFor(path)).toBeNull();
  });
});

describe("logEvent", () => {
  it("goes to the desktop shell when it has bridge 7", () => {
    const shell = stubShell();
    const browser = stubSink();
    logEvent("task_move", { from_column: "todo", to_column: "in_progress" });
    expect(shell.track).toHaveBeenCalledWith({
      name: "task_move",
      params: { from_column: "todo", to_column: "in_progress" },
    });
    expect(browser.track).not.toHaveBeenCalled();
  });

  it("goes to the registered sink in a browser", () => {
    const browser = stubSink();
    logEvent("chat_start", { agent_role: "product-manager", source: "chat" });
    expect(browser.track).toHaveBeenCalledWith("chat_start", { agent_role: "product-manager", source: "chat" });
  });

  it("is a no-op with neither, and never throws into the caller", () => {
    expect(() => logEvent("project_create", {})).not.toThrow();
    setAnalyticsSink({
      track: () => {
        throw new Error("boom");
      },
      identify: () => undefined,
    });
    expect(() => logEvent("project_create", {})).not.toThrow();
  });

  it("drops params left undefined", () => {
    const browser = stubSink();
    logEvent("integration_connect", { provider: "jira", method: undefined });
    expect(browser.track).toHaveBeenCalledWith("integration_connect", { provider: "jira" });
  });

  it("reports a catalog agent by slug and anything else as custom", () => {
    expect(agentRole({ catalog_slug: "product-manager" })).toBe("product-manager");
    expect(agentRole({ catalog_slug: "" })).toBe("custom");
    expect(agentRole(null)).toBe("custom");
  });

  it("skips an older shell whose bridge has no track", () => {
    window.__tasktrooperDesktop = { analytics: { get: vi.fn(), set: vi.fn() } };
    const browser = stubSink();
    logEvent("project_create", {});
    expect(browser.track).toHaveBeenCalledOnce();
  });
});

describe("identifyUser", () => {
  it("sends only the hash, and null on sign-out", async () => {
    const browser = stubSink();
    await identifyUser("firebase-uid-123");
    const sent = browser.identify.mock.calls[0]?.[0] as string;
    expect(sent).toMatch(/^[0-9a-f]{64}$/);
    expect(sent).toBe(await hashUserId("firebase-uid-123"));
    expect(sent).not.toContain("firebase-uid-123");
    await identifyUser(null);
    expect(browser.identify).toHaveBeenLastCalledWith(null);
  });

  it("matches the hash iOS computes for the same uid", async () => {
    // sha256("tasktrooper:abc") — the fixture Analytics.swift's test asserts too.
    expect(await hashUserId("abc")).toBe("78b9ce823bb2fa78bd344d3dad3fbfc5ee8254867cba143edce475f47245734c");
  });
});

describe("useScreenTracking", () => {
  let go: (to: string) => void = () => undefined;
  function Tracker() {
    useScreenTracking();
    go = useNavigate();
    return null;
  }

  it("sends one page_view per screen, named by template", () => {
    const browser = stubSink();
    render(
      <MemoryRouter initialEntries={["/board"]}>
        <Routes>
          <Route path="*" element={<Tracker />} />
        </Routes>
      </MemoryRouter>,
    );
    act(() => go("/projects/abc-123"));
    act(() => go("/teams/legacy"));
    expect(browser.track.mock.calls.map((c) => c[1])).toEqual([
      { screen_name: "board", page_title: "board", page_location: `${window.location.origin}/board` },
      {
        screen_name: "project_detail",
        page_title: "project_detail",
        page_location: `${window.location.origin}/projects/:projectId`,
      },
    ]);
    expect(JSON.stringify(browser.track.mock.calls)).not.toContain("abc-123");
  });
});
