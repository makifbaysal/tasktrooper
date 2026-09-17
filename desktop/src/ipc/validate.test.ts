import { describe, expect, it } from "vitest";
import {
  ValidationError,
  validateOpenExternal,
  validateOverrides,
  validatePreferences,
  validateRestartChild,
  validateReveal,
  validateSettingsPatch,
} from "./validate.js";

const NOTIFICATIONS = {
  enabled: true,
  analizReview: true,
  humanUat: false,
  humanNeeded: true,
  agentComments: true,
};

/**
 * The three payloads that reach something with consequences: a child id that
 * selects a process to restart, a name that opens Finder, and a path this app
 * will hand to `spawn`.
 */
describe("validateRestartChild", () => {
  it("accepts the children this supervisor actually runs", () => {
    for (const child of ["embedder", "agent-server", "appium"]) {
      expect(validateRestartChild({ child }).child).toBe(child);
    }
  });

  it("refuses anything else, including the children this app used to have", () => {
    const refused: unknown[] = [
      undefined,
      null,
      "agent-server",
      [],
      {},
      { child: "" },
      // Gone with the tunnel; a payload naming it must not resolve to a child.
      { child: "runner" },
      { child: "database" },
      { child: 3 },
    ];
    for (const payload of refused) {
      expect(() => validateRestartChild(payload), JSON.stringify(payload) ?? "undefined").toThrow(ValidationError);
    }
  });
});

describe("validateReveal", () => {
  /** A NAME, not a path: the main process supplies the directory. */
  it("accepts the three names and refuses a path", () => {
    expect(validateReveal({ what: "workspace" }).what).toBe("workspace");
    expect(validateReveal({ what: "logs" }).what).toBe("logs");
    expect(() => validateReveal({ what: "/etc" })).toThrow(ValidationError);
    expect(() => validateReveal({ what: "~/Documents" })).toThrow(ValidationError);
  });
});

describe("validateOpenExternal", () => {
  it("accepts a non-empty url", () => {
    expect(validateOpenExternal({ url: "https://github.com/org/repo/pull/1" })).toEqual({
      url: "https://github.com/org/repo/pull/1",
    });
  });

  it("refuses an empty, missing or non-string url", () => {
    const refused: unknown[] = [undefined, null, {}, { url: "" }, { url: "  " }, { url: 3 }, { url: ["x"] }];
    for (const payload of refused) {
      expect(() => validateOpenExternal(payload), JSON.stringify(payload) ?? "undefined").toThrow(ValidationError);
    }
  });

  it("refuses a url with a control character", () => {
    expect(() => validateOpenExternal({ url: "https://example.com\n/evil" })).toThrow(ValidationError);
  });
});

describe("validateSettingsPatch notifications", () => {
  it("accepts the full five-field object and keeps every value", () => {
    expect(validateSettingsPatch({ notifications: NOTIFICATIONS }).notifications).toEqual(NOTIFICATIONS);
  });

  it("omits notifications from the result when the patch does not mention it", () => {
    expect(validateSettingsPatch({ launchAtLogin: true }).notifications).toBeUndefined();
  });

  it("refuses a notifications object missing a field or holding a non-boolean", () => {
    expect(() => validateSettingsPatch({ notifications: { enabled: true } })).toThrow(ValidationError);
    expect(() => validateSettingsPatch({ notifications: { ...NOTIFICATIONS, enabled: "yes" } })).toThrow(
      ValidationError,
    );
  });
});

describe("validatePreferences notifications", () => {
  it("accepts the full five-field object alongside the existing switches", () => {
    const result = validatePreferences({ launchAtLogin: false, notifications: NOTIFICATIONS });
    expect(result).toEqual({ launchAtLogin: false, notifications: NOTIFICATIONS });
  });

  it("leaves launchAtLogin/autoConnect untouched when only notifications is sent", () => {
    expect(validatePreferences({ notifications: NOTIFICATIONS })).toEqual({ notifications: NOTIFICATIONS });
  });

  it("refuses an incomplete notifications object", () => {
    expect(() => validatePreferences({ notifications: { enabled: true, analizReview: true } })).toThrow(
      ValidationError,
    );
  });
});

describe("validateOverrides", () => {
  it("keeps an absolute path and lets an empty string clear one", () => {
    expect(validateOverrides({ claudeBin: "/opt/homebrew/bin/claude" })).toEqual({
      claudeBin: "/opt/homebrew/bin/claude",
    });
    expect(validateOverrides({ gitBin: "  " })).toEqual({ gitBin: "" });
  });

  it("refuses a relative path, a newline, and anything that is not a string", () => {
    expect(() => validateOverrides({ claudeBin: "claude" })).toThrow(ValidationError);
    expect(() => validateOverrides({ claudeBin: "/bin/sh\n/bin/evil" })).toThrow(ValidationError);
    expect(() => validateOverrides({ chromeBin: 7 })).toThrow(ValidationError);
  });
});
