import { Search } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Card } from "@/components/ui/card";
import { Spinner } from "@/components/ui/spinner";
import { useI18n } from "@/hooks/useI18n";

export interface EmbeddingMapSearchSummaryProps {
  query: string;
  total: number;
  placed: number;
  groups: { id: string; label: string; color: string; count: number }[];
  locating: boolean;
  onClear?: () => void;
}

export function EmbeddingMapSearchSummary({
  query,
  total,
  placed,
  groups,
  locating,
  onClear,
}: EmbeddingMapSearchSummaryProps) {
  const { t } = useI18n();
  const s = (key: string, params?: Record<string, string | number>) =>
    t(`content.embeddingMap.search.${key}`, params);

  return (
    <Card className="flex flex-wrap items-start justify-between gap-3 p-4 shadow-none">
      <div className="min-w-0 flex-1 space-y-2">
        <div className="flex items-center gap-2">
          <Search className="h-4 w-4 shrink-0 text-muted-foreground" aria-hidden />
          <h3 className="break-words text-sm font-medium">{s("title", { query })}</h3>
        </div>
        <p className="text-xs text-muted-foreground">{s("hint", { count: total })}</p>
        {locating ? (
          <div className="flex items-center gap-2 text-sm text-muted-foreground" aria-live="polite">
            <Spinner size="sm" />
            <span>{s("locating")}</span>
          </div>
        ) : (
          <>
            {groups.length > 0 && (
              <ul className="flex flex-wrap gap-x-4 gap-y-1 text-caption">
                {groups.map((group) => (
                  <li key={group.id} className="flex items-center gap-1.5">
                    <span
                      aria-hidden
                      className="h-2 w-2 shrink-0 rounded-full"
                      style={{ backgroundColor: group.color }}
                    />
                    <span>{group.label}</span>
                    <span className="text-muted-foreground">{s("groupHits", { count: group.count })}</span>
                  </li>
                ))}
              </ul>
            )}
            {placed < total && (
              <p className="text-caption text-muted-foreground">{s("unplaced", { count: total - placed })}</p>
            )}
          </>
        )}
      </div>
      {onClear && (
        <Button size="sm" variant="outline" onClick={onClear}>
          {s("clear")}
        </Button>
      )}
    </Card>
  );
}
