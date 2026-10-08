import { AlertTriangle, ArrowUpRight } from "lucide-react";
import { useEffect, useMemo, useState } from "react";
import { Link } from "react-router-dom";
import { toast } from "sonner";
import type { DesignSystemVersion } from "@/api";
import { DesignBaseProjectPicker } from "@/components/projects/designsystem/DesignBaseProjectPicker";
import { DesignLintChecks } from "@/components/projects/designsystem/DesignLintChecks";
import { DesignSystemFilesCard } from "@/components/projects/designsystem/DesignSystemFilesCard";
import { DesignSystemGenerateCard } from "@/components/projects/designsystem/DesignSystemGenerateCard";
import { DesignSystemVersionList } from "@/components/projects/designsystem/DesignSystemVersionList";
import { DesignSystemVersionPanel } from "@/components/projects/designsystem/DesignSystemVersionPanel";
import { DesignTokensPreview } from "@/components/projects/designsystem/DesignTokensPreview";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { EmptyState } from "@/components/ui/empty-state";
import { Notice } from "@/components/ui/notice";
import { Skeleton } from "@/components/ui/skeleton";
import { useRepositoryDesignSystem } from "@/hooks/useDesignSystem";
import { useI18n } from "@/hooks/useI18n";
import { projectDesignSystemPath } from "@/lib/design-system";

interface RepositoryDesignSystemTabProps {
  repositoryId: string;
}

/**
 * The repository page's Design System tab: which project base it builds on,
 * the merged tokens it ends up with, its own layer (overrides + rationale),
 * proposals in review, history, and the call to create or update the layer.
 */
export function RepositoryDesignSystemTab({ repositoryId }: RepositoryDesignSystemTabProps) {
  const { t } = useI18n();
  const { view, loading, error, generate, setBaseProject } = useRepositoryDesignSystem(repositoryId);
  const [selectedId, setSelectedId] = useState<string | null>(null);

  useEffect(() => {
    if (error && view) toast.error(error);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [error]);

  const byId = useMemo(() => {
    const map = new Map<string, DesignSystemVersion>();
    for (const version of [...(view?.layer_versions ?? []), ...(view?.pending_layers ?? [])]) map.set(version.id, version);
    return map;
  }, [view]);

  if (loading && !view) {
    return (
      <div className="space-y-4">
        <Skeleton className="h-20 w-full rounded-xl" />
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

  const { effective } = view;
  const base = effective.base;
  const layer = effective.layer;
  const hasDesignSystem = !!base || !!layer;
  const showPicker = view.project_choices.length > 1 || !!view.base_project_id;
  const shownLayer = (selectedId ? byId.get(selectedId) : undefined) ?? layer ?? view.pending_layers[0];
  const viewingOlder = !!layer && !!shownLayer && shownLayer.id !== layer.id;
  const history = view.layer_versions.filter((v) => v.status !== "in_review");

  return (
    <div className="space-y-4">
      {showPicker && (
        <DesignBaseProjectPicker choices={view.project_choices} value={view.base_project_id} onChange={setBaseProject} />
      )}

      {effective.ambiguous && (
        <Notice variant="warning" title={t("designSystem.repository.ambiguousTitle")}>
          {t("designSystem.repository.ambiguousBody")}
        </Notice>
      )}

      {!hasDesignSystem && (
        <DesignSystemGenerateCard scope="repository" mode="create" request={view.request} onGenerate={generate} />
      )}

      <DesignSystemVersionList
        variant="pending"
        versions={view.pending_layers}
        selectedId={shownLayer?.id}
        onSelect={(v) => setSelectedId(v.id)}
      />

      {hasDesignSystem && (
        <Card>
          <CardHeader className="pb-3">
            <CardTitle className="text-heading">{t("designSystem.repository.baseTitle")}</CardTitle>
            <CardDescription>
              {base && effective.project
                ? t("designSystem.repository.buildsOn", { project: effective.project.name, version: base.version })
                : t("designSystem.repository.noBase")}
            </CardDescription>
          </CardHeader>
          {base && effective.project && (
            <CardContent>
              <Button size="sm" variant="outline" asChild>
                <Link to={projectDesignSystemPath(effective.project.id)}>
                  <ArrowUpRight />
                  {t("designSystem.repository.openProject")}
                </Link>
              </Button>
            </CardContent>
          )}
        </Card>
      )}

      {hasDesignSystem && (
        <Card>
          <CardHeader className="pb-3">
            <CardTitle className="text-heading">{t("designSystem.repository.effectiveTitle")}</CardTitle>
            <CardDescription>{t("designSystem.repository.effectiveDescription")}</CardDescription>
          </CardHeader>
          <CardContent className="space-y-4">
            <DesignLintChecks findings={view.lint} hint={t("designSystem.checks.mergedHint")} />
            <DesignTokensPreview tokens={effective.tokens} overriddenPaths={view.overrides} />
          </CardContent>
        </Card>
      )}

      {hasDesignSystem && <DesignSystemFilesCard repositoryId={repositoryId} />}

      {shownLayer ? (
        <DesignSystemVersionPanel
          version={shownLayer}
          title={t("designSystem.panel.layerTitle")}
          overriddenPaths={shownLayer.id === layer?.id ? view.overrides : undefined}
          actions={
            viewingOlder ? (
              <Button size="sm" variant="ghost" onClick={() => setSelectedId(null)}>
                {t("designSystem.panel.showCurrent")}
              </Button>
            ) : undefined
          }
        >
          {viewingOlder && (
            <Notice variant="info" title={t("designSystem.panel.viewingOlder", { version: shownLayer.version })} />
          )}
          {!viewingOlder && shownLayer.id === layer?.id && base && <LayerOverrides overrides={view.overrides} />}
        </DesignSystemVersionPanel>
      ) : (
        hasDesignSystem && <p className="text-body text-muted-foreground">{t("designSystem.repository.noLayer")}</p>
      )}

      {hasDesignSystem && (
        <DesignSystemGenerateCard
          scope="repository"
          mode="update"
          request={view.request}
          onGenerate={generate}
          label={layer ? undefined : t("designSystem.generate.createRepository")}
          description={layer ? undefined : t("designSystem.generate.explainRepository")}
        />
      )}

      <DesignSystemVersionList
        variant="history"
        versions={history}
        selectedId={shownLayer?.id}
        onSelect={(v) => setSelectedId(v.id)}
      />
    </div>
  );
}

function LayerOverrides({ overrides }: { overrides: string[] }) {
  const { t } = useI18n();
  return (
    <div className="space-y-1.5">
      <p className="text-caption font-medium text-muted-foreground">
        {t("designSystem.repository.overridesTitle", { count: overrides.length })}
      </p>
      {overrides.length > 0 ? (
        <div className="flex flex-wrap gap-1.5">
          {overrides.map((path) => (
            <Badge key={path} variant="outline" className="font-mono">
              {path}
            </Badge>
          ))}
        </div>
      ) : (
        <p className="text-caption text-muted-foreground">{t("designSystem.repository.noOverrides")}</p>
      )}
    </div>
  );
}
