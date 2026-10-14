import { Button } from "@/components/ui/button";
import { Notice } from "@/components/ui/notice";
import { DesktopOnlyNotice } from "@/components/setup/DesktopOnlyNotice";
import { EnvironmentPreflight } from "@/components/runner/EnvironmentPreflight";
import { ToolchainRunningNotice } from "@/components/toolchain/ToolchainRunningNotice";
import { useI18n } from "@/hooks/useI18n";
import { useSetup } from "@/hooks/useSetup";
import { useToolchain } from "@/hooks/useToolchain";
import type { SetupStepId } from "@/lib/setup";

/**
 * Step 1: the environment preflight.
 *
 * The checklist itself is `EnvironmentPreflight` unchanged — the same
 * component the Claude Code card on Settings → LLM Connection renders, with
 * the same three-state rows and the same remediation text and copyable
 * commands. This wrapper adds only what the sequence needs around it: the
 * verdict in one sentence, and a Continue that appears once the shell says
 * every required item is `ok`.
 *
 * Every report it fetches is pushed into `useSetup` as well, so pressing
 * "Check again" after installing something unlocks step 2 immediately rather
 * than at the next slow poll.
 */
export function EnvironmentStep({ onContinue }: { onContinue: (next: SetupStepId) => void }) {
  const { t } = useI18n();
  const { steps, host, reportEnvironment } = useSetup();
  const { state: toolchainJob } = useToolchain({ report: false });
  const step = steps.environment;

  if (!step.actionable) return <DesktopOnlyNotice />;

  return (
    <div className="space-y-3">
      <p className="text-sm text-muted-foreground">{t("setup.environment.description")}</p>

      <ToolchainRunningNotice state={toolchainJob} />

      <EnvironmentPreflight host={host} onReport={reportEnvironment} />

      {step.state === "unknown" && step.error && (
        <Notice variant="error" title={t("setup.state.unknown")}>
          <p>{step.error}</p>
          <p className="mt-1">{t("setup.unknownHint")}</p>
        </Notice>
      )}

      {step.state === "todo" && (
        <Notice variant="warning" title={t("setup.environment.notReadyTitle")}>
          {t("setup.environment.notReadyBody")}
        </Notice>
      )}

      {step.state === "done" && (
        <>
          <Notice variant="info" title={t("setup.environment.readyTitle")}>
            {t("setup.environment.readyBody")}
          </Notice>
          <Button onClick={() => onContinue("agent")}>{t("setup.environment.continue")}</Button>
        </>
      )}
    </div>
  );
}
