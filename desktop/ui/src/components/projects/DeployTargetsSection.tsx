import { ExternalLink, RefreshCw } from "lucide-react";
import { useCallback, useEffect, useState } from "react";
import { Link } from "react-router-dom";
import { toast } from "sonner";
import {
  api,
  type DeployConfigView,
  type DeployEnv,
  type DeployProvider,
  type DeployTarget,
  type DeployTemplate,
  type MobilePlatform,
  type SaveDeployTargetInput,
  type StoreCredentialProvider,
  type StoreCredentialView,
} from "@/api";
import { Accordion, AccordionContent, AccordionItem, AccordionTrigger } from "@/components/ui/accordion";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { ConfirmDialog } from "@/components/ui/confirm-dialog";
import { HelpTooltip } from "@/components/ui/help-tooltip";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Notice } from "@/components/ui/notice";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Skeleton } from "@/components/ui/skeleton";
import { Switch } from "@/components/ui/switch";
import { useI18n } from "@/hooks/useI18n";
import {
  detectLogsUrlSupport,
  isHttpUrl,
  isStoreProvider,
  logsUrlWasDropped,
  markLogsUrlUnsupported,
  providersForKind,
  templatesForKind,
  type LogsUrlSupport,
} from "@/lib/deployTargets";
import { cn } from "@/lib/utils";

// The Select value that means "no template": a target may legitimately have
// none, since it can just record where an environment answers without
// claiming to know how it got there. Radix Select cannot hold "" as an item
// value, hence the sentinel.
const NO_TEMPLATE = "__none__";

// storeCredentialProviderFor maps a deploy provider to the credential-vault
// key that gates it, or null if the provider isn't a store provider (no
// credential requirement). "google_play" matches on both axes; App Store
// Connect's deploy provider ("app_store") and credential key ("asc") differ.
function storeCredentialProviderFor(provider: DeployProvider): StoreCredentialProvider | null {
  if (provider === "app_store") return "asc";
  if (provider === "google_play") return "google_play";
  return null;
}

// Production reads as the loudest badge on the card on purpose: these three
// forms look identical and the only thing separating "restarted staging" from
// "re-pointed production" is which one you were looking at.
function envBadgeVariant(env: DeployEnv): "secondary" | "warning" | "destructive" {
  if (env === "prod") return "destructive";
  if (env === "preprod") return "warning";
  return "secondary";
}

interface EnvTargetCardProps {
  env: DeployEnv;
  repositoryId: string;
  /** "" = the repository itself. */
  subProjectPath: string;
  /** The repository's (or, when scoped, the sub-repo's) kind — hides fields
   * that don't apply, e.g. base/health/logs URLs for a worker. */
  repoKind: string;
  templates: DeployTemplate[];
  target?: DeployTarget;
  missing?: string[];
  credentials: StoreCredentialView[];
  credentialsHref: string;
  logsSupport: LogsUrlSupport;
  onLogsUrlDropped: () => void;
  onChanged: () => void;
}

/**
 * EnvTargetCard edits one environment's deploy target: where it answers, what
 * proves it is alive, which template ships it, and the variables that template
 * needs.
 *
 * Two things here are load-bearing and easy to undo by accident:
 *
 * 1. **The template is optional.** An earlier version of this form required
 *    one to enable Save, which made every target the import dialog writes
 *    (provider `custom`, no template) permanently read-only — the addresses
 *    were collected once and could never be corrected. Provider is edited
 *    directly when no template is chosen, and derived from the template when
 *    one is.
 * 2. **Save is a full upsert.** Fields this form does not show still travel in
 *    the payload, because the server writes every column from the request and
 *    an omitted field comes back cleared.
 */
