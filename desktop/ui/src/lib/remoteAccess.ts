import type { DesktopRemoteProvider, DesktopRemoteMode } from "@/lib/desktop-bridge";

type Translate = (key: string, params?: Record<string, string | number>) => string;

/** The device-token lifetimes the pairing form offers, in days (0 = until revoked). */
export const PAIRING_EXPIRY_DAYS = [0, 1, 7, 30, 90] as const;

export const EXPIRY_KEYS: Record<(typeof PAIRING_EXPIRY_DAYS)[number], string> = {
  0: "never",
  1: "day1",
  7: "day7",
  30: "day30",
  90: "day90",
};

/** "3 min", "2 h", "5 d": the largest unit that is at least one. */
export function spanLabel(ms: number, t: Translate): string {
  const seconds = Math.max(0, Math.round(ms / 1000));
  if (seconds < 60) return t("remoteAccess.time.seconds", { count: seconds });
  const minutes = Math.round(seconds / 60);
  if (minutes < 60) return t("remoteAccess.time.minutes", { count: minutes });
  const hours = Math.round(minutes / 60);
  if (hours < 48) return t("remoteAccess.time.hours", { count: hours });
  return t("remoteAccess.time.days", { count: Math.round(hours / 24) });
}

/** "just now", "5 min ago", "in 3 d" relative to `now`. */
export function relativeTime(iso: string | null | undefined, now: number, t: Translate): string {
  if (!iso) return "";
  const at = Date.parse(iso);
  if (Number.isNaN(at)) return "";
  const delta = at - now;
  if (Math.abs(delta) < 45_000) return t("remoteAccess.time.now");
  const span = spanLabel(Math.abs(delta), t);
  return delta < 0 ? t("remoteAccess.time.ago", { time: span }) : t("remoteAccess.time.in", { time: span });
}

export function modeHelpKey(provider: DesktopRemoteProvider, mode: DesktopRemoteMode): string {
  return `remoteAccess.tunnel.modeHelp.${provider}${mode === "quick" ? "Quick" : "Named"}`;
}

export function providerLabel(provider: DesktopRemoteProvider): string {
  return provider === "cloudflare" ? "cloudflared" : "ngrok";
}

/** A tunnel that can carry a pairing: connected, with a public https address. */
export function pairableUrl(state: { tunnel: string; url?: string } | null): string | null {
  if (!state || state.tunnel !== "connected" || !state.url) return null;
  return /^https:\/\//i.test(state.url) ? state.url : null;
}
