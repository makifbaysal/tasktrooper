import { Plug } from "lucide-react";
import { useMemo, type ReactNode } from "react";
import { Link, useNavigate } from "react-router-dom";
import { hasTrend } from "@/components/operations/charts/chartData";
import { Sparkline } from "@/components/operations/charts/Sparkline";
import { roleHasGolden } from "@/components/operations/console/entityTypes";
import { goldenCell, type GoldenIndex, type GoldenReadState } from "@/components/operations/console/goldenMetrics";
import { HealthSquare, healthReasonText } from "@/components/operations/console/HealthSquare";
import {
  entityPath,
  issuesPath,
  releaseLabel,
  releaseTime,
  repositoryDeployPath,
  type ExplorerRow,
  type ExplorerRowType,
} from "@/components/operations/console/opsEntities";
import type { SignalSpec } from "@/components/operations/console/roleViews";
import { formatSignalValue, type SignalFormatContext } from "@/components/operations/console/signalFormat";
import { useOpsHref } from "@/components/operations/shell/OpsContext";
import { storePlatformLabelKey } from "@/components/operations/storeTestBuilds";
import { Card } from "@/components/ui/card";
import { Skeleton } from "@/components/ui/skeleton";
import { useI18n } from "@/hooks/useI18n";
import { intlLocale } from "@/lib/languages";
import { cn, formatDate, formatRelativeTime } from "@/lib/utils";

const GROUP_ORDER: ExplorerRowType[] = ["services", "web", "mobile", "workers", "other", "repository"];

export type RowSeries = Map<string, Array<[number, number | null]>>;

interface EntityTableProps {
  rows: ExplorerRow[];
  golden?: SignalSpec[];
  groupByType?: boolean;
  showType?: boolean;
  series?: RowSeries;
  /** The golden metrics read for the rows' environment and range. */
  goldenValues?: GoldenIndex;
  /** That read's state, so a cell in flight or failed is not read as unbound. */
  goldenRead?: GoldenReadState;
}

const SETTLED: GoldenReadState = { data: null, loading: false };

const TH = "px-3 py-2 text-left text-micro font-medium tracking-wide text-muted-foreground uppercase whitespace-nowrap";
const TD = "px-3 py-2 align-middle";

/** The explorer's dense list: health, name, type, the type's golden metrics
 * (blank until a data source is bound), errors, open issues and last deploy. */
export function EntityTable({ rows, golden = [], groupByType = false, showType = true, series, goldenValues, goldenRead = SETTLED }: EntityTableProps) {
  const { t, lang } = useI18n();
  const locale = intlLocale(lang);
  const formatCtx = useMemo<SignalFormatContext>(() => ({ locale, t }), [locale, t]);
  const withTrend = series !== undefined && rows.some((r) => (series.get(r.key)?.length ?? 0) > 0);
  const columnCount = 5 + (showType ? 1 : 0) + golden.length + (withTrend ? 1 : 0);

  const groups = groupByType
    ? GROUP_ORDER.map((type) => ({ type, rows: rows.filter((r) => r.type === type) })).filter((g) => g.rows.length > 0)
    : [{ type: undefined, rows }];

  return (
    <Card className="overflow-hidden">
      <div className="overflow-x-auto">
        <table className="w-full min-w-[720px] border-collapse text-caption" aria-label={t("operations.explorer.listLabel")}>
          <thead className="bg-muted/40">
            <tr className="border-b border-border">
              <th scope="col" className={cn(TH, "w-10")}>
                <span className="sr-only">{t("operations.explorer.columns.health")}</span>
              </th>
              <th scope="col" className={TH}>{t("operations.explorer.columns.name")}</th>
              {showType && <th scope="col" className={TH}>{t("operations.explorer.columns.type")}</th>}
              {golden.map((spec) => (
                <th key={spec.id} scope="col" className={cn(TH, "text-right")} title={t(`operations.signals.${spec.label}`)}>
                  {t(`operations.signals.${spec.short ?? spec.label}`)}
                </th>
              ))}
              {withTrend && <th scope="col" className={TH}>{t("operations.explorer.columns.trend")}</th>}
              <th scope="col" className={cn(TH, "text-right")}>{t("operations.explorer.columns.errors24h")}</th>
              <th scope="col" className={cn(TH, "text-right")}>{t("operations.explorer.columns.openIssues")}</th>
              <th scope="col" className={TH}>{t("operations.explorer.columns.lastDeploy")}</th>
            </tr>
          </thead>
          {groups.map((group) => (
            <tbody key={group.type ?? "all"}>
              {group.type && (
                <tr className="border-b border-border bg-surface-sunken/60">
                  <th scope="colgroup" colSpan={columnCount} className="px-3 py-1.5 text-left text-caption font-semibold">
                    {t(`operations.explorer.types.${group.type}`)}
                    <span className="ml-2 font-mono font-normal text-muted-foreground tabular-nums">{group.rows.length}</span>
                  </th>
                </tr>
              )}
              {group.rows.map((row) => (
                <EntityTableRow
                  key={row.key}
                  row={row}
                  golden={golden}
                  showType={showType}
                  trend={withTrend ? (series?.get(row.key) ?? []) : undefined}
                  goldenValues={goldenValues}
                  goldenRead={goldenRead}
                  formatCtx={formatCtx}
                />
              ))}
            </tbody>
          ))}
        </table>
      </div>
    </Card>
  );
}

