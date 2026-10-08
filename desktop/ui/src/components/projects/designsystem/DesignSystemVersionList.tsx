import { CheckCircle2, Eye } from "lucide-react";
import type { DesignSystemVersion } from "@/api";
import { DesignSystemStatusBadge } from "@/components/projects/designsystem/DesignSystemStatusBadge";
import { DesignTaskLink } from "@/components/projects/designsystem/DesignTaskLink";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { useI18n } from "@/hooks/useI18n";
import { formatRelativeTime } from "@/lib/utils";

interface DesignSystemVersionListProps {
  variant: "pending" | "history";
  versions: DesignSystemVersion[];
  /** The version the panel above shows; its row reads "Shown" instead of "View". */
  selectedId?: string;
  onSelect?: (version: DesignSystemVersion) => void;
  /** Labels repository-scoped rows (a project's design task may propose layers too). */
  repositoryNames?: Record<string, string>;
}

/** Pending proposals (each with "Review and approve") or the version history. */
export function DesignSystemVersionList({
  variant,
  versions,
  selectedId,
  onSelect,
  repositoryNames = {},
}: DesignSystemVersionListProps) {
  const { t, lang } = useI18n();
  if (versions.length === 0) return null;
  const pending = variant === "pending";

  return (
    <Card className="p-0">
      <CardHeader className="pb-3">
        <CardTitle className="flex items-center gap-2 text-heading">
          {pending ? t("designSystem.pending.title") : t("designSystem.history.title")}
          <Badge variant={pending ? "warning" : "secondary"}>{versions.length}</Badge>
        </CardTitle>
        {pending && <CardDescription>{t("designSystem.pending.description")}</CardDescription>}
      </CardHeader>
      <ul className="divide-y divide-border border-t border-border">
        {versions.map((version) => {
          const shown = version.id === selectedId;
          const repositoryName = version.repository_id ? repositoryNames[version.repository_id] : undefined;
          return (
            <li key={version.id} className="flex flex-wrap items-center gap-3 px-4 py-3" data-testid="design-system-version-row">
              <div className="flex min-w-0 flex-1 flex-wrap items-center gap-2">
                <Badge variant="outline" className="font-mono">
                  {t("designSystem.version", { version: version.version })}
                </Badge>
                <DesignSystemStatusBadge status={version.status} />
                {version.scope === "repository" && (
                  <Badge variant="secondary">
                    {repositoryName
                      ? `${t("designSystem.scope.repository")} · ${repositoryName}`
                      : t("designSystem.scope.repository")}
                  </Badge>
                )}
                {version.source_task_id && (
                  <DesignTaskLink
                    taskId={version.source_task_id}
                    taskKey={version.source_task_key}
                    repositoryId={version.source_task_repository_id}
                    className="font-mono text-caption text-muted-foreground hover:text-foreground hover:underline"
                  />
                )}
                <span className="text-caption text-muted-foreground">
                  {version.approved_at
                    ? t("designSystem.panel.approvedAt", { date: formatRelativeTime(version.approved_at, lang) })
                    : t("designSystem.panel.createdAt", { date: formatRelativeTime(version.created_at, lang) })}
                </span>
              </div>
              <div className="flex shrink-0 items-center gap-2">
                {onSelect &&
                  (shown ? (
                    <Badge variant="outline" className="gap-1">
                      <CheckCircle2 className="h-3 w-3" aria-hidden />
                      {t("designSystem.history.shown")}
                    </Badge>
                  ) : (
                    <Button size="sm" variant="ghost" onClick={() => onSelect(version)}>
                      <Eye />
                      {pending ? t("designSystem.pending.preview") : t("designSystem.history.view")}
                    </Button>
                  ))}
                {pending && version.source_task_id && (
                  <Button size="sm" asChild>
                    <DesignTaskLink
                      taskId={version.source_task_id}
                      taskKey={version.source_task_key}
                      repositoryId={version.source_task_repository_id}
                    >
                      {t("designSystem.generate.reviewAndApprove")}
                    </DesignTaskLink>
                  </Button>
                )}
              </div>
            </li>
          );
        })}
      </ul>
    </Card>
  );
}
