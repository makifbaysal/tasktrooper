import { SearchX } from "lucide-react";
import type { ReactNode } from "react";
import { TelemetryGuide } from "@/components/operations/telemetry/TelemetryGate";
import { EmptyState } from "@/components/ui/empty-state";
import { Notice } from "@/components/ui/notice";
import { Skeleton } from "@/components/ui/skeleton";
import { useI18n } from "@/hooks/useI18n";
import type { TelemetryQueryState } from "@/hooks/useTelemetryQuery";

interface TelemetryQueryViewProps<T> {
  query: TelemetryQueryState<T>;
  children: (data: T) => ReactNode;
  /** True when the answer holds nothing to list. */
  isEmpty?: (data: T) => boolean;
  emptyTitle?: string;
  emptyDescription?: string;
  skeletonHeight?: string;
}

/** The three states every telemetry read shares: loading, failed (or the
 * server lost its configuration meanwhile) and nothing to show. */
export function TelemetryQueryView<T>({ query, children, isEmpty, emptyTitle, emptyDescription, skeletonHeight = "h-48" }: TelemetryQueryViewProps<T>) {
  const { t } = useI18n();
  if (query.notConfigured) return <TelemetryGuide connection="notConfigured" onRetry={query.reload} />;
  if (query.data === undefined) {
    if (query.error) {
      return (
        <Notice variant="error" title={t("telemetryHub.common.loadFailed")}>
          {query.error}
        </Notice>
      );
    }
    return <Skeleton className={`${skeletonHeight} w-full rounded-xl`} aria-busy="true" />;
  }
  return (
    <div className="space-y-3">
      {query.error && (
        <Notice variant="warning" title={t("telemetryHub.common.refreshFailed")}>
          {query.error}
        </Notice>
      )}
      {isEmpty?.(query.data) ? (
        <EmptyState
          icon={SearchX}
          variant="search"
          title={emptyTitle ?? t("telemetryHub.common.empty")}
          description={emptyDescription}
        />
      ) : (
        children(query.data)
      )}
    </div>
  );
}
