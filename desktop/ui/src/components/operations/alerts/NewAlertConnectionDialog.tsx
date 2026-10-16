import { useState, type FormEvent } from "react";
import { api, type AlertToken, type DeployEnv } from "@/api";
import { FormDialog } from "@/components/admin/FormDialog";
import {
  ALERT_DEFAULT_ENVS,
  ALERT_LABEL_MAX,
  ALERT_TOOLS,
  DEFAULT_ALERT_ENV,
  alertSnippet,
  ingestUrl,
  isAlertEnv,
  isLocalOnly,
  labelProblem,
  listedToken,
  serverBase,
  type AlertTool,
} from "@/components/operations/alerts/alertConnections";
import { CopyableSnippet } from "@/components/operations/alerts/CopyableSnippet";
import { LocalOnlyNotice } from "@/components/operations/alerts/LocalOnlyNotice";
import type { OpsRepository } from "@/components/operations/shell/useOpsEntities";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Notice } from "@/components/ui/notice";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { useI18n } from "@/hooks/useI18n";

const FORM_ID = "new-alert-connection";

function isAlertTool(value: string): value is AlertTool {
  return ALERT_TOOLS.some((tool) => tool === value);
}

/** What the reveal step shows. It holds the plaintext token (inside `url`)
 * and lives in this dialog's state only: the page mounts the dialog while it
 * is open, so closing it drops the token for good. */
interface Revealed {
  label: string;
  repositoryName: string;
  env: DeployEnv;
  base: string;
  url: string;
}

interface NewAlertConnectionDialogProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  repositories: OpsRepository[];
  initialRepositoryId?: string;
  onCreated: (repositoryId: string, token: AlertToken) => void;
}

