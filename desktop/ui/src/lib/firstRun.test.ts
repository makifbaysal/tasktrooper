import { beforeEach, describe, expect, it } from "vitest";
import {
  FIRST_RUN_MODE_KEY,
  FIRST_RUN_TEAM_DONE_KEY,
  FIRST_RUN_VERSION,
  FIRST_RUN_VERSION_KEY,
  readFirstRunIntegrationsDone,
  readFirstRunToolchainDone,
  writeFirstRunMode,
  writeFirstRunTeamDone,
} from "@/lib/firstRun";

beforeEach(() => {
  window.localStorage.clear();
});

describe("first-run version marker", () => {
  it("treats an install that answered the first run before this version as past the new screens", () => {
    window.localStorage.setItem(FIRST_RUN_MODE_KEY, "local");
    window.localStorage.setItem(FIRST_RUN_TEAM_DONE_KEY, "1");

    expect(readFirstRunToolchainDone()).toBe(true);
    expect(readFirstRunIntegrationsDone()).toBe(true);
    expect(window.localStorage.getItem(FIRST_RUN_VERSION_KEY)).toBe(FIRST_RUN_VERSION);
  });

  it("still asks an install whose mode this version wrote", () => {
    writeFirstRunMode("local");
    window.localStorage.setItem(FIRST_RUN_TEAM_DONE_KEY, "1");

    expect(window.localStorage.getItem(FIRST_RUN_VERSION_KEY)).toBe(FIRST_RUN_VERSION);
    expect(readFirstRunToolchainDone()).toBe(false);
    expect(readFirstRunIntegrationsDone()).toBe(false);
  });

  it("leaves a brand-new install alone until it answers", () => {
    expect(readFirstRunToolchainDone()).toBe(false);
    expect(readFirstRunIntegrationsDone()).toBe(false);
    expect(window.localStorage.getItem(FIRST_RUN_VERSION_KEY)).toBeNull();

    writeFirstRunMode("account");
    expect(readFirstRunToolchainDone()).toBe(false);
  });

  it("treats a page that only ever stored the team answer before this version as past the new screens", () => {
    window.localStorage.setItem(FIRST_RUN_TEAM_DONE_KEY, "1");

    expect(readFirstRunToolchainDone()).toBe(true);
    expect(readFirstRunIntegrationsDone()).toBe(true);
  });

  it("still asks a page that answered only the team with this version", () => {
    writeFirstRunTeamDone();

    expect(window.localStorage.getItem(FIRST_RUN_VERSION_KEY)).toBe(FIRST_RUN_VERSION);
    expect(readFirstRunToolchainDone()).toBe(false);
    expect(readFirstRunIntegrationsDone()).toBe(false);
  });
});
