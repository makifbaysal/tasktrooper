import type { ScanSummary } from "@/api";
import { Badge } from "@/components/ui/badge";
import { Spinner } from "@/components/ui/spinner";
import { useI18n } from "@/hooks/useI18n";
import { cn, formatRelativeDate } from "@/lib/utils";

interface ScanStatusLabelProps {
  scan?: ScanSummary;
  className?: string;
}

/** A repository's last scan, as one of: never run, a running spinner, a
 * failed badge, or the relative time of the finished run. */
export function ScanStatusLabel({ scan, className }: ScanStatusLabelProps) {
  const { t } = useI18n();

  if (!scan) {
    return <span className={cn("text-muted-foreground", className)}>{t("projectsHub.card.scanNone")}</span>;
  }

  if (scan.status === "running" || scan.status === "queued") {
    return (
      <span className={cn("inline-flex items-center gap-1.5 text-muted-foreground", className)}>
        <Spinner size="sm" className="h-3.5 w-3.5" />
        {t("projectsHub.card.scanRunning")}
      </span>
    );
  }

  if (scan.status === "failed") {
    return (
      <Badge variant="destructive" className={className}>
        {t("projectsHub.card.scanFailed")}
      </Badge>
    );
  }

  return (
    <span className={cn("text-muted-foreground", className)}>
      {formatRelativeDate(scan.finished_at ?? scan.started_at)}
    </span>
  );
}
