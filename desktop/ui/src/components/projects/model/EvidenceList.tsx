import { FileText } from "lucide-react";
import type { SourceEvidence } from "@/api";
import { useI18n } from "@/hooks/useI18n";
import { cn } from "@/lib/utils";

/** Mirrors the server's SourceEvidence.String(): "path:line", or just "path"
 * when no line was recorded. */
export function evidenceLabel(evidence: SourceEvidence): string {
  return evidence.line ? `${evidence.path}:${evidence.line}` : evidence.path;
}

interface EvidenceListProps {
  evidence: SourceEvidence[];
  className?: string;
}

export function EvidenceList({ evidence, className }: EvidenceListProps) {
  const { t } = useI18n();
  if (evidence.length === 0) return null;
  return (
    <ul
      aria-label={t("projectModel.evidence")}
      className={cn("flex flex-col gap-0.5 text-micro text-muted-foreground", className)}
    >
      {evidence.map((item, index) => (
        <li key={`${item.path}:${item.line ?? 0}:${index}`} className="flex items-start gap-1">
          <FileText className="mt-0.5 h-3 w-3 shrink-0" aria-hidden />
          <span className="truncate font-mono">{evidenceLabel(item)}</span>
          {item.note && <span className="truncate text-muted-foreground/80">— {item.note}</span>}
        </li>
      ))}
    </ul>
  );
}
