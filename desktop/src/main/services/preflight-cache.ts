import { readFileSync, realpathSync, statSync } from "node:fs";
import { mkdir, rename, writeFile } from "node:fs/promises";
import path from "node:path";

/**
 * What the preflight remembers between launches: the login shell's PATH, and
 * each binary's `--version` answer.
 *
 * Every one of those costs a process — a login shell reading the user's
 * profile, a Node CLI booting just to print its version — and on a warm launch
 * their answers are the same as last time. A version answer is keyed by the
 * binary's path AND by what that path resolves to, its mtime and its size, so
 * an upgrade (a new file, or a symlink moved to a new version) is a miss
 * rather than a stale hit. Shims that route to another version without
 * changing themselves are why an answer also goes stale after a day, and why a
 * forced sweep ("Check again") ignores the cache entirely.
 *
 * Best-effort throughout: a missing, unreadable or corrupt file is an empty
 * cache, and a failed write is a slower next launch, never an error.
 */

export const VERSION_FRESH_MS = 24 * 60 * 60_000;
const MAX_VERSION_ENTRIES = 64;
const MAX_OUT_CHARS = 2_048;

interface VersionEntry {
  real: string;
  mtimeMs: number;
  size: number;
  code: number;
  out: string;
  at: number;
}

interface Identity {
  real: string;
  mtimeMs: number;
  size: number;
}

export interface CachedVersion {
  /** Its exit code. */
  code: number;
  /** What `--version` printed, both streams. */
  out: string;
  /** False once the answer is old enough to be worth asking again in the background. */
  fresh: boolean;
}

function identify(binary: string): Identity | null {
  try {
    const real = realpathSync(binary);
    const stat = statSync(real);
    return { real, mtimeMs: stat.mtimeMs, size: stat.size };
  } catch {
    return null;
  }
}

function isEntry(value: unknown): value is VersionEntry {
  if (typeof value !== "object" || value === null) return false;
  const e = value as Record<string, unknown>;
  return (
    typeof e.real === "string" &&
    typeof e.mtimeMs === "number" &&
    typeof e.size === "number" &&
    typeof e.code === "number" &&
    typeof e.out === "string" &&
    typeof e.at === "number"
  );
}

export class PreflightCache {
  readonly #file: string;
  readonly #now: () => number;
  #loaded = false;
  #loginPath: string[] | undefined;
  #versions = new Map<string, VersionEntry>();
  #writing: Promise<void> = Promise.resolve();
  #queued = false;

  constructor(file: string, now: () => number = Date.now) {
    this.#file = file;
    this.#now = now;
  }

  /** The login shell's PATH as an earlier launch recorded it, if any launch did. */
  get loginPath(): string[] | undefined {
    this.#load();
    return this.#loginPath;
  }

  /** Record this launch's login PATH. True when it differs from what was recorded. */
  setLoginPath(entries: string[]): boolean {
    this.#load();
    const same =
      this.#loginPath !== undefined &&
      this.#loginPath.length === entries.length &&
      this.#loginPath.every((entry, i) => entry === entries[i]);
    if (same) return false;
    this.#loginPath = [...entries];
    this.#save();
    return true;
  }

  /** The remembered `--version` answer for `binary`, if the file is still the one that gave it. */
  version(binary: string): CachedVersion | undefined {
    this.#load();
    const entry = this.#versions.get(binary);
    if (!entry) return undefined;
    const id = identify(binary);
    if (!id || id.real !== entry.real || id.mtimeMs !== entry.mtimeMs || id.size !== entry.size) return undefined;
    return { code: entry.code, out: entry.out, fresh: entry.at > 0 && this.#now() - entry.at < VERSION_FRESH_MS };
  }

  /**
   * Remember an answer. `recheck` stores one that is never fresh: the next
   * sweep's gating half still uses it — so a launch starts the backend with
   * the same answer the last one ended on — and its complete half asks again.
   */
  setVersion(binary: string, answer: { code: number; out: string }, opts: { recheck?: boolean } = {}): void {
    this.#load();
    const id = identify(binary);
    if (!id) return;
    this.#versions.delete(binary);
    this.#versions.set(binary, {
      ...id,
      code: answer.code,
      out: answer.out.slice(0, MAX_OUT_CHARS),
      at: opts.recheck ? 0 : this.#now(),
    });
    // Insertion order is age order, so the first key is the oldest entry.
    while (this.#versions.size > MAX_VERSION_ENTRIES) {
      const oldest = this.#versions.keys().next().value;
      if (oldest === undefined) break;
      this.#versions.delete(oldest);
    }
    this.#save();
  }

  /** Drop what is remembered for `binary`, so no sweep answers from it again. */
  forgetVersion(binary: string): void {
    this.#load();
    if (this.#versions.delete(binary)) this.#save();
  }

  /** Resolves once every write asked for so far has landed (or failed). */
  flush(): Promise<void> {
    return this.#writing;
  }

  #load(): void {
    if (this.#loaded) return;
    this.#loaded = true;
    try {
      const parsed: unknown = JSON.parse(readFileSync(this.#file, "utf8"));
      if (typeof parsed !== "object" || parsed === null) return;
      const doc = parsed as { loginPath?: unknown; versions?: unknown };
      if (Array.isArray(doc.loginPath) && doc.loginPath.every((e) => typeof e === "string")) {
        this.#loginPath = doc.loginPath as string[];
      }
      if (typeof doc.versions === "object" && doc.versions !== null) {
        const entries = Object.entries(doc.versions as Record<string, unknown>).filter(
          (pair): pair is [string, VersionEntry] => isEntry(pair[1]),
        );
        entries.sort((a, b) => a[1].at - b[1].at);
        this.#versions = new Map(entries);
      }
    } catch {
      // No file yet, or one this build cannot read: an empty cache.
    }
  }

  /**
   * One write per burst: a sweep records several answers in the same tick,
   * and they land in one file write rather than one each.
   */
  #save(): void {
    if (this.#queued) return;
    this.#queued = true;
    this.#writing = this.#writing.then(async () => {
      await new Promise((resolve) => setImmediate(resolve));
      this.#queued = false;
      const body = JSON.stringify({
        v: 1,
        ...(this.#loginPath !== undefined ? { loginPath: this.#loginPath } : {}),
        versions: Object.fromEntries(this.#versions),
      });
      const tmp = `${this.#file}.${process.pid}.tmp`;
      try {
        await mkdir(path.dirname(this.#file), { recursive: true });
        await writeFile(tmp, body, "utf8");
        await rename(tmp, this.#file);
      } catch {
        // Best-effort; see the header.
      }
    });
  }
}