function EnvTargetCard({
  env,
  repositoryId,
  subProjectPath,
  repoKind,
  templates,
  target,
  missing,
  credentials,
  credentialsHref,
  logsSupport,
  onLogsUrlDropped,
  onChanged,
}: EnvTargetCardProps) {
  const { t } = useI18n();
  const [templateID, setTemplateID] = useState(target?.template_id ?? "");
  const [provider, setProvider] = useState<DeployProvider>(target?.provider ?? "custom");
  const [vars, setVars] = useState<Record<string, string>>(() => ({ ...(target?.vars ?? {}) }));
  const [baseURL, setBaseURL] = useState(target?.base_url ?? "");
  const [healthURL, setHealthURL] = useState(target?.health_url ?? "");
  const [logsURL, setLogsURL] = useState(target?.logs_url ?? "");
  const [appPackage, setAppPackage] = useState(target?.app_package ?? "");
  const [appURL, setAppURL] = useState(target?.app_url ?? "");
  const [autoRollback, setAutoRollback] = useState(target?.auto_rollback ?? false);
  const [instructions, setInstructions] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);
  const [confirmClear, setConfirmClear] = useState(false);
  const [confirmRemove, setConfirmRemove] = useState(false);

  // Reset only when THIS env's saved record actually changes (id/updated_at),
  // not on object identity: every save refetches the whole config and rebuilds
  // all three targets, and resetting on identity wiped the other cards'
  // unsaved drafts.
  useEffect(() => {
    setTemplateID(target?.template_id ?? "");
    setProvider(target?.provider ?? "custom");
    setVars({ ...(target?.vars ?? {}) });
    setBaseURL(target?.base_url ?? "");
    setHealthURL(target?.health_url ?? "");
    setLogsURL(target?.logs_url ?? "");
    setAppPackage(target?.app_package ?? "");
    setAppURL(target?.app_url ?? "");
    setAutoRollback(target?.auto_rollback ?? false);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [target?.id, target?.updated_at]);

  const isWorker = repoKind === "worker";
  const isMobile = repoKind === "mobile";
  // Mobile ships to test tracks, not to a staging URL — "Stage"/"Pre-prod"
  // read as backend jargon on a card that actually means "internal testers"
  // or "TestFlight public beta". Local has no mobile-specific meaning and
  // stays on LocalEnvCard regardless of kind.
  const envNameKey = isMobile ? `projectAdmin.prodOps.envNamesMobile.${env}` : `projectAdmin.prodOps.envNames.${env}`;
  const envHintKey = isMobile ? `projectAdmin.prodOps.envHintsMobile.${env}` : `projectAdmin.prodOps.envHints.${env}`;
  const template = templateID ? templates.find((tpl) => tpl.id === templateID) : undefined;
  // A template pins the provider — the server rejects a mismatch outright
  // ("template X ships to Y, not Z"), so the picker is derived rather than
  // free while one is selected.
  const effectiveProvider: DeployProvider = template ? template.provider : provider;
  const storeTarget = isStoreProvider(effectiveProvider);
  const credentialProvider = storeCredentialProviderFor(effectiveProvider);
  const credentialConfigured = credentialProvider
    ? (credentials.find((c) => c.provider === credentialProvider)?.configured ?? false)
    : true;

  const urlError = (value: string) =>
    value.trim() !== "" && !isHttpUrl(value) ? t("projectAdmin.prodOps.invalidUrl") : "";
  const logsEditable = logsSupport !== "unsupported";
  // A mobile card keeps the address fields — under "advanced" — so they are
  // still validated there; a non-mobile store target hides them entirely.
  const urlsEditable = isMobile || (!storeTarget && !isWorker);
  const baseError = urlsEditable ? urlError(baseURL) : "";
  const healthError = urlsEditable ? urlError(healthURL) : "";
  const logsError = urlsEditable && logsEditable ? urlError(logsURL) : "";
  const appURLError = urlError(appURL);
  const hasInvalidURL = Boolean(baseError || healthError || logsError || appURLError);

  // App identity (bundle ID / package name) now lives in the store connection
  // panel, so app_package falls back to whatever the server already has —
  // deploy.Service.SaveTarget derives it from vars["package_name"] when this
  // is blank — rather than to a value this card no longer collects.
  const effectiveAppPackage = appPackage.trim();

  // Addresses that are recorded today and would be blank after this save.
  // Blanking health_url stops the production monitor probing this environment
  // — silently, forever — so it is confirmed rather than merely saved.
  const clearedFields: string[] = [];
  if (target) {
    if ((target.health_url ?? "") !== "" && healthURL.trim() === "") {
      clearedFields.push(t("projectAdmin.prodOps.healthUrl"));
    }
    if ((target.base_url ?? "") !== "" && baseURL.trim() === "") {
      clearedFields.push(t("projectAdmin.prodOps.baseUrl"));
    }
    if (logsEditable && (target.logs_url ?? "") !== "" && logsURL.trim() === "") {
      clearedFields.push(t("projectAdmin.prodOps.logsUrl"));
    }
  }

  const save = useCallback(async () => {
    setBusy(true);
    try {
      // Every server-known field travels, including the ones no input on this
      // card showed: a partial payload clears the rest (see
      // SaveDeployTargetInput). app_url in particular is written by CI after
      // each build, and dropping it here would send QA to last week's APK.
      const payload: SaveDeployTargetInput = {
        sub_project_path: subProjectPath,
        env,
        provider: effectiveProvider,
        // The raw state, not `template?.id`: `templates` is filtered to the
        // repo's kind, so a target whose template no longer fits (the repo was
        // retyped) resolves to undefined here while still existing in the
        // server's registry. Sending "" in that case would quietly unlink it.
        template_id: templateID,
        vars,
        base_url: baseURL.trim(),
        health_url: healthURL.trim(),
        app_package: effectiveAppPackage,
        app_url: appURL.trim(),
        auto_rollback: autoRollback,
      };
      const sentLogsURL = logsURL.trim();
      // Withheld once the server has been shown not to understand it, so a
      // stale value cannot keep round-tripping into a payload it ignores.
      if (logsEditable) payload.logs_url = sentLogsURL;

      const saved = await api.saveDeployTarget(repositoryId, payload);

      // The round trip is the only reliable probe: `logs_url` is omitempty, so
      // a server that dropped an unknown field and one that stored an empty
      // string are indistinguishable until a non-empty value fails to return.
      if (logsEditable && logsUrlWasDropped(sentLogsURL, saved)) {
        markLogsUrlUnsupported();
        onLogsUrlDropped();
        toast.warning(t("projectAdmin.prodOps.logsUrlDropped"));
      } else {
        toast.success(t("projectAdmin.prodOps.targetSaved"));
      }
      onChanged();
    } catch (err) {
      toast.error(err instanceof Error ? err.message : t("common.actionFailed"));
    } finally {
      setBusy(false);
      setConfirmClear(false);
    }
  }, [
    appURL,
    autoRollback,
    baseURL,
    effectiveAppPackage,
    effectiveProvider,
    env,
    healthURL,
    logsEditable,
    logsURL,
    onChanged,
    onLogsUrlDropped,
    repositoryId,
    subProjectPath,
    t,
    templateID,
    vars,
  ]);

  const requestSave = useCallback(() => {
    if (hasInvalidURL) {
      toast.error(t("projectAdmin.prodOps.invalidUrlToast"));
      return;
    }
    if (clearedFields.length > 0) {
      setConfirmClear(true);
      return;
    }
    void save();
  }, [clearedFields.length, hasInvalidURL, save, t]);

  const remove = useCallback(async () => {
    setBusy(true);
    try {
      await api.deleteDeployTarget(repositoryId, env, subProjectPath);
      toast.success(t("projectAdmin.prodOps.targetRemoved"));
      onChanged();
    } catch (err) {
      toast.error(err instanceof Error ? err.message : t("common.actionFailed"));
    } finally {
      setBusy(false);
      setConfirmRemove(false);
    }
  }, [env, onChanged, repositoryId, subProjectPath, t]);

  const createTask = useCallback(async () => {
    setBusy(true);
    try {
      const task = await api.createDeploySetupTask(repositoryId, env);
      toast.success(t("projectAdmin.prodOps.setupTaskCreated", { key: task.key }));
    } catch (err) {
      toast.error(err instanceof Error ? err.message : t("common.actionFailed"));
    } finally {
      setBusy(false);
    }
  }, [env, repositoryId, t]);

  const showInstructions = useCallback(async () => {
    try {
      const res = await api.getDeployInstructions(repositoryId, env);
      setInstructions(res.instructions);
    } catch (err) {
      toast.error(err instanceof Error ? err.message : t("common.actionFailed"));
    }
  }, [env, repositoryId, t]);

  const urlField = (
    id: string,
    label: string,
    hint: string,
    value: string,
    onChange: (next: string) => void,
    error: string,
    options?: { disabled?: boolean; placeholder?: string },
  ) => (
    <div className="space-y-1">
      <Label htmlFor={id}>{label}</Label>
      <Input
        id={id}
        type="url"
        inputMode="url"
        value={value}
        disabled={options?.disabled}
        aria-invalid={error ? true : undefined}
        placeholder={options?.placeholder}
        onChange={(e) => onChange(e.target.value)}
        className={cn(error && "border-destructive focus-visible:ring-destructive")}
      />
      {error ? (
        <p className="text-xs text-destructive">{error}</p>
      ) : (
        <p className="text-xs text-muted-foreground">{hint}</p>
      )}
    </div>
  );

  const addressFields = (
    <>
      {urlField(
        `base-${env}`,
        t("projectAdmin.prodOps.baseUrl"),
        t("projectAdmin.prodOps.baseUrlHint"),
        baseURL,
        setBaseURL,
        baseError,
        { placeholder: "https://api.example.com" },
      )}
      {urlField(
        `health-${env}`,
        t("projectAdmin.prodOps.healthUrl"),
        t("projectAdmin.prodOps.healthUrlHint"),
        healthURL,
        setHealthURL,
        healthError,
        { placeholder: "https://api.example.com/health" },
      )}
      {/* Rendered in the `unknown` state too: on a server that does know
          the field but has nothing stored yet, `unknown` and "older
          server" look identical, and hiding it there would leave the
          field permanently unreachable. Only a proven drop disables it. */}
      {urlField(
        `logs-${env}`,
        t("projectAdmin.prodOps.logsUrl"),
        logsEditable ? t("projectAdmin.prodOps.logsUrlHint") : t("projectAdmin.prodOps.logsUrlUnsupported"),
        logsURL,
        setLogsURL,
        logsError,
        { disabled: !logsEditable, placeholder: "https://api.example.com/logs" },
      )}
    </>
  );

  const artifactFields = (
    <>
      <div className="space-y-1">
        <Label htmlFor={`pkg-${env}`}>{t("projectAdmin.prodOps.appPackage")}</Label>
        <Input
          id={`pkg-${env}`}
          value={appPackage}
          placeholder="ai.tasktrooper.app.stage"
          onChange={(e) => setAppPackage(e.target.value)}
        />
        <p className="text-xs text-muted-foreground">{t("projectAdmin.prodOps.appPackageHint")}</p>
      </div>
      {urlField(
        `appurl-${env}`,
        t("projectAdmin.prodOps.appUrl"),
        t("projectAdmin.prodOps.appUrlHint"),
        appURL,
        setAppURL,
        appURLError,
        { placeholder: "https://ci.example.com/build.apk" },
      )}
    </>
  );

  return (
    <Card>
      <CardHeader>
        <div className="flex items-center justify-between gap-2">
          <CardTitle className="text-base">{t(envNameKey)}</CardTitle>
          <Badge variant={envBadgeVariant(env)} className="font-mono text-micro">
            {env}
          </Badge>
        </div>
        <CardDescription>{t(envHintKey)}</CardDescription>
        <div className="flex flex-wrap items-center gap-2 pt-1">
          {target ? (
            <Badge variant="outline">{t(`projectAdmin.prodOps.providers.${target.provider}`)}</Badge>
          ) : (
            <Badge variant="outline">{t("projectAdmin.prodOps.notConfigured")}</Badge>
          )}
        </div>
        {missing && missing.length > 0 && (
          <CardDescription className="text-warning">
            {t("projectAdmin.prodOps.missingVars", { keys: missing.join(", ") })}
          </CardDescription>
        )}
      </CardHeader>
      <CardContent className="space-y-3">
        <div className="space-y-1">
          <Label htmlFor={`tpl-${env}`}>{t("projectAdmin.prodOps.template")}</Label>
          <Select
            value={templateID || NO_TEMPLATE}
            onValueChange={(v) => setTemplateID(v === NO_TEMPLATE ? "" : v)}
          >
            <SelectTrigger id={`tpl-${env}`}>
              <SelectValue placeholder={t("projectAdmin.prodOps.template")} />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value={NO_TEMPLATE}>{t("projectAdmin.prodOps.templateNone")}</SelectItem>
              {/* A target can carry a template that no longer fits the repo's
                  kind (the repo was retyped, or the target predates it). Keep
                  it selectable so opening the form does not silently rewrite
                  what is stored. */}
              {templateID && !templates.some((tpl) => tpl.id === templateID) && (
                <SelectItem value={templateID}>{templateID}</SelectItem>
              )}
              {templates.map((tpl) => (
                <SelectItem key={tpl.id} value={tpl.id}>
                  {tpl.name}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
          <p className="text-xs text-muted-foreground">
            {template ? template.summary : t("projectAdmin.prodOps.templateNoneHint")}
          </p>
        </div>

        <div className="space-y-1">
          <Label htmlFor={`provider-${env}`}>{t("projectAdmin.prodOps.provider")}</Label>
          <Select
            value={effectiveProvider}
            disabled={Boolean(template)}
            onValueChange={(v) => setProvider(v as DeployProvider)}
          >
            <SelectTrigger id={`provider-${env}`}>
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              {/* Same safety net as the template select above: a provider a
                  repo carries from before it had this kind (or from a kind
                  change) stays selectable rather than rendering blank. */}
              {!providersForKind(repoKind).includes(effectiveProvider) && (
                <SelectItem value={effectiveProvider}>
                  {t(`projectAdmin.prodOps.providers.${effectiveProvider}`)}
                </SelectItem>
              )}
              {providersForKind(repoKind).map((p) => (
                <SelectItem key={p} value={p}>
                  {t(`projectAdmin.prodOps.providers.${p}`)}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
          {template && (
            <p className="text-xs text-muted-foreground">{t("projectAdmin.prodOps.providerFromTemplate")}</p>
          )}
        </div>

        {template && (template.required_vars ?? []).length > 0 && (
          <div className="space-y-2">
            <Label>{t("projectAdmin.prodOps.variables")}</Label>
            {(template.required_vars ?? []).map((v) => (
              <div key={v.key} className="space-y-1">
                <Label htmlFor={`${env}-${v.key}`} className="text-xs font-normal text-muted-foreground">
                  {v.label}
                </Label>
                <Input
                  id={`${env}-${v.key}`}
                  value={vars[v.key] ?? ""}
                  placeholder={v.example}
                  onChange={(e) => setVars((prev) => ({ ...prev, [v.key]: e.target.value }))}
                />
              </div>
            ))}
          </div>
        )}

        {isWorker && (
          <Notice variant="info" title={t("projectAdmin.prodOps.workerUrlsNotImplementedTitle")}>
            {t("projectAdmin.prodOps.workerUrlsNotImplemented")}
          </Notice>
        )}

        {isMobile ? (
          // App identity (bundle ID / package name) is set from the store
          // connection panel, not this card — only the build address and
          // artifact location stay editable here, under "advanced".
          <Accordion type="single">
            <AccordionItem value="advanced">
              <AccordionTrigger>{t("projectAdmin.prodOps.advanced")}</AccordionTrigger>
              <AccordionContent className="space-y-3 text-foreground">
                {addressFields}
                {artifactFields}
              </AccordionContent>
            </AccordionItem>
          </Accordion>
        ) : (
          <>
            {!storeTarget && !isWorker && addressFields}
            {(storeTarget || appPackage || appURL) && artifactFields}
          </>
        )}

        <div className="flex items-center justify-between gap-3">
          <div className="flex items-center gap-1.5">
            <Label htmlFor={`rollback-${env}`}>{t("projectAdmin.prodOps.autoRollback")}</Label>
            <HelpTooltip text={t("projectAdmin.prodOps.autoRollbackHint")} />
          </div>
          <Switch id={`rollback-${env}`} checked={autoRollback} onCheckedChange={setAutoRollback} />
        </div>

        {credentialProvider && !credentialConfigured && (
          <p className="text-xs text-warning">
            {t("projectAdmin.prodOps.storeCredentialRequired")}{" "}
            {/* The vault lives on the deploy settings page. Mounted there this
                is a same-page jump; mounted on repository settings it has to
                be a route, or it would point at an anchor that page has not
                got. */}
            {credentialsHref.startsWith("#") ? (
              <a href={credentialsHref} className="underline">
                {t("projectAdmin.prodOps.storeCredentialRequiredLink")}
              </a>
            ) : (
              <Link to={credentialsHref} className="underline">
                {t("projectAdmin.prodOps.storeCredentialRequiredLink")}
              </Link>
            )}
          </p>
        )}

        <div className="flex flex-wrap gap-2">
          <Button size="sm" disabled={busy || !credentialConfigured} onClick={requestSave}>
            {t("projectAdmin.prodOps.saveTarget")}
          </Button>
          {target && (
            <>
              {!subProjectPath && (
                <>
                  <Button size="sm" variant="outline" disabled={busy} onClick={() => void createTask()}>
                    {t("projectAdmin.prodOps.setupTask")}
                  </Button>
                  <Button size="sm" variant="ghost" disabled={busy} onClick={() => void showInstructions()}>
                    {t("projectAdmin.prodOps.instructions")}
                  </Button>
                </>
              )}
              <Button size="sm" variant="ghost" disabled={busy} onClick={() => setConfirmRemove(true)}>
                {t("projectAdmin.prodOps.removeTarget")}
              </Button>
            </>
          )}
        </div>

        {instructions && (
          <pre className="max-h-72 overflow-auto whitespace-pre-wrap rounded-md bg-muted p-3 text-xs">
            {instructions}
          </pre>
        )}
      </CardContent>

      <ConfirmDialog
        open={confirmClear}
        onOpenChange={setConfirmClear}
        title={t("projectAdmin.prodOps.clearUrlTitle")}
        description={t("projectAdmin.prodOps.clearUrlDesc", {
          env: t(envNameKey),
          fields: clearedFields.join(", "),
        })}
        confirmLabel={t("projectAdmin.prodOps.clearUrlConfirm")}
        variant="destructive"
        loading={busy}
        onConfirm={() => void save()}
      />

      <ConfirmDialog
        open={confirmRemove}
        onOpenChange={setConfirmRemove}
        title={t("projectAdmin.prodOps.removeTargetTitle")}
        description={t("projectAdmin.prodOps.removeTargetDesc", {
          env: t(envNameKey),
        })}
        confirmLabel={t("projectAdmin.prodOps.removeTarget")}
        loading={busy}
        onConfirm={() => void remove()}
      />
    </Card>
  );
}

interface LocalEnvCardProps {
  repositoryId: string;
  subProjectPath: string;
}

/**
 * LocalEnvCard replaces EnvTargetCard for the "local" environment: local isn't
 * a real address (see envHints.local), so the base/health/logs URL fields
 * EnvTargetCard shows for every other env make no sense here. Instead this
 * offers to generate a LOCAL_SETUP.md + run script for the repository (or
 * sub-project) via a board task, matching the filenames CreateLocalSetupTask
 * asks the agent to write.
 */
function LocalEnvCard({ repositoryId, subProjectPath }: LocalEnvCardProps) {
  const { t } = useI18n();
  const [busy, setBusy] = useState(false);

  const dir = subProjectPath ? `${subProjectPath}/` : "";
  const docPath = `${dir}LOCAL_SETUP.md`;
  const scriptPath = `${dir}scripts/dev.sh`;

  const createTask = useCallback(async () => {
    setBusy(true);
    try {
      const task = await api.createLocalSetupTask(repositoryId, subProjectPath || undefined);
      toast.success(t("projectAdmin.prodOps.localSetupTaskCreated", { key: task.key }));
    } catch (err) {
      toast.error(err instanceof Error ? err.message : t("common.actionFailed"));
    } finally {
      setBusy(false);
    }
  }, [repositoryId, subProjectPath, t]);

  return (
    <Card>
      <CardHeader>
        <div className="flex items-center justify-between gap-2">
          <CardTitle className="text-base">{t("projectAdmin.prodOps.envNames.local")}</CardTitle>
          <Badge variant="secondary" className="font-mono text-micro">
            local
          </Badge>
        </div>
        <CardDescription>{t("projectAdmin.prodOps.envHints.local")}</CardDescription>
      </CardHeader>
      <CardContent className="space-y-3">
        <Notice variant="info" title={t("projectAdmin.prodOps.localNoteTitle")}>
          {t("projectAdmin.prodOps.localNote", { doc: docPath, script: scriptPath })}
        </Notice>
        <Button size="sm" disabled={busy} onClick={() => void createTask()}>
          {t("projectAdmin.prodOps.localSetupTask")}
        </Button>
      </CardContent>
    </Card>
  );
}

interface DeployTargetsSectionProps {
  repositoryId: string;
  /**
   * Store credentials that gate App Store / Google Play targets. Pass them
   * when the host page owns the credential vault (DeploySettingsPage renders
   * it below and must not hold a second, stale copy); omit and the section
   * fetches its own read-only copy.
   */
  credentials?: StoreCredentialView[];
  /** Extra classes on the outer Card — grid spans, mostly. */
  className?: string;
  /** Where the rest of the deploy settings live. Omitted, no link is shown. */
  moreHref?: string;
  /** Scopes every target to one monorepo sub-project. "" (default) = the
   * repository itself. */
  subProjectPath?: string;
  /** Overrides the card's heading — used to label which sub-project this is. */
  title?: string;
  /** The kind that decides which fields apply. Pass it when the host page knows
   * the sub-repo's kind; omitted, the deploy config's own kind is used. */
  kind?: string;
  /** Which stores a mobile kind ships to. Accepted for caller compatibility;
   * this section no longer reads it — App Store / Google Play identity now
   * lives in the store connection panel, not here. */
  mobilePlatform?: MobilePlatform;
}

/**
 * DeployTargetsSection — every environment this repository ships to, with its
 * addresses, editable in place.
 *
 * It exists as a component rather than as part of DeploySettingsPage because
 * these facts were collected once, by the import dialog, and then had nowhere
 * to be seen or corrected: the repository settings page never showed them and
 * the deploy page was reachable only from the operations matrix. Both pages
 * mount this, so there is one editor rather than two that drift.
 */
export function DeployTargetsSection({
  repositoryId,
  credentials,
  className,
  moreHref,
  subProjectPath = "",
  title,
  kind,
  mobilePlatform: _mobilePlatform = "",
}: DeployTargetsSectionProps) {
  const { t } = useI18n();
  const [config, setConfig] = useState<DeployConfigView | null>(null);
  const [ownCredentials, setOwnCredentials] = useState<StoreCredentialView[]>([]);
  const [logsSupport, setLogsSupport] = useState<LogsUrlSupport>("unknown");
  const [loading, setLoading] = useState(true);
  const [failed, setFailed] = useState(false);

  const ownsCredentials = credentials === undefined;

  const load = useCallback(async () => {
    if (!repositoryId) return;
    setLoading(true);
    try {
      const cfg = await api.getDeployConfig(repositoryId, subProjectPath || undefined);
      setConfig(cfg);
      setLogsSupport(detectLogsUrlSupport(cfg.targets ?? []));
      setFailed(false);
    } catch (err) {
      setFailed(true);
      toast.error(err instanceof Error ? err.message : t("projectAdmin.prodOps.deployLoadFailed"));
    } finally {
      setLoading(false);
    }
  }, [repositoryId, subProjectPath, t]);

  useEffect(() => {
    void load();
  }, [load]);

  useEffect(() => {
    if (!ownsCredentials) return;
    // Gating only — a failure here leaves store targets un-saveable, which is
    // the safe direction, and must not blank the section.
    void api
      .listStoreCredentials()
      .then(setOwnCredentials)
      .catch(() => setOwnCredentials([]));
  }, [ownsCredentials]);

  const repoKind = kind ?? config?.kind ?? "";
  const isMobileConfig = repoKind === "mobile";
  const templates = templatesForKind(config?.templates ?? [], repoKind);
  const targets = config?.targets ?? [];
  const envs = config?.envs ?? [];

  return (
    <Card className={cn("w-full space-y-4 p-6", className)}>
      <div className="flex flex-wrap items-start justify-between gap-3">
        <div>
          <h2 className="font-semibold">{title ?? t("projectAdmin.prodOps.deployTitle")}</h2>
          <p className="text-sm text-muted-foreground">{t("projectAdmin.prodOps.deploySubtitle")}</p>
        </div>
        <div className="flex shrink-0 items-center gap-2">
          <Button variant="outline" size="sm" onClick={() => void load()} disabled={loading}>
            <RefreshCw className={cn("mr-2 h-4 w-4", loading && "animate-spin")} />
            {t("common.refresh")}
          </Button>
          {moreHref && (
            <Button variant="outline" size="sm" asChild className="gap-2">
              <Link to={moreHref}>
                <ExternalLink className="h-4 w-4" />
                {t("projectAdmin.prodOps.openDeploySettings")}
              </Link>
            </Button>
          )}
        </div>
      </div>

      {logsSupport === "unsupported" && (
        <Notice variant="info" title={t("projectAdmin.prodOps.logsUrlUnsupportedTitle")}>
          {t("projectAdmin.prodOps.logsUrlUnsupported")}
        </Notice>
      )}

      {loading ? (
        <div className="grid gap-4 lg:grid-cols-3">
          <Skeleton className="h-72 w-full" />
          <Skeleton className="h-72 w-full" />
          <Skeleton className="h-72 w-full" />
        </div>
      ) : failed ? (
        <p className="text-sm text-muted-foreground">{t("projectAdmin.prodOps.deployLoadFailed")}</p>
      ) : (
        <>
          {isMobileConfig && (
            <Notice variant="info" title={t("projectAdmin.prodOps.mobileStoreManagedTitle")}>
              {t("projectAdmin.prodOps.mobileStoreManagedNote")}
            </Notice>
          )}
          {/* items-start: the environments differ wildly in height (local has a
              notice, an unconfigured one a short form) and a stretched row left
              a column of empty card under the shortest. */}
          <div className="grid items-start gap-4 lg:grid-cols-3">
            {envs.map((env) =>
              env === "local" ? (
                <LocalEnvCard key={env} repositoryId={repositoryId} subProjectPath={subProjectPath} />
              ) : (
                <EnvTargetCard
                  key={env}
                  env={env}
                  repositoryId={repositoryId}
                  subProjectPath={subProjectPath}
                  repoKind={repoKind}
                  templates={templates}
                  target={targets.find((target) => target.env === env)}
                  missing={config?.missing?.[env]}
                  credentials={credentials ?? ownCredentials}
                  credentialsHref={moreHref ? `${moreHref}#store-credentials` : "#store-credentials"}
                  logsSupport={logsSupport}
                  onLogsUrlDropped={() => setLogsSupport("unsupported")}
                  onChanged={() => void load()}
                />
              ),
            )}
          </div>
        </>
      )}
    </Card>
  );
}
