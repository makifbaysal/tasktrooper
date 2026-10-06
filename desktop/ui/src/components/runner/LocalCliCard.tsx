import { Loader2, Plug, Unplug } from "lucide-react";
import type { AgentCLIFlavor, AgentCLIState, LLMProviderView } from "@/api";
import { IntegrationCard } from "@/components/admin/IntegrationCard";
import { LLMProviderIcon } from "@/components/admin/LLMProviderIcon";
import { Button } from "@/components/ui/button";
import { useI18n } from "@/hooks/useI18n";
import type { DesktopBlocker, DesktopRunnerHost, DesktopRunnerSnapshot } from "@/lib/desktop-bridge";
import { BlockerNotice } from "./BlockerNotice";
import type { ConnectStep } from "./claudeCodeConnect";

/**
 * One host-executed provider — Claude Code, Cursor — as a card you can act on.
 *
 * This card replaced the old Settings → Local Runner page. The reason is that
 * the page and the card were always two halves of one question: "Connect" here
 * could only ever succeed if a Mac was attached over there, and nothing on
 * either screen said so. Now one button does the whole thing. The machinery
 * behind it (processes, logs, diagnostics, machine settings) is deliberately
 * NOT shown any more — nor the binary path or the catalog snapshot: none of it
 * was something a person could act on from here, so it only ever read as noise
 * next to the one button that matters. The single exception is a blocker,
 * which ships its own install command.
 *
 * Connect is one call to the local server on either surface — in the desktop
 * shell that server is a child process the app already started, and in a
 * browser it is whichever local server this build points at. The only thing
 * that differs is the environment preflight, which only the shell can probe.
 */
export interface LocalCliCardProps {
  view: LLMProviderView;
  /** Null when the server declares no CLI flavor for this provider. */
  flavor: AgentCLIFlavor | null;
  available: boolean;
  cliState: AgentCLIState | null;
  /** This card's own flow is running. */
  busy: boolean;
  /**
   * Any card's flow is running. Connect/Disconnect on every OTHER card is
   * disabled meanwhile — not because the flavors interact any more (they do
   * not: connecting one no longer touches another), but because the desktop
   * shell supervises one flow at a time and a second press mid-flow would
   * have nothing to act on yet.
   */
  anyBusy: boolean;
  /** Where the desktop flow is, for the narration line. Null in a browser. */
  step: ConnectStep | null;
  /** The server's own sentence about the last failure, kept on the card. */
  error: string;
  /** A missing required tool, with the exact command that installs it. */
  blocker?: DesktopBlocker;
  host: DesktopRunnerHost | null;
  snapshot: DesktopRunnerSnapshot | null;
  onConnect: () => void;
  onDisconnect: () => void;
  className?: string;
  /**
   * The label of the required preflight item currently failing, if any.
   * Connect stays visible but disabled while this is set, and the card says
   * which item is blocking it rather than just going grey.
   */
  environmentBlockingLabel?: string;
}

