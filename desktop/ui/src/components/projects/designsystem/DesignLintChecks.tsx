import { CheckCircle2 } from "lucide-react";
import { useMemo } from "react";
import type { DesignLintFinding } from "@/api";
import { Badge } from "@/components/ui/badge";
import { useI18n } from "@/hooks/useI18n";
import { lintCounts, lintMessage, sortLintFindings } from "@/lib/designLint";
import { cn } from "@/lib/utils";

interface DesignLintChecksProps {
  /** Absent is the same as empty: the server omits an empty list. */
  findings: DesignLintFinding[] | undefined;
  /** A line under the title, e.g. that these ran on the merged tokens. */
  hint?: string;
  className?: string;
}

/** Molecule: a version's lint findings, errors first, each as one sentence with its severity. */
export function DesignLintChecks({ findings, hint, className }: DesignLintChecksProps) {
  const { t, lang } = useI18n();
  const sorted = useMemo(() => sortLintFindings(findings ?? []), [findings]);
  const counts = lintCounts(sorted);

  return (
    <section className={cn("space-y-2", className)} aria-label={t("designSystem.checks.title")}>
      <div className="flex flex-wrap items-center gap-2">
        <p className="text-caption font-medium text-muted-foreground">{t("designSystem.checks.title")}</p>
        {counts.errors > 0 && (
          <Badge variant="destructive">{t("designSystem.checks.errors", { count: counts.errors })}</Badge>
        )}
        {counts.warnings > 0 && (
          <Badge variant="warning">{t("designSystem.checks.warnings", { count: counts.warnings })}</Badge>
        )}
      </div>
      {hint && <p className="text-caption text-muted-foreground">{hint}</p>}
      {sorted.length === 0 ? (
        <p className="flex items-center gap-1.5 text-body text-success">
          <CheckCircle2 className="h-4 w-4 shrink-0" aria-hidden />
          {t("designSystem.checks.none")}
        </p>
      ) : (
        <ul className="divide-y divide-border rounded-lg border border-border">
          {sorted.map((finding, index) => (
            <li key={`${finding.code}-${finding.path ?? ""}-${finding.related_path ?? ""}-${index}`} className="flex items-start gap-3 px-3 py-2">
              <Badge variant={finding.severity === "error" ? "destructive" : "warning"} className="mt-0.5 shrink-0">
                {t(`designSystem.checks.severity.${finding.severity === "error" ? "error" : "warning"}`)}
              </Badge>
              <span className="min-w-0 break-words text-body">{lintMessage(finding, t, lang)}</span>
            </li>
          ))}
        </ul>
      )}
    </section>
  );
}
