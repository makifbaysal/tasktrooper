import { useCallback, useEffect, useState } from "react";
import { Link, useParams } from "react-router-dom";
import { toast } from "sonner";
import {
  api,
  type EnvInventory,
  type IncidentPolicy,
  type Repository,
  type StoreCredentialView,
  type TestStrategy,
} from "@/api";
import { DeployTargetsSection } from "@/components/projects/DeployTargetsSection";
import { PageHeader } from "@/components/admin/PageHeader";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Skeleton } from "@/components/ui/skeleton";
import { useI18n } from "@/hooks/useI18n";

const POLICIES: IncidentPolicy[] = ["off", "suggest", "auto_fix"];
const STRATEGIES: TestStrategy[] = ["local", "stage", "per_step"];

// DeploySettingsPage — the repository's shipping definition per environment,
// plus what a production incident on it is allowed to trigger.
//
// The per-environment editor itself lives in DeployTargetsSection, which the
// repository settings page mounts too: the addresses are the part people come
// looking for, and having them on exactly one page (this one, reachable only
// from the operations matrix) is what made them feel write-once.
export function DeploySettingsPage() {
  const { t } = useI18n();
  const { repositoryId = "" } = useParams();
  const [repository, setRepository] = useState<Repository | null>(null);
  const [inventory, setInventory] = useState<EnvInventory | null>(null);
  const [credentials, setCredentials] = useState<StoreCredentialView[]>([]);
  const [loading, setLoading] = useState(true);

  const load = useCallback(async () => {
    if (!repositoryId) return;
    setLoading(true);
    try {
      const [repo, creds] = await Promise.all([
        api.getRepository(repositoryId),
        // Gates the per-env save button in DeployTargetsSection, so it must be
        // ready before the page stops showing its loading skeleton — a failure
        // here must not blank the whole page, it just leaves every store
        // target gated.
        api.listStoreCredentials().catch(() => []),
      ]);
      setRepository(repo);
      setCredentials(creds);
      // The inventory reads the working copy, which may be missing on a fresh
      // clone — a failure here must not blank the whole page.
      setInventory(await api.getEnvInventory(repositoryId).catch(() => null));
    } catch (err) {
      toast.error(err instanceof Error ? err.message : t("common.actionFailed"));
    } finally {
      setLoading(false);
    }
  }, [repositoryId, t]);

  useEffect(() => {
    void load();
  }, [load]);

  const saveStrategy = useCallback(
    async (strategy: TestStrategy) => {
      try {
        setRepository(await api.setTestStrategy(repositoryId, strategy));
        toast.success(t("projectAdmin.prodOps.testStrategySaved"));
      } catch (err) {
        toast.error(err instanceof Error ? err.message : t("common.actionFailed"));
      }
    },
    [repositoryId, t],
  );

  const savePolicy = useCallback(
    async (policy: IncidentPolicy) => {
      try {
        setRepository(await api.setIncidentPolicy(repositoryId, policy));
        toast.success(t("projectAdmin.prodOps.policySaved"));
      } catch (err) {
        toast.error(err instanceof Error ? err.message : t("common.actionFailed"));
      }
    },
    [repositoryId, t],
  );

  if (loading) {
    return (
      <div className="space-y-3 p-4 md:p-6">
        <Skeleton className="h-24 w-full" />
        <Skeleton className="h-64 w-full" />
      </div>
    );
  }

  return (
    <div className="space-y-4 p-4 md:p-6">
      <PageHeader
        title={t("projectAdmin.prodOps.deployTitle")}
        description={t("projectAdmin.prodOps.deploySubtitle")}
      />

      <DeployTargetsSection repositoryId={repositoryId} credentials={credentials} />

      <Card>
        <CardHeader>
          <CardTitle className="text-base">{t("projectAdmin.prodOps.policyTitle")}</CardTitle>
        </CardHeader>
        <CardContent>
          <Select
            value={repository?.incident_policy ?? "suggest"}
            onValueChange={(v) => void savePolicy(v as IncidentPolicy)}
          >
            <SelectTrigger className="max-w-sm">
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              {POLICIES.map((policy) => (
                <SelectItem key={policy} value={policy}>
                  {policy === "off" && t("projectAdmin.prodOps.policyOff")}
                  {policy === "suggest" && t("projectAdmin.prodOps.policySuggest")}
                  {policy === "auto_fix" && t("projectAdmin.prodOps.policyAutoFix")}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
        </CardContent>
      </Card>

      <Card>
        <CardHeader>
          <CardTitle className="text-base">{t("projectAdmin.prodOps.testStrategyTitle")}</CardTitle>
        </CardHeader>
        <CardContent>
          <Select
            value={repository?.test_strategy ?? "stage"}
            onValueChange={(v) => void saveStrategy(v as TestStrategy)}
          >
            <SelectTrigger className="max-w-sm">
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              {STRATEGIES.map((strategy) => (
                <SelectItem key={strategy} value={strategy}>
                  {strategy === "local" && t("projectAdmin.prodOps.testStrategyLocal")}
                  {strategy === "stage" && t("projectAdmin.prodOps.testStrategyStage")}
                  {strategy === "per_step" && t("projectAdmin.prodOps.testStrategyPerStep")}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
        </CardContent>
      </Card>

      <Card>
        <CardHeader>
          <CardTitle className="text-base">{t("projectAdmin.prodOps.envInventory")}</CardTitle>
          {(inventory?.keys?.length ?? 0) > 0 && (
            <CardDescription>
              {t("projectAdmin.prodOps.envKeysCount", { count: String(inventory?.keys?.length ?? 0) })}
            </CardDescription>
          )}
        </CardHeader>
        <CardContent className="space-y-3">
          {(inventory?.files?.length ?? 0) === 0 ? (
            <p className="text-sm text-muted-foreground">{t("projectAdmin.prodOps.envInventoryEmpty")}</p>
          ) : (
            (inventory?.files ?? []).map((file) => (
              <div key={file.path} className="space-y-1">
                <p className="font-mono text-xs text-muted-foreground">{file.path}</p>
                <div className="flex flex-wrap gap-1">
                  {file.keys.map((key) => (
                    <Badge key={key} variant="outline" className="font-mono text-[11px]">
                      {key}
                    </Badge>
                  ))}
                </div>
              </div>
            ))
          )}
        </CardContent>
      </Card>

      {/* Keeps the #store-credentials anchor alive: DeployTargetsSection still
          links here when a store target has no credential yet, and the vault
          itself now lives on Settings → Integrations. */}
      <Card id="store-credentials">
        <CardHeader>
          <CardTitle className="text-base">{t("settingsPages.integrations.movedTitle")}</CardTitle>
          <CardDescription>{t("settingsPages.integrations.movedBody")}</CardDescription>
        </CardHeader>
        <CardContent>
          <Button variant="outline" asChild className="gap-2">
            <Link to="/settings/integrations">{t("settingsPages.integrations.movedLink")}</Link>
          </Button>
        </CardContent>
      </Card>

      <Card>
        <CardHeader>
          <CardTitle className="text-base">{t("projectAdmin.prodOps.storeStatusTitle")}</CardTitle>
          <CardDescription>{t("projectAdmin.prodOps.storeStatusMoved")}</CardDescription>
        </CardHeader>
        <CardContent>
          <Button variant="outline" asChild className="gap-2">
            <Link to="/operations/apps">{t("projectAdmin.prodOps.storeStatusMovedLink")}</Link>
          </Button>
        </CardContent>
      </Card>
    </div>
  );
}
