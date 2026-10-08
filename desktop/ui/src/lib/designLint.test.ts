import { describe, expect, it } from "vitest";
import type { DesignLintFinding } from "@/api";
import { lintCounts, lintMessage, sortLintFindings } from "@/lib/designLint";
import { en } from "@/locales/en";
import { tr } from "@/locales/tr";

function translator(dict: unknown) {
  return (key: string, params?: Record<string, string | number>) => {
    let cur: unknown = dict;
    for (const part of key.split(".")) cur = (cur as Record<string, unknown> | undefined)?.[part];
    if (typeof cur !== "string") return key;
    return cur.replace(/\{(\w+)\}/g, (match, name: string) => (params && name in params ? String(params[name]) : match));
  };
}

const t = translator(en);

describe("lintMessage", () => {
  it("says a contrast pair's ratio against the AA minimum", () => {
    const finding: DesignLintFinding = {
      code: "contrast_below_aa",
      severity: "error",
      path: "color.accent",
      related_path: "color.on-accent",
      ratio: 1.82,
    };
    expect(lintMessage(finding, t)).toBe("color.on-accent on color.accent is 1.82:1 — AA needs 4.5:1");
  });

  it("formats the ratio for the language", () => {
    const finding: DesignLintFinding = {
      code: "contrast_below_aa",
      severity: "error",
      path: "color.accent",
      related_path: "color.on-accent",
      ratio: 3.1,
    };
    expect(lintMessage(finding, translator(tr), "tr")).toBe(
      "color.accent ile color.on-accent arasındaki kontrast 3,10:1 — AA için en az 4,5:1 gerekir",
    );
  });

  it("names the missing DESIGN.md section", () => {
    expect(lintMessage({ code: "missing_section", severity: "warning", value: "Do's and Don'ts" }, t)).toBe(
      "DESIGN.md has no Do's and Don'ts section",
    );
  });

  it("builds a sentence for every other code", () => {
    expect(
      lintMessage({ code: "unresolved_alias", severity: "error", path: "color.link", value: "{color.brand}" }, t),
    ).toBe("color.link refers to {color.brand}, which does not exist");
    expect(lintMessage({ code: "invalid_color", severity: "error", path: "color.bg", value: "blurple" }, t)).toBe(
      "color.bg is not a valid color: blurple",
    );
    expect(lintMessage({ code: "empty_group", severity: "warning", path: "shadow" }, t)).toBe(
      "The shadow group has no tokens",
    );
  });

  it("falls back to a generic sentence for a code it does not know", () => {
    const finding = { code: "font_missing", severity: "warning" } as unknown as DesignLintFinding;
    expect(lintMessage(finding, t)).toBe("Check font_missing reported a problem");
  });
});

describe("sortLintFindings", () => {
  it("puts errors first and keeps the server's order within a severity", () => {
    const findings: DesignLintFinding[] = [
      { code: "empty_group", severity: "warning", path: "a" },
      { code: "invalid_color", severity: "error", path: "b" },
      { code: "missing_section", severity: "warning", value: "Overview" },
      { code: "unresolved_alias", severity: "error", path: "c" },
    ];
    expect(sortLintFindings(findings).map((f) => f.path ?? f.value)).toEqual(["b", "c", "a", "Overview"]);
    expect(lintCounts(findings)).toEqual({ errors: 2, warnings: 2 });
  });
});
