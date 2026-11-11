import { describe, expect, it } from "vitest";
import { buildLaunch, parseCloudflaredLine, parseLine, parseNgrokLine, parseNgrokTunnels, redact } from "./parse.js";

const CF_QUICK_OUTPUT = [
  "2026-11-11T09:00:00Z INF Thank you for trying Cloudflare Tunnel. Doing so, without a Cloudflare account, is a quick way to experiment.",
  "2026-11-11T09:00:00Z INF Requesting new quick Tunnel on trycloudflare.com...",
  "2026-11-11T09:00:02Z INF +--------------------------------------------------------------------------------------------+",
  "2026-11-11T09:00:02Z INF |  Your quick Tunnel has been created! Visit it at (it may take some time to be reachable):  |",
  "2026-11-11T09:00:02Z INF |  https://calm-fox-12.trycloudflare.com                                                     |",
  "2026-11-11T09:00:02Z INF +--------------------------------------------------------------------------------------------+",
  "2026-11-11T09:00:05Z INF Registered tunnel connection connIndex=0 connection=abc event=0 ip=198.41.200.13 location=fra07 protocol=quic",
];

describe("parseCloudflaredLine", () => {
  it("finds the quick tunnel URL inside the box and ignores the api endpoint", () => {
    const events = CF_QUICK_OUTPUT.map(parseCloudflaredLine).filter(Boolean);
    expect(events).toEqual([
      { kind: "url", url: "https://calm-fox-12.trycloudflare.com" },
      { kind: "connected" },
    ]);
    expect(parseCloudflaredLine("INF Requesting new quick Tunnel on https://api.trycloudflare.com/tunnel")).toBeNull();
  });

  it("reads the URL through terminal colour codes", () => {
    expect(parseCloudflaredLine("\u001b[90m2026\u001b[0m INF |  https://a-b-9.trycloudflare.com  |")).toEqual({
      kind: "url",
      url: "https://a-b-9.trycloudflare.com",
    });
  });

  it("treats a registered connection of a named tunnel as connected", () => {
    expect(parseCloudflaredLine("INF Registered tunnel connection connIndex=1 connection=x location=ist01 protocol=quic")).toEqual({
      kind: "connected",
    });
  });

  it("notices a dropped connection", () => {
    for (const line of [
      "WRN Retrying connection in up to 2s connIndex=0",
      "INF Unregistered tunnel connection connIndex=0",
      "ERR Connection terminated error=\"timeout\" connIndex=0",
    ]) {
      expect(parseCloudflaredLine(line), line).toEqual({ kind: "reconnecting" });
    }
  });

  it("calls a tunnel Cloudflare no longer accepts fatal, with a sentence a person can act on", () => {
    for (const line of ["ERR Unauthorized: Invalid tunnel secret", "ERR Tunnel not found", "ERR Provided Tunnel token is not valid (Error parsing tunnel ID)."]) {
      const event = parseCloudflaredLine(line);
      expect(event).toMatchObject({ kind: "fatal", problem: "auth_failed" });
      expect((event as { message: string }).message).toContain("log in again");
    }
  });

  it("calls missing tunnel credentials fatal too", () => {
    expect(parseCloudflaredLine("ERR Tunnel credentials file '/x/y.json' doesn't exist or is not a file")).toMatchObject({
      kind: "fatal",
      problem: "tunnel_setup_failed",
    });
  });

  it("keeps an unreachable origin as a non-fatal error that names the local server", () => {
    const event = parseCloudflaredLine(
      'ERR Unable to reach the origin service. The service may be down: dial tcp 127.0.0.1:47820: connect: connection refused',
    );
    expect(event).toMatchObject({ kind: "error" });
    expect(event && "message" in event ? event.message : "").toContain("local server");
  });

  it("ignores noise", () => {
    for (const line of ["", "   ", "INF Starting tunnel tunnelID=1234", "INF Settings: map[url:http://127.0.0.1:47820]"]) {
      expect(parseCloudflaredLine(line), line).toBeNull();
    }
  });
});

const NGROK_STARTED =
  '{"addr":"http://127.0.0.1:47820","lvl":"info","msg":"started tunnel","name":"command_line","obj":"tunnels","t":"2026-11-11T09:00:00Z","url":"https://odd-owl-1.ngrok-free.app"}';

describe("parseNgrokLine", () => {
  it("reads the public URL from the JSON log", () => {
    expect(parseNgrokLine(NGROK_STARTED)).toEqual({ kind: "url", url: "https://odd-owl-1.ngrok-free.app" });
  });

  it("reads the same line in logfmt", () => {
    expect(
      parseNgrokLine('t=2026-11-11T09:00:00+0000 lvl=info msg="started tunnel" obj=tunnels name=command_line addr=http://127.0.0.1:47820 url=https://odd-owl-1.ngrok-free.app'),
    ).toEqual({ kind: "url", url: "https://odd-owl-1.ngrok-free.app" });
  });

  it("learns where the agent's inspection API listens", () => {
    expect(parseNgrokLine('{"addr":"127.0.0.1:4041","lvl":"info","msg":"starting web service","obj":"web"}')).toEqual({
      kind: "api_addr",
      addr: "127.0.0.1:4041",
    });
  });

  it("maps ERR_NGROK_4018 to an authentication problem", () => {
    const line =
      '{"err":"authentication failed: Usage of ngrok requires a verified account and authtoken.\\n\\nERR_NGROK_4018\\n","lvl":"eror","msg":"command failed","t":"2026-11-11T09:00:00Z"}';
    expect(parseNgrokLine(line)).toMatchObject({ kind: "fatal", problem: "auth_failed" });
  });

  it("treats a busy domain as fatal but not an auth problem", () => {
    expect(parseNgrokLine('{"err":"The endpoint is already online. ERR_NGROK_334","lvl":"eror","msg":"command failed"}')).toMatchObject({
      kind: "fatal",
      problem: "exited",
    });
  });

  it("notices a session drop and a re-established session", () => {
    expect(parseNgrokLine('{"lvl":"info","msg":"session closed, starting reconnect loop","err":"EOF"}')).toEqual({ kind: "reconnecting" });
    expect(parseNgrokLine('{"lvl":"info","msg":"client session established","obj":"tunnels.session"}')).toEqual({ kind: "connected" });
  });

  it("does not take an http URL or a non-tunnel line for a tunnel", () => {
    expect(parseNgrokLine('{"msg":"started tunnel","url":"http://x.ngrok.app"}')).toBeNull();
    expect(parseNgrokLine("not json at all")).toBeNull();
    expect(parseNgrokLine("{broken")).toBeNull();
  });
});

