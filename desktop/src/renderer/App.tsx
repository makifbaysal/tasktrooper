import { useCallback, useEffect, useState } from "react";
import { ArrowUpCircle, Loader2, RefreshCw, WifiOff } from "lucide-react";
import type { AppInfo, CloudStatus, SupervisorSnapshot, UpdateStatus } from "@ipc/types.js";
import { Button } from "@shared/ui/button.js";
import { api } from "./bridge";
import { unreachableCopy } from "./copy";

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
      <nav className={`drag-region flex h-11 shrink-0 items-center gap-2 border-b border-border pr-3 ${IS_MAC ? "pl-20" : "pl-3"}`} />

      <div className="min-h-0 flex-1">
        {cloud?.state === "failed" ? <Unreachable status={cloud} info={info} onRetry={reload} /> : null}
        {cloud?.state === "loading" ? <Loading detail={snapshot?.detail} /> : null}
        {/* When the hosted app is up, the view covers this area exactly, which
            is why there is nothing to render for it here. */}
      </div>

      <UpdatePopup status={update} />
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
 * The whole update UI: a small floating popup, not a title-bar affordance.
 *
 * Four of the seven phases draw nothing at all — `unsupported`, `idle`,
 * `checking` and `current` — because a build that is already the newest one
 * has nothing to say. `error` also stays silent here; a failed background
 * check is not something worth interrupting the user for.
 *
 * The popup only appears once a download is actually in flight (`available`,
 * with its percent) or finished (`ready`). Clicking it in the `ready` state
 * is what does the restart: the four child processes are stopped in order and
 * the app comes back on the new version. A task that is mid-run is the reason
 * this is never automatic — quitting normally also applies it, but without
 * returning, which is what somebody pressing Quit meant.
 */
function UpdatePopup({ status }: { status: UpdateStatus | null }) {
  const restart = useCallback(() => void api.restartToUpdate(), []);

  if (!status || (status.phase !== "available" && status.phase !== "ready")) return null;

  return (
    <div className="no-drag fixed bottom-4 right-4 z-50 w-64 rounded-lg border border-border bg-popover p-3 shadow-lg">
      {status.phase === "ready" ? (
        <Button
          variant="secondary"
          size="sm"
          className="w-full gap-2 text-xs"
          onClick={restart}
          title={`${status.detail ?? "An update is ready."} The four local processes are stopped in order first. Quitting normally installs it too, without reopening.`}
        >
          <ArrowUpCircle className="size-3.5" />
          Restart to update
        </Button>
      ) : (
        <div className="flex items-center gap-2 text-xs text-muted-foreground" title={status.detail}>
          <Loader2 className="size-3.5 shrink-0 animate-spin" />
          <span>Downloading update{status.percent ? `… ${status.percent}%` : "…"}</span>
        </div>
      )}
    </div>
  );
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
