import { useEffect, useState } from "react";
import { Navigate, useNavigate } from "react-router-dom";
import { SetupShell } from "@/components/setup/SetupShell";
import { AgentRuntimeStep } from "@/components/setup/AgentRuntimeStep";
import { EnvironmentStep } from "@/components/setup/EnvironmentStep";
import { FirstProjectStep } from "@/components/setup/FirstProjectStep";
import { FirstRunChoice } from "@/components/setup/FirstRunChoice";
import { GitHubStep } from "@/components/setup/GitHubStep";
import { TeamTemplateStep } from "@/components/setup/TeamTemplateStep";
import { SETUP_STEP_TITLE_KEY, SetupStepList } from "@/components/setup/SetupStepList";
import { Button } from "@/components/ui/button";
import { Separator } from "@/components/ui/separator";
import { useI18n } from "@/hooks/useI18n";
import { useSetup } from "@/hooks/useSetup";
import { readFirstRunMode, readFirstRunTeamDone, type FirstRunMode } from "@/lib/firstRun";
import { SETUP_STEP_IDS, setupStepUnlocked, type SetupStepId } from "@/lib/setup";

/**
 * `/setup` — the guided first-run sequence, in order and resumable.
 *
 * Four steps, each gated on the previous one having actually succeeded rather
 * than having been visited: the environment preflight, connecting an agent
 * runtime, connecting GitHub, and a first project with a repository in it. Every one of
 * those verdicts is derived in `useSetup` from the thing itself, so a reload,
 * an app restart, or someone disconnecting GitHub next week all land on the
 * right screen with no stored progress to go stale.
 *
 * The environment preflight cannot be performed in a browser at all — it probes
 * this machine through the desktop bridge — so there it says so rather than
 * offering a button that could never work.
 */
export function SetupPage() {
  const { t } = useI18n();
  const navigate = useNavigate();
  const { steps, activeId, complete, dismiss } = useSetup();

  // The two first-run choices, remembered in localStorage and only asked while
  // they are unanswered. They sit in front of the sequence, which means an
  // install that never made them sees them once; an existing install whose
  // steps are already done is redirected home below before either renders.
  const [mode, setMode] = useState<FirstRunMode | null>(() => readFirstRunMode());
  const [teamDone, setTeamDone] = useState(() => readFirstRunTeamDone());

  // What the user is LOOKING at, which is not always where they are: a
  // finished step stays openable so someone can re-run the preflight or swap
  // the GitHub account without leaving the sequence.
  const [selected, setSelected] = useState<SetupStepId | null>(null);
  const shown = selected && setupStepUnlocked(steps, selected) ? selected : (activeId ?? "project");

  // Follow the sequence forward on its own once a step completes in the
  // background — the tunnel attaching, an import finishing — unless the user
  // has deliberately opened an earlier one.
  useEffect(() => {
    if (selected !== null && setupStepUnlocked(steps, selected)) return;
    setSelected(null);
  }, [steps, selected]);

  if (complete) {
    // Nothing left to ask. Someone who typed the URL gets home, which is
    // where the "all set" state of this screen would have sent them anyway.
    return <Navigate to="/home" replace />;
  }

  if (mode === null) {
    return (
      <SetupShell title={t("setup.firstRun.title")} description={t("setup.firstRun.description")}>
        <FirstRunChoice onChosen={setMode} />
      </SetupShell>
    );
  }

  if (!teamDone) {
    return (
      <SetupShell title={t("setup.team.title")} description={t("setup.team.description")}>
        <TeamTemplateStep onDone={() => setTeamDone(true)} />
      </SetupShell>
    );
  }

  const index = SETUP_STEP_IDS.indexOf(shown);

  return (
    <SetupShell title={t("setup.title")} description={t("setup.description")}>
      <SetupStepList steps={steps} selected={shown} onSelect={setSelected} />

      <Separator />

      <div className="space-y-3">
        <div>
          <p className="text-xs font-medium uppercase tracking-wide text-muted-foreground">
            {t("setup.stepLabel", { index: index + 1, total: SETUP_STEP_IDS.length })}
          </p>
          <h2 className="text-base font-semibold">{t(SETUP_STEP_TITLE_KEY[shown])}</h2>
        </div>

        {shown === "environment" && <EnvironmentStep onContinue={setSelected} />}
        {shown === "agent" && <AgentRuntimeStep onContinue={setSelected} />}
        {shown === "github" && <GitHubStep onContinue={setSelected} />}
        {shown === "project" && <FirstProjectStep />}
      </div>

      <Separator />

      {/* The way out. Skipping ahead is allowed — it suppresses the redirect
          that brought them here and nothing else — and the sidebar keeps a
          "Finish setup" link for as long as anything is genuinely undone. */}
      <Button
        variant="ghost"
        className="w-full"
        onClick={() => {
          dismiss();
          navigate("/home");
        }}
      >
        {t("setup.later")}
      </Button>
    </SetupShell>
  );
}
