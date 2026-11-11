import { Loader2, Plus, X } from "lucide-react";
import { useEffect, useState } from "react";
import { toast } from "sonner";
import { api, type AudienceStudy } from "@/api";
import { FormDialog } from "@/components/admin/FormDialog";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Notice } from "@/components/ui/notice";
import { useI18n } from "@/hooks/useI18n";
import { parsePrices } from "@/lib/audienceHub";

const MAX_QUESTIONS = 5;

interface StudyPlanDialogProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  audienceId: string;
  /** People in the audience: the study asks every one of them. */
  personaCount: number;
  /** A planned study being edited, or null to plan a new one. */
  study?: AudienceStudy | null;
  onSaved: (study: AudienceStudy) => void;
}

/**
 * Plans a study: the extra questions and the prices every person reacts to.
 * Every person is always asked the core purchase questions; saving the plan
 * asks nobody and spends nothing.
 */
export function StudyPlanDialog({ open, onOpenChange, audienceId, personaCount, study = null, onSaved }: StudyPlanDialogProps) {
  const { t } = useI18n();
  const p = (key: string, params?: Record<string, string | number>) => t(`audienceHub.plan.${key}`, params);
  const [title, setTitle] = useState("");
  const [goal, setGoal] = useState("");
  const [questions, setQuestions] = useState<string[]>([]);
  const [prices, setPrices] = useState("");
  const [currency, setCurrency] = useState("USD");
  const [saving, setSaving] = useState(false);

  useEffect(() => {
    if (!open) return;
    setTitle(study?.title ?? "");
    setGoal(study?.goal ?? "");
    setQuestions(study?.questions ?? []);
    setPrices(study?.price_points?.join(", ") ?? "");
    setCurrency(study?.currency ?? "USD");
  }, [open, study]);

  const save = async () => {
    setSaving(true);
    const input = {
      title: title.trim(),
      goal: goal.trim(),
      questions: questions.map((q) => q.trim()).filter(Boolean),
      price_points: parsePrices(prices),
      currency: currency.trim().toUpperCase() || "USD",
    };
    try {
      const saved = study ? await api.updateAudienceStudy(study.id, input) : await api.planAudienceStudy(audienceId, input);
      toast.success(study ? p("updated") : p("planned"));
      onOpenChange(false);
      onSaved(saved);
    } catch (e) {
      toast.error(e instanceof Error ? e.message : t("common.saveFailed"));
    } finally {
      setSaving(false);
    }
  };

  return (
    <FormDialog
      open={open}
      onOpenChange={onOpenChange}
      title={study ? p("editTitle") : p("newTitle")}
      description={p("intro")}
      className="sm:max-w-xl"
      footer={
        <>
          <Button variant="outline" onClick={() => onOpenChange(false)}>
            {t("common.cancel")}
          </Button>
          <Button onClick={() => void save()} disabled={saving}>
            {saving && <Loader2 className="animate-spin" />}
            {study ? t("common.save") : p("plan")}
          </Button>
        </>
      }
    >
      <div className="max-h-[60vh] space-y-4 overflow-y-auto pr-1">
        <Notice variant="info" title={p("coreTitle")} role={null}>
          {p("coreBody")}
        </Notice>
        <div className="space-y-2">
          <Label htmlFor="study-title">{p("title")}</Label>
          <Input id="study-title" value={title} onChange={(e) => setTitle(e.target.value)} placeholder={p("titlePlaceholder")} autoFocus />
        </div>
        <div className="space-y-2">
          <Label htmlFor="study-goal">{p("goal")}</Label>
          <Input id="study-goal" value={goal} onChange={(e) => setGoal(e.target.value)} placeholder={p("goalPlaceholder")} />
        </div>
        <div className="grid grid-cols-[1fr_96px] gap-3">
          <div className="space-y-2">
            <Label htmlFor="study-prices">{p("prices")}</Label>
            <Input id="study-prices" value={prices} onChange={(e) => setPrices(e.target.value)} placeholder="9, 19, 49" inputMode="decimal" />
            <p className="text-caption text-muted-foreground">{p("pricesHelp")}</p>
          </div>
          <div className="space-y-2">
            <Label htmlFor="study-currency">{p("currency")}</Label>
            <Input id="study-currency" value={currency} onChange={(e) => setCurrency(e.target.value)} maxLength={3} />
          </div>
        </div>
        <fieldset className="space-y-2">
          <legend className="text-body font-medium">{p("questions")}</legend>
          <p className="text-caption text-muted-foreground">{p("questionsHelp", { max: MAX_QUESTIONS })}</p>
          {questions.map((q, i) => (
            <div key={i} className="flex gap-2">
              <Input aria-label={p("question")} value={q} onChange={(e) => setQuestions((prev) => prev.map((x, j) => (j === i ? e.target.value : x)))} />
              <Button variant="ghost" size="icon" onClick={() => setQuestions((prev) => prev.filter((_, j) => j !== i))} aria-label={p("removeQuestion")}>
                <X />
              </Button>
            </div>
          ))}
          {questions.length < MAX_QUESTIONS && (
            <Button variant="outline" size="sm" onClick={() => setQuestions((prev) => [...prev, ""])}>
              <Plus />
              {p("addQuestion")}
            </Button>
          )}
        </fieldset>
        <p className="text-caption text-muted-foreground">{p("cost", { calls: personaCount + 1, people: personaCount })}</p>
      </div>
    </FormDialog>
  );
}
