import { FolderGit2, Plug, Rocket } from "lucide-react";
import { Link } from "react-router-dom";
import type { DeployEnvironment, EnvironmentSummary, Release } from "@/api";
import { HealthSquare, healthReasonText } from "@/components/operations/console/HealthSquare";
import { releaseLabel, releaseTime, repositoryDeployPath } from "@/components/operations/console/opsEntities";
import type { OpsHealth } from "@/components/operations/console/opsStatus";
import { EnableObservabilityButton } from "@/components/operations/observability/EnableObservabilityButton";
import type { OpsEntity } from "@/components/operations/shell/useOpsEntities";
import { ProviderIcon } from "@/components/projects/model/ProviderIcon";
import { RoleBadge } from "@/components/projects/model/RoleBadge";
import { useI18n } from "@/hooks/useI18n";
import { badgeVariants } from "@/components/ui/badge";
import { cn, formatDate, formatRelativeTime } from "@/lib/utils";

interface EntityHeaderProps {
  entity: OpsEntity;
  env: DeployEnvironment;
  environment?: EnvironmentSummary;
  health: OpsHealth;
  lastRelease?: Release;
}

const CHIP = cn(badgeVariants({ variant: "outline" }), "min-w-0 gap-1.5 rounded-md bg-card px-2 font-normal");

/** An entity page's identity line: health, name, type, repository, the
 * environment shown and what shipped there last. */
export function EntityHeader({ entity, env, environment, health, lastRelease }: EntityHeaderProps) {
  const { t, lang } = useI18n();
  const envLabel = t(`cloud.environments.${env}`);
  const runsOn = environment ? environment.resource_name || environment.url : undefined;

  return (
    <div className="flex min-w-0 items-start gap-3">
      <HealthSquare status={health.status} reason={healthReasonText(t, health)} size="lg" className="mt-0.5" />
      <div className="min-w-0 flex-1 space-y-1.5">
        <div className="flex min-w-0 flex-wrap items-center gap-2">
          <h1 className="min-w-0 truncate text-title font-semibold tracking-tight">{entity.name}</h1>
          <RoleBadge role={entity.role} />
          <span className="text-caption text-muted-foreground">{t(`operations.status.${health.status}`)}</span>
        </div>
        <div className="flex min-w-0 flex-wrap items-center gap-2">
          <Link
            to={`/repositories/${encodeURIComponent(entity.repositoryId)}`}
            className={cn(CHIP, "text-muted-foreground hover:text-foreground hover:underline")}
            aria-label={t("operations.entity.openRepository", { name: entity.repositoryName })}
          >
            <FolderGit2 className="h-3.5 w-3.5 shrink-0" aria-hidden />
            <span className="truncate">{entity.repositoryName}</span>
          </Link>
          {environment ? (
            <span className={CHIP} title={runsOn}>
              {environment.provider && <ProviderIcon provider={environment.provider} className="h-3.5 w-3.5 shrink-0" />}
              <span className="font-medium">{envLabel}</span>
              {runsOn && <span className="truncate font-mono text-muted-foreground">{runsOn}</span>}
            </span>
          ) : (
            <span className={cn(CHIP, "border-dashed text-muted-foreground")}>
              {t("operations.entity.notConnected", { env: envLabel })}
              <Link to={repositoryDeployPath(entity.repositoryId)} className="inline-flex items-center gap-1 font-medium text-info hover:underline">
                <Plug className="h-3 w-3" aria-hidden />
                {t("operations.entity.connect")}
              </Link>
            </span>
          )}
          <span className={CHIP} title={lastRelease ? formatDate(releaseTime(lastRelease)) : undefined}>
            <Rocket className="h-3.5 w-3.5 shrink-0 text-muted-foreground" aria-hidden />
            {lastRelease ? (
              <>
                <span className="text-muted-foreground">{t("operations.entity.lastDeploy")}</span>
                <span className="font-mono">{releaseLabel(lastRelease)}</span>
                <span className="text-muted-foreground">{formatRelativeTime(releaseTime(lastRelease), lang)}</span>
              </>
            ) : (
              <span className="text-muted-foreground">{t("operations.entity.noDeploy", { env: envLabel })}</span>
            )}
          </span>
        </div>
      </div>
      <EnableObservabilityButton entity={entity} env={env} className="shrink-0" />
    </div>
  );
}
