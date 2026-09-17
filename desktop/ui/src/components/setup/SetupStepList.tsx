import { Check, HelpCircle, Laptop, Lock } from "lucide-react";
import { Badge } from "@/components/ui/badge";
import { useI18n } from "@/hooks/useI18n";
import { SETUP_STEP_IDS, setupStepUnlocked, type SetupStepId, type SetupSteps } from "@/lib/setup";
import { cn } from "@/lib/utils";

interface SetupStepListProps {
  steps: SetupSteps;
  selected: SetupStepId;
  onSelect: (id: SetupStepId) => void;
}

/** Title key per step, so the list and the bodies cannot name them differently. */
export const SETUP_STEP_TITLE_KEY: Record<SetupStepId, string> = {
  environment: "setup.environment.title",
  agent: "setup.agent.title",
  github: "setup.github.title",
  project: "setup.project.title",
};

/**
 * The four steps as a list you can see your way down, with each one's real
 * state next to it.
 *
 * A locked step is shown, not hidden: the point of the sequence is that a new
 * person can see what is coming and roughly how far there is to go. What
 * locking buys is that they cannot start step 4 before step 2 has produced the
 * Mac that step 4's calls travel through.
 */
export function SetupStepList({ steps, selected, onSelect }: SetupStepListProps) {
  const { t } = useI18n();

  return (
    <ol className="divide-y divide-border rounded-lg border border-border">
      {SETUP_STEP_IDS.map((id, index) => {
        const step = steps[id];
        const unlocked = setupStepUnlocked(steps, id);
        const active = selected === id;
        const done = step.state === "done";
        return (
          // The badge is a sibling of the button rather than its child: it
          // renders a <div>, which a <button> may not contain.
          <li
            key={id}
            className={cn(
              "flex items-center gap-2 pr-3 transition-colors",
              active && "bg-muted/60",
              unlocked ? "hover:bg-muted/40" : "opacity-60",
            )}
          >
            <button
              type="button"
              onClick={() => onSelect(id)}
              disabled={!unlocked}
              className={cn(
                "flex min-w-0 flex-1 items-center gap-3 px-3 py-2.5 text-left",
                !unlocked && "cursor-not-allowed",
              )}
            >
              <span
                className={cn(
                  "flex h-7 w-7 shrink-0 items-center justify-center rounded-full border-2 text-xs font-semibold",
                  done
                    ? "border-success bg-success/15 text-success"
                    : active
                      ? "border-primary bg-primary/10 text-primary"
                      : "border-muted-foreground/30 text-muted-foreground",
                )}
              >
                {done ? <Check className="h-3.5 w-3.5" /> : !unlocked ? <Lock className="h-3 w-3" /> : index + 1}
              </span>
              <span
                className={cn(
                  "min-w-0 flex-1 truncate text-sm font-medium",
                  active ? "text-foreground" : "text-muted-foreground",
                )}
              >
                {t(SETUP_STEP_TITLE_KEY[id])}
              </span>
            </button>
            <SetupStateBadge steps={steps} id={id} />
          </li>
        );
      })}
    </ol>
  );
}

/**
 * One step's state as a pill.
 *
 * Four readings, not two, and the extra pair is the honest part: "couldn't
 * check" is never dressed up as "to do", and a step this surface cannot
 * perform says so rather than sitting there greyed out with no explanation.
 */
export function SetupStateBadge({ steps, id }: { steps: SetupSteps; id: SetupStepId }) {
  const { t } = useI18n();
  const step = steps[id];

  if (step.state === "done") {
    return (
      <Badge variant="success" className="shrink-0 whitespace-nowrap">
        {t("setup.state.done")}
      </Badge>
    );
  }
  if (!setupStepUnlocked(steps, id)) {
    return (
      <Badge variant="outline" className="shrink-0 whitespace-nowrap">
        {t("setup.state.locked")}
      </Badge>
    );
  }
  if (step.state === "unknown") {
    return (
      <Badge variant="outline" className="shrink-0 gap-1 whitespace-nowrap">
        <HelpCircle className="h-3 w-3" aria-hidden />
        {t("setup.state.unknown")}
      </Badge>
    );
  }
  if (!step.actionable) {
    return (
      <Badge variant="outline" className="shrink-0 gap-1 whitespace-nowrap">
        <Laptop className="h-3 w-3" aria-hidden />
        {t("setup.state.desktopOnly")}
      </Badge>
    );
  }
  return (
    <Badge variant="secondary" className="shrink-0 whitespace-nowrap">
      {t("setup.state.todo")}
    </Badge>
  );
}
