import { ExternalLink, ShieldAlert } from "lucide-react";
import { useEffect, useState } from "react";
import { useNavigate } from "react-router-dom";
import { toast } from "sonner";
import { api, type RuntimeErrorGroup } from "@/api";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { EmptyState } from "@/components/ui/empty-state";
import { Skeleton } from "@/components/ui/skeleton";
import { useI18n } from "@/hooks/useI18n";
import { cn, formatRelativeDate } from "@/lib/utils";

type Range = "1h" | "24h" | "7d";
const RANGES: Range[] = ["1h", "24h", "7d"];
const RANGE_MS: Record<Range, number> = { "1h": 3_600_000, "24h": 86_400_000, "7d": 7 * 86_400_000 };

interface ErrorsPanelProps {
  envId: string;
  className?: string;
}

/** Recurring error groups for one environment, over a 1h/24h/7d window. */
export function ErrorsPanel({ envId, className }: ErrorsPanelProps) {
  const { t } = useI18n();
  const navigate = useNavigate();
  const [range, setRange] = useState<Range>("24h");
  const [errors, setErrors] = useState<RuntimeErrorGroup[] | null>(null);
  const [loading, setLoading] = useState(true);
  const [expanded, setExpanded] = useState<string | null>(null);
  const [creating, setCreating] = useState<string | null>(null);

  useEffect(() => {
    let cancelled = false;
    setLoading(true);
    api
      .getEnvironmentErrors(envId, { since: new Date(Date.now() - RANGE_MS[range]).toISOString() })
      .then((res) => {
        if (!cancelled) setErrors(res.errors);
      })
      .catch((e) => {
        if (cancelled) return;
        toast.error(e instanceof Error ? e.message : t("repositoryPage.deploy.runtime.errors.loadFailed"));
      })
      .finally(() => {
        if (!cancelled) setLoading(false);
      });
    return () => {
      cancelled = true;
    };
  }, [envId, range, t]);

  const createTask = async (group: RuntimeErrorGroup) => {
    setCreating(group.fingerprint);
    try {
      const task = await api.createErrorTask(envId, group);
      toast.success(t("repositoryPage.deploy.runtime.errors.taskCreated"), {
        action: { label: task.key, onClick: () => navigate(`/board?task=${task.id}`) },
      });
    } catch (e) {
      toast.error(e instanceof Error ? e.message : t("common.actionFailed"));
    } finally {
      setCreating(null);
    }
  };

  return (
    <div className={cn("space-y-3", className)}>
      <div className="flex items-center gap-2">
        {RANGES.map((r) => (
          <Button key={r} size="sm" variant={range === r ? "default" : "outline"} onClick={() => setRange(r)}>
            {t(`repositoryPage.deploy.runtime.errors.range.${r}`)}
          </Button>
        ))}
      </div>

      {loading ? (
        <Skeleton className="h-40 w-full" />
      ) : !errors || errors.length === 0 ? (
        <EmptyState icon={ShieldAlert} title={t("repositoryPage.deploy.runtime.errors.empty")} />
      ) : (
        <div className="divide-y divide-border rounded-lg border border-border">
          {errors.map((group) => {
            const isExpanded = expanded === group.fingerprint;
            return (
              <div key={group.fingerprint} className="flex flex-col gap-1.5 px-4 py-3">
                <div className="flex flex-wrap items-start gap-3">
                  <button
                    type="button"
                    onClick={() => setExpanded(isExpanded ? null : group.fingerprint)}
                    className={cn(
                      "min-w-0 flex-1 text-left font-mono text-caption",
                      isExpanded ? "whitespace-pre-wrap break-all" : "truncate",
                    )}
                  >
                    {group.message}
                  </button>
                  {group.new && <Badge variant="destructive">{t("repositoryPage.deploy.runtime.errors.new")}</Badge>}
                  <Badge variant="secondary" className="shrink-0">
                    {group.count}
                  </Badge>
                </div>
                {isExpanded && group.sample && (
                  <pre className="whitespace-pre-wrap break-all rounded-md bg-muted px-3 py-2 font-mono text-micro">
                    {group.sample}
                  </pre>
                )}
                <div className="flex flex-wrap items-center gap-3 text-micro text-muted-foreground">
                  {group.source && <span className="font-mono">{group.source}</span>}
                  <span>{t("repositoryPage.deploy.runtime.errors.firstSeen", { time: formatRelativeDate(group.first_seen) })}</span>
                  <span>{t("repositoryPage.deploy.runtime.errors.lastSeen", { time: formatRelativeDate(group.last_seen) })}</span>
                  {group.external_url && (
                    <a
                      href={group.external_url}
                      target="_blank"
                      rel="noreferrer"
                      className="inline-flex items-center gap-1 text-info hover:underline"
                    >
                      <ExternalLink className="h-3 w-3" />
                    </a>
                  )}
                  <Button
                    size="sm"
                    variant="outline"
                    className="ml-auto"
                    disabled={creating === group.fingerprint}
                    onClick={() => void createTask(group)}
                  >
                    {t("repositoryPage.deploy.runtime.errors.createTask")}
                  </Button>
                </div>
              </div>
            );
          })}
        </div>
      )}
    </div>
  );
}
