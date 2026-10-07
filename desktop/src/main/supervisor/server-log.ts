/**
 * Turns the backend's output into the two things the supervisor needs from it:
 * the loopback address it bound, and a line worth putting in the log view.
 *
 * The address arrives as exactly one stdout line, `LISTENING
 * http://127.0.0.1:<port>`, printed once the HTTP listener is bound (see
 * `server/cmd/agent-server`). That string is quoted here rather than matched
 * loosely for the same reason the embedder's is: a wrong match hands the UI a
 * base URL that does not answer, several layers from where anyone would look.
 *
 * Everything else the backend writes is zerolog JSON on stderr, so the level is
 * lifted out and the fields are rendered flat — a raw JSON blob per line in the
 * log view is a log view nobody reads.
 */

const LISTENING_PREFIX = "LISTENING ";

export interface ParsedServerLine {
  /** The human-readable text to put in the ring buffer. */
  text: string;
  level?: string;
  /** Set only by the `LISTENING` line: the base URL the backend bound. */
  listening?: string;
}

/**
 * The base URL from a `LISTENING <url>` line, or `undefined` for any other
 * line.
 *
 * Loopback only. The backend binds 127.0.0.1 and nothing else; a line naming
 * another host is either a different program's output or a backend that was
 * built wrong, and in both cases trusting it would point the UI — and its
 * bearer token — at something off this machine.
 */
export function parseServerListening(line: string): string | undefined {
  const trimmed = line.trim();
  if (!trimmed.startsWith(LISTENING_PREFIX)) return undefined;
  const raw = trimmed.slice(LISTENING_PREFIX.length).trim();
  let url: URL;
  try {
    url = new URL(raw);
  } catch {
    return undefined;
  }
  if (url.protocol !== "http:" && url.protocol !== "https:") return undefined;
  if (url.hostname !== "127.0.0.1" && url.hostname !== "localhost") return undefined;
  if (url.port === "") return undefined;
  return `${url.protocol}//127.0.0.1:${url.port}`;
}

interface ServerLogRecord {
  level?: unknown;
  message?: unknown;
  time?: unknown;
}

export function parseServerLine(raw: string): ParsedServerLine {
  const listening = parseServerListening(raw);
  if (listening !== undefined) return { text: raw.trim(), listening };

  const trimmed = raw.trim();
  if (!trimmed.startsWith("{")) return { text: raw };

  let record: ServerLogRecord;
  try {
    record = JSON.parse(trimmed) as ServerLogRecord;
  } catch {
    // Not JSON after all. In practice this is a Go panic trace written straight
    // to the descriptor — never JSON, and exactly the line we must not swallow.
    return { text: raw };
  }

  const message = typeof record.message === "string" ? record.message : "";
  const level = typeof record.level === "string" ? record.level : undefined;
  return {
    text: message === "" ? raw : renderFields(message, record as Record<string, unknown>),
    ...(level ? { level } : {}),
  };
}

function renderFields(message: string, record: Record<string, unknown>): string {
  const extras: string[] = [];
  for (const [key, value] of Object.entries(record)) {
    if (key === "message" || key === "level" || key === "time") continue;
    if (value === null || value === undefined) continue;
    extras.push(`${key}=${typeof value === "object" ? JSON.stringify(value) : String(value)}`);
  }
  return extras.length > 0 ? `${message} ${extras.join(" ")}` : message;
}

const LEVEL_FIELD = /"level"\s*:\s*"([A-Za-z]+)"/;

/**
 * The level of a zerolog line, without parsing it. This is what runs on every
 * line as it arrives; the full parse and render (`renderServerLine`) runs only
 * when somebody reads the line.
 */
export function serverLineLevel(raw: string): string | undefined {
  const start = raw.trimStart();
  if (!start.startsWith("{")) return undefined;
  return LEVEL_FIELD.exec(start)?.[1];
}

/** A stored backend line as the log view shows it. */
export function renderServerLine(raw: string): string {
  return parseServerLine(raw).text;
}
