/**
 * Whether this install uses mobile automation, asked of the running backend.
 *
 * The backend dials the Appium hub itself, from the `mobile_*` tools and its
 * device probes, so there is no request this app sees that says "the hub is
 * needed now". What it can see is the data those calls follow from: a
 * registered device (the only thing the hub is ever dialled for) or a mobile
 * repository (where the first device is about to come from).
 *
 * Fails OPEN: an answer it cannot read counts as "in use", which starts the
 * hub — what every install did before this gate existed.
 */

const REQUEST_TIMEOUT_MS = 3_000;

interface RepoLike {
  kind?: unknown;
  sub_repo_kinds?: unknown;
  sub_projects?: unknown;
}

const MOBILE = "mobile";

export function reposUseMobile(body: unknown): boolean {
  const repos = (body as { repositories?: unknown } | null)?.repositories;
  if (!Array.isArray(repos)) return false;
  return repos.some((raw: RepoLike | null) => {
    if (!raw || typeof raw !== "object") return false;
    if (raw.kind === MOBILE) return true;
    if (Array.isArray(raw.sub_repo_kinds) && raw.sub_repo_kinds.includes(MOBILE)) return true;
    return (
      Array.isArray(raw.sub_projects) &&
      raw.sub_projects.some((p: { kind?: unknown } | null) => !!p && typeof p === "object" && p.kind === MOBILE)
    );
  });
}

export function devicesRegistered(body: unknown): boolean {
  const devices = (body as { devices?: unknown } | null)?.devices;
  return Array.isArray(devices) && devices.length > 0;
}

async function read(url: string, token: string, fetchImpl: typeof fetch): Promise<unknown> {
  const res = await fetchImpl(url, {
    headers: { Authorization: `Bearer ${token}` },
    signal: AbortSignal.timeout(REQUEST_TIMEOUT_MS),
  });
  if (!res.ok) throw new Error(`${url} answered ${res.status}`);
  return res.json();
}

/**
 * Repositories first: a plain database read. The device list is asked only
 * when no repository is mobile, because answering it probes each device.
 */
export async function mobileAutomationInUse(base: string, token: string, fetchImpl: typeof fetch = fetch): Promise<boolean> {
  try {
    if (reposUseMobile(await read(`${base}/v1/repositories`, token, fetchImpl))) return true;
    return devicesRegistered(await read(`${base}/v1/settings/mobile-devices`, token, fetchImpl));
  } catch {
    return true;
  }
}
