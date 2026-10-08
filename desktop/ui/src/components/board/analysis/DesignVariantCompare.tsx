import { CheckCircle2 } from "lucide-react";
import {
  forwardRef,
  useEffect,
  useImperativeHandle,
  useMemo,
  useRef,
  useState,
  type ReactNode,
  type RefObject,
} from "react";
import type { TaskAnnotation, TaskDocument } from "@/api";
import { AnalysisFrame, type AnalysisFrameHandle, type FrameAnnotation } from "@/components/board/analysis/AnalysisFrame";
import { DesignPagesList } from "@/components/board/analysis/DesignPagesList";
import type { CanvasPage, FrameSelection } from "@/components/board/analysis/srcdoc";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Label } from "@/components/ui/label";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Switch } from "@/components/ui/switch";
import { useI18n } from "@/hooks/useI18n";
import { matchingPage } from "@/lib/design-pages";
import { designDocumentLabel } from "@/lib/design-system";
import { cn } from "@/lib/utils";

type Side = "left" | "right";
const OTHER: Record<Side, Side> = { left: "right", right: "left" };

export interface DesignVariantCompareHandle {
  /** Brings an annotation into view in whichever pane shows its document. */
  scrollTo: (annotationId: string, documentId: string) => void;
}

interface DesignVariantCompareProps {
  /** The variants of every screen that has more than one, in the order the selects list them; at least two. */
  documents: TaskDocument[];
  leftId: string;
  rightId: string;
  onLeftChange: (id: string) => void;
  onRightChange: (id: string) => void;
  /** Every annotation of the task; each pane draws its own document's. */
  annotations: TaskAnnotation[];
  activeId: string | null;
  /** The titles the newest `Chosen variant: ` comments name, one per screen. */
  chosenTitles: string[];
  /** Opens the choice for this document; the page owns the dialog. */
  onChoose: (doc: TaskDocument) => void;
  onSelection: (documentId: string, selection: FrameSelection) => void;
  onFocusAnnotation: (id: string) => void;
  onAnchored: (found: Record<string, boolean>) => void;
  theme: "light" | "dark";
  className?: string;
}

/**
 * Organism: two variants side by side, each on its own canvas, with one page
 * list between them. Going to a page — from the list, or by panning one canvas
 * onto another page — takes the other variant to its version of the same page
 * while "keep pages in step" is on. Both canvases take comments; the page's
 * comment panel lists them with the variant they were made on.
 */
