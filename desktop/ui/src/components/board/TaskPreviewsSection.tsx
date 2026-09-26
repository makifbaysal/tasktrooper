import { ExternalLink, Eye, Lock, RefreshCw } from "lucide-react";
import { useState } from "react";
import { toast } from "sonner";
import { type TaskPreviewStatus } from "@/api";
import { type BadgeProps, Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Label } from "@/components/ui/label";
import { useI18n } from "@/hooks/useI18n";
import { useTaskPreviews } from "@/hooks/useTaskPreviews";
import { cn } from "@/lib/utils";

const STATUS_VARIANT: Record<TaskPreviewStatus, NonNullable<BadgeProps["variant"]>> = {
  queued: "info",
  building: "info",
  ready: "success",
  error: "destructive",
  canceled: "secondary",
  none: "outline",
};

interface TaskPreviewsSectionProps {
  repositoryId: string;
  taskId: string;
}

/** The per-branch preview deployments built for this task's PR, one per
 * component with a per-branch environment. Renders nothing when no component
 * has one, and polls only while a preview is still being built. */
export function TaskPreviewsSection({ repositoryId, taskId }: TaskPreviewsSectionProps) {
  const { t } = useI18n();
  const { previews, reload } = useTaskPreviews(repositoryId, taskId);
  const [refreshing, setRefreshing] = useState(false);

  const refresh = async () => {
    setRefreshing(true);
    try {
      await reload();
    } catch (e) {
      toast.error(e instanceof Error ? e.message : t("cloud.preview.task.loadFailed"));
    } finally {
      setRefreshing(false);
    }
  };

  if (!previews || previews.length === 0) return null;

  return (
    <section className="space-y-2">
      <div className="flex items-center justify-between gap-2">
        <Label className="flex items-center gap-2 text-xs text-muted-foreground">
          <Eye className="h-3.5 w-3.5" />
          {t("cloud.preview.task.label")}
        </Label>
        <Button
          size="icon"
          variant="ghost"
          className="h-6 w-6"
          onClick={() => void refresh()}
          disabled={refreshing}
          aria-label={t("cloud.preview.task.refresh")}
          title={t("cloud.preview.task.refresh")}
        >
          <RefreshCw className={cn("h-3.5 w-3.5", refreshing && "animate-spin")} />
        </Button>
      </div>
      <ul className="space-y-1.5">
        {previews.map((p) => {
          const href = p.branch_url || p.url;
          return (
            <li key={p.component_id} data-testid={`task-preview-${p.component_id}`} className="space-y-1">
              <div className="flex items-center gap-2">
                <span className="min-w-0 flex-1 truncate text-sm">{p.component_name}</span>
                <Badge variant={STATUS_VARIANT[p.status]}>{t(`cloud.preview.task.status.${p.status}`)}</Badge>
              </div>
              <div className="flex flex-wrap items-center gap-2 text-xs text-muted-foreground">
                {href && p.status !== "none" && (
                  <a
                    href={href}
                    target="_blank"
                    rel="noreferrer"
                    className="inline-flex items-center gap-1 text-primary hover:underline"
                  >
                    <ExternalLink className="h-3 w-3" />
                    {t("cloud.preview.task.open")}
                  </a>
                )}
                {p.pr_number > 0 && <span>{t("cloud.preview.prNumber", { number: p.pr_number })}</span>}
                {p.protected && (
                  <Badge
                    variant={p.bypass_configured ? "secondary" : "warning"}
                    className="gap-1 px-1.5 py-0 text-micro"
                    title={p.bypass_configured ? undefined : t("cloud.preview.bypass.tooltip")}
                  >
                    <Lock className="h-3 w-3" aria-hidden />
                    {t("cloud.preview.access.protectedShort")}
                  </Badge>
                )}
              </div>
            </li>
          );
        })}
      </ul>
    </section>
  );
}
