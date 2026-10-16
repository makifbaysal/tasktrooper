import { Inbox } from "lucide-react";
import { Link } from "react-router-dom";
import { DATA_SOURCES_PATH } from "@/components/operations/console/ChartSlot";
import { useOpsHref, useOpsPageScope } from "@/components/operations/shell/OpsContext";
import { Button } from "@/components/ui/button";
import { Card } from "@/components/ui/card";
import { EmptyState } from "@/components/ui/empty-state";
import { useI18n } from "@/hooks/useI18n";
import type { EntitySubPageProps } from "@/pages/ops/entity/types";

/** A worker's queues come from its broker's metrics; until a data source
 * sends them there is nothing to draw, only the way to bind one. */
export default function EntityQueues({ entity }: EntitySubPageProps) {
  const { t } = useI18n();
  const href = useOpsHref();
  useOpsPageScope({ time: false, env: false });
  return (
    <Card>
      <EmptyState
        icon={Inbox}
        title={t("operations.entity.queues.title")}
        description={t("operations.entity.queues.description", { name: entity.name })}
        action={
          <Button variant="outline" size="sm" asChild>
            <Link to={href(DATA_SOURCES_PATH)}>{t("operations.console.connectSource")}</Link>
          </Button>
        }
      />
    </Card>
  );
}
