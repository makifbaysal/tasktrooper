import { describe, expect, it, vi } from "vitest";

vi.mock("electron", () => ({
  app: { getAppPath: () => "/app", getPath: () => "/userData", isPackaged: false },
}));

const updates: Array<Record<string, unknown>> = [];
vi.mock("./child.js", () => ({
  SupervisedChild: class {
    on(): void {}
    update(spec: Record<string, unknown>): void {
      updates.push(spec);
    }
    start(): Promise<void> {
      return Promise.resolve();
    }
    markHealthy(): void {}
  },
}));

const { describeServerExit, Supervisor } = await import("./supervisor.js");

describe("embedder spec", () => {
  it("is stopped by closing stdin and watches the parent pid", async () => {
    await new Supervisor().startEmbedder();
    const spec = updates.find((u) => u["id"] === "embedder") as { stdinPipe: boolean; env: Record<string, string> };
    expect(spec.stdinPipe).toBe(true);
    expect(spec.env["TASKTROOPER_EXIT_ON_STDIN_CLOSE"]).toBe("1");
    expect(spec.env["TASKTROOPER_PARENT_PID"]).toBe(String(process.pid));
  });
});

describe("describeServerExit", () => {
  it("includes the child's last log line when there is one", () => {
    expect(describeServerExit('embedded postgres failed to start: exec: "initdb.exe": file does not exist')).toBe(
      'The local server exited before it opened a port. Its own last line: "embedded postgres failed to start: exec: "initdb.exe": file does not exist"',
    );
  });

  it("falls back to the generic sentence when there is no last line", () => {
    expect(describeServerExit(undefined)).toBe(
      "The local server exited before it opened a port. Its own last line says why.",
    );
  });
});
