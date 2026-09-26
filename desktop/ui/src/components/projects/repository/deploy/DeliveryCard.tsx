import { useState } from "react";
import { toast } from "sonner";
import { api, type CloudProviderKind, type Component, type ComponentDelivery, type ComponentEnvironment } from "@/api";
import { DeliveryEditDialog } from "@/components/projects/repository/deploy/DeliveryEditDialog";
import { ProviderIcon } from "@/components/projects/model/ProviderIcon";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Notice } from "@/components/ui/notice";
import { useI18n } from "@/hooks/useI18n";

interface DeliveryCardProps {
  component: Component;
  onChanged: (component: Component) => void;
  /** The confirmed production environment — the release engine's actual
   * deploy target. Only meaningful when `coupled`. */
  production?: ComponentEnvironment | null;
  onBindProduction?: (provider?: CloudProviderKind) => void;
  /** False for mobile: a mobile component ships through store releases, not
   * an environment binding, so the delivery↔environment coupling doesn't apply. */
  coupled?: boolean;
  className?: string;
}

function resourceLabel(env: ComponentEnvironment): string | undefined {
  return env.resource?.name ?? env.url;
}

/**
 * The component's delivery profile: WHAT a merge sets in motion (mode) and
 * WHO carries it out (executor). Shown for every component, mobile included
 * — a mobile component's profile is `batch`/`store`, still worth confirming
 * or editing here rather than only implied by its role.
 */
