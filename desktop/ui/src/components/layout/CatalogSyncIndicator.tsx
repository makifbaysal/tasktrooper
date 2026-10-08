import { Loader2 } from "lucide-react";
import { Link } from "react-router-dom";
import type { CatalogSyncProgress } from "@/api";
import { Badge } from "@/components/ui/badge";
import { catalogSyncBusy } from "@/hooks/useCatalogSync";
import { useI18n } from "@/hooks/useI18n";

interface CatalogSyncIndicatorProps {
  progress: CatalogSyncProgress | null;
}

/**
 * The header's sign that the catalog is still adding agents. Indexing a new
 * agent's skills takes minutes, and without this the team simply looked
 * incomplete; it links to the catalog page, which shows the full progress.
 */
export function CatalogSyncIndicator({ progress }: CatalogSyncIndicatorProps) {
  const { t } = useI18n();
  if (!progress || !catalogSyncBusy(progress)) return null;

  const base = "frame.layout.header.catalogSync";
  const label = progress.agent
    ? t(progress.new_agent ? `${base}.adding` : `${base}.updating`, { agent: progress.agent })
    : t(`${base}.syncing`);
  const counts =
    progress.skills_total > 0
      ? t(`${base}.skills`, { done: progress.skills_done, total: progress.skills_total })
      : "";
  const detail = t(`${base}.detail`, {
    done: Math.min(progress.agents_done + 1, progress.agents_total),
    total: progress.agents_total,
  });

  return (
    <Link to="/settings/catalog" title={detail} aria-label={`${label} ${counts}`.trim()} data-testid="catalog-sync-indicator">
      <Badge variant="info" className="gap-1.5 px-2 py-0.5 text-micro">
        <Loader2 aria-hidden className="h-3 w-3 animate-spin" />
        <span className="hidden max-w-[16rem] truncate md:inline">{label}</span>
        {counts && <span className="font-mono tabular-nums">{counts}</span>}
      </Badge>
    </Link>
  );
}
