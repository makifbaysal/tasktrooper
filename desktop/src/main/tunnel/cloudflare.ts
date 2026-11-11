import path from "node:path";
import type { RemoteTunnelProblem } from "../../ipc/types.js";
import { redact } from "./parse.js";
import type { RunResult, TunnelFiles } from "./process.js";

/**
 * Cloudflare, from "log in" to "a named tunnel on my own domain".
 *
 * `cloudflared tunnel login` opens the browser, the person picks a domain, and
 * cloudflared saves an origin certificate (`cert.pem`) bound to that one zone.
 * With that certificate this app can, on the person's behalf:
 *   1. create a named tunnel     `cloudflared tunnel create <name>`
 *   2. point a hostname at it    `cloudflared tunnel route dns <tunnel> <host>`
 *   3. run it                    `cloudflared tunnel --config <file> run <tunnel>`
 * The tunnel's ingress is written to a config file under the app's data
 * directory, so the hostname, not the whole tunnel, is what reaches the server.
 *
 * Everything here is pure or takes the disk / the process runner as an
 * argument, so the real programs' output can be replayed in a test.
 */

export interface OriginCert {
  zoneId: string;
  accountId: string;
  /** Lets the app ask Cloudflare for the zone's name. Never leaves this process. */
  apiToken: string;
}

/** Where `cloudflared tunnel login` keeps the certificate (unless TUNNEL_ORIGIN_CERT says otherwise). */
export function originCertPath(home: string, env: NodeJS.ProcessEnv): string {
  const fromEnv = env.TUNNEL_ORIGIN_CERT?.trim();
  return fromEnv ? fromEnv : path.join(home, ".cloudflared", "cert.pem");
}

/**
 * cert.pem is a PEM file with three blocks; the one called
 * `ARGO TUNNEL TOKEN` is base64 JSON `{zoneID, accountID, apiToken}`.
 */
export function parseOriginCert(pem: string): OriginCert | null {
  const block = /-----BEGIN ARGO TUNNEL TOKEN-----([\s\S]*?)-----END ARGO TUNNEL TOKEN-----/.exec(pem);
  if (!block?.[1]) return null;
  try {
    const json = JSON.parse(Buffer.from(block[1].replace(/\s+/g, ""), "base64").toString("utf8")) as Record<string, unknown>;
    const zoneId = json.zoneID ?? json.zoneId;
    const accountId = json.accountID ?? json.accountId;
    const apiToken = json.apiToken;
    if (typeof zoneId !== "string" || zoneId === "" || typeof apiToken !== "string" || apiToken === "") return null;
    return { zoneId, accountId: typeof accountId === "string" ? accountId : "", apiToken };
  } catch {
    return null;
  }
}

/** The answer of `GET /client/v4/zones/{id}`: the zone's domain name, lowercase. */
export function parseZoneName(body: unknown): string | null {
  if (typeof body !== "object" || body === null) return null;
  const result = (body as { result?: unknown }).result;
  if (typeof result !== "object" || result === null) return null;
  const name = (result as { name?: unknown }).name;
  return typeof name === "string" && /^[a-z0-9.-]+\.[a-z]{2,63}$/i.test(name) ? name.toLowerCase() : null;
}

export const SUBDOMAIN = /^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?$/;

/** What a person types as "subdomain": one lowercase DNS label, or null. */
export function normalizeSubdomain(raw: string, domain?: string): string | null {
  let sub = raw.trim().toLowerCase();
  // They may paste the whole hostname; keep the part before the domain.
  if (domain && sub.endsWith(`.${domain}`)) sub = sub.slice(0, -(domain.length + 1));
  return SUBDOMAIN.test(sub) ? sub : null;
}

export function joinHostname(subdomain: string, domain: string): string {
  return `${subdomain}.${domain}`;
}

// --- `cloudflared tunnel login` ------------------------------------------------

export type LoginEvent = { kind: "url"; url: string } | { kind: "done" } | { kind: "error"; message: string };

