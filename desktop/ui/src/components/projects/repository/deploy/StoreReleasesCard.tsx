import { Apple, Play, Store } from "lucide-react";
import { useCallback, useEffect, useState } from "react";
import { Link } from "react-router-dom";
import { toast } from "sonner";
import { api, type MobilePlatform, type StoreCredentialProvider, type StoreCredentialView } from "@/api";
import { StoreCredentialForm } from "@/components/admin/StoreCredentialsSection";
import { CREDENTIAL_PROVIDER, MobileStorePanel, platformsFor } from "@/components/projects/MobileStorePanel";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Dialog, DialogContent, DialogDescription, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import { DropdownMenu, DropdownMenuContent, DropdownMenuItem, DropdownMenuTrigger } from "@/components/ui/dropdown-menu";
import { EmptyState } from "@/components/ui/empty-state";
import { Skeleton } from "@/components/ui/skeleton";
import { tStatic, useI18n } from "@/hooks/useI18n";

const PROVIDER_ICON: Record<StoreCredentialProvider, typeof Apple> = {
  asc: Apple,
  google_play: Play,
};

const PROVIDER_COPY: Record<StoreCredentialProvider, "asc" | "play"> = {
  asc: "asc",
  google_play: "play",
};

interface StoreReleasesCardProps {
  repositoryId: string;
  mobilePlatform: MobilePlatform;
}

/**
 * The Deploy & Runtime tab's top card for a mobile component. A mobile app
 * ships to App Store / Google Play, not to a cloud environment, so this stands
 * in for `EnvironmentsCard`: until a store account this component can publish
 * through is connected it asks for one, then it is `MobileStorePanel`.
 */
export function StoreReleasesCard({ repositoryId, mobilePlatform }: StoreReleasesCardProps) {
  const { t } = useI18n();
  const [credentials, setCredentials] = useState<StoreCredentialView[] | null>(null);
  const [connectProvider, setConnectProvider] = useState<StoreCredentialProvider | null>(null);

  const providers = platformsFor(mobilePlatform).map((platform) => CREDENTIAL_PROVIDER[platform]);

  // Not keyed on `t`: a language switch must not blank the card while the
  // vault is re-read.
  const load = useCallback(async () => {
    try {
      setCredentials(await api.listStoreCredentials());
    } catch (err) {
      setCredentials([]);
      toast.error(err instanceof Error ? err.message : tStatic("settingsPages.integrations.loadFailed"));
    }
  }, []);

  useEffect(() => {
    void load();
  }, [load]);

  if (credentials === null) {
    return <Skeleton className="h-48 w-full rounded-xl" />;
  }

  const connected = credentials.some((c) => c.configured && providers.includes(c.provider));
  if (connected) {
    return <MobileStorePanel repositoryId={repositoryId} mobilePlatform={mobilePlatform} />;
  }

  return (
    <Card>
      <CardHeader>
        <CardTitle className="text-base">{t("projectAdmin.mobileStore.title")}</CardTitle>
      </CardHeader>
      <CardContent className="p-0">
        <EmptyState
          icon={Store}
          title={t("repositoryPage.deploy.stores.noAccountsTitle")}
          description={t("repositoryPage.deploy.stores.noAccountsDesc")}
          action={
            <div className="flex flex-col items-center gap-2">
              <DropdownMenu>
                <DropdownMenuTrigger asChild>
                  <Button size="sm">{t("repositoryPage.deploy.stores.connectAccount")}</Button>
                </DropdownMenuTrigger>
                <DropdownMenuContent>
                  {providers.map((provider) => {
                    const Icon = PROVIDER_ICON[provider];
                    return (
                      <DropdownMenuItem key={provider} onSelect={() => setConnectProvider(provider)} className="gap-2">
                        <Icon className="h-4 w-4" />
                        {t(`settingsPages.integrations.${PROVIDER_COPY[provider]}.title`)}
                      </DropdownMenuItem>
                    );
                  })}
                </DropdownMenuContent>
              </DropdownMenu>
              <Button variant="link" size="sm" asChild>
                <Link to="/settings/integrations">{t("repositoryPage.deploy.environments.goToIntegrations")}</Link>
              </Button>
            </div>
          }
        />
      </CardContent>

      <Dialog open={connectProvider !== null} onOpenChange={(open) => !open && setConnectProvider(null)}>
        {connectProvider && (
          <DialogContent>
            <DialogHeader>
              <DialogTitle>{t(`settingsPages.integrations.${PROVIDER_COPY[connectProvider]}.title`)}</DialogTitle>
              <DialogDescription>
                {t(`settingsPages.integrations.${PROVIDER_COPY[connectProvider]}.description`)}
              </DialogDescription>
            </DialogHeader>
            <StoreCredentialForm
              provider={connectProvider}
              credential={credentials.find((c) => c.provider === connectProvider)}
              onChanged={() => void load()}
            />
          </DialogContent>
        )}
      </Dialog>
    </Card>
  );
}
