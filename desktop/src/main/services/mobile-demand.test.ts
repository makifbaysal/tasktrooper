import { describe, expect, it, vi } from "vitest";
import { devicesRegistered, mobileAutomationInUse, reposUseMobile } from "./mobile-demand.js";

const BASE = "http://127.0.0.1:5555";

function answering(routes: Record<string, { status?: number; body: unknown }>) {
  const calls: string[] = [];
  const fetchImpl = vi.fn(async (url: string | URL | Request, init?: RequestInit) => {
    const path = String(url).slice(BASE.length);
    calls.push(path);
    expect(new Headers(init?.headers).get("Authorization")).toBe("Bearer tok");
    const route = routes[path];
    if (!route) throw new Error(`unexpected ${path}`);
    const status = route.status ?? 200;
    return { ok: status < 400, status, json: async () => route.body } as Response;
  });
  return { fetchImpl: fetchImpl as unknown as typeof fetch, calls };
}

describe("reposUseMobile", () => {
  it("is true for a mobile repository, a monorepo with a mobile sub-project, or one listing the mobile kind", () => {
    expect(reposUseMobile({ repositories: [{ kind: "backend" }, { kind: "mobile" }] })).toBe(true);
    expect(reposUseMobile({ repositories: [{ kind: "monorepo", sub_repo_kinds: ["backend", "mobile"] }] })).toBe(true);
    expect(reposUseMobile({ repositories: [{ kind: "monorepo", sub_projects: [{ path: "app", kind: "mobile" }] }] })).toBe(
      true,
    );
  });

  it("is false for anything else, a stray mobile_platform included", () => {
    expect(reposUseMobile({ repositories: [{ kind: "frontend", mobile_platform: "ios" }] })).toBe(false);
    expect(reposUseMobile({ repositories: [] })).toBe(false);
    expect(reposUseMobile({ repositories: null })).toBe(false);
    expect(reposUseMobile(null)).toBe(false);
    expect(reposUseMobile({ repositories: [null, "x", { sub_projects: [null] }] })).toBe(false);
  });
});

describe("devicesRegistered", () => {
  it("is true once any device is registered", () => {
    expect(devicesRegistered({ devices: [{ id: "d1", configured: false }] })).toBe(true);
    expect(devicesRegistered({ devices: [] })).toBe(false);
    expect(devicesRegistered({ devices: null })).toBe(false);
    expect(devicesRegistered(undefined)).toBe(false);
  });
});

describe("mobileAutomationInUse", () => {
  it("answers from the repositories alone when one is mobile, without probing devices", async () => {
    const { fetchImpl, calls } = answering({ "/v1/repositories": { body: { repositories: [{ kind: "mobile" }] } } });
    await expect(mobileAutomationInUse(BASE, "tok", fetchImpl)).resolves.toBe(true);
    expect(calls).toEqual(["/v1/repositories"]);
  });

  it("asks for devices only when no repository is mobile", async () => {
    const { fetchImpl, calls } = answering({
      "/v1/repositories": { body: { repositories: [{ kind: "backend" }] } },
      "/v1/settings/mobile-devices": { body: { devices: [{ id: "d1" }] } },
    });
    await expect(mobileAutomationInUse(BASE, "tok", fetchImpl)).resolves.toBe(true);
    expect(calls).toEqual(["/v1/repositories", "/v1/settings/mobile-devices"]);
  });

  it("is false when neither a repository nor a device uses it", async () => {
    const { fetchImpl } = answering({
      "/v1/repositories": { body: { repositories: [{ kind: "backend" }], count: 1 } },
      "/v1/settings/mobile-devices": { body: { devices: [] } },
    });
    await expect(mobileAutomationInUse(BASE, "tok", fetchImpl)).resolves.toBe(false);
  });

  it("fails open: an answer it cannot read starts the hub, as every install did before", async () => {
    const unavailable = answering({
      "/v1/repositories": { body: { repositories: [] } },
      "/v1/settings/mobile-devices": { status: 503, body: {} },
    });
    await expect(mobileAutomationInUse(BASE, "tok", unavailable.fetchImpl)).resolves.toBe(true);

    const refused = (async () => {
      throw new Error("ECONNREFUSED");
    }) as unknown as typeof fetch;
    await expect(mobileAutomationInUse(BASE, "tok", refused)).resolves.toBe(true);
  });
});
