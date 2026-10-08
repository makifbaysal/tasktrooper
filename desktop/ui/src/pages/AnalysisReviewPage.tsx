import { CheckCircle2, Columns2, FileText, HelpCircle, Loader2, SearchX, Send } from "lucide-react";
import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { useNavigate, useParams, useSearchParams } from "react-router-dom";
import { toast } from "sonner";
import { api, type BoardTask, type TaskAnnotation, type TaskComment, type TaskDocument, type TaskQuestion } from "@/api";
import { AnalysisFrame, type AnalysisFrameHandle } from "@/components/board/analysis/AnalysisFrame";
import { AnnotationsPanel, type PendingSelection } from "@/components/board/analysis/AnnotationsPanel";
import { ChooseVariantDialog } from "@/components/board/analysis/ChooseVariantDialog";
import { DesignPagesList } from "@/components/board/analysis/DesignPagesList";
import {
  DesignVariantCompare,
  type DesignVariantCompareHandle,
} from "@/components/board/analysis/DesignVariantCompare";
import { SubmitAnnotationsDialog } from "@/components/board/analysis/SubmitAnnotationsDialog";
import type { CanvasPage } from "@/components/board/analysis/srcdoc";
import { Badge } from "@/components/ui/badge";
import { PageBackLink } from "@/components/layout/PageBackLink";
import { Button } from "@/components/ui/button";
import { ConfirmDialog } from "@/components/ui/confirm-dialog";
import { EmptyState } from "@/components/ui/empty-state";
import { Notice } from "@/components/ui/notice";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Spinner } from "@/components/ui/spinner";
import { useI18n } from "@/hooks/useI18n";
import { usePolling } from "@/hooks/usePolling";
import { useTheme } from "@/hooks/useTheme";
import {
  answeredUnsubmittedQuestions,
  annotationCounts,
  canSendAnswers,
  documentFormat,
  isQuestionAnswerEditable,
  isRevising,
  pendingBlockingQuestions,
  pickReviewDocument,
  visibleQuestions,
} from "@/lib/analysis-review";
import {
  chosenVariantComment,
  chosenVariantTitle,
  designDocumentLabel,
  designHtmlDocuments,
  isDesignTask,
} from "@/lib/design-system";

const REVISION_POLL_MS = 3000;

/**
 * Full-window review of a task's analysis document: the document in a
 * sandboxed frame on the left, the reader's passage comments on the right, and
 * one button that sends every open comment to the agent at once. A page rather
 * than another dialog inside the task drawer, which is itself a dialog.
 */
