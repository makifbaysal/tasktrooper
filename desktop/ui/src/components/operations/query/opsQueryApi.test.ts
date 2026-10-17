import { afterEach, describe, expect, it, vi } from "vitest";
import { api, opsTooManyEnvironments } from "@/api";

function jsonResponse(body: unknown, status = 200) {
  return new Response(JSON.stringify(body), { status, headers: { "content-type": "application/json" } });
}

function requested(fetchMock: ReturnType<typeof vi.fn>): URL {
  return new URL(String(fetchMock.mock.calls[0][0]), "http://localhost");
}

afterEach(() => {
  vi.unstubAllGlobals();
});

describe("Operations query API calls", () => {
  it("builds the errors query: env, since and one comma list of components", async () => {
    const fetchMock = vi.fn().mockResolvedValue(jsonResponse({ groups: [], unavailable: [], truncated: false }));
    vi.stubGlobal("fetch", fetchMock);
    await api.listOpsErrors({ env: "staging", since: "2026-10-15T12:00:00.000Z", componentIds: ["c-1", "c-2", "c-1", " "] });
    const url = requested(fetchMock);
    expect(url.pathname).toBe("/v1/operations/errors");
    expect(url.searchParams.get("env")).toBe("staging");
    expect(url.searchParams.get("since")).toBe("2026-10-15T12:00:00.000Z");
    expect(url.searchParams.getAll("component_id")).toEqual(["c-1,c-2"]);
  });

  it("leaves out what is not set", async () => {
    const fetchMock = vi.fn().mockResolvedValue(jsonResponse({ groups: [], unavailable: [], truncated: false }));
    vi.stubGlobal("fetch", fetchMock);
    await api.listOpsErrors();
    expect(String(fetchMock.mock.calls[0][0])).toMatch(/\/v1\/operations\/errors$/);
  });

  it("builds the logs query and passes `until` exactly as it was returned", async () => {
    const fetchMock = vi.fn().mockResolvedValue(jsonResponse({ entries: [], unavailable: [], truncated: false }));
    vi.stubGlobal("fetch", fetchMock);
    await api.listOpsLogs({
      env: "production",
      since: "2026-10-16T09:00:00.000Z",
      until: "2026-10-16T10:00:00.123456789Z",
      componentIds: ["c-api"],
      minSeverity: "warning",
      text: "  timeout  ",
      limit: 200,
    });
    const url = requested(fetchMock);
    expect(url.pathname).toBe("/v1/operations/logs");
    expect(Object.fromEntries(url.searchParams)).toEqual({
      env: "production",
      since: "2026-10-16T09:00:00.000Z",
      until: "2026-10-16T10:00:00.123456789Z",
      min_severity: "warning",
      text: "timeout",
      limit: "200",
      component_id: "c-api",
    });
  });

  it("reads too_many_environments' counts off the 400", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn().mockResolvedValue(
        jsonResponse({ error: "too_many_environments", code: "too_many_environments", matched: 14, max_environments: 10 }, 400),
      ),
    );
    const err = await api.listOpsLogs({ env: "production" }).catch((e: unknown) => e);
    expect(opsTooManyEnvironments(err)).toEqual({ matched: 14, max: 10 });
    expect(opsTooManyEnvironments(new Error("nope"))).toBeNull();
  });
});
