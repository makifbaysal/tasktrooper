import { Telescope } from "lucide-react";
import { useState } from "react";
import type { DeployEnvironment } from "@/api";
import { EnableObservabilityDialog } from "@/components/operations/observability/EnableObservabilityDialog";
import { OPS_RUNTIME_ROLES, type OpsEntity } from "@/components/operations/shell/useOpsEntities";
import { Button, type ButtonProps } from "@/components/ui/button";
import { useI18n } from "@/hooks/useI18n";

interface EnableObservabilityButtonProps {
  entity: OpsEntity;
  env: DeployEnvironment;
  variant?: ButtonProps["variant"];
  className?: string;
}

/** "Enable observability" and its dialog, for a component that runs
 * somewhere; nothing for a library or a tool. */
export function EnableObservabilityButton({ entity, env, variant = "outline", className }: EnableObservabilityButtonProps) {
  const { t } = useI18n();
  const [open, setOpen] = useState(false);
  if (!OPS_RUNTIME_ROLES.includes(entity.role)) return null;
  return (
    <>
      <Button
        size="sm"
        variant={variant}
        className={className}
        onClick={() => setOpen(true)}
        aria-label={t("operations.observability.enableLabel", { name: entity.name })}
      >
        <Telescope aria-hidden />
        {t("operations.observability.enable")}
      </Button>
      {open && <EnableObservabilityDialog open onOpenChange={setOpen} entity={entity} env={env} />}
    </>
  );
}
