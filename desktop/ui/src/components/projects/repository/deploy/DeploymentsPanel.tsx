import { ExternalLink, Rocket } from "lucide-react";
import { useEffect, useState } from "react";
import { toast } from "sonner";
import { api, type CloudDeployment, type CloudDeploymentStatus } from "@/api";
import { type BadgeProps, Badge } from "@/components/ui/badge";
import { EmptyState } from "@/components/ui/empty-state";
import { Skeleton } from "@/components/ui/skeleton";
import { useI18n } from "@/hooks/useI18n";
import { formatDurationMs, formatRelativeDate } from "@/lib/utils";

export const DEPLOYMENT_STATUS_VARIANT: Record<CloudDeploymentStatus, NonNullable<BadgeProps["variant"]>> = {
  ready: "success",
  building: "info",
  error: "destructive",
  canceled: "secondary",
  unknown: "secondary",
};

interface DeploymentsPanelProps {
  envId: string;
  className?: string;
}

/** One environment's deploy history, newest first. */
export function DeploymentsPanel({ envId, className }: DeploymentsPanelProps) {
  const { t } = useI18n();
  const [deployments, setDeployments] = useState<CloudDeployment[] | null>(null);
  const [loading, setLoading] = useState(true);

  useEffect(() => {
    let cancelled = false;
    setLoading(true);
    api
      .getEnvironmentDeployments(envId, { limit: 30 })
      .then((res) => {
        if (!cancelled) setDeployments(res.deployments);
      })
      .catch((e) => {
        if (cancelled) return;
        toast.error(e instanceof Error ? e.message : t("repositoryPage.deploy.runtime.deployments.loadFailed"));
      })
      .finally(() => {
        if (!cancelled) setLoading(false);
      });
    return () => {
      cancelled = true;
    };
  }, [envId, t]);

  if (loading) return <Skeleton className={className ?? "h-40 w-full"} />;

  if (!deployments || deployments.length === 0) {
    return <EmptyState icon={Rocket} title={t("repositoryPage.deploy.runtime.deployments.empty")} className={className} />;
  }

  return (
    <div className={className ?? "divide-y divide-border rounded-lg border border-border"}>
      {deployments.map((d) => {
        const readyMs = d.ready_at ? new Date(d.ready_at).getTime() - new Date(d.created_at).getTime() : undefined;
        return (
          <div key={d.id} className="flex flex-wrap items-center gap-3 px-4 py-3">
            <Badge variant={DEPLOYMENT_STATUS_VARIANT[d.status]}>{t(`repositoryPage.deploy.runtime.deployments.status.${d.status}`)}</Badge>
            {d.environment && <Badge variant="outline">{t(`cloud.environments.${d.environment}`)}</Badge>}
            {d.pr_number ? (
              <Badge variant="secondary">{t("cloud.preview.prNumber", { number: d.pr_number })}</Badge>
            ) : null}
            <div className="min-w-0 flex-1">
              <p className="truncate text-sm">
                {d.commit_sha && <span className="font-mono text-xs text-muted-foreground">{d.commit_sha.slice(0, 7)}</span>}{" "}
                {d.commit_message}
              </p>
              <p className="truncate text-xs text-muted-foreground">
                {[d.branch, d.creator, formatRelativeDate(d.created_at), readyMs !== undefined ? t("repositoryPage.deploy.runtime.deployments.readyIn", { duration: formatDurationMs(readyMs) }) : undefined]
                  .filter(Boolean)
                  .join(" · ")}
              </p>
            </div>
            {d.branch_url && <DeploymentLink href={d.branch_url} label={t("cloud.preview.branchUrl")} />}
            {d.url && <DeploymentLink href={d.url} label={t("cloud.preview.commitUrl")} />}
            {d.inspect_url && (
              <DeploymentLink href={d.inspect_url} label={t("repositoryPage.deploy.runtime.deployments.inspect")} />
            )}
          </div>
        );
      })}
    </div>
  );
}

function DeploymentLink({ href, label }: { href: string; label: string }) {
  return (
    <a
      href={href}
      target="_blank"
      rel="noreferrer"
      className="inline-flex shrink-0 items-center gap-1 text-caption text-info hover:underline"
    >
      {label}
      <ExternalLink className="h-3 w-3" />
    </a>
  );
}
