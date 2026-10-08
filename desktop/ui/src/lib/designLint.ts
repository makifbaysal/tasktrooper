import type { DesignLintFinding } from "@/api";

type Translate = (key: string, params?: Record<string, string | number>) => string;

const KNOWN_CODES = new Set(["unresolved_alias", "invalid_color", "contrast_below_aa", "empty_group", "missing_section"]);

/** Errors before warnings; the server's order within each severity is kept. */
export function sortLintFindings(findings: DesignLintFinding[]): DesignLintFinding[] {
  const rank = (finding: DesignLintFinding) => (finding.severity === "error" ? 0 : 1);
  return findings
    .map((finding, index) => ({ finding, index }))
    .sort((a, b) => rank(a.finding) - rank(b.finding) || a.index - b.index)
    .map(({ finding }) => finding);
}

export function lintCounts(findings: DesignLintFinding[]): { errors: number; warnings: number } {
  let errors = 0;
  for (const finding of findings) if (finding.severity === "error") errors++;
  return { errors, warnings: findings.length - errors };
}

function formatRatio(ratio: number | undefined, lang: string): string {
  if (ratio === undefined || !Number.isFinite(ratio)) return "?";
  try {
    return new Intl.NumberFormat(lang, { minimumFractionDigits: 2, maximumFractionDigits: 2 }).format(ratio);
  } catch {
    return ratio.toFixed(2);
  }
}

/**
 * The finding as one sentence in the UI language. The server sends codes and
 * values only, so every wording lives in `designSystem.checks.messages`.
 */
export function lintMessage(finding: DesignLintFinding, t: Translate, lang = "en"): string {
  const params = {
    path: finding.path ?? "",
    related: finding.related_path ?? "",
    value: finding.value ?? "",
    ratio: formatRatio(finding.ratio, lang),
    code: finding.code,
  };
  const code = KNOWN_CODES.has(finding.code) ? finding.code : "unknown";
  return t(`designSystem.checks.messages.${code}`, params);
}
