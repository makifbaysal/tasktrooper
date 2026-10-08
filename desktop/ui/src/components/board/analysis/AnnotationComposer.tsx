import { Loader2 } from "lucide-react";
import { useState } from "react";
import { ANNOTATION_BODY_MAX, ANNOTATION_QUOTE_MAX } from "@/api";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card } from "@/components/ui/card";
import { Label } from "@/components/ui/label";
import { Textarea } from "@/components/ui/textarea";
import { useI18n } from "@/hooks/useI18n";

interface AnnotationComposerProps {
  quote: string;
  /** The variant the passage is in, when the panel lists more than one document. */
  label?: string;
  saving: boolean;
  onSave: (body: string) => void;
  onCancel: () => void;
}

export function AnnotationComposer({ quote, label, saving, onSave, onCancel }: AnnotationComposerProps) {
  const { t } = useI18n();
  const [body, setBody] = useState("");
  const trimmed = body.trim();
  const quoteTooLong = quote.length > ANNOTATION_QUOTE_MAX;

  const save = () => {
    if (!trimmed || quoteTooLong || saving) return;
    onSave(trimmed);
  };

  return (
    <Card className="space-y-3 border-primary/60 p-3">
      <div className="flex flex-wrap items-center justify-between gap-2">
        <Label htmlFor="analysis-annotation-body" className="text-caption text-muted-foreground">
          {t("analysisReview.panel.composerTitle")}
        </Label>
        {label && (
          <Badge variant="outline" className="max-w-40 truncate">
            {label}
          </Badge>
        )}
      </div>
      <blockquote className="line-clamp-4 border-l-2 border-primary/60 pl-2 text-caption italic text-muted-foreground">
        {quote}
      </blockquote>
      {quoteTooLong && (
        <p className="text-caption text-destructive">
          {t("analysisReview.panel.quoteTooLong", { max: ANNOTATION_QUOTE_MAX })}
        </p>
      )}
      <Textarea
        id="analysis-annotation-body"
        value={body}
        onChange={(e) => setBody(e.target.value)}
        onKeyDown={(e) => {
          if (e.key === "Enter" && (e.metaKey || e.ctrlKey)) {
            e.preventDefault();
            save();
          }
        }}
        placeholder={t("analysisReview.panel.composerPlaceholder")}
        maxLength={ANNOTATION_BODY_MAX}
        rows={4}
        disabled={saving}
        autoFocus
      />
      <div className="flex justify-end gap-2">
        <Button variant="ghost" size="sm" onClick={onCancel} disabled={saving}>
          {t("common.cancel")}
        </Button>
        <Button size="sm" onClick={save} disabled={saving || !trimmed || quoteTooLong}>
          {saving && <Loader2 className="animate-spin" />}
          {t("analysisReview.panel.add")}
        </Button>
      </div>
    </Card>
  );
}
