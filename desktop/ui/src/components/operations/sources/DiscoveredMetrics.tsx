import { CircleCheck, Search } from "lucide-react";
import { useEffect, useRef, useState } from "react";
import { ApiError, api, type DataSourceDiscovery } from "@/api";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Notice } from "@/components/ui/notice";
import { Skeleton } from "@/components/ui/skeleton";
import { useI18n } from "@/hooks/useI18n";

export const DISCOVER_LIMIT = 200;
const SEARCH_DEBOUNCE_MS = 300;

interface DiscoveredMetricsProps {
  sourceId: string;
  /** Present for a source that can run queries: a metric name then fills the query box. */
  onPickMetric?: (metric: string) => void;
  /** False for a name no query can read as itself (a RabbitMQ queue named with `*`): it is listed, not offered. */
  canPick?: (metric: string) => boolean;
}

const ANY_NAME = () => true;

/** What a source holds: its metric names (filtered on the server) and the
 * catalog recipes whose telltale metric is among them. */
export function DiscoveredMetrics({ sourceId, onPickMetric, canPick = ANY_NAME }: DiscoveredMetricsProps) {
  const { t } = useI18n();
  const d = (key: string, params?: Record<string, string | number>) => t(`operations.sources.discover.${key}`, params);
  const [search, setSearch] = useState("");
  const [match, setMatch] = useState("");
  const [result, setResult] = useState<DataSourceDiscovery | null>(null);
  const [loading, setLoading] = useState(true);
  const [failure, setFailure] = useState<{ title: string; message: string } | null>(null);
  const seq = useRef(0);

  useEffect(() => {
    const id = window.setTimeout(() => setMatch(search.trim()), SEARCH_DEBOUNCE_MS);
    return () => window.clearTimeout(id);
  }, [search]);

  useEffect(() => {
    const mine = ++seq.current;
    setLoading(true);
    api
      .discoverDataSource(sourceId, { match, limit: DISCOVER_LIMIT })
      .then((res) => {
        if (mine !== seq.current) return;
        setResult(res);
        setFailure(null);
      })
      .catch((err: unknown) => {
        if (mine !== seq.current) return;
        const unsupported = err instanceof ApiError && err.type === "unsupported";
        setFailure({
          title: unsupported ? d("unsupported") : d("failed"),
          message: unsupported ? "" : err instanceof Error ? err.message : t("common.actionFailed"),
        });
      })
      .finally(() => {
        if (mine === seq.current) setLoading(false);
      });
    // `d` and `t` only format text; a language switch must not re-read the source.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [sourceId, match]);

  const metrics = result?.metrics ?? [];
  const recipes = result?.recipes ?? [];

  return (
    <section className="space-y-3" aria-labelledby="ds-discover-title">
      <div>
        <h3 id="ds-discover-title" className="text-heading font-semibold">
          {d("title")}
        </h3>
        <p className="text-caption text-muted-foreground">{d("description")}</p>
      </div>

      {result && (
        <div className="space-y-1.5">
          <p className="text-caption font-medium text-muted-foreground">{d("recipes")}</p>
          {recipes.length === 0 ? (
            <p className="text-caption text-muted-foreground">{d("noRecipes")}</p>
          ) : (
            <ul className="flex flex-wrap gap-1.5" aria-label={d("recipes")}>
              {recipes.map((r) => (
                <li key={r.recipe_id}>
                  <Badge variant="success" className="gap-1 font-mono" title={d("recipeMatched", { metric: r.matched_metric })}>
                    {r.recipe_id}
                    <CircleCheck className="h-3 w-3" aria-label={d("recipeMatched", { metric: r.matched_metric })} />
                  </Badge>
                </li>
              ))}
            </ul>
          )}
        </div>
      )}

      <div className="relative">
        <Search className="pointer-events-none absolute left-2.5 top-1/2 h-3.5 w-3.5 -translate-y-1/2 text-muted-foreground" aria-hidden />
        <Input
          value={search}
          onChange={(e) => setSearch(e.target.value)}
          placeholder={d("search")}
          aria-label={d("search")}
          className="pl-8 font-mono text-caption"
          spellCheck={false}
          autoComplete="off"
        />
      </div>

      {failure ? (
        <Notice variant={failure.message ? "error" : "info"} title={failure.title}>
          {failure.message && <p className="break-words font-mono text-caption">{failure.message}</p>}
        </Notice>
      ) : loading && !result ? (
        <div className="space-y-1.5">
          <Skeleton className="h-6 w-2/3" />
          <Skeleton className="h-6 w-1/2" />
          <Skeleton className="h-6 w-3/5" />
        </div>
      ) : metrics.length === 0 ? (
        <p className="text-caption text-muted-foreground">{d("none")}</p>
      ) : (
        <div className="space-y-1.5" aria-busy={loading || undefined}>
          <p className="text-caption text-muted-foreground">
            {result?.truncated ? d("truncated", { count: metrics.length }) : d("count", { count: metrics.length })}
          </p>
          <ul className="max-h-64 divide-y divide-border/50 overflow-y-auto rounded-md border border-border" aria-label={d("title")}>
            {metrics.map((m) => (
              <li key={m}>
                {onPickMetric && canPick(m) ? (
                  <Button
                    variant="ghost"
                    size="sm"
                    className="h-auto w-full justify-start rounded-none px-3 py-1.5 font-mono text-caption font-normal"
                    onClick={() => onPickMetric(m)}
                    title={d("useMetric", { metric: m })}
                  >
                    <span className="truncate">{m}</span>
                  </Button>
                ) : (
                  <p className="truncate px-3 py-1.5 font-mono text-caption" title={onPickMetric ? d("patternName", { metric: m }) : undefined}>
                    {m}
                    {onPickMetric && <span className="sr-only">{` — ${d("patternName", { metric: m })}`}</span>}
                  </p>
                )}
              </li>
            ))}
          </ul>
        </div>
      )}
    </section>
  );
}
