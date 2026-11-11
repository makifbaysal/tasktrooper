import { describe, expect, it } from "vitest";
import { en } from "@/locales/en";
import { LANGS } from "@/lib/languages";
import {
  WHATS_NEW,
  compareVersions,
  findWhatsNew,
  latestWhatsNew,
  releaseNotesDecision,
  whatsNewKey,
  type WhatsNewRecord,
} from "@/lib/whatsNew/entries";

function lookup(dict: unknown, path: string[]): unknown {
  return path.reduce<unknown>((cur, part) => (cur && typeof cur === "object" ? (cur as Record<string, unknown>)[part] : undefined), dict);
}

describe("whatsNew data", () => {
  it("has a unique, well-formed version per record", () => {
    const versions = WHATS_NEW.map((r) => r.version);
    expect(new Set(versions).size).toBe(versions.length);
    for (const v of versions) expect(v).toMatch(/^\d+\.\d+\.\d+$/);
  });

  it("keeps 3-6 items per entry", () => {
    for (const r of WHATS_NEW) {
      if ("items" in r) {
        expect(r.items.length).toBeGreaterThanOrEqual(3);
        expect(r.items.length).toBeLessThanOrEqual(6);
      }
    }
  });

  it("has every entry's title and items in all seven locales", async () => {
    expect(LANGS).toHaveLength(7);
    for (const lang of LANGS) {
      const dict = lang === "en" ? en : ((await import(`../../locales/${lang}.ts`)) as Record<string, unknown>)[lang];
      for (const r of WHATS_NEW) {
        if (!("items" in r)) continue;
        const key = whatsNewKey(r.version);
        expect(lookup(dict, ["whatsNew", key, "title"]), `${lang} ${key}.title`).toEqual(expect.any(String));
        for (const item of r.items) {
          expect(lookup(dict, ["whatsNew", key, "items", item]), `${lang} ${key}.items.${item}`).toEqual(expect.any(String));
        }
      }
    }
  });

  it("requires an entry or an explicit skip for the release being prepared", () => {
    const latest = [...WHATS_NEW].map((r) => r.version).sort(compareVersions).pop()!;
    expect(releaseNotesDecision(latest)).not.toBe("missing");
  });
});

describe("whatsNew helpers", () => {
  const records: WhatsNewRecord[] = [
    { version: "0.3.13", items: ["a", "b", "c"] },
    { version: "0.3.14", skip: true },
    { version: "0.4.0", items: ["x", "y", "z"] },
  ];

  it("finds an entry by version, tolerating a leading v", () => {
    expect(findWhatsNew("v0.3.13", records)?.items[0]).toBe("a");
    expect(findWhatsNew("0.3.14", records)).toBeNull();
    expect(findWhatsNew("0.9.9", records)).toBeNull();
  });

  it("tells entry, skip and missing apart", () => {
    expect(releaseNotesDecision("0.3.13", records)).toBe("entry");
    expect(releaseNotesDecision("0.3.14", records)).toBe("skip");
    expect(releaseNotesDecision("0.3.15", records)).toBe("missing");
  });

  it("picks the highest entry, ignoring skips", () => {
    expect(latestWhatsNew(records)?.version).toBe("0.4.0");
    expect(latestWhatsNew([{ version: "1.0.0", skip: true }])).toBeNull();
    expect(compareVersions("0.10.0", "0.9.9")).toBeGreaterThan(0);
  });

  it("builds the locale key", () => {
    expect(whatsNewKey("v0.3.13")).toBe("v0_3_13");
  });
});
