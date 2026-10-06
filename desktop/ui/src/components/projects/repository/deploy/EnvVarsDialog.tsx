import type { CloudResourceRef } from "@/api";
import { EnvVarsCard } from "@/components/projects/repository/deploy/EnvVarsCard";
import { Dialog, DialogContent, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import { useI18n } from "@/hooks/useI18n";

/** Resource kinds whose variables TaskTrooper can read and write (server port.CloudEnvManager). */
const ENV_MANAGED_KINDS = new Set(["vercel_project", "cloud_run_service", "lambda_function", "app_runner_service", "ecs_service"]);

export function managesEnvVars(resource?: CloudResourceRef | null): boolean {
  return Boolean(resource && ENV_MANAGED_KINDS.has(resource.kind));
}

interface EnvVarsDialogProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  repositoryId: string;
  componentId: string;
}

/** The env vars panel opened from a bound production environment's row. */
export function EnvVarsDialog({ open, onOpenChange, repositoryId, componentId }: EnvVarsDialogProps) {
  const { t } = useI18n();
  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="flex max-h-[85vh] max-w-4xl flex-col gap-0 p-0">
        <DialogHeader className="border-b border-border px-6 py-4">
          <DialogTitle>{t("cloud.envVars.title")}</DialogTitle>
        </DialogHeader>
        {open && <EnvVarsCard repositoryId={repositoryId} componentId={componentId} embedded />}
      </DialogContent>
    </Dialog>
  );
}
