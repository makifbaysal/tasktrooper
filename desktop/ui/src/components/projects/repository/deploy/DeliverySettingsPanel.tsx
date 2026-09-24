import { useCallback, useEffect, useState } from "react";
import { Link } from "react-router-dom";
import { toast } from "sonner";
import {
  api,
  type Component,
  type EnvInventory,
  type IncidentPolicy,
  type Repository,
  type StoreCredentialView,
  type TestStrategy,
} from "@/api";
import { DeployTargetsSection } from "@/components/projects/DeployTargetsSection";
import { MobileStorePanel } from "@/components/projects/MobileStorePanel";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Skeleton } from "@/components/ui/skeleton";
import { useI18n } from "@/hooks/useI18n";

const POLICIES: IncidentPolicy[] = ["off", "suggest", "auto_fix"];
const STRATEGIES: TestStrategy[] = ["local", "stage", "per_step"];

interface DeliverySettingsPanelProps {
  repositoryId: string;
  repository: Repository;
  component: Component;
  className?: string;
}

/**
 * `DeploySettingsSection`'s targets/store/incident/test-strategy story, scoped
 * to the component the Deploy & Runtime tab's rail already has selected —
 * this tab has its own scope picker (the rail), so it composes the same
 * sub-organisms directly instead of mounting `DeploySettingsSection`'s own
 * repository/sub-project Select, which would ask the scope question twice.
 */
export function DeliverySettingsPanel({ repositoryId, repository, component, className }: DeliverySettingsPanelProps) {
  const { t } = useI18n();
  const [repo, setRepo] = useState(repository);
  const [inventory, setInventory] = useState<EnvInventory | null>(null);
  const [credentials, setCredentials] = useState<StoreCredentialView[]>([]);
  const [loading, setLoading] = useState(true);

  useEffect(() => setRepo(repository), [repository]);

  const load = useCallback(async () => {
    setLoading(true);
    try {
      const [creds, inv] = await Promise.all([
        api.listStoreCredentials().catch(() => []),
        api.getEnvInventory(repositoryId).catch(() => null),
      ]);
      setCredentials(creds);
      setInventory(inv);
    } finally {
      setLoading(false);
    }
  }, [repositoryId]);

  useEffect(() => {
    void load();
  }, [load]);

  const saveStrategy = async (strategy: TestStrategy) => {
    try {
      setRepo(await api.setTestStrategy(repositoryId, strategy));
      toast.success(t("projectAdmin.prodOps.testStrategySaved"));
    } catch (err) {
      toast.error(err instanceof Error ? err.message : t("common.actionFailed"));
    }
  };

  const savePolicy = async (policy: IncidentPolicy) => {
    try {
      setRepo(await api.setIncidentPolicy(repositoryId, policy));
      toast.success(t("projectAdmin.prodOps.policySaved"));
    } catch (err) {
      toast.error(err instanceof Error ? err.message : t("common.actionFailed"));
    }
  };

  const subProjects = repo.sub_projects ?? [];
  const subProjectPath = component.path === "." || component.path === "" ? "" : component.path;
  const scopeKind = subProjectPath ? (subProjects.find((sp) => sp.path === subProjectPath)?.kind ?? "") : (repo.kind ?? "");
  const isMobile = scopeKind === "mobile";
  const hasStoreScope = isMobile || subProjects.some((sp) => sp.kind === "mobile");
  const mobilePlatform =
    (subProjectPath ? subProjects.find((sp) => sp.path === subProjectPath)?.mobile_platform : repo.mobile_platform) ?? "";

  if (loading) {
    return (
      <div className="space-y-4">
        <Skeleton className="h-24 w-full" />
        <Skeleton className="h-64 w-full" />
      </div>
    );
  }

  return (
    <div className={className ?? "space-y-4"}>
      {isMobile && <MobileStorePanel repositoryId={repositoryId} mobilePlatform={mobilePlatform} />}

      <DeployTargetsSection key={subProjectPath} repositoryId={repositoryId} subProjectPath={subProjectPath} kind={scopeKind} credentials={credentials} />

      <div className="grid gap-4 lg:grid-cols-2">
        <Card>
          <CardHeader>
            <CardTitle className="text-base">{t("projectAdmin.prodOps.policyTitle")}</CardTitle>
          </CardHeader>
          <CardContent>
            <Select value={repo.incident_policy ?? "suggest"} onValueChange={(v) => void savePolicy(v as IncidentPolicy)}>
              <SelectTrigger>
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
            <Select value={repo.test_strategy ?? "stage"} onValueChange={(v) => void saveStrategy(v as TestStrategy)}>
              <SelectTrigger>
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
      </div>

      <Card>
        <CardHeader>
          <CardTitle className="text-base">{t("projectAdmin.prodOps.envInventory")}</CardTitle>
          {(inventory?.keys?.length ?? 0) > 0 && (
            <CardDescription>{t("projectAdmin.prodOps.envKeysCount", { count: String(inventory?.keys?.length ?? 0) })}</CardDescription>
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
                    <Badge key={key} variant="outline" className="font-mono text-micro">
                      {key}
                    </Badge>
                  ))}
                </div>
              </div>
            ))
          )}
        </CardContent>
      </Card>

      {hasStoreScope && (
        <div className="grid gap-4 lg:grid-cols-2">
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
      )}
    </div>
  );
}
