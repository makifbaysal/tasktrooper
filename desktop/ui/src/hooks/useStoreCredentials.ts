import { useCallback, useEffect, useRef, useState } from "react";
import { api, type StoreCredentialView } from "@/api";
import { tStatic } from "@/hooks/useI18n";

export interface StoreCredentials {
  credentials: StoreCredentialView[];
  loading: boolean;
  /** The last read's failure, "" once one succeeds. */
  error: string;
  reload: () => Promise<void>;
  /** Undefined while loading or when the vault could not be read — what the store cards expect. */
  credentialFor: (provider: StoreCredentialView["provider"]) => StoreCredentialView | undefined;
}

/**
 * The store credential vault, as the App Store Connect and Google Play cards
 * read it (Settings → Integrations and the first-run integrations step).
 */
export function useStoreCredentials({ onError }: { onError?: (message: string) => void } = {}): StoreCredentials {
  const [credentials, setCredentials] = useState<StoreCredentialView[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState("");
  const onErrorRef = useRef(onError);
  onErrorRef.current = onError;

  // Not keyed on `t`: a language switch must not refire this and blank the
  // store cards while the vault is re-read.
  const load = useCallback(async () => {
    try {
      setCredentials(await api.listStoreCredentials());
      setError("");
    } catch (err) {
      const message = err instanceof Error ? err.message : tStatic("settingsPages.integrations.loadFailed");
      setError(message);
      onErrorRef.current?.(message);
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    void load();
  }, [load]);

  const credentialFor = useCallback(
    (provider: StoreCredentialView["provider"]) => credentials.find((c) => c.provider === provider),
    [credentials],
  );

  return { credentials, loading, error, reload: load, credentialFor };
}
