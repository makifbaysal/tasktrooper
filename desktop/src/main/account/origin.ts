/**
 * Which origin account mode opens, and which origins it may open at all.
 *
 * The account's web app is loaded into this window with the desktop bridge
 * attached, so the origin is the boundary those powers are granted across.
 * https only — except plain http to this machine's loopback, for a control
 * plane run locally, and only in a build that is not packaged.
 */

export const DEFAULT_ACCOUNT_ORIGIN = "https://app.tasktrooper.ai";

/** Development only: points account mode at another control plane. */
export const ACCOUNT_ORIGIN_ENV = "TASKTROOPER_ACCOUNT_ORIGIN";

/** The page signing in opens, and the one a launch in account mode opens. */
export const ACCOUNT_LOGIN_ROUTE = "/login";
export const ACCOUNT_HOME_ROUTE = "/home";

export interface OriginRules {
  allowLoopbackHttp: boolean;
}

const LOOPBACK = new Set(["127.0.0.1", "localhost", "[::1]"]);

/**
 * `scheme://host[:port]` for an acceptable account origin, or null. A path,
 * query, fragment or credentials make it not an origin, and are refused
 * rather than dropped: whoever typed one meant something this app would not
 * do.
 */
export function normalizeAccountOrigin(raw: string, rules: OriginRules): string | null {
  let url: URL;
  try {
    url = new URL(raw.trim());
  } catch {
    return null;
  }
  if (url.username !== "" || url.password !== "") return null;
  if ((url.pathname !== "/" && url.pathname !== "") || url.search !== "" || url.hash !== "") return null;
  if (url.host === "") return null;
  if (url.protocol === "https:") return `https://${url.host}`;
  if (url.protocol === "http:" && rules.allowLoopbackHttp && LOOPBACK.has(url.hostname)) return `http://${url.host}`;
  return null;
}

/**
 * The origin used when nothing was chosen: the TaskTrooper cloud, or in a
 * development build `TASKTROOPER_ACCOUNT_ORIGIN` when it is an acceptable one.
 */
export function defaultAccountOrigin(opts: { packaged: boolean; env?: NodeJS.ProcessEnv }): string {
  if (opts.packaged) return DEFAULT_ACCOUNT_ORIGIN;
  const override = (opts.env ?? process.env)[ACCOUNT_ORIGIN_ENV];
  if (!override) return DEFAULT_ACCOUNT_ORIGIN;
  return normalizeAccountOrigin(override, { allowLoopbackHttp: true }) ?? DEFAULT_ACCOUNT_ORIGIN;
}

/**
 * The session partition the account's web app runs in: its cookies and
 * storage are kept apart from the local app's and cleared on sign-out, and two
 * origins never share one.
 */
export function accountPartition(origin: string): string {
  return `persist:account:${origin}`;
}

/** The origin of a URL, the same way `normalizeAccountOrigin` spells one. */
export function originOfUrl(raw: string): string | null {
  try {
    const url = new URL(raw);
    return url.host === "" ? null : `${url.protocol}//${url.host}`;
  } catch {
    return null;
  }
}
