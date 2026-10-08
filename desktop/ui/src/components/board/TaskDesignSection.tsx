import { AlertTriangle, FileCode2, Palette } from "lucide-react";
import { useState } from "react";
import { Link } from "react-router-dom";
import type { AttachmentMeta, TaskDesignReference, TaskDesignSystemSummary, TaskDocument } from "@/api";
import { AttachmentList } from "@/components/attachments/AttachmentList";
import { DesignDocumentDialog } from "@/components/board/DesignDocumentDialog";
import { MarkdownContent } from "@/components/markdown/MarkdownContent";
import { Accordion, AccordionContent, AccordionItem, AccordionTrigger } from "@/components/ui/accordion";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardHeader } from "@/components/ui/card";
import { Label } from "@/components/ui/label";
import { Separator } from "@/components/ui/separator";
import { useI18n } from "@/hooks/useI18n";
import { useTaskDesign } from "@/hooks/useTaskDesign";
import { analysisReviewPath } from "@/lib/analysis-review";
import {
  designDocumentKind,
  designDocumentLabel,
  designHtmlDocuments,
  designMarkdownDocuments,
  repositoryDesignSystemPath,
} from "@/lib/design-system";

interface TaskDesignSectionProps {
  repositoryId: string;
  taskId: string;
  /** Names the repository's layer in the design system line. */
  repositoryName?: string;
  /** False while the drawer is closed: nothing is fetched. */
  active: boolean;
}

/**
 * Organism: the approved designs a task builds on — the design system its
 * repository follows, then per design task its hand-off specs, its screens
 * (opened in a sandboxed frame) and the screenshots the designer saved. The
 * same documents every run on the task already receives in its prompt.
 * Renders nothing when there is neither a design nor a design system.
 */
export function TaskDesignSection({ repositoryId, taskId, repositoryName, active }: TaskDesignSectionProps) {
  const { t } = useI18n();
  const { design, screenshots } = useTaskDesign(repositoryId, taskId, active);
  const [openDoc, setOpenDoc] = useState<TaskDocument | null>(null);

  if (!design) return null;
  const summary = design.design_system;
  if (design.references.length === 0 && !summary) return null;

  return (
    <>
      <Separator />
      <section className="space-y-3">
        <Label className="flex items-center gap-2 text-muted-foreground">
          <Palette className="h-3.5 w-3.5" />
          {t("designSystem.task.title")}
        </Label>
        {summary && <DesignSystemLine summary={summary} repositoryId={repositoryId} repositoryName={repositoryName} />}
        {design.references.length > 0 && (
          <>
            <p className="text-caption text-muted-foreground">{t("designSystem.task.referencesHint")}</p>
            {design.references.map((reference) => (
              <DesignReferenceCard
                key={reference.task_id}
                reference={reference}
                screenshots={screenshots[reference.task_id] ?? []}
                onOpenDocument={setOpenDoc}
              />
            ))}
          </>
        )}
      </section>
      <DesignDocumentDialog document={openDoc} onOpenChange={(open) => !open && setOpenDoc(null)} />
    </>
  );
}

function DesignSystemLine({
  summary,
  repositoryId,
  repositoryName,
}: {
  summary: TaskDesignSystemSummary;
  repositoryId: string;
  repositoryName?: string;
}) {
  const { t } = useI18n();
  const parts: string[] = [];
  if (summary.project_name && summary.base_version) {
    parts.push(t("designSystem.task.base", { project: summary.project_name, version: summary.base_version }));
  }
  if (summary.layer_version) {
    parts.push(
      repositoryName
        ? t("designSystem.task.layer", { repository: repositoryName, version: summary.layer_version })
        : t("designSystem.task.layerUnnamed", { version: summary.layer_version }),
    );
  }
  return (
    <div className="flex flex-wrap items-center gap-x-2 gap-y-1 text-caption">
      <span className="text-muted-foreground">{t("designSystem.task.designSystemLabel")}</span>
      <Link
        to={repositoryDesignSystemPath(repositoryId)}
        className="font-medium text-primary hover:underline"
        title={t("designSystem.task.openDesignSystem")}
      >
        {parts.length > 0 ? parts.join(" · ") : t("designSystem.task.openDesignSystem")}
      </Link>
      {summary.ambiguous && (
        <Badge variant="warning" className="gap-1">
          <AlertTriangle className="h-3 w-3" aria-hidden />
          {t("designSystem.repositories.ambiguous")}
        </Badge>
      )}
    </div>
  );
}

