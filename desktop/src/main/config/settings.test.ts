import { mkdtempSync, rmSync } from "node:fs";
import { tmpdir } from "node:os";
import path from "node:path";
import { afterEach, beforeEach, describe, expect, it } from "vitest";
import { defaultSettings, SettingsStore } from "./settings.js";

/**
 * The settings file survives a restart, so a preference saved through the
 * bridge has to round-trip exactly as it was written — including the fields
 * this change did not touch.
 */
describe("SettingsStore notifications", () => {
  let dir: string;

  beforeEach(() => {
    dir = mkdtempSync(path.join(tmpdir(), "tasktrooper-settings-"));
  });

  afterEach(() => {
    rmSync(dir, { recursive: true, force: true });
  });

  it("defaults every notification category on", () => {
    expect(defaultSettings().notifications).toEqual({
      enabled: true,
      analizReview: true,
      humanUat: true,
      humanNeeded: true,
      agentComments: true,
    });
  });

  it("round-trips a saved preference across a fresh read, as a reload would see it", () => {
    const store = new SettingsStore(dir);
    const saved = store.set({ notifications: { ...defaultSettings().notifications, humanUat: false } });
    expect(saved.notifications.humanUat).toBe(false);

    // A brand-new instance, reading the same file: what a relaunch does.
    const reopened = new SettingsStore(dir);
    expect(reopened.get().notifications).toEqual({
      enabled: true,
      analizReview: true,
      humanUat: false,
      humanNeeded: true,
      agentComments: true,
    });
  });

  it("does not disturb launchAtLogin or autoConnect when only notifications change", () => {
    const store = new SettingsStore(dir);
    store.set({ launchAtLogin: true, autoConnect: false });
    const after = store.set({ notifications: { ...defaultSettings().notifications, agentComments: false } });

    expect(after.launchAtLogin).toBe(true);
    expect(after.autoConnect).toBe(false);
  });
});
