import { useState } from "react";
import { toast } from "sonner";
import { api, type CloudResource, type ComponentEnvironment } from "@/api";
import { CloudAccountDialog } from "@/components/admin/CloudAccountDialog";
import { Button } from "@/components/ui/button";
import { ProviderIcon } from "@/components/projects/model/ProviderIcon";
import { useI18n } from "@/hooks/useI18n";
import { cn } from "@/lib/utils";

function useAction(onChanged: () => void) {
  const { t } = useI18n();
  const [busy, setBusy] = useState(false);
  const run = async (fn: () => Promise<unknown>) => {
    setBusy(true);
    try {
      await fn();
      onChanged();
    } catch (e) {
      toast.error(e instanceof Error ? e.message : t("projectModel.review.failed"));
    } finally {
      setBusy(false);
    }
  };
  return { busy, run };
}

interface EnvironmentCandidatesProps {
  env: ComponentEnvironment;
  onChanged: () => void;
  className?: string;
}

/**
 * Answers one ambiguous environment binding: a radio list of matching
 * `env.candidates` ("Use this" confirms one), or — once there are none left
 * and no account is connected for `env.provider` — a prompt to connect one.
 * Shared by `ReviewList`'s environment review item and the Deploy & Runtime
 * tab's Environments card, so a suggested row answers the same way in both
 * places.
 */
export function EnvironmentCandidates({ env, onChanged, className }: EnvironmentCandidatesProps) {
  const { t } = useI18n();
  const { busy, run } = useAction(onChanged);
  const [selected, setSelected] = useState("");
  const [dialogOpen, setDialogOpen] = useState(false);
  const [promptDismissed, setPromptDismissed] = useState(false);

  const candidates = env.candidates ?? [];
  const hasAccount = Boolean(env.account_id);
  const showConnectPrompt = candidates.length === 0 && !hasAccount && Boolean(env.provider) && !promptDismissed;

  const pick = (candidate: CloudResource) =>
    run(() =>
      api.patchEnvironment(env.id, {
        status: "confirmed",
        account_id: candidate.account_id,
        resource: candidate.ref,
      }),
    );

  const dismiss = () => run(() => api.patchEnvironment(env.id, { status: "dismissed" }));

  if (candidates.length > 0) {
    return (
      <div className={cn("flex flex-col gap-2", className)}>
        <fieldset className="rounded-lg border border-border">
          <legend className="sr-only">{t("cloud.review.candidatesLabel")}</legend>
          <div className="divide-y divide-border">
            {candidates.map((candidate) => (
              <label
                key={`${candidate.ref.kind}:${candidate.ref.id}`}
                className="flex cursor-pointer items-start gap-3 px-3 py-2 transition-colors hover:bg-muted/50 has-[:checked]:bg-muted"
              >
                <input
                  type="radio"
                  name={`env-candidate-${env.id}`}
                  className="mt-1 h-4 w-4 shrink-0 accent-primary focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
                  checked={selected === candidate.ref.id}
                  onChange={() => setSelected(candidate.ref.id)}
                />
                <div className="flex min-w-0 flex-1 items-center gap-2">
                  <ProviderIcon provider={candidate.provider} className="shrink-0" />
                  <div className="min-w-0">
                    <p className="truncate text-sm font-medium">{candidate.ref.name}</p>
                    <p className="truncate text-xs text-muted-foreground">
                      {[candidate.ref.region, candidate.url].filter(Boolean).join(" · ")}
                    </p>
                  </div>
                </div>
              </label>
            ))}
          </div>
        </fieldset>
        <div className="flex flex-wrap gap-1.5">
          <Button
            size="sm"
            disabled={busy || !selected}
            onClick={() => {
              const candidate = candidates.find((c) => c.ref.id === selected);
              if (candidate) void pick(candidate);
            }}
          >
            {t("cloud.review.useThis")}
          </Button>
          <Button size="sm" variant="ghost" disabled={busy} onClick={() => void dismiss()}>
            {t("projectModel.review.dismiss")}
          </Button>
        </div>
      </div>
    );
  }

  if (showConnectPrompt) {
    return (
      <div className={cn("flex flex-col gap-2", className)}>
        <p className="text-caption text-muted-foreground">
          {t("cloud.review.connectPrompt", { provider: t(`cloud.providers.${env.provider}`) })}
        </p>
        <div className="flex flex-wrap gap-1.5">
          <Button size="sm" onClick={() => setDialogOpen(true)}>
            {t("cloud.review.connectAction")}
          </Button>
          <Button size="sm" variant="outline" onClick={() => setPromptDismissed(true)}>
            {t("cloud.review.notNow")}
          </Button>
          <Button size="sm" variant="ghost" disabled={busy} onClick={() => void dismiss()}>
            {t("projectModel.review.dismiss")}
          </Button>
        </div>
        {env.provider && (
          <CloudAccountDialog
            open={dialogOpen}
            onOpenChange={setDialogOpen}
            provider={env.provider}
            onSaved={() => {
              setDialogOpen(false);
              onChanged();
            }}
          />
        )}
      </div>
    );
  }

  return (
    <div className={className}>
      <Button size="sm" variant="ghost" disabled={busy} onClick={() => void dismiss()}>
        {t("projectModel.review.dismiss")}
      </Button>
    </div>
  );
}
