import { ExternalLink, Loader2, Lock } from "lucide-react";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { useI18n } from "@/hooks/useI18n";
import { ACTIVE_PREVIEW_STATUSES, useTaskPreviews } from "@/hooks/useTaskPreviews";

interface TaskPreviewActionsProps {
  repositoryId: string;
  taskId: string;
}

/**
 * The "try it on the preview" buttons that sit next to "Run locally" in the
 * human UAT box: one per component whose per-branch preview (Vercel) exists for
 * this task's branch. A protected preview needs no bypass here — the reviewer's
 * own browser carries their Vercel login; the bypass secret is only for agents.
 */
export function TaskPreviewActions({ repositoryId, taskId }: TaskPreviewActionsProps) {
  const { t } = useI18n();
  const { previews } = useTaskPreviews(repositoryId, taskId);

  const shown = (previews ?? []).filter((p) => p.status !== "none" && p.status !== "canceled");
  if (shown.length === 0) return null;
  const named = shown.length > 1;

  return (
    <>
      {shown.map((p) => {
        const label = named
          ? t("cloud.preview.task.openInPreviewNamed", { name: p.component_name })
          : t("cloud.preview.task.openInPreview");
        if (ACTIVE_PREVIEW_STATUSES.includes(p.status)) {
          return (
            <Button key={p.component_id} size="sm" variant="outline" disabled>
              <Loader2 className="mr-1.5 h-3.5 w-3.5 animate-spin" />
              {named ? `${p.component_name} · ${t("cloud.preview.task.building")}` : t("cloud.preview.task.building")}
            </Button>
          );
        }
        if (p.status === "error") {
          return (
            <Badge key={p.component_id} variant="destructive">
              {named ? `${p.component_name} · ${t("cloud.preview.task.failed")}` : t("cloud.preview.task.failed")}
            </Badge>
          );
        }
        const href = p.branch_url || p.url;
        if (!href) return null;
        return (
          <Button key={p.component_id} size="sm" asChild>
            <a
              href={href}
              target="_blank"
              rel="noreferrer"
              title={p.protected ? t("cloud.preview.task.protectedHint") : undefined}
            >
              <ExternalLink className="mr-1.5 h-3.5 w-3.5" />
              {label}
              {p.protected && <Lock className="ml-1.5 h-3 w-3" aria-hidden />}
            </a>
          </Button>
        );
      })}
    </>
  );
}
