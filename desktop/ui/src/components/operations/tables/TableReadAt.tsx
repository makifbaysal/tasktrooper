import { Elapsed } from "@/components/ui/elapsed";
import { useI18n } from "@/hooks/useI18n";
import { cn, formatDate, formatRelativeTime } from "@/lib/utils";

interface TableReadAtProps {
  /** The answer's `read_at` (RFC3339); nothing is shown without one. */
  readAt?: string;
  className?: string;
}

/** "Read 2 minutes ago" for a table answer: the server keeps one 30 s, so
 * it can be older than the page's last refresh, which the title says. */
export function TableReadAt({ readAt, className }: TableReadAtProps) {
  const { t, lang } = useI18n();
  const at = readAt ? Date.parse(readAt) : Number.NaN;
  if (!readAt || !Number.isFinite(at)) return null;
  return (
    <span
      className={cn("text-micro whitespace-nowrap text-muted-foreground tabular-nums", className)}
      title={t("operations.telemetry.table.readAtTitle", { time: formatDate(readAt) })}
    >
      <Elapsed
        since={at}
        format={(ms) => t("operations.telemetry.table.readAt", { time: formatRelativeTime(new Date(Date.now() - ms).toISOString(), lang) })}
      />
    </span>
  );
}
