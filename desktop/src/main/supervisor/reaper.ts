import { execFile } from "node:child_process";
import { mkdir, readFile, rename, writeFile } from "node:fs/promises";
import { dirname } from "node:path";

export interface RecordedChild {
  id: string;
  pid: number;
  /** Executable plus arguments, as spawned. Compared against the live command line before anything is killed. */
  command: string;
  startedAt: number;
}

/** Signals a pid's whole process group; children are spawned detached so the pid is the group id. */
export function signalTree(pid: number, signal: NodeJS.Signals): void {
  try {
    process.kill(-pid, signal);
    return;
  } catch {
    // Not a group leader, or already gone.
  }
  try {
    process.kill(pid, signal);
  } catch {
    // Already gone.
  }
}

export function taskkillTree(pid: number): Promise<void> {
  return new Promise((resolve) => {
    execFile("taskkill", ["/T", "/F", "/PID", String(pid)], { windowsHide: true }, () => resolve());
  });
}

/** Forceful kill of a pid and everything under it. */
export function killTree(pid: number): Promise<void> {
  if (process.platform === "win32") return taskkillTree(pid);
  signalTree(pid, "SIGKILL");
  return Promise.resolve();
}

export class ChildRegistry {
  readonly #entries = new Map<string, RecordedChild>();
  #chain: Promise<void> = Promise.resolve();

  constructor(
    readonly path: string,
    private readonly log: (line: string) => void = () => undefined,
  ) {}

  record(entry: RecordedChild): void {
    this.#entries.set(entry.id, entry);
    this.#persist();
  }

  forget(id: string, pid: number): void {
    if (this.#entries.get(id)?.pid !== pid) return;
    this.#entries.delete(id);
    this.#persist();
  }

  /** Rewrite the file from live state, dropping every entry a previous run left. */
  persist(): Promise<void> {
    return this.#persist();
  }

  async load(): Promise<RecordedChild[]> {
    try {
      const parsed: unknown = JSON.parse(await readFile(this.path, "utf8"));
      if (!Array.isArray(parsed)) return [];
      return parsed.filter(
        (e): e is RecordedChild =>
          !!e &&
          typeof e.id === "string" &&
          Number.isInteger(e.pid) &&
          e.pid > 0 &&
          typeof e.command === "string" &&
          typeof e.startedAt === "number",
      );
    } catch {
      return [];
    }
  }

  #persist(): Promise<void> {
    const snapshot = JSON.stringify([...this.#entries.values()]);
    this.#chain = this.#chain.then(async () => {
      try {
        await mkdir(dirname(this.path), { recursive: true });
        const tmp = `${this.path}.${process.pid}.tmp`;
        await writeFile(tmp, snapshot, "utf8");
        await rename(tmp, this.path);
      } catch (err) {
        this.log(`could not write ${this.path}: ${err instanceof Error ? err.message : String(err)}`);
      }
    });
    return this.#chain;
  }
}

export interface ReaperDeps {
  platform: NodeJS.Platform;
  selfPid: number;
  /** Runs a program and returns its stdout; rejects on failure or timeout. */
  run: (file: string, args: string[]) => Promise<string>;
  isAlive: (pid: number) => boolean;
  /** Asks a process tree to stop, escalating to a forceful kill. */
  terminate: (pid: number) => Promise<void>;
}

const RUN_TIMEOUT_MS = 4_000;
const TERM_WAIT_MS = 2_000;

function run(file: string, args: string[]): Promise<string> {
  return new Promise((resolve, reject) => {
    execFile(
      file,
      args,
      { windowsHide: true, timeout: RUN_TIMEOUT_MS, maxBuffer: 8 * 1024 * 1024 },
      (err, stdout) => (err ? reject(err) : resolve(String(stdout))),
    );
  });
}

function isAlive(pid: number): boolean {
  try {
    process.kill(pid, 0);
    return true;
  } catch (err) {
    return (err as NodeJS.ErrnoException).code === "EPERM";
  }
}

async function terminate(pid: number): Promise<void> {
  if (process.platform === "win32") {
    await taskkillTree(pid);
    return;
  }
  signalTree(pid, "SIGTERM");
  const deadline = Date.now() + TERM_WAIT_MS;
  while (Date.now() < deadline) {
    if (!isAlive(pid)) return;
    await new Promise((resolve) => setTimeout(resolve, 100));
  }
  signalTree(pid, "SIGKILL");
}

export function defaultReaperDeps(): ReaperDeps {
  return { platform: process.platform, selfPid: process.pid, run, isAlive, terminate };
}

const unquote = (s: string): string => s.replaceAll('"', "");

