import { describe, expect, it } from "vitest";
import {
  cloudflareConfig,
  firstProblem,
  joinHostname,
  normalizeSubdomain,
  originCertPath,
  parseLoginLine,
  parseOriginCert,
  parseRouteDns,
  parseTunnelCreate,
  parseZoneName,
  provisionTunnel,
  type CloudflareTunnelRecord,
  type RunFn,
} from "./cloudflare.js";
import type { RunResult, TunnelFiles } from "./process.js";

const ID = "0b8a3c1e-5d2f-4c7a-9e11-3f6d8a2b4c90";

function pem(json: unknown): string {
  return `-----BEGIN PRIVATE KEY-----\nAAAA\n-----END PRIVATE KEY-----\n-----BEGIN ARGO TUNNEL TOKEN-----\n${Buffer.from(JSON.stringify(json)).toString("base64")}\n-----END ARGO TUNNEL TOKEN-----\n`;
}

const ok = (...lines: string[]): RunResult => ({ code: 0, lines, timedOut: false });
const bad = (...lines: string[]): RunResult => ({ code: 1, lines, timedOut: false });

describe("parseOriginCert", () => {
  it("reads zone and account out of the ARGO TUNNEL TOKEN block", () => {
    expect(parseOriginCert(pem({ zoneID: "z1", accountID: "a1", apiToken: "tok" }))).toEqual({ zoneId: "z1", accountId: "a1", apiToken: "tok" });
  });

  it("wraps long base64 lines like a real PEM", () => {
    const body = Buffer.from(JSON.stringify({ zoneID: "z".repeat(40), accountID: "a", apiToken: "t".repeat(80) })).toString("base64");
    const wrapped = body.replace(/(.{64})/g, "$1\n");
    const text = `-----BEGIN ARGO TUNNEL TOKEN-----\n${wrapped}\n-----END ARGO TUNNEL TOKEN-----`;
    expect(parseOriginCert(text)?.zoneId).toBe("z".repeat(40));
  });

  it("is null for a file that is not a login", () => {
    expect(parseOriginCert("")).toBeNull();
    expect(parseOriginCert("-----BEGIN CERTIFICATE-----\nAAAA\n-----END CERTIFICATE-----")).toBeNull();
    expect(parseOriginCert(pem({ accountID: "a1" }))).toBeNull();
    expect(parseOriginCert("-----BEGIN ARGO TUNNEL TOKEN-----\n!!notbase64!!\n-----END ARGO TUNNEL TOKEN-----")).toBeNull();
  });
});

describe("originCertPath", () => {
  it("is ~/.cloudflared/cert.pem unless cloudflared's own environment variable says otherwise", () => {
    expect(originCertPath("/home/u", {})).toBe("/home/u/.cloudflared/cert.pem");
    expect(originCertPath("/home/u", { TUNNEL_ORIGIN_CERT: "/etc/cf/cert.pem" })).toBe("/etc/cf/cert.pem");
  });
});

describe("parseZoneName", () => {
  it("takes result.name from Cloudflare's zone answer", () => {
    expect(parseZoneName({ success: true, result: { id: "z1", name: "Example.COM" } })).toBe("example.com");
  });
  it("ignores anything else", () => {
    expect(parseZoneName({ result: { name: "not a domain" } })).toBeNull();
    expect(parseZoneName({ errors: [{ code: 9109 }] })).toBeNull();
    expect(parseZoneName(null)).toBeNull();
  });
});

describe("subdomains", () => {
  it("accepts one DNS label, trimmed and lowercased", () => {
    expect(normalizeSubdomain("  TaskTrooper-ab12 ")).toBe("tasktrooper-ab12");
    expect(joinHostname("phone", "example.com")).toBe("phone.example.com");
  });
  it("reduces a pasted full hostname to its subdomain, and refuses anything that is not one label", () => {
    expect(normalizeSubdomain("phone.example.com", "example.com")).toBe("phone");
    expect(normalizeSubdomain("a.b", "example.com")).toBeNull();
    expect(normalizeSubdomain("-bad", "example.com")).toBeNull();
    expect(normalizeSubdomain("", "example.com")).toBeNull();
    expect(normalizeSubdomain("x/../y")).toBeNull();
  });
});

