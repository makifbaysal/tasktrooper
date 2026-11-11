import { FileText, GitCompareArrows, MessageSquareText, MessagesSquare } from "lucide-react";
import type { ComponentType } from "react";
import { STUDY_KINDS, type StudyKind } from "@/api";
import { Card } from "@/components/ui/card";
import { useI18n } from "@/hooks/useI18n";
import { cn } from "@/lib/utils";

const ICONS: Record<StudyKind, ComponentType<{ className?: string }>> = {
  user_interviews: MessagesSquare,
  landing_page_test: FileText,
  message_testing: MessageSquareText,
  ab_test: GitCompareArrows,
};

interface StudyKindCardsProps {
  value: StudyKind | null;
  onChange: (kind: StudyKind) => void;
}

/** The four kinds of study, one card each; exactly one can be chosen. */
export function StudyKindCards({ value, onChange }: StudyKindCardsProps) {
  const { t } = useI18n();
  return (
    <div role="radiogroup" aria-label={t("audienceStudy.context.pickKind")} className="grid gap-3 sm:grid-cols-2 xl:grid-cols-4">
      {STUDY_KINDS.map((kind) => {
        const Icon = ICONS[kind];
        const selected = value === kind;
        return (
          <button
            key={kind}
            type="button"
            role="radio"
            aria-checked={selected}
            onClick={() => onChange(kind)}
            className="rounded-xl text-left focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
          >
            <Card className={cn("flex h-full flex-col gap-2 p-4 transition-colors hover:bg-accent/30", selected && "border-primary bg-primary/5 ring-1 ring-primary")}>
              <Icon className="h-5 w-5 text-primary" aria-hidden />
              <span className="font-semibold">{t(`audienceStudy.kinds.${kind}.title`)}</span>
              <span className="text-caption text-muted-foreground">{t(`audienceStudy.kinds.${kind}.body`)}</span>
            </Card>
          </button>
        );
      })}
    </div>
  );
}
