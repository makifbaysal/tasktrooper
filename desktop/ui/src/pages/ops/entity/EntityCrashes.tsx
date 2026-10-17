import { ExternalLink, Smartphone } from "lucide-react";
import { useMemo, type ReactNode } from "react";
import { Link } from "react-router-dom";
import type { ComponentTable, TableColumn } from "@/api";
import { DATA_SOURCES_PATH } from "@/components/operations/console/ChartSlot";
import { entityPath, hasTab } from "@/components/operations/console/roleViews";
import { useOpsHref, useOpsPageScope } from "@/components/operations/shell/OpsContext";
import { ComponentTablesView } from "@/components/operations/tables/ComponentTablesView";
import {
  CRASH_MEASURES,
  SIGNATURE_MEASURES,
  crashTypeKey,
  headlineColumn,
  safeLink,
  sortBy,
  tablesNamed,
  type TableRow,
} from "@/components/operations/tables/componentTables";
import { DataTableCard } from "@/components/operations/tables/DataTableCard";
import { TableWindowNote } from "@/components/operations/tables/TableWindowNote";
import { useTableBindings } from "@/components/operations/tables/useTableBindings";
import { Badge, type BadgeProps } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card } from "@/components/ui/card";
import { EmptyState } from "@/components/ui/empty-state";
import { useI18n } from "@/hooks/useI18n";
import { useComponentCrashes } from "@/hooks/useOpsTables";
import { intlLocale } from "@/lib/languages";
import { formatDate, formatRelativeTime } from "@/lib/utils";
import type { EntitySubPageProps } from "@/pages/ops/entity/types";

const CRASHES = "crashes";
const SIGNATURES = "signatures";
// The issue id links to it already.
const HIDDEN_COLUMNS = ["link"];

const TYPE_TONE: Record<string, BadgeProps["variant"]> = {
  crash: "destructive",
  fatal: "destructive",
  anr: "warning",
  hangs: "warning",
  error: "warning",
};

/** A mobile app's Crashes: the crash and ANR issues its bound sources list
 * (Google Play's error issues, Sentry's issues), most users hit first, and
 * an iOS build's hang, launch and disk-write diagnostic signatures. */
export default function EntityCrashes({ entity, env }: EntitySubPageProps) {
  const { t, lang } = useI18n();
  const p = (key: string, params?: Record<string, string | number>) => t(`operations.telemetry.crashes.${key}`, params);
  const href = useOpsHref();
  useOpsPageScope({ time: false, env: true, note: p("scopeNote") });
  const formatCtx = useMemo(() => ({ locale: intlLocale(lang), t }), [lang, t]);
  const read = useComponentCrashes(entity.id, env);
  const signalsHref = hasTab(entity.role, "signals") ? href(entityPath(entity.id, "signals")) : undefined;
  const bindings = useTableBindings(entity.id, env, read.data?.tables ?? [], () => void read.reload());

  const typeWord = (raw: string) => {
    const key = `operations.telemetry.crashes.types.${crashTypeKey(raw)}`;
    const text = t(key);
    return text === key ? raw : text;
  };

  const renderCell = (column: TableColumn, row: TableRow): ReactNode | undefined => {
    const value = row[column.key];
    if (column.key === "issue") {
      const link = safeLink(row.link);
      if (value === null || value === undefined || value === "") return undefined;
      return link ? (
        <a href={link} target="_blank" rel="noreferrer" className="inline-flex max-w-full items-center gap-1 font-mono text-info hover:underline">
          <span className="truncate">{String(value)}</span>
          <ExternalLink className="h-3 w-3 shrink-0" aria-hidden />
          <span className="sr-only">{p("openIssue")}</span>
        </a>
      ) : (
        <span className="block truncate font-mono">{String(value)}</span>
      );
    }
    if (column.key === "type" && typeof value === "string" && value !== "") {
      return (
        <Badge variant={TYPE_TONE[crashTypeKey(value)] ?? "secondary"} className="px-1.5 py-0 text-micro whitespace-nowrap">
          {typeWord(value)}
        </Badge>
      );
    }
    if (column.key === "cause" && typeof value === "string") {
      return (
        <span className="line-clamp-2 break-words" title={value}>
          {value}
        </span>
      );
    }
    if (column.key === "last_seen" && typeof value === "string" && Number.isFinite(Date.parse(value))) {
      return (
        <span className="font-mono whitespace-nowrap tabular-nums" title={formatDate(value)}>
          {formatRelativeTime(value, lang)}
        </span>
      );
    }
    return undefined;
  };

  const card = (table: ComponentTable, measures: readonly string[]) => {
    const measure = headlineColumn(table.columns ?? [], measures);
    return (
      <DataTableCard
        table={table}
        formatCtx={formatCtx}
        barColumn={measure}
        defaultSort={sortBy(measure)}
        renderCell={renderCell}
        hiddenColumns={HIDDEN_COLUMNS}
        onEditBinding={bindings.edit(table)}
        signalsHref={signalsHref}
        readAt={read.data?.read_at}
        meta={<TableWindowNote window={bindings.windowOf(table)} />}
        onRetry={() => void read.reload()}
        retrying={read.fetching}
      />
    );
  };

  const all = read.data?.tables ?? [];
  const crashes = tablesNamed(all, CRASHES);
  const signatures = tablesNamed(all, SIGNATURES);

  const empty = (
    <Card>
      <EmptyState
        icon={Smartphone}
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
    <>
      <ComponentTablesView
        read={read}
        empty={empty}
        pick={(tables) => [...tablesNamed(tables, CRASHES), ...tablesNamed(tables, SIGNATURES)]}
        renderTable={(table) => {
          const isSignature = table.table === SIGNATURES;
          const first = isSignature ? signatures[0] === table : crashes[0] === table;
          return (
            <section className="space-y-2" aria-label={p(isSignature ? "signatures.title" : "issues.title")}>
              {first && (
                <div className="flex flex-wrap items-baseline justify-between gap-2 pt-1">
                  <h2 className="text-micro font-medium tracking-wide text-muted-foreground uppercase">
                    {p(isSignature ? "signatures.title" : "issues.title")}
                  </h2>
                  <p className="text-micro text-muted-foreground">{p(isSignature ? "signatures.hint" : "issues.hint")}</p>
                </div>
              )}
              {card(table, isSignature ? SIGNATURE_MEASURES : CRASH_MEASURES)}
            </section>
          );
        }}
      />
      {bindings.editor}
    </>
  );
}
