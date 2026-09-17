import { Loader2, Plug, Terminal, Unplug } from "lucide-react";
import type { AgentCLIFlavor, AgentCLIState, LLMProviderView } from "@/api";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card } from "@/components/ui/card";
import { useI18n } from "@/hooks/useI18n";
import type { DesktopBlocker, DesktopRunnerHost, DesktopRunnerSnapshot } from "@/lib/desktop-bridge";
import { cn } from "@/lib/utils";
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
 * NOT shown any more: none of it was something a person could act on from
 * here, so it only ever read as noise next to the one button that matters.
 * The single exception is a blocker, which ships its own install command.
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
  /** The provider's accent tint, applied only once it is actually usable. */
  accentClassName?: string;
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
  accentClassName,
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

  return (
    <Card className={cn("flex flex-col border p-4", available && accentClassName, className)}>
      <div className="mb-3 flex items-start justify-between gap-2">
        <div className="min-w-0">
          <div className="flex items-center gap-2">
            <Terminal
              className={cn("h-4 w-4 shrink-0", available ? "text-foreground" : "text-muted-foreground")}
            />
            <h3 className="font-semibold">{view.definition.label}</h3>
          </div>
        </div>
        {!available ? (
          <Badge variant="outline" className="whitespace-nowrap">
            {t("settingsPages.llm.badgeComingSoon")}
          </Badge>
        ) : connected ? (
          <Badge variant="success" className="whitespace-nowrap">
            {t("settingsPages.llm.badgeConnected")}
          </Badge>
        ) : (
          <Badge variant="outline" className="whitespace-nowrap">
            {t("settingsPages.llm.badgeNotConnected")}
          </Badge>
        )}
      </div>

      {!available && (
        <p className="mb-3 rounded-md bg-muted/40 p-2 text-xs text-muted-foreground">
          {t("settingsPages.llm.cliComingSoonHint")}
        </p>
      )}

      {/* What the connect actually did. Without it "Bağlı" is a claim; with it
          the user can see the version that answered and how much catalog was
          installed. */}
      {connected && myConnection && (
        <div className="mb-3 space-y-1 rounded-md bg-muted/40 p-2 text-xs text-muted-foreground">
          <p className="truncate">
            <span className="font-medium text-foreground">{t("settingsPages.llm.cliBinaryLabel")}</span>{" "}
            {myConnection.binary_path}
            {myConnection.binary_version ? ` · ${myConnection.binary_version}` : ""}
          </p>
          <p>
            <span className="font-medium text-foreground">{t("settingsPages.llm.cliInstalledLabel")}</span>{" "}
            {t("settingsPages.llm.cliInstalledValue", {
              agents: myConnection.agent_count,
              skills: myConnection.skill_count,
            })}
          </p>
          {myConnection.catalog_path && (
            <>
              <p className="truncate">
                <span className="font-medium text-foreground">{t("settingsPages.llm.cliCatalogLabel")}</span>{" "}
                {myConnection.catalog_path}
              </p>
              <p>{t("settingsPages.llm.cliCatalogHint")}</p>
            </>
          )}
        </div>
      )}

      {/* The flow takes tens of seconds and silence reads as a hang. While the
          supervisor is starting up it narrates itself far better than we can
          ("starting agent-server…"), so its own line wins over ours. */}
      {busy && step !== null && (
        <p className="mb-3 flex items-center gap-2 text-xs text-muted-foreground">
          <Loader2 className="h-3.5 w-3.5 shrink-0 animate-spin" aria-hidden />
          {connectStepLine(step, t)}
        </p>
      )}

      {/* A blocker is the only failure that comes with its own fix. It gets the
          full treatment — the command in full, never run for the user. */}
      {shownBlocker && <BlockerNotice blocker={shownBlocker} />}

      {/* The server's own sentence. It is the only text that knows whether the
          binary is missing or signed out, and the two are fixed in different
          places. */}
      {shownError && (
        <p className="mb-3 mt-3 rounded-md bg-destructive/10 p-2 text-xs text-destructive">
          {shownError}
        </p>
      )}

      <div className="mt-auto flex flex-wrap gap-2">
        {/* Re-pressing Connect is safe: the CLI connect is idempotent. */}
        {canConnect && (
          <div className="flex flex-col gap-1.5">
            <Button
              size="sm"
              variant="default"
              onClick={onConnect}
              // Disabled rather than absent for a CLI that is only "Coming
              // soon": there the greyed button IS the answer. A failing
              // required preflight item disables it the same way — visible,
              // not absent, because the caption below it is what says why.
              disabled={!available || flavor === null || anyBusy || envBlocked}
            >
              {busy ? (
                <Loader2 className="mr-1.5 h-3.5 w-3.5 animate-spin" />
              ) : (
                <Plug className="mr-1.5 h-3.5 w-3.5" />
              )}
              {busy ? t("settingsPages.llm.cliConnecting") : t("settingsPages.llm.connect")}
            </Button>
            {envBlocked && available && (
              <p className="text-xs text-warning">
                {t("settingsPages.llm.claudeCode.preflight.blocking", { item: environmentBlockingLabel ?? "" })}
              </p>
            )}
          </div>
        )}

        {canDisconnect && (
          <Button size="sm" variant="outline" onClick={onDisconnect} disabled={anyBusy}>
            {busy ? (
              <Loader2 className="mr-1.5 h-3.5 w-3.5 animate-spin" />
            ) : (
              <Unplug className="mr-1.5 h-3.5 w-3.5" />
            )}
            {t("settingsPages.llm.disconnectShort")}
          </Button>
        )}
      </div>

    </Card>
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
