import type { AudienceReport, AudienceStatus, AudienceStudyStatus } from "@/api";

export type Tone = "success" | "warning" | "destructive" | "info" | "secondary";

export function audienceStatusTone(status: AudienceStatus): Tone {
  switch (status) {
    case "ready":
      return "success";
    case "building":
      return "info";
    case "failed":
      return "destructive";
    default:
      return "secondary";
  }
}

export function studyStatusTone(status: AudienceStudyStatus): Tone {
  switch (status) {
    case "done":
      return "success";
    case "running":
    case "synthesizing":
      return "info";
    case "failed":
      return "destructive";
    case "cancelled":
      return "warning";
    default:
      return "secondary";
  }
}

export function studyIsActive(status: AudienceStudyStatus): boolean {
  return status === "running" || status === "synthesizing";
}

export function verdictTone(label: AudienceReport["verdict"]["label"]): Tone {
  switch (label) {
    case "strong":
      return "success";
    case "promising":
      return "info";
    case "weak":
      return "warning";
    default:
      return "destructive";
  }
}

/** Intent scores 0-10 as a tone: the cold end reads red, the warm end green. */
export function intentTone(score: number): "destructive" | "warning" | "success" {
  if (score >= 7) return "success";
  if (score >= 4) return "warning";
  return "destructive";
}

export function formatMoney(value: number, currency: string, lang: string): string {
  try {
    return new Intl.NumberFormat(lang, {
      style: "currency",
      currency,
      maximumFractionDigits: Number.isInteger(value) ? 0 : 2,
    }).format(value);
  } catch {
    return `${value} ${currency}`;
  }
}

export function formatPct(value: number): string {
  return `${Math.round(value)}%`;
}

export function initials(name: string): string {
  const parts = name.trim().split(/\s+/).filter(Boolean);
  const first = (word?: string) => (word ? (Array.from(word)[0] ?? "") : "");
  return (first(parts[0]) + (parts.length > 1 ? first(parts[parts.length - 1]) : "")).toLocaleUpperCase();
}

/** A study is worth a "ready to read" nudge once its report exists. */
export function hasReport(status: AudienceStudyStatus): boolean {
  return status === "done";
}

/** Comma-, semicolon- or newline-separated prices (decimal point) as numbers; anything else is dropped. */
export function parsePrices(text: string): number[] {
  const seen = new Set<number>();
  for (const part of text.split(/[,;\n]+/)) {
    const n = Number(part.trim());
    if (part.trim() !== "" && Number.isFinite(n) && n >= 0) seen.add(Math.round(n * 100) / 100);
  }
  return [...seen].sort((a, b) => a - b);
}