function Blank({ label }: { label: string }) {
  return (
    <span title={label} className="text-muted-foreground">
      <span aria-hidden>—</span>
      <span className="sr-only">{label}</span>
    </span>
  );
}

interface RowProps {
  row: ExplorerRow;
  golden: SignalSpec[];
  showType: boolean;
  trend?: Array<[number, number | null]>;
  goldenValues?: GoldenIndex;
  goldenRead: GoldenReadState;
  formatCtx: SignalFormatContext;
}

const NO_VALUES: GoldenIndex = new Map();

interface GoldenCellProps {
  spec: SignalSpec;
  row: ExplorerRow;
  values?: GoldenIndex;
  read: GoldenReadState;
  formatCtx: SignalFormatContext;
}

function GoldenCell({ spec, row, values, read, formatCtx }: GoldenCellProps) {
  const { t } = useI18n();
  if (row.kind !== "entity" || !roleHasGolden(row.role, spec.id)) return <Blank label={t("operations.explorer.notApplicable")} />;
  const cell = goldenCell(read, values ?? NO_VALUES, row.entityId, spec.id);
  switch (cell.kind) {
    case "loading":
      return (
        <span className="inline-flex justify-end">
          <Skeleton className="h-3.5 w-12" aria-hidden />
          <span className="sr-only">{t("operations.explorer.goldenLoading")}</span>
        </span>
      );
    case "failed":
      return <Blank label={t("operations.explorer.goldenFailed")} />;
    case "unbound":
      return <Blank label={t("operations.explorer.noSource")} />;
    case "noData":
      return <Blank label={t("operations.explorer.noData")} />;
    case "unknown":
      return <Blank label={t("operations.explorer.noValue")} />;
  }
  const found = cell.value;
  const trend = !found.live && hasTrend(found.spark);
  return (
    <span className="inline-flex items-center justify-end gap-2" title={found.live ? t("operations.explorer.liveValue") : undefined}>
      {trend && <Sparkline points={found.spark} width={48} height={16} />}
      <span className="text-foreground">{formatSignalValue(found.value, found.unit, formatCtx)}</span>
    </span>
  );
}

