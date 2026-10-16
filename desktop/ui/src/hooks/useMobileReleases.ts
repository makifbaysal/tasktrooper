import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { useNavigate } from "react-router-dom";
import { toast } from "sonner";
import {
  api,
  type MobileStorePlatform,
  type StoreAppView,
  type StoreCredentialProvider,
  type StoreCredentialView,
  type StoreTracks,
} from "@/api";
import { hasStoreChannels, isTracksCacheFresh } from "@/components/operations/StoreReleaseControls";
import { byPlatform, describeRefusal, INTEGRATIONS_PATH } from "@/components/operations/releases/mobileReleases";
import { createOpsResource, useOpsResource } from "@/components/operations/shell/useOpsResource";
import { tStatic, useI18n } from "@/hooks/useI18n";

export const MOBILE_APPS_POLL_MS = 30_000;
const CREDENTIALS_POLL_MS = 60_000;

const NO_APPS: StoreAppView[] = [];

const ALL_APPS = createOpsResource(() => api.listAllStoreApps().then((apps) => apps ?? NO_APPS));
const CREDENTIALS = createOpsResource(() => api.listStoreCredentials());

export const CACHE_MOBILE_APPS = "operations.mobileReleases.apps";
export const CACHE_STORE_CREDENTIALS = "operations.mobileReleases.credentials";

/** The repository's store apps, iOS first, from the cross-repository registry
 * — the one read that carries `repository_name`, the server's confirm phrase. */
export function useRepositoryStoreApps(repositoryId: string) {
  const { value, loading, error, reload } = useOpsResource<StoreAppView[]>(ALL_APPS, CACHE_MOBILE_APPS, NO_APPS, MOBILE_APPS_POLL_MS);
  const apps = useMemo(
    () => value.filter((a) => a.repository_id === repositoryId).sort(byPlatform),
    [value, repositoryId],
  );
  return { apps, loading, error, reload };
}

/** Every repository's store apps, for the explorer's rows of apps no mobile component carries. */
export function useAllStoreApps(enabled = true): StoreAppView[] {
  return useOpsResource<StoreAppView[]>(ALL_APPS, CACHE_MOBILE_APPS, NO_APPS, MOBILE_APPS_POLL_MS, enabled).value;
}

/** Whether each store's credential is saved; undefined until the vault answers. */
export function useStoreCredentials() {
  const { value, error } = useOpsResource<StoreCredentialView[] | null>(CREDENTIALS, CACHE_STORE_CREDENTIALS, null, CREDENTIALS_POLL_MS);
  const configured = useCallback(
    (provider: StoreCredentialProvider): boolean | undefined => {
      if (!value) return undefined;
      return value.some((c) => c.provider === provider && c.configured);
    },
    [value],
  );
  return { credentials: value, configured, error };
}

// Before onboarding has put a build on any channel the store refuses the read
// outright (Play rejects edits.insert until the first upload).
const noRead = () => Promise.resolve();

interface LiveTracks {
  tracks: StoreTracks;
  at: number;
}

export interface StoreTracksState {
  tracks: StoreTracks | null;
  readAt: number | null;
  stale: boolean;
  loading: boolean;
  error: string | null;
  refresh(): Promise<void>;
}

/**
 * One app's three channels. The row's own cache answers while it is fresh;
 * otherwise the store is read live once, and again only on demand — on Play a
 * read is an edit inserted and deleted, so it never runs on a timer. A stale
 * cache is still shown while that read is out, marked as such.
 */
export function useStoreTracks(app: StoreAppView): StoreTracksState {
  const repositoryId = app.repository_id;
  const platform: MobileStorePlatform = app.platform;
  const ready = hasStoreChannels(app);
  const syncedAt = app.tracks_synced_at ? Date.parse(app.tracks_synced_at) : NaN;
  const cacheFresh = ready && Boolean(app.tracks) && isTracksCacheFresh(app.tracks_synced_at);
  const [live, setLive] = useState<LiveTracks | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [loading, setLoading] = useState(false);
  const seq = useRef(0);

  useEffect(() => {
    seq.current += 1;
    setLive(null);
    setError(null);
    setLoading(false);
  }, [repositoryId, platform]);

  const refresh = useCallback(async () => {
    const ticket = ++seq.current;
    setLoading(true);
    try {
      const tracks = await api.storeAppTracks(repositoryId, platform);
      if (ticket !== seq.current) return;
      setLive({ tracks, at: Date.now() });
      setError(null);
    } catch (err) {
      if (ticket !== seq.current) return;
      setError(err instanceof Error ? describeRefusal(err).detail : tStatic("operations.mobileReleases.pipeline.readFailed"));
    } finally {
      if (ticket === seq.current) setLoading(false);
    }
  }, [platform, repositoryId]);

  const wanted = ready && !cacheFresh && live === null && error === null;
  useEffect(() => {
    if (wanted) void refresh();
  }, [refresh, wanted]);

  if (!ready) return { tracks: null, readAt: null, stale: false, loading: false, error: null, refresh: noRead };
  const cached = app.tracks && Number.isFinite(syncedAt) ? { tracks: app.tracks, at: syncedAt } : null;
  const shown = live && (!cached || live.at >= cached.at) ? live : cached;
  return {
    tracks: shown?.tracks ?? null,
    readAt: shown?.at ?? null,
    stale: shown !== null && shown === cached && !cacheFresh,
    loading,
    error,
    refresh,
  };
}

export interface StoreRefusal {
  title: string;
  detail: string;
}

/** Runs one store action: a toast with the server's own sentence when it says
 * no, and a way to Integrations when the no is a missing credential. A
 * `success` function gets the answer and may toast on its own instead. The
 * last refusal stays in `refusal` for a confirm dialog kept open on it,
 * until `clearRefusal` or the next success. */
export function useStoreAction() {
  const { t } = useI18n();
  const navigate = useNavigate();
  const [busy, setBusy] = useState<string | null>(null);
  const [refusal, setRefusal] = useState<StoreRefusal | null>(null);

  const run = useCallback(
    async <T,>(key: string, action: () => Promise<T>, success: string | ((value: T) => string | void)): Promise<boolean> => {
      setBusy(key);
      try {
        const value = await action();
        const message = typeof success === "function" ? success(value) : success;
        if (message) toast.success(message);
        setRefusal(null);
        return true;
      } catch (err) {
        const refused = describeRefusal(err);
        const title = t(`operations.mobileReleases.refused.${refused.kind}`);
        setRefusal({ title, detail: refused.detail });
        toast.error(title, {
          description: refused.detail || undefined,
          action:
            refused.kind === "credentials"
              ? { label: t("operations.mobileReleases.refused.openIntegrations"), onClick: () => navigate(INTEGRATIONS_PATH) }
              : undefined,
        });
        return false;
      } finally {
        setBusy(null);
      }
    },
    [navigate, t],
  );

  const clearRefusal = useCallback(() => setRefusal(null), []);

  return { busy, run, refusal, clearRefusal };
}
