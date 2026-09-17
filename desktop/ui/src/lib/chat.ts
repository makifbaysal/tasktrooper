import type { SessionMessage } from "@/api";

// The backend writes failed agent runs into the transcript with this prefix, and
// the chat bubble styles itself off it. "**Hata:**" is the pre-English-UI marker
// — still recognized so transcripts saved before the switch keep rendering as
// errors rather than as ordinary assistant replies.
const ERROR_PREFIX = "**Error:**";
const LEGACY_ERROR_PREFIX = "**Hata:**";

export function isAssistantErrorMessage(content: string): boolean {
  return content.startsWith(ERROR_PREFIX) || content.startsWith(LEGACY_ERROR_PREFIX);
}

// A provider rate limit is written into the transcript under its own marker, so
// the bubble can be a warning the user can act on instead of a red failure. The
// sentence after the marker is already user-facing text from the server — it
// never carries the provider's JSON or the run's attempt trace.
const RATE_LIMIT_PREFIX = "**Rate limit:**";

/** The stream error frame's `type` for a provider rate limit. */
export const RATE_LIMIT_ERROR_TYPE = "rate_limited";

export function isRateLimitMessage(content: string): boolean {
  return content.startsWith(RATE_LIMIT_PREFIX);
}

/** The message body without its marker, for rendering under a warning heading. */
export function rateLimitMessageBody(content: string): string {
  return content.slice(RATE_LIMIT_PREFIX.length).trim();
}

// A quota-blocked turn that WAS queued gets its own marker, distinct from a
// bare rate limit: it is not asking the reader to do anything (see
// session.Service.parkTurnOnQuota on the server) — a SessionQuotaSweeper
// reruns it once the limit lifts and the real reply lands on its own.
const QUOTA_QUEUED_PREFIX = "**Queued:**";

/** The stream/JSON error frame's `type` for a chat turn that was queued. */
export const QUOTA_QUEUED_ERROR_TYPE = "quota_queued";

export function isQuotaQueuedMessage(content: string): boolean {
  return content.startsWith(QUOTA_QUEUED_PREFIX);
}

/** The message body without its marker, for rendering under an info heading. */
export function quotaQueuedMessageBody(content: string): string {
  return content.slice(QUOTA_QUEUED_PREFIX.length).trim();
}

export function assistantErrorMessage(text: string): SessionMessage {
  return {
    id: crypto.randomUUID(),
    role: "assistant",
    content: `${ERROR_PREFIX} ${text}`,
    created_at: new Date().toISOString(),
  };
}
