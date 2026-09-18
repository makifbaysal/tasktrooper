import { useCallback, useEffect, useState } from "react";
import { AlertTriangle, ArrowUpCircle, Loader2, RefreshCw, WifiOff } from "lucide-react";
import type { AppInfo, CloudStatus, SupervisorSnapshot, UpdateStatus } from "@ipc/types.js";
import { Button } from "@shared/ui/button.js";
import { StatusDot, type Tone } from "@shared/components/StatusDot.js";
import { api } from "./bridge";
import { supervisorLabel, unreachableCopy } from "./copy";

// Only macOS draws traffic lights inside the window, over the title bar.
const IS_MAC = navigator.userAgent.includes("Macintosh");

/**
 * The shell: a title bar, and a hole where the product is.
 *
 * There is no tab strip and there are no local pages. The bundled web app is
 * the entire visible product — Board, Agents, Settings, and the Claude Code
 * card where this Mac's own machinery is driven — and it is composited on top
 * of this window's contents by the main process. So the React tree here draws
 * exactly three things:
 *
 *  - the title bar, which exists because macOS's traffic lights need somewhere
 *    to sit and because the local server's state should be glanceable from any
 *    screen without going and looking for it;
 *  - the starting screen, which is what is on screen while the backend comes
 *    up. A first launch downloads a database, so it narrates instead of
 *    spinning silently;
 *  - the failure screen, for when the backend never came up. That is the one
 *    moment a web app cannot speak for itself, and a blank white rectangle is
 *    the worst available answer.
 */
export default function App() {
  const [snapshot, setSnapshot] = useState<SupervisorSnapshot | null>(null);
  const [cloud, setCloud] = useState<CloudStatus | null>(null);
  const [info, setInfo] = useState<AppInfo | null>(null);
  const [update, setUpdate] = useState<UpdateStatus | null>(null);

  useEffect(() => {
    void api.supervisorState().then(setSnapshot);
    void api.cloudStatus().then(setCloud);
    void api.appInfo().then(setInfo);
    void api.updateStatus().then(setUpdate);
    const offState = api.onSupervisorState(setSnapshot);
    const offCloud = api.onCloudStatus(setCloud);
    const offUpdate = api.onUpdateStatus(setUpdate);
    return () => {
      offState();
      offCloud();
      offUpdate();
    };
  }, []);

  const reload = useCallback(() => void api.reloadCloud(), []);

  return (
    <div className="flex h-full flex-col bg-background text-foreground">
      <nav className={`drag-region flex h-11 shrink-0 items-center gap-2 border-b border-border pr-3 ${IS_MAC ? "pl-20" : "pl-3"}`}>
        <span className="text-sm font-medium">TaskTrooper</span>

        <div className="flex-1" />

        <UpdateAffordance status={update} />

        {/* Not a button any more: there is no local page to send anyone to.
            It is a read-out, and its tooltip is the sentence the tray shows. */}
        <span
          className="flex items-center gap-2 rounded-md px-2 py-1 text-xs text-muted-foreground"
          title={snapshot?.detail ?? "The local server"}
        >
          <StatusDot tone={tone(snapshot)} pulse={snapshot?.state === "starting"} />
          {supervisorLabel(snapshot, IS_MAC)}
        </span>

        <Button variant="ghost" size="sm" className="no-drag" onClick={reload} title="Reload">
          <RefreshCw />
        </Button>
      </nav>

      <div className="min-h-0 flex-1">
        {cloud?.state === "failed" ? <Unreachable status={cloud} info={info} onRetry={reload} /> : null}
        {cloud?.state === "loading" ? <Loading detail={snapshot?.detail} /> : null}
        {/* When the hosted app is up, the view covers this area exactly, which
            is why there is nothing to render for it here. */}
      </div>
    </div>
  );
}

/**
 * The starting screen, and the reason it carries a sentence: the very first
 * launch downloads a Postgres and migrates it, which is tens of seconds of a
 * spinner that looks identical to a hang. The supervisor already narrates each
 * step; this shows what it said.
 */
