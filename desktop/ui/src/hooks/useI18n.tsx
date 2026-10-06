import { createContext, useCallback, useContext, useMemo, useState, type ReactNode } from "react";
import { getStoredLocale, setStoredLocale } from "@/api";
import { htmlLang, normalizeLang, type Lang } from "@/lib/languages";
import { de } from "@/locales/de";
import { en, type Dict } from "@/locales/en";
import { es } from "@/locales/es";
import { fr } from "@/locales/fr";
import { pt } from "@/locales/pt";
import { tr } from "@/locales/tr";
import { zh } from "@/locales/zh";

export type { Lang } from "@/lib/languages";

export const DICTS: Record<Lang, Dict> = { en, tr, es, de, fr, pt, zh };

interface I18nContextValue {
  lang: Lang;
  setLang: (lang: Lang) => void;
  t: (key: string, params?: Record<string, string | number>) => string;
}

const I18nContext = createContext<I18nContextValue | null>(null);

function lookup(dict: unknown, key: string): string | undefined {
  const parts = key.split(".");
  let cur: unknown = dict;
  for (const part of parts) {
    if (cur && typeof cur === "object" && part in (cur as Record<string, unknown>)) {
      cur = (cur as Record<string, unknown>)[part];
    } else {
      return undefined;
    }
  }
  return typeof cur === "string" ? cur : undefined;
}

function interpolate(template: string, params?: Record<string, string | number>): string {
  if (!params) return template;
  return template.replace(/\{(\w+)\}/g, (match, name: string) =>
    name in params ? String(params[name]) : match,
  );
}

// Module-level mirror of the active language, kept in sync by the provider.
// Lets non-component modules (lib/* label maps) translate via tStatic() without
// a hook. Components that render those labels re-render on switch (they consume
// the context), so the labels update.
let activeLang: Lang = normalizeLang(getStoredLocale());

/**
 * Translate outside React (module-level label maps, non-component helpers).
 * Prefer useI18n().t / useT() inside components. Reactivity to language switch
 * relies on the rendering component consuming the i18n context.
 */
export function tStatic(key: string, params?: Record<string, string | number>): string {
  const active = lookup(DICTS[activeLang], key) ?? lookup(DICTS.en, key);
  if (active === undefined) return key;
  return interpolate(active, params);
}

export function I18nProvider({ children }: { children: ReactNode }) {
  const [lang, setLangState] = useState<Lang>(() => {
    const initial = normalizeLang(getStoredLocale());
    activeLang = initial;
    document.documentElement.setAttribute("lang", htmlLang(initial));
    return initial;
  });

  const setLang = useCallback((next: Lang) => {
    setStoredLocale(next);
    activeLang = next;
    document.documentElement.setAttribute("lang", htmlLang(next));
    setLangState(next);
  }, []);

  const t = useCallback(
    (key: string, params?: Record<string, string | number>): string => {
      const active = lookup(DICTS[lang], key) ?? lookup(DICTS.en, key);
      if (active === undefined) return key;
      return interpolate(active, params);
    },
    [lang],
  );

  const value = useMemo(() => ({ lang, setLang, t }), [lang, setLang, t]);

  return <I18nContext.Provider value={value}>{children}</I18nContext.Provider>;
}

export function useI18n() {
  const ctx = useContext(I18nContext);
  if (!ctx) throw new Error("useI18n must be used within I18nProvider");
  return ctx;
}

/** Convenience hook returning just the translate function. */
export function useT() {
  return useI18n().t;
}
