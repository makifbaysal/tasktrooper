import { describe, expect, it, vi } from "vitest";

vi.mock("electron", () => ({
  app: { getAppPath: () => "/app", getPath: () => "/userData", isPackaged: false },
}));

const { describeServerExit } = await import("./supervisor.js");

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
