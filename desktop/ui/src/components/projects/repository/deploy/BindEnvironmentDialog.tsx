import { RefreshCw } from "lucide-react";
import { useEffect, useState } from "react";
import { Link } from "react-router-dom";
import { toast } from "sonner";
import {
  api,
  type CloudAccount,
  type CloudProviderKind,
  type CloudResource,
  type ComponentEnvironment,
  type DeployEnvironment,
} from "@/api";
import { FormDialog } from "@/components/admin/FormDialog";
import { ProviderIcon } from "@/components/projects/model/ProviderIcon";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Skeleton } from "@/components/ui/skeleton";
import { useI18n } from "@/hooks/useI18n";
import { cn } from "@/lib/utils";

type Step = "account" | "resource" | "custom";

function resourceSubtitle(r: CloudResource): string {
  return [r.ref.region, r.domains?.[0], r.labels?.git_repo, r.labels?.cluster].filter(Boolean).join(" · ");
}

interface BindEnvironmentDialogProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  componentId: string;
  environment: DeployEnvironment;
  accounts: CloudAccount[];
  /** The row being re-bound; absent for a first-time connect. */
  existing?: ComponentEnvironment;
  /** Restricts step 1 to this provider's accounts — set when the caller already
   * knows which provider the target must be (e.g. "bind PROD to Vercel"). */
  provider?: CloudProviderKind;
  onBound: (env: ComponentEnvironment) => void;
}

/**
 * Binds one component's environment to a resource in a connected account, or
 * to a custom URL. Step 1 picks the account (or "Custom URL"); step 2 is
 * either a searchable resource list (`listCloudResources`, with a manual
 * refresh past the server's 60s cache) or the URL/health-URL pair.
 */
