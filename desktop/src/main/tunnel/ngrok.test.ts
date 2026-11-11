import { describe, expect, it, vi } from "vitest";
import { listNgrokDomains, parseReservedDomains, probeNgrok } from "./ngrok.js";
import { HttpStatusError, type TunnelProcess } from "./process.js";

class Proc implements TunnelProcess {
  lines: ((l: string) => void)[] = [];
  exits: ((code: number | null, signal: string | null) => void)[] = [];
  killed = false;
  onLine(cb: (l: string) => void) {
    this.lines.push(cb);
  }
  onExit(cb: (code: number | null, signal: string | null) => void) {
    this.exits.push(cb);
  }
  kill() {
    this.killed = true;
    return Promise.resolve();
  }
}

describe("probeNgrok", () => {
  it("is ok on 'started tunnel' and stops the test tunnel; the token is in the environment only", async () => {
    const proc = new Proc();
    const spawn = vi.fn(() => proc);
    const result = probeNgrok(spawn, "/bin/ngrok", "TOKEN_SECRET_1234567", 1000);
    proc.lines[0]?.('{"lvl":"info","msg":"started tunnel","url":"https://x.ngrok-free.app"}');
    expect(await result).toEqual({ ok: true });
    expect(proc.killed).toBe(true);
    expect(spawn).toHaveBeenCalledWith("/bin/ngrok", ["http", "127.0.0.1:9", "--log=stdout", "--log-format=json"], { NGROK_AUTHTOKEN: "TOKEN_SECRET_1234567" });
  });

  it("fails with ngrok's refusal", async () => {
    const proc = new Proc();
    const result = probeNgrok(() => proc, "/bin/ngrok", "tok-123456789", 1000);
    proc.lines[0]?.('{"err":"authentication failed: ERR_NGROK_4018","lvl":"eror","msg":"command failed"}');
    expect(await result).toMatchObject({ ok: false, message: expect.stringContaining("authtoken") });
  });

  it("fails when ngrok exits first, naming the last error", async () => {
    const proc = new Proc();
    const result = probeNgrok(() => proc, "/bin/ngrok", "tok-123456789", 1000);
    proc.lines[0]?.('{"err":"dial tcp: lookup connect.ngrok-agent.com: no such host","lvl":"warn","msg":"failed to connect"}');
    proc.exits[0]?.(1, null);
    expect(await result).toMatchObject({ ok: false, message: expect.stringContaining("no such host") });
  });

  it("times out", async () => {
    vi.useFakeTimers();
    const proc = new Proc();
    const result = probeNgrok(() => proc, "/bin/ngrok", "tok-123456789", 500);
    await vi.advanceTimersByTimeAsync(600);
    expect(await result).toMatchObject({ ok: false, message: expect.stringContaining("did not answer") });
    expect(proc.killed).toBe(true);
    vi.useRealTimers();
  });
});

describe("parseReservedDomains", () => {
  it("reads the domains and the next page link, only from ngrok's API", () => {
    expect(
      parseReservedDomains({
        reserved_domains: [{ id: "rd_1", domain: "Mine.ngrok.app" }, { domain: "nope" }, { id: "x" }, { domain: "tunnel.example.com", cname_target: "x" }],
        next_page_uri: "https://api.ngrok.com/reserved_domains?before_id=rd_1",
      }),
    ).toEqual({ domains: ["mine.ngrok.app", "tunnel.example.com"], next: "https://api.ngrok.com/reserved_domains?before_id=rd_1" });
    expect(parseReservedDomains({ reserved_domains: [], next_page_uri: "https://evil.example.com/steal" }).next).toBeNull();
    expect(parseReservedDomains(null)).toEqual({ domains: [], next: null });
  });
});

describe("listNgrokDomains", () => {
  it("sends the key as a bearer with the API version and follows pages, sorted and unique", async () => {
    const fetchJson = vi
      .fn()
      .mockResolvedValueOnce({ reserved_domains: [{ domain: "b.ngrok.app" }], next_page_uri: "https://api.ngrok.com/reserved_domains?before_id=1" })
      .mockResolvedValueOnce({ reserved_domains: [{ domain: "a.ngrok.app" }, { domain: "b.ngrok.app" }], next_page_uri: null });
    expect(await listNgrokDomains(fetchJson, "KEY")).toEqual(["a.ngrok.app", "b.ngrok.app"]);
    expect(fetchJson.mock.calls[0]?.[1]).toMatchObject({ Authorization: "Bearer KEY", "Ngrok-Version": "2" });
    expect(fetchJson).toHaveBeenCalledTimes(2);
  });

  it("explains a rejected key, and an unreachable API", async () => {
    await expect(listNgrokDomains(() => Promise.reject(new HttpStatusError(401)), "KEY")).rejects.toThrow(/did not accept that API key/);
    await expect(listNgrokDomains(() => Promise.reject(new Error("offline")), "KEY")).rejects.toThrow(/Could not reach the ngrok API \(offline\)/);
  });
});
