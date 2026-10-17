import { Database, Inbox, Link2, Sparkles } from "lucide-react";
import { Link } from "react-router-dom";
import { Button } from "@/components/ui/button";
import { Card } from "@/components/ui/card";
import { EmptyState } from "@/components/ui/empty-state";
import { Notice } from "@/components/ui/notice";
import { useI18n } from "@/hooks/useI18n";

interface QueuesEmptyStateProps {
  name: string;
  /** Suggestions waiting for a `queue.*` signal. */
  suggestions: number;
  /** A search ran on this visit and found nothing new. */
  searchedNone: boolean;
  suggesting: boolean;
  onFindSuggestions: () => void;
  onBind: () => void;
  sourcesHref: string;
  /** The entity's Signals page, where suggestions are accepted. */
  signalsHref?: string;
}

/** No `queue.*` signal is bound yet: where queue data comes from, a search
 * for sources that fit, binding the backlog by hand, or adding a broker. */
export function QueuesEmptyState({
  name,
  suggestions,
  searchedNone,
  suggesting,
  onFindSuggestions,
  onBind,
  sourcesHref,
  signalsHref,
}: QueuesEmptyStateProps) {
  const { t } = useI18n();
  const e = (key: string, params?: Record<string, string | number>) => t(`operations.queues.empty.${key}`, params);
  return (
    <div className="space-y-3">
      {suggestions > 0 && (
        <Notice variant="info" title={e("found", { count: suggestions, name })} role="status">
          {signalsHref && (
            <Link to={signalsHref} className="font-medium text-info hover:underline">
              {e("review")}
            </Link>
          )}
        </Notice>
      )}
      {suggestions === 0 && searchedNone && (
        <Notice variant="info" title={e("noneTitle")} role="status">
          {e("none", { name })}
        </Notice>
      )}
      <Card>
        <EmptyState
          icon={Inbox}
          title={t("operations.entity.queues.title")}
          description={t("operations.entity.queues.description", { name })}
          action={
            <div className="flex flex-col items-center gap-3">
              <div className="flex flex-wrap justify-center gap-2">
                <Button size="sm" onClick={onFindSuggestions} disabled={suggesting}>
                  <Sparkles aria-hidden />
                  {suggesting ? e("finding") : e("findSuggestions")}
                </Button>
                <Button variant="outline" size="sm" onClick={onBind}>
                  <Link2 aria-hidden />
                  {e("bindBacklog")}
                </Button>
              </div>
              <Link to={sourcesHref} className="inline-flex items-center gap-1.5 text-caption font-medium text-info hover:underline">
                <Database className="h-3.5 w-3.5" aria-hidden />
                {e("addSource")}
              </Link>
            </div>
          }
        />
      </Card>
    </div>
  );
}
