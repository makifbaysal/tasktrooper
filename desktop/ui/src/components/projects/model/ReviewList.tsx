import { Box, Globe, Link2 } from "lucide-react";
import { useState } from "react";
import { toast } from "sonner";
import { api, COMPONENT_ROLES } from "@/api";
import type { Component, ComponentLink, ComponentRole, RepositoryModel, ReviewItem } from "@/api";
import { Button } from "@/components/ui/button";
import { ConfidenceBadge } from "@/components/projects/model/ConfidenceBadge";
import { EnvironmentCandidates } from "@/components/projects/model/EnvironmentCandidates";
import { EvidenceList } from "@/components/projects/model/EvidenceList";
import { ProviderIcon } from "@/components/projects/model/ProviderIcon";
import { RoleBadge } from "@/components/projects/model/RoleBadge";
import { useI18n } from "@/hooks/useI18n";
import { componentLabel, factValue, linkTargetLabel } from "@/lib/project-model";
import { cn } from "@/lib/utils";

interface ReviewListProps {
  model: RepositoryModel;
  onChanged: () => void;
  /** Hide the per-item evidence; used where space is tight (the add-repository flow shows it). */
  compact?: boolean;
  className?: string;
}

/** Every medium-confidence value in one repository, each answerable in place. */
export function ReviewList({ model, onChanged, compact = false, className }: ReviewListProps) {
  const { t } = useI18n();
  if (model.review.length === 0) return null;
  return (
    <ul className={cn("divide-y divide-border", className)}>
      {model.review.map((item) => (
        <li key={`${item.kind}:${item.entity_id}`} className="py-3 first:pt-0 last:pb-0">
          {item.kind === "role" ? (
            <RoleReview item={item} model={model} onChanged={onChanged} compact={compact} />
          ) : item.kind === "link" ? (
            <LinkReview item={item} model={model} onChanged={onChanged} compact={compact} />
          ) : (
            <EnvironmentReview item={item} model={model} onChanged={onChanged} compact={compact} />
          )}
        </li>
      ))}
      {model.review.length > 0 && <li className="sr-only">{t("projectModel.review.count", { count: model.review.length })}</li>}
    </ul>
  );
}

interface ItemProps {
  item: ReviewItem;
  model: RepositoryModel;
  onChanged: () => void;
  compact: boolean;
}

function useAction(onChanged: () => void) {
  const { t } = useI18n();
  const [busy, setBusy] = useState(false);
  const run = async (fn: () => Promise<unknown>) => {
    setBusy(true);
    try {
      await fn();
      onChanged();
    } catch (e) {
      toast.error(e instanceof Error ? e.message : t("projectModel.review.failed"));
    } finally {
      setBusy(false);
    }
  };
  return { busy, run };
}

function RoleReview({ item, model, onChanged, compact }: ItemProps) {
  const { t } = useI18n();
  const { busy, run } = useAction(onChanged);
  const [choosing, setChoosing] = useState(false);
  const component = model.components.find((c) => c.id === item.entity_id);
  if (!component) return null;
  const detected = component.role.detected;
  const setRole = (role: ComponentRole) => run(() => api.updateComponent(component.id, { role }));

  return (
    <div className="flex flex-col gap-2">
      <div className="flex items-start justify-between gap-3">
        <div className="flex items-start gap-2">
          <Box className="mt-0.5 h-4 w-4 shrink-0 text-warning" aria-hidden />
          <div className="flex flex-col gap-0.5">
            <span className="text-body font-medium">
              {t("projectModel.review.roleQuestion", {
                component: componentLabel(component, model.repository.name),
                role: detected ? t(`projectModel.roles.${detected}`) : "—",
              })}
            </span>
            {!compact && <EvidenceList evidence={component.role.evidence ?? []} />}
          </div>
        </div>
        <ConfidenceBadge confidence={item.confidence} />
      </div>
      <div className="flex flex-wrap gap-1.5 pl-6">
        {detected && (
          <Button size="sm" disabled={busy} onClick={() => setRole(detected)}>
            {t("projectModel.review.yesRole", { role: t(`projectModel.roles.${detected}`) })}
          </Button>
        )}
        {!choosing ? (
          <Button size="sm" variant="outline" disabled={busy} onClick={() => setChoosing(true)}>
            {t("projectModel.review.otherRole")}
          </Button>
        ) : (
          COMPONENT_ROLES.filter((r) => r !== detected).map((r) => (
            <button
              key={r}
              type="button"
              disabled={busy}
              onClick={() => setRole(r)}
              className="rounded-md focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring disabled:opacity-50"
            >
              <RoleBadge role={r} />
            </button>
          ))
        )}
      </div>
    </div>
  );
}

