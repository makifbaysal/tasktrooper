import { useEffect, useMemo, useState } from "react";
import { ChevronLeft, ChevronRight, HelpCircle } from "lucide-react";
import type { ClarificationOption, ClarificationQuestion, ClarificationRequest } from "@/api";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Textarea } from "@/components/ui/textarea";
import { useI18n } from "@/hooks/useI18n";
import { cn } from "@/lib/utils";

type TranslateFn = (key: string, params?: Record<string, string | number>) => string;

interface ClarificationCardProps {
  clarification: ClarificationRequest;
  onSubmit: (answer: string) => void;
  disabled?: boolean;
  className?: string;
}

type QuestionAnswer = {
  selectedIds: string[];
  otherText: string;
  freeText: string;
};

type QuestionMode = "text" | "choice";

function isOtherOption(opt: ClarificationOption): boolean {
  return opt.id === "other";
}

function isFreeTextOption(opt: ClarificationOption): boolean {
  return opt.id === "free_text";
}

function isSkipOption(opt: ClarificationOption): boolean {
  return opt.id === "skip";
}

function resolveQuestionMode(q: ClarificationQuestion): QuestionMode {
  if (q.options.some(isFreeTextOption)) return "text";
  return "choice";
}

function standardOptionLabel(id: string, t: TranslateFn): string {
  if (id === "other") return t("chatArea.chat.clarification.other");
  if (id === "skip") return t("chatArea.chat.clarification.skip");
  return id;
}

function optionDisplayLabel(opt: ClarificationOption, t: TranslateFn): string {
  if (opt.label.trim()) return opt.label;
  return standardOptionLabel(opt.id, t);
}

function displayOptions(q: ClarificationQuestion, t: TranslateFn): ClarificationOption[] {
  if (resolveQuestionMode(q) === "text") return q.options;
  const opts = [...q.options];
  if (!opts.some(isOtherOption)) {
    opts.push({ id: "other", label: standardOptionLabel("other", t) });
  }
  return opts;
}

function cardLabels(t: TranslateFn) {
  return {
    title: t("chatArea.chat.clarification.title"),
    questionOf: (current: number, total: number) =>
      t("chatArea.chat.clarification.questionOf", { current, total }),
    back: t("chatArea.chat.clarification.back"),
    next: t("chatArea.chat.clarification.next"),
    submit: t("chatArea.chat.clarification.submit"),
    otherPlaceholder: t("chatArea.chat.clarification.otherPlaceholder"),
    freeTextPlaceholder: t("chatArea.chat.clarification.freeTextPlaceholder"),
    otherRequired: t("chatArea.chat.clarification.otherRequired"),
    textRequired: t("chatArea.chat.clarification.textRequired"),
    selectOne: t("chatArea.chat.clarification.selectOne"),
    selectMany: t("chatArea.chat.clarification.selectMany"),
    selectUpTo: (max: number) => t("chatArea.chat.clarification.selectUpTo", { max }),
  };
}

function formatAnswerLine(
  q: ClarificationQuestion,
  answer: QuestionAnswer,
  t: TranslateFn,
): string {
  const mode = resolveQuestionMode(q);
  if (mode === "text") {
    const skip = q.options.find(
      (opt) => isSkipOption(opt) && answer.selectedIds.includes(opt.id),
    );
    if (skip) return `${q.prompt}: ${optionDisplayLabel(skip, t)}`;
    return `${q.prompt}: ${answer.freeText.trim()}`;
  }
  const options = displayOptions(q, t);
  const parts: string[] = [];
  for (const opt of options) {
    if (!answer.selectedIds.includes(opt.id)) continue;
    if (isOtherOption(opt)) {
      const text = answer.otherText.trim();
      if (text) parts.push(text);
    } else {
      parts.push(optionDisplayLabel(opt, t));
    }
  }
  return `${q.prompt}: ${parts.join(", ")}`;
}