export function DeliveryCard({
  component,
  onChanged,
  production = null,
  onBindProduction = () => {},
  coupled = false,
  className,
}: DeliveryCardProps) {
  const { t } = useI18n();
  const [editOpen, setEditOpen] = useState(false);
  const [confirming, setConfirming] = useState(false);
  const [switchingToVercel, setSwitchingToVercel] = useState(false);

  const fact = component.delivery;
  const override = fact?.override ?? null;
  const detected = fact?.detected ?? null;
  const confidence = fact?.confidence;
  // Mirrors domain.DeliveryConfirmed: an override always confirms; a
  // detected profile confirms only at exact/high confidence — a medium
  // guess must not start dispatching production deploys nobody asked for.
  const confirmed = Boolean(override) || (Boolean(detected) && (confidence === "exact" || confidence === "high"));
  const effective: ComponentDelivery | null = override ?? detected;

  const prodIsVercel = production?.provider === "vercel";
  const executorIsVercel = effective?.executor === "vercel";
  const executorIsGithubActions = effective?.executor === "github_actions";

  const confirmDetected = async () => {
    if (!detected) return;
    setConfirming(true);
    try {
      const saved = await api.updateComponentDelivery(component.id, detected);
      toast.success(t("release.delivery.confirmedToast"));
      onChanged(saved);
    } catch (e) {
      toast.error(e instanceof Error ? e.message : t("release.delivery.saveFailed"));
    } finally {
      setConfirming(false);
    }
  };

  const deliverWithVercel = async () => {
    if (!effective) return;
    setSwitchingToVercel(true);
    try {
      const saved = await api.updateComponentDelivery(component.id, {
        ...effective,
        mode: "on_merge",
        executor: "vercel",
        workflow: undefined,
        tag_pattern: undefined,
        local_command: undefined,
      });
      toast.success(t("release.delivery.deliverWithVercelDone"));
      onChanged(saved);
    } catch (e) {
      toast.error(e instanceof Error ? e.message : t("release.delivery.saveFailed"));
    } finally {
      setSwitchingToVercel(false);
    }
  };

  return (
    <Card className={className}>
      <CardHeader className="flex flex-row flex-wrap items-center justify-between gap-2">
        <CardTitle className="text-base">{t("release.delivery.title")}</CardTitle>
        <div className="flex gap-2">
          {!confirmed && detected && (
            <Button size="sm" onClick={() => void confirmDetected()} disabled={confirming}>
              {t("release.delivery.confirm")}
            </Button>
          )}
          <Button size="sm" variant="outline" onClick={() => setEditOpen(true)}>
            {t("release.delivery.edit")}
          </Button>
        </div>
      </CardHeader>
      <CardContent className="space-y-3">
        {!confirmed && (
          <Notice variant="warning" title={t("release.delivery.unconfirmedTitle")}>
            <p>{t("release.delivery.unconfirmedBody")}</p>
          </Notice>
        )}

        {effective ? (
          <>
            <div className="flex flex-wrap items-center gap-2">
              <Badge variant={override ? "secondary" : "outline"}>
                {override ? t("release.delivery.setByYou") : t("release.delivery.detectedBadge", { confidence: t(`release.delivery.confidence.${confidence}`) })}
              </Badge>
              <Badge variant="outline">{t(`release.modes.${effective.mode}`)}</Badge>
              {effective.executor && (
                <Badge variant="outline">
                  {executorIsVercel && prodIsVercel && production
                    ? t("release.delivery.executorWithResource", {
                        executor: t("release.executors.vercel"),
                        resource: resourceLabel(production) ?? "",
                      })
                    : t(`release.executors.${effective.executor}`)}
                </Badge>
              )}
            </div>

            {coupled && effective.mode !== "none" && production && resourceLabel(production) && (
              <div className="flex flex-wrap items-center gap-1.5 text-caption text-muted-foreground">
                <span>{t("release.delivery.target")}:</span>
                <Badge variant="outline" className="font-mono uppercase">
                  {t("release.delivery.prodBadge")}
                </Badge>
                {production.provider && <ProviderIcon provider={production.provider} className="h-3.5 w-3.5" />}
                <span className="truncate font-mono">{resourceLabel(production)}</span>
              </div>
            )}

            {coupled && executorIsVercel && (!production || !prodIsVercel) && (
              <Notice variant="warning" title={t("release.delivery.vercelNotBoundTitle")}>
                <p>{t("release.delivery.vercelNotBoundBody")}</p>
                <Button size="sm" className="mt-2" onClick={() => onBindProduction("vercel")}>
                  {t("release.delivery.bindProduction")}
                </Button>
              </Notice>
            )}

            {coupled && effective.mode !== "none" && executorIsGithubActions && !production && (
              <Notice variant="info" title={t("release.delivery.githubActionsNotBoundTitle")}>
                <Button size="sm" className="mt-2" onClick={() => onBindProduction()}>
                  {t("release.delivery.bindProduction")}
                </Button>
              </Notice>
            )}

            {coupled && prodIsVercel && (effective.mode === "none" || (!executorIsVercel && !executorIsGithubActions)) && (
              <Notice variant="warning" title={t("release.delivery.vercelReadyTitle")}>
                <Button size="sm" className="mt-2" onClick={() => void deliverWithVercel()} disabled={switchingToVercel}>
                  {t("release.delivery.deliverWithVercel")}
                </Button>
              </Notice>
            )}

            {effective.mode === "batch" ? (
              effective.executor === "github_actions" && (
                <p className="text-caption text-muted-foreground">{t("release.delivery.batchGithubActionsHint")}</p>
              )
            ) : effective.mode === "none" ? null : (
              <dl className="grid grid-cols-2 gap-x-4 gap-y-1.5 text-caption sm:grid-cols-3">
                {effective.workflow && (
                  <div>
                    <dt className="text-muted-foreground">{t("release.deliveryEdit.workflow")}</dt>
                    <dd className="truncate font-mono">{effective.workflow}</dd>
                  </div>
                )}
                <div>
                  <dt className="text-muted-foreground">{t("release.delivery.soakMinutes")}</dt>
                  <dd>{t("release.delivery.soakMinutesValue", { minutes: effective.verify.soak_minutes ?? 10 })}</dd>
                </div>
                <div>
                  <dt className="text-muted-foreground">{t("release.delivery.maxNewErrors")}</dt>
                  <dd>{effective.verify.max_new_errors ?? 0}</dd>
                </div>
                <div>
                  <dt className="text-muted-foreground">{t("release.deliveryEdit.smoke.title")}</dt>
                  <dd>{t("release.delivery.smokeChecksCount", { count: effective.verify.smoke?.length ?? 0 })}</dd>
                </div>
                <div>
                  <dt className="text-muted-foreground">{t("release.deliveryEdit.autoRollback")}</dt>
                  <dd>{effective.auto_rollback ? t("release.delivery.autoRollbackOn") : t("release.delivery.autoRollbackOff")}</dd>
                </div>
              </dl>
            )}
          </>
        ) : (
          <p className="text-caption text-muted-foreground">{t("release.delivery.none")}</p>
        )}
      </CardContent>

      {editOpen && (
        <DeliveryEditDialog
          key={component.id}
          open={editOpen}
          onOpenChange={setEditOpen}
          componentId={component.id}
          current={effective}
          hasOverride={Boolean(override)}
          production={coupled ? production : null}
          onSaved={(saved) => {
            setEditOpen(false);
            onChanged(saved);
          }}
        />
      )}
    </Card>
  );
}
