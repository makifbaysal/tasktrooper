import { afterEach, describe, expect, it, vi } from "vitest";
import { ApiError, api, dataSourceFieldErrors } from "@/api";

function jsonResponse(body: unknown, status = 200) {
  return new Response(JSON.stringify(body), { status, headers: { "content-type": "application/json" } });
}

afterEach(() => {
  vi.unstubAllGlobals();
});

describe("data source API calls", () => {
  it("gives discover the server's upstream budget, not the 20s read deadline", async () => {
    const timeout = vi.spyOn(AbortSignal, "timeout");
    const fetchMock = vi.fn().mockResolvedValue(jsonResponse({ metrics: [], truncated: false, recipes: [] }));
    vi.stubGlobal("fetch", fetchMock);
    await api.discoverDataSource("ds-1", { match: "up", limit: 200 });
    expect(fetchMock.mock.calls[0][0]).toContain("/v1/data-sources/ds-1/discover?match=up&limit=200");
    expect(timeout).toHaveBeenCalledWith(60_000);
  });

  it("carries a 400's field codes on the error", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn().mockResolvedValue(jsonResponse({ error: "invalid data source", fields: { url: "not_permitted" } }, 400)),
    );
    const err = await api.createDataSource({ kind: "prometheus", config: { url: "http://169.254.169.254" }, secrets: {} }).catch((e: unknown) => e);
    expect(err).toBeInstanceOf(ApiError);
    expect(dataSourceFieldErrors(err)).toEqual({ url: "not_permitted" });
  });
});
