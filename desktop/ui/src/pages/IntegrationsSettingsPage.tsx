import { useCallback, useEffect, useState } from "react";
import { toast } from "sonner";
import { api, type StoreCredentialView } from "@/api";
import { AppStoreConnectCard } from "@/components/admin/AppStoreConnectCard";
import { CloudAccountsCard } from "@/components/admin/CloudAccountsCard";
import { GitHubCard } from "@/components/admin/GitHubCard";
import { GooglePlayCard } from "@/components/admin/GooglePlayCard";
import { IntegrationSection } from "@/components/admin/IntegrationCard";
import { IssueSyncCard } from "@/components/admin/IssueSyncCard";
import { JiraCard } from "@/components/admin/JiraCard";
import { Skeleton } from "@/components/ui/skeleton";
import { tStatic, useI18n } from "@/hooks/useI18n";

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
    <div className="grid items-start gap-x-6 gap-y-8 lg:grid-cols-2">
      <div className="min-w-0 space-y-8">
        <IntegrationSection title={t("settingsPages.integrations.sections.code")}>
          <GitHubCard />
        </IntegrationSection>
        <IntegrationSection title={t("settingsPages.integrations.sections.stores")}>
          {loading ? (
            <>
              <Skeleton className="h-20 w-full rounded-xl" />
              <Skeleton className="h-20 w-full rounded-xl" />
            </>
          ) : (
            <>
              <AppStoreConnectCard credential={credentialFor("asc")} onChanged={() => void load()} />
              <GooglePlayCard credential={credentialFor("google_play")} onChanged={() => void load()} />
            </>
          )}
        </IntegrationSection>
      </div>
      <div className="min-w-0 space-y-8">
        <IntegrationSection title={t("settingsPages.integrations.sections.cloud")}>
          <CloudAccountsCard />
        </IntegrationSection>
        <IntegrationSection title={t("settingsPages.integrations.sections.issues")}>
          <JiraCard />
          <IssueSyncCard />
        </IntegrationSection>
      </div>
    </div>
  );
}