function EntityTableRow({ row, golden, showType, trend, goldenValues, goldenRead, formatCtx }: RowProps) {
  const { t, lang } = useI18n();
  const navigate = useNavigate();
  const href = useOpsHref();
  const isEntity = row.kind === "entity" && row.entityId !== undefined;
  const name = row.name || t("operations.explorer.unknownRepository");
  const target = row.href
    ? href(row.href)
    : isEntity
      ? href(entityPath(row.entityId!))
      : href(issuesPath({ repositoryId: row.repositoryId }));
  const issuesTarget = href(isEntity ? issuesPath({ entityId: row.entityId }) : issuesPath({ repositoryId: row.repositoryId }));
  const env = row.environment;
  const release = row.lastRelease;

  let subtitle: ReactNode = null;
  if (row.kind === "storeApps") {
    subtitle = t("operations.explorer.storeAppsRow", {
      platforms: (row.platforms ?? []).map((p) => t(storePlatformLabelKey(p))).join(" · "),
    });
  } else if (!isEntity) subtitle = t("operations.explorer.repositoryRow");
  else if (row.repositoryName && row.repositoryName !== row.name) subtitle = row.repositoryName;

  return (
    <tr
      className="cursor-pointer border-b border-border/60 last:border-0 hover:bg-muted/40"
      onClick={(e) => {
        if ((e.target as HTMLElement).closest("a, button")) return;
        navigate(target);
      }}
    >
      <td className={cn(TD, "w-10")}>
        <HealthSquare status={row.health.status} reason={healthReasonText(t, row.health)} />
      </td>
      <td className={cn(TD, "max-w-80")}>
        <div className="flex min-w-0 items-center gap-2">
          <Link to={target} className="truncate text-[13px] font-medium hover:underline focus-visible:underline focus-visible:outline-none">
            {name}
          </Link>
          {isEntity && !env && (
            <Link
              to={repositoryDeployPath(row.repositoryId)}
              aria-label={t("operations.explorer.connectLabel", { name })}
              className="inline-flex shrink-0 items-center gap-1 text-micro font-medium text-info hover:underline"
            >
              <Plug className="h-3 w-3" aria-hidden />
              {t("operations.explorer.connect")}
            </Link>
          )}
        </div>
        {subtitle && <p className="truncate text-micro text-muted-foreground">{subtitle}</p>}
      </td>
      {showType && (
        <td className={cn(TD, "whitespace-nowrap text-muted-foreground")}>
          {row.role
            ? t(`projectModel.roles.${row.role}`)
            : row.kind === "storeApps"
              ? t("operations.explorer.storeApps")
              : t("operations.explorer.types.repository")}
        </td>
      )}
      {golden.map((spec) => (
        <td key={spec.id} className={cn(TD, "whitespace-nowrap text-right font-mono tabular-nums")}>
          <GoldenCell spec={spec} row={row} values={goldenValues} read={goldenRead} formatCtx={formatCtx} />
        </td>
      ))}
      {trend !== undefined && (
        <td className={TD}>{trend.length > 0 ? <Sparkline points={trend} /> : <Blank label={t("operations.explorer.noSource")} />}</td>
      )}
      <td className={cn(TD, "text-right font-mono tabular-nums")}>
        {env ? (
          <span className={env.error_count_24h > 0 ? "font-semibold text-foreground" : "text-muted-foreground"}>{env.error_count_24h}</span>
        ) : (
          <Blank label={t("operations.explorer.notConnectedHere")} />
        )}
      </td>
      <td className={cn(TD, "text-right font-mono tabular-nums")}>
        {row.incidents.length > 0 ? (
          <Link
            to={issuesTarget}
            aria-label={t("operations.explorer.issuesLabel", { count: row.incidents.length, name })}
            className="font-semibold text-foreground hover:underline"
          >
            {row.incidents.length}
          </Link>
        ) : (
          <span className="text-muted-foreground">0</span>
        )}
      </td>
      <td className={cn(TD, "whitespace-nowrap")}>
        {release ? (
          <span className="flex items-baseline gap-2" title={formatDate(releaseTime(release))}>
            <span className="font-mono">{releaseLabel(release)}</span>
            <span className="text-micro text-muted-foreground">{formatRelativeTime(releaseTime(release), lang)}</span>
          </span>
        ) : (
          <span className="text-muted-foreground">{t("operations.explorer.noDeploy")}</span>
        )}
      </td>
    </tr>
  );
}
