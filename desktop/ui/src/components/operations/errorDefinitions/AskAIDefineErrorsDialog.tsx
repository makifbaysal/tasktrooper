import { Bug, Code2 } from "lucide-react";
import { useId, useMemo, useState } from "react";
import type { DeployEnvironment, ErrorSuggestMode, ErrorSuggestRequest } from "@/api";
import { FormDialog } from "@/components/admin/FormDialog";
import { ALL } from "@/components/operations/console/opsEntities";
import { RadioCards } from "@/components/operations/console/RadioCards";
import type { OpsEntity, OpsRepository } from "@/components/operations/shell/useOpsEntities";
import { Button } from "@/components/ui/button";
import { Label } from "@/components/ui/label";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { useI18n } from "@/hooks/useI18n";

interface AskAIDefineErrorsDialogProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  repositories: OpsRepository[];
  entities: OpsEntity[];
  defaultRepositoryId?: string;
  defaultComponentId?: string;
  env: DeployEnvironment;
  /** Starts the job; the page's job card follows it from there. */
  onStart: (request: ErrorSuggestRequest) => Promise<unknown>;
}

/** What the release engineer is asked to define errors from: a repository
 * (and optionally one component), and either the recent error groups of the
 * console's environment or the code's own error types. Mount only while open. */
export function AskAIDefineErrorsDialog({
  open,
  onOpenChange,
  repositories,
  entities,
  defaultRepositoryId,
  defaultComponentId,
  env,
  onStart,
}: AskAIDefineErrorsDialogProps) {
  const { t } = useI18n();
  const a = (key: string, params?: Record<string, string | number>) => t(`operations.errorDefinitions.ask.${key}`, params);
  const modeLabelId = useId();
  const [repositoryId, setRepositoryId] = useState(defaultRepositoryId ?? (repositories.length === 1 ? repositories[0].id : ""));
  const [componentId, setComponentId] = useState(defaultComponentId ?? "");
  const [mode, setMode] = useState<ErrorSuggestMode>("errors");
  const [submitting, setSubmitting] = useState(false);
  const components = useMemo(
    () => entities.filter((x) => x.repositoryId === repositoryId).sort((x, y) => x.name.localeCompare(y.name)),
    [entities, repositoryId],
  );
  const envLabel = t(`cloud.environments.${env}`);

  const submit = async () => {
    if (!repositoryId) return;
    setSubmitting(true);
    try {
      await onStart({ repository_id: repositoryId, component_id: componentId || undefined, environment: env, mode });
    } finally {
      setSubmitting(false);
      onOpenChange(false);
    }
  };

  return (
    <FormDialog
      open={open}
      onOpenChange={(next) => !submitting && onOpenChange(next)}
      title={a("title")}
      description={a("description")}
      footer={
        <>
          <Button variant="outline" onClick={() => onOpenChange(false)} disabled={submitting}>
            {t("common.cancel")}
          </Button>
          <Button onClick={() => void submit()} disabled={submitting || !repositoryId}>
            {submitting ? a("starting") : a("start")}
          </Button>
        </>
      }
    >
      <div className="grid gap-4 sm:grid-cols-2">
        <div className="space-y-1.5">
          <Label htmlFor="ask-errors-repository">{t("operations.errorDefinitions.editor.repository")}</Label>
          <Select
            value={repositoryId}
            onValueChange={(v) => {
              setRepositoryId(v);
              setComponentId("");
            }}
            disabled={submitting}
          >
            <SelectTrigger id="ask-errors-repository">
              <SelectValue placeholder={t("operations.errorDefinitions.editor.repositoryPlaceholder")} />
            </SelectTrigger>
            <SelectContent>
              {repositories.map((r) => (
                <SelectItem key={r.id} value={r.id}>
                  {r.name}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
        </div>
        <div className="space-y-1.5">
          <Label htmlFor="ask-errors-component">{t("operations.errorDefinitions.editor.component")}</Label>
          <Select value={componentId || ALL} onValueChange={(v) => setComponentId(v === ALL ? "" : v)} disabled={submitting || !repositoryId}>
            <SelectTrigger id="ask-errors-component">
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value={ALL}>{t("operations.errorDefinitions.editor.everyComponent")}</SelectItem>
              {components.map((c) => (
                <SelectItem key={c.id} value={c.id}>
                  {c.name}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
        </div>
      </div>
      <div className="space-y-1.5">
        <p id={modeLabelId} className="text-body font-medium">
          {a("modeLabel")}
        </p>
        <RadioCards
          labelledBy={modeLabelId}
          value={mode}
          onChange={setMode}
          disabled={submitting}
          options={[
            { value: "errors", icon: Bug, label: a("modes.errors.label"), description: a("modes.errors.description", { env: envLabel }) },
            { value: "code", icon: Code2, label: a("modes.code.label"), description: a("modes.code.description") },
          ]}
        />
      </div>
      <p className="text-caption text-muted-foreground">{a("reviewNote")}</p>
    </FormDialog>
  );
}
