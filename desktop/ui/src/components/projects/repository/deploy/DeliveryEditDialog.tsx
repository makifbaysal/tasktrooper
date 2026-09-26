import { useState } from "react";
import { toast } from "sonner";
import {
  api,
  DELIVERY_MODES,
  MAX_SMOKE_LATENCY_MS,
  MAX_SOAK_MINUTES,
  type Component,
  type ComponentDelivery,
  type ComponentEnvironment,
  type DeliveryExecutor,
  type DeliveryMode,
  type SmokeCheck,
} from "@/api";
import { FormDialog } from "@/components/admin/FormDialog";
import { MAX_SMOKE_NAME_LEN, SmokeChecksEditor } from "@/components/projects/repository/deploy/SmokeChecksEditor";
import { Button } from "@/components/ui/button";
import { ConfirmDialog } from "@/components/ui/confirm-dialog";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Switch } from "@/components/ui/switch";
import { useI18n } from "@/hooks/useI18n";

// The mode a component ships under decides which executors even make sense —
// dispatching a batch store release through GitHub Actions is fine, but
// "on_merge" through a store executor has no meaning (nothing merges an app
// store release). Mirrors domain.ComponentDelivery.Validate's mode/executor
// pairing.
const EXECUTORS_FOR_MODE: Record<DeliveryMode, DeliveryExecutor[]> = {
  on_merge: ["github_actions", "vercel"],
  dispatch: ["github_actions"],
  batch: ["github_actions", "local", "store"],
  none: [],
};

function defaultDelivery(production?: ComponentEnvironment | null): ComponentDelivery {
  return production?.provider === "vercel"
    ? {
        mode: "on_merge",
        executor: "vercel",
        verify: { soak_minutes: 10, max_new_errors: 0, smoke: [] },
        auto_rollback: true,
      }
    : {
        mode: "on_merge",
        executor: "github_actions",
        verify: { soak_minutes: 10, max_new_errors: 0, smoke: [] },
        auto_rollback: true,
      };
}

interface DeliveryEditDialogProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  componentId: string;
  /** The effective profile to seed the form with (override, or detected, or none yet). */
  current: ComponentDelivery | null;
  /** Only an override can be reset — there is nothing to fall back to otherwise. */
  hasOverride: boolean;
  /** The confirmed production environment, when this component is coupled to one. */
  production?: ComponentEnvironment | null;
  onSaved: (component: Component) => void;
}

/**
 * Edits one component's delivery override. Mode/executor/verify fields
 * mirror domain.ComponentDelivery exactly; validation mirrors
 * ComponentDelivery.Validate so a mistake is caught before the round trip,
 * and the server's own 400 message is shown verbatim for whatever this pass
 * misses.
 */