export const DesignVariantCompare = forwardRef<DesignVariantCompareHandle, DesignVariantCompareProps>(
  function DesignVariantCompare(
    {
      documents,
      leftId,
      rightId,
      onLeftChange,
      onRightChange,
      annotations,
      activeId,
      chosenTitles,
      onChoose,
      onSelection,
      onFocusAnnotation,
      onAnchored,
      theme,
      className,
    },
    ref,
  ) {
    const { t } = useI18n();
    const leftFrame = useRef<AnalysisFrameHandle>(null);
    const rightFrame = useRef<AnalysisFrameHandle>(null);
    const frames: Record<Side, RefObject<AnalysisFrameHandle | null>> = { left: leftFrame, right: rightFrame };
    const [outlines, setOutlines] = useState<Record<Side, CanvasPage[]>>({ left: [], right: [] });
    const [pages, setPages] = useState<Record<Side, string | null>>({ left: null, right: null });
    const [sync, setSync] = useState(true);
    const latest = useRef({ outlines, pages, sync });
    latest.current = { outlines, pages, sync };

    const left = documents.find((doc) => doc.id === leftId) ?? documents[0];
    const right = documents.find((doc) => doc.id === rightId) ?? documents[1] ?? documents[0];

    useEffect(() => {
      setOutlines((prev) => ({ ...prev, left: [] }));
      setPages((prev) => ({ ...prev, left: null }));
    }, [left?.id]);
    useEffect(() => {
      setOutlines((prev) => ({ ...prev, right: [] }));
      setPages((prev) => ({ ...prev, right: null }));
    }, [right?.id]);

    useImperativeHandle(
      ref,
      () => ({
        scrollTo: (annotationId, documentId) => {
          if (left?.id === documentId) leftFrame.current?.scrollTo(annotationId);
          else if (right?.id === documentId) rightFrame.current?.scrollTo(annotationId);
        },
      }),
      [left?.id, right?.id],
    );

    const follow = (from: Side, id: string) => {
      const to = OTHER[from];
      const { outlines: current, pages: shown } = latest.current;
      const target = matchingPage(current[from], id, current[to]);
      if (target && target !== shown[to]) frames[to].current?.goToPage(target);
    };

    const pageChanged = (side: Side) => (id: string | null, cause: "view" | "goto") => {
      setPages((prev) => (prev[side] === id ? prev : { ...prev, [side]: id }));
      if (cause === "view" && id && latest.current.sync) follow(side, id);
    };

    const goToPage = (id: string) => {
      leftFrame.current?.goToPage(id);
      if (sync) follow("left", id);
    };

    if (!left || !right) return null;

    const pane = (side: Side, doc: TaskDocument, onChange: (id: string) => void) => (
      <VariantPane
        key={side}
        label={t(side === "left" ? "analysisReview.design.compare.left" : "analysisReview.design.compare.right")}
        documents={documents}
        doc={doc}
        onSelect={onChange}
        chosenTitles={chosenTitles}
        onChoose={onChoose}
      >
        <AnalysisFrame
          ref={frames[side]}
          className={cn(
            "min-h-0 flex-1 overflow-hidden rounded-lg border",
            isChosen(doc, chosenTitles) ? "border-success" : "border-border",
          )}
          document={doc}
          annotations={frameAnnotations(annotations, doc.id)}
          activeId={activeId}
          theme={theme}
          canvas
          onSelection={(selection) => onSelection(doc.id, selection)}
          onFocusAnnotation={onFocusAnnotation}
          onAnchored={onAnchored}
          onOutline={(next) => setOutlines((prev) => ({ ...prev, [side]: next }))}
          onPageChange={pageChanged(side)}
          title={t("designSystem.task.frameTitle", { title: designDocumentLabel(doc) })}
        />
      </VariantPane>
    );

    return (
      <div className={cn("flex min-h-0 min-w-0 flex-1", className)}>
        <DesignPagesList
          className="w-52 shrink-0 border-r border-border"
          pages={outlines.left}
          activeId={pages.left}
          onSelect={goToPage}
          toolbar={
            <div className="flex items-start gap-2">
              <Switch id="design-compare-sync" checked={sync} onCheckedChange={setSync} className="mt-0.5" />
              <Label htmlFor="design-compare-sync" className="text-caption font-normal text-muted-foreground">
                {t("analysisReview.design.pages.sync")}
              </Label>
            </div>
          }
        />
        <div className="grid min-h-0 min-w-0 flex-1 grid-cols-1 grid-rows-2 gap-3 p-3 lg:grid-cols-2 lg:grid-rows-1">
          {pane("left", left, onLeftChange)}
          {pane("right", right, onRightChange)}
        </div>
      </div>
    );
  },
);

function isChosen(doc: TaskDocument, chosenTitles: string[]): boolean {
  return chosenTitles.includes(doc.title.trim());
}

function frameAnnotations(annotations: TaskAnnotation[], documentId: string): FrameAnnotation[] {
  return annotations
    .filter((annotation) => annotation.document_id === documentId)
    .map(({ id, quote, prefix, suffix, status }) => ({ id, quote, prefix, suffix, status }));
}

function VariantPane({
  label,
  documents,
  doc,
  onSelect,
  chosenTitles,
  onChoose,
  children,
}: {
  label: string;
  documents: TaskDocument[];
  doc: TaskDocument;
  onSelect: (id: string) => void;
  chosenTitles: string[];
  onChoose: (doc: TaskDocument) => void;
  children: ReactNode;
}) {
  const { t } = useI18n();
  const chosen = isChosen(doc, chosenTitles);
  const chosenLabel = t("analysisReview.design.compare.chosen");
  const options = useMemo(
    () =>
      documents.map((item) => ({
        id: item.id,
        label: isChosen(item, chosenTitles) ? `${designDocumentLabel(item)} · ${chosenLabel}` : designDocumentLabel(item),
      })),
    [documents, chosenTitles, chosenLabel],
  );

  return (
    <section className="flex min-h-0 min-w-0 flex-col gap-2" aria-label={label}>
      <div className="flex flex-wrap items-center gap-2">
        <Select value={doc.id} onValueChange={onSelect}>
          <SelectTrigger className="min-w-0 flex-1" aria-label={label}>
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            {options.map((item) => (
              <SelectItem key={item.id} value={item.id}>
                {item.label}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
        {chosen ? (
          <Badge variant="success" className="h-8 shrink-0 gap-1 px-2.5">
            <CheckCircle2 className="h-3.5 w-3.5" aria-hidden />
            {chosenLabel}
          </Badge>
        ) : (
          <Button size="sm" className="shrink-0" onClick={() => onChoose(doc)}>
            <CheckCircle2 />
            {t("analysisReview.design.compare.choose")}
          </Button>
        )}
      </div>
      {children}
    </section>
  );
}
