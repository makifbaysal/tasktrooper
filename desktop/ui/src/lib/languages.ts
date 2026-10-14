// The UI languages, in picker order. Each code names a dictionary at
// src/locales/<code>.ts and is the value stored in localStorage and sent as the
// server's default_language. "pt" is Brazilian Portuguese and "zh" Simplified
// Chinese; `tag` and `intl` carry the region for <html lang> and Intl.
const REGISTRY = {
  tr: { label: "Türkçe", tag: "tr", intl: "tr-TR" },
  en: { label: "English", tag: "en", intl: "en-US" },
  es: { label: "Español", tag: "es", intl: "es" },
  de: { label: "Deutsch", tag: "de", intl: "de-DE" },
  fr: { label: "Français", tag: "fr", intl: "fr-FR" },
  pt: { label: "Português (Brasil)", tag: "pt-BR", intl: "pt-BR" },
  zh: { label: "简体中文", tag: "zh-CN", intl: "zh-CN" },
} as const;

export type Lang = keyof typeof REGISTRY;

export const LANGS = Object.keys(REGISTRY) as Lang[];

export const DEFAULT_LANG: Lang = "en";

export const LANGUAGE_OPTIONS: readonly { value: Lang; label: string }[] = LANGS.map((value) => ({
  value,
  label: REGISTRY[value].label,
}));

export function isLang(value: unknown): value is Lang {
  return typeof value === "string" && Object.prototype.hasOwnProperty.call(REGISTRY, value);
}

/** The UI language a code or BCP 47 tag ("pt-BR", "zh_CN", "de-AT") names, if the UI has it. */
function matchLang(value: string | null | undefined): Lang | undefined {
  if (!value) return undefined;
  const lower = value.trim().toLowerCase();
  if (isLang(lower)) return lower;
  const primary = lower.split(/[-_]/)[0];
  return isLang(primary) ? primary : undefined;
}

/** Also takes a BCP 47 tag ("pt-BR", "zh_CN", "de-AT"); anything unknown is English. */
export function normalizeLang(value: string | null | undefined): Lang {
  return matchLang(value) ?? DEFAULT_LANG;
}

/**
 * The first of the device's preferred languages the UI has, else English. In
 * the desktop app `navigator.languages` is the OS's own preference list, so a
 * fresh install opens in the system language; in a browser it is the
 * browser's.
 */
export function deviceLang(preferred: readonly string[] = browserLanguages()): Lang {
  for (const tag of preferred) {
    const lang = matchLang(tag);
    if (lang) return lang;
  }
  return DEFAULT_LANG;
}

function browserLanguages(): readonly string[] {
  if (typeof navigator === "undefined") return [];
  if (navigator.languages?.length) return navigator.languages;
  return navigator.language ? [navigator.language] : [];
}

export function intlLocale(lang: string | null | undefined): string {
  return REGISTRY[normalizeLang(lang)].intl;
}

/** For <html lang>: the region picks CJK glyph variants and hyphenation rules. */
export function htmlLang(lang: string | null | undefined): string {
  return REGISTRY[normalizeLang(lang)].tag;
}
