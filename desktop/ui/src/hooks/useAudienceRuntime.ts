import { useEffect, useState } from "react";
import { api, type AudienceRuntime } from "@/api";

/** Whether there is an agent runtime to run audience research on; null until known. */
export function useAudienceRuntime(): [AudienceRuntime | null, (r: AudienceRuntime) => void] {
  const [runtime, setRuntime] = useState<AudienceRuntime | null>(null);
  useEffect(() => {
    let cancelled = false;
    api
      .audienceRuntime()
      .then((r) => {
        if (!cancelled) setRuntime(r);
      })
      .catch(() => {
        // An unanswered check must not read as "no runtime": the page keeps working.
        if (!cancelled) setRuntime({ ready: true });
      });
    return () => {
      cancelled = true;
    };
  }, []);
  return [runtime, setRuntime];
}
