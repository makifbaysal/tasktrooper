import { useCallback } from "react";
import { useSearchParams } from "react-router-dom";

/** One query-string value, replaced in place so filters do not fill the history. */
export function useUrlParam(key: string): [string, (value: string | null) => void] {
  const [params, setParams] = useSearchParams();
  const value = params.get(key) ?? "";
  const set = useCallback(
    (next: string | null) =>
      setParams(
        (prev) => {
          const out = new URLSearchParams(prev);
          if (next === null || next === "") out.delete(key);
          else out.set(key, next);
          return out;
        },
        { replace: true },
      ),
    [key, setParams],
  );
  return [value, set];
}