async function commandLineOf(deps: ReaperDeps, pid: number): Promise<string | null> {
  const out =
    deps.platform === "win32"
      ? await deps.run("powershell", [
          "-NoProfile",
          "-Command",
          `(Get-CimInstance Win32_Process -Filter 'ProcessId=${pid}').CommandLine`,
        ])
      : await deps.run("ps", ["-o", "command=", "-p", String(pid)]);
  const line = out.trim();
  return line === "" ? null : line;
}

interface ProcessRow {
  pid: number;
  command: string;
}

async function listEmbedderCandidates(deps: ReaperDeps): Promise<ProcessRow[]> {
  if (deps.platform === "win32") {
    const out = await deps.run("powershell", [
      "-NoProfile",
      "-Command",
      "Get-CimInstance Win32_Process | Where-Object { $_.CommandLine -like '*index.cjs*--cache-dir*' } | Select-Object ProcessId,CommandLine | ConvertTo-Json",
    ]);
    if (out.trim() === "") return [];
    const parsed: unknown = JSON.parse(out);
    const rows = Array.isArray(parsed) ? parsed : [parsed];
    return rows.flatMap((r: { ProcessId?: unknown; CommandLine?: unknown }) =>
      typeof r?.ProcessId === "number" && typeof r.CommandLine === "string"
        ? [{ pid: r.ProcessId, command: r.CommandLine }]
        : [],
    );
  }
  const out = await deps.run("ps", ["-axo", "pid=,command="]);
  return out.split("\n").flatMap((line) => {
    const m = /^\s*(\d+)\s+(.*)$/.exec(line);
    return m ? [{ pid: Number(m[1]), command: m[2] ?? "" }] : [];
  });
}

export function isOurEmbedder(command: string, userData: string): boolean {
  const cmd = unquote(command).trim();
  if (!cmd.includes("embedder") || !cmd.includes("index.cjs")) return false;
  const flag = `--cache-dir ${unquote(userData)}`;
  const at = cmd.indexOf(flag);
  if (at < 0) return false;
  const next = cmd[at + flag.length];
  return next === undefined || /\s/.test(next);
}

export interface ReapResult {
  killed: number[];
}

/**
 * Kills children a previous run of this app left behind. A pid is only
 * touched when its live command line still matches what was recorded or the
 * embedder shape for this userData, because pids are reused. Never throws.
 */
export async function reapStaleChildren(opts: {
  registry: ChildRegistry;
  userData: string;
  deps?: ReaperDeps;
  log?: (line: string) => void;
}): Promise<ReapResult> {
  const deps = opts.deps ?? defaultReaperDeps();
  const log = opts.log ?? (() => undefined);
  const killed: number[] = [];
  const handled = new Set<number>([deps.selfPid]);

  const kill = async (pid: number, why: string): Promise<void> => {
    handled.add(pid);
    try {
      await deps.terminate(pid);
      killed.push(pid);
      log(`reaped ${why} (pid ${pid}) left by an earlier run`);
    } catch (err) {
      log(`could not reap pid ${pid}: ${err instanceof Error ? err.message : String(err)}`);
    }
  };

  try {
    const recorded = await opts.registry.load();
    for (const entry of recorded) {
      if (handled.has(entry.pid) || !deps.isAlive(entry.pid)) continue;
      try {
        const current = await commandLineOf(deps, entry.pid);
        if (current === null || !unquote(current).includes(unquote(entry.command))) continue;
        await kill(entry.pid, entry.id);
      } catch (err) {
        log(`could not inspect pid ${entry.pid}: ${err instanceof Error ? err.message : String(err)}`);
      }
    }
  } catch (err) {
    log(`reaping recorded children failed: ${err instanceof Error ? err.message : String(err)}`);
  }

  try {
    for (const row of await listEmbedderCandidates(deps)) {
      if (handled.has(row.pid) || !isOurEmbedder(row.command, opts.userData)) continue;
      await kill(row.pid, "embedder");
    }
  } catch (err) {
    log(`sweeping legacy embedders failed: ${err instanceof Error ? err.message : String(err)}`);
  }

  try {
    await opts.registry.persist();
  } catch {
    // persist() already logs through the registry.
  }
  return { killed };
}

/** `reapStaleChildren` bounded so a slow `ps` or PowerShell never holds the boot. */
export async function reapWithin(ms: number, ...args: Parameters<typeof reapStaleChildren>): Promise<void> {
  let timer: NodeJS.Timeout | undefined;
  const cap = new Promise<void>((resolve) => {
    timer = setTimeout(() => {
      args[0].log?.(`reaping stale children exceeded ${ms}ms; continuing`);
      resolve();
    }, ms);
  });
  try {
    await Promise.race([reapStaleChildren(...args).then(() => undefined), cap]);
  } catch {
    // reapStaleChildren does not throw; this is the boot's last line of defence.
  } finally {
    clearTimeout(timer);
  }
}
