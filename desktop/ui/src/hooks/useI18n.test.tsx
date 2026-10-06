import { act, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it } from "vitest";
import { DICTS, I18nProvider, useI18n } from "@/hooks/useI18n";
import { LANGS, type Lang } from "@/lib/languages";
import { en } from "@/locales/en";

function flatten(value: unknown, prefix = "", out = new Map<string, string>()): Map<string, string> {
  for (const [key, child] of Object.entries(value as Record<string, unknown>)) {
    const full = prefix ? `${prefix}.${key}` : key;
    if (child && typeof child === "object") flatten(child, full, out);
    else out.set(full, String(child));
  }
  return out;
}

const placeholders = (text: string) => [...new Set([...text.matchAll(/\{(\w+)\}/g)].map((m) => m[1]))].sort();

const enFlat = flatten(en);
const translated = LANGS.filter((lang) => lang !== "en");

describe("dictionaries", () => {
  it("cover every registered language", () => {
    expect(Object.keys(DICTS).sort()).toEqual([...LANGS].sort());
  });

  it.each(translated)("%s has exactly en's keys", (lang) => {
    const keys = [...flatten(DICTS[lang]).keys()];
    expect(keys.filter((k) => !enFlat.has(k))).toEqual([]);
    expect([...enFlat.keys()].filter((k) => !keys.includes(k))).toEqual([]);
  });

  it.each(translated)("%s interpolates the same {placeholders} as en, key by key", (lang) => {
    const mismatched = [...flatten(DICTS[lang])]
      .filter(([key, text]) => placeholders(text).join() !== placeholders(enFlat.get(key) ?? "").join())
      .map(([key]) => key);
    expect(mismatched).toEqual([]);
  });

  it.each(translated)("%s leaves no string empty that en fills", (lang) => {
    const empty = [...flatten(DICTS[lang])]
      .filter(([key, text]) => !text.trim() && (enFlat.get(key) ?? "").trim())
      .map(([key]) => key);
    expect(empty).toEqual([]);
  });
});

function Probe() {
  const { t, lang, setLang } = useI18n();
  return (
    <div>
      <span data-testid="lang">{lang}</span>
      <span data-testid="text">{t("common.save")}</span>
      <span data-testid="interpolated">{t("setup.stepLabel", { index: 2, total: 4 })}</span>
      {LANGS.map((l: Lang) => (
        <button key={l} type="button" onClick={() => setLang(l)}>
          {`to-${l}`}
        </button>
      ))}
    </div>
  );
}

describe("I18nProvider", () => {
  afterEach(() => {
    localStorage.clear();
    document.documentElement.removeAttribute("lang");
  });

  it("starts in the stored language and tags <html> with its region", () => {
    localStorage.setItem("bridge_locale", "pt");
    render(
      <I18nProvider>
        <Probe />
      </I18nProvider>,
    );
    expect(screen.getByTestId("lang")).toHaveTextContent("pt");
    expect(screen.getByTestId("text")).toHaveTextContent(DICTS.pt.common.save);
    expect(document.documentElement.getAttribute("lang")).toBe("pt-BR");
  });

  it("reads a stored region tag and falls back to English for an unknown code", () => {
    localStorage.setItem("bridge_locale", "zh-CN");
    const { unmount } = render(
      <I18nProvider>
        <Probe />
      </I18nProvider>,
    );
    expect(screen.getByTestId("lang")).toHaveTextContent("zh");
    unmount();

    localStorage.setItem("bridge_locale", "xx");
    render(
      <I18nProvider>
        <Probe />
      </I18nProvider>,
    );
    expect(screen.getByTestId("lang")).toHaveTextContent("en");
    expect(screen.getByTestId("text")).toHaveTextContent(en.common.save);
  });

  it.each(translated)("switches to %s, persists it and interpolates its strings", (lang) => {
    render(
      <I18nProvider>
        <Probe />
      </I18nProvider>,
    );
    act(() => screen.getByRole("button", { name: `to-${lang}` }).click());
    expect(screen.getByTestId("text")).toHaveTextContent(DICTS[lang].common.save);
    const interpolated = screen.getByTestId("interpolated").textContent ?? "";
    expect(interpolated).toContain("2");
    expect(interpolated).toContain("4");
    expect(interpolated).not.toMatch(/\{\w+\}/);
    expect(localStorage.getItem("bridge_locale")).toBe(lang);
  });
});
