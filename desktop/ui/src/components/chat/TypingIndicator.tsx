import { useEffect, useState } from "react";
import { useI18n } from "@/hooks/useI18n";
import { cn } from "@/lib/utils";

// A reply that has not started after this long is a wait, not typing: the dots
// stop bouncing so a slow agent does not keep the compositor busy every frame.
export const TYPING_ANIMATION_MS = 10_000;

const DOT_DELAYS = ["[animation-delay:0ms]", "[animation-delay:150ms]", "[animation-delay:300ms]"];

export function TypingIndicator() {
  const { t } = useI18n();
  const [animating, setAnimating] = useState(true);
  useEffect(() => {
    const id = setTimeout(() => setAnimating(false), TYPING_ANIMATION_MS);
    return () => clearTimeout(id);
  }, []);
  return (
    <div className="py-1" aria-label={t("chatArea.chat.typing.ariaLabel")}>
      <div className="flex items-center gap-1">
        {DOT_DELAYS.map((delay) => (
          <span
            key={delay}
            className={cn("h-2 w-2 rounded-full bg-muted-foreground/60", animating && cn("animate-bounce", delay))}
          />
        ))}
      </div>
    </div>
  );
}