describe("parseLoginLine", () => {
  it("finds the sign-in link", () => {
    const event = parseLoginLine("https://dash.cloudflare.com/argotunnel?aud=&callback=https%3A%2F%2Flogin.cloudflareaccess.org%2Fabc");
    expect(event).toEqual({ kind: "url", url: "https://dash.cloudflare.com/argotunnel?aud=&callback=https%3A%2F%2Flogin.cloudflareaccess.org%2Fabc" });
  });
  it("does not take a link of another site for the sign-in", () => {
    expect(parseLoginLine("see https://example.com/help")).toBeNull();
    expect(parseLoginLine("https://evilcloudflare.com/argotunnel")).toBeNull();
    expect(parseLoginLine("https://cloudflare.com.evil.org/argotunnel")).toBeNull();
  });
  it("knows when it is done, and explains an existing certificate", () => {
    expect(parseLoginLine("You have successfully logged in.")).toEqual({ kind: "done" });
    expect(parseLoginLine("You have an existing certificate at /home/u/.cloudflared/cert.pem which login would overwrite.")).toMatchObject({ kind: "error", message: expect.stringContaining("Log out first") });
  });
  it("reports errors and ignores chatter", () => {
    expect(parseLoginLine("2026-11-11T09:00:00Z ERR failed to fetch resource")).toMatchObject({ kind: "error" });
    expect(parseLoginLine("Leave cloudflared running to download the cert automatically.")).toBeNull();
    expect(parseLoginLine("")).toBeNull();
  });
});

describe("parseTunnelCreate", () => {
  const OUT = [
    "Tunnel credentials written to /Users/akif/Library/Application Support/x/.cloudflared/0b8a3c1e-5d2f-4c7a-9e11-3f6d8a2b4c90.json. cloudflared chose this file based on where your origin certificate was found. Keep this file secret.",
    "To revoke these credentials, delete the tunnel.",
    "",
    "Created tunnel tasktrooper-ab12 with id 0B8A3C1E-5D2F-4C7A-9E11-3F6D8A2B4C90",
  ];
  it("reads the id and the credentials path, which may contain spaces", () => {
    expect(parseTunnelCreate(OUT)).toEqual({
      id: ID,
      name: "tasktrooper-ab12",
      credentialsFile: "/Users/akif/Library/Application Support/x/.cloudflared/0b8a3c1e-5d2f-4c7a-9e11-3f6d8a2b4c90.json",
    });
  });
  it("is null when no tunnel was made", () => {
    expect(parseTunnelCreate(["Tunnel create failed"])).toBeNull();
  });
});

describe("parseRouteDns", () => {
  it("reads the hostname cloudflared actually routed", () => {
    expect(parseRouteDns(ok(`2026 INF Added CNAME Phone.Example.com which will route to this tunnel tunnelID=${ID}`))).toEqual({ kind: "ok", hostname: "phone.example.com" });
    expect(parseRouteDns(ok(`INF phone.example.com is already configured to route to your tunnel tunnelID=${ID}`))).toEqual({ kind: "ok", hostname: "phone.example.com" });
  });
  it("tells an existing record from other failures", () => {
    expect(parseRouteDns(bad("Failed to add route: ... A, AAAA, or CNAME record with that host already exists."))).toEqual({ kind: "exists" });
    expect(parseRouteDns(bad("ERR Failed to add route: code: 10000, reason: Authentication error"))).toMatchObject({ kind: "failed", message: expect.stringContaining("Authentication error") });
  });
});

describe("firstProblem", () => {
  it("picks the line that says what went wrong and keeps secrets out", () => {
    expect(firstProblem(["INF starting", "ERR Unauthorized: token eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9abcdef failed"])).toBe("Unauthorized: token [redacted] failed");
    expect(firstProblem([])).toBe("cloudflared did not say why");
  });
});

describe("cloudflareConfig", () => {
  it("sends only the chosen hostname to the remote listener, everything else to a 404", () => {
    const text = cloudflareConfig({ tunnelId: ID, credentialsFile: "C:\\Users\\a b\\.cloudflared\\x.json", hostname: "phone.example.com", port: 47820 });
    expect(text).toBe(
      [
        `tunnel: ${ID}`,
        'credentials-file: "C:\\\\Users\\\\a b\\\\.cloudflared\\\\x.json"',
        "ingress:",
        "  - hostname: phone.example.com",
        "    service: http://127.0.0.1:47820",
        "  - service: http_status:404",
        "",
      ].join("\n"),
    );
  });
});