export function DeliveryEditDialog({ open, onOpenChange, componentId, current, hasOverride, production = null, onSaved }: DeliveryEditDialogProps) {
  const { t } = useI18n();
  const [draft, setDraft] = useState<ComponentDelivery>(() => current ?? defaultDelivery(production));
  const [saving, setSaving] = useState(false);
  const [resetOpen, setResetOpen] = useState(false);
  const [resetting, setResetting] = useState(false);
  const [errors, setErrors] = useState<string[]>([]);

  const setMode = (mode: DeliveryMode) => {
    const executors = EXECUTORS_FOR_MODE[mode];
    setDraft((d) => ({
      ...d,
      mode,
      executor: executors.length === 0 ? undefined : executors.includes(d.executor as DeliveryExecutor) ? d.executor : executors[0],
    }));
  };

  const setSmoke = (next: SmokeCheck[]) => setDraft((d) => ({ ...d, verify: { ...d.verify, smoke: next } }));

  const validate = (d: ComponentDelivery): string[] => {
    const out: string[] = [];
    if (d.mode === "none") return out;
    if (d.mode === "dispatch" && !(d.workflow ?? "").trim()) {
      out.push(t("release.deliveryEdit.errors.workflowRequired"));
    }
    if ((d.workflow ?? "").includes("/")) {
      out.push(t("release.deliveryEdit.errors.workflowNoSlash"));
    }
    if (d.mode === "batch" && d.executor === "local" && !(d.local_command ?? "").trim()) {
      out.push(t("release.deliveryEdit.errors.localCommandRequired"));
    }
    const soak = d.verify.soak_minutes ?? 0;
    if (soak < 1 || soak > MAX_SOAK_MINUTES) {
      out.push(t("release.deliveryEdit.errors.soakRange", { max: MAX_SOAK_MINUTES }));
    }
    if ((d.verify.max_new_errors ?? 0) < 0) {
      out.push(t("release.deliveryEdit.errors.maxNewErrorsNegative"));
    }
    for (const c of d.verify.smoke ?? []) {
      if (c.method !== "GET" && c.method !== "HEAD") {
        out.push(t("release.deliveryEdit.errors.smokeMethod"));
      }
      const path = c.path.trim();
      const isAbsolute = path.includes("://");
      if (!path || (!isAbsolute && !path.startsWith("/"))) {
        out.push(t("release.deliveryEdit.errors.smokePath"));
      }
      if (c.expect_status && (c.expect_status < 100 || c.expect_status > 599)) {
        out.push(t("release.deliveryEdit.errors.smokeExpectStatus"));
      }
      if ((c.name?.length ?? 0) > MAX_SMOKE_NAME_LEN) {
        out.push(t("release.deliveryEdit.errors.smokeName", { max: MAX_SMOKE_NAME_LEN }));
      }
      if (c.max_latency_ms !== undefined && (c.max_latency_ms < 1 || c.max_latency_ms > MAX_SMOKE_LATENCY_MS)) {
        out.push(t("release.deliveryEdit.errors.smokeLatency", { max: MAX_SMOKE_LATENCY_MS }));
      }
    }
    return Array.from(new Set(out));
  };

  const submit = async () => {
    const problems = validate(draft);
    setErrors(problems);
    if (problems.length > 0) return;
    setSaving(true);
    try {
      const saved = await api.updateComponentDelivery(componentId, draft);
      toast.success(t("release.deliveryEdit.saved"));
      onSaved(saved);
    } catch (e) {
      const message = e instanceof Error ? e.message : t("release.delivery.saveFailed");
      setErrors([message]);
      toast.error(message);
    } finally {
      setSaving(false);
    }
  };

  const resetToDetected = async () => {
    setResetting(true);
    try {
      const saved = await api.updateComponentDelivery(componentId, null);
      toast.success(t("release.deliveryEdit.resetDone"));
      onSaved(saved);
    } catch (e) {
      toast.error(e instanceof Error ? e.message : t("release.delivery.saveFailed"));
    } finally {
      setResetting(false);
      setResetOpen(false);
    }
  };

  const executorOptions = EXECUTORS_FOR_MODE[draft.mode];
  const showWorkflow = draft.mode === "on_merge" || draft.mode === "dispatch";
  const showTagPattern = draft.mode === "batch" && draft.executor === "github_actions";
  const showLocalCommand = draft.mode === "batch" && draft.executor === "local";
  const showVerify = draft.mode !== "none";

  return (
    <>
      <FormDialog
        open={open}
        onOpenChange={onOpenChange}
        title={current ? t("release.deliveryEdit.editTitle") : t("release.deliveryEdit.newTitle")}
        description={t("release.deliveryEdit.description")}
        className="sm:max-w-2xl"
        footer={
          <>
            {hasOverride && (
              <Button variant="outline" onClick={() => setResetOpen(true)} disabled={saving}>
                {t("release.deliveryEdit.resetToDetected")}
              </Button>
            )}
            <Button variant="outline" onClick={() => onOpenChange(false)} disabled={saving}>
              {t("common.cancel")}
            </Button>
            <Button onClick={() => void submit()} disabled={saving}>
              {saving ? t("release.deliveryEdit.saving") : t("release.deliveryEdit.save")}
            </Button>
          </>
        }
      >
        {errors.length > 0 && (
          <ul className="space-y-1 rounded-md border border-destructive/40 bg-destructive/10 px-3 py-2 text-caption text-destructive">
            {errors.map((err) => (
              <li key={err}>{err}</li>
            ))}
          </ul>
        )}

        <div className="grid gap-4 sm:grid-cols-2">
          <div className="space-y-1.5">
            <Label>{t("release.deliveryEdit.mode")}</Label>
            <Select value={draft.mode} onValueChange={(v) => setMode(v as DeliveryMode)}>
              <SelectTrigger>
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                {DELIVERY_MODES.map((m) => (
                  <SelectItem key={m} value={m}>
                    {t(`release.modes.${m}`)}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          </div>

          {executorOptions.length > 0 && (
            <div className="space-y-1.5">
              <Label>{t("release.deliveryEdit.executor")}</Label>
              <Select value={draft.executor ?? ""} onValueChange={(v) => setDraft((d) => ({ ...d, executor: v as DeliveryExecutor }))}>
                <SelectTrigger>
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  {executorOptions.map((ex) => (
                    <SelectItem key={ex} value={ex}>
                      {t(`release.executors.${ex}`)}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </div>
          )}
        </div>

        {draft.executor === "vercel" && (
          <p className="text-caption text-muted-foreground">
            {production?.provider === "vercel"
              ? t("release.deliveryEdit.vercelProjectFromProd", { name: production.resource?.name ?? production.url ?? "" })
              : t("release.deliveryEdit.vercelNoProdWarning")}
          </p>
        )}

        {showWorkflow && (
          <div className="space-y-1.5">
            <Label htmlFor="delivery-workflow">{t("release.deliveryEdit.workflow")}</Label>
            <Input
              id="delivery-workflow"
              value={draft.workflow ?? ""}
              onChange={(e) => setDraft((d) => ({ ...d, workflow: e.target.value }))}
              placeholder={t("release.deliveryEdit.workflowPlaceholder")}
              className="font-mono"
            />
            <p className="text-micro text-muted-foreground">{t("release.deliveryEdit.workflowHelp")}</p>
          </div>
        )}

        {showTagPattern && (
          <div className="space-y-1.5">
            <Label htmlFor="delivery-tag-pattern">{t("release.deliveryEdit.tagPattern")}</Label>
            <Input
              id="delivery-tag-pattern"
              value={draft.tag_pattern ?? ""}
              onChange={(e) => setDraft((d) => ({ ...d, tag_pattern: e.target.value }))}
              placeholder={t("release.deliveryEdit.tagPatternPlaceholder")}
              className="font-mono"
            />
          </div>
        )}

        {showLocalCommand && (
          <div className="space-y-1.5">
            <Label htmlFor="delivery-local-command">{t("release.deliveryEdit.localCommand")}</Label>
            <Input
              id="delivery-local-command"
              value={draft.local_command ?? ""}
              onChange={(e) => setDraft((d) => ({ ...d, local_command: e.target.value }))}
              placeholder={t("release.deliveryEdit.localCommandPlaceholder")}
              className="font-mono"
            />
          </div>
        )}

        {showVerify && (
          <>
            <div className="grid gap-4 sm:grid-cols-2">
              <div className="space-y-1.5">
                <Label htmlFor="delivery-soak">{t("release.deliveryEdit.soakMinutes")}</Label>
                <Input
                  id="delivery-soak"
                  type="number"
                  min={1}
                  max={MAX_SOAK_MINUTES}
                  value={draft.verify.soak_minutes ?? ""}
                  onChange={(e) =>
                    setDraft((d) => ({ ...d, verify: { ...d.verify, soak_minutes: e.target.value === "" ? undefined : Number(e.target.value) } }))
                  }
                />
              </div>
              <div className="space-y-1.5">
                <Label htmlFor="delivery-max-errors">{t("release.deliveryEdit.maxNewErrors")}</Label>
                <Input
                  id="delivery-max-errors"
                  type="number"
                  min={0}
                  value={draft.verify.max_new_errors ?? 0}
                  onChange={(e) =>
                    setDraft((d) => ({ ...d, verify: { ...d.verify, max_new_errors: e.target.value === "" ? 0 : Number(e.target.value) } }))
                  }
                />
              </div>
            </div>

            <SmokeChecksEditor
              checks={draft.verify.smoke ?? []}
              onChange={setSmoke}
              componentId={componentId}
              baseUrl={production?.url}
            />

            <div className="flex items-center justify-between gap-3 rounded-md border border-border p-3">
              <div>
                <p className="text-body">{t("release.deliveryEdit.autoRollback")}</p>
                <p className="text-micro text-muted-foreground">{t("release.deliveryEdit.autoRollbackHelp")}</p>
              </div>
              <Switch checked={draft.auto_rollback} onCheckedChange={(checked) => setDraft((d) => ({ ...d, auto_rollback: checked }))} />
            </div>
          </>
        )}
      </FormDialog>

      <ConfirmDialog
        open={resetOpen}
        onOpenChange={setResetOpen}
        title={t("release.deliveryEdit.resetConfirmTitle")}
        description={t("release.deliveryEdit.resetConfirmDesc")}
        confirmLabel={t("release.deliveryEdit.resetToDetected")}
        loading={resetting}
        onConfirm={resetToDetected}
      />
    </>
  );
}
