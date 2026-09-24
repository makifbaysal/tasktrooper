import { useCallback, useEffect, useMemo, useState } from "react";
import { toast } from "sonner";
import { api, type CloudAccount, type ComponentEnvironment, type RepositoryModel } from "@/api";
import { ProviderIcon } from "@/components/projects/model/ProviderIcon";
import { ComponentRail } from "@/components/projects/repository/ComponentRail";
import { DeliverySettingsPanel } from "@/components/projects/repository/deploy/DeliverySettingsPanel";
import { EnvironmentsCard } from "@/components/projects/repository/deploy/EnvironmentsCard";
import { RuntimePanel } from "@/components/projects/repository/deploy/RuntimePanel";
import { Accordion, AccordionContent, AccordionItem, AccordionTrigger } from "@/components/ui/accordion";
import { useI18n } from "@/hooks/useI18n";
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

/**
 * The Deploy & Runtime tab: the component rail, that component's environment
 * bindings and live runtime, and the still-relevant half of the old
 * `DeploySettingsSection` collapsed below.
 */
export function DeployRuntimeTab({ model, repositoryId, selectedComponentId, onSelectComponent, onReload }: DeployRuntimeTabProps) {
  const { t } = useI18n();
  const [accounts, setAccounts] = useState<CloudAccount[] | null>(null);
  const [accountsLoading, setAccountsLoading] = useState(true);
  const [selectedEnvId, setSelectedEnvId] = useState<string | null>(null);

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

  if (!selected) return null;

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
        <EnvironmentsCard
          component={selected}
          environments={componentEnvs}
          accounts={accounts ?? []}
          accountsLoading={accountsLoading}
          selectedEnvId={selectedEnv?.id ?? null}
          onSelectEnv={(env) => setSelectedEnvId(env.id)}
          onChanged={onReload}
          onAccountsChanged={loadAccounts}
        />

        {selectedEnv?.account_id && (
          <RuntimePanel env={selectedEnv} accounts={accounts ?? []} onAccountsChanged={loadAccounts} />
        )}

        <Accordion type="single" collapsible>
          <AccordionItem value="delivery">
            <AccordionTrigger>{t("repositoryPage.deploy.delivery.title")}</AccordionTrigger>
            <AccordionContent>
              <DeliverySettingsPanel repositoryId={repositoryId} repository={model.repository} component={selected} />
            </AccordionContent>
          </AccordionItem>
        </Accordion>
      </div>
    </div>
  );
}
