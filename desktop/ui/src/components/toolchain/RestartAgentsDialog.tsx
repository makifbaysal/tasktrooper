import { useState } from "react";
import { toast } from "sonner";
import { ConfirmDialog } from "@/components/ui/confirm-dialog";
import { agentRestarter, markAgentsRestarted } from "@/hooks/useToolchain";
import { useI18n } from "@/hooks/useI18n";
import type { ToolchainJobState } from "@/lib/desktop-bridge";

interface RestartAgentsDialogProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  /** The job the restart is for; once it goes through, that job stops asking for one. */
  state: ToolchainJobState;
}

/**
 * Confirms before restarting what runs the agents: it interrupts a run in
 * progress, so a stray click on "Restart agents" must not do it.
 */
export function RestartAgentsDialog({ open, onOpenChange, state }: RestartAgentsDialogProps) {
  const { t } = useI18n();
  const [restarting, setRestarting] = useState(false);

  const restart = async () => {
    const run = agentRestarter();
    if (!run) return;
    setRestarting(true);
    // Marked before the call: a local-mode restart reloads this window, often
    // before the call itself answers.
    const undo = markAgentsRestarted(state);
    try {
      await run();
    } catch (e) {
      undo();
      toast.error(t("toolchain.restart.failed"), { description: e instanceof Error ? e.message : undefined });
    } finally {
      setRestarting(false);
    }
  };

  return (
    <ConfirmDialog
      open={open}
      onOpenChange={onOpenChange}
      title={t("toolchain.restart.title")}
      description={t("toolchain.restart.description")}
      confirmLabel={t("toolchain.restart.confirm")}
      variant="default"
      loading={restarting}
      onConfirm={restart}
    />
  );
}
