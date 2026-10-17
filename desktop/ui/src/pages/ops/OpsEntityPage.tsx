import { CloudOff, SearchX } from "lucide-react";
import { useMemo } from "react";
import { Link, Outlet, useLocation, useParams } from "react-router-dom";
import type { DeployEnvironment, EnvironmentSummary } from "@/api";
import { PageSuspense } from "@/components/layout/PageSuspense";
import { EntityHeader } from "@/components/operations/entity/EntityHeader";
import { EntitySubNav } from "@/components/operations/entity/EntitySubNav";
import { useOpsContext, useOpsHref } from "@/components/operations/shell/OpsContext";
import { matchEntityRoute } from "@/components/operations/shell/opsNavigation";
import { useOpsEntity, type OpsEntity } from "@/components/operations/shell/useOpsEntities";
import { Button } from "@/components/ui/button";
import { Card } from "@/components/ui/card";
import { EmptyState } from "@/components/ui/empty-state";
import { Skeleton } from "@/components/ui/skeleton";
import { useI18n } from "@/hooks/useI18n";
import { useEntityHealth, useEntityReleases } from "@/hooks/useOpsEntityData";
import type { EntitySubPageProps } from "@/pages/ops/entity/types";

/** `/operations/entity/:componentId/*`: one entity, New Relic APM style — its
 * header, its own section list and the section. The environment is always the
 * one picked in the top bar, connected here or not. */
export default function OpsEntityPage() {
  const { t } = useI18n();
  const { componentId } = useParams();
  const { entity, envFor, loading, loaded, error, reload } = useOpsEntity(componentId);
  const { env } = useOpsContext();
  const href = useOpsHref();

  if (!entity) {
    // Nothing was ever read: the entity is not known to be missing, so the
    // failure is said as one, with a way to try again.
    if (!loaded && error && !loading) {
      return (
        <Card>
          <EmptyState
            icon={CloudOff}
            variant="critical"
            title={t("operations.entity.loadFailed.title")}
            description={error}
            action={
              <Button variant="outline" size="sm" onClick={reload}>
                {t("operations.entity.loadFailed.retry")}
              </Button>
            }
          />
        </Card>
      );
    }
    if (loading) {
      return (
        <div className="space-y-4" aria-busy>
          <Skeleton className="h-12 w-full max-w-lg" />
          <div className="flex gap-4">
            <Skeleton className="hidden h-56 w-44 lg:block" />
            <Skeleton className="h-96 flex-1" />
          </div>
        </div>
      );
    }
    return (
      <Card>
        <EmptyState
          icon={SearchX}
          title={t("operations.entity.notFound.title")}
          description={t("operations.entity.notFound.description")}
          action={
            <Button variant="outline" size="sm" asChild>
              <Link to={href("/operations")}>{t("operations.entity.notFound.action")}</Link>
            </Button>
          }
        />
      </Card>
    );
  }

  return <EntityView key={entity.id} entity={entity} env={env} environment={envFor(env)} />;
}

function EntityView({ entity, env, environment }: { entity: OpsEntity; env: DeployEnvironment; environment?: EnvironmentSummary }) {
  const { pathname } = useLocation();
  const active = matchEntityRoute(pathname)?.tab;
  const { health } = useEntityHealth(entity, env, environment);
  const { releases } = useEntityReleases(entity, env);
  const context = useMemo<EntitySubPageProps>(() => ({ entity, env, environment }), [entity, env, environment]);

  return (
    <div className="space-y-4">
      <EntityHeader entity={entity} env={env} environment={environment} health={health} lastRelease={releases[0]} />
      <div className="flex flex-col gap-4 lg:flex-row lg:items-start">
        <EntitySubNav entity={entity} active={active} />
        <div className="min-w-0 flex-1">
          <PageSuspense>
            <Outlet context={context} />
          </PageSuspense>
        </div>
      </div>
    </div>
  );
}
