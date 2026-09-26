import { ExternalLink, Lock } from "lucide-react";
import { useEffect, useState } from "react";
import { toast } from "sonner";
import {
  api,
  type CloudAccount,
  type CloudResourceStatus,
  type ComponentEnvironment,
  type EnvironmentRuntime,
  type PreviewAccess,
} from "@/api";
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

type RuntimeTab = "errors" | "logs" | "deployments";

const BYPASS_DOCS_URL =
  "https://vercel.com/docs/deployment-protection/methods-to-bypass-deployment-protection/protection-bypass-automation";

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
  const [connectOpen, setConnectOpen] = useState(false);

  useEffect(() => {
    let cancelled = false;
    setLoading(true);
    setOverview(null);
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
  // The UI acts on the server's own reason code, not the message text.
  const showReconnect = overview?.unavailable_code === "cloud_auth" && Boolean(account);
  const showConnect = overview?.unavailable_code === "not_connected" && Boolean(env.provider);
  const previewAccess = overview?.preview_access;
  // Until the overview answers, a per-branch env is assumed to have no error
  // surface, so its Errors panel never fires a request just to fail.
  const errorsSupported = overview ? overview.errors_supported !== false : !env.per_branch;
  const activeTab: RuntimeTab = tab === "errors" && !errorsSupported ? "logs" : tab;

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
            {showConnect && (
              <Button size="sm" className="mt-2" onClick={() => setConnectOpen(true)}>
                {t("repositoryPage.deploy.runtime.connect")}
              </Button>
            )}
          </Notice>
        ) : (
          <div className="flex flex-wrap items-center gap-3">
            {detail && <Badge variant={RESOURCE_STATUS_VARIANT[detail.status]}>{t(`cloud.health.${detail.status}`)}</Badge>}
            {detail?.revision && <span className="font-mono text-caption text-muted-foreground">{detail.revision}</span>}
            {previewAccess && <PreviewAccessBadge access={previewAccess} />}
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
        {!loading && previewAccess?.protected && !previewAccess.bypass_configured && (
          <Notice title={t("cloud.preview.bypass.warningTitle")}>
            <p>{t("cloud.preview.bypass.warningDesc")}</p>
            <a
              href={BYPASS_DOCS_URL}
              target="_blank"
              rel="noreferrer"
              className="mt-1 inline-flex items-center gap-1 text-info hover:underline"
            >
              {t("cloud.preview.bypass.learnMore")}
              <ExternalLink className="h-3 w-3" />
            </a>
          </Notice>
        )}
      </CardHeader>
      <CardContent>
        <Tabs value={activeTab} onValueChange={(v) => setTab(v as RuntimeTab)} variant="pill">
          <TabsList>
            {errorsSupported && (
              <TabsTrigger value="errors">{t("repositoryPage.deploy.runtime.tabs.errors")}</TabsTrigger>
            )}
            <TabsTrigger value="logs">{t("repositoryPage.deploy.runtime.tabs.logs")}</TabsTrigger>
            <TabsTrigger value="deployments">{t("repositoryPage.deploy.runtime.tabs.deployments")}</TabsTrigger>
          </TabsList>
          {errorsSupported && (
            <TabsContent value="errors">
              <ErrorsPanel envId={env.id} />
            </TabsContent>
          )}
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
      {env.provider && (
        <CloudAccountDialog
          open={connectOpen}
          onOpenChange={setConnectOpen}
          provider={env.provider}
          onSaved={() => {
            setConnectOpen(false);
            onAccountsChanged();
          }}
        />
      )}
    </Card>
  );
}

function PreviewAccessBadge({ access }: { access: PreviewAccess }) {
  const { t } = useI18n();
  if (!access.protected) return <Badge variant="outline">{t("cloud.preview.access.public")}</Badge>;
  return (
    <Badge variant="info" className="gap-1">
      <Lock className="h-3 w-3" aria-hidden />
      {access.mode === "none"
        ? t("cloud.preview.access.protectedShort")
        : t("cloud.preview.access.protected", { mode: t(`cloud.preview.access.mode.${access.mode}`) })}
    </Badge>
  );
}
