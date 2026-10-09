import type { TunnelStatus } from "../../ipc/types.js";

/**
 * Turns the runner's JSON log lines into tunnel state.
 *
 * This is why the runner logs JSON and nothing else: it opens no port, so its
 * log IS its status channel. The messages are pinned to what
 * `desktop/runner/main.go` actually emits:
 *
 *   log.Info().Msg("tunnel attached")
 *   log.Info().Int("streams", …).Msg("tunnel detached")
 *   log.Warn().Err(…).Dur("uptime", …).Dur("retry_in", …)
 *       .Msg("tunnel session ended, reconnecting")
 *   log.Info().Msg("shut down cleanly")
 *
 * If those strings change, this file changes with them — matched by full
 * string rather than a loose substring, because a wrong match shows the user
 * a tunnel that is up when it is not, and that is worse than `unknown`.
 */

interface RunnerLogRecord {
  level?: unknown;
  message?: unknown;
  streams?: unknown;
  retry_in?: unknown;
  uptime?: unknown;
  error?: unknown;
}

export interface ParsedRunnerLine {
  /** The human-readable text to put in the log ring. */
  text: string;
  level?: string;
  /** Set when this line moves the tunnel state machine. */
  tunnel?: TunnelStatus;
}

export function parseRunnerLine(raw: string): ParsedRunnerLine {
  const trimmed = raw.trim();
  if (!trimmed.startsWith("{")) return { text: raw };

  let record: RunnerLogRecord;
  try {
    record = JSON.parse(trimmed) as RunnerLogRecord;
  } catch {
    // Not JSON after all — a Go panic trace or a runtime message written
    // straight to the descriptor. Never swallowed.
    return { text: raw };
  }

  const message = typeof record.message === "string" ? record.message : "";
  const level = typeof record.level === "string" ? record.level : undefined;
  const text = message === "" ? raw : renderFields(message, record);
  const now = Date.now();

  switch (message) {
    case "tunnel attached":
      return { text, ...(level ? { level } : {}), tunnel: { state: "attached", changedAt: now } };

    case "tunnel detached": {
      const streams = typeof record.streams === "number" ? record.streams : undefined;
      return {
        text,
        ...(level ? { level } : {}),
        tunnel: { state: "detached", changedAt: now, ...(streams !== undefined ? { streams } : {}) },
      };
    }

    case "tunnel session ended, reconnecting": {
      // zerolog writes a Dur field as a number in the configured unit — the
      // default TimeFieldFormat's is milliseconds. A string is accepted too so
      // a future DurationFieldUnit change degrades to "reconnecting" with no
      // countdown rather than to a NaN one.
      const retryMs = typeof record.retry_in === "number" ? record.retry_in : undefined;
      const detail = typeof record.error === "string" ? record.error : undefined;
      return {
        text,
        ...(level ? { level } : {}),
        tunnel: {
          state: "reconnecting",
          changedAt: now,
          ...(retryMs !== undefined ? { retryAt: now + retryMs } : {}),
          ...(detail !== undefined ? { detail } : {}),
        },
      };
    }

    case "shut down cleanly":
      return { text, ...(level ? { level } : {}), tunnel: { state: "detached", changedAt: now, detail: "shut down" } };

    default:
      return { text, ...(level ? { level } : {}) };
  }
}

/**
 * The one failure retrying cannot fix: the control plane rejected the token,
 * the runner logged it and exited 1. Matched on the text because it arrives
 * through `log.Fatal().Err(err)` as the error field, not as a message of its
 * own — see the runner's `authError.Error()`.
 */
export function isAuthRejection(line: string): boolean {
  return line.includes("control plane rejected the connection");
}

function renderFields(message: string, record: RunnerLogRecord): string {
  const extras: string[] = [];
  for (const [key, value] of Object.entries(record)) {
    if (key === "message" || key === "level" || key === "time") continue;
    if (value === null || value === undefined) continue;
    extras.push(`${key}=${typeof value === "object" ? JSON.stringify(value) : String(value)}`);
  }
  return extras.length > 0 ? `${message} ${extras.join(" ")}` : message;
}