function Loading({ detail }: { detail?: string }) {
  return (
    <div className="flex h-full flex-col items-center justify-center gap-3">
      <Loader2 className="size-5 animate-spin text-muted-foreground" />
      {detail ? <p className="max-w-sm text-center text-xs text-muted-foreground">{detail}</p> : null}
    </div>
  );
}

/**
 * The whole update UI: at most one small thing in the title bar.
 *
 * Three of the seven phases draw nothing at all — `unsupported`, `idle` and
 * `current` — because a build that is already the newest one has nothing to say
 * and saying it anyway is how a title bar becomes a notification area. There is
 * no modal, no toast, and nothing that appears while the user is doing
 * something else.
 *
 * `ready` is the only phase with a button, and it is worded as what it does
 * rather than what it wants: pressing it takes the four child processes down in
 * order and brings the app back on the new version. A task that is mid-run is
 * the reason this is never automatic — quitting normally also applies it, but
 * without returning, which is what somebody pressing Quit meant.
 */
function UpdateAffordance({ status }: { status: UpdateStatus | null }) {
  const restart = useCallback(() => void api.restartToUpdate(), []);

  if (!status) return null;

  switch (status.phase) {
    case "ready":
      return (
        <Button
          variant="ghost"
          size="sm"
          className="no-drag text-xs"
          onClick={restart}
          title={`${status.detail ?? "An update is ready."} The four local processes are stopped in order first. Quitting normally installs it too, without reopening.`}
        >
          <ArrowUpCircle />
          Restart to update
        </Button>
      );

    case "available":
      return (
        <span className="flex items-center gap-1.5 px-2 py-1 text-xs text-muted-foreground" title={status.detail}>
          <Loader2 className="size-3 animate-spin" />
          {status.percent ? `${status.percent}%` : "update"}
        </span>
      );

    case "error":
      return (
        <span
          className="flex items-center gap-1.5 px-2 py-1 text-xs text-muted-foreground"
          title={`Could not check for updates: ${status.detail ?? "no reason given"}${
            status.feed ? ` (feed: ${status.feed})` : ""
          }`}
        >
          <AlertTriangle className="size-3" />
          update check failed
        </span>
      );

    default:
      // idle, checking, current, unsupported: nothing worth a pixel.
      return null;
  }
}

/**
 * The one screen this app still owns.
 *
 * It names the address that failed and what the network said, because "could
 * not connect" without either is a message that sends someone to reinstall the
 * app when their VPN is off.
 */
function Unreachable({
  status,
  info,
  onRetry,
}: {
  status: CloudStatus;
  info: AppInfo | null;
  onRetry: () => void;
}) {
  return (
    <div className="flex h-full items-center justify-center p-8">
      <div className="max-w-md text-center">
        <WifiOff className="mx-auto size-8 text-muted-foreground" />
        <h1 className="mt-4 text-base font-semibold">TaskTrooper could not start</h1>
        <p className="mt-2 text-sm text-muted-foreground">{unreachableCopy(IS_MAC)}</p>
        {status.description ? (
          <code className="selectable mt-3 block break-all rounded bg-muted px-2 py-1.5 font-mono text-xs">
            {status.description}
            {status.code !== undefined ? ` (${status.code})` : ""}
          </code>
        ) : null}
        <Button className="no-drag mt-4" onClick={onRetry}>
          <RefreshCw />
          Try again
        </Button>
        {info ? <p className="mt-4 text-xs text-muted-foreground">TaskTrooper {info.version}</p> : null}
      </div>
    </div>
  );
}

function tone(snapshot: SupervisorSnapshot | null): Tone {
  switch (snapshot?.state) {
    case "running":
      return "ok";
    case "degraded":
    case "starting":
    case "preflight":
    case "stopping":
      return "warn";
    case "failed":
      return "bad";
    default:
      return "idle";
  }
}
