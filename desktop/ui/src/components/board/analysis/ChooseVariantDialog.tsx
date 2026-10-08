import { CheckCircle2, Loader2 } from "lucide-react";
import { useEffect, useMemo, useState } from "react";
import type { TaskDocument } from "@/api";
import { FormDialog } from "@/components/admin/FormDialog";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Label } from "@/components/ui/label";
import { Textarea } from "@/components/ui/textarea";
import { useI18n } from "@/hooks/useI18n";
import { designDocumentLabel, type DesignVariantGroup } from "@/lib/design-system";
import { cn } from "@/lib/utils";

interface ChooseVariantDialogProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  /** The screens that have more than one variant; at least one. */
  groups: DesignVariantGroup[];
  /** The titles the newest `Chosen variant: ` comments name, one per screen. */
  chosenTitles: string[];
  /** Selected when the dialog opens — the pane whose "Choose" was clicked. */
  initialId: string | null;
  onChoose: (doc: TaskDocument, note: string) => Promise<void>;
}

/**
 * Molecule: the one place a variant is chosen — a screen's variants as the
 * options, grouped by screen when several have alternatives, each screen's
 * current choice marked, and an optional note that travels with the choice
 * (which parts of another variant to keep, say). A screen drawn only once is
 * not offered: there is nothing to choose between.
 */
export function ChooseVariantDialog({
  open,
  onOpenChange,
  groups,
  chosenTitles,
  initialId,
  onChoose,
}: ChooseVariantDialogProps) {
  const { t } = useI18n();
  const [selectedId, setSelectedId] = useState<string | null>(null);
  const [note, setNote] = useState("");
  const [saving, setSaving] = useState(false);

  const documents = useMemo(() => groups.flatMap((group) => group.documents), [groups]);

  useEffect(() => {
    if (!open) return;
    const chosen = documents.find((doc) => chosenTitles.includes(doc.title.trim()));
    setSelectedId(initialId ?? chosen?.id ?? null);
    setNote("");
  }, [open, initialId, chosenTitles, documents]);

  const selected = documents.find((doc) => doc.id === selectedId) ?? null;
  const grouped = groups.length > 1;

  const confirm = async () => {
    if (!selected) return;
    setSaving(true);
    try {
      await onChoose(selected, note.trim());
      onOpenChange(false);
    } catch {
      // The caller reported it; the dialog stays open with the choice and note.
    } finally {
      setSaving(false);
    }
  };

  return (
    <FormDialog
      open={open}
      onOpenChange={(next) => !saving && onOpenChange(next)}
      title={t("analysisReview.design.choice.dialogTitle")}
      description={t("analysisReview.design.choice.dialogBody")}
      footer={
        <>
          <Button variant="outline" onClick={() => onOpenChange(false)} disabled={saving}>
            {t("common.cancel")}
          </Button>
          <Button onClick={() => void confirm()} disabled={saving || !selected}>
            {saving ? <Loader2 className="animate-spin" /> : <CheckCircle2 />}
            {selected
              ? t("analysisReview.design.choice.confirm", { title: designDocumentLabel(selected) })
              : t("analysisReview.design.choice.open")}
          </Button>
        </>
      }
    >
      <div className="space-y-4">
        <div role="radiogroup" aria-label={t("analysisReview.design.choice.dialogTitle")} className="grid gap-2">
          {groups.map((group) => (
            <div key={group.screen} role="group" aria-label={group.screen} className="grid gap-2">
              {grouped && <p className="pt-1 text-caption font-medium text-muted-foreground">{group.screen}</p>}
              {group.documents.map((doc) => {
                const checked = doc.id === selectedId;
                const current = chosenTitles.includes(doc.title.trim());
                return (
                  <Button
                    key={doc.id}
                    role="radio"
                    aria-checked={checked}
                    variant="outline"
                    onClick={() => setSelectedId(doc.id)}
                    disabled={saving}
                    className={cn(
                      "h-auto justify-between gap-3 whitespace-normal py-2.5 text-left",
                      checked && "border-primary bg-primary/5 ring-2 ring-primary/20",
                    )}
                  >
                    <span className="min-w-0">{designDocumentLabel(doc)}</span>
                    {current && (
                      <Badge variant="success" className="shrink-0">
                        {t("analysisReview.design.compare.chosen")}
                      </Badge>
                    )}
                  </Button>
                );
              })}
            </div>
          ))}
        </div>
        <div className="space-y-2">
          <Label htmlFor="design-choice-note">{t("analysisReview.design.choice.noteLabel")}</Label>
          <Textarea
            id="design-choice-note"
            value={note}
            onChange={(e) => setNote(e.target.value)}
            placeholder={t("analysisReview.design.choice.notePlaceholder")}
            rows={3}
            disabled={saving}
          />
        </div>
      </div>
    </FormDialog>
  );
}
