/** github.com URLs only, in either the https or the git@ form — the only
 * remotes a "view on GitHub" link makes sense for. */
export function githubWebUrl(remoteUrl: string | undefined): string | null {
  if (!remoteUrl) return null;
  const ssh = remoteUrl.match(/^git@github\.com:(.+?)(\.git)?$/);
  if (ssh) return `https://github.com/${ssh[1]}`;
  const https = remoteUrl.match(/^https?:\/\/github\.com\/(.+?)(\.git)?$/);
  if (https) return `https://github.com/${https[1]}`;
  return null;
}

export function githubCommitUrl(remoteUrl: string | undefined, sha: string | undefined): string | null {
  const base = githubWebUrl(remoteUrl);
  if (!base || !sha || !/^[0-9a-f]{7,40}$/i.test(sha)) return null;
  return `${base}/commit/${sha}`;
}

/** True only for a remote positively hosted somewhere other than github.com;
 * an empty or local-path remote is not flagged. */
export function isNonGithubRemote(remoteUrl: string | undefined): boolean {
  const url = remoteUrl?.trim();
  if (!url) return false;
  const scp = url.match(/^(?:[^@/]+@)?([^:/]+):(?!\/\/)/);
  const host = scp ? scp[1] : url.match(/^[a-z][a-z0-9+.-]*:\/\/(?:[^@/]*@)?([^/:]+)/i)?.[1];
  if (!host) return false;
  const h = host.toLowerCase();
  return h !== "github.com" && h !== "www.github.com";
}
