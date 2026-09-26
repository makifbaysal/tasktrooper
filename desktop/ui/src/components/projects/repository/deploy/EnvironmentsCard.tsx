import { ExternalLink, Globe } from "lucide-react";
import { useState } from "react";
import { Link } from "react-router-dom";
import { toast } from "sonner";
import {
  api,
  CLOUD_PROVIDERS,
  type CloudAccount,
  type CloudProviderKind,
  type ComponentDelivery,
  type ComponentEnvironment,
  type DeployEnvironment,
} from "@/api";
import { CloudAccountDialog } from "@/components/admin/CloudAccountDialog";
import { EnvironmentCandidates } from "@/components/projects/model/EnvironmentCandidates";
import { ProviderIcon } from "@/components/projects/model/ProviderIcon";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { ConfirmDialog } from "@/components/ui/confirm-dialog";
import { DropdownMenu, DropdownMenuContent, DropdownMenuItem, DropdownMenuTrigger } from "@/components/ui/dropdown-menu";
import { EmptyState } from "@/components/ui/empty-state";
import { Skeleton } from "@/components/ui/skeleton";
import { useI18n } from "@/hooks/useI18n";
import { cn, formatRelativeDate } from "@/lib/utils";

const ENV_ORDER: DeployEnvironment[] = ["production", "staging", "preview", "development"];

const HEALTH_DOT: Record<string, string> = {
  healthy: "bg-success",
  deploying: "bg-info",
  degraded: "bg-warning",
  failed: "bg-destructive",
  unknown: "bg-muted-foreground",
};

interface EnvironmentsCardProps {
  /** Already filtered to the selected component by the caller. */
  environments: ComponentEnvironment[];
  accounts: CloudAccount[];
  accountsLoading: boolean;
  selectedEnvId: string | null;
  onSelectEnv: (env: ComponentEnvironment) => void;
  /** The effective delivery profile — the production row shows what deploys there. */
  delivery: ComponentDelivery | null;
  /** Opens the (parent-owned) bind dialog for this environment; `existing` seeds a re-bind. */
  onRequestBind: (environment: DeployEnvironment, existing?: ComponentEnvironment) => void;
  onChanged: () => void;
  onAccountsChanged: () => void;
  className?: string;
}

/**
 * One component's production/staging/preview rows (development only when
 * something is bound to it) — where it runs, its health, and the actions to
 * bind or unbind it. A `suggested` row hands off to `EnvironmentCandidates`
 * instead of the usual actions, same as the review queue.
 */
