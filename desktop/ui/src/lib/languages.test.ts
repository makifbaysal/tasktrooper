import { describe, expect, it } from "vitest";
import { htmlLang, intlLocale, LANGS, LANGUAGE_OPTIONS, normalizeLang } from "@/lib/languages";
import { formatDuration, formatRelativeTime } from "@/lib/utils";
import { formatInteger, formatPercent } from "@/lib/usage";

describe("normalizeLang", () => {
  it("keeps every registered code", () => {
    for (const lang of LANGS) expect(normalizeLang(lang)).toBe(lang);
  });

  it("reads a BCP 47 tag by its primary subtag", () => {
    expect(normalizeLang("pt-BR")).toBe("pt");
    expect(normalizeLang("zh_CN")).toBe("zh");
    expect(normalizeLang("zh-Hans")).toBe("zh");
    expect(normalizeLang("DE-at")).toBe("de");
    expect(normalizeLang("es-419")).toBe("es");
    expect(normalizeLang(" fr ")).toBe("fr");
  });

  it("falls back to English for anything it does not know", () => {
    for (const value of [null, undefined, "", "xx", "ja-JP", "english"]) expect(normalizeLang(value)).toBe("en");
  });
});

describe("intlLocale and htmlLang", () => {
  it("carry the region of the Portuguese and Chinese variants", () => {
    expect(intlLocale("pt")).toBe("pt-BR");
    expect(intlLocale("zh")).toBe("zh-CN");
    expect(htmlLang("pt")).toBe("pt-BR");
    expect(htmlLang("zh")).toBe("zh-CN");
    expect(htmlLang("tr")).toBe("tr");
  });

  it("format an unknown code as English", () => {
    expect(intlLocale("xx")).toBe("en-US");
    expect(htmlLang(undefined)).toBe("en");
  });
});

describe("LANGUAGE_OPTIONS", () => {
  it("offers every language once, each in its own name", () => {
    expect(LANGUAGE_OPTIONS.map((o) => o.value).sort()).toEqual([...LANGS].sort());
    expect(Object.fromEntries(LANGUAGE_OPTIONS.map((o) => [o.value, o.label]))).toEqual({
      tr: "Türkçe",
      en: "English",
      es: "Español",
      de: "Deutsch",
      fr: "Français",
      pt: "Português (Brasil)",
      zh: "简体中文",
    });
  });
});

describe("formatters follow the UI language", () => {
  it("formats numbers with each language's separators", () => {
    expect(formatInteger(1234567, "de")).toBe("1.234.567");
    expect(formatInteger(1234567, "pt")).toBe("1.234.567");
    expect(formatInteger(1234567, "zh")).toBe("1,234,567");
    expect(formatPercent(0.031, "fr")).toMatch(/^3,1\s%$/);
  });

  it("words relative times in the UI language", () => {
    const now = Date.parse("2026-10-06T12:00:00Z");
    expect(formatRelativeTime("2026-10-06T11:55:00Z", "es", now)).toBe("hace 5 minutos");
    expect(formatRelativeTime("2026-10-06T11:55:00Z", "de", now)).toBe("vor 5 Minuten");
    expect(formatRelativeTime("2026-10-06T11:55:00Z", "xx", now)).toBe("5 minutes ago");
  });

  it("abbreviates duration units per language", () => {
    expect(formatDuration(125_000, "en")).toBe("2m 5s");
    expect(formatDuration(125_000, "tr")).toBe("2dk 5sn");
    expect(formatDuration(125_000, "fr")).toBe("2min 5s");
    expect(formatDuration(3_725_000, "zh")).toBe("1小时 2分");
    expect(formatDuration(125_000, "xx")).toBe("2m 5s");
  });
});
