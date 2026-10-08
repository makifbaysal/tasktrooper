import { CheckCircle2, Loader2 } from "lucide-react";
import { useState } from "react";
import type { TaskDocument } from "@/api";
import { AnalysisFrame } from "@/components/board/analysis/AnalysisFrame";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { useI18n } from "@/hooks/useI18n";
import { designDocumentLabel } from "@/lib/design-system";
import { cn } from "@/lib/utils";

interface DesignVariantCompareProps {
  /** The design task's HTML documents, in the order the selects list them; at least two. */
  documents: TaskDocument[];
  /** The title the newest `Chosen variant: ` comment names. */
  chosenTitle: string | null;
  /** The document whose choice is being posted. */
  choosingId: string | null;
  onChoose: (doc: TaskDocument) => void;
  theme: "light" | "dark";
  className?: string;
}

/**
 * Organism: two design documents side by side (stacked on narrow widths),
 * each in its own read-only sandboxed frame with its own document select and
 * a "Choose this variant" action under it.
 */
export function DesignVariantCompare({
  documents,
  chosenTitle,
  choosingId,
  onChoose,
  theme,
  className,
}: DesignVariantCompareProps) {
  const { t } = useI18n();
  const [leftId, setLeftId] = useState(() => documents[0]?.id ?? "");
  const [rightId, setRightId] = useState(() => documents[1]?.id ?? documents[0]?.id ?? "");
  const left = documents.find((doc) => doc.id === leftId) ?? documents[0];
  const right = documents.find((doc) => doc.id === rightId) ?? documents[1] ?? documents[0];

  if (!left || !right) return null;

  return (
    <div className={cn("flex min-h-0 flex-1 flex-col overflow-y-auto lg:overflow-hidden", className)}>
      <p className="px-6 pt-3 text-caption text-muted-foreground">{t("analysisReview.design.compare.hint")}</p>
      <div className="flex flex-col gap-4 p-4 lg:min-h-0 lg:flex-1 lg:flex-row">
        <VariantPane
          label={t("analysisReview.design.compare.left")}
          documents={documents}
          doc={left}
          onSelect={setLeftId}
          chosenTitle={chosenTitle}
          choosingId={choosingId}
          onChoose={onChoose}
          theme={theme}
        />
        <VariantPane
          label={t("analysisReview.design.compare.right")}
          documents={documents}
          doc={right}
          onSelect={setRightId}
          chosenTitle={chosenTitle}
          choosingId={choosingId}
          onChoose={onChoose}
          theme={theme}
        />
      </div>
    </div>
  );
}

function isChosen(doc: TaskDocument, chosenTitle: string | null): boolean {
  return chosenTitle !== null && doc.title.trim() === chosenTitle;
}

function VariantPane({
  label,
  documents,
  doc,
  onSelect,
  chosenTitle,
  choosingId,
  onChoose,
  theme,
}: {
  label: string;
  documents: TaskDocument[];
  doc: TaskDocument;
  onSelect: (id: string) => void;
  chosenTitle: string | null;
  choosingId: string | null;
  onChoose: (doc: TaskDocument) => void;
  theme: "light" | "dark";
}) {
  const { t } = useI18n();
  const chosen = isChosen(doc, chosenTitle);
  const chosenLabel = t("analysisReview.design.compare.chosen");

  return (
    <section className="flex min-h-[70vh] min-w-0 flex-1 flex-col gap-2 lg:min-h-0" aria-label={label}>
      <div className="flex flex-wrap items-center gap-2">
        <Select value={doc.id} onValueChange={onSelect}>
          <SelectTrigger className="min-w-0 flex-1" aria-label={label}>
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            {documents.map((item) => (
              <SelectItem key={item.id} value={item.id}>
                {isChosen(item, chosenTitle)
                  ? `${designDocumentLabel(item)} · ${chosenLabel}`
                  : designDocumentLabel(item)}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
        {chosen && (
          <Badge variant="success" className="shrink-0 gap-1">
            <CheckCircle2 className="h-3 w-3" aria-hidden />
            {chosenLabel}
          </Badge>
        )}
      </div>
      <AnalysisFrame
        className={cn(
          "min-h-0 flex-1 overflow-hidden rounded-lg border",
          chosen ? "border-success" : "border-border",
        )}
        document={doc}
        theme={theme}
        title={t("designSystem.task.frameTitle", { title: designDocumentLabel(doc) })}
      />
      <Button
        variant={chosen ? "outline" : "default"}
        className="self-start"
        disabled={chosen || choosingId !== null}
        onClick={() => onChoose(doc)}
      >
        {choosingId === doc.id ? <Loader2 className="animate-spin" /> : <CheckCircle2 />}
        {t("analysisReview.design.compare.choose")}
      </Button>
    </section>
  );
}
