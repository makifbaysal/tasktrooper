import path from "node:path";
import { mergePath, pathKeyOf, splitPath } from "../services/process-env.js";
import { runProbe } from "../services/detect.js";

/**
 * The PATH a new Windows session would get, read back into this process.
 *
 * A winget install adds its directory to the machine or user Path in the
 * registry; a process that is already running keeps the PATH it started with,
 * so without this the tool just installed is "not found" until the app is
 * restarted — exactly the moment the job checks for it.
 */

const MACHINE_KEY = "HKLM\\SYSTEM\\CurrentControlSet\\Control\\Session Manager\\Environment";
const USER_KEY = "HKCU\\Environment";

/** The `Path` value out of `reg query <key> /v Path`, or null when the output has none. */
export function parseRegPath(output: string): string | null {
  for (const line of output.split(/\r?\n/)) {
    const m = /^\s+Path\s+REG_(?:EXPAND_)?SZ\s+(.*)$/i.exec(line);
    if (m) return (m[1] ?? "").trim();
  }
  return null;
}

/** `%SystemRoot%\system32` → `C:\Windows\system32`. An unknown variable is left as written, like cmd does. */
export function expandWindowsVars(value: string, env: Readonly<NodeJS.ProcessEnv>): string {
  return value.replace(/%([^%]+)%/g, (whole, name: string) => {
    const key = Object.keys(env).find((k) => k.toUpperCase() === name.toUpperCase());
    return key === undefined ? whole : (env[key] ?? whole);
  });
}

/** This process's own entries first, then what the registry added since — nothing that resolved before moves. */
export function refreshedPath(current: string, machine: string | null, user: string | null, env: Readonly<NodeJS.ProcessEnv>): string {
  const expand = (v: string | null): string[] => (v === null ? [] : splitPath(expandWindowsVars(v, env), ";"));
  return mergePath([splitPath(current, ";"), expand(machine), expand(user)], "win32").join(";");
}

async function readKey(reg: string, key: string): Promise<string | null> {
  const answer = await runProbe(reg, ["query", key, "/v", "Path"]);
  return answer.code === 0 ? parseRegPath(answer.out) : null;
}

export async function refreshWindowsPath(env: NodeJS.ProcessEnv = process.env): Promise<void> {
  if (process.platform !== "win32") return;
  const reg = path.win32.join(env.SystemRoot ?? "C:\\Windows", "System32", "reg.exe");
  const [machine, user] = await Promise.all([readKey(reg, MACHINE_KEY), readKey(reg, USER_KEY)]);
  const key = pathKeyOf(env);
  env[key] = refreshedPath(env[key] ?? "", machine, user, env);
}
