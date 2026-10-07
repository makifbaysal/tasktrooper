import { createContext, useCallback, useContext, useEffect, useMemo, useRef, useState, type ReactNode } from "react";
import { getStoredLocale, setStoredLocale } from "@/api";
import { htmlLang, normalizeLang, type Lang } from "@/lib/languages";
import { en, type Dict } from "@/locales/en";

export type { Lang } from "@/lib/languages";

// English is bundled because it is every lookup's fallback; the other
// dictionaries are their own chunks (each is as large as the rest of the app's
// startup code) and only the one in use is ever fetched.
const LOADERS: Record<Exclude<Lang, "en">, () => Promise<Dict>> = {
  tr: () => import("@/locales/tr").then((m) => m.tr),
  es: () => import("@/locales/es").then((m) => m.es),
  de: () => import("@/locales/de").then((m) => m.de),
  fr: () => import("@/locales/fr").then((m) => m.fr),
  pt: () => import("@/locales/pt").then((m) => m.pt),
  zh: () => import("@/locales/zh").then((m) => m.zh),
};

const loaded: Partial<Record<Lang, Dict>> = { en };
const pending: Partial<Record<Lang, Promise<Dict>>> = {};

/** Resolves once `lang`'s dictionary is in memory; main.tsx awaits the stored one before the first render. */
export function loadLocale(lang: Lang): Promise<Dict> {
  const have = loaded[lang];
  if (have) return Promise.resolve(have);
  const inFlight = pending[lang];
  if (inFlight) return inFlight;
  const load = LOADERS[lang as Exclude<Lang, "en">]()
    .then((dict) => {
      loaded[lang] = dict;
      return dict;
    })
    .finally(() => {
      delete pending[lang];
    });
  pending[lang] = load;
  return load;
}

interface I18nContextValue {
  lang: Lang;
  /** Switches once the language's dictionary has loaded; until then the current one stays. */
  setLang: (lang: Lang) => Promise<void>;
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

function translate(lang: Lang, key: string, params?: Record<string, string | number>): string {
  const active = lookup(loaded[lang] ?? en, key) ?? lookup(en, key);
  if (active === undefined) return key;
  return interpolate(active, params);
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
  return translate(activeLang, key, params);
}

export function I18nProvider({ children }: { children: ReactNode }) {
  const [lang, setLangState] = useState<Lang>(() => {
    const stored = normalizeLang(getStoredLocale());
    // Only a dictionary already in memory is shown; otherwise English renders
    // until the effect below has loaded the stored one.
    const initial = loaded[stored] ? stored : "en";
    activeLang = initial;
    document.documentElement.setAttribute("lang", htmlLang(initial));
    return initial;
  });
  const latestRequest = useRef(0);

  const apply = useCallback(async (next: Lang, persist: boolean) => {
    const request = ++latestRequest.current;
    try {
      await loadLocale(next);
    } catch {
      return;
    }
    // A slower load must not undo a later pick.
    if (request !== latestRequest.current) return;
    if (persist) setStoredLocale(next);
    activeLang = next;
    document.documentElement.setAttribute("lang", htmlLang(next));
    setLangState(next);
  }, []);

  useEffect(() => {
    const stored = normalizeLang(getStoredLocale());
    if (stored !== activeLang) void apply(stored, false);
  }, [apply]);

  const setLang = useCallback((next: Lang) => apply(next, true), [apply]);

  const t = useCallback(
    (key: string, params?: Record<string, string | number>): string => translate(lang, key, params),
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
