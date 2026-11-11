import type { RemoteMode, RemoteProvider, RemoteTunnelProblem } from "../../ipc/types.js";

/**
 * What the tunnel programs say, and what to run. Pure: no process, no network, so
 * the output of a real `cloudflared` or `ngrok` can be replayed in a test.
 */

export type TunnelEvent =
  | { kind: "url"; url: string }
  | { kind: "connected" }
  | { kind: "reconnecting" }
  | { kind: "api_addr"; addr: string }
  | { kind: "fatal"; problem: RemoteTunnelProblem; message: string }
  | { kind: "error"; message: string };

// eslint-disable-next-line no-control-regex -- terminal colour codes are the point.
const ANSI = /\u001b\[[0-?]*[ -/]*[@-~]/g;

/** One cloudflared console line (it logs to stderr; quick tunnels print the URL inside a box). */
export function parseCloudflaredLine(raw: string): TunnelEvent | null {
  const line = raw.replace(ANSI, "").trim();
  if (line === "") return null;

  const quick = /https:\/\/([a-z0-9][a-z0-9-]*)\.trycloudflare\.com\b/i.exec(line);
  // `api.trycloudflare.com` is the control endpoint the program talks to, never a tunnel.
  if (quick && quick[1]?.toLowerCase() !== "api") return { kind: "url", url: quick[0].toLowerCase() };

  if (/(?<!Un)Registered tunnel connection/i.test(line)) return { kind: "connected" };
  if (/Unregistered tunnel connection|Retrying connection in|Connection terminated|Unable to establish connection|failed to serve tunnel connection/i.test(line)) {
    return { kind: "reconnecting" };
  }
  if (/Unauthorized: Invalid tunnel secret|Invalid tunnel secret|Tunnel not found|tunnel .* not found|Provided Tunnel token is not valid/i.test(line)) {
    return { kind: "fatal", problem: "auth_failed", message: "Cloudflare does not accept this computer's tunnel any more. Log out of Cloudflare and log in again." };
  }
  if (/credentials file .*(?:doesn't|does not) exist|Tunnel credentials file|Cannot determine default (?:origin certificate|configuration)/i.test(line)) {
    return { kind: "fatal", problem: "tunnel_setup_failed", message: "cloudflared cannot find this computer's tunnel credentials. Log out of Cloudflare and log in again." };
  }
  if (/failed to unmarshal quick Tunnel|Error unmarshaling QuickTunnel/i.test(line)) {
    return { kind: "error", message: "Cloudflare could not create a quick tunnel just now. It retries by itself." };
  }
  if (/Unable to reach the origin service/i.test(line)) {
    return { kind: "error", message: "Cloudflare reached this computer but nothing answered on the remote access port. Is the local server running?" };
  }
  if (/\bERR\b/.test(line)) {
    const message = line.replace(/^\S+\s+ERR\s+/, "").slice(0, 200);
    return { kind: "error", message };
  }
  return null;
}

/** ngrok's well-known failure codes worth a sentence of their own. */
const NGROK_CODES: Record<string, string> = {
  ERR_NGROK_105: "ngrok does not accept this authtoken. Copy it again from the ngrok dashboard.",
  ERR_NGROK_108: "This ngrok account already has the maximum number of agent sessions running (close the other ngrok agent).",
  ERR_NGROK_334: "That ngrok domain is already online somewhere else. Stop the other agent or use another domain.",
  ERR_NGROK_4018: "ngrok needs an authtoken. Paste the one from your ngrok dashboard.",
  ERR_NGROK_4016: "ngrok rejected the authtoken. Copy it again from the ngrok dashboard.",
  ERR_NGROK_314: "ngrok does not allow that domain on this account. Use your reserved/dev domain.",
  ERR_NGROK_320: "ngrok does not know that domain for this account. Reserve it in the ngrok dashboard first.",
};

const NGROK_AUTH = new Set(["ERR_NGROK_105", "ERR_NGROK_4016", "ERR_NGROK_4018"]);

/** Reads one `key=value` / `key="quoted value"` pair out of a logfmt line. */
function logfmt(line: string, key: string): string | undefined {
  const m = new RegExp(`(?:^|\\s)${key}=(?:"((?:[^"\\\\]|\\\\.)*)"|(\\S+))`).exec(line);
  return m ? (m[1] ?? m[2]) : undefined;
}

/**
 * One ngrok agent log line. `--log-format=json` gives objects
 * (`{"lvl":"info","msg":"started tunnel","url":"https://…"}`); the logfmt form
 * (`t=… lvl=info msg="started tunnel" url=https://…`) is read too, because a
 * line that is not JSON must never be silently dropped.
 */
export function parseNgrokLine(raw: string): TunnelEvent | null {
  const line = raw.replace(ANSI, "").trim();
  if (line === "") return null;

  let fields: Record<string, string>;
  if (line.startsWith("{")) {
    try {
      const parsed = JSON.parse(line) as Record<string, unknown>;
      fields = {};
      for (const [key, value] of Object.entries(parsed)) if (typeof value === "string") fields[key] = value;
    } catch {
      fields = {};
    }
  } else {
    fields = {};
    for (const key of ["msg", "url", "addr", "err", "lvl", "obj"]) {
      const value = logfmt(line, key);
      if (value !== undefined) fields[key] = value;
    }
  }

  const code = /ERR_NGROK_\d+/.exec(fields.err ?? line)?.[0];
  if (code) {
    const known = NGROK_CODES[code];
    return {
      kind: "fatal",
      problem: NGROK_AUTH.has(code) ? "auth_failed" : "exited",
      message: known ?? `ngrok refused to start (${code}).`,
    };
  }

  const msg = fields.msg ?? "";
  if (msg === "started tunnel" && fields.url) {
    if (/^https:\/\//i.test(fields.url)) return { kind: "url", url: trimUrl(fields.url) };
    return null;
  }
  if (msg === "starting web service" && fields.addr && /^[\w.[\]:-]+:\d+$/.test(fields.addr)) {
    return { kind: "api_addr", addr: fields.addr };
  }
  if (/session closed, starting reconnect loop|failed to reconnect session|heartbeat timeout|decode error/i.test(msg)) {
    return { kind: "reconnecting" };
  }
  if (msg === "client session established" || msg === "tunnel session started") return { kind: "connected" };
  if (fields.err) return { kind: "error", message: fields.err.slice(0, 200) };
  return null;
}

function trimUrl(url: string): string {
  return url.replace(/\/+$/, "");
}

/**
 * ngrok's local API answer (`GET /api/tunnels`): the https public URL of the
 * tunnel pointing at `port`, falling back to the first https one.
 */
export function parseNgrokTunnels(body: unknown, port: number): string | null {
  if (typeof body !== "object" || body === null) return null;
  const list = (body as { tunnels?: unknown }).tunnels;
  if (!Array.isArray(list)) return null;
  const https: { url: string; addr: string }[] = [];
  for (const item of list) {
    if (typeof item !== "object" || item === null) continue;
    const t = item as { public_url?: unknown; config?: { addr?: unknown } };
    if (typeof t.public_url !== "string" || !/^https:\/\//i.test(t.public_url)) continue;
    https.push({ url: trimUrl(t.public_url), addr: typeof t.config?.addr === "string" ? t.config.addr : "" });
  }
  const exact = https.find((t) => t.addr.endsWith(`:${port}`));
  return (exact ?? https[0])?.url ?? null;
}

export function parseLine(provider: RemoteProvider, line: string): TunnelEvent | null {
  return provider === "cloudflare" ? parseCloudflaredLine(line) : parseNgrokLine(line);
}

// --- launching ----------------------------------------------------------------

export interface LaunchInput {
  provider: RemoteProvider;
  mode: RemoteMode;
  port: number;
  hostname: string;
  /** The ngrok authtoken (unused by Cloudflare, whose login is a certificate on disk). */
  secret: string;
  /** A named Cloudflare tunnel: the config file this app wrote and the tunnel it names. */
  cloudflare?: { configFile: string; tunnelId: string };
}

export interface TunnelLaunch {
  args: string[];
  /** Added to the child's environment. This is where secrets travel: never argv. */
  env: Record<string, string>;
}

export function buildLaunch(input: LaunchInput): TunnelLaunch {
  const target = `127.0.0.1:${input.port}`;
  if (input.provider === "cloudflare") {
    if (input.mode === "named" && input.cloudflare) {
      return { args: ["tunnel", "--no-autoupdate", "--config", input.cloudflare.configFile, "run", input.cloudflare.tunnelId], env: {} };
    }
    return { args: ["tunnel", "--no-autoupdate", "--url", `http://${target}`], env: {} };
  }
  const args = ["http", target, "--log=stdout", "--log-format=json"];
  if (input.mode === "named" && input.hostname !== "") args.push(`--url=https://${input.hostname}`);
  return { args, env: { NGROK_AUTHTOKEN: input.secret } };
}

// --- redaction ----------------------------------------------------------------

/** Base64 JSON tokens ("eyJh…", also what a Cloudflare certificate holds); ngrok authtokens are `<digits>_<alnum>`. */
const TOKEN_SHAPES = [/eyJ[A-Za-z0-9_-]{20,}={0,2}/g, /\b\d[A-Za-z0-9]{8,}_[A-Za-z0-9]{12,}\b/g];

/** Replaces the stored secrets and anything shaped like a tunnel token. */
export function redact(text: string, secrets: readonly string[] = []): string {
  let out = text;
  for (const secret of secrets) {
    if (secret.length >= 6) out = out.split(secret).join("[redacted]");
  }
  for (const shape of TOKEN_SHAPES) out = out.replace(shape, "[redacted]");
  return out;
}
