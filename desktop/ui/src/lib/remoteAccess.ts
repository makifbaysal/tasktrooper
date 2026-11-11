import type { RemoteDevice } from "@/api";
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

/** One DNS label: what a person types in front of the domain. */
const SUBDOMAIN = /^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?$/;

export function isSubdomain(value: string): boolean {
  return SUBDOMAIN.test(value);
}

/** `phone` for `phone.example.com` on `example.com`; null when the hostname is not on that domain. */
export function subdomainOf(hostname: string, domain: string): string | null {
  const suffix = `.${domain}`;
  return hostname.endsWith(suffix) && hostname.length > suffix.length ? hostname.slice(0, -suffix.length) : null;
}

/** The only phone that is connected now: not revoked, not expired. */
export function connectedDevice(devices: readonly RemoteDevice[], now: number): RemoteDevice | null {
  return devices.find((d) => !d.revoked_at && (!d.expires_at || Date.parse(d.expires_at) > now)) ?? null;
}

export function modeLabelKey(provider: DesktopRemoteProvider, mode: DesktopRemoteMode): string {
  if (mode === "quick") return "remoteAccess.tunnel.modes.quick";
  return provider === "ngrok" ? "remoteAccess.tunnel.modes.namedNgrok" : "remoteAccess.tunnel.modes.named";
}
