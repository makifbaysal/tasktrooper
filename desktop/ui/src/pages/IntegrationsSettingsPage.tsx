import { useCallback, useEffect, useState } from "react";
import { toast } from "sonner";
import { api, type StoreCredentialView } from "@/api";
import { AppStoreConnectCard } from "@/components/admin/AppStoreConnectCard";
import { CloudAccountsCard } from "@/components/admin/CloudAccountsCard";
import { GitHubCard } from "@/components/admin/GitHubCard";
import { GooglePlayCard } from "@/components/admin/GooglePlayCard";
import { PageHeader } from "@/components/admin/PageHeader";
import { Skeleton } from "@/components/ui/skeleton";
import { tStatic, useI18n } from "@/hooks/useI18n";

/**
 * Settings → Integrations: every outside account TaskTrooper signs in to.
 *
 * GitHub's OAuth callback is a fixed redirect to /settings, which this page is
 * not — so its card is told to park this path, and SettingsPage forwards the
 * answer here.
 */
export function IntegrationsSettingsPage() {
  const { t } = useI18n();
  const [credentials, setCredentials] = useState<StoreCredentialView[]>([]);
  const [loading, setLoading] = useState(true);

  // Not keyed on `t`: a language switch must not refire this and blank the
  // store cards while the vault is re-read.
  const load = useCallback(async () => {
    try {
      setCredentials(await api.listStoreCredentials());
    } catch (err) {
      toast.error(err instanceof Error ? err.message : tStatic("settingsPages.integrations.loadFailed"));
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    void load();
  }, [load]);

  const credentialFor = (provider: StoreCredentialView["provider"]) =>
    credentials.find((c) => c.provider === provider);

  return (
    <>
      <PageHeader
        title={t("settingsPages.integrations.title")}
        description={t("settingsPages.integrations.description")}
      />
      <div className="grid gap-4 [grid-template-columns:repeat(auto-fit,minmax(268px,1fr))]">
        <GitHubCard />
        <CloudAccountsCard className="lg:col-span-2" />
        {loading ? (
          <>
            <Skeleton className="mt-4 h-72 w-full" />
            <Skeleton className="mt-4 h-72 w-full" />
          </>
        ) : (
          <>
            <AppStoreConnectCard credential={credentialFor("asc")} onChanged={() => void load()} />
            <GooglePlayCard credential={credentialFor("google_play")} onChanged={() => void load()} />
          </>
        )}
      </div>
    </>
  );
}
