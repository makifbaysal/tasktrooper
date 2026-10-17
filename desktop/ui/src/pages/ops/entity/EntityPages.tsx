import { Globe } from "lucide-react";
import { useMemo } from "react";
import { Link, useSearchParams } from "react-router-dom";
import { WEB_TABLES, type WebTableName } from "@/api";
import { DATA_SOURCES_PATH } from "@/components/operations/console/ChartSlot";
import { entityPath, hasTab } from "@/components/operations/console/roleViews";
import { useOpsHref, useOpsPageScope } from "@/components/operations/shell/OpsContext";
import { ComponentTablesView } from "@/components/operations/tables/ComponentTablesView";
import { WEB_MEASURES, headlineColumn, sortBy, tablesNamed } from "@/components/operations/tables/componentTables";
import { DataTableCard } from "@/components/operations/tables/DataTableCard";
import { TableWindowNote } from "@/components/operations/tables/TableWindowNote";
import { useTableBindings } from "@/components/operations/tables/useTableBindings";
import { Button } from "@/components/ui/button";
import { Card } from "@/components/ui/card";
import { EmptyState } from "@/components/ui/empty-state";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { useI18n } from "@/hooks/useI18n";
import { useComponentWeb } from "@/hooks/useOpsTables";
import { intlLocale } from "@/lib/languages";
import type { EntitySubPageProps } from "@/pages/ops/entity/types";

const TABLE_PARAM = "table";

export function webTableOf(raw: string | null): WebTableName {
  return WEB_TABLES.find((t) => t === raw) ?? "pages";
}

// Names a row and reads as prose, not code: a referrer's site, a country,
// a device class. A page's path stays mono.
const PROSE_COLUMNS = ["referrer", "source", "country", "country_name", "device", "device_type", "browser", "os"];

/** A frontend's Pages, New Relic Browser's page views: the bound web
 * analytics source's pages, referrers, countries and devices, one table per
 * tab (`?table=`), busiest first with bars. */
export default function EntityPages({ entity, env }: EntitySubPageProps) {
  const { t, lang } = useI18n();
  const p = (key: string, params?: Record<string, string | number>) => t(`operations.telemetry.pages.${key}`, params);
  const href = useOpsHref();
  useOpsPageScope({ time: false, env: true, note: p("scopeNote") });
  const [params, setParams] = useSearchParams();
  const table = webTableOf(params.get(TABLE_PARAM));
  const formatCtx = useMemo(() => ({ locale: intlLocale(lang), t }), [lang, t]);
  const read = useComponentWeb(entity.id, env, table);
  const signalsHref = hasTab(entity.role, "signals") ? href(entityPath(entity.id, "signals")) : undefined;
  const bindings = useTableBindings(entity.id, env, read.data?.tables ?? [], () => void read.reload());

  const select = (next: string) =>
    setParams(
      (prev) => {
        const out = new URLSearchParams(prev);
        if (next === "pages") out.delete(TABLE_PARAM);
        else out.set(TABLE_PARAM, next);
        return out;
      },
      { replace: true },
    );

  const empty = (
    <Card>
      <EmptyState
        icon={Globe}
        title={p("empty.title", { table: p(`tabs.${table}`) })}
        description={p("empty.body", { name: entity.name, table: p(`tabs.${table}`) })}
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
      <Tabs value={table} onValueChange={select} variant="pill">
        <div className="flex flex-wrap items-center justify-between gap-2">
          <TabsList aria-label={p("tabsLabel")}>
            {WEB_TABLES.map((name) => (
              <TabsTrigger key={name} value={name}>
                {p(`tabs.${name}`)}
              </TabsTrigger>
            ))}
          </TabsList>
          <p className="text-micro text-muted-foreground">{p("hint")}</p>
        </div>
        <TabsContent value={table} className="mt-3">
          <ComponentTablesView
            read={read}
            pick={(tables) => {
              const named = tablesNamed(tables, table);
              return named.length > 0 ? named : tables;
            }}
            empty={empty}
            renderTable={(tbl) => {
              const measure = headlineColumn(tbl.columns ?? [], WEB_MEASURES);
              return (
                <DataTableCard
                  table={tbl}
                  formatCtx={formatCtx}
                  barColumn={measure}
                  defaultSort={sortBy(measure)}
                  textColumns={PROSE_COLUMNS}
                  onEditBinding={bindings.edit(tbl)}
                  signalsHref={signalsHref}
                  readAt={read.data?.read_at}
                  meta={<TableWindowNote window={bindings.windowOf(tbl)} />}
                  onRetry={() => void read.reload()}
                  retrying={read.fetching}
                />
              );
            }}
          />
        </TabsContent>
      </Tabs>
      {bindings.editor}
    </div>
  );
}
