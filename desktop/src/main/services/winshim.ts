import { existsSync, readFileSync } from "node:fs";
import path from "node:path";

/**
 * npm, pnpm and yarn install a CLI on Windows as a `.cmd` batch file that
 * re-invokes node on the package's script. Node refuses to spawn a batch file
 * with `shell: false` (EINVAL), and `shell: true` runs
 * `cmd.exe /d /s /c "<file> <args>"` with nothing quoted: a shim under
 * `C:\Program Files\nodejs` or `C:\Users\John Doe\...` does not start, an
 * argument with a space is split in two, cmd metacharacters in an argument are
 * live, and a timeout kills cmd.exe while node.exe carries on.
 *
 * So the batch file is read, and what it would have run is run directly: node
 * and the script, no shell. The two layouts that exist in the wild are both
 * understood — npm's cmd-shim (`"%_prog%" "%dp0%\...\cli.js" %*`, also what
 * pnpm and yarn write) and npm's own `npm.cmd`/`npx.cmd`
 * (`"%NODE_EXE%" "%NPX_CLI_JS%" %*`).
 */

const win = path.win32;

export interface ShimTarget {
  /** An absolute path, or the bare `node` for the caller to find on PATH the way the shim would have. */
  command: string;
  /** What the shim puts before the caller's own arguments (`%*`). */
  args: string[];
  /** Variables the shim sets for the program — pnpm's NODE_PATH. */
  env?: Record<string, string>;
}

const SET_LINE = /^\s*@?\s*set\s+(?:"([^"=]+)=([^"]*)"|([^\s"=/][^\s"=]*)=(.*?))\s*$/i;
const VAR_REF = /%([A-Za-z_][A-Za-z0-9_]*)%/;
const MAX_CANDIDATES = 16;

const isNode = (p: string): boolean => /^node(\.exe)?$/i.test(win.basename(p));
const isScript = (p: string): boolean => /\.(c|m)?js$/i.test(p);

/**
 * The tokens of the last command on a line, up to `%*`: whatever follows the
 * last unquoted `&`, `|` or `(` — which is how cmd-shim chains
 * `endLocal & goto ... || title %COMSPEC% & "%_prog%" ...`.
 */
function launchTokens(line: string): string[] {
  const before = line.slice(0, line.lastIndexOf("%*"));
  let start = 0;
  let quoted = false;
  for (let i = 0; i < before.length; i += 1) {
    const c = before[i];
    if (c === '"') quoted = !quoted;
    else if (!quoted && (c === "&" || c === "|" || c === "(")) start = i + 1;
  }
  const segment = before.slice(start).replace(/^\s*@/, "");
  const tokens: string[] = [];
  for (const m of segment.matchAll(/"([^"]*)"|([^\s"]+)/g)) tokens.push(m[1] ?? m[2] ?? "");
  return tokens.filter((t) => t !== "");
}

/**
 * Read a `.cmd`/`.bat` shim and return the program it launches, or null when
 * the file is not one of the layouts above or names a script that is not there.
 */
export function resolveWindowsShim(
  shimPath: string,
  readFile: (file: string) => string,
  exists: (file: string) => boolean,
): ShimTarget | null {
  if (!/\.(cmd|bat)$/i.test(shimPath)) return null;
  let text: string;
  try {
    text = readFile(shimPath);
  } catch {
    return null;
  }
  const dir = win.dirname(shimPath);
  const dirSlash = dir.endsWith("\\") ? dir : `${dir}\\`;
  const lines = text.split(/\r?\n/);

  // Every assignment, in order. `_prog` and `NODE_EXE` are assigned twice —
  // once to the node.exe beside the shim, once to bare `node` — and which one
  // cmd would have taken depends on a file that is checked again below.
  const vars = new Map<string, string[]>();
  for (const line of lines) {
    const m = SET_LINE.exec(line);
    if (!m) continue;
    const name = (m[1] ?? m[3] ?? "").toLowerCase();
    const value = m[2] ?? m[4] ?? "";
    vars.set(name, [...(vars.get(name) ?? []), value]);
  }

  const expand = (raw: string, depth = 0): string[] => {
    const s = raw.replace(/%~dp0/gi, () => dirSlash);
    const m = VAR_REF.exec(s);
    if (!m) return s.includes("%") ? [] : [s];
    if (depth > 8) return [];
    const name = (m[1] ?? "").toLowerCase();
    const values = vars.get(name) ?? (name === "dp0" ? [dirSlash] : []);
    const out: string[] = [];
    for (const value of values) {
      for (const resolved of expand(value, depth + 1)) {
        const next = s.slice(0, m.index) + resolved + s.slice(m.index + m[0].length);
        for (const candidate of expand(next, depth + 1)) {
          if (!out.includes(candidate)) out.push(candidate);
        }
      }
      if (out.length >= MAX_CANDIDATES) break;
    }
    return out.slice(0, MAX_CANDIDATES);
  };

  const tidy = (p: string): string => (win.isAbsolute(p) ? win.normalize(p) : p);
  const firstExisting = (candidates: string[]): string | undefined =>
    candidates.find((c) => win.isAbsolute(c) && exists(win.normalize(c)));

  const launch = [...lines].reverse().find((line) => line.includes("%*"));
  if (!launch) return null;
  const [program, ...rest] = launchTokens(launch);
  if (program === undefined) return null;

  const programs = expand(program);
  if (programs.length === 0) return null;

  const localNode = win.join(dir, "node.exe");
  const node = (): string => {
    const named = firstExisting(programs.filter(isNode));
    if (named) return tidy(named);
    return exists(localNode) ? localNode : "node";
  };

  const args: string[] = [];
  for (const token of rest) {
    const candidates = expand(token);
    const chosen = firstExisting(candidates) ?? candidates[0];
    if (chosen === undefined) return null;
    args.push(tidy(chosen));
  }

  const nodePathRaw = vars.get("node_path")?.[0];
  const nodePath = nodePathRaw === undefined ? undefined : expand(nodePathRaw)[0];
  const env = nodePath ? { env: { NODE_PATH: nodePath } } : {};

  if (programs.some(isNode)) {
    const script = args[0];
    if (script === undefined || !win.isAbsolute(script) || !exists(script)) return null;
    return { command: node(), args, ...env };
  }

  const target = firstExisting(programs);
  if (!target) return null;
  if (/\.exe$/i.test(target)) return { command: tidy(target), args, ...env };
  if (isScript(target)) return { command: node(), args: [tidy(target), ...args], ...env };
  return null;
}

