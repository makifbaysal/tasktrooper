import path from "node:path";
import { describe, expect, it } from "vitest";
import { escapeCmdArgument, launchFor, resolveWindowsShim } from "./winshim.js";

/**
 * The shims below are the files npm, Node's installer and pnpm actually write,
 * CRLF line endings included. The paths are the ones that broke `shell: true`:
 * a profile with a space in it, and Program Files.
 */

const crlf = (lines: string[]): string => `${lines.join("\r\n")}\r\n`;

/** npm's cmd-shim, as written into %APPDATA%\npm by `npm install -g @anthropic-ai/claude-code`. */
const CMD_SHIM = crlf([
  "@ECHO off",
  "GOTO start",
  ":find_dp0",
  "SET dp0=%~dp0",
  "EXIT /b",
  ":start",
  "SETLOCAL",
  "CALL :find_dp0",
  "",
  'IF EXIST "%dp0%\\node.exe" (',
  '  SET "_prog=%dp0%\\node.exe"',
  ") ELSE (",
  '  SET "_prog=node"',
  "  SET PATHEXT=%PATHEXT:;.JS;=;%",
  ")",
  "",
  'endLocal & goto #_undefined_# 2>NUL || title %COMSPEC% & "%_prog%"  "%dp0%\\node_modules\\@anthropic-ai\\claude-code\\cli.js" %*',
]);

/** Node's own npx.cmd, as installed into C:\Program Files\nodejs. */
const NPX_CMD = crlf([
  ":: Created by npm, please don't edit manually.",
  "@ECHO OFF",
  "",
  "SETLOCAL",
  "",
  'SET "NODE_EXE=%~dp0\\node.exe"',
  'IF NOT EXIST "%NODE_EXE%" (',
  '  SET "NODE_EXE=node"',
  ")",
  "",
  'SET "NPM_PREFIX_JS=%~dp0\\node_modules\\npm\\bin\\npm-prefix.js"',
  'SET "NPX_CLI_JS=%~dp0\\node_modules\\npm\\bin\\npx-cli.js"',
  "FOR /F \"delims=\" %%F IN ('CALL \"%NODE_EXE%\" \"%NPM_PREFIX_JS%\"') DO (",
  '  SET "NPM_PREFIX_NPX_CLI_JS=%%F\\node_modules\\npm\\bin\\npx-cli.js"',
  ")",
  'IF EXIST "%NPM_PREFIX_NPX_CLI_JS%" (',
  '  SET "NPX_CLI_JS=%NPM_PREFIX_NPX_CLI_JS%"',
  ")",
  "",
  '"%NODE_EXE%" "%NPX_CLI_JS%" %*',
]);

/** pnpm's (and npm 6's) older layout: a bare `node` in the ELSE branch, and NODE_PATH. */
const PNPM_SHIM = crlf([
  "@SETLOCAL",
  "@IF NOT DEFINED NODE_PATH (",
  '  @SET "NODE_PATH=C:\\Users\\John Doe\\AppData\\Local\\pnpm\\global\\5\\node_modules"',
  ") ELSE (",
  '  @SET "NODE_PATH=%NODE_PATH%;C:\\Users\\John Doe\\AppData\\Local\\pnpm\\global\\5\\node_modules"',
  ")",
  '@IF EXIST "%~dp0\\node.exe" (',
  '  "%~dp0\\node.exe"  "%~dp0\\global\\5\\node_modules\\appium\\index.js" %*',
  ") ELSE (",
  "  @SET PATHEXT=%PATHEXT:;.JS;=;%",
  '  node  "%~dp0\\global\\5\\node_modules\\appium\\index.js" %*',
  ")",
]);

function machine(files: Record<string, string | true>): {
  readFile: (file: string) => string;
  exists: (file: string) => boolean;
} {
  const norm = (p: string): string => path.win32.normalize(p).toLowerCase();
  const table = new Map(Object.entries(files).map(([k, v]) => [norm(k), v]));
  return {
    readFile: (file) => {
      const v = table.get(norm(file));
      if (typeof v !== "string") throw new Error(`ENOENT ${file}`);
      return v;
    },
    exists: (file) => table.has(norm(file)),
  };
}

