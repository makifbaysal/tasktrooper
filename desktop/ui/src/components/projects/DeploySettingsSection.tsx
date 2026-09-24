import { useCallback, useEffect, useState } from "react";
import { Link } from "react-router-dom";
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
import { MobileStorePanel } from "@/components/projects/MobileStorePanel";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { Label } from "@/components/ui/label";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Skeleton } from "@/components/ui/skeleton";
import { useI18n } from "@/hooks/useI18n";
import { cn } from "@/lib/utils";

// Radix Select cannot hold "" as an item value, and "" is exactly what the
// repository-itself scope is called on the wire.
const ROOT_SCOPE = "__root__";

const POLICIES: IncidentPolicy[] = ["off", "suggest", "auto_fix"];
const STRATEGIES: TestStrategy[] = ["local", "stage", "per_step"];

interface DeploySettingsSectionProps {
  repositoryId: string;
  className?: string;
}

/**
 * Everything about where one repository ships and what a production incident on
 * it may trigger: the hosting link, the per-environment addresses, the incident
 * policy, the test strategy and the env inventory.
 *
 * A section rather than a page because it is mounted twice — under the
 * repository's own settings, where people go to configure it, and at
 * `/repositories/:id/deploy`, which the operations matrix links to. It loads
 * what it needs itself so neither mount site has to know.
 */
export function DeploySettingsSection({ repositoryId, className }: DeploySettingsSectionProps) {
  const { t } = useI18n();
  const [repository, setRepository] = useState<Repository | null>(null);
  const [inventory, setInventory] = useState<EnvInventory | null>(null);
  const [credentials, setCredentials] = useState<StoreCredentialView[]>([]);
  const [scope, setScope] = useState(ROOT_SCOPE);
  const [loading, setLoading] = useState(true);

  const load = useCallback(async () => {
    if (!repositoryId) return;
    setLoading(true);
    try {
      const [repo, creds] = await Promise.all([
        api.getRepository(repositoryId),
        // Gates the per-env save button in DeployTargetsSection, so it must be
        // ready before the section stops showing its loading skeleton — a
        // failure here must not blank everything, it just leaves every store
        // target gated.
        api.listStoreCredentials().catch(() => []),
      ]);
      setRepository(repo);
      setCredentials(creds);
      // The inventory reads the working copy, which may be missing on a fresh
      // clone — a failure here must not blank the section.
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
      <div className={cn("space-y-4", className)}>
        <Skeleton className="h-24 w-full" />
        <Skeleton className="h-64 w-full" />
      </div>
    );
  }

  const subProjects = repository?.sub_projects ?? [];
  const subProjectPath = scope === ROOT_SCOPE ? "" : scope;
  // The scope answers for itself: a monorepo's mobile sub-project ships through
  // a store console while the repository around it does not, and the hosting
  // link, the addresses and the templates are all stored per scope.
  const scopeKind = subProjectPath
    ? (subProjects.find((sp) => sp.path === subProjectPath)?.kind ?? "")
    : (repository?.kind ?? "");
  // A mobile scope publishes through a store console and has no hosted runtime;
  // everything else is the other way round. Showing both to both is what made
  // this screen full of widgets that did not apply.
  const isMobile = scopeKind === "mobile";
  // A monorepo is not itself mobile, but one of its sub-projects may be, and
  // that scope needs the same store furniture.
  const hasStoreScope = isMobile || subProjects.some((sp) => sp.kind === "mobile");

  return (
    <div className={cn("space-y-4", className)}>
      {subProjects.length > 0 && (
        <div className="flex flex-wrap items-center justify-end gap-2">
          <Label htmlFor="deploy-scope" className="text-xs text-muted-foreground">
            {t("projectAdmin.hosting.scope")}
          </Label>
          <Select value={scope} onValueChange={setScope}>
            <SelectTrigger id="deploy-scope" className="h-8 w-64">
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value={ROOT_SCOPE}>{t("projectAdmin.hosting.wholeRepository")}</SelectItem>
              {subProjects.map((sp) => (
                <SelectItem key={sp.path} value={sp.path}>
                  {sp.path}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
        </div>
      )}

      {/* The non-mobile branch used to mount HostingPanel here; the Deploy &
          Runtime tab (next UI step, built on the Phase 2 environments/cloud
          accounts) replaces it. */}
      {isMobile && (
        <MobileStorePanel
          repositoryId={repositoryId}
          mobilePlatform={
            (subProjectPath
              ? subProjects.find((sp) => sp.path === subProjectPath)?.mobile_platform
              : repository?.mobile_platform) ?? ""
          }
        />
      )}

      <DeployTargetsSection
        key={subProjectPath}
        repositoryId={repositoryId}
        subProjectPath={subProjectPath}
        kind={scopeKind}
        credentials={credentials}
      />

      {/* Two one-line choices: side by side, because a card holding a single
          Select reads as an empty card when it is given the full width. */}
      <div className="grid gap-4 lg:grid-cols-2">
        <Card>
          <CardHeader>
            <CardTitle className="text-base">{t("projectAdmin.prodOps.policyTitle")}</CardTitle>
          </CardHeader>
          <CardContent>
            <Select
              value={repository?.incident_policy ?? "suggest"}
              onValueChange={(v) => void savePolicy(v as IncidentPolicy)}
            >
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
            <Select
              value={repository?.test_strategy ?? "stage"}
              onValueChange={(v) => void saveStrategy(v as TestStrategy)}
            >
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
        <>
        {/* Keeps the #store-credentials anchor alive: DeployTargetsSection still
            links here when a store target has no credential yet, and the vault
            itself now lives on Settings → Integrations. */}
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
        </>
      )}
    </div>
  );
}