// --- the last resort -----------------------------------------------------------

/** cmd's metacharacters, the set cross-spawn escapes. */
const CMD_META = /([()\][%!^"`<>&|;, *?])/g;

/**
 * One argument for `cmd.exe /d /s /c "..."` that runs a batch file which
 * forwards `%*`. Escaped twice: cmd parses the `/c` line, then parses the
 * shim's own line again after substituting `%*` into it.
 */
export function escapeCmdArgument(arg: string): string {
  let out = arg.replace(/(\\*)"/g, '$1$1\\"');
  out = out.replace(/(\\*)$/, "$1$1");
  out = `"${out}"`;
  out = out.replace(CMD_META, "^$1");
  return out.replace(CMD_META, "^$1");
}

export function escapeCmdCommand(command: string): string {
  return command.replace(CMD_META, "^$1");
}

// --- the launch --------------------------------------------------------------

export interface Launch {
  command: string;
  args: string[];
  env: NodeJS.ProcessEnv;
  /** Set only for the cmd.exe last resort, whose single argument is pre-quoted. */
  windowsVerbatimArguments?: true;
}

export interface LaunchDeps {
  readFile: (file: string) => string;
  exists: (file: string) => boolean;
}

const fsDeps: LaunchDeps = {
  readFile: (file) => readFileSync(file, "utf8"),
  exists: (file) => existsSync(file),
};

function envValue(env: NodeJS.ProcessEnv, name: string): string | undefined {
  const key = Object.keys(env).find((k) => k.toUpperCase() === name.toUpperCase());
  return key === undefined ? undefined : env[key];
}

/** `node.exe` the way cmd would have found the shim's bare `node`: on the PATH it was given. */
function nodeOnPath(env: NodeJS.ProcessEnv, exists: (file: string) => boolean): string | null {
  for (const dir of (envValue(env, "PATH") ?? "").split(";")) {
    if (dir.trim() === "") continue;
    const candidate = win.join(dir.trim(), "node.exe");
    if (exists(candidate)) return candidate;
  }
  return null;
}

/**
 * What to actually spawn for `command args` with `shell: false`.
 *
 * Anything that is not a Windows batch file is returned as it is. A batch file
 * becomes the program it launches. One this file cannot read becomes an
 * explicit `cmd.exe /d /s /c` with every argument escaped — the last resort, and
 * still not `shell: true`, whose unquoted command line is the bug this exists
 * to avoid.
 */
export function launchFor(
  command: string,
  args: string[],
  env: NodeJS.ProcessEnv,
  deps: LaunchDeps = fsDeps,
  platform: NodeJS.Platform = process.platform,
): Launch {
  if (platform !== "win32" || !/\.(cmd|bat)$/i.test(command)) return { command, args, env };

  const shim = resolveWindowsShim(command, deps.readFile, deps.exists);
  if (shim) {
    const program = shim.command === "node" ? nodeOnPath(env, deps.exists) : shim.command;
    if (program) {
      const extra = shim.env && envValue(env, "NODE_PATH") === undefined ? shim.env : {};
      return { command: program, args: [...shim.args, ...args], env: { ...env, ...extra } };
    }
  }

  const line = [escapeCmdCommand(win.normalize(command)), ...args.map(escapeCmdArgument)].join(" ");
  return {
    command: envValue(env, "ComSpec") ?? "cmd.exe",
    args: ["/d", "/s", "/c", `"${line}"`],
    env,
    windowsVerbatimArguments: true,
  };
}