function LinkReview({ item, model, onChanged, compact }: ItemProps) {
  const { t } = useI18n();
  const { busy, run } = useAction(onChanged);
  const link = model.links.find((l) => l.id === item.entity_id);
  if (!link) return null;
  const from: Component | undefined = model.components.find((c) => c.id === link.from_component_id);
  const resolved = Boolean(link.to_component_id || link.to_resource_id);
  const envVars = link.env_vars ?? [];

  return (
    <div className="flex flex-col gap-2">
      <div className="flex items-start justify-between gap-3">
        <div className="flex items-start gap-2">
          <Link2 className="mt-0.5 h-4 w-4 shrink-0 text-warning" aria-hidden />
          <div className="flex flex-col gap-0.5">
            <span className="text-body font-medium">
              {t("projectModel.review.linkQuestion", {
                from: from ? componentLabel(from, model.repository.name) : model.repository.name,
                target: linkTargetLabel(link, model),
              })}
            </span>
            {(link.reason || envVars.length > 0) && (
              <span className="text-caption text-muted-foreground">
                {[envVars.join(", "), link.reason].filter(Boolean).join(" · ")}
              </span>
            )}
            {!compact && <EvidenceList evidence={link.evidence ?? []} />}
          </div>
        </div>
        <ConfidenceBadge confidence={item.confidence} />
      </div>
      <div className="flex flex-wrap gap-1.5 pl-6">
        {resolved && (
          <Button size="sm" disabled={busy} onClick={() => run(() => api.updateLink(link.id, { status: "confirmed" }))}>
            {t("projectModel.review.confirmLink")}
          </Button>
        )}
        <Button
          size="sm"
          variant="outline"
          disabled={busy}
          onClick={() => run(() => api.updateLink(link.id, { to_resource: externalResource(link) }))}
        >
          {t("projectModel.review.external")}
        </Button>
        <Button
          size="sm"
          variant="ghost"
          disabled={busy}
          onClick={() => run(() => api.updateLink(link.id, { status: "dismissed" }))}
        >
          {t("projectModel.review.dismiss")}
        </Button>
      </div>
    </div>
  );
}

function EnvironmentReview({ item, model, onChanged, compact: _compact }: ItemProps) {
  const { t } = useI18n();
  const env = model.environments.find((e) => e.id === item.entity_id);
  const component = model.components.find((c) => c.id === item.component_id);
  if (!env || !component) return null;

  return (
    <div className="flex flex-col gap-2">
      <div className="flex items-start justify-between gap-3">
        <div className="flex items-start gap-2">
          {env.provider ? (
            <ProviderIcon provider={env.provider} className="mt-0.5 h-4 w-4 shrink-0 text-warning" />
          ) : (
            <Globe className="mt-0.5 h-4 w-4 shrink-0 text-warning" aria-hidden />
          )}
          <span className="text-body font-medium">
            {t("cloud.review.whereQuestion", {
              component: componentLabel(component, model.repository.name),
              environment: t(`cloud.environments.${env.environment}`),
            })}
          </span>
        </div>
        <ConfidenceBadge confidence={item.confidence} />
      </div>

      <EnvironmentCandidates env={env} onChanged={onChanged} className="pl-6" />
    </div>
  );
}

function externalResource(link: ComponentLink) {
  const name = (link.hint || link.env_vars?.[0] || "external service").replace(/\s*\(.*\)$/, "");
  return { kind: "api" as const, name };
}

export function reviewCountOf(model: RepositoryModel | undefined | null): number {
  return model?.review.length ?? 0;
}

export function isRoleReview(item: ReviewItem, model: RepositoryModel): boolean {
  if (item.kind !== "role") return false;
  const c = model.components.find((x) => x.id === item.entity_id);
  return Boolean(c && factValue(c.role));
}
