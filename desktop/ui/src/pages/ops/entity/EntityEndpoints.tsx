import { Gauge, OctagonAlert, Timer } from "lucide-react";
import { useMemo } from "react";
import { Link } from "react-router-dom";
import { DATA_SOURCES_PATH } from "@/components/operations/console/ChartSlot";
import { entityPath, hasTab } from "@/components/operations/console/roleViews";
import { useOpsHref, useOpsPageScope } from "@/components/operations/shell/OpsContext";
import { ComponentTablesView } from "@/components/operations/tables/ComponentTablesView";
import {
  ENDPOINT_THROUGHPUT,
  headlineColumn,
  sortBy,
  worstEndpoints,
  type EndpointFlag,
  type TableRow,
} from "@/components/operations/tables/componentTables";
import { DataTableCard, type RowFlag } from "@/components/operations/tables/DataTableCard";
import { TableWindowNote } from "@/components/operations/tables/TableWindowNote";
import { useTableBindings } from "@/components/operations/tables/useTableBindings";
import { Button } from "@/components/ui/button";
import { Card } from "@/components/ui/card";
import { EmptyState } from "@/components/ui/empty-state";
import { useI18n } from "@/hooks/useI18n";
import { useComponentEndpoints } from "@/hooks/useOpsTables";
import { intlLocale } from "@/lib/languages";
import type { EntitySubPageProps } from "@/pages/ops/entity/types";

/** A backend's Endpoints, New Relic's Transactions: throughput, error rate
 * and p95 per endpoint over the bound source's own recent window, busiest
 * first, the most failing and the slowest called out. */
export default function EntityEndpoints({ entity, env }: EntitySubPageProps) {
  const { t, lang } = useI18n();
  const p = (key: string, params?: Record<string, string | number>) => t(`operations.telemetry.endpoints.${key}`, params);
  const href = useOpsHref();
  useOpsPageScope({ time: false, env: true, note: p("scopeNote") });
  const formatCtx = useMemo(() => ({ locale: intlLocale(lang), t }), [lang, t]);
  const read = useComponentEndpoints(entity.id, env);
  const signalsHref = hasTab(entity.role, "signals") ? href(entityPath(entity.id, "signals")) : undefined;
  const bindings = useTableBindings(entity.id, env, read.data?.tables ?? [], () => void read.reload());

  const flagWords: Record<EndpointFlag, RowFlag> = {
    errors: { label: p("flags.errors"), tone: "destructive", icon: <OctagonAlert className="h-3.5 w-3.5" /> },
    slowest: { label: p("flags.slowest"), tone: "warning", icon: <Timer className="h-3.5 w-3.5" /> },
  };

  const empty = (
    <Card>
      <EmptyState
        icon={Gauge}
        title={p("empty.title")}
        description={p("empty.body", { name: entity.name })}
        action={
          <div className="flex flex-wrap justify-center gap-2">
            {signalsHref && (
              <Button size="sm" asChild>
                <Link to={signalsHref}>{p("empty.signals")}</Link>
              </Button>
            )}
            <Button size="sm" variant="outline" asChild>
              <Link to={href(DATA_SOURCES_PATH)}>{p("empty.addSource")}</Link>
            </Button>
          </div>
        }
      />
    </Card>
  );

  return (
    <div className="space-y-3">
      <div className="flex flex-wrap items-baseline justify-between gap-2">
        <h2 className="text-micro font-medium tracking-wide text-muted-foreground uppercase">{p("title")}</h2>
        <p className="text-micro text-muted-foreground">{p("hint")}</p>
      </div>
      <ComponentTablesView
        read={read}
        empty={empty}
        renderTable={(table) => {
          const columns = table.columns ?? [];
          const throughput = headlineColumn(columns, ENDPOINT_THROUGHPUT);
          const worst = worstEndpoints(table.rows ?? [], columns);
          const flags = new Map<TableRow, RowFlag[]>([...worst].map(([row, reasons]) => [row, reasons.map((r) => flagWords[r])]));
          return (
            <DataTableCard
              table={table}
              formatCtx={formatCtx}
              barColumn={throughput}
              defaultSort={sortBy(throughput)}
              flags={flags}
              onEditBinding={bindings.edit(table)}
              signalsHref={signalsHref}
              readAt={read.data?.read_at}
              meta={<TableWindowNote window={bindings.windowOf(table)} />}
              onRetry={() => void read.reload()}
              retrying={read.fetching}
            />
          );
        }}
      />
      {bindings.editor}
    </div>
  );
}
