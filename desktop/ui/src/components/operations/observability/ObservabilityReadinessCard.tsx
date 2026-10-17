import { Telescope } from "lucide-react";
import type { DeployEnvironment } from "@/api";
import { EnableObservabilityButton } from "@/components/operations/observability/EnableObservabilityButton";
import { ReadinessSummary } from "@/components/operations/observability/ReadinessSummary";
import { OPS_RUNTIME_ROLES, type OpsEntity } from "@/components/operations/shell/useOpsEntities";
import { Card } from "@/components/ui/card";
import { useI18n } from "@/hooks/useI18n";
import { useObservabilityReadiness } from "@/hooks/useObservabilityReadiness";

/** The entity Summary's compact readiness line: shown only while the
 * component has a gap, with the way to close it. A read that fails (a server
 * without AI suggestions) shows nothing. */
export function ObservabilityReadinessCard({ entity, env }: { entity: OpsEntity; env: DeployEnvironment }) {
  const { t } = useI18n();
  const runs = OPS_RUNTIME_ROLES.includes(entity.role);
  const readiness = useObservabilityReadiness(entity.id, env, runs);
  const data = readiness.data;
  if (!runs || !data || (data.suggestions ?? []).length === 0) return null;
  return (
    <Card role="region" className="flex flex-wrap items-start gap-3 p-3" aria-labelledby={`readiness-${entity.id}`}>
      <Telescope className="mt-0.5 h-4 w-4 shrink-0 text-info" aria-hidden />
      <div className="min-w-0 flex-1 space-y-1.5">
        <h2 id={`readiness-${entity.id}`} className="text-body font-medium">
          {t("operations.observability.card.title")}
        </h2>
        <ReadinessSummary readiness={data} maxGaps={2} />
      </div>
      <EnableObservabilityButton entity={entity} env={env} variant="default" />
    </Card>
  );
}
