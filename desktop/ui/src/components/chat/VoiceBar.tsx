import { AudioLines, Mic, PhoneOff } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Spinner } from "@/components/ui/spinner";
import { useI18n } from "@/hooks/useI18n";
import type { VoiceSnapshot } from "@/lib/voice/conversation";
import { cn } from "@/lib/utils";

interface VoiceBarProps {
  snapshot: VoiceSnapshot;
  onEnd: () => void;
}

const BARS = 5;

/**
 * The strip above the composer while a voice conversation is on: who has the
 * turn, what the person is saying as they say it, and the way out.
 */
export function VoiceBar({ snapshot, onEnd }: VoiceBarProps) {
  const { t } = useI18n();
  const { state, transcript, level } = snapshot;
  const listening = state === "listening";

  return (
    <div
      className="flex items-center gap-3 border-t border-border bg-primary/5 px-4 py-3"
      role="status"
      aria-live="polite"
    >
      <div
        className={cn(
          "flex h-9 w-9 shrink-0 items-center justify-center rounded-full",
          listening ? "bg-primary text-primary-foreground" : "bg-muted text-muted-foreground",
        )}
      >
        {state === "speaking" ? (
          <AudioLines className="h-4 w-4" />
        ) : state === "thinking" || state === "starting" ? (
          <Spinner size="sm" className="text-current" />
        ) : (
          <Mic className="h-4 w-4" />
        )}
      </div>
      <div className="min-w-0 flex-1">
        <p className="text-caption font-medium text-muted-foreground">{t(`chatArea.voice.state.${state}`)}</p>
        <p className={cn("truncate text-sm", transcript ? "text-foreground" : "text-muted-foreground")}>
          {transcript || (listening ? t("chatArea.voice.hint") : " ")}
        </p>
      </div>
      {listening && (
        <div className="flex h-6 items-end gap-0.5" aria-hidden>
          {Array.from({ length: BARS }, (_, i) => {
            const threshold = (i + 1) / (BARS + 1);
            return (
              <span
                key={i}
                className={cn(
                  "w-1 rounded-full transition-all duration-100",
                  level >= threshold ? "bg-primary" : "bg-muted-foreground/30",
                )}
                style={{ height: `${30 + i * 15}%` }}
              />
            );
          })}
        </div>
      )}
      <Button variant="outline" size="sm" onClick={onEnd}>
        <PhoneOff className="mr-1.5 h-4 w-4" />
        {t("chatArea.voice.end")}
      </Button>
    </div>
  );
}
