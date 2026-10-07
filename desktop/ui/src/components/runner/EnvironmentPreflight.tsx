import { AlertTriangle, CheckCircle2, RefreshCw, XCircle } from "lucide-react";
import { useCallback, useEffect, useRef, useState } from "react";
import { Button } from "@/components/ui/button";
import { Notice } from "@/components/ui/notice";
import { useI18n } from "@/hooks/useI18n";
import { usePolling } from "@/hooks/usePolling";
import type { DesktopPreflightItem, DesktopPreflightReport, DesktopRunnerHost } from "@/lib/desktop-bridge";
import { cn } from "@/lib/utils";

const POLL_MS = 10_000;

/** What this item currently blocks, for the parent card's Connect button. */
export interface EnvironmentBlocker {
  itemId: string;
  label: string;
}

interface EnvironmentPreflightProps {
  /** Null in a browser, or in a shell too old to have grown `preflight()`. */
  host: DesktopRunnerHost | null;
  /** The item currently blocking Connect, or null. Fires on every report. */
  onBlockingChange?: (blocker: EnvironmentBlocker | null) => void;
  /**
   * Every outcome of the probe, report or failure, for a caller deriving
   * something from it — the guided setup, which gates its next step on
   * `report.ready` and must react to "Check again" the moment it answers
   * rather than on its own slower timer. `report: null` with a message is a
   * probe that could not run, which is never the same as "not ready".
   */
  onReport?: (result: { report: DesktopPreflightReport | null; error: string }) => void;
  /**
   * Draw only the required items that fail — nothing while everything holds.
   * Settings → LLM uses it: the full checklist (Chrome, Appium, the Android
   * SDK…) is setup's business, and only a blocker matters next to Connect.
   */
  blockersOnly?: boolean;
}

function StatusIcon({ item }: { item: DesktopPreflightItem }) {
  if (item.status === "ok") {
    return <CheckCircle2 className="h-4 w-4 shrink-0 text-success" aria-hidden />;
  }
  // A failing optional item is a warning, not a stop sign — it costs a
  // capability, not the whole connection, so it does not read as red.
  return item.required ? (
    <XCircle className="h-4 w-4 shrink-0 text-destructive" aria-hidden />
  ) : (
    <AlertTriangle className="h-4 w-4 shrink-0 text-warning" aria-hidden />
  );
}

/**
 * The environment preflight checklist, rendered ONCE on Settings → LLM
 * Connection, above every local-CLI card rather than inside any one of them —
 * it reports on this MACHINE (the bundled server, Postgres, git, Chrome,
 * Appium, Android SDK, and each agent CLI's own binary), not on any single
 * provider. The shell reports
 * it over IPC — `host.preflight()`, mirrored between
 * `web/src/lib/desktop-bridge.ts` and `desktop/src/ipc/host.ts` — and this is
 * the one place it is rendered: each item with its status, what was found,
 * and its remediation text. `status` is one of three, not two — `missing` and
 * `unusable` take different fixes (install vs. sign in / change plan / load a
 * model), so both are shown distinctly rather than collapsed into one generic
 * failure.
 *
 * A failing REQUIRED item disables Connect on every card via
 * `onBlockingChange`, since none of them can run without it — see
 * `LocalCliCard`'s `environmentBlockingLabel`.
 *
 * Absent (not disabled) outside the desktop shell and on a shell old enough
 * to predate this call.
 */