export function ClarificationCard({
  clarification,
  onSubmit,
  disabled = false,
  className,
}: ClarificationCardProps) {
  const { t } = useI18n();
  const labels = useMemo(() => cardLabels(t), [t]);
  const questions = clarification.questions;

  const [step, setStep] = useState(0);
  const [answers, setAnswers] = useState<Record<string, QuestionAnswer>>({});

  useEffect(() => {
    setStep(0);
    setAnswers({});
  }, [clarification]);

  const current = questions[step];
  const currentAnswer = answers[current?.id ?? ""] ?? { selectedIds: [], otherText: "", freeText: "" };
  const isLast = step === questions.length - 1;
  const mode = current ? resolveQuestionMode(current) : "choice";
  const allowMultiple = current?.allow_multiple === true;
  const maxSelections =
    allowMultiple && current?.max_selections && current.max_selections > 0
      ? current.max_selections
      : 0;
  const atMax = maxSelections > 0 && currentAnswer.selectedIds.length >= maxSelections;
  const multiHint = maxSelections > 0 ? labels.selectUpTo(maxSelections) : labels.selectMany;
  const visibleOptions = current ? displayOptions(current, t) : [];
  const otherSelected = visibleOptions.some(
    (opt) => isOtherOption(opt) && currentAnswer.selectedIds.includes(opt.id),
  );
  const skipSelected = current?.options.some(
    (opt) => isSkipOption(opt) && currentAnswer.selectedIds.includes(opt.id),
  );
  const choiceOptions = visibleOptions.filter((opt) => !isFreeTextOption(opt));
  const skipOption = choiceOptions.find(isSkipOption);

  const stepValid = useMemo(() => {
    if (!current) return false;
    if (mode === "text") {
      if (skipSelected) return true;
      return currentAnswer.freeText.trim().length > 0;
    }
    if (currentAnswer.selectedIds.length === 0) return false;
    if (otherSelected && !currentAnswer.otherText.trim()) return false;
    return true;
  }, [current, currentAnswer, mode, otherSelected, skipSelected]);

  const selectChoice = (optionId: string) => {
    if (!current) return;
    setAnswers((prev) => {
      const existing = prev[current.id] ?? { selectedIds: [], otherText: "", freeText: "" };
      const already = existing.selectedIds.includes(optionId);
      const opt = visibleOptions.find((o) => o.id === optionId);
      if (mode === "text" && opt && isSkipOption(opt)) {
        return {
          ...prev,
          [current.id]: {
            selectedIds: already ? [] : [optionId],
            otherText: "",
            freeText: already ? existing.freeText : "",
          },
        };
      }
      if (allowMultiple) {
        let selectedIds: string[];
        if (already) {
          selectedIds = existing.selectedIds.filter((id) => id !== optionId);
        } else if (maxSelections > 0 && existing.selectedIds.length >= maxSelections) {
          return prev;
        } else {
          selectedIds = [...existing.selectedIds, optionId];
        }
        let otherText = existing.otherText;
        if (opt && isOtherOption(opt) && already) {
          otherText = "";
        }
        return {
          ...prev,
          [current.id]: {
            selectedIds,
            otherText,
            freeText: "",
          },
        };
      }
      return {
        ...prev,
        [current.id]: {
          selectedIds: already ? [] : [optionId],
          otherText: opt && isOtherOption(opt) && !already ? existing.otherText : "",
          freeText: "",
        },
      };
    });
  };

  const goNext = () => {
    if (!stepValid || disabled) return;
    if (isLast) {
      const lines = questions.map((q) =>
        formatAnswerLine(
          q,
          answers[q.id] ?? { selectedIds: [], otherText: "", freeText: "" },
          t,
        ),
      );
      onSubmit(lines.join("\n"));
      return;
    }
    setStep((s) => s + 1);
  };

  if (!current) return null;

  return (
    <Card className={cn("border-warning/40 bg-warning/5", className)}>
      <CardHeader className="pb-2">
        <div className="flex items-center justify-between gap-2">
          <CardTitle className="flex items-center gap-2 text-sm font-medium">
            <HelpCircle className="h-4 w-4 text-warning" />
            {labels.title}
          </CardTitle>
          <span className="text-xs text-muted-foreground">
            {labels.questionOf(step + 1, questions.length)}
          </span>
        </div>
        {clarification.context && step === 0 && (
          <p className="text-sm text-muted-foreground">{clarification.context}</p>
        )}
      </CardHeader>
      <CardContent className="space-y-4">
        <div className="space-y-3">
          <div className="space-y-1">
            <p className="text-sm font-medium">{current.prompt}</p>
            {mode === "choice" && allowMultiple && (
              <p className="text-xs text-muted-foreground">{multiHint}</p>
            )}
          </div>

          {mode === "text" ? (
            <>
              {!skipSelected && (
                <Textarea
                  value={currentAnswer.freeText}
                  onChange={(e) =>
                    setAnswers((prev) => ({
                      ...prev,
                      [current.id]: {
                        ...(prev[current.id] ?? { selectedIds: [], otherText: "", freeText: "" }),
                        freeText: e.target.value,
                        selectedIds: [],
                      },
                    }))
                  }
                  placeholder={labels.freeTextPlaceholder}
                  disabled={disabled}
                  rows={4}
                  className="resize-y min-h-[100px]"
                />
              )}
              {skipOption && (
                <Button
                  type="button"
                  size="sm"
                  variant={skipSelected ? "default" : "outline"}
                  disabled={disabled}
                  onClick={() => selectChoice(skipOption.id)}
                >
                  {optionDisplayLabel(skipOption, t)}
                </Button>
              )}
              {!stepValid && !skipSelected && (
                <p className="text-xs text-muted-foreground">{labels.textRequired}</p>
              )}
            </>
          ) : (
            <>
              <div className="flex flex-wrap gap-2">
                {choiceOptions.map((opt) => {
                  const selected = currentAnswer.selectedIds.includes(opt.id);
                  return (
                    <Button
                      key={opt.id}
                      type="button"
                      size="sm"
                      variant={selected ? "default" : "outline"}
                      disabled={disabled || (atMax && !selected)}
                      onClick={() => selectChoice(opt.id)}
                    >
                      {optionDisplayLabel(opt, t)}
                    </Button>
                  );
                })}
              </div>
              {otherSelected && (
                <Textarea
                  value={currentAnswer.otherText}
                  onChange={(e) =>
                    setAnswers((prev) => ({
                      ...prev,
                      [current.id]: {
                        ...(prev[current.id] ?? {
                          selectedIds: currentAnswer.selectedIds,
                          otherText: "",
                          freeText: "",
                        }),
                        otherText: e.target.value,
                      },
                    }))
                  }
                  placeholder={labels.otherPlaceholder}
                  disabled={disabled}
                  rows={3}
                  className="resize-y min-h-[72px]"
                />
              )}
              {!stepValid && currentAnswer.selectedIds.length === 0 && !allowMultiple && (
                <p className="text-xs text-muted-foreground">{labels.selectOne}</p>
              )}
              {!stepValid && otherSelected && !currentAnswer.otherText.trim() && (
                <p className="text-xs text-muted-foreground">{labels.otherRequired}</p>
              )}
            </>
          )}
        </div>

        <div className="flex items-center gap-2">
          {step > 0 && (
            <Button
              type="button"
              size="sm"
              variant="outline"
              disabled={disabled}
              onClick={() => setStep((s) => s - 1)}
            >
              <ChevronLeft className="mr-1 h-4 w-4" />
              {labels.back}
            </Button>
          )}
          <Button type="button" size="sm" disabled={disabled || !stepValid} onClick={goNext}>
            {isLast ? labels.submit : labels.next}
            {!isLast && <ChevronRight className="ml-1 h-4 w-4" />}
          </Button>
        </div>
      </CardContent>
    </Card>
  );
}