// eslint-disable-next-line no-control-regex -- terminal colour codes are the point.
const ANSI = /\u001b\[[0-?]*[ -/]*[@-~]/g;

export function parseLoginLine(raw: string): LoginEvent | null {
  const line = raw.replace(ANSI, "").trim();
  if (line === "") return null;
  const url = /https:\/\/(?:[a-z0-9-]+\.)*cloudflare\.com\/\S+/i.exec(line);
  if (url) return { kind: "url", url: url[0] };
  if (/successfully logged in/i.test(line)) return { kind: "done" };
  if (/existing certificate/i.test(line)) {
    return { kind: "error", message: "A Cloudflare login already exists on this computer. Log out first to choose another domain." };
  }
  if (/\bERR\b|failed to|unable to|error/i.test(line)) return { kind: "error", message: line.replace(/^(?:\S+\s+)?ERR\s+/, "").slice(0, 200) };
  return null;
}

// --- creating and routing the tunnel ------------------------------------------

export interface CreatedTunnel {
  id: string;
  name?: string;
  credentialsFile?: string;
}

/**
 * `Created tunnel NAME with id UUID` and `Tunnel credentials written to PATH.
 * cloudflared chose this file based on…`. The path may contain spaces.
 */
export function parseTunnelCreate(lines: readonly string[]): CreatedTunnel | null {
  const text = lines.join("\n").replace(ANSI, "");
  const created = /Created tunnel (\S+) with id ([0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12})/i.exec(text);
  if (!created?.[1] || !created[2]) return null;
  const file = /credentials written to (.+?\.json)/i.exec(text)?.[1];
  return { id: created[2].toLowerCase(), name: created[1], ...(file ? { credentialsFile: file } : {}) };
}

export type RouteOutcome =
  | { kind: "ok"; hostname: string | null }
  | { kind: "exists" }
  | { kind: "failed"; message: string };

/** `Added CNAME h which will route to this tunnel`, or `h is already configured to route to your tunnel`. */
export function parseRouteDns(result: RunResult): RouteOutcome {
  const text = result.lines.join("\n").replace(ANSI, "");
  if (result.code === 0) {
    const added = /Added CNAME (\S+) which will route/i.exec(text)?.[1];
    const same = /(\S+) is already configured to route to your tunnel/i.exec(text)?.[1];
    return { kind: "ok", hostname: (added ?? same ?? null)?.toLowerCase() ?? null };
  }
  if (/already exists/i.test(text)) return { kind: "exists" };
  return { kind: "failed", message: firstProblem(result.lines) };
}

/** The most useful line of a failed run: the one that says what went wrong. */
export function firstProblem(lines: readonly string[]): string {
  const cleaned = lines.map((l) => l.replace(ANSI, "").trim()).filter((l) => l !== "");
  const bad = cleaned.find((l) => /\bERR\b|error|failed|unauthorized|forbidden|denied/i.test(l)) ?? cleaned[cleaned.length - 1] ?? "cloudflared did not say why";
  return redact(bad.replace(/^(?:\S+\s+)?(?:ERR|FTL)\s+/, "")).slice(0, 240);
}

export interface CloudflareTunnelRecord {
  tunnelId: string;
  tunnelName: string;
  credentialsFile: string;
  /** The zone it was made under; another login means another account's tunnel. */
  zoneId: string;
  /** The hostname already routed to it, so `route dns` is not repeated (it refuses an existing record). */
  routedHostname?: string;
}

export type RunFn = (command: string, args: string[], timeoutMs?: number) => Promise<RunResult>;

export interface ProvisionInput {
  run: RunFn;
  binary: string;
  files: TunnelFiles;
  /** The directory of the certificate: where cloudflared writes tunnel credentials by default. */
  certDir: string;
  hostname: string;
  zoneId: string;
  /** Short stable id of this installation: `tasktrooper-<seed>` names the tunnel. */
  seed: string;
  record: CloudflareTunnelRecord | undefined;
}

export type ProvisionResult =
  | { ok: true; record: CloudflareTunnelRecord }
  | { ok: false; problem: RemoteTunnelProblem; message: string };

const MAX_NAME_TRIES = 4;

/**
 * Make sure a tunnel of ours exists and `hostname` points at it. Reuses the
 * tunnel from last time; never overwrites a DNS record that is already there.
 */
export async function provisionTunnel(input: ProvisionInput): Promise<ProvisionResult> {
  let record = input.record;
  if (!record || record.zoneId !== input.zoneId || !input.files.exists(record.credentialsFile)) {
    record = undefined;
    const base = `tasktrooper-${input.seed}`;
    for (let attempt = 1; attempt <= MAX_NAME_TRIES && !record; attempt += 1) {
      const name = attempt === 1 ? base : `${base}-${attempt}`;
      const result = await input.run(input.binary, ["tunnel", "create", name]);
      if (result.timedOut) return { ok: false, problem: "tunnel_setup_failed", message: "Cloudflare did not answer while creating the tunnel. Try again." };
      const created = result.code === 0 ? parseTunnelCreate(result.lines) : null;
      if (created) {
        const credentialsFile = created.credentialsFile ?? path.join(input.certDir, `${created.id}.json`);
        if (!input.files.exists(credentialsFile)) {
          return { ok: false, problem: "tunnel_setup_failed", message: `cloudflared made the tunnel but its credentials file is not at ${credentialsFile}.` };
        }
        record = { tunnelId: created.id, tunnelName: name, credentialsFile, zoneId: input.zoneId };
      } else if (!/already exists/i.test(result.lines.join("\n"))) {
        return { ok: false, problem: "tunnel_setup_failed", message: `Could not create the tunnel: ${firstProblem(result.lines)}` };
      }
    }
    if (!record) return { ok: false, problem: "tunnel_setup_failed", message: "Could not find a free tunnel name in your Cloudflare account." };
  }

  if (record.routedHostname !== input.hostname) {
    const routed = parseRouteDns(await input.run(input.binary, ["tunnel", "route", "dns", record.tunnelId, input.hostname]));
    if (routed.kind === "exists") {
      return { ok: false, problem: "needs_hostname", message: `${input.hostname} already has a DNS record in your Cloudflare account. Choose another subdomain.` };
    }
    if (routed.kind === "failed") {
      return { ok: false, problem: "tunnel_setup_failed", message: `Could not point ${input.hostname} at the tunnel: ${routed.message}` };
    }
    if (routed.hostname !== null && routed.hostname !== input.hostname) {
      return {
        ok: false,
        problem: "needs_hostname",
        message: `Cloudflare pointed ${routed.hostname} at the tunnel instead of ${input.hostname}: that domain is not the one you logged in with. Log out and log in with the right domain.`,
      };
    }
    record = { ...record, routedHostname: input.hostname };
  }
  return { ok: true, record };
}

/** The tunnel's config file: only `hostname` reaches the server's remote listener; anything else is a 404. */
export function cloudflareConfig(input: { tunnelId: string; credentialsFile: string; hostname: string; port: number }): string {
  return [
    `tunnel: ${input.tunnelId}`,
    `credentials-file: ${JSON.stringify(input.credentialsFile)}`,
    "ingress:",
    `  - hostname: ${input.hostname}`,
    `    service: http://127.0.0.1:${input.port}`,
    "  - service: http_status:404",
    "",
  ].join("\n");
}

export function cloudflareConfigPath(dataDir: string): string {
  return path.join(dataDir, "cloudflared", "config.yml");
}