describe("parseNgrokTunnels", () => {
  const body = {
    tunnels: [
      { name: "other", public_url: "https://other.ngrok.app", config: { addr: "http://localhost:3000" } },
      { name: "command_line", public_url: "https://mine.ngrok-free.app/", config: { addr: "http://127.0.0.1:47820" } },
      { name: "plain", public_url: "http://mine.ngrok-free.app", config: { addr: "http://127.0.0.1:47820" } },
    ],
  };

  it("prefers the https tunnel pointing at our port", () => {
    expect(parseNgrokTunnels(body, 47820)).toBe("https://mine.ngrok-free.app");
  });

  it("falls back to the first https tunnel and tolerates junk", () => {
    expect(parseNgrokTunnels(body, 9999)).toBe("https://other.ngrok.app");
    expect(parseNgrokTunnels({ tunnels: [] }, 1)).toBeNull();
    expect(parseNgrokTunnels(null, 1)).toBeNull();
    expect(parseNgrokTunnels({ tunnels: [null, 3, { public_url: 4 }] }, 1)).toBeNull();
  });
});

describe("parseLine", () => {
  it("routes by provider", () => {
    expect(parseLine("cloudflare", "INF Registered tunnel connection connIndex=0")).toEqual({ kind: "connected" });
    expect(parseLine("ngrok", NGROK_STARTED)).toMatchObject({ kind: "url" });
    expect(parseLine("ngrok", "INF Registered tunnel connection connIndex=0")).toBeNull();
  });
});

describe("buildLaunch", () => {
  const base = { port: 47820, hostname: "", secret: "SECRET-VALUE-123456" };

  it("runs a quick Cloudflare tunnel at the remote port with no secret anywhere", () => {
    const launch = buildLaunch({ ...base, provider: "cloudflare", mode: "quick" });
    expect(launch.args).toEqual(["tunnel", "--no-autoupdate", "--url", "http://127.0.0.1:47820"]);
    expect(launch.env).toEqual({});
  });

  it("runs a named Cloudflare tunnel from the config file this app wrote, with no secret at all", () => {
    const launch = buildLaunch({
      ...base,
      provider: "cloudflare",
      mode: "named",
      hostname: "phone.example.com",
      cloudflare: { configFile: "/data/cloudflared/config.yml", tunnelId: "0b8a3c1e-5d2f-4c7a-9e11-3f6d8a2b4c90" },
    });
    expect(launch.args).toEqual(["tunnel", "--no-autoupdate", "--config", "/data/cloudflared/config.yml", "run", "0b8a3c1e-5d2f-4c7a-9e11-3f6d8a2b4c90"]);
    expect(launch.env).toEqual({});
    expect(launch.args.join(" ")).not.toContain("SECRET");
  });

  it("starts ngrok with JSON logs on stdout and the authtoken in the environment", () => {
    const quick = buildLaunch({ ...base, provider: "ngrok", mode: "quick" });
    expect(quick.args).toEqual(["http", "127.0.0.1:47820", "--log=stdout", "--log-format=json"]);
    expect(quick.env).toEqual({ NGROK_AUTHTOKEN: "SECRET-VALUE-123456" });
    const named = buildLaunch({ ...base, provider: "ngrok", mode: "named", hostname: "phone.example.com" });
    expect(named.args).toContain("--url=https://phone.example.com");
    expect(named.args.join(" ")).not.toContain("SECRET");
  });
});

describe("redact", () => {
  it("removes the stored secret wherever it appears", () => {
    expect(redact("run --token SECRET-VALUE-123456 failed: SECRET-VALUE-123456", ["SECRET-VALUE-123456"])).toBe(
      "run --token [redacted] failed: [redacted]",
    );
  });

  it("removes anything shaped like a tunnel token or an ngrok authtoken", () => {
    const cf = "eyJhIjoiMDEyMzQ1Njc4OWFiY2RlZiIsInQiOiIxMjM0NTY3OC05YWJjLWRlZi0xMjM0LTU2Nzg5MGFiY2RlZiIsInMiOiJxd2VydHk9PSJ9";
    expect(redact(`token ${cf} bad`, [])).toBe("token [redacted] bad");
    expect(redact("authtoken 2abcDEF1234ghi_5JkLmNoPqRsTuVwXyZ12 invalid", [])).toBe("authtoken [redacted] invalid");
  });

  it("does not blank ordinary text or very short 'secrets'", () => {
    expect(redact("Registered tunnel connection", ["abc"])).toBe("Registered tunnel connection");
  });
});
