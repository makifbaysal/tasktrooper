import { afterEach, describe, expect, it, vi } from "vitest";
import { loginPathEntries, loginShellArgs, parseLoginEnv } from "./login-env.js";

const BEGIN = "__TASKTROOPER_ENV_BEGIN__";
const END = "__TASKTROOPER_ENV_END__";

describe("loginShellArgs", () => {
  it("makes the POSIX family interactive and login, where nvm and friends are initialised", () => {
    for (const shell of ["/bin/zsh", "/bin/bash", "/usr/bin/dash", "/bin/sh"]) {
      const args = loginShellArgs(shell);
      expect(args?.slice(0, 3)).toEqual(["-i", "-l", "-c"]);
    }
  });

  it("gives fish its own flags", () => {
    expect(loginShellArgs("/usr/bin/fish")?.slice(0, 2)).toEqual(["-l", "-c"]);
  });

  it("does not guess at a shell whose flags it does not know", () => {
    expect(loginShellArgs("/bin/tcsh")).toBeNull();
    expect(loginShellArgs("/usr/bin/nu")).toBeNull();
  });

  it("prints its markers around the dump in every shell it runs", () => {
    const script = loginShellArgs("/bin/zsh")?.at(-1) ?? "";
    expect(script).toContain(BEGIN);
    expect(script).toContain(END);
    expect(script).toContain("env -0");
  });
});

describe("parseLoginEnv", () => {
  it("reads a NUL-delimited dump and ignores what the profile printed around it", () => {
    const out = `Welcome back!\n${BEGIN}PATH=/home/me/.nvm/versions/node/v22.11.0/bin:/usr/bin\0NVM_DIR=/home/me/.nvm\0EMPTY=\0${END}\nbye`;
    expect(parseLoginEnv(out)).toEqual({
      PATH: "/home/me/.nvm/versions/node/v22.11.0/bin:/usr/bin",
      NVM_DIR: "/home/me/.nvm",
      EMPTY: "",
    });
  });

  it("reads a newline-delimited dump from an env without -0, skipping a multi-line value's continuation", () => {
    const out = `${BEGIN}HOME=/home/me\nBASH_FUNC_x%%=() {  echo hi\n PATH=/evil\n}\nPATH=/a:/b\n${END}`;
    const env = parseLoginEnv(out);
    expect(env?.PATH).toBe("/a:/b");
    expect(env?.HOME).toBe("/home/me");
  });

  it("keeps an = inside a value", () => {
    expect(parseLoginEnv(`${BEGIN}X=a=b\0${END}`)).toEqual({ X: "a=b" });
  });

  it("is null without both markers — a shell that died part-way says nothing", () => {
    expect(parseLoginEnv("PATH=/a")).toBeNull();
    expect(parseLoginEnv(`${BEGIN}PATH=/a`)).toBeNull();
  });
});

describe("loginPathEntries", () => {
  it("keeps absolute entries only, so the cwd can never become a search directory", () => {
    expect(loginPathEntries({ PATH: "/a::.:bin:/b/c" })).toEqual(["/a", "/b/c"]);
    expect(loginPathEntries(null)).toEqual([]);
    expect(loginPathEntries({ HOME: "/h" })).toEqual([]);
  });
});

describe.skipIf(process.platform === "win32")("loginShellPath", () => {
  const shell = process.env.SHELL;
  afterEach(() => {
    process.env.SHELL = shell;
    vi.resetModules();
  });

  it("keeps a seeded PATH when the login shell fails", async () => {
    vi.resetModules();
    process.env.SHELL = "/nonexistent/zsh";
    const env = await import("./login-env.js");
    env.seedLoginShellPath(["/seeded/bin"]);
    expect(await env.loginShellPath()).toEqual(["/seeded/bin"]);
    expect(env.knownLoginShellPath()).toEqual(["/seeded/bin"]);
  });

  it("answers empty when the login shell fails and nothing was seeded", async () => {
    vi.resetModules();
    process.env.SHELL = "/nonexistent/zsh";
    const env = await import("./login-env.js");
    expect(await env.loginShellPath()).toEqual([]);
  });
});
