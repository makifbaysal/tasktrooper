import { type ClassValue, clsx } from "clsx";
import { extendTailwindMerge } from "tailwind-merge";
import { intlLocale, normalizeLang, type Lang } from "@/lib/languages";

// tailwind-merge only knows Tailwind's default text-size scale (xs/sm/base/…).
// Our own scale (globals.css `--text-*`) doesn't match any of those names, so
// its font-size validator rejects e.g. `text-caption` and falls through to
// the (permissive) text-color group instead — which then collides with a
// real color utility like `text-primary-foreground` in the same class list
// and silently drops it, keeping whichever came last. Every `size="sm"`
// Button hit this: `text-primary-foreground` (variant) lost to `text-caption`
// (size), leaving button text at the inherited `--foreground` color instead.
const twMerge = extendTailwindMerge({
  extend: {
    classGroups: {
      "font-size": ["text-display", "text-title", "text-heading", "text-body", "text-caption", "text-micro"],
    },
  },
});

export function cn(...inputs: ClassValue[]) {
  return twMerge(clsx(inputs));
}

export function formatDate(iso: string): string {
  return new Date(iso).toLocaleString(intlLocale(localStorage.getItem("bridge_locale")), {
    day: "2-digit",
    month: "short",
    year: "numeric",
    hour: "2-digit",
    minute: "2-digit",
  });
}

export function formatRelativeDate(iso: string): string {
  const date = new Date(iso);
  const now = new Date();
  const diffMs = now.getTime() - date.getTime();
  if (diffMs < -60000) {
    const aheadMins = Math.ceil(-diffMs / 60000);
    if (aheadMins < 60) return `in ${aheadMins}m`;
    const aheadHours = Math.round(aheadMins / 60);
    if (aheadHours < 24) return `in ${aheadHours}h`;
    return formatDate(iso);
  }
  const diffMins = Math.floor(diffMs / 60000);
  const diffHours = Math.floor(diffMs / 3600000);
  const diffDays = Math.floor(diffMs / 86400000);

  if (diffMins < 1) return "Just now";
  if (diffMins < 60) return `${diffMins}m ago`;
  if (diffHours < 24) return `${diffHours}h ago`;
  if (diffDays < 7) return `${diffDays}d ago`;
  return formatDate(iso);
}

const RELATIVE_STEPS: [Intl.RelativeTimeFormatUnit, number][] = [
  ["second", 60],
  ["minute", 60],
  ["hour", 24],
  ["day", 7],
];

// formatRelativeDate's words are English only; this one speaks the UI language.
export function formatRelativeTime(iso: string, lang: string, now: number = Date.now()): string {
  let value = (Date.parse(iso) - now) / 1000;
  const rtf = new Intl.RelativeTimeFormat(intlLocale(lang), { numeric: "auto" });
  for (const [unit, size] of RELATIVE_STEPS) {
    if (Math.abs(value) < size) return rtf.format(Math.round(value), unit);
    value /= size;
  }
  return new Date(iso).toLocaleDateString(intlLocale(lang), { day: "numeric", month: "short", year: "numeric" });
}

const DURATION_UNITS: Record<Lang, readonly [second: string, minute: string, hour: string]> = {
  en: ["s", "m", "h"],
  tr: ["sn", "dk", "sa"],
  es: ["s", "min", "h"],
  de: ["s", "min", "h"],
  fr: ["s", "min", "h"],
  pt: ["s", "min", "h"],
  zh: ["秒", "分", "小时"],
};

export function formatDuration(ms: number, lang: string): string {
  const [s, m, h] = DURATION_UNITS[normalizeLang(lang)];
  const total = Math.round(ms / 1000);
  if (total < 60) return `${total}${s}`;
  const minutes = Math.floor(total / 60);
  if (minutes < 60) return `${minutes}${m} ${total % 60}${s}`;
  return `${Math.floor(minutes / 60)}${h} ${minutes % 60}${m}`;
}

export function formatFileSize(bytes: number): string {
  if (bytes === 0) return "0 B";
  const units = ["B", "KB", "MB", "GB"];
  const i = Math.floor(Math.log(bytes) / Math.log(1024));
  const size = bytes / Math.pow(1024, i);
  return `${size.toFixed(i === 0 ? 0 : 1)} ${units[i]}`;
}

export function formatDurationMs(ms: number): string {
  if (!ms || ms < 0) return "-";
  const totalSeconds = Math.round(ms / 1000);
  if (totalSeconds < 60) return `${totalSeconds}sn`;
  const minutes = Math.floor(totalSeconds / 60);
  const seconds = totalSeconds % 60;
  if (minutes < 60) return `${minutes}dk ${seconds}sn`;
  const hours = Math.floor(minutes / 60);
  const remMinutes = minutes % 60;
  return `${hours}sa ${remMinutes}dk`;
}

export function commaListToArray(value: string): string[] | undefined {
  const items = value
    .split(",")
    .map((s) => s.trim())
    .filter(Boolean);
  return items.length > 0 ? items : undefined;
}
