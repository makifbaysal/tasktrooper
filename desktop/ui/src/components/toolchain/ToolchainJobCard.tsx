import { PackageCheck, RotateCcw, StopCircle, X } from "lucide-react";
import { useState } from "react";
import { toast } from "sonner";
import { RestartAgentsDialog } from "@/components/toolchain/RestartAgentsDialog";
import { ToolchainJobDetails } from "@/components/toolchain/ToolchainJobDetails";
import { ToolchainStatusBadge } from "@/components/toolchain/ToolchainStatusBadge";
import { Button } from "@/components/ui/button";
import { Card } from "@/components/ui/card";
import { Label } from "@/components/ui/label";
import { useI18n } from "@/hooks/useI18n";
import { agentRestarter, useRestartPending } from "@/hooks/useToolchain";
import type { ToolchainJobState } from "@/lib/desktop-bridge";
import { jobTitleKey } from "@/lib/toolchainJob";

interface ToolchainJobCardProps {
  state: ToolchainJobState;
  onStop: () => Promise<void>;
  onDismiss: () => Promise<void>;
}

/** Settings → Development environment's view of the running or last job, with its controls. */
export function ToolchainJobCard({ state, onStop, onDismiss }: ToolchainJobCardProps) {
  const { t } = useI18n();
  const [restartOpen, setRestartOpen] = useState(false);
  const [busy, setBusy] = useState(false);
  const running = state.phase === "running";
  const restartPending = useRestartPending(state);
  const canRestart = restartPending && agentRestarter() !== null;

  const act = async (action: () => Promise<void>, failedKey: string) => {
    setBusy(true);
    try {
      await action();
    } catch (e) {
      toast.error(t(failedKey), { description: e instanceof Error ? e.message : undefined });
    } finally {
      setBusy(false);
    }
  };

  return (
    <Card className="w-full space-y-4 p-6" data-testid="toolchain-job-card">
      <div className="flex flex-wrap items-center justify-between gap-2">
        <Label className="flex items-center gap-2">
          <PackageCheck className="h-4 w-4" />
          {t(jobTitleKey(state))}
        </Label>
        <ToolchainStatusBadge state={state} />
      </div>

      <ToolchainJobDetails state={state} restartPending={restartPending} />

      <div className="flex flex-wrap gap-2">
        {running && (
          <Button variant="outline" disabled={busy} onClick={() => void act(onStop, "toolchain.errors.stopFailed")}>
            <StopCircle className="h-4 w-4" />
            {t("toolchain.actions.stop")}
          </Button>
        )}
        {canRestart && (
          <Button onClick={() => setRestartOpen(true)}>
            <RotateCcw className="h-4 w-4" />
            {t("toolchain.actions.restartAgents")}
          </Button>
        )}
        {!running && (
          <Button variant="ghost" disabled={busy} onClick={() => void act(onDismiss, "toolchain.errors.dismissFailed")}>
            <X className="h-4 w-4" />
            {t("toolchain.actions.dismiss")}
          </Button>
        )}
      </div>

      <RestartAgentsDialog open={restartOpen} onOpenChange={setRestartOpen} state={state} />
    </Card>
  );
}
