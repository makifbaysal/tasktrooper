import { useCallback, useEffect, useMemo, useState } from "react";
import { toast } from "sonner";
import {
  api,
  type CloudAccount,
  type CloudProviderKind,
  type Component,
  type ComponentEnvironment,
  type DeployEnvironment,
  type MobilePlatform,
  type RepositoryModel,
} from "@/api";
import { ProviderIcon } from "@/components/projects/model/ProviderIcon";
import { ComponentRail } from "@/components/projects/repository/ComponentRail";
import { BindEnvironmentDialog } from "@/components/projects/repository/deploy/BindEnvironmentDialog";
import { DeliveryCard } from "@/components/projects/repository/deploy/DeliveryCard";
import { EnvironmentsCard } from "@/components/projects/repository/deploy/EnvironmentsCard";
import { ReleasesCard } from "@/components/projects/repository/deploy/ReleasesCard";
import { RuntimePanel } from "@/components/projects/repository/deploy/RuntimePanel";
import { StoreReleasesCard } from "@/components/projects/repository/deploy/StoreReleasesCard";
import { useI18n } from "@/hooks/useI18n";
import { effectiveRole, factValue } from "@/lib/project-model";
import { cn } from "@/lib/utils";

const HEALTH_DOT: Record<string, string> = {
  healthy: "bg-success",
  deploying: "bg-info",
  degraded: "bg-warning",
  failed: "bg-destructive",
  unknown: "bg-muted-foreground",
};

interface DeployRuntimeTabProps {
  model: RepositoryModel;
  repositoryId: string;
  selectedComponentId: string | null;
  onSelectComponent: (id: string) => void;
  onReload: () => void;
}

function railHealth(environments: ComponentEnvironment[], componentId: string) {
  const prod = environments.find(
    (e) => e.component_id === componentId && e.environment === "production" && e.status === "confirmed",
  );
  if (!prod) return null;
  const status = prod.health?.status ?? "unknown";
  return (
    <span className="inline-flex items-center gap-1">
      {prod.provider && <ProviderIcon provider={prod.provider} className="h-3 w-3" />}
      <span className={cn("h-1.5 w-1.5 shrink-0 rounded-full", HEALTH_DOT[status])} aria-hidden />
    </span>
  );
}

/** The component's own scan answer first, then the repository scope it sits in. */
function mobilePlatformOf(model: RepositoryModel, component: Component): MobilePlatform {
  const detected = factValue(component.mobile)?.platform;
  if (detected) return detected as MobilePlatform;
  const path = component.path === "." ? "" : component.path;
  const repo = model.repository;
  return (path ? repo.sub_projects?.find((sp) => sp.path === path)?.mobile_platform : repo.mobile_platform) ?? "";
}

/**
 * The Deploy & Runtime tab: the component rail, then that component's
 * environment bindings and live runtime (or store releases for mobile).
 */
export function DeployRuntimeTab({ model, repositoryId, selectedComponentId, onSelectComponent, onReload }: DeployRuntimeTabProps) {
  const { t } = useI18n();
  const [accounts, setAccounts] = useState<CloudAccount[] | null>(null);
  const [accountsLoading, setAccountsLoading] = useState(true);
  const [selectedEnvId, setSelectedEnvId] = useState<string | null>(null);
  const [bindTarget, setBindTarget] = useState<{
    environment: DeployEnvironment;
    existing?: ComponentEnvironment;
    provider?: CloudProviderKind;
  } | null>(null);

  const loadAccounts = useCallback(async () => {
    setAccountsLoading(true);
    try {
      const res = await api.listCloudAccounts();
      setAccounts(res.accounts);
    } catch (e) {
      setAccounts([]);
      toast.error(e instanceof Error ? e.message : t("cloud.accounts.loadFailed"));
    } finally {
      setAccountsLoading(false);
    }
  }, [t]);

  useEffect(() => {
    void loadAccounts();
  }, [loadAccounts]);

  // Hooks below must run unconditionally, so the "no components" bail-out
  // stays after them even though `selected` may be undefined until then.
  const selected = model.components.find((c) => c.id === selectedComponentId) ?? model.components[0];
  const componentEnvs = useMemo(
    () => (selected ? model.environments.filter((e) => e.component_id === selected.id) : []),
    [model.environments, selected],
  );

  const selectedEnv = useMemo(() => {
    const fromState = selectedEnvId ? componentEnvs.find((e) => e.id === selectedEnvId) : undefined;
    if (fromState) return fromState;
    const bound = componentEnvs.filter((e) => e.account_id);
    return bound.find((e) => e.environment === "production") ?? bound[0] ?? null;
  }, [selectedEnvId, componentEnvs]);

  // The release engine's actual deploy target: the confirmed production
  // binding, not just any row named "production".
  const productionEnv = useMemo(
    () => componentEnvs.find((e) => e.environment === "production" && e.status === "confirmed") ?? null,
    [componentEnvs],
  );
  const productionRow = useMemo(() => componentEnvs.find((e) => e.environment === "production"), [componentEnvs]);

  if (!selected) return null;

  const isMobile = effectiveRole(selected) === "mobile";
  const effectiveDelivery = selected.delivery?.override ?? selected.delivery?.detected ?? null;

  return (
    <div className="flex gap-6">
      <ComponentRail
        components={model.components}
        selectedId={selected.id}
        onSelect={(id) => {
          if (!id) return;
          setSelectedEnvId(null);
          onSelectComponent(id);
        }}
        renderTrailing={(c) => railHealth(model.environments, c.id)}
      />
      <div className="min-w-0 flex-1 space-y-4">
        <DeliveryCard
          key={selected.id}
          component={selected}
          onChanged={onReload}
          production={isMobile ? null : productionEnv}
          coupled={!isMobile}
          onBindProduction={(provider) => setBindTarget({ environment: "production", existing: productionRow, provider })}
        />

        {isMobile ? (
          <StoreReleasesCard
            key={selected.id}
            repositoryId={repositoryId}
            mobilePlatform={mobilePlatformOf(model, selected)}
          />
        ) : (
          <>
            <EnvironmentsCard
              environments={componentEnvs}
              accounts={accounts ?? []}
              accountsLoading={accountsLoading}
              selectedEnvId={selectedEnv?.id ?? null}
              onSelectEnv={(env) => setSelectedEnvId(env.id)}
              delivery={effectiveDelivery}
              onRequestBind={(environment, existing) => setBindTarget({ environment, existing })}
              onChanged={onReload}
              onAccountsChanged={loadAccounts}
            />

            {selectedEnv?.account_id && (
              <RuntimePanel env={selectedEnv} accounts={accounts ?? []} onAccountsChanged={loadAccounts} />
            )}
          </>
        )}

        <ReleasesCard repositoryId={repositoryId} repositoryName={model.repository.name} component={selected} />
      </div>

      {bindTarget && (
        <BindEnvironmentDialog
          open={Boolean(bindTarget)}
          onOpenChange={(open) => !open && setBindTarget(null)}
          componentId={selected.id}
          environment={bindTarget.environment}
          existing={bindTarget.existing}
          provider={bindTarget.provider}
          accounts={accounts ?? []}
          onBound={() => {
            setBindTarget(null);
            onReload();
          }}
        />
      )}
    </div>
  );
}
