import { Plug, Unplug } from "lucide-react";
import { Link } from "react-router-dom";
import type { DeployEnvironment } from "@/api";
import { repositoryDeployPath } from "@/components/operations/console/opsEntities";
import type { OpsEntity } from "@/components/operations/shell/useOpsEntities";
import { Button } from "@/components/ui/button";
import { Card } from "@/components/ui/card";
import { EmptyState } from "@/components/ui/empty-state";
import { useI18n } from "@/hooks/useI18n";

/** An entity sub-page whose entity has no environment in the one picked:
 * nothing to read, only the way to connect one. */
export function EntityNotReporting({ entity, env, description }: { entity: OpsEntity; env: DeployEnvironment; description: string }) {
  const { t } = useI18n();
  return (
    <Card>
      <EmptyState
        icon={Unplug}
        title={t("operations.entity.summary.notReporting.title", { env: t(`cloud.environments.${env}`) })}
        description={description}
        action={
          <Button variant="outline" size="sm" asChild>
            <Link to={repositoryDeployPath(entity.repositoryId)}>
              <Plug aria-hidden />
              {t("operations.entity.connect")}
            </Link>
          </Button>
        }
      />
    </Card>
  );
}