export function BindEnvironmentDialog({
  open,
  onOpenChange,
  componentId,
  environment,
  accounts,
  existing,
  provider,
  onBound,
}: BindEnvironmentDialogProps) {
  const { t } = useI18n();
  const [step, setStep] = useState<Step>("account");
  const [accountId, setAccountId] = useState("");
  const [resources, setResources] = useState<CloudResource[] | null>(null);
  const [resourcesLoading, setResourcesLoading] = useState(false);
  const [resourceQuery, setResourceQuery] = useState("");
  const [selectedResourceId, setSelectedResourceId] = useState("");
  const [url, setUrl] = useState("");
  const [healthUrl, setHealthUrl] = useState("");
  const [saving, setSaving] = useState(false);

  const fetchResources = async (accId: string, refresh = false) => {
    setResourcesLoading(true);
    try {
      const res = await api.listCloudResources(accId, { refresh });
      setResources(res.resources);
    } catch (e) {
      toast.error(e instanceof Error ? e.message : t("repositoryPage.deploy.bind.resourcesLoadFailed"));
    } finally {
      setResourcesLoading(false);
    }
  };

  useEffect(() => {
    if (!open) return;
    setResourceQuery("");
    setResources(null);
    // A row on another provider can't satisfy a provider-bound request, so it
    // only seeds the dialog when it already matches.
    const seed = provider && existing?.provider !== provider ? undefined : existing;
    if (seed?.account_id) {
      setAccountId(seed.account_id);
      setSelectedResourceId(seed.resource?.id ?? "");
      setUrl("");
      setHealthUrl("");
      setStep("resource");
      void fetchResources(seed.account_id);
    } else if (seed?.url) {
      setAccountId("");
      setSelectedResourceId("");
      setUrl(seed.url);
      setHealthUrl(seed.health_url ?? "");
      setStep("custom");
    } else {
      setSelectedResourceId("");
      setUrl("");
      setHealthUrl("");
      // A caller that already knows the required provider (e.g. "bind PROD to
      // Vercel") skips the account step when it isn't even a choice.
      const matching = provider ? accounts.filter((a) => a.provider === provider) : accounts;
      if (provider && matching.length === 1) {
        setAccountId(matching[0].id);
        setStep("resource");
        void fetchResources(matching[0].id);
      } else {
        setAccountId("");
        setStep("account");
      }
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [open, existing, environment, provider]);

  const chooseAccount = (account: CloudAccount) => {
    setAccountId(account.id);
    setSelectedResourceId("");
    setStep("resource");
    void fetchResources(account.id);
  };

  const availableAccounts = provider ? accounts.filter((a) => a.provider === provider) : accounts;

  const filteredResources = (resources ?? []).filter((r) =>
    r.ref.name.toLowerCase().includes(resourceQuery.trim().toLowerCase()),
  );

  const canSubmit = step === "custom" ? url.trim() !== "" : step === "resource" ? selectedResourceId !== "" : false;

  const submit = async () => {
    setSaving(true);
    try {
      const saved =
        step === "custom"
          ? await api.bindEnvironment(componentId, environment, {
              url: url.trim(),
              health_url: healthUrl.trim() || undefined,
            })
          : await api.bindEnvironment(componentId, environment, {
              account_id: accountId,
              resource: resources?.find((r) => r.ref.id === selectedResourceId)?.ref,
            });
      toast.success(t("repositoryPage.deploy.bind.bound"));
      onBound(saved);
    } catch (e) {
      toast.error(e instanceof Error ? e.message : t("common.actionFailed"));
    } finally {
      setSaving(false);
    }
  };

  const envLabel = t(`cloud.environments.${environment}`);

  return (
    <FormDialog
      open={open}
      onOpenChange={onOpenChange}
      title={
        existing
          ? t("repositoryPage.deploy.bind.changeTitle", { environment: envLabel })
          : t("repositoryPage.deploy.bind.connectTitle", { environment: envLabel })
      }
      footer={
        <>
          {step !== "account" && (
            <Button variant="outline" onClick={() => setStep("account")} disabled={saving}>
              {t("repositoryPage.deploy.bind.back")}
            </Button>
          )}
          <Button variant="outline" onClick={() => onOpenChange(false)} disabled={saving}>
            {t("common.cancel")}
          </Button>
          {step !== "account" && (
            <Button onClick={() => void submit()} disabled={saving || !canSubmit}>
              {saving ? t("repositoryPage.deploy.bind.saving") : t("repositoryPage.deploy.bind.submit")}
            </Button>
          )}
        </>
      }
    >
      {step === "account" && (
        <div className="space-y-2">
          {availableAccounts.length > 0 ? (
            <fieldset className="divide-y divide-border rounded-lg border border-border">
              <legend className="sr-only">{t("repositoryPage.deploy.bind.chooseAccount")}</legend>
              {availableAccounts.map((account) => (
                <button
                  key={account.id}
                  type="button"
                  onClick={() => chooseAccount(account)}
                  className="flex w-full items-center gap-2.5 px-3 py-2 text-left transition-colors hover:bg-muted/50"
                >
                  <ProviderIcon provider={account.provider} className="shrink-0" />
                  <span className="min-w-0 flex-1 truncate text-sm font-medium">{account.label}</span>
                  <span className="shrink-0 text-xs text-muted-foreground">{t(`cloud.providers.${account.provider}`)}</span>
                </button>
              ))}
            </fieldset>
          ) : (
            <div className="space-y-1">
              <p className="text-caption text-muted-foreground">
                {provider
                  ? t("repositoryPage.deploy.bind.noProviderAccounts", { provider: t(`cloud.providers.${provider}`) })
                  : t("repositoryPage.deploy.bind.noAccounts")}
              </p>
              {provider && (
                <Button variant="link" size="sm" className="h-auto p-0" asChild>
                  <Link to="/settings/integrations">{t("repositoryPage.deploy.environments.goToIntegrations")}</Link>
                </Button>
              )}
            </div>
          )}
          {!provider && (
            <Button type="button" variant="outline" className="w-full" onClick={() => setStep("custom")}>
              {t("repositoryPage.deploy.bind.customUrlOption")}
            </Button>
          )}
        </div>
      )}

      {step === "resource" && (
        <div className="space-y-2">
          <div className="flex items-center gap-2">
            <Input
              value={resourceQuery}
              onChange={(e) => setResourceQuery(e.target.value)}
              placeholder={t("repositoryPage.deploy.bind.searchPlaceholder")}
              className="flex-1"
            />
            <Button
              type="button"
              variant="outline"
              size="sm"
              disabled={resourcesLoading}
              onClick={() => void fetchResources(accountId, true)}
            >
              <RefreshCw className={cn("h-3.5 w-3.5", resourcesLoading && "animate-spin")} />
              {t("repositoryPage.deploy.bind.refresh")}
            </Button>
          </div>
          {resourcesLoading ? (
            <Skeleton className="h-32 w-full" />
          ) : filteredResources.length === 0 ? (
            <p className="py-4 text-center text-caption text-muted-foreground">
              {t("repositoryPage.deploy.bind.noResources")}
            </p>
          ) : (
            <fieldset className="max-h-64 overflow-y-auto rounded-lg border border-border">
              <legend className="sr-only">{t("repositoryPage.deploy.bind.resourcesLabel")}</legend>
              <div className="divide-y divide-border">
                {filteredResources.map((r) => (
                  <label
                    key={r.ref.id}
                    className="flex cursor-pointer items-start gap-3 px-3 py-2 transition-colors hover:bg-muted/50 has-[:checked]:bg-muted"
                  >
                    <input
                      type="radio"
                      name="bind-resource"
                      className="mt-1 h-4 w-4 shrink-0 accent-primary focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
                      checked={selectedResourceId === r.ref.id}
                      onChange={() => setSelectedResourceId(r.ref.id)}
                    />
                    <div className="min-w-0 flex-1">
                      <p className="truncate text-sm font-medium">{r.ref.name}</p>
                      <p className="truncate text-xs text-muted-foreground">
                        {[t(`repositoryPage.deploy.bind.resourceKinds.${r.ref.kind}`), resourceSubtitle(r)]
                          .filter(Boolean)
                          .join(" · ")}
                      </p>
                    </div>
                  </label>
                ))}
              </div>
            </fieldset>
          )}
        </div>
      )}

      {step === "custom" && (
        <div className="space-y-3">
          <div className="space-y-1">
            <Label htmlFor="bind-url">{t("repositoryPage.deploy.bind.url")}</Label>
            <Input id="bind-url" value={url} onChange={(e) => setUrl(e.target.value)} placeholder="https://…" />
          </div>
          <div className="space-y-1">
            <Label htmlFor="bind-health-url">{t("repositoryPage.deploy.bind.healthUrl")}</Label>
            <Input
              id="bind-health-url"
              value={healthUrl}
              onChange={(e) => setHealthUrl(e.target.value)}
              placeholder="https://…/health"
            />
          </div>
        </div>
      )}
    </FormDialog>
  );
}