export function EnvironmentPreflight({ host, onBlockingChange, onReport, blockersOnly = false }: EnvironmentPreflightProps) {
  const { t } = useI18n();
  const [report, setReport] = useState<DesktopPreflightReport | null>(null);
  const [error, setError] = useState("");
  const [loading, setLoading] = useState(true);
  const hasPreflight = host !== null && typeof host.preflight === "function";
  const onBlockingChangeRef = useRef(onBlockingChange);
  onBlockingChangeRef.current = onBlockingChange;
  // Held in a ref for the same reason as the one above: the callbacks are
  // recreated on every render of the card that owns them, and depending on
  // them would restart the poll each time.
  const onReportRef = useRef(onReport);
  onReportRef.current = onReport;

  // An unforced preflight() answers from the shell's last sweep, as a new
  // object each time; the same generatedAt means nothing was re-probed.
  const shownReport = useRef<DesktopPreflightReport | null>(null);

  const load = useCallback(
    async (force = false) => {
      if (!hasPreflight || !host) return;
      setLoading(true);
      try {
        const fetched = await host.preflight(force);
        const next = shownReport.current?.generatedAt === fetched.generatedAt ? shownReport.current : fetched;
        shownReport.current = next;
        setReport(next);
        setError("");
        onReportRef.current?.({ report: next, error: "" });
      } catch (e) {
        // Stubbing this silently would leave Connect looking clear when it is
        // not — the checklist failing to load is itself something to say.
        const message = e instanceof Error ? e.message : t("settingsPages.llm.claudeCode.preflight.loadFailed");
        setError(message);
        onReportRef.current?.({ report: null, error: message });
      } finally {
        setLoading(false);
      }
    },
    [hasPreflight, host, t],
  );

  useEffect(() => {
    if (!hasPreflight) onBlockingChangeRef.current?.(null);
  }, [hasPreflight]);

  usePolling(() => load(), POLL_MS, hasPreflight);

  useEffect(() => {
    if (!report) return;
    const blocking = report.items.find((i) => i.required && i.status !== "ok");
    onBlockingChangeRef.current?.(blocking ? { itemId: blocking.id, label: blocking.label } : null);
  }, [report]);

  if (!hasPreflight) return null;

  if (blockersOnly) {
    const blockers = report?.items.filter((i) => i.required && i.status !== "ok") ?? [];
    if (blockers.length === 0 && !error) return null;
    return (
      <Notice
        variant={error ? "error" : "warning"}
        title={error ? t("settingsPages.llm.claudeCode.preflight.loadFailed") : t("settingsPages.llm.claudeCode.preflight.blockersTitle")}
      >
        {error ? (
          <p>{error}</p>
        ) : (
          <ul className="space-y-1.5">
            {blockers.map((item) => (
              <li key={item.id}>
                <span className="font-medium">{item.label}</span>
                {item.remediation && <span> — {item.remediation}</span>}
                {item.command && (
                  <code className="mt-1 block break-all rounded bg-muted/60 px-1.5 py-1 font-mono text-micro">
                    {item.command}
                  </code>
                )}
              </li>
            ))}
          </ul>
        )}
        <Button variant="outline" size="sm" className="mt-2" onClick={() => void load(true)} disabled={loading}>
          <RefreshCw className={cn("mr-1.5 h-3.5 w-3.5", loading && "animate-spin")} />
          {t("settingsPages.llm.claudeCode.preflight.refresh")}
        </Button>
      </Notice>
    );
  }

  return (
    <div className="mb-3 space-y-2">
      <div className="flex items-center justify-between">
        <p className="text-xs font-medium text-foreground">{t("settingsPages.llm.claudeCode.preflight.title")}</p>
        <Button
          variant="ghost"
          size="icon"
          className="h-6 w-6"
          onClick={() => void load(true)}
          disabled={loading}
          title={t("settingsPages.llm.claudeCode.preflight.refresh")}
        >
          <RefreshCw className={cn("h-3.5 w-3.5", loading && "animate-spin")} />
        </Button>
      </div>

      {error && (
        <Notice variant="error" title={t("settingsPages.llm.claudeCode.preflight.loadFailed")}>
          <p>{error}</p>
        </Notice>
      )}

      {report && report.items.length > 0 && (
        <div className="divide-y divide-border rounded-md border border-border">
          {report.items.map((item) => (
            <div key={item.id} className="flex items-start gap-2.5 px-3 py-2">
              <StatusIcon item={item} />
              <div className="min-w-0 flex-1">
                <p className="text-xs font-medium text-foreground">{item.label}</p>
                {item.detail && <p className="text-xs text-muted-foreground">{item.detail}</p>}
                {item.status !== "ok" && item.remediation && (
                  <p className="mt-0.5 text-xs text-destructive">{item.remediation}</p>
                )}
                {item.status !== "ok" && item.command && (
                  <code className="mt-1 block break-all rounded bg-muted/60 px-1.5 py-1 font-mono text-micro">
                    {item.command}
                  </code>
                )}
              </div>
            </div>
          ))}
        </div>
      )}

      {report && report.items.length === 0 && !error && (
        <p className="text-xs text-muted-foreground">{t("settingsPages.llm.claudeCode.preflight.empty")}</p>
      )}
    </div>
  );
}
