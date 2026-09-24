import type { CloudResourceStatus, EnvironmentSummary } from "@/api";
import { ProviderIcon } from "@/components/projects/model/ProviderIcon";
import { Badge } from "@/components/ui/badge";
import { useI18n } from "@/hooks/useI18n";
import { cn } from "@/lib/utils";

const HEALTH_DOT: Record<CloudResourceStatus, string> = {
  healthy: "bg-success",
  deploying: "bg-info",
  degraded: "bg-warning",
  failed: "bg-destructive",
  unknown: "bg-muted-foreground",
};

interface EnvironmentChipsProps {
  environments: EnvironmentSummary[];
  className?: string;
}

/**
 * One component's confirmed environments, compactly: a provider mark, a
 * short env label (prod/stg/prev), a health dot and — only when the last 24h
 * saw any — an error-count badge. Suggested/dismissed rows are not shown
 * here; that is what the review queue is for. Renders nothing when there is
 * nothing confirmed.
 */
export function EnvironmentChips({ environments, className }: EnvironmentChipsProps) {
  const { t } = useI18n();
  const confirmed = environments.filter((e) => e.status === "confirmed");
  if (confirmed.length === 0) return null;
  return (
    <span className={cn("inline-flex flex-wrap items-center gap-2", className)}>
      {confirmed.map((env) => {
        const health = env.health ?? "unknown";
        return (
          <span key={env.id} className="inline-flex items-center gap-1 text-micro text-muted-foreground">
            {env.provider && <ProviderIcon provider={env.provider} className="h-3 w-3" />}
            <span>{t(`cloud.environmentsShort.${env.environment}`)}</span>
            <span
              className={cn("h-1.5 w-1.5 shrink-0 rounded-full", HEALTH_DOT[health])}
              title={t(`cloud.health.${health}`)}
              aria-hidden
            />
            {env.error_count_24h > 0 && (
              <Badge
                variant="destructive"
                className="px-1.5 py-0 text-micro"
                title={t("cloud.errorCount", { count: env.error_count_24h })}
              >
                {env.error_count_24h}
              </Badge>
            )}
          </span>
        );
      })}
    </span>
  );
}
