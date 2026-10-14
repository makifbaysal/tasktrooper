import { createHash, type Hash } from "node:crypto";
import { createWriteStream } from "node:fs";
import { mkdir, rename, rm } from "node:fs/promises";
import path from "node:path";
import { once } from "node:events";
import { CancelledError, InstallError, type DownloadOptions } from "./types.js";

/**
 * A download for an installer: https only (redirects included), streamed to
 * `<dest>.part` and renamed into place only once it is whole and, when the
 * table pins one, its digest matches. A file that does not match is deleted,
 * never run.
 *
 * The fetch is injected: the app passes Electron's `net.fetch`, which goes
 * through the system's (or a company's) proxy the way the browser does.
 * A connection that goes quiet is given up after `DOWNLOAD_IDLE_MS` with no
 * bytes, rather than holding the job's one lane for good.
 */

export const DOWNLOAD_IDLE_MS = 60_000;

export type FetchLike = (url: string, init: { signal: AbortSignal; redirect: "follow" }) => Promise<Response>;

export interface FetchFileRequest extends DownloadOptions {
  signal: AbortSignal;
  fetch?: FetchLike;
  idleMs?: number;
}

function digestMismatch(name: string): InstallError {
  return new InstallError(`The download of ${name} did not match its published checksum and was deleted.`);
}

export async function fetchFile(url: string, dest: string, req: FetchFileRequest): Promise<void> {
  const parsed = new URL(url);
  if (parsed.protocol !== "https:") throw new InstallError(`Refusing to download ${url}: not https.`);
  const name = path.posix.basename(parsed.pathname) || parsed.host;
  const doFetch: FetchLike = req.fetch ?? ((u, init) => fetch(u, init));
  const idleMs = req.idleMs ?? DOWNLOAD_IDLE_MS;
  const quiet = new AbortController();
  const signal = AbortSignal.any([req.signal, quiet.signal]);
  let timer: ReturnType<typeof setTimeout> | undefined;
  const heard = (): void => {
    clearTimeout(timer);
    timer = setTimeout(() => quiet.abort(), idleMs);
  };
  const failure = (err: unknown): Error => {
    if (req.signal.aborted) return new CancelledError();
    if (quiet.signal.aborted) return new InstallError(`The download of ${name} stalled: nothing arrived for ${Math.round(idleMs / 1000)} seconds.`);
    if (err instanceof InstallError || err instanceof CancelledError) return err;
    return new InstallError(`Could not download ${name}: ${err instanceof Error ? err.message : String(err)}`);
  };

  heard();
  try {
    await transfer({ url, dest, name, req, fetch: doFetch, signal, heard, failure });
  } finally {
    clearTimeout(timer);
  }
}

interface Transfer {
  url: string;
  dest: string;
  name: string;
  req: FetchFileRequest;
  fetch: FetchLike;
  /** The caller's cancel and the idle timeout, together. */
  signal: AbortSignal;
  /** Bytes (or headers) arrived: the idle clock starts over. */
  heard: () => void;
  failure: (err: unknown) => Error;
}

async function transfer({ url, dest, name, req, fetch: doFetch, signal, heard, failure }: Transfer): Promise<void> {
  let res: Response;
  try {
    res = await doFetch(url, { signal, redirect: "follow" });
  } catch (err) {
    throw failure(err);
  }
  heard();
  if (res.url !== "" && !res.url.startsWith("https:")) throw new InstallError(`Refusing ${name}: the download was redirected off https.`);
  if (!res.ok || !res.body) throw new InstallError(`Could not download ${name}: HTTP ${res.status}.`);

  const total = Number.parseInt(res.headers.get("content-length") ?? "", 10);
  const hashes: [Hash, string][] = [];
  if (req.sha1) hashes.push([createHash("sha1"), req.sha1.toLowerCase()]);
  if (req.sha256) hashes.push([createHash("sha256"), req.sha256.toLowerCase()]);

  await mkdir(path.dirname(dest), { recursive: true });
  const part = `${dest}.part`;
  const out = createWriteStream(part);
  let received = 0;
  let lastPercent = -1;
  try {
    for await (const chunk of res.body as unknown as AsyncIterable<Uint8Array>) {
      heard();
      if (signal.aborted) throw failure(undefined);
      for (const [hash] of hashes) hash.update(chunk);
      received += chunk.length;
      if (!out.write(chunk)) await once(out, "drain");
      if (Number.isFinite(total) && total > 0) {
        const percent = Math.min(100, Math.floor((received * 100) / total));
        if (percent !== lastPercent) {
          lastPercent = percent;
          req.onPercent?.(percent);
        }
      }
    }
    await new Promise<void>((resolve, reject) => out.end((err?: Error | null) => (err ? reject(err) : resolve())));
  } catch (err) {
    out.destroy();
    await rm(part, { force: true });
    throw failure(err);
  }

  for (const [hash, expected] of hashes) {
    if (hash.digest("hex") !== expected) {
      await rm(part, { force: true });
      throw digestMismatch(name);
    }
  }
  await rename(part, dest);
}
