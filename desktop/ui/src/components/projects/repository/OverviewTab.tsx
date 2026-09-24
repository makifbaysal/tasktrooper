import { Box } from "lucide-react";
import type { RepositoryModel } from "@/api";
import { EvidenceList } from "@/components/projects/model/EvidenceList";
import { RoleBadge } from "@/components/projects/model/RoleBadge";
import { ReviewList } from "@/components/projects/model/ReviewList";
import { Badge } from "@/components/ui/badge";
import { Card } from "@/components/ui/card";
import { MarkdownContent } from "@/components/markdown/MarkdownContent";
import { useI18n } from "@/hooks/useI18n";
import { componentLabel, factValue, requiredChecks, reviewCount, stackSummary } from "@/lib/project-model";
import { cn } from "@/lib/utils";

interface OverviewTabProps {
  model: RepositoryModel;
  onReload: () => void;
  onOpenComponent: (componentId: string) => void;
}

function StatTile({ label, value, warn }: { label: string; value: number; warn?: boolean }) {
  return (
    <Card className="p-4">
      <p className="text-caption text-muted-foreground">{label}</p>
      <p className={cn("text-display font-semibold", warn && value > 0 && "text-warning")}>{value}</p>
    </Card>
  );
}

export function OverviewTab({ model, onReload, onOpenComponent }: OverviewTabProps) {
  const { t } = useI18n();
  const activeComponents = model.components.filter((c) => c.status === "active");
  const confirmedLinks = model.links.filter((l) => l.status === "confirmed").length;
  const purposeNote = model.notes.find((n) => n.topic === "purpose" && !n.component_id);

  return (
    <div className="space-y-6">
      <div className="grid gap-4 sm:grid-cols-2 lg:grid-cols-4">
        <StatTile label={t("repositoryPage.overview.statComponents")} value={activeComponents.length} />
        <StatTile label={t("repositoryPage.overview.statRequiredChecks")} value={requiredChecks(model.checks)} />
        <StatTile label={t("repositoryPage.overview.statConfirmedLinks")} value={confirmedLinks} />
        <StatTile label={t("repositoryPage.overview.statReview")} value={reviewCount(model.review)} warn />
      </div>

      {model.review.length > 0 && (
        <Card className="p-6">
          <h2 className="mb-3 font-semibold">{t("repositoryPage.overview.needsReview")}</h2>
          <ReviewList model={model} onChanged={onReload} />
        </Card>
      )}

      {purposeNote && (
        <Card className="p-6">
          <div className="mb-3 flex items-center justify-between gap-2">
            <h2 className="font-semibold">{t("repositoryPage.overview.purposeTitle")}</h2>
            <Badge variant={purposeNote.author === "agent" ? "info" : "secondary"}>
              {t(purposeNote.author === "agent" ? "repositoryPage.knowledge.authorAgent" : "repositoryPage.knowledge.authorYou")}
            </Badge>
          </div>
          <MarkdownContent content={purposeNote.body_md} />
          {(purposeNote.evidence?.length ?? 0) > 0 && <EvidenceList evidence={purposeNote.evidence ?? []} className="mt-3" />}
        </Card>
      )}

      <div>
        <h2 className="mb-3 font-semibold">{t("repositoryPage.overview.componentsTitle")}</h2>
        <div className="grid gap-3 sm:grid-cols-2 lg:grid-cols-3 xl:grid-cols-4">
          {activeComponents.map((component) => {
            const role = factValue(component.role);
            const stack = stackSummary(factValue(component.stack), 3);
            const checksCount = model.checks.filter((c) => c.component_id === component.id && c.status === "active").length;
            return (
              <button
                key={component.id}
                type="button"
                onClick={() => onOpenComponent(component.id)}
                className="flex flex-col items-start gap-2 rounded-xl border border-border bg-card p-4 text-left shadow-[var(--shadow-raised)] transition-colors hover:bg-muted/40"
              >
                <div className="flex w-full items-center justify-between gap-2">
                  {role ? <RoleBadge role={role} /> : <Box className="h-4 w-4 text-muted-foreground" aria-hidden />}
                </div>
                <span className="truncate font-mono text-caption">{componentLabel(component, model.repository.name)}</span>
                {stack && <span className="line-clamp-2 text-micro text-muted-foreground">{stack}</span>}
                <span className="text-micro text-muted-foreground">
                  {t("repositoryPage.overview.checksCount", { count: checksCount })}
                </span>
              </button>
            );
          })}
        </div>
      </div>
    </div>
  );
}
