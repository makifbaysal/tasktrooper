import { useCallback, useState } from "react";
import { Loader2 } from "lucide-react";
import { toast } from "sonner";

import { api, type SessionWake } from "@/api";
import { Button } from "@/components/ui/button";
import { useI18n } from "@/hooks/useI18n";
import { usePolling } from "@/hooks/usePolling";
import { keepRows } from "@/lib/stableState";

const WAKE_POLL_MS = 4000;

// What the agent in this chat is waiting on (an audience build, a study): it
// answers again by itself when each one ends, and the person can say "stop
// waiting" without cancelling the work itself.
export function PendingWakesBar({ sessionId }: { sessionId: string }) {
  const { t } = useI18n();
  const [wakes, setWakes] = useState<SessionWake[]>([]);
  const [stopping, setStopping] = useState(false);

  const load = useCallback(async () => {
    try {
      const res = await api.sessionWakes(sessionId);
      setWakes((prev) => keepRows(prev, res.wakes ?? [], (w) => w.id));
    } catch {
      /* retry on next tick */
    }
  }, [sessionId]);

  usePolling(load, WAKE_POLL_MS, true);

  const stop = async () => {
    setStopping(true);
    try {
      await api.stopSessionWakes(sessionId);
      setWakes([]);
    } catch (e) {
      toast.error(e instanceof Error ? e.message : t("chatArea.chat.wakes.stopFailed"));
    } finally {
      setStopping(false);
    }
  };

  if (wakes.length === 0) return null;
  return (
    <div
      role="status"
      className="mx-auto flex w-full max-w-3xl items-center gap-3 rounded-lg border border-border bg-muted/40 px-3 py-2 text-sm"
    >
      <Loader2 className="size-4 shrink-0 animate-spin text-muted-foreground" aria-hidden />
      <div className="min-w-0 flex-1">
        <p className="font-medium">{t("chatArea.chat.wakes.waiting")}</p>
        <p className="truncate text-xs text-muted-foreground">{wakes.map((w) => w.label).join(" · ")}</p>
      </div>
      <Button variant="outline" size="sm" disabled={stopping} onClick={() => void stop()}>
        {t("chatArea.chat.wakes.stop")}
      </Button>
    </div>
  );
}