export function NewAlertConnectionDialog({
  open,
  onOpenChange,
  repositories,
  initialRepositoryId,
  onCreated,
}: NewAlertConnectionDialogProps) {
  const { t } = useI18n();
  const a = (key: string, params?: Record<string, string | number>) => t(`operations.alertConnections.${key}`, params);
  const [repositoryId, setRepositoryId] = useState(
    () => repositories.find((r) => r.id === initialRepositoryId)?.id ?? repositories[0]?.id ?? "",
  );
  const [label, setLabel] = useState("");
  const [env, setEnv] = useState<DeployEnv>(DEFAULT_ALERT_ENV);
  const [touched, setTouched] = useState(false);
  const [submitting, setSubmitting] = useState(false);
  const [failure, setFailure] = useState<string | null>(null);
  const [revealed, setRevealed] = useState<Revealed | null>(null);
  const [tool, setTool] = useState<AlertTool>("alertmanager");

  const problem = labelProblem(label);
  const repository = repositories.find((r) => r.id === repositoryId);

  const close = (next: boolean) => {
    if (next) return;
    setRevealed(null);
    onOpenChange(false);
  };

  const submit = async (event?: FormEvent) => {
    event?.preventDefault();
    setTouched(true);
    if (problem || !repository || submitting) return;
    setSubmitting(true);
    setFailure(null);
    try {
      const created = await api.createAlertToken(repository.id, { label: label.trim(), default_env: env });
      const base = serverBase();
      onCreated(repository.id, listedToken(created));
      setRevealed({
        label: created.label,
        repositoryName: repository.name,
        env: created.default_env,
        base,
        url: ingestUrl(base, created.ingest_path),
      });
    } catch (err) {
      setFailure(err instanceof Error ? err.message : t("common.actionFailed"));
    } finally {
      setSubmitting(false);
    }
  };

  if (revealed) {
    return (
      <FormDialog
        open={open}
        onOpenChange={close}
        className="sm:max-w-2xl"
        title={a("reveal.title")}
        description={a("reveal.description", { label: revealed.label, repository: revealed.repositoryName })}
        footer={<Button onClick={() => close(false)}>{a("reveal.done")}</Button>}
      >
        <Notice variant="warning" title={a("reveal.once")}>
          {a("reveal.onceBody")}
        </Notice>
        <div className="space-y-2">
          <p className="text-body font-medium">{a("reveal.url")}</p>
          <CopyableSnippet value={revealed.url} label={a("reveal.url")} copyLabel={a("reveal.copyUrl")} wrap />
          <p className="text-caption text-muted-foreground">{a("reveal.secret", { repository: revealed.repositoryName })}</p>
        </div>
        {isLocalOnly(revealed.base) && <LocalOnlyNotice base={revealed.base} />}
        <div className="space-y-2">
          <p className="text-body font-medium">{a("reveal.setup")}</p>
          <Tabs value={tool} onValueChange={(v) => isAlertTool(v) && setTool(v)} variant="pill">
            <TabsList aria-label={a("reveal.setup")} className="flex-wrap">
              {ALERT_TOOLS.map((id) => (
                <TabsTrigger key={id} value={id}>
                  {a(`tools.${id}.name`)}
                </TabsTrigger>
              ))}
            </TabsList>
            {ALERT_TOOLS.map((id) => (
              <TabsContent key={id} value={id} className="mt-3 space-y-2">
                <p className="text-caption text-muted-foreground">{a(`tools.${id}.hint`)}</p>
                <CopyableSnippet
                  value={alertSnippet(id, revealed.url, revealed.env)}
                  copyLabel={a("reveal.copySnippet", { tool: a(`tools.${id}.name`) })}
                  wrap={id === "sentry"}
                />
              </TabsContent>
            ))}
          </Tabs>
          <p className="text-caption text-muted-foreground">{a("reveal.sourceNote")}</p>
          <p className="text-caption text-muted-foreground">{a("reveal.limits")}</p>
        </div>
      </FormDialog>
    );
  }

  return (
    <FormDialog
      open={open}
      onOpenChange={close}
      title={a("form.title")}
      description={a("form.description")}
      footer={
        <>
          <Button variant="outline" onClick={() => close(false)} disabled={submitting}>
            {t("common.cancel")}
          </Button>
          <Button type="submit" form={FORM_ID} disabled={submitting || !repository}>
            {submitting ? a("form.creating") : a("form.create")}
          </Button>
        </>
      }
    >
      <form id={FORM_ID} className="space-y-4" onSubmit={(e) => void submit(e)} noValidate>
        <div className="space-y-2">
          <Label htmlFor="alert-connection-repository">{a("form.repository")}</Label>
          <Select value={repositoryId} onValueChange={setRepositoryId}>
            <SelectTrigger id="alert-connection-repository">
              <SelectValue placeholder={a("form.repositoryPlaceholder")} />
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
        <div className="space-y-2">
          <Label htmlFor="alert-connection-label">{a("form.label")}</Label>
          <Input
            id="alert-connection-label"
            value={label}
            onChange={(e) => setLabel(e.target.value)}
            placeholder={a("form.labelPlaceholder")}
            autoComplete="off"
            autoFocus
            aria-invalid={touched && problem !== undefined}
            aria-describedby="alert-connection-label-hint"
          />
          {touched && problem ? (
            <p id="alert-connection-label-hint" className="text-caption text-destructive">
              {a(problem === "required" ? "form.labelRequired" : "form.labelTooLong", { max: ALERT_LABEL_MAX })}
            </p>
          ) : (
            <p id="alert-connection-label-hint" className="text-caption text-muted-foreground">
              {a("form.labelHint", { max: ALERT_LABEL_MAX })}
            </p>
          )}
        </div>
        <div className="space-y-2">
          <Label htmlFor="alert-connection-env">{a("form.env")}</Label>
          <Select value={env} onValueChange={(v) => isAlertEnv(v) && setEnv(v)}>
            <SelectTrigger id="alert-connection-env" aria-describedby="alert-connection-env-hint">
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              {ALERT_DEFAULT_ENVS.map((e) => (
                <SelectItem key={e} value={e}>
                  {a(`envs.${e}`)} <span className="font-mono text-muted-foreground">{e}</span>
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
          <p id="alert-connection-env-hint" className="text-caption text-muted-foreground">
            {a("form.envHint")}
          </p>
        </div>
        {failure && (
          <Notice variant="error" title={a("form.failed")}>
            {failure}
          </Notice>
        )}
      </form>
    </FormDialog>
  );
}