export function EnvironmentsCard({
  environments,
  accounts,
  accountsLoading,
  selectedEnvId,
  onSelectEnv,
  delivery,
  onRequestBind,
  onChanged,
  onAccountsChanged,
  className,
}: EnvironmentsCardProps) {
  const { t } = useI18n();
  const [connectProvider, setConnectProvider] = useState<CloudProviderKind | null>(null);
  const [disconnectTarget, setDisconnectTarget] = useState<ComponentEnvironment | null>(null);
  const [disconnecting, setDisconnecting] = useState(false);

  const byEnv = new Map(environments.map((e) => [e.environment, e]));
  const rows = ENV_ORDER.filter((env) => env !== "development" || byEnv.has("development"));
  const hasAnyBound = environments.some((e) => e.status === "confirmed");
  const noAccountsAtAll = !accountsLoading && accounts.length === 0 && !hasAnyBound;

  const disconnect = async () => {
    if (!disconnectTarget) return;
    setDisconnecting(true);
    try {
      await api.deleteEnvironment(disconnectTarget.id);
      toast.success(t("repositoryPage.deploy.environments.disconnected"));
      onChanged();
    } catch (e) {
      toast.error(e instanceof Error ? e.message : t("common.actionFailed"));
    } finally {
      setDisconnecting(false);
      setDisconnectTarget(null);
    }
  };

  return (
    <Card className={className}>
      <CardHeader>
        <CardTitle className="text-base">{t("repositoryPage.deploy.environments.title")}</CardTitle>
      </CardHeader>
      <CardContent className="p-0">
        {accountsLoading ? (
          <div className="p-4">
            <Skeleton className="h-24 w-full" />
          </div>
        ) : noAccountsAtAll ? (
          <EmptyState
            icon={Globe}
            title={t("repositoryPage.deploy.environments.noAccountsTitle")}
            description={t("repositoryPage.deploy.environments.noAccountsDesc")}
            action={
              <div className="flex flex-col items-center gap-2">
                <DropdownMenu>
                  <DropdownMenuTrigger asChild>
                    <Button size="sm">{t("repositoryPage.deploy.environments.connectAccount")}</Button>
                  </DropdownMenuTrigger>
                  <DropdownMenuContent>
                    {CLOUD_PROVIDERS.map((provider) => (
                      <DropdownMenuItem key={provider} onSelect={() => setConnectProvider(provider)} className="gap-2">
                        <ProviderIcon provider={provider} />
                        {t("cloud.accounts.connectProvider", { provider: t(`cloud.providers.${provider}`) })}
                      </DropdownMenuItem>
                    ))}
                  </DropdownMenuContent>
                </DropdownMenu>
                <Button variant="link" size="sm" asChild>
                  <Link to="/settings/integrations">{t("repositoryPage.deploy.environments.goToIntegrations")}</Link>
                </Button>
              </div>
            }
          />
        ) : (
          <div className="divide-y divide-border">
            {rows.map((env) => {
              const row = byEnv.get(env);
              return (
                <EnvironmentRow
                  key={env}
                  environment={env}
                  row={row}
                  selected={Boolean(row && row.id === selectedEnvId)}
                  onSelect={() => row?.account_id && onSelectEnv(row)}
                  onConnect={() => onRequestBind(env, row)}
                  onDisconnect={() => row && setDisconnectTarget(row)}
                  onChanged={onChanged}
                  delivery={env === "production" ? delivery : null}
                />
              );
            })}
          </div>
        )}
      </CardContent>

      {connectProvider && (
        <CloudAccountDialog
          open={Boolean(connectProvider)}
          onOpenChange={(open) => !open && setConnectProvider(null)}
          provider={connectProvider}
          onSaved={() => {
            setConnectProvider(null);
            onAccountsChanged();
          }}
        />
      )}

      <ConfirmDialog
        open={Boolean(disconnectTarget)}
        onOpenChange={(open) => !open && setDisconnectTarget(null)}
        title={t("repositoryPage.deploy.environments.disconnectConfirmTitle", {
          environment: disconnectTarget ? t(`cloud.environments.${disconnectTarget.environment}`) : "",
        })}
        description={
          disconnectTarget?.environment === "production" && delivery && delivery.mode !== "none"
            ? t("repositoryPage.deploy.environments.disconnectConfirmDescProduction")
            : t("repositoryPage.deploy.environments.disconnectConfirmDesc")
        }
        confirmLabel={t("repositoryPage.deploy.environments.disconnect")}
        loading={disconnecting}
        onConfirm={disconnect}
      />
    </Card>
  );
}

interface EnvironmentRowProps {
  environment: DeployEnvironment;
  row?: ComponentEnvironment;
  selected: boolean;
  onSelect: () => void;
  onConnect: () => void;
  onDisconnect: () => void;
  onChanged: () => void;
  /** Set only for the production row — what deploys here, if anything. */
  delivery: ComponentDelivery | null;
}

