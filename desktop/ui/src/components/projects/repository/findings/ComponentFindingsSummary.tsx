import { ScanSearch } from "lucide-react";
import type { ComponentFinding, FindingSeverity } from "@/api";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card } from "@/components/ui/card";
import { useI18n } from "@/hooks/useI18n";
import { openFindings, severityVariant } from "@/lib/findings";

const SEVERITIES: FindingSeverity[] = ["high", "medium", "low"];

/** The component page's line on code health: open findings by severity and a way to the Findings tab. */
export function ComponentFindingsSummary({
  findings,
  componentId,
  onOpen,
}: {
  findings: ComponentFinding[];
  componentId: string;
  onOpen: () => void;
}) {
  const { t } = useI18n();
  const open = openFindings(findings, componentId);
  return (
    <Card className="flex items-center justify-between gap-3 p-4">
      <div className="flex min-w-0 flex-wrap items-center gap-2">
        <ScanSearch className="h-4 w-4 shrink-0 text-muted-foreground" aria-hidden />
        <span className="text-body">
          {open.length > 0 ? t("findings.componentSummary.open", { count: open.length }) : t("findings.componentSummary.none")}
        </span>
        {SEVERITIES.map((sev) => {
          const n = open.filter((f) => f.severity === sev).length;
          return n > 0 ? (
            <Badge key={sev} variant={severityVariant(sev)}>
              {t(`findings.severity.${sev}`)} · {n}
            </Badge>
          ) : null;
        })}
      </div>
      <Button size="sm" variant="outline" onClick={onOpen}>
        {t("findings.componentSummary.view")}
      </Button>
    </Card>
  );
}