const NPM_DIR = "C:\\Users\\John Doe\\AppData\\Roaming\\npm";
const CLAUDE_CMD = `${NPM_DIR}\\claude.cmd`;
const CLAUDE_JS = `${NPM_DIR}\\node_modules\\@anthropic-ai\\claude-code\\cli.js`;
const NODEJS = "C:\\Program Files\\nodejs";

describe("resolveWindowsShim: npm's cmd-shim", () => {
  it("runs node on the package script when no node.exe sits beside the shim", () => {
    const m = machine({ [CLAUDE_CMD]: CMD_SHIM, [CLAUDE_JS]: true });
    expect(resolveWindowsShim(CLAUDE_CMD, m.readFile, m.exists)).toEqual({ command: "node", args: [CLAUDE_JS] });
  });

  it("prefers the node.exe beside the shim, as its IF EXIST does", () => {
    const m = machine({ [CLAUDE_CMD]: CMD_SHIM, [CLAUDE_JS]: true, [`${NPM_DIR}\\node.exe`]: true });
    expect(resolveWindowsShim(CLAUDE_CMD, m.readFile, m.exists)).toEqual({
      command: `${NPM_DIR}\\node.exe`,
      args: [CLAUDE_JS],
    });
  });

  it("refuses a shim whose script is not there, rather than launching node on nothing", () => {
    const m = machine({ [CLAUDE_CMD]: CMD_SHIM });
    expect(resolveWindowsShim(CLAUDE_CMD, m.readFile, m.exists)).toBeNull();
  });

  it("is null for something that is not a batch file, or cannot be read", () => {
    const m = machine({});
    expect(resolveWindowsShim(`${NPM_DIR}\\claude.exe`, m.readFile, m.exists)).toBeNull();
    expect(resolveWindowsShim(CLAUDE_CMD, m.readFile, m.exists)).toBeNull();
  });
});

describe("resolveWindowsShim: npm's own npx.cmd", () => {
  it("takes the first NPX_CLI_JS that resolves, skipping the FOR loop's %%F", () => {
    const npx = `${NODEJS}\\npx.cmd`;
    const cli = `${NODEJS}\\node_modules\\npm\\bin\\npx-cli.js`;
    const m = machine({ [npx]: NPX_CMD, [cli]: true, [`${NODEJS}\\node.exe`]: true });
    expect(resolveWindowsShim(npx, m.readFile, m.exists)).toEqual({ command: `${NODEJS}\\node.exe`, args: [cli] });
  });
});

describe("resolveWindowsShim: pnpm's layout", () => {
  it("reads the bare `node` branch and carries NODE_PATH", () => {
    const dir = "C:\\Users\\John Doe\\AppData\\Local\\pnpm";
    const shim = `${dir}\\appium.cmd`;
    const script = `${dir}\\global\\5\\node_modules\\appium\\index.js`;
    const m = machine({ [shim]: PNPM_SHIM, [script]: true });
    expect(resolveWindowsShim(shim, m.readFile, m.exists)).toEqual({
      command: "node",
      args: [script],
      env: { NODE_PATH: `${dir}\\global\\5\\node_modules` },
    });
  });
});

describe("resolveWindowsShim: other targets", () => {
  it("runs a .exe target directly and a .js target under node", () => {
    const dir = "C:\\Tools";
    const m = machine({
      [`${dir}\\a.cmd`]: crlf(["@ECHO off", '"%~dp0\\bin\\a.exe" --flag %*']),
      [`${dir}\\bin\\a.exe`]: true,
      [`${dir}\\b.cmd`]: crlf(["@ECHO off", '@"%~dp0\\lib\\b.mjs" %*']),
      [`${dir}\\lib\\b.mjs`]: true,
    });
    expect(resolveWindowsShim(`${dir}\\a.cmd`, m.readFile, m.exists)).toEqual({
      command: `${dir}\\bin\\a.exe`,
      args: ["--flag"],
    });
    expect(resolveWindowsShim(`${dir}\\b.cmd`, m.readFile, m.exists)).toEqual({
      command: "node",
      args: [`${dir}\\lib\\b.mjs`],
    });
  });
});

