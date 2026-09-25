import { useState } from "react";
import { toast } from "sonner";
import { api, type Component, type ComponentDelivery } from "@/api";
import { DeliveryEditDialog } from "@/components/projects/repository/deploy/DeliveryEditDialog";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Notice } from "@/components/ui/notice";
import { useI18n } from "@/hooks/useI18n";

interface DeliveryCardProps {
  component: Component;
  onChanged: (component: Component) => void;
  className?: string;
}

/**
 * The component's delivery profile: WHAT a merge sets in motion (mode) and
 * WHO carries it out (executor). Shown for every component, mobile included
 * — a mobile component's profile is `batch`/`store`, still worth confirming
 * or editing here rather than only implied by its role.
 */
export function DeliveryCard({ component, onChanged, className }: DeliveryCardProps) {
  const { t } = useI18n();
  const [editOpen, setEditOpen] = useState(false);
  const [confirming, setConfirming] = useState(false);

  const fact = component.delivery;
  const override = fact?.override ?? null;
  const detected = fact?.detected ?? null;
  const confidence = fact?.confidence;
  // Mirrors domain.DeliveryConfirmed: an override always confirms; a
  // detected profile confirms only at exact/high confidence — a medium
  // guess must not start dispatching production deploys nobody asked for.
  const confirmed = Boolean(override) || (Boolean(detected) && (confidence === "exact" || confidence === "high"));
  const effective: ComponentDelivery | null = override ?? detected;

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
              {effective.executor && <Badge variant="outline">{t(`release.executors.${effective.executor}`)}</Badge>}
            </div>

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
          onSaved={(saved) => {
            setEditOpen(false);
            onChanged(saved);
          }}
        />
      )}
    </Card>
  );
}
