import { RotateCcw } from "lucide-react";
import type { ReactNode } from "react";
import type { Fact } from "@/api";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { HelpTooltip } from "@/components/ui/help-tooltip";
import { evidenceLabel } from "@/components/projects/model/EvidenceList";
import { useI18n } from "@/hooks/useI18n";
import { factValue, isOverridden } from "@/lib/project-model";
import { cn } from "@/lib/utils";

interface FactValueProps<T> {
  fact: Fact<T>;
  /** Custom rendering for the effective value (e.g. a RoleBadge for a role fact). Defaults to String(value). */
  render?: (value: T) => ReactNode;
  emptyLabel?: string;
  /** Sends a PATCH with `null` for this field on the server — present only when the fact is overridden. */
  onRevert?: () => void;
  className?: string;
}

/** The effective value of a Fact<T>, with an "edited by you" marker, an
 * evidence tooltip, and (when overridden and `onRevert` is given) a
 * "revert to detected" action. */
export function FactValue<T>({ fact, render, emptyLabel, onRevert, className }: FactValueProps<T>) {
  const { t } = useI18n();
  const value = factValue(fact);
  const overridden = isOverridden(fact);
  const evidence = fact.evidence ?? [];

  return (
    <span className={cn("inline-flex flex-wrap items-center gap-1.5", className)}>
      <span>{value !== undefined ? (render ? render(value) : String(value)) : (emptyLabel ?? "—")}</span>
      {overridden && <Badge variant="info">{t("projectModel.editedByYou")}</Badge>}
      {evidence.length > 0 && <HelpTooltip text={evidence.map(evidenceLabel).join("\n")} />}
      {overridden && onRevert && (
        <Button variant="link" size="sm" className="h-auto gap-1 p-0 text-micro" onClick={onRevert}>
          <RotateCcw className="h-3 w-3" aria-hidden />
          {t("projectModel.revertToDetected")}
        </Button>
      )}
    </span>
  );
}
