import { mkdtemp, readFile, rm, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { ChildRegistry, isOurEmbedder, reapStaleChildren, type ReaperDeps } from "./reaper.js";

const USER_DATA = "/Users/me/Library/Application Support/TaskTrooper";
const EMBEDDER = `/Apps/TaskTrooper.app/Contents/MacOS/TaskTrooper /Apps/TaskTrooper.app/Contents/Resources/embedder/index.cjs --cache-dir ${USER_DATA}`;

let dir: string;
let registry: ChildRegistry;
beforeEach(async () => {
  dir = await mkdtemp(join(tmpdir(), "reaper-"));
  registry = new ChildRegistry(join(dir, "children.json"));
});
afterEach(() => rm(dir, { recursive: true, force: true }));

function deps(over: Partial<ReaperDeps> & { ps?: Record<number, string>; list?: string }): {
  d: ReaperDeps;
  terminated: number[];
} {
  const terminated: number[] = [];
  const d: ReaperDeps = {
    platform: "darwin",
    selfPid: 1,
    isAlive: (pid) => over.ps?.[pid] !== undefined,
    terminate: async (pid) => {
      terminated.push(pid);
    },
    run: async (file, args) => {
      if (file === "ps" && args[0] === "-o") return over.ps?.[Number(args[3])] ?? "";
      if (file === "ps") return over.list ?? "";
      throw new Error("unexpected " + file);
    },
    ...over,
  };
  return { d, terminated };
}

describe("reapStaleChildren", () => {
  it("kills a recorded child whose command line still matches", async () => {
    await writeFile(registry.path, JSON.stringify([{ id: "embedder", pid: 50, command: "node a.js", startedAt: 1 }]));
    const { d, terminated } = deps({ ps: { 50: "node a.js --flag" } });
    await reapStaleChildren({ registry, userData: USER_DATA, deps: d });
    expect(terminated).toEqual([50]);
    expect(JSON.parse(await readFile(registry.path, "utf8"))).toEqual([]);
  });

  it("leaves a reused pid alone", async () => {
    await writeFile(registry.path, JSON.stringify([{ id: "embedder", pid: 50, command: "node a.js", startedAt: 1 }]));
    const { d, terminated } = deps({ ps: { 50: "/usr/bin/vim notes.txt" } });
    await reapStaleChildren({ registry, userData: USER_DATA, deps: d });
    expect(terminated).toEqual([]);
  });

  it("skips dead pids", async () => {
    await writeFile(registry.path, JSON.stringify([{ id: "embedder", pid: 50, command: "node a.js", startedAt: 1 }]));
    const { d, terminated } = deps({ ps: {} });
    await reapStaleChildren({ registry, userData: USER_DATA, deps: d });
    expect(terminated).toEqual([]);
  });

  it("sweeps legacy embedders for this userData only", async () => {
    const list = [
      `  10 ${EMBEDDER}`,
      `  11 ${EMBEDDER}-other`,
      `  12 /Apps/TaskTrooper.app/Contents/MacOS/TaskTrooper /x/embedder/index.cjs --cache-dir /Users/me/Library/Application Support/Other`,
      `  13 ${EMBEDDER}`,
      `  14 /usr/bin/vim index.cjs`,
    ].join("\n");
    const { d, terminated } = deps({ list, selfPid: 13 });
    await reapStaleChildren({ registry, userData: USER_DATA, deps: d });
    expect(terminated).toEqual([10]);
  });

  it("parses Windows JSON output, single object or array", async () => {
    const row = { ProcessId: 77, CommandLine: `"C:\\App\\TaskTrooper.exe" "C:\\App\\resources\\embedder\\index.cjs" --cache-dir "C:\\Users\\me\\AppData\\Roaming\\TaskTrooper"` };
    const { d, terminated } = deps({
      platform: "win32",
      run: async () => JSON.stringify(row),
    });
    await reapStaleChildren({ registry, userData: "C:\\Users\\me\\AppData\\Roaming\\TaskTrooper", deps: d });
    expect(terminated).toEqual([77]);
  });

  it("never throws and logs when the process listing fails", async () => {
    const log = vi.fn();
    const { d } = deps({
      run: async () => {
        throw new Error("ps broke");
      },
    });
    await expect(reapStaleChildren({ registry, userData: USER_DATA, deps: d, log })).resolves.toEqual({ killed: [] });
    expect(log).toHaveBeenCalled();
  });
});

describe("isOurEmbedder", () => {
  it("matches the exact userData path, spaces included", () => {
    expect(isOurEmbedder(EMBEDDER, USER_DATA)).toBe(true);
    expect(isOurEmbedder(`${EMBEDDER}2`, USER_DATA)).toBe(false);
    expect(isOurEmbedder(EMBEDDER, "/Users/me/Library/Application Support/Other")).toBe(false);
  });
});

describe("ChildRegistry", () => {
  it("writes atomically and forgets only the matching pid", async () => {
    registry.record({ id: "a", pid: 1, command: "x", startedAt: 1 });
    registry.forget("a", 2);
    await registry.persist();
    expect(JSON.parse(await readFile(registry.path, "utf8"))).toHaveLength(1);
    registry.forget("a", 1);
    await registry.persist();
    expect(JSON.parse(await readFile(registry.path, "utf8"))).toEqual([]);
  });
});
