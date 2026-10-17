import { Inbox } from "lucide-react";
import type { ReactNode } from "react";
import type { ComponentTable, ComponentTables } from "@/api";
import { tableKey } from "@/components/operations/tables/componentTables";
import { Button } from "@/components/ui/button";
import { Card } from "@/components/ui/card";
import { EmptyState } from "@/components/ui/empty-state";
import { Notice } from "@/components/ui/notice";
import { Skeleton } from "@/components/ui/skeleton";
import { useI18n } from "@/hooks/useI18n";
import type { KeyedRead } from "@/hooks/useOpsSignals";

interface ComponentTablesViewProps {
  read: KeyedRead<ComponentTables>;
  /** Which of the answer's tables this view lists; all of them when left out. */
  pick?: (tables: ComponentTable[]) => ComponentTable[];
  /** What is shown when no binding reads such a table. */
  empty: ReactNode;
  renderTable: (table: ComponentTable) => ReactNode;
}

/** The read states every recipe-table page shares: a skeleton, a failed first
 * read with Retry, the page's own empty state, then one card per table — and
 * a failed refresh kept beside the last answer. */
export function ComponentTablesView({ read, pick, empty, renderTable }: ComponentTablesViewProps) {
  const { t } = useI18n();
  const d = (key: string) => t(`operations.telemetry.table.${key}`);
  if (read.loading) {
    return (
      <div className="space-y-3" aria-busy="true">
        <Skeleton className="h-10 w-full" />
        <Skeleton className="h-48 w-full" />
      </div>
    );
  }
  if (read.error && !read.data) {
    return (
      <Card>
        <EmptyState
          icon={Inbox}
          variant="critical"
          title={d("loadFailed")}
          description={read.error}
          action={
            <Button variant="outline" size="sm" onClick={() => void read.reload()} disabled={read.fetching}>
              {t("operations.chart.retry")}
            </Button>
          }
        />
      </Card>
    );
  }
  const all = read.data?.tables ?? [];
  const tables = pick ? pick(all) : all;
  return (
    <div className="space-y-3">
      {read.error && read.data && (
        <Notice variant="warning" title={d("refreshFailed")} role="status">
          <Button variant="link" size="sm" className="h-auto p-0" onClick={() => void read.reload()} disabled={read.fetching}>
            {t("operations.chart.retry")}
          </Button>
        </Notice>
      )}
      {tables.length === 0
        ? empty
        : tables.map((table, i) => <div key={tableKey(table, i)}>{renderTable(table)}</div>)}
    </div>
  );
}
