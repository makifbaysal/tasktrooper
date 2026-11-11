import { useEffect, useState } from "react";
import { desktopRemoteAccess, type DesktopRemoteAccessHost, type DesktopRemoteAccessState } from "@/lib/desktop-bridge";

/**
 * The tunnel's state, pushed by the shell. `host` is null in a browser, in an
 * older shell and in account mode: the page then explains that remote access
 * lives in the desktop app.
 */
export function useRemoteAccess(): { host: DesktopRemoteAccessHost | null; state: DesktopRemoteAccessState | null; setState: (s: DesktopRemoteAccessState) => void } {
  const host = desktopRemoteAccess();
  const [state, setState] = useState<DesktopRemoteAccessState | null>(null);

  useEffect(() => {
    if (!host) return;
    let alive = true;
    void host.state().then(
      (s) => alive && setState(s),
      () => undefined,
    );
    // The Cloudflare login is a file on disk the page cannot see: ask main to look once.
    void host.refreshAccount().then(
      (s) => alive && setState(s),
      () => undefined,
    );
    const off = host.subscribe((s) => alive && setState(s));
    return () => {
      alive = false;
      off();
    };
  }, [host]);

  return { host, state, setState };
}