describe("provisionTunnel", () => {
  function harness(opts: { record?: CloudflareTunnelRecord; existing?: string[] } = {}) {
    const calls: string[] = [];
    const disk = new Set<string>(opts.existing ?? []);
    const files: TunnelFiles = { read: () => null, write: () => undefined, remove: () => undefined, exists: (f) => disk.has(f) };
    const answers: RunResult[] = [];
    const run: RunFn = (_cmd, args) => {
      calls.push(args.join(" "));
      return Promise.resolve(answers.shift() ?? ok());
    };
    const input = (extra: Partial<Parameters<typeof provisionTunnel>[0]> = {}) => ({
      run,
      binary: "/bin/cloudflared",
      files,
      certDir: "/home/u/.cloudflared",
      hostname: "phone.example.com",
      zoneId: "z1",
      seed: "ab12",
      record: opts.record,
      ...extra,
    });
    return { calls, disk, answers, input };
  }
  const created = (id = ID) => ok(`Tunnel credentials written to /home/u/.cloudflared/${id}.json.`, `Created tunnel tasktrooper-ab12 with id ${id}`);

  it("creates, then routes", async () => {
    const h = harness();
    h.disk.add(`/home/u/.cloudflared/${ID}.json`);
    h.answers.push(created(), ok(`Added CNAME phone.example.com which will route to this tunnel`));
    const result = await provisionTunnel(h.input());
    expect(h.calls).toEqual(["tunnel create tasktrooper-ab12", `tunnel route dns ${ID} phone.example.com`]);
    expect(result).toEqual({
      ok: true,
      record: { tunnelId: ID, tunnelName: "tasktrooper-ab12", credentialsFile: `/home/u/.cloudflared/${ID}.json`, zoneId: "z1", routedHostname: "phone.example.com" },
    });
  });

  it("falls back to the certificate's directory when cloudflared did not print the credentials path", async () => {
    const h = harness();
    h.disk.add(`/home/u/.cloudflared/${ID}.json`);
    h.answers.push(ok(`Created tunnel tasktrooper-ab12 with id ${ID}`), ok(`Added CNAME phone.example.com which will route to this tunnel`));
    const result = await provisionTunnel(h.input());
    expect(result).toMatchObject({ ok: true, record: { credentialsFile: `/home/u/.cloudflared/${ID}.json` } });
  });

  it("tries the next name when the first is taken", async () => {
    const h = harness();
    h.disk.add(`/home/u/.cloudflared/${ID}.json`);
    h.answers.push(bad("Tunnel create failed: a tunnel with the name tasktrooper-ab12 already exists"), created(), ok());
    const result = await provisionTunnel(h.input());
    expect(h.calls[1]).toBe("tunnel create tasktrooper-ab12-2");
    expect(result).toMatchObject({ ok: true });
  });

  it("does nothing for a tunnel it already made with the hostname already routed", async () => {
    const record: CloudflareTunnelRecord = { tunnelId: ID, tunnelName: "tasktrooper-ab12", credentialsFile: "/c.json", zoneId: "z1", routedHostname: "phone.example.com" };
    const h = harness({ record, existing: ["/c.json"] });
    expect(await provisionTunnel(h.input())).toEqual({ ok: true, record });
    expect(h.calls).toEqual([]);
  });

  it("starts over when the credentials file is gone or the login is for another zone", async () => {
    const record: CloudflareTunnelRecord = { tunnelId: ID, tunnelName: "tasktrooper-ab12", credentialsFile: "/c.json", zoneId: "z1", routedHostname: "phone.example.com" };
    const gone = harness({ record });
    gone.disk.add(`/home/u/.cloudflared/${ID}.json`);
    gone.answers.push(created(), ok());
    await provisionTunnel(gone.input());
    expect(gone.calls[0]).toBe("tunnel create tasktrooper-ab12");

    const other = harness({ record, existing: ["/c.json", `/home/u/.cloudflared/${ID}.json`] });
    other.answers.push(created(), ok());
    await provisionTunnel(other.input({ zoneId: "z2" }));
    expect(other.calls[0]).toBe("tunnel create tasktrooper-ab12");
  });

  it("reports a timeout, a missing credentials file and an unknown failure in plain words", async () => {
    const timeout = harness();
    timeout.answers.push({ code: null, lines: [], timedOut: true });
    expect(await provisionTunnel(timeout.input())).toMatchObject({ ok: false, problem: "tunnel_setup_failed", message: expect.stringContaining("did not answer") });

    const missing = harness();
    missing.answers.push(created());
    expect(await provisionTunnel(missing.input())).toMatchObject({ ok: false, message: expect.stringContaining("credentials file") });

    const failed = harness();
    failed.answers.push(bad("ERR Cannot determine default origin certificate path"));
    expect(await provisionTunnel(failed.input())).toMatchObject({ ok: false, message: expect.stringContaining("Cannot determine") });
  });

  it("gives up after a few taken names", async () => {
    const h = harness();
    for (let i = 0; i < 4; i += 1) h.answers.push(bad("already exists"));
    expect(await provisionTunnel(h.input())).toMatchObject({ ok: false, message: expect.stringContaining("free tunnel name") });
    expect(h.calls).toHaveLength(4);
  });
});
