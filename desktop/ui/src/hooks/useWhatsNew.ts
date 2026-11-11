import { useCallback, useEffect, useState } from "react";
import { findWhatsNew, normalizeVersion, type WhatsNewEntry } from "@/lib/whatsNew/entries";
import { readSeenVersion, writeSeenVersion } from "@/lib/whatsNew/seen";

/**
 * The release notes to show now: the entry for the running version, once, on
 * the first launch after the version changed. A fresh install records the
 * version silently, and a version without an entry (or any browser tab, which
 * has no shell to ask) shows nothing. The version is recorded as the notes
 * open, so a quit without dismissing them does not show them a second time.
 */
export function useWhatsNewOnLaunch(): { entry: WhatsNewEntry | null; dismiss: () => void } {
  const [entry, setEntry] = useState<WhatsNewEntry | null>(null);

  useEffect(() => {
    const info = window.__tasktrooperDesktop?.info;
    if (!info) return;
    let cancelled = false;
    void info().then(
      ({ version }) => {
        if (cancelled || !version) return;
        const current = normalizeVersion(version);
        const seen = readSeenVersion();
        if (seen === current) return;
        writeSeenVersion(current);
        if (seen === null) return;
        setEntry(findWhatsNew(current));
      },
      () => undefined,
    );
    return () => {
      cancelled = true;
    };
  }, []);

  const dismiss = useCallback(() => setEntry(null), []);
  return { entry, dismiss };
}
