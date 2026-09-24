import { useEffect } from "react";
import type { RepositoryModel, ResourceKind } from "@/api";
import { Accordion, AccordionContent, AccordionItem, AccordionTrigger } from "@/components/ui/accordion";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Notice } from "@/components/ui/notice";
import { Skeleton } from "@/components/ui/skeleton";
import { LinkTargetLabel } from "@/components/projects/model/LinkTargetLabel";
import { ReviewList } from "@/components/projects/model/ReviewList";
import { RepoShapeBadge } from "@/components/projects/model/RepoShapeBadge";
import { ResourceKindIcon } from "@/components/projects/model/ResourceKindIcon";
import { RoleBadge } from "@/components/projects/model/RoleBadge";
import { useI18n } from "@/hooks/useI18n";
import { useRepositoryModel } from "@/hooks/useRepositoryModel";
import { componentLabel, effectiveRole, factValue, requiredChecks, stackSummary } from "@/lib/project-model";

interface RepoReviewCardProps {
  repositoryId: string;
  onModelChange: (repositoryId: string, model: RepositoryModel) => void;
}

/** One repository's Review-step card: the shared ReviewList for anything
 * medium-confidence, and a collapsible summary of everything the scan saved
 * directly. Reports its model up to ReviewStep on every load/reload, which
 * is how the step knows the aggregate review count and the Done stats. */
export function RepoReviewCard({ repositoryId, onModelChange }: RepoReviewCardProps) {
  const { t } = useI18n();
  const { model, error, reload } = useRepositoryModel(repositoryId);

  useEffect(() => {
    if (model) onModelChange(repositoryId, model);
  }, [model, repositoryId, onModelChange]);

  if (error && !model) {
    return (
      <Notice variant="error" title={t("addRepository.review.loadFailed")}>
        <p>{error}</p>
        <Button variant="outline" size="sm" className="mt-2" onClick={reload}>
          {t("common.refresh")}
        </Button>
      </Notice>
    );
  }

  if (!model) {
    return (
      <Card>
        <CardContent className="space-y-2 py-6">
          <Skeleton className="h-5 w-1/3" />
          <Skeleton className="h-4 w-2/3" />
        </CardContent>
      </Card>
    );
  }

  const activeComponents = model.components.filter((c) => c.status === "active");
  const resourceLinks = model.links.filter((l) => l.to_resource_id);
  const resourcesByKind = new Map<ResourceKind, string[]>();
  for (const link of resourceLinks) {
    const resource = model.resources.find((r) => r.id === link.to_resource_id);
    if (!resource) continue;
    const names = resourcesByKind.get(resource.kind) ?? [];
    if (!names.includes(resource.name)) names.push(resource.name);
    resourcesByKind.set(resource.kind, names);
  }
  const componentLinks = model.links.filter((l) => l.to_component_id);

  return (
    <Card>
      <CardHeader className="flex flex-row items-center justify-between gap-3 space-y-0">
        <div className="flex items-center gap-2">
          <CardTitle>{model.repository.name}</CardTitle>
          <RepoShapeBadge shape={model.shape} />
        </div>
        <span className="text-caption text-muted-foreground">
          {t("addRepository.review.componentsCount", { count: activeComponents.length })}
        </span>
      </CardHeader>
      <CardContent className="space-y-4">
        {model.review.length > 0 && (
          <div className="space-y-2">
            <h3 className="text-body font-medium">{t("addRepository.review.needsAnswer")}</h3>
            <ReviewList model={model} onChanged={reload} compact />
          </div>
        )}

        <Accordion type="single" collapsible>
          <AccordionItem value="saved">
            <AccordionTrigger>{t("addRepository.review.savedAutomatically")}</AccordionTrigger>
            <AccordionContent className="space-y-3">
              {activeComponents.length > 0 && (
                <div className="space-y-1.5">
                  <p className="text-caption font-medium text-foreground">{t("addRepository.review.componentsLabel")}</p>
                  <ul className="space-y-1">
                    {activeComponents.map((c) => {
                      const role = effectiveRole(c);
                      return (
                        <li key={c.id} className="flex flex-wrap items-center gap-2">
                          {role && <RoleBadge role={role} />}
                          <span className="truncate font-mono text-caption">{c.path === "." ? model.repository.name : c.path}</span>
                          <span className="truncate text-caption text-muted-foreground">{stackSummary(factValue(c.stack), 3)}</span>
                        </li>
                      );
                    })}
                  </ul>
                </div>
              )}

              <div className="flex items-center justify-between text-body">
                <span className="text-muted-foreground">{t("addRepository.review.checksLabel")}</span>
                <span>{t("addRepository.review.checksValue", { total: model.checks.length, required: requiredChecks(model.checks) })}</span>
              </div>

              {resourcesByKind.size > 0 && (
                <div className="space-y-1.5">
                  <p className="text-caption font-medium text-foreground">{t("addRepository.review.resourcesLabel")}</p>
                  <ul className="space-y-1">
                    {[...resourcesByKind.entries()].map(([kind, names]) => (
                      <li key={kind} className="flex items-center gap-2 text-body">
                        <ResourceKindIcon kind={kind} className="text-muted-foreground" />
                        <span>{t(`projectModel.resourceKinds.${kind}`)}</span>
                        <span className="truncate text-caption text-muted-foreground">{names.join(", ")}</span>
                      </li>
                    ))}
                  </ul>
                </div>
              )}

              {componentLinks.length > 0 && (
                <div className="space-y-1.5">
                  <p className="text-caption font-medium text-foreground">{t("addRepository.review.componentLinksLabel")}</p>
                  <ul className="space-y-1">
                    {componentLinks.map((link) => {
                      const from = model.components.find((c) => c.id === link.from_component_id);
                      return (
                        <li key={link.id} className="flex items-center gap-1 text-body">
                          <span className="truncate">{from ? componentLabel(from, model.repository.name) : model.repository.name}</span>
                          <span aria-hidden>→</span>
                          <LinkTargetLabel link={link} model={model} />
                        </li>
                      );
                    })}
                  </ul>
                </div>
              )}

              <p className="text-micro text-muted-foreground">{t("addRepository.review.savedHint")}</p>
            </AccordionContent>
          </AccordionItem>
        </Accordion>
      </CardContent>
    </Card>
  );
}
