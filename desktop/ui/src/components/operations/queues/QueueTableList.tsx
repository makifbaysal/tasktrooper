import { useMemo } from "react";
import type { DeployEnvironment, QueueTable } from "@/api";
import type { SignalFormatContext } from "@/components/operations/console/signalFormat";
import { QueueTableCard } from "@/components/operations/queues/QueueTableCard";
import { backlogThreshold } from "@/components/operations/queues/queueTables";
import { useAlertRules } from "@/hooks/useOpsAlerts";

interface QueueTableListProps {
  componentId: string;
  env: DeployEnvironment;
  tables: QueueTable[];
  formatCtx: SignalFormatContext;
  onEditBinding: (table: QueueTable) => void;
  onRetry: () => void;
  retrying: boolean;
  readAt?: string;
}

/** Every queue table of the component, its worst rows measured against the
 * backlog alert's threshold when a rule sets one. Mounted only with tables to
 * show, so a component without any reads no rules. */
export function QueueTableList({ componentId, env, tables, formatCtx, onEditBinding, onRetry, retrying, readAt }: QueueTableListProps) {
  const rules = useAlertRules(componentId);
  const threshold = useMemo(() => backlogThreshold(rules.data, env), [rules.data, env]);
  return (
    <div className="space-y-3">
      {tables.map((table, i) => (
        <QueueTableCard
          key={`${table.signal_binding_id}:${table.data_source_id}:${i}`}
          table={table}
          formatCtx={formatCtx}
          threshold={threshold}
          onEditBinding={() => onEditBinding(table)}
          onRetry={onRetry}
          retrying={retrying}
          readAt={readAt}
        />
      ))}
    </div>
  );
}
