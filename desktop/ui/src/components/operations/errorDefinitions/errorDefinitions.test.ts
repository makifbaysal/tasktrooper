import { describe, expect, it } from "vitest";
import type { ErrorDefinition } from "@/api";
import {
  DEFAULT_THRESHOLDS,
  compileForBrowser,
  definitionInput,
  definitionMatches,
  definitionPath,
  draftFromDefinition,
  draftFromGroup,
  emptyDefinitionDraft,
  mapDefinitionFields,
  matchClauses,
  matchSummary,
  statusCounts,
  suggestMessageRegex,
  switchAction,
  testPattern,
  thresholdText,
  validateDefinitionDraft,
  type DefinitionDraft,
} from "@/components/operations/errorDefinitions/errorDefinitions";
import { en } from "@/locales/en";

function lookup(key: string): string | undefined {
  let node: unknown = en;
  for (const part of key.split(".")) {
    if (!node || typeof node !== "object") return undefined;
    node = (node as Record<string, unknown>)[part];
  }
  return typeof node === "string" ? node : undefined;
}

const t = (key: string, params?: Record<string, string | number>) =>
  (lookup(key) ?? key).replace(/\{(\w+)\}/g, (m, name: string) => (params && name in params ? String(params[name]) : m));
const ctx = { locale: "en", t };

function definition(overrides: Partial<ErrorDefinition> = {}): ErrorDefinition {
  return {
    id: "d-1",
    repository_id: "r-shop",
    component_id: null,
    name: "Stripe timeouts",
    match: { message_regex: "context deadline exceeded.*stripe" },
    category: "dependency",
    severity: "medium",
    action: "none",
    alert_threshold: null,
    source: "human",
    status: "active",
    rationale: "",
    evidence: [],
    task_id: null,
    firing_since: null,
    created_at: "2026-10-17T10:00:00Z",
    updated_at: "2026-10-17T10:00:00Z",
    ...overrides,
  };
}

function draft(overrides: Partial<DefinitionDraft> = {}): DefinitionDraft {
  return { ...emptyDefinitionDraft("r-shop"), name: "Stripe timeouts", messageRegex: "deadline", category: "dependency", ...overrides };
}

describe("matchSummary", () => {
  it("reads a single pattern back in words", () => {
    expect(matchSummary({ message_regex: "context deadline exceeded.*stripe" }, ctx)).toBe(
      "message matches /context deadline exceeded.*stripe/",
    );
  });

  it("joins every field that is set, the fingerprint last and shortened", () => {
    const summary = matchSummary(
      {
        fingerprint: "0123456789abcdef0123",
        message_regex: "timeout",
        exception_type: "NullPointerException",
        endpoint: "POST /v1/charges",
        status: 502,
        attributes: { region: "eu" },
      },
      ctx,
    );
    expect(summary).toBe(
      "message matches /timeout/, exception type is NullPointerException, endpoint is POST /v1/charges, status is 502, region is eu, and fingerprint is 0123456789ab…",
    );
  });

  it("says so for an empty match", () => {
    expect(matchSummary({}, ctx)).toBe("Matches nothing");
    expect(matchClauses({}, t)).toEqual([]);
  });
});

describe("thresholdText", () => {
  it("names the server's default for an alert without its own", () => {
    expect(thresholdText(definition({ action: "alert" }), ctx)).toBe("10 in 1 h (default)");
  });

  it("shows a set threshold and nothing for an action without one", () => {
    expect(thresholdText(definition({ action: "incident", alert_threshold: { count: 3, window_seconds: 300 } }), ctx)).toBe("3 in 5 min");
    expect(thresholdText(definition({ action: "ignore" }), ctx)).toBeNull();
  });
});

describe("testPattern (the regex test box)", () => {
  it("marks where a pattern matches the pasted sample", () => {
    const sample = "rpc error: context deadline exceeded calling stripe.com";
    const result = testPattern("deadline exceeded.*stripe", sample);
    expect(result).toEqual({ state: "match", start: sample.indexOf("deadline"), end: sample.indexOf("stripe") + "stripe".length });
  });

  it("tells no match, an empty pattern and a missing sample apart", () => {
    expect(testPattern("stripe", "paypal timed out")).toEqual({ state: "noMatch" });
    expect(testPattern("  ", "anything")).toEqual({ state: "empty" });
    expect(testPattern("stripe", "")).toEqual({ state: "noSample" });
  });

  it("refuses what RE2 refuses and reports what the browser cannot compile", () => {
    expect(testPattern("(?=stripe)", "stripe")).toEqual({ state: "unsupported" });
    expect(testPattern("(a)\\1", "aa")).toEqual({ state: "unsupported" });
    expect(testPattern("(unclosed", "x").state).toBe("invalid");
  });

  it("translates RE2's leading flags and named groups", () => {
    expect(testPattern("(?i)STRIPE", "stripe failed").state).toBe("match");
    const compiled = compileForBrowser("(?P<code>\\d+)");
    expect(compiled.ok && compiled.regex.source).toBe("(?<code>\\d+)");
  });
});

