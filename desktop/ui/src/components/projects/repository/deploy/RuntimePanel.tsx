import { ExternalLink } from "lucide-react";
import { useEffect, useState } from "react";
import { toast } from "sonner";
import { api, type CloudAccount, type CloudResourceStatus, type ComponentEnvironment, type EnvironmentRuntime } from "@/api";
import { CloudAccountDialog } from "@/components/admin/CloudAccountDialog";
import { DeploymentsPanel, DEPLOYMENT_STATUS_VARIANT } from "@/components/projects/repository/deploy/DeploymentsPanel";
import { ErrorsPanel } from "@/components/projects/repository/deploy/ErrorsPanel";
import { LogsPanel } from "@/components/projects/repository/deploy/LogsPanel";
import { type BadgeProps, Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardHeader } from "@/components/ui/card";
import { Notice } from "@/components/ui/notice";
import { Skeleton } from "@/components/ui/skeleton";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { useI18n } from "@/hooks/useI18n";
import { formatRelativeDate } from "@/lib/utils";

const RESOURCE_STATUS_VARIANT: Record<CloudResourceStatus, NonNullable<BadgeProps["variant"]>> = {
  healthy: "success",
  deploying: "info",
  degraded: "warning",
  failed: "destructive",
  unknown: "secondary",
};

// The overview endpoint reports `unavailable` in-band (200, not a thrown
// error), so isCloudAuthError doesn't apply here — this is the closest
// signal without the server adding a machine-readable reason to the field.
function looksLikeAuthIssue(message: string): boolean {
  return /auth|credential|token|unauthorized|forbidden|expired|401|403/i.test(message);
}

type RuntimeTab = "errors" | "logs" | "deployments";

interface RuntimePanelProps {
  env: ComponentEnvironment;
  accounts: CloudAccount[];
  onAccountsChanged: () => void;
  className?: string;
}

/** The live picture for one bound environment: resource header, then
 * Errors/Logs/Deployments. Only mounted when the environment has an
 * account behind it — a custom URL has no provider to read runtime from. */
export function RuntimePanel({ env, accounts, onAccountsChanged, className }: RuntimePanelProps) {
  const { t } = useI18n();
  const [overview, setOverview] = useState<EnvironmentRuntime | null>(null);
  const [loading, setLoading] = useState(true);
  const [tab, setTab] = useState<RuntimeTab>("errors");
  const [reconnectOpen, setReconnectOpen] = useState(false);

  useEffect(() => {
    let cancelled = false;
    setLoading(true);
    api
      .getEnvironmentOverview(env.id)
      .then((res) => {
        if (!cancelled) setOverview(res);
      })
      .catch((e) => {
        if (cancelled) return;
        toast.error(e instanceof Error ? e.message : t("repositoryPage.deploy.runtime.loadFailed"));
      })
      .finally(() => {
        if (!cancelled) setLoading(false);
      });
    return () => {
      cancelled = true;
    };
  }, [env.id, t]);

  const account = accounts.find((a) => a.id === env.account_id);
  const detail = overview?.detail;
  const latestDeployment = detail?.latest_deployment ?? overview?.deployments[0];
  const unavailable = overview?.unavailable;
  const showReconnect = Boolean(unavailable && account && looksLikeAuthIssue(unavailable));

  return (
    <Card className={className}>
      <CardHeader className="space-y-3">
        {loading ? (
          <Skeleton className="h-16 w-full" />
        ) : unavailable ? (
          <Notice variant="error" title={t("repositoryPage.deploy.runtime.unavailableTitle")}>
            <p>{unavailable}</p>
            {showReconnect && (
              <Button size="sm" className="mt-2" onClick={() => setReconnectOpen(true)}>
                {t("repositoryPage.deploy.runtime.reconnect")}
              </Button>
            )}
          </Notice>
        ) : (
          <div className="flex flex-wrap items-center gap-3">
            {detail && <Badge variant={RESOURCE_STATUS_VARIANT[detail.status]}>{t(`cloud.health.${detail.status}`)}</Badge>}
            {detail?.revision && <span className="font-mono text-caption text-muted-foreground">{detail.revision}</span>}
            {latestDeployment && (
              <span className="flex items-center gap-1.5 text-caption text-muted-foreground">
                <Badge variant={DEPLOYMENT_STATUS_VARIANT[latestDeployment.status]}>
                  {t(`repositoryPage.deploy.runtime.deployments.status.${latestDeployment.status}`)}
                </Badge>
                {latestDeployment.commit_sha && <span className="font-mono">{latestDeployment.commit_sha.slice(0, 7)}</span>}
                {latestDeployment.commit_message}
                <span>· {formatRelativeDate(latestDeployment.created_at)}</span>
              </span>
            )}
            {detail?.console_url && (
              <a
                href={detail.console_url}
                target="_blank"
                rel="noreferrer"
                className="ml-auto inline-flex items-center gap-1 text-caption text-info hover:underline"
              >
                {t("repositoryPage.deploy.runtime.consoleLink")}
                <ExternalLink className="h-3 w-3" />
              </a>
            )}
          </div>
        )}
      </CardHeader>
      <CardContent>
        <Tabs value={tab} onValueChange={(v) => setTab(v as RuntimeTab)} variant="pill">
          <TabsList>
            <TabsTrigger value="errors">{t("repositoryPage.deploy.runtime.tabs.errors")}</TabsTrigger>
            <TabsTrigger value="logs">{t("repositoryPage.deploy.runtime.tabs.logs")}</TabsTrigger>
            <TabsTrigger value="deployments">{t("repositoryPage.deploy.runtime.tabs.deployments")}</TabsTrigger>
          </TabsList>
          <TabsContent value="errors">
            <ErrorsPanel envId={env.id} />
          </TabsContent>
          <TabsContent value="logs">
            <LogsPanel envId={env.id} />
          </TabsContent>
          <TabsContent value="deployments">
            <DeploymentsPanel envId={env.id} />
          </TabsContent>
        </Tabs>
      </CardContent>

      {account && (
        <CloudAccountDialog
          open={reconnectOpen}
          onOpenChange={setReconnectOpen}
          provider={account.provider}
          account={account}
          onSaved={() => {
            setReconnectOpen(false);
            onAccountsChanged();
          }}
        />
      )}
    </Card>
  );
}
