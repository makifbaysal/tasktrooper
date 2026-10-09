import { describe, expect, it } from "vitest";
import {
  ValidationError,
  validateChatFocus,
  validateChooseDirectory,
  validateKeyRemove,
  validateKeysPrefill,
  validateKeySet,
  validateLogsStream,
  validateMcpServerSet,
  validateOpenExternal,
  validateOverrides,
  validatePreferences,
  validateRestartChild,
  validatePairRequest,
  validateReveal,
  validateSettingsPatch,
  validateSignIn,
} from "./validate.js";

const NOTIFICATIONS = {
  enabled: true,
  analizReview: true,
  humanUat: false,
  humanNeeded: true,
  agentComments: true,
  agentChatReplies: true,
};

/**
 * The three payloads that reach something with consequences: a child id that
 * selects a process to restart, a name that opens Finder, and a path this app
 * will hand to `spawn`.
 */
describe("validateRestartChild", () => {
  it("accepts the children this supervisor actually runs", () => {
    for (const child of ["embedder", "agent-server"]) {
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
      // The backend runs the hub now; there is no process here to restart.
      { child: "appium" },
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

describe("validateChatFocus", () => {
  it("accepts a matched agent and session id", () => {
    expect(validateChatFocus({ agentId: "agent-1", sessionId: "session-1" })).toEqual({
      agentId: "agent-1",
      sessionId: "session-1",
    });
  });

  it("accepts both fields null, meaning no chat screen is open", () => {
    expect(validateChatFocus({ agentId: null, sessionId: null })).toEqual({ agentId: null, sessionId: null });
  });

  it("refuses one id set without the other", () => {
    const refused: unknown[] = [
      { agentId: "agent-1", sessionId: null },
      { agentId: null, sessionId: "session-1" },
    ];
    for (const payload of refused) {
      expect(() => validateChatFocus(payload), JSON.stringify(payload)).toThrow(ValidationError);
    }
  });

  it("refuses an empty, missing or non-string id", () => {
    const refused: unknown[] = [
      undefined,
      null,
      {},
      { agentId: "", sessionId: "session-1" },
      { agentId: "agent-1", sessionId: 3 },
    ];
    for (const payload of refused) {
      expect(() => validateChatFocus(payload), JSON.stringify(payload) ?? "undefined").toThrow(ValidationError);
    }
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

describe("validateChooseDirectory", () => {
  it("accepts undefined and null as empty options", () => {
    expect(validateChooseDirectory(undefined)).toEqual({});
    expect(validateChooseDirectory(null)).toEqual({});
  });

  it("validates and trims title, defaultPath and buttonLabel", () => {
    expect(
      validateChooseDirectory({
        title: " Choose Repo ",
        defaultPath: " /Users/test/projects ",
        buttonLabel: " Open ",
      }),
    ).toEqual({
      title: "Choose Repo",
      defaultPath: "/Users/test/projects",
      buttonLabel: "Open",
    });
  });

  it("refuses control characters in options", () => {
    expect(() => validateChooseDirectory({ title: "Bad\ntitle" })).toThrow(ValidationError);
    expect(() => validateChooseDirectory({ defaultPath: "Bad\0path" })).toThrow(ValidationError);
    expect(() => validateChooseDirectory({ buttonLabel: "Bad\rlabel" })).toThrow(ValidationError);
  });
});

describe("validateLogsStream", () => {
  it("takes a boolean and nothing else", () => {
    expect(validateLogsStream({ on: true })).toEqual({ on: true });
    expect(validateLogsStream({ on: false })).toEqual({ on: false });
    for (const bad of [undefined, null, {}, { on: "yes" }, { on: 1 }, []]) {
      expect(() => validateLogsStream(bad)).toThrow(ValidationError);
    }
  });
});

/**
 * Account mode's payloads. The mode itself is never one of them: a page can
 * ask to sign in or out, and only the settings file (read through
 * `validateSettingsPatch`) carries `mode`.
 */
describe("account mode", () => {
  it("reads the mode and the origin from the settings file, and refuses anything else there", () => {
    expect(validateSettingsPatch({ mode: "account", accountOrigin: "https://app.tasktrooper.ai/" })).toEqual({
      mode: "account",
      accountOrigin: "https://app.tasktrooper.ai",
    });
    expect(() => validateSettingsPatch({ mode: "cloud" })).toThrow(ValidationError);
    expect(() => validateSettingsPatch({ accountOrigin: "https://app.tasktrooper.ai/login" })).toThrow(ValidationError);
    expect(() => validateSettingsPatch({ accountOrigin: "file:///etc" })).toThrow(ValidationError);
  });

  it("keeps the page's preferences unable to switch modes", () => {
    expect(validatePreferences({ mode: "account", accountOrigin: "https://evil.example" })).toEqual({});
  });

  it("takes an optional origin for signing in, shaped as an origin", () => {
    expect(validateSignIn(undefined)).toEqual({});
    expect(validateSignIn({})).toEqual({});
    expect(validateSignIn({ origin: "https://acme.example" })).toEqual({ origin: "https://acme.example" });
    expect(() => validateSignIn({ origin: "https://acme.example/x?y" })).toThrow(ValidationError);
    expect(() => validateSignIn({ origin: 42 })).toThrow(ValidationError);
  });

  it("checks a pairing bundle's every field before it reaches the main process", () => {
    const bundle = {
      runner_token: "rtok",
      tm_base_url: "https://app.tasktrooper.ai",
      tenant_id: "t",
      member_uid: "m",
      paired_at: "2026-10-09T00:00:00Z",
      label: "laptop",
    };
    expect(validatePairRequest({ bundle }).bundle).toEqual(bundle);
    expect(() => validatePairRequest({ bundle: { ...bundle, runner_token: "" } })).toThrow(ValidationError);
    expect(() => validatePairRequest({ bundle: { ...bundle, label: "a\nb" } })).toThrow(ValidationError);
    expect(() => validatePairRequest({})).toThrow(ValidationError);
  });
});

/**
 * The API-key window's payloads. A key is checked for shape and never quoted
 * back: every refusal names the field, not the value.
 */
describe("validateKeySet", () => {
  const KEY = "sk-ant-SHAPE-ONLY-123";

  it("keeps what a set may carry, trimming the key and dropping an empty one", () => {
    expect(validateKeySet({ type: "anthropic", api_key: `  ${KEY} `, models: ["claude-sonnet-4-5"] })).toEqual({
      type: "anthropic",
      api_key: KEY,
      models: ["claude-sonnet-4-5"],
    });
    expect(validateKeySet({ type: "openai", api_key: "" })).toEqual({ type: "openai" });
    expect(validateKeySet({ id: "8f0e7c0a-1b2c-4d5e-9f00-0123456789ab", type: "openai_compatible", base_url: " https://x.example/v1 " })).toEqual({
      id: "8f0e7c0a-1b2c-4d5e-9f00-0123456789ab",
      type: "openai_compatible",
      base_url: "https://x.example/v1",
    });
  });

  it("refuses a type this app does not offer, an agent CLI's above all", () => {
    for (const type of ["claude_code", "cursor_agent", "", 3]) {
      expect(() => validateKeySet({ type, api_key: KEY }), String(type)).toThrow(ValidationError);
    }
  });

  it("refuses a malformed field without quoting the key", () => {
    for (const payload of [
      { type: "anthropic", api_key: `${KEY}\r\nX: 1` },
      { type: "anthropic", api_key: 42 },
      { type: "anthropic", api_key: KEY, id: "-flag" },
      { type: "anthropic", api_key: KEY, models: "gpt" },
      { type: "anthropic", api_key: KEY, models: Array.from({ length: 33 }, (_, i) => `m${i}`) },
    ]) {
      let message = "";
      try {
        validateKeySet(payload);
      } catch (err) {
        message = (err as Error).message;
      }
      expect(message, JSON.stringify(payload)).not.toBe("");
      expect(message).not.toContain(KEY);
    }
  });

  it("removes by an identifier only", () => {
    expect(validateKeyRemove({ id: "openai" })).toEqual({ id: "openai" });
    expect(() => validateKeyRemove({ id: "../x" })).toThrow(ValidationError);
    expect(() => validateKeyRemove({})).toThrow(ValidationError);
  });
});

describe("validateKeysPrefill", () => {
  const UUID = "8f0e7c0a-1b2c-4d5e-9f00-0123456789ab";

  it("accepts a built-in type named by itself and a custom endpoint named by a UUID", () => {
    expect(validateKeysPrefill({ id: "anthropic", type: "anthropic" })).toEqual({ id: "anthropic", type: "anthropic" });
    expect(
      validateKeysPrefill({
        id: UUID,
        type: "openai_compatible",
        base_url: " https://llm.example/v1 ",
        models: ["m1", "m2"],
        name: " Team gateway ",
      }),
    ).toEqual({ id: UUID, type: "openai_compatible", base_url: "https://llm.example/v1", models: ["m1", "m2"], name: "Team gateway" });
  });

  it("allows http only to this computer", () => {
    for (const host of ["127.0.0.1:11434", "localhost:1234", "[::1]:8080"]) {
      expect(validateKeysPrefill({ id: UUID, type: "openai_compatible", base_url: `http://${host}/v1` }).base_url).toBe(
        `http://${host}/v1`,
      );
    }
    for (const base_url of ["http://llm.example/v1", "ftp://llm.example", "https://user:pw@llm.example/v1", "not a url"]) {
      expect(() => validateKeysPrefill({ id: UUID, type: "openai_compatible", base_url }), base_url).toThrow(ValidationError);
    }
  });

  it("refuses ids that do not match the type, and types this app does not offer", () => {
    for (const payload of [
      { id: "openai", type: "anthropic" },
      { id: "my-endpoint", type: "openai_compatible", base_url: "https://x.example" },
      { id: UUID, type: "anthropic" },
      { id: "claude_code", type: "claude_code" },
      { id: UUID, type: "openai_compatible" },
    ]) {
      expect(() => validateKeysPrefill(payload), JSON.stringify(payload)).toThrow(ValidationError);
    }
  });

  it("takes no key, and says so without quoting one", () => {
    const KEY = "sk-NEVER-IN-A-PREFILL";
    let message = "";
    try {
      validateKeysPrefill({ id: "openai", type: "openai", api_key: KEY });
    } catch (err) {
      message = (err as Error).message;
    }
    expect(message).not.toBe("");
    expect(message).not.toContain(KEY);
  });
});

describe("validateMcpServerSet", () => {
  it("accepts an ordinary name", () => {
    expect(validateMcpServerSet({ name: "notes", command: "/bin/notes" }).name).toBe("notes");
  });

  it("refuses the name TaskTrooper's own server holds, in any case", () => {
    for (const name of ["tasktrooper", "TaskTrooper", "TASKTROOPER"]) {
      expect(() => validateMcpServerSet({ name, command: "/bin/notes" })).toThrow(ValidationError);
    }
  });
});
