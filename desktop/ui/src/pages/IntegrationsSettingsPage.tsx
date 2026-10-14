import { toast } from "sonner";
import { AppStoreConnectCard } from "@/components/admin/AppStoreConnectCard";
import { CloudAccountsCard } from "@/components/admin/CloudAccountsCard";
import { GitHubCard } from "@/components/admin/GitHubCard";
import { GooglePlayCard } from "@/components/admin/GooglePlayCard";
import { IntegrationSection } from "@/components/admin/IntegrationCard";
import { IssueSyncCard } from "@/components/admin/IssueSyncCard";
import { JiraCard } from "@/components/admin/JiraCard";
import { Skeleton } from "@/components/ui/skeleton";
import { useI18n } from "@/hooks/useI18n";
import { useStoreCredentials } from "@/hooks/useStoreCredentials";

export function IntegrationsSettingsPage() {
  const { t } = useI18n();
  const { loading, reload, credentialFor } = useStoreCredentials({ onError: (message) => toast.error(message) });
  const load = () => void reload();

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
              <AppStoreConnectCard credential={credentialFor("asc")} onChanged={load} />
              <GooglePlayCard credential={credentialFor("google_play")} onChanged={load} />
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
