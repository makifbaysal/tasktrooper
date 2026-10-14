import { RotateCcw, StopCircle, Wrench, X } from "lucide-react";
import { useState } from "react";
import { useNavigate } from "react-router-dom";
import { toast } from "sonner";
import { RestartAgentsDialog } from "@/components/toolchain/RestartAgentsDialog";
import { ToolchainJobDetails } from "@/components/toolchain/ToolchainJobDetails";
import { ToolchainStatusBadge } from "@/components/toolchain/ToolchainStatusBadge";
import { Button } from "@/components/ui/button";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import { useI18n } from "@/hooks/useI18n";
import { agentRestarter, useRestartPending, useToolchain } from "@/hooks/useToolchain";
import type { ToolchainJobState } from "@/lib/desktop-bridge";
import { TOOLCHAIN_SETTINGS_PATH, jobTitleKey } from "@/lib/toolchainJob";

/**
 * The header's chip for the background toolchain install, beside the
 * notification bell. Nothing while idle or outside the desktop app; a finished
 * job stays until dismissed, so a failure is not missed by someone who looked
 * away while it ran.
 */
export function ToolchainIndicator() {
  const { available, state, cancel, dismiss } = useToolchain({ report: false });
  if (!available || !state || state.phase === "idle") return null;
  return <ToolchainIndicatorMenu state={state} onStop={cancel} onDismiss={dismiss} />;
}

interface ToolchainIndicatorMenuProps {
  state: ToolchainJobState;
  onStop: () => Promise<void>;
  onDismiss: () => Promise<void>;
}

function ToolchainIndicatorMenu({ state, onStop, onDismiss }: ToolchainIndicatorMenuProps) {
  const { t } = useI18n();
  const navigate = useNavigate();
  const [restartOpen, setRestartOpen] = useState(false);
  const running = state.phase === "running";
  const restartPending = useRestartPending(state);
  const canRestart = restartPending && agentRestarter() !== null;

  const act = (action: () => Promise<void>, failedKey: string) => () => {
    action().catch((e: unknown) => {
      toast.error(t(failedKey), { description: e instanceof Error ? e.message : undefined });
    });
  };

  return (
    <>
      <DropdownMenu>
        <DropdownMenuTrigger asChild>
          <Button
            variant="ghost"
            size="sm"
            className="h-auto px-1.5 py-1"
            title={t("frame.layout.header.toolchain.title")}
            data-testid="toolchain-indicator"
          >
            <ToolchainStatusBadge state={state} />
          </Button>
        </DropdownMenuTrigger>
        <DropdownMenuContent align="end" sideOffset={8} className="w-96 max-w-[calc(100vw-2rem)] p-0">
          <div className="space-y-3 px-3 pb-2 pt-3">
            <DropdownMenuLabel className="p-0 text-sm text-foreground">
              {t(jobTitleKey(state))}
            </DropdownMenuLabel>
            <ToolchainJobDetails state={state} restartPending={restartPending} listClassName="max-h-72 overflow-y-auto pr-1" />
          </div>
          <DropdownMenuSeparator className="m-0" />
          <div className="p-1">
            {running && (
              <DropdownMenuItem onSelect={act(onStop, "toolchain.errors.stopFailed")}>
                <StopCircle aria-hidden className="mr-2 h-4 w-4" />
                {t("toolchain.actions.stop")}
              </DropdownMenuItem>
            )}
            {canRestart && (
              <DropdownMenuItem onSelect={() => setRestartOpen(true)}>
                <RotateCcw aria-hidden className="mr-2 h-4 w-4" />
                {t("toolchain.actions.restartAgents")}
              </DropdownMenuItem>
            )}
            <DropdownMenuItem onSelect={() => navigate(TOOLCHAIN_SETTINGS_PATH)}>
              <Wrench aria-hidden className="mr-2 h-4 w-4" />
              {t("toolchain.actions.details")}
            </DropdownMenuItem>
            {!running && (
              <DropdownMenuItem onSelect={act(onDismiss, "toolchain.errors.dismissFailed")}>
                <X aria-hidden className="mr-2 h-4 w-4" />
                {t("toolchain.actions.dismiss")}
              </DropdownMenuItem>
            )}
          </div>
        </DropdownMenuContent>
      </DropdownMenu>
      <RestartAgentsDialog open={restartOpen} onOpenChange={setRestartOpen} state={state} />
    </>
  );
}
