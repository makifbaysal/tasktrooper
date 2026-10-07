import { useEffect, useState } from "react";
import { elapsedTickMs, formatLiveElapsed } from "@/lib/elapsed";

interface ElapsedProps {
  /** Epoch milliseconds the clock counts from. */
  since: number;
  format?: (elapsedMs: number) => string;
  className?: string;
}

// A leaf so the clock re-renders this span alone, never the card around it.
export function Elapsed({ since, format = formatLiveElapsed, className }: ElapsedProps) {
  const [now, setNow] = useState(() => Date.now());
  useEffect(() => {
    let timer: ReturnType<typeof setTimeout> | undefined;
    const tick = () => {
      const current = Date.now();
      setNow(current);
      timer = setTimeout(tick, elapsedTickMs(current - since));
    };
    tick();
    return () => clearTimeout(timer);
  }, [since]);
  const elapsed = Number.isFinite(since) ? Math.max(0, now - since) : 0;
  return <span className={className}>{format(elapsed)}</span>;
}
