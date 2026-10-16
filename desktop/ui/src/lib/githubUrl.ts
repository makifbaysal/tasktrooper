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