export function LocalCliCard({
  view,
  flavor,
  available,
  cliState,
  busy,
  anyBusy,
  step,
  error,
  blocker,
  host,
  snapshot,
  onConnect,
  onDisconnect,
  className,
  environmentBlockingLabel,
}: LocalCliCardProps) {
  const { t } = useI18n();
  const envBlocked = Boolean(environmentBlockingLabel);

  // This card's OWN connection, if any — a different flavor being connected
  // has no bearing on this one any more (see migrations/120_agent_cli_multi_connection).
  const myConnection = flavor !== null ? (cliState?.connections?.find((c) => c.flavor === flavor) ?? null) : null;
  const cliUp = myConnection !== null;

  // The row IS the connection now: the server that holds it is the one running
  // on this machine, so there is no second question about whether a machine is
  // still behind it.
  const connected = cliUp;

  const canDisconnect = available && cliUp;
  const canConnect = !connected;

  // The blocker the flow captured belongs to the press the user just made; the
  // snapshot's belongs to whatever failed last — an auto-connect at launch, say,
  // which nobody was watching. Without the fallback that failure is silent: a
  // grey badge, no sentence, and the one error in the product that ships with
  // its own install command nowhere on screen until Connect is pressed again.
  const hostFailed = host !== null && available && snapshot?.phase === "failed";
  const shownBlocker = blocker ?? (hostFailed ? snapshot?.blocker : undefined);
  const shownError = error || (hostFailed && !blocker ? (snapshot?.detail ?? "") : "");

  const status = !available
    ? { tone: "idle" as const, label: t("settingsPages.llm.badgeComingSoon") }
    : connected
      ? { tone: "connected" as const, label: t("settingsPages.llm.badgeConnected") }
      : { tone: "idle" as const, label: t("settingsPages.llm.badgeNotConnected") };

  const showStep = busy && step !== null;
  const showEnvBlocked = canConnect && envBlocked && available;
  const hasBody = showStep || Boolean(shownBlocker) || Boolean(shownError) || showEnvBlocked;

  return (
    <IntegrationCard
      className={className}
      icon={<LLMProviderIcon type={view.definition.type} />}
      name={view.definition.label}
      {...(connected && myConnection?.binary_version ? { description: myConnection.binary_version } : {})}
      status={status}
      actions={
        <>
          {/* Re-pressing Connect is safe: the CLI connect is idempotent. */}
          {canConnect && available && (
            <Button
              size="sm"
              onClick={onConnect}
              // A failing required preflight item disables it rather than
              // hiding it: the line under the card says why.
              disabled={flavor === null || anyBusy || envBlocked}
            >
              {busy ? <Loader2 className="mr-1.5 h-3.5 w-3.5 animate-spin" /> : <Plug className="mr-1.5 h-3.5 w-3.5" />}
              {busy ? t("settingsPages.llm.cliConnecting") : t("settingsPages.llm.connect")}
            </Button>
          )}
          {canDisconnect && (
            <Button size="sm" variant="outline" onClick={onDisconnect} disabled={anyBusy}>
              {busy ? <Loader2 className="mr-1.5 h-3.5 w-3.5 animate-spin" /> : <Unplug className="mr-1.5 h-3.5 w-3.5" />}
              {t("settingsPages.llm.disconnectShort")}
            </Button>
          )}
        </>
      }
    >
      {hasBody ? (
        <>
          {/* The flow takes tens of seconds and silence reads as a hang. */}
          {showStep && step !== null && (
            <p className="flex items-center gap-2 text-caption text-muted-foreground">
              <Loader2 className="h-3.5 w-3.5 shrink-0 animate-spin" aria-hidden />
              {connectStepLine(step, t)}
            </p>
          )}
          {/* A blocker is the only failure that comes with its own fix: the
              command in full, never run for the user. */}
          {shownBlocker && <BlockerNotice blocker={shownBlocker} />}
          {/* The server's own sentence: only it knows whether the binary is
              missing or signed out, and the two are fixed in different places. */}
          {shownError && <p className="rounded-md bg-destructive/10 p-2 text-caption text-destructive">{shownError}</p>}
          {showEnvBlocked && (
            <p className="text-caption text-warning">
              {t("settingsPages.llm.claudeCode.preflight.blocking", { item: environmentBlockingLabel ?? "" })}
            </p>
          )}
        </>
      ) : undefined}
    </IntegrationCard>
  );
}

/**
 * One line for the step the flow is on.
 *
 * Exported because the guided setup narrates the SAME flow and a second set of
 * sentences for it would drift from these the first time one is reworded.
 */
export function connectStepLine(
  step: ConnectStep,
  t: (key: string, params?: Record<string, string | number>) => string,
): string {
  const k = (leaf: string) => t(`settingsPages.llm.claudeCode.${leaf}`);
  switch (step) {
    case "installing":
      return k("stepInstalling");
    case "disconnecting-cli":
      return k("stepDisconnectingCli");
  }
}
