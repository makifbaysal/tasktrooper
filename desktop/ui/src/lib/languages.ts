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

/** Also takes a BCP 47 tag ("pt-BR", "zh_CN", "de-AT"); anything unknown is English. */
export function normalizeLang(value: string | null | undefined): Lang {
  if (!value) return DEFAULT_LANG;
  const lower = value.trim().toLowerCase();
  if (isLang(lower)) return lower;
  const primary = lower.split(/[-_]/)[0];
  return isLang(primary) ? primary : DEFAULT_LANG;
}

export function intlLocale(lang: string | null | undefined): string {
  return REGISTRY[normalizeLang(lang)].intl;
}

/** For <html lang>: the region picks CJK glyph variants and hyphenation rules. */
export function htmlLang(lang: string | null | undefined): string {
  return REGISTRY[normalizeLang(lang)].tag;
}
