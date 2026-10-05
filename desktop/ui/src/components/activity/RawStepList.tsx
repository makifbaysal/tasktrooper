import { ChevronDown, ChevronRight } from "lucide-react";
import { useState } from "react";
import type { SessionStep } from "@/api";
import { Button } from "@/components/ui/button";
import { useI18n } from "@/hooks/useI18n";
import { formatClock } from "@/lib/activityFeedLabels";
import { feedPre, feedRowButton } from "./feedStyles";

function RawStepRow({ step }: { step: SessionStep }) {
  const { lang } = useI18n();
  const [open, setOpen] = useState(false);
  const Chevron = open ? ChevronDown : ChevronRight;
  return (
    <li className="min-w-0">
      <Button
        type="button"
        variant="ghost"
        className={feedRowButton}
        aria-expanded={open}
        aria-label={step.step_type}
        onClick={() => setOpen((v) => !v)}
      >
        <span className="shrink-0 tabular-nums text-muted-foreground">{formatClock(step.created_at, lang, true)}</span>
        <span className="min-w-0 flex-1 truncate font-mono">{step.step_type}</span>
        <Chevron aria-hidden className="text-muted-foreground" />
      </Button>
      {open && (
        <pre className={`${feedPre} mx-2 mb-2`}>{JSON.stringify(step.payload ?? {}, null, 2)}</pre>
      )}
    </li>
  );
}

export function RawStepList({ steps }: { steps: SessionStep[] }) {
  const { t } = useI18n();
  if (steps.length === 0) {
    return <p className="px-2 text-caption text-muted-foreground">{t("activityArea.feed.rawEmpty")}</p>;
  }
  return (
    <ul className="max-h-96 divide-y divide-border overflow-y-auto rounded-lg border border-border">
      {steps.map((step) => (
        <RawStepRow key={step.id} step={step} />
      ))}
    </ul>
  );
}