describe("suggestMessageRegex", () => {
  it("keeps the first line literal and opens ids and numbers", () => {
    expect(suggestMessageRegex("order 12345 failed: user 9f8e7d6c1a2b not found (retry 3)\n  at handler.go:42")).toBe(
      "order \\d+ failed: user [0-9a-f-]+ not found \\(retry \\d+\\)",
    );
    expect(suggestMessageRegex("   \n")).toBe("");
  });

  it("is something the browser compiles and that matches the message it came from", () => {
    const message = "GET /v1/carts/7b2f0c1e-1c2d-4e5f-8a9b-0c1d2e3f4a5b 503 (upstream)";
    const pattern = suggestMessageRegex(message);
    expect(testPattern(pattern, message).state).toBe("match");
  });
});

describe("draftFromGroup", () => {
  it("starts from the group's fingerprint and a pattern for its message", () => {
    const d = draftFromGroup({ repository_id: "r-shop", component_id: "c-api", fingerprint: "fp-1", message: "Timeout 30s calling payments" });
    expect(d).toMatchObject({ repositoryId: "r-shop", componentId: "c-api", fingerprint: "fp-1", messageRegex: "Timeout \\d+s calling payments", name: "Timeout 30s calling payments" });
  });
});

describe("validateDefinitionDraft", () => {
  it("accepts a complete draft", () => {
    expect(validateDefinitionDraft(draft())).toEqual({});
  });

  it("asks for a name, a category and something to match", () => {
    expect(validateDefinitionDraft(draft({ name: " ", messageRegex: "", category: "" }))).toEqual({
      name: "required",
      match: "required",
      category: "required",
    });
  });

  it("checks the status, the pattern and the threshold the way the server does", () => {
    expect(validateDefinitionDraft(draft({ status: "99" })).status).toBe("invalid");
    expect(validateDefinitionDraft(draft({ messageRegex: "x".repeat(513) })).messageRegex).toBe("too_long");
    expect(validateDefinitionDraft(draft({ messageRegex: "(?!x)" })).messageRegex).toBe("re2_unsupported");
    const alert = draft({ action: "alert", thresholdCount: "0", thresholdWindowSeconds: 30 });
    expect(validateDefinitionDraft(alert)).toEqual({ thresholdCount: "invalid", thresholdWindow: "invalid" });
  });
});

describe("definitionInput", () => {
  it("creates with the repository, the component only when set, and a threshold only for alert/incident", () => {
    expect(definitionInput(draft(), false)).toEqual({
      repository_id: "r-shop",
      name: "Stripe timeouts",
      match: { message_regex: "deadline" },
      category: "dependency",
      severity: "medium",
      action: "none",
      rationale: "",
    });
    const body = definitionInput(draft({ componentId: "c-api", action: "alert", thresholdCount: "5", thresholdWindowSeconds: 600 }), false);
    expect(body).toMatchObject({ component_id: "c-api", alert_threshold: { count: 5, window_seconds: 600 } });
  });

  it("edits with component_id \"\" for repository-wide and clears a threshold the action no longer takes", () => {
    const body = definitionInput(draftFromDefinition(definition({ action: "ignore", alert_threshold: { count: 2, window_seconds: 60 } })), true);
    expect(body.component_id).toBe("");
    expect(body.alert_threshold).toBeNull();
    expect(body.repository_id).toBeUndefined();
  });

  it("sends trimmed attributes and drops a blank key", () => {
    const body = definitionInput(draft({ attributes: { " region ": " eu ", "": "x" } }), false);
    expect(body.match?.attributes).toEqual({ region: "eu" });
  });
});

describe("switchAction", () => {
  it("starts an untouched threshold from the new action's default and keeps one typed by hand", () => {
    const alert = switchAction(draft(), "alert");
    expect(alert.thresholdCount).toBe(String(DEFAULT_THRESHOLDS.alert.count));
    expect(switchAction(alert, "incident").thresholdCount).toBe(String(DEFAULT_THRESHOLDS.incident.count));
    expect(switchAction({ ...alert, thresholdCount: "42" }, "incident").thresholdCount).toBe("42");
  });
});

describe("mapDefinitionFields", () => {
  it("puts the server's keys under the form's fields and keeps the rest", () => {
    const { errors, rest } = mapDefinitionFields({
      "match.message_regex": "invalid",
      "match.attributes.region": "too_long",
      "alert_threshold.window_seconds": "invalid",
      evidence: "too_long",
    });
    expect(errors).toEqual({ messageRegex: "invalid", attributes: "too_long", thresholdWindow: "invalid", evidence: "too_long" });
    expect(rest).toEqual([]);
    expect(mapDefinitionFields({ evidence: "too_long", nope: "invalid" }, new Set(["name"])).rest).toEqual([
      ["evidence", "too_long"],
      ["nope", "invalid"],
    ]);
  });
});

describe("the list", () => {
  it("counts by status and searches names, rationales and the match in words", () => {
    const defs = [definition(), definition({ id: "d-2", status: "suggested", rationale: "Seen after deploys" }), definition({ id: "d-3", status: "suggested" })];
    expect(statusCounts(defs)).toEqual({ active: 1, suggested: 2, dismissed: 0 });
    expect(definitionMatches(defs[1], "after deploys", t)).toBe(true);
    expect(definitionMatches(defs[0], "matches /context", t)).toBe(true);
    expect(definitionMatches(defs[0], "paypal", t)).toBe(false);
  });

  it("links a definition with its repository picked", () => {
    expect(definitionPath({ id: "d-1", repository_id: "r-shop" })).toBe("/operations/errors/definitions?repository=r-shop&definition=d-1");
  });
});