function EnvironmentRow({ environment, row, selected, onSelect, onConnect, onDisconnect, onChanged, delivery }: EnvironmentRowProps) {
  const { t } = useI18n();
  const bound = row?.status === "confirmed";
  const suggested = row?.status === "suggested";
  const health = row?.health?.status ?? "unknown";
  // A per-branch environment has no single address: each PR/branch push gets
  // its own deployment, and its health is the newest one's state.
  const perBranch = bound && Boolean(row?.per_branch);

  return (
    <div
      data-testid={`env-row-${environment}`}
      className={cn("flex flex-col gap-2 px-4 py-3", selected && bound && "bg-muted/40")}
    >
      <div className="flex flex-wrap items-center gap-3">
        <Badge variant="outline" className="w-20 shrink-0 justify-center font-mono uppercase">
          {t(`cloud.environmentsShort.${environment}`)}
        </Badge>

        {bound && row ? (
          <button
            type="button"
            onClick={onSelect}
            disabled={!row.account_id}
            className="flex min-w-0 flex-1 items-center gap-2 text-left disabled:cursor-default"
          >
            {row.provider ? (
              <ProviderIcon provider={row.provider} className="h-4 w-4 shrink-0" />
            ) : (
              <Globe className="h-4 w-4 shrink-0 text-muted-foreground" aria-hidden />
            )}
            {perBranch ? (
              <>
                <span className="shrink-0 text-caption font-medium">{t("cloud.preview.autoLabel")}</span>
                {row.resource && (
                  <span className="min-w-0 truncate font-mono text-caption text-muted-foreground">
                    {row.resource.name}
                  </span>
                )}
              </>
            ) : (
              <span className="min-w-0 truncate font-mono text-caption">
                {row.resource ? [row.resource.name, row.resource.region].filter(Boolean).join(" · ") : row.url}
              </span>
            )}
          </button>
        ) : suggested && row ? (
          <div className="flex min-w-0 flex-1 items-center gap-2">
            {row.provider ? (
              <ProviderIcon provider={row.provider} className="h-4 w-4 shrink-0 text-warning" />
            ) : (
              <Globe className="h-4 w-4 shrink-0 text-warning" aria-hidden />
            )}
            <span className="truncate text-caption text-muted-foreground">
              {row.resource?.name ?? row.signal_key ?? t("repositoryPage.deploy.environments.needsAnswer")}
            </span>
            <Badge variant="warning">{t("repositoryPage.deploy.environments.needsAnswer")}</Badge>
          </div>
        ) : (
          <span className="flex-1 text-caption text-muted-foreground">—</span>
        )}

        <div className="ml-auto flex shrink-0 gap-1.5">
          {bound ? (
            <>
              <Button size="sm" variant="outline" onClick={onConnect}>
                {t("repositoryPage.deploy.environments.change")}
              </Button>
              <Button size="sm" variant="ghost" onClick={onDisconnect}>
                {t("repositoryPage.deploy.environments.disconnect")}
              </Button>
            </>
          ) : !suggested ? (
            <Button size="sm" variant="outline" onClick={onConnect}>
              {t("repositoryPage.deploy.environments.connect")}
            </Button>
          ) : null}
        </div>
      </div>

      {bound && row && (
        <div className="flex flex-wrap items-center gap-3 pl-[5.75rem] text-micro text-muted-foreground">
          {row.url && !perBranch && (
            <a
              href={row.url}
              target="_blank"
              rel="noreferrer"
              className="inline-flex items-center gap-1 text-info hover:underline"
            >
              {row.url}
              <ExternalLink className="h-3 w-3" />
            </a>
          )}
          <span className="inline-flex items-center gap-1.5">
            <span className={cn("h-1.5 w-1.5 rounded-full", HEALTH_DOT[health])} aria-hidden />
            {t(`cloud.health.${health}`)}
          </span>
          {(row.health?.error_count_24h ?? 0) > 0 && (
            <Badge
              variant="destructive"
              className="px-1.5 py-0 text-micro"
              title={t("cloud.errorCount", { count: row.health!.error_count_24h })}
            >
              {row.health!.error_count_24h}
            </Badge>
          )}
          <span>
            {row.health?.last_deploy_at
              ? t("repositoryPage.deploy.environments.lastDeploy", { time: formatRelativeDate(row.health.last_deploy_at) })
              : t("repositoryPage.deploy.environments.neverDeployed")}
          </span>
          {perBranch && <span>{t("cloud.preview.hint")}</span>}
          {row.auto_confirmed && <Badge variant="secondary">{t("repositoryPage.deploy.environments.auto")}</Badge>}
          {delivery && delivery.mode !== "none" && (
            <Badge variant="outline" className="text-micro">
              {t("repositoryPage.deploy.environments.deployBadge", {
                mode: t(`release.modes.${delivery.mode}`),
                executor: delivery.executor ? t(`release.executors.${delivery.executor}`) : "—",
              })}
            </Badge>
          )}
        </div>
      )}

      {suggested && row && <EnvironmentCandidates env={row} onChanged={onChanged} className="pl-[5.75rem]" />}
    </div>
  );
}