export function AnalysisReviewPage() {
  const { repositoryId = "", taskId = "" } = useParams();
  const [searchParams, setSearchParams] = useSearchParams();
  const navigate = useNavigate();
  const { t } = useI18n();
  const { theme } = useTheme();
  const [task, setTask] = useState<BoardTask | null>(null);
  const [documents, setDocuments] = useState<TaskDocument[]>([]);
  const [annotations, setAnnotations] = useState<TaskAnnotation[]>([]);
  const [questions, setQuestions] = useState<TaskQuestion[]>([]);
  const [loading, setLoading] = useState(true);
  const [loadError, setLoadError] = useState<string | null>(null);
  const [anchored, setAnchored] = useState<Record<string, boolean>>({});
  const [activeId, setActiveId] = useState<string | null>(null);
  const [pending, setPending] = useState<PendingSelection | null>(null);
  const [submitOpen, setSubmitOpen] = useState(false);
  const [approveConfirmOpen, setApproveConfirmOpen] = useState(false);
  const [approving, setApproving] = useState(false);
  const [sendingAnswers, setSendingAnswers] = useState(false);
  const [comments, setComments] = useState<TaskComment[]>([]);
  const [compare, setCompare] = useState(false);
  const [compareIds, setCompareIds] = useState<{ left: string | null; right: string | null }>({ left: null, right: null });
  const [chooseOpen, setChooseOpen] = useState(false);
  const [chooseInitialId, setChooseInitialId] = useState<string | null>(null);
  const [outline, setOutline] = useState<CanvasPage[]>([]);
  const [page, setPage] = useState<string | null>(null);
  const frameRef = useRef<AnalysisFrameHandle>(null);
  const compareRef = useRef<DesignVariantCompareHandle>(null);
  // Bumped by every local annotation change, so a poll that left before the
  // change cannot land after it and put the old list back.
  const mutationVersion = useRef(0);

  const boardPath = `/board?task=${encodeURIComponent(taskId)}`;

  const refresh = useCallback(async () => {
    const version = mutationVersion.current;
    const [tasks, docs, notes, qs] = await Promise.allSettled([
      api.listRepositoryTasks(repositoryId),
      api.listTaskDocuments(repositoryId, taskId),
      api.listTaskAnnotations(repositoryId, taskId),
      api.listTaskQuestions(repositoryId, taskId),
    ]);
    if (tasks.status === "fulfilled") setTask((tasks.value.tasks ?? []).find((item) => item.id === taskId) ?? null);
    if (docs.status === "fulfilled") setDocuments(docs.value.documents ?? []);
    if (notes.status === "fulfilled" && version === mutationVersion.current) {
      setAnnotations(notes.value.annotations ?? []);
    }
    if (qs.status === "fulfilled" && version === mutationVersion.current) {
      setQuestions(qs.value.questions ?? []);
    }
    const failed = [tasks, docs].find((result) => result.status === "rejected");
    return failed ? (failed.reason as unknown) : null;
  }, [repositoryId, taskId]);

  useEffect(() => {
    let cancelled = false;
    setLoading(true);
    setLoadError(null);
    void refresh().then((error) => {
      if (cancelled) return;
      if (error) setLoadError(error instanceof Error ? error.message : t("analysisReview.page.loadFailed"));
      setLoading(false);
    });
    return () => {
      cancelled = true;
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [refresh]);

  const revising = isRevising(task?.column);
  const inReview = task?.column === "analiz_review";
  const design = isDesignTask(task);
  const poll = useCallback(async () => {
    await refresh();
  }, [refresh]);
  usePolling(poll, REVISION_POLL_MS, revising);

  // The poll that sees the task back in review fetched the document in the
  // same round trip, possibly a moment before the agent's last write — one
  // more read makes the reloaded document the final one.
  const wasRevising = useRef(false);
  useEffect(() => {
    if (wasRevising.current && !revising) void refresh();
    wasRevising.current = revising;
  }, [revising, refresh]);

  useEffect(() => {
    if (revising) setPending(null);
  }, [revising]);

  const requestedDocId = searchParams.get("doc");
  const currentDoc = useMemo(
    () => documents.find((doc) => doc.id === requestedDocId) ?? pickReviewDocument(documents),
    [documents, requestedDocId],
  );
  const currentDocId = currentDoc?.id ?? null;
  const docAnnotations = useMemo(
    () => annotations.filter((annotation) => annotation.document_id === currentDocId),
    [annotations, currentDocId],
  );
  const openCount = annotationCounts(annotations).open;

  const variantDocs = useMemo(() => (design ? designHtmlDocuments(documents) : []), [design, documents]);
  const canCompare = variantDocs.length >= 2;
  const comparing = compare && canCompare;
  const leftId = (variantDocs.find((doc) => doc.id === compareIds.left) ?? variantDocs[0])?.id ?? null;
  const rightId = (variantDocs.find((doc) => doc.id === compareIds.right) ?? variantDocs[1] ?? variantDocs[0])?.id ?? null;
  const canvasDoc = design && currentDoc !== null && documentFormat(currentDoc) === "html";
  const panelAnnotations = useMemo(
    () =>
      comparing
        ? annotations.filter((annotation) => annotation.document_id === leftId || annotation.document_id === rightId)
        : docAnnotations,
    [comparing, annotations, docAnnotations, leftId, rightId],
  );
  const documentLabel = useCallback(
    (id: string) => {
      const doc = documents.find((item) => item.id === id);
      return doc ? designDocumentLabel(doc) : undefined;
    },
    [documents],
  );

  useEffect(() => {
    if (!canCompare) return;
    let cancelled = false;
    api
      .listTaskComments(repositoryId, taskId)
      .then((data) => {
        if (!cancelled) setComments(data.comments ?? []);
      })
      .catch(() => {});
    return () => {
      cancelled = true;
    };
  }, [canCompare, repositoryId, taskId]);

  const chosenTitle = useMemo(() => (canCompare ? chosenVariantTitle(comments) : null), [canCompare, comments]);
  const needsChoice = canCompare && chosenTitle === null;

  const blockedOnQuestions = task?.column === "blocked" && task?.blocked_resource === "analysis_questions";
  const shownQuestions = useMemo(() => visibleQuestions(questions), [questions]);
  const unansweredBlocking = useMemo(() => pendingBlockingQuestions(shownQuestions), [shownQuestions]);
  const unsubmittedAnswers = useMemo(() => answeredUnsubmittedQuestions(shownQuestions), [shownQuestions]);
  const canSend = canSendAnswers(shownQuestions);
  const frameQuestions = useMemo(
    () =>
      task === null
        ? []
        : shownQuestions.map((question) => ({
            id: question.id,
            key: question.key,
            kind: question.kind,
            blocking: question.blocking,
            prompt: question.prompt,
            recommendedAnswer: question.recommended_answer,
            answer: question.answer,
            status: question.status,
            editable: isQuestionAnswerEditable(question, task),
          })),
    [shownQuestions, task],
  );

  useEffect(() => {
    setPending(null);
    setActiveId(null);
    setAnchored({});
  }, [currentDocId, comparing]);

  useEffect(() => {
    setOutline([]);
    setPage(null);
  }, [currentDocId]);

  const selectDocument = (id: string) => {
    setSearchParams(
      (prev) => {
        const next = new URLSearchParams(prev);
        next.set("doc", id);
        return next;
      },
      { replace: true },
    );
  };

  const upsert = useCallback((annotation: TaskAnnotation) => {
    mutationVersion.current += 1;
    setAnnotations((prev) =>
      prev.some((item) => item.id === annotation.id)
        ? prev.map((item) => (item.id === annotation.id ? annotation : item))
        : [...prev, annotation],
    );
  }, []);

  const remove = useCallback((id: string) => {
    mutationVersion.current += 1;
    setAnnotations((prev) => prev.filter((item) => item.id !== id));
    setActiveId((prev) => (prev === id ? null : prev));
  }, []);

  const activate = (id: string) => {
    setActiveId(id);
    if (!comparing) {
      frameRef.current?.scrollTo(id);
      return;
    }
    const annotation = annotations.find((item) => item.id === id);
    if (annotation) compareRef.current?.scrollTo(id, annotation.document_id);
  };

  const mergeAnchored = useCallback((found: Record<string, boolean>) => {
    setAnchored((prev) => ({ ...prev, ...found }));
  }, []);

  const openChoice = (doc: TaskDocument | null) => {
    setChooseInitialId(doc?.id ?? null);
    setChooseOpen(true);
  };

  // The marker line is English — the designer agent reads it; the reviewer's
  // note follows on its own lines. Only the buttons are translated.
  const chooseVariant = async (doc: TaskDocument, note: string) => {
    const content = note ? `${chosenVariantComment(doc.title)}\n\n${note}` : chosenVariantComment(doc.title);
    try {
      const comment = await api.createTaskComment(repositoryId, taskId, content);
      setComments((prev) => [...prev, { ...comment, content, created_at: comment.created_at || new Date().toISOString() }]);
      toast.success(t("analysisReview.design.compare.chosenToast", { title: designDocumentLabel(doc) }));
    } catch (e) {
      toast.error(e instanceof Error ? e.message : t("analysisReview.design.compare.chooseFailed"));
      throw e;
    }
  };

  const approve = async () => {
    setApproving(true);
    try {
      await api.updateRepositoryTask(repositoryId, taskId, { column: "done" });
      toast.success(t("boardArea.components.taskDetail.analizReviewApproved"));
      navigate(boardPath);
    } catch (e) {
      toast.error(e instanceof Error ? e.message : t("boardArea.components.taskDetail.updateFailed"));
    } finally {
      setApproving(false);
    }
  };

  const submit = async (note: string) => {
    const answerCount = unsubmittedAnswers.length;
    try {
      const result = await api.submitTaskAnnotations(repositoryId, taskId, note || undefined);
      setSubmitOpen(false);
      toast.success(
        result.submitted > 0
          ? t(design ? "analysisReview.design.commentsSent" : "analysisReview.submit.sent", { count: result.submitted })
          : t(design ? "analysisReview.design.answersSent" : "analysisReview.submit.answersSent", { count: answerCount }),
      );
      navigate(boardPath);
    } catch (e) {
      toast.error(e instanceof Error ? e.message : t("analysisReview.submit.failed"));
    }
  };

  const answerQuestion = useCallback(
    async (id: string, text: string) => {
      mutationVersion.current += 1;
      try {
        const { question } = await api.answerTaskQuestion(repositoryId, taskId, id, text);
        setQuestions((prev) => prev.map((item) => (item.id === id ? question : item)));
      } catch (e) {
        toast.error(e instanceof Error ? e.message : t("analysisReview.questions.answerFailed"));
        void refresh();
      }
    },
    [repositoryId, taskId, refresh, t],
  );

  const sendAnswers = async () => {
    setSendingAnswers(true);
    try {
      const result = await api.submitTaskQuestions(repositoryId, taskId);
      toast.success(t(design ? "analysisReview.design.questionsSent" : "analysisReview.questions.sent", { count: result.submitted }));
      navigate(boardPath);
    } catch (e) {
      toast.error(e instanceof Error ? e.message : t("analysisReview.questions.sendFailed"));
    } finally {
      setSendingAnswers(false);
    }
  };

  if (loading) {
    return (
      <div className="flex h-full items-center justify-center">
        <Spinner />
      </div>
    );
  }

  if (!task) {
    return (
      <div className="p-page">
        <PageBackLink to={boardPath} label={t("analysisReview.page.back")} />
        <EmptyState
          icon={SearchX}
          variant={loadError ? "critical" : "default"}
          title={loadError ? t("analysisReview.page.loadFailed") : t("analysisReview.page.taskNotFound")}
          description={loadError ?? t("analysisReview.page.taskNotFoundBody")}
        />
      </div>
    );
  }

  const annotationsPanel = (width: string) => (
    <AnnotationsPanel
      className={`${width} shrink-0 border-l border-border`}
      repositoryId={repositoryId}
      taskId={taskId}
      documentId={comparing ? leftId : currentDocId}
      annotations={panelAnnotations}
      anchored={anchored}
      activeId={activeId}
      pending={pending}
      canComment={!revising && (comparing || currentDoc !== null)}
      onActivate={activate}
      onCancelPending={() => setPending(null)}
      onUpsert={upsert}
      onRemove={remove}
      pausedLabel={design ? t("analysisReview.design.commentingPaused") : undefined}
      documentLabel={comparing ? documentLabel : undefined}
    />
  );

  const frameAnnotations = docAnnotations.map(({ id, quote, prefix, suffix, status }) => ({
    id,
    quote,
    prefix,
    suffix,
    status,
  }));

  return (
    <div className="flex h-full min-h-0 flex-col">
      <header className="flex flex-wrap items-center gap-3 border-b border-border px-6 py-3">
        <PageBackLink to={boardPath} label={t("analysisReview.page.back")} className="mb-0" />
        <div className="min-w-0 flex-1">
          <p className="font-mono text-caption text-muted-foreground">{task.key}</p>
          <h1 className="truncate text-title font-semibold">{task.title}</h1>
        </div>
        {documents.length > 1 && currentDoc && !comparing && (
          <Select value={currentDoc.id} onValueChange={selectDocument}>
            <SelectTrigger className="w-64" aria-label={t("analysisReview.page.documentLabel")}>
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              {documents.map((doc) => (
                <SelectItem key={doc.id} value={doc.id}>
                  {chosenTitle !== null && doc.title.trim() === chosenTitle
                    ? `${doc.title} · ${t("analysisReview.design.compare.chosen")}`
                    : doc.title}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
        )}
        {canCompare && (
          <div className="flex items-center gap-2">
            {chosenTitle !== null ? (
              <Badge variant="success" className="h-9 max-w-64 gap-1.5 px-3">
                <CheckCircle2 className="h-3.5 w-3.5 shrink-0" aria-hidden />
                <span className="truncate">
                  {t("analysisReview.design.choice.current", { title: designDocumentLabel({ title: chosenTitle }) })}
                </span>
              </Badge>
            ) : (
              <span className="text-caption text-muted-foreground">{t("analysisReview.design.choice.none")}</span>
            )}
            <Button variant={chosenTitle === null ? "default" : "outline"} onClick={() => openChoice(null)}>
              <CheckCircle2 />
              {t("analysisReview.design.choice.open")}
            </Button>
          </div>
        )}
        {canCompare && (
          <Button variant="outline" aria-pressed={comparing} onClick={() => setCompare((v) => !v)}>
            <Columns2 />
            {comparing ? t("analysisReview.design.compare.close") : t("analysisReview.design.compare.toggle")}
          </Button>
        )}
        {inReview && (
          <Button
            variant="outline"
            onClick={() =>
              openCount > 0 || unsubmittedAnswers.length > 0 || needsChoice ? setApproveConfirmOpen(true) : void approve()
            }
            disabled={approving}
          >
            {approving && <Loader2 className="animate-spin" />}
            {t("boardArea.components.taskDetail.analizReviewApprove")}
          </Button>
        )}
        {blockedOnQuestions && (
          <Button
            onClick={() => void sendAnswers()}
            disabled={sendingAnswers || !canSend}
            title={
              canSend
                ? undefined
                : t("analysisReview.questions.missingBlocking", {
                    keys: unansweredBlocking.map((question) => question.key).join(", "),
                  })
            }
          >
            {sendingAnswers ? <Loader2 className="animate-spin" /> : <HelpCircle />}
            {t("analysisReview.questions.sendAnswers")}
          </Button>
        )}
        <Button
          onClick={() => setSubmitOpen(true)}
          disabled={!inReview || (openCount === 0 && unsubmittedAnswers.length === 0)}
          title={
            inReview
              ? undefined
              : t(design ? "analysisReview.design.submitOnlyInReview" : "analysisReview.page.submitOnlyInReview")
          }
        >
          <Send />
          {t("analysisReview.page.submit", { count: openCount + unsubmittedAnswers.length })}
        </Button>
      </header>
      {revising && (
        <Notice
          variant="info"
          title={t(design ? "analysisReview.design.revising" : "analysisReview.page.revising")}
          className="mx-6 mt-3"
        >
          {t("analysisReview.page.revisingBody")}
        </Notice>
      )}
      {inReview && design && (
        <Notice variant="info" title={t("analysisReview.page.designApproveHint")} className="mx-6 mt-3" />
      )}
      {comparing && leftId && rightId ? (
        <div className="flex min-h-0 flex-1">
          <DesignVariantCompare
            ref={compareRef}
            documents={variantDocs}
            leftId={leftId}
            rightId={rightId}
            onLeftChange={(id) => setCompareIds((prev) => ({ ...prev, left: id }))}
            onRightChange={(id) => setCompareIds((prev) => ({ ...prev, right: id }))}
            annotations={panelAnnotations}
            activeId={activeId}
            chosenTitle={chosenTitle}
            onChoose={openChoice}
            onSelection={(documentId, selection) => {
              if (!revising) setPending({ ...selection, documentId });
            }}
            onFocusAnnotation={setActiveId}
            onAnchored={mergeAnchored}
            theme={theme}
          />
          {annotationsPanel("w-[320px]")}
        </div>
      ) : (
        <div className="flex min-h-0 flex-1">
          {currentDoc ? (
            <>
              {canvasDoc && (
                <DesignPagesList
                  className="w-52 shrink-0 border-r border-border"
                  pages={outline}
                  activeId={page}
                  onSelect={(id) => frameRef.current?.goToPage(id)}
                />
              )}
              <AnalysisFrame
                ref={frameRef}
                className="min-w-0 flex-1"
                document={currentDoc}
                annotations={frameAnnotations}
                questions={frameQuestions}
                activeId={activeId}
                theme={theme}
                onSelection={(selection) => {
                  if (!revising) setPending({ ...selection, documentId: currentDoc.id });
                }}
                onFocusAnnotation={setActiveId}
                onAnchored={setAnchored}
                onAnswer={(id, text) => void answerQuestion(id, text)}
                title={design ? t("analysisReview.design.frameTitle") : undefined}
                canvas={design}
                onOutline={setOutline}
                onPageChange={(id) => setPage(id)}
              />
            </>
          ) : (
            <EmptyState
              className="flex-1"
              icon={FileText}
              title={t(design ? "analysisReview.design.noDocuments" : "analysisReview.page.noDocuments")}
              description={t(design ? "analysisReview.design.noDocumentsBody" : "analysisReview.page.noDocumentsBody")}
            />
          )}
          {annotationsPanel("w-[360px]")}
        </div>
      )}
      <ChooseVariantDialog
        open={chooseOpen}
        onOpenChange={setChooseOpen}
        documents={variantDocs}
        chosenTitle={chosenTitle}
        initialId={chooseInitialId}
        onChoose={chooseVariant}
      />
      <SubmitAnnotationsDialog
        open={submitOpen}
        onOpenChange={setSubmitOpen}
        openCount={openCount}
        answerCount={unsubmittedAnswers.length}
        onSubmit={submit}
      />
      <ConfirmDialog
        open={approveConfirmOpen}
        onOpenChange={setApproveConfirmOpen}
        title={
          openCount > 0
            ? t("analysisReview.page.approveWithOpenTitle")
            : unsubmittedAnswers.length > 0
              ? t("analysisReview.questions.approveWithAnswersTitle")
              : t("analysisReview.design.choice.approveTitle")
        }
        description={[
          openCount > 0 ? t("analysisReview.page.approveWithOpenBody", { count: openCount }) : null,
          needsChoice ? t("analysisReview.design.choice.approveNote") : null,
          unsubmittedAnswers.length > 0
            ? t("analysisReview.questions.approveNote", { count: unsubmittedAnswers.length })
            : null,
        ]
          .filter(Boolean)
          .join(" ")}
        confirmLabel={t("boardArea.components.taskDetail.analizReviewApprove")}
        variant="default"
        loading={approving}
        onConfirm={approve}
      />
    </div>
  );
}
