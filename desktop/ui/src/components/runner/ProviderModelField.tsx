import { useEffect, useId, useState } from "react";
import { api, type LLMProviderType } from "@/api";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { useI18n } from "@/hooks/useI18n";

export interface ProviderModelFieldProps {
  provider: LLMProviderType;
  value: string;
  onChange: (model: string) => void;
  /** Fires when the user leaves the field; the card persists the value here. */
  onCommit?: (model: string) => void;
  placeholder?: string;
  disabled?: boolean;
  /** Bump to refetch, e.g. after the provider was connected. */
  reloadKey?: string | number;
}

/**
 * A model name input backed by the provider's own model list. The list is a
 * suggestion, never a gate: when the provider cannot be asked (no key yet, CLI
 * missing, endpoint down) the field stays a plain text input and says so.
 */
export function ProviderModelField({
  provider,
  value,
  onChange,
  onCommit,
  placeholder,
  disabled,
  reloadKey,
}: ProviderModelFieldProps) {
  const { t } = useI18n();
  const inputId = useId();
  const listId = `${inputId}-models`;
  const [models, setModels] = useState<string[]>([]);
  const [failed, setFailed] = useState(false);

  useEffect(() => {
    let cancelled = false;
    setFailed(false);
    api
      .listModels(provider)
      .then((res) => {
        if (cancelled) return;
        const ids = (res.data ?? []).map((m) => m.id).filter(Boolean);
        setModels(ids);
        setFailed(ids.length === 0);
      })
      .catch(() => {
        if (cancelled) return;
        setModels([]);
        setFailed(true);
      });
    return () => {
      cancelled = true;
    };
  }, [provider, reloadKey]);

  return (
    <div className="space-y-1.5">
      <Label htmlFor={inputId}>{t("settingsPages.llm.providerDefaultModelLabel")}</Label>
      <Input
        id={inputId}
        value={value}
        list={listId}
        disabled={disabled}
        onChange={(e) => onChange(e.target.value)}
        onBlur={() => onCommit?.(value.trim())}
        placeholder={placeholder ?? t("settingsPages.llm.providerDefaultModelPlaceholder")}
        className="font-mono text-sm"
      />
      <datalist id={listId}>
        {models.map((m) => (
          <option key={m} value={m} />
        ))}
      </datalist>
      <p className="text-caption text-muted-foreground">
        {failed ? t("settingsPages.llm.modelListFailed") : t("settingsPages.llm.providerDefaultModelHelp")}
      </p>
    </div>
  );
}
