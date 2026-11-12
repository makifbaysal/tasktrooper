import { useCallback, useEffect, useMemo, useState } from "react";
import { toast } from "sonner";

import { api, type AudienceRuntime, type LLMEndpoint, type LLMProviderType, type LLMProviderView } from "@/api";
import { ModelSelect } from "@/components/agent/ModelSelect";
import { Button } from "@/components/ui/button";
import { Label } from "@/components/ui/label";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { useI18n } from "@/hooks/useI18n";
import { isHostExecutedProvider, isProviderAvailable } from "@/lib/constants";

const CLI_DEFAULT_MODEL = "__cli_default__";

interface ProviderOption {
  value: string;
  label: string;
  hostExecuted: boolean;
}

/**
 * Which provider and model audience research runs on. Left alone it follows
 * the product manager's own runtime; picking here overrides that for every
 * audience and study.
 */
export function AudienceRuntimePicker({ runtime, onChange }: { runtime: AudienceRuntime; onChange: (r: AudienceRuntime) => void }) {
  const { t } = useI18n();
  const [open, setOpen] = useState(false);
  const [providers, setProviders] = useState<LLMProviderView[]>([]);
  const [endpoints, setEndpoints] = useState<LLMEndpoint[]>([]);
  const [provider, setProvider] = useState(runtime.chosen ? (runtime.provider ?? "") : "");
  const [model, setModel] = useState(runtime.chosen ? (runtime.model ?? "") : "");
  const [models, setModels] = useState<string[]>([]);
  const [hasCliDefault, setHasCliDefault] = useState(false);
  const [loadingModels, setLoadingModels] = useState(false);
  const [saving, setSaving] = useState(false);

  useEffect(() => {
    if (!open) return;
    api
      .listLLMProviders()
      .then((d) => {
        setProviders(d.providers ?? []);
        setEndpoints(d.endpoints ?? []);
      })
      .catch(() => {
        setProviders([]);
        setEndpoints([]);
      });
  }, [open]);

  const options = useMemo<ProviderOption[]>(
    () => [
      ...providers
        .filter((p) => isProviderAvailable(p.definition.type, p.definition))
        .filter((p) => p.config.configured || isHostExecutedProvider(p.definition.type, p.definition))
        .map((p) => ({
          value: p.definition.type as string,
          label: p.definition.label,
          hostExecuted: isHostExecutedProvider(p.definition.type, p.definition),
        })),
      ...endpoints.filter((e) => e.configured).map((e) => ({ value: e.id, label: e.name, hostExecuted: false })),
    ],
    [providers, endpoints],
  );

  const loadModels = useCallback(async (p: string) => {
    if (!p) {
      setModels([]);
      setHasCliDefault(false);
      return;
    }
    setLoadingModels(true);
    try {
      const data = await api.listModels(p as LLMProviderType);
      const entries = data.data ?? [];
      setModels(entries.map((m) => m.id).filter(Boolean));
      setHasCliDefault(entries.some((m) => !m.id));
    } catch {
      setModels([]);
      setHasCliDefault(false);
    } finally {
      setLoadingModels(false);
    }
  }, []);

  useEffect(() => {
    if (open) void loadModels(provider);
  }, [open, provider, loadModels]);

  const hostExecuted = isHostExecutedProvider(provider as LLMProviderType);
  const modelOptions = useMemo(() => (model && !models.includes(model) ? [...models, model] : models), [models, model]);
  const canSave = Boolean(provider) && (hostExecuted || Boolean(model));

  const save = async (nextProvider: string, nextModel: string) => {
    setSaving(true);
    try {
      const next = await api.setAudienceRuntime(nextProvider, nextModel);
      onChange(next);
      if (!nextProvider) {
        setProvider("");
        setModel("");
      }
      setOpen(false);
      toast.success(t("audienceHub.runtime.saved"));
    } catch (e) {
      toast.error(e instanceof Error ? e.message : t("audienceHub.runtime.saveFailed"));
    } finally {
      setSaving(false);
    }
  };

  const current = runtime.chosen
    ? [runtime.provider, runtime.model].filter(Boolean).join(" · ")
    : t("audienceHub.runtime.followsAgent", { agent: runtime.agent ?? "" });

  return (
    <div className="rounded-lg border border-border bg-card px-4 py-3">
      <div className="flex flex-wrap items-center gap-3">
        <div className="min-w-0 flex-1">
          <p className="text-caption text-muted-foreground">{t("audienceHub.runtime.runsOn")}</p>
          <p className="truncate text-sm font-medium">{current}</p>
        </div>
        <Button variant="outline" size="sm" onClick={() => setOpen((v) => !v)}>
          {open ? t("audienceHub.runtime.close") : t("audienceHub.runtime.change")}
        </Button>
      </div>
      {open && (
        <div className="mt-4 grid gap-4 md:grid-cols-2">
          <div className="space-y-2">
            <Label>{t("audienceHub.runtime.provider")}</Label>
            <Select
              value={provider || undefined}
              onValueChange={(v) => {
                setProvider(v);
                setModel("");
              }}
            >
              <SelectTrigger>
                <SelectValue placeholder={t("audienceHub.runtime.selectProvider")} />
              </SelectTrigger>
              <SelectContent>
                {options.map((p) => (
                  <SelectItem key={p.value} value={p.value}>
                    {p.label}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          </div>
          <div className="space-y-2">
            <Label>{t("audienceHub.runtime.model")}</Label>
            <ModelSelect
              value={hostExecuted && hasCliDefault && !model ? CLI_DEFAULT_MODEL : model}
              onValueChange={(v) => setModel(v === CLI_DEFAULT_MODEL ? "" : v)}
              models={modelOptions}
              disabled={!provider || loadingModels}
              placeholder={provider ? t("audienceHub.runtime.selectModel") : t("audienceHub.runtime.selectProviderFirst")}
              extraItems={
                hostExecuted && hasCliDefault ? (
                  <SelectItem value={CLI_DEFAULT_MODEL}>{t("audienceHub.runtime.cliDefault")}</SelectItem>
                ) : undefined
              }
            />
          </div>
          <div className="flex gap-2 md:col-span-2">
            <Button size="sm" disabled={!canSave || saving} onClick={() => void save(provider, model)}>
              {t("audienceHub.runtime.save")}
            </Button>
            {runtime.chosen && (
              <Button size="sm" variant="ghost" disabled={saving} onClick={() => void save("", "")}>
                {t("audienceHub.runtime.reset")}
              </Button>
            )}
          </div>
        </div>
      )}
    </div>
  );
}
