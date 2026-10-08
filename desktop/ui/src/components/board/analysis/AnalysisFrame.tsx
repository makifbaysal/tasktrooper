import { forwardRef, useCallback, useEffect, useImperativeHandle, useMemo, useRef, useState } from "react";
import type { TaskAnnotationStatus, TaskDocument } from "@/api";
import { CanvasZoomControls } from "@/components/board/analysis/CanvasZoomControls";
import { buildFrameScript } from "@/components/board/analysis/frameRuntime";
import { renderMarkdownFrameHtml } from "@/components/board/analysis/markdownFrame";
import {
  buildAnalysisSrcdoc,
  createNonce,
  FRAME_SANDBOX,
  parseFrameMessage,
  type CanvasPage,
  type FrameQuestion,
  type FrameSelection,
  type QuestionsLabels,
} from "@/components/board/analysis/srcdoc";
import { Spinner } from "@/components/ui/spinner";
import { useI18n } from "@/hooks/useI18n";
import { documentFormat } from "@/lib/analysis-review";
import { cn } from "@/lib/utils";

const FRAME_SCRIPT = buildFrameScript();

export interface FrameAnnotation {
  id: string;
  quote: string;
  prefix: string;
  suffix: string;
  status: TaskAnnotationStatus;
}

export interface AnalysisFrameHandle {
  scrollTo: (id: string) => void;
  /** Canvas only: frame one page of the document (an id from `onOutline`). */
  goToPage: (id: string) => void;
}

interface AnalysisFrameProps {
  document: Pick<TaskDocument, "id" | "content" | "format">;
  /** Omit, with the handlers below, for a read-only view (no highlights, selections ignored). */
  annotations?: FrameAnnotation[];
  /** Drawn in the Open questions section at the top of the frame body, built from this structured data, never from the document's own HTML. */
  questions?: FrameQuestion[];
  activeId?: string | null;
  theme: "light" | "dark";
  onSelection?: (selection: FrameSelection) => void;
  onFocusAnnotation?: (id: string) => void;
  onAnchored?: (found: Record<string, boolean>) => void;
  onAnswer?: (id: string, text: string) => void;
  /** The iframe's accessible title; defaults to "Analysis document". */
  title?: string;
  /** Show an HTML document as a pan-and-zoom canvas (design mockups); markdown ignores it. */
  canvas?: boolean;
  /** Canvas only: the document's pages, each time the frame lays it out. */
  onOutline?: (pages: CanvasPage[]) => void;
  /** Canvas only: the page in view changed — by panning (`view`) or by `goToPage` (`goto`). */
  onPageChange?: (id: string | null, cause: "view" | "goto") => void;
  className?: string;
}

/**
 * The analysis document, rendered where it cannot reach the app: a sandboxed,
 * opaque-origin iframe (see srcdoc.ts) that talks to this component only by
 * postMessage. Nothing goes in but the document and the annotations to draw.
 */
const NOOP = () => {};
const NO_ANNOTATIONS: FrameAnnotation[] = [];