describe("launchFor", () => {
  const env = { Path: `C:\\Windows\\system32;${NODEJS}`, ComSpec: "C:\\Windows\\system32\\cmd.exe" };

  it("leaves everything alone off Windows, and a .exe alone on it", () => {
    expect(launchFor("/usr/local/bin/claude", ["--version"], env, machine({}), "darwin")).toEqual({
      command: "/usr/local/bin/claude",
      args: ["--version"],
      env,
    });
    expect(launchFor("C:\\x\\git.exe", ["--version"], env, machine({}), "win32").command).toBe("C:\\x\\git.exe");
  });

  it("turns a shim into node plus the script, finding node on the PATH it was given", () => {
    const m = machine({ [CLAUDE_CMD]: CMD_SHIM, [CLAUDE_JS]: true, [`${NODEJS}\\node.exe`]: true });
    const got = launchFor(CLAUDE_CMD, ["-p", "say ok"], env, m, "win32");
    expect(got.command).toBe(`${NODEJS}\\node.exe`);
    // "say ok" stays one argument, which shell: true could not promise.
    expect(got.args).toEqual([CLAUDE_JS, "-p", "say ok"]);
    expect(got.windowsVerbatimArguments).toBeUndefined();
  });

  it("does not override a NODE_PATH the environment already has", () => {
    const dir = "C:\\pnpm";
    const shim = `${dir}\\appium.cmd`;
    const m = machine({
      [shim]: PNPM_SHIM,
      [`${dir}\\global\\5\\node_modules\\appium\\index.js`]: true,
      [`${dir}\\node.exe`]: true,
    });
    expect(launchFor(shim, [], env, m, "win32").env.NODE_PATH).toBe(
      "C:\\Users\\John Doe\\AppData\\Local\\pnpm\\global\\5\\node_modules",
    );
    expect(launchFor(shim, [], { ...env, NODE_PATH: "C:\\mine" }, m, "win32").env.NODE_PATH).toBe("C:\\mine");
  });

  it("falls back to an escaped cmd.exe line, never shell: true, for a shim it cannot read", () => {
    const shim = `${NODEJS}\\odd.cmd`;
    const m = machine({ [shim]: crlf(["@ECHO off", "call something-else"]) });
    const got = launchFor(shim, ["-p", "say ok"], env, m, "win32");
    expect(got.command).toBe("C:\\Windows\\system32\\cmd.exe");
    expect(got.windowsVerbatimArguments).toBe(true);
    expect(got.args.slice(0, 3)).toEqual(["/d", "/s", "/c"]);
    expect(got.args[3]).toBe('"C:\\Program^ Files\\nodejs\\odd.cmd ^^^"-p^^^" ^^^"say^^^ ok^^^""');
  });

  it("falls back the same way when the shim wants a node that is not on PATH", () => {
    const m = machine({ [CLAUDE_CMD]: CMD_SHIM, [CLAUDE_JS]: true });
    expect(launchFor(CLAUDE_CMD, [], { Path: "C:\\Windows" }, m, "win32").command).toBe("cmd.exe");
  });
});

describe("escapeCmdArgument", () => {
  it("neutralises cmd metacharacters and keeps quotes and backslashes intact", () => {
    expect(escapeCmdArgument("a&b")).toBe('^^^"a^^^&b^^^"');
    expect(escapeCmdArgument('say "hi"')).toBe('^^^"say^^^ \\^^^"hi\\^^^"^^^"');
    expect(escapeCmdArgument("C:\\dir\\")).toBe('^^^"C:\\dir\\\\^^^"');
  });
});