function DesignReferenceCard({
  reference,
  screenshots,
  onOpenDocument,
}: {
  reference: TaskDesignReference;
  screenshots: AttachmentMeta[];
  onOpenDocument: (doc: TaskDocument) => void;
}) {
  const { t } = useI18n();
  const specs = designMarkdownDocuments(reference.documents);
  const screens = designHtmlDocuments(reference.documents);
  const openSpecs = specs.filter((doc) => designDocumentKind(doc) === "handoff").map((doc) => doc.id);

  return (
    <Card>
      <CardHeader className="p-4 pb-3">
        <Link
          to={analysisReviewPath(reference.repository_id, reference.task_id)}
          className="flex min-w-0 items-baseline gap-2 hover:underline"
        >
          <span className="shrink-0 font-mono text-caption text-muted-foreground">{reference.key}</span>
          <span className="min-w-0 truncate text-body font-medium">{reference.title}</span>
        </Link>
      </CardHeader>
      <CardContent className="space-y-4 p-4 pt-0">
        {specs.length > 0 && (
          <div className="space-y-1.5">
            <p className="text-caption font-medium text-muted-foreground">{t("designSystem.task.handoff")}</p>
            <Accordion type="multiple" defaultValue={openSpecs}>
              {specs.map((doc) => (
                <AccordionItem key={doc.id} value={doc.id}>
                  <AccordionTrigger>{designDocumentLabel(doc)}</AccordionTrigger>
                  <AccordionContent className="text-foreground">
                    {doc.content.trim() ? (
                      <MarkdownContent content={doc.content} />
                    ) : (
                      <p className="text-caption text-muted-foreground">{t("designSystem.task.emptyDocument")}</p>
                    )}
                  </AccordionContent>
                </AccordionItem>
              ))}
            </Accordion>
          </div>
        )}

        {screens.length > 0 && (
          <div className="space-y-1.5">
            <p className="text-caption font-medium text-muted-foreground">{t("designSystem.task.screens")}</p>
            <ul className="divide-y divide-border rounded-lg border border-border">
              {screens.map((doc) => {
                const kind = designDocumentKind(doc);
                const label = designDocumentLabel(doc);
                return (
                  <li key={doc.id} className="flex items-center gap-3 px-3 py-2">
                    <FileCode2 className="h-4 w-4 shrink-0 text-muted-foreground" aria-hidden />
                    <span className="min-w-0 flex-1 truncate text-body">{label}</span>
                    {kind !== "mockup" && (
                      <Badge variant="outline" className="shrink-0">
                        {t(`designSystem.task.kinds.${kind}`)}
                      </Badge>
                    )}
                    <Button
                      size="sm"
                      variant="outline"
                      className="shrink-0"
                      onClick={() => onOpenDocument(doc)}
                      aria-label={t("designSystem.task.openScreenLabel", { title: label })}
                    >
                      {t("designSystem.task.openScreen")}
                    </Button>
                  </li>
                );
              })}
            </ul>
          </div>
        )}

        {screenshots.length > 0 && (
          <div className="space-y-1.5">
            <p className="text-caption font-medium text-muted-foreground">{t("designSystem.task.screenshots")}</p>
            <AttachmentList attachments={screenshots} />
          </div>
        )}

        {specs.length === 0 && screens.length === 0 && screenshots.length === 0 && (
          <p className="text-caption text-muted-foreground">{t("designSystem.task.noDocuments")}</p>
        )}
      </CardContent>
    </Card>
  );
}