export const AnalysisFrame = forwardRef<AnalysisFrameHandle, AnalysisFrameProps>(function AnalysisFrame(
  {
    document: doc,
    annotations = NO_ANNOTATIONS,
    questions = [],
    activeId = null,
    theme,
    onSelection = NOOP,
    onFocusAnnotation = NOOP,
    onAnchored = NOOP,
    onAnswer = NOOP,
    title,
    canvas = false,
    onOutline = NOOP,
    onPageChange = NOOP,
    className,
  },
  ref,
) {
  const { t } = useI18n();
  const iframeRef = useRef<HTMLIFrameElement>(null);
  const [srcdoc, setSrcdoc] = useState<string | null>(null);
  const [zoom, setZoom] = useState<number | null>(null);
  const format = documentFormat(doc);
  const markdownTheme = format === "markdown" ? theme : null;
  const canvasMode = canvas && format === "html";

  useEffect(() => {
    let cancelled = false;
    setZoom(null);
    const build = (html: string) =>
      buildAnalysisSrcdoc(html, { nonce: createNonce(), script: FRAME_SCRIPT, canvas: canvasMode });
    if (markdownTheme === null) {
      setSrcdoc(build(doc.content));
      return;
    }
    setSrcdoc(null);
    renderMarkdownFrameHtml(doc.content, markdownTheme)
      .then((html) => {
        if (!cancelled) setSrcdoc(build(html));
      })
      .catch(() => {
        if (!cancelled) setSrcdoc(build(""));
      });
    return () => {
      cancelled = true;
    };
  }, [doc.id, doc.content, markdownTheme, canvasMode]);

  const items = useMemo(
    () =>
      annotations.map((a) => ({
        id: a.id,
        quote: a.quote,
        prefix: a.prefix,
        suffix: a.suffix,
        status: a.status,
        active: a.id === activeId,
      })),
    [annotations, activeId],
  );
  // The board polls while the agent revises, so `annotations` is a new array
  // every few seconds; re-marking the document each time would disturb a
  // selection the reader is in the middle of. Only a real change is sent.
  const payload = JSON.stringify(items);
  const itemsRef = useRef(items);
  itemsRef.current = items;

  const handlers = useRef({ onSelection, onFocusAnnotation, onAnchored, onAnswer, onOutline, onPageChange });
  handlers.current = { onSelection, onFocusAnnotation, onAnchored, onAnswer, onOutline, onPageChange };

  const questionLabels: QuestionsLabels = useMemo(
    () => ({
      heading: t("analysisReview.questions.heading"),
      kindProduct: t("analysisReview.questions.kind.product"),
      kindTechnical: t("analysisReview.questions.kind.technical"),
      blocking: t("analysisReview.questions.blockingBadge"),
      recommendedPrefix: t("analysisReview.questions.recommendedPrefix"),
      answerLabel: t("analysisReview.questions.answerLabel"),
      answerPlaceholder: t("analysisReview.questions.answerPlaceholder"),
      unanswered: t("analysisReview.questions.unanswered"),
      recommendedStands: t("analysisReview.questions.recommendedStands"),
    }),
    [t],
  );
  // Same reasoning as `payload` above: only a real change (new answers, a
  // newly recorded/withdrawn question) re-posts, so a poll never interrupts
  // someone mid-sentence in an answer box — frameRuntime.ts additionally
  // never overwrites the one textarea that currently has focus.
  const questionsPayload = JSON.stringify([questionLabels, questions]);
  const questionsRef = useRef(questions);
  questionsRef.current = questions;

  const post = useCallback((message: Record<string, unknown>) => {
    iframeRef.current?.contentWindow?.postMessage(message, "*");
  }, []);

  const postItems = useCallback(() => {
    post({ type: "tt:annotations", items: itemsRef.current });
  }, [post]);

  const postQuestions = useCallback(() => {
    post({ type: "tt:questions", labels: questionLabels, items: questionsRef.current });
  }, [post, questionLabels]);

  useEffect(() => {
    postItems();
  }, [payload, postItems]);

  useEffect(() => {
    postQuestions();
  }, [questionsPayload, postQuestions]);

  useEffect(() => {
    const onMessage = (event: MessageEvent) => {
      const frameWindow = iframeRef.current?.contentWindow;
      if (!frameWindow || event.source !== frameWindow) return;
      const message = parseFrameMessage(event.data);
      if (!message) return;
      switch (message.type) {
        case "tt:ready":
          postItems();
          postQuestions();
          break;
        case "tt:selection":
          handlers.current.onSelection({ quote: message.quote, prefix: message.prefix, suffix: message.suffix });
          break;
        case "tt:anchored":
          handlers.current.onAnchored(Object.fromEntries(message.results.map((r) => [r.id, r.found])));
          break;
        case "tt:focus":
          handlers.current.onFocusAnnotation(message.id);
          break;
        case "tt:answer":
          handlers.current.onAnswer(message.id, message.text);
          break;
        case "tt:zoom":
          setZoom(message.zoom);
          break;
        case "tt:outline":
          handlers.current.onOutline(message.pages);
          break;
        case "tt:page":
          handlers.current.onPageChange(message.id, message.cause);
          break;
      }
    };
    window.addEventListener("message", onMessage);
    return () => window.removeEventListener("message", onMessage);
  }, [postItems, postQuestions]);

  useImperativeHandle(
    ref,
    () => ({
      scrollTo: (id: string) => post({ type: "tt:scrollTo", id }),
      goToPage: (id: string) => post({ type: "tt:goto", page: id }),
    }),
    [post],
  );

  const onFrameLoad = useCallback(() => {
    postItems();
    postQuestions();
  }, [postItems, postQuestions]);

  return (
    <div className={cn("relative min-h-0 min-w-0", className)}>
      {srcdoc === null ? (
        <div className="flex h-full items-center justify-center">
          <Spinner />
        </div>
      ) : (
        <iframe
          ref={iframeRef}
          title={title ?? t("analysisReview.page.frameTitle")}
          sandbox={FRAME_SANDBOX}
          srcDoc={srcdoc}
          referrerPolicy="no-referrer"
          onLoad={onFrameLoad}
          className={cn("h-full w-full border-0", format === "html" ? "bg-white" : "bg-background")}
        />
      )}
      {canvasMode && srcdoc !== null && (
        <CanvasZoomControls
          zoom={zoom}
          onAction={(action) => post({ type: "tt:zoom", action })}
          className="absolute bottom-3 right-3"
        />
      )}
    </div>
  );
});
