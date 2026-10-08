import { AlertTriangle } from "lucide-react";
import { useEffect, useMemo, useState } from "react";
import { toast } from "sonner";
import type { DesignSystemVersion } from "@/api";
import { DesignSystemGenerateCard } from "@/components/projects/designsystem/DesignSystemGenerateCard";
import { DesignSystemRepositoriesTable } from "@/components/projects/designsystem/DesignSystemRepositoriesTable";
import { DesignSystemVersionList } from "@/components/projects/designsystem/DesignSystemVersionList";
import { DesignSystemVersionPanel } from "@/components/projects/designsystem/DesignSystemVersionPanel";
import { Button } from "@/components/ui/button";
import { Card } from "@/components/ui/card";
import { EmptyState } from "@/components/ui/empty-state";
import { Notice } from "@/components/ui/notice";
import { Skeleton } from "@/components/ui/skeleton";
import { useProjectDesignSystem } from "@/hooks/useDesignSystem";
import { useI18n } from "@/hooks/useI18n";

interface ProjectDesignSystemTabProps {
  projectId: string;
}

/**
 * The project page's Design System tab: the approved base (or the call to
 * create one), proposals waiting for review, the version history, and how
 * each repository layers on top. Loaded only once the tab mounts.
 */
export function ProjectDesignSystemTab({ projectId }: ProjectDesignSystemTabProps) {
  const { t } = useI18n();
  const { view, loading, error, generate } = useProjectDesignSystem(projectId);
  const [selectedId, setSelectedId] = useState<string | null>(null);

  useEffect(() => {
    if (error && view) toast.error(error);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [error]);

  const byId = useMemo(() => {
    const map = new Map<string, DesignSystemVersion>();
    for (const version of [...(view?.versions ?? []), ...(view?.pending ?? [])]) map.set(version.id, version);
    return map;
  }, [view]);

  if (loading && !view) {
    return (
      <div className="space-y-4">
        <Skeleton className="h-32 w-full rounded-xl" />
        <Skeleton className="h-96 w-full rounded-xl" />
      </div>
    );
  }

  if (!view) {
    return (
      <Card className="p-0">
        <EmptyState icon={AlertTriangle} variant="critical" title={t("designSystem.loadFailed")} description={error ?? undefined} />
      </Card>
    );
  }

  const current = view.current;
  const shown = (selectedId ? byId.get(selectedId) : undefined) ?? current ?? view.pending[0];
  const viewingOlder = !!current && !!shown && shown.id !== current.id;
  const history = view.versions.filter((v) => v.status !== "in_review");
  const repositoryNames = Object.fromEntries(view.repositories.map((r) => [r.id, r.name]));

  const generateCard = (
    <DesignSystemGenerateCard
      scope="project"
      mode={current ? "update" : "create"}
      request={view.request}
      onGenerate={generate}
      projectId={projectId}
    />
  );

  return (
    <div className="space-y-4">
      {!current && generateCard}

      <DesignSystemVersionList
        variant="pending"
        versions={view.pending}
        selectedId={shown?.id}
        onSelect={(v) => setSelectedId(v.id)}
        repositoryNames={repositoryNames}
      />

      {shown && (
        <DesignSystemVersionPanel
          version={shown}
          title={
            shown.scope === "repository"
              ? `${t("designSystem.panel.layerTitle")}${shown.repository_id && repositoryNames[shown.repository_id] ? ` · ${repositoryNames[shown.repository_id]}` : ""}`
              : t("designSystem.panel.baseTitle")
          }
          actions={
            viewingOlder ? (
              <Button size="sm" variant="ghost" onClick={() => setSelectedId(null)}>
                {t("designSystem.panel.showCurrent")}
              </Button>
            ) : undefined
          }
        >
          {viewingOlder && (
            <Notice variant="info" title={t("designSystem.panel.viewingOlder", { version: shown.version })} />
          )}
        </DesignSystemVersionPanel>
      )}

      {current && generateCard}

      <DesignSystemVersionList
        variant="history"
        versions={history}
        selectedId={shown?.id}
        onSelect={(v) => setSelectedId(v.id)}
        repositoryNames={repositoryNames}
      />

      <DesignSystemRepositoriesTable projectId={projectId} repositories={view.repositories} hasBase={!!current} />
    </div>
  );
}
