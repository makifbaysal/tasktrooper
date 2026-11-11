import { useCallback, useEffect, useState } from "react";
import { desktopAccount, onAccountChange, type DesktopAccountState } from "@/lib/desktop-bridge";

/**
 * The shell's account state, kept current: read at mount, again when someone
 * announces a change (`announceAccountChange`) and when the window regains
 * focus. `null` until the first answer, and always `null` outside the shell.
 */
export function useDesktopAccountState(): DesktopAccountState | null {
  const account = desktopAccount();
  const [state, setState] = useState<DesktopAccountState | null>(null);

  const refresh = useCallback(async () => {
    if (!account) return;
    try {
      setState(await account.state());
    } catch {
      /* keep what was last known */
    }
  }, [account]);

  useEffect(() => {
    if (!account) return;
    void refresh();
    const off = onAccountChange(() => void refresh());
    const onFocus = () => void refresh();
    window.addEventListener("focus", onFocus);
    return () => {
      off();
      window.removeEventListener("focus", onFocus);
    };
  }, [account, refresh]);

  return state;
}
