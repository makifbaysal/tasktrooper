import { parseNgrokLine, redact } from "./parse.js";
import { HttpStatusError, type TunnelSpawner } from "./process.js";

/**
 * Connecting an ngrok account.
 *
 * ngrok has no browser sign-in for its command line: the agent is authorised
 * with an authtoken the person copies from their dashboard. So "connecting"
 * means two honest things:
 *   - prove the authtoken by starting a throw-away tunnel with it (ngrok only
 *     says "started tunnel" once the account accepted the token), and
 *   - optionally, with an ngrok API key, list the account's domains. The
 *     authtoken cannot do that: ngrok's REST API wants a separate key.
 */

const PROBE_TIMEOUT_MS = 25_000;
const API = "https://api.ngrok.com";
const MAX_PAGES = 5;

export type ProbeResult = { ok: true } | { ok: false; message: string };

/**
 * Starts `ngrok http` at a port nothing listens on (the tunnel is up for a few
 * seconds and serves only ngrok's own error page), waits for "started tunnel" or
 * a refusal, and stops it. The token travels in the environment, not argv.
 */
export function probeNgrok(spawn: TunnelSpawner, binary: string, authtoken: string, timeoutMs = PROBE_TIMEOUT_MS): Promise<ProbeResult> {
  return new Promise((resolve) => {
    let done = false;
    let lastError: string | undefined;
    const proc = spawn(binary, ["http", "127.0.0.1:9", "--log=stdout", "--log-format=json"], { NGROK_AUTHTOKEN: authtoken });
    const finish = (result: ProbeResult): void => {
      if (done) return;
      done = true;
      clearTimeout(timer);
      void proc.kill();
      resolve(result);
    };
    const timer = setTimeout(() => finish({ ok: false, message: "ngrok did not answer in time. Check the internet connection and try again." }), timeoutMs);
    timer.unref?.();
    proc.onLine((line) => {
      const event = parseNgrokLine(line);
      if (!event) return;
      if (event.kind === "url" || event.kind === "connected") finish({ ok: true });
      else if (event.kind === "fatal") finish({ ok: false, message: redact(event.message, [authtoken]) });
      else if (event.kind === "error") lastError = redact(event.message, [authtoken]);
    });
    proc.onExit(() => finish({ ok: false, message: lastError ? `ngrok stopped: ${lastError}` : "ngrok stopped before it connected." }));
  });
}

/** One page of `GET /reserved_domains`: the domains and the link to the next page. */
export function parseReservedDomains(body: unknown): { domains: string[]; next: string | null } {
  if (typeof body !== "object" || body === null) return { domains: [], next: null };
  const o = body as { reserved_domains?: unknown; next_page_uri?: unknown };
  const domains: string[] = [];
  if (Array.isArray(o.reserved_domains)) {
    for (const item of o.reserved_domains) {
      const domain = typeof item === "object" && item !== null ? (item as { domain?: unknown }).domain : undefined;
      if (typeof domain === "string" && /^[a-z0-9]([a-z0-9.-]*[a-z0-9])?\.[a-z]{2,63}$/i.test(domain)) domains.push(domain.toLowerCase());
    }
  }
  const next = typeof o.next_page_uri === "string" && o.next_page_uri.startsWith(`${API}/`) ? o.next_page_uri : null;
  return { domains, next };
}

export type FetchJson = (url: string, headers?: Record<string, string>) => Promise<unknown>;

/** Every reserved domain of the account (the free dev domain included), sorted. Throws a sentence a person can act on. */
export async function listNgrokDomains(fetchJson: FetchJson, apiKey: string): Promise<string[]> {
  const headers = { Authorization: `Bearer ${apiKey}`, "Ngrok-Version": "2", Accept: "application/json" };
  const all = new Set<string>();
  let url: string | null = `${API}/reserved_domains?limit=100`;
  for (let page = 0; url && page < MAX_PAGES; page += 1) {
    let body: unknown;
    try {
      body = await fetchJson(url, headers);
    } catch (err) {
      if (err instanceof HttpStatusError && (err.status === 401 || err.status === 403)) {
        throw new Error("ngrok did not accept that API key. Create one at dashboard.ngrok.com → API and paste it again.");
      }
      throw new Error(`Could not reach the ngrok API (${err instanceof Error ? err.message : String(err)}).`);
    }
    const parsed = parseReservedDomains(body);
    for (const domain of parsed.domains) all.add(domain);
    url = parsed.next;
  }
  return [...all].sort();
}
