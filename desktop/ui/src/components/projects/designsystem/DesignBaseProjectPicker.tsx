import { useId, useState } from "react";
import { toast } from "sonner";
import type { DesignSystemProjectChoice } from "@/api";
import { Card } from "@/components/ui/card";
import { Label } from "@/components/ui/label";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { useI18n } from "@/hooks/useI18n";

// Radix Select reserves "" for "no value", so automatic needs a real sentinel.
const AUTOMATIC = "__automatic__";

interface DesignBaseProjectPickerProps {
  choices: DesignSystemProjectChoice[];
  /** The explicit choice; absent = automatic. */
  value?: string;
  onChange: (projectId: string | null) => Promise<void>;
}

/** Which project's base a repository in several projects builds on. */
export function DesignBaseProjectPicker({ choices, value, onChange }: DesignBaseProjectPickerProps) {
  const { t } = useI18n();
  const labelId = useId();
  const [saving, setSaving] = useState(false);
  const [optimistic, setOptimistic] = useState<string | null>(null);

  const change = async (next: string) => {
    setOptimistic(next);
    setSaving(true);
    try {
      await onChange(next === AUTOMATIC ? null : next);
      toast.success(t("designSystem.repository.baseProjectSaved"));
    } catch (e) {
      toast.error(e instanceof Error ? e.message : t("designSystem.repository.baseProjectFailed"));
    } finally {
      setOptimistic(null);
      setSaving(false);
    }
  };

  return (
    <Card className="flex flex-wrap items-center justify-between gap-3 p-4">
      <div className="min-w-0 space-y-1">
        <Label id={labelId}>{t("designSystem.repository.baseProject")}</Label>
        <p className="text-caption text-muted-foreground">{t("designSystem.repository.baseProjectHelp")}</p>
      </div>
      <Select value={optimistic ?? value ?? AUTOMATIC} onValueChange={(next) => void change(next)} disabled={saving}>
        <SelectTrigger className="w-full sm:w-80" aria-labelledby={labelId}>
          <SelectValue />
        </SelectTrigger>
        <SelectContent>
          <SelectItem value={AUTOMATIC}>{t("designSystem.repository.automatic")}</SelectItem>
          {choices.map((choice) => (
            <SelectItem key={choice.project.id} value={choice.project.id}>
              {choice.base_version
                ? t("designSystem.repository.choiceWithBase", { name: choice.project.name, version: choice.base_version })
                : t("designSystem.repository.choiceNoBase", { name: choice.project.name })}
            </SelectItem>
          ))}
        </SelectContent>
      </Select>
    </Card>
  );
}
