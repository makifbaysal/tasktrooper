import { CirclePause, Play, Send, TrendingUp, Upload } from "lucide-react";
import { useId, useState, type ComponentProps, type ReactNode } from "react";
import { api, type StoreAppView, type TrackRelease } from "@/api";
import {
  androidRolloutState,
  formatPercent,
  iosProductionState,
  isValidRolloutTarget,
  parseRolloutTarget,
  ROLLOUT_PRESETS,
  type IOSBlock,
  type RolloutBlock,
} from "@/components/operations/releases/mobileReleases";
import { Button } from "@/components/ui/button";
import { Card } from "@/components/ui/card";
import { ConfirmDialog } from "@/components/ui/confirm-dialog";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { useI18n } from "@/hooks/useI18n";
import { useStoreAction } from "@/hooks/useMobileReleases";
import { cn } from "@/lib/utils";

interface ProductionControlsProps {
  app: StoreAppView;
  production?: TrackRelease;
  onActed: () => void;
}

/** What can be done to production now: on iOS submit and release, on Android
 * the staged rollout's share, halt and resume — each disabled with its reason
 * where the server would refuse it. Every write sends the phrase as typed. */
export function ProductionControls({ app, production, onActed }: ProductionControlsProps) {
  return (
    <Card className="p-3">
      {app.platform === "ios" ? (
        <IOSControls app={app} production={production} onActed={onActed} />
      ) : (
        <AndroidControls app={app} production={production} onActed={onActed} />
      )}
    </Card>
  );
}

function GatedButton({
  reason,
  children,
  ...props
}: { reason: string | null; children: ReactNode } & Omit<ComponentProps<typeof Button>, "children">) {
  const reasonId = useId();
  return (
    <div className="min-w-0 max-w-64 space-y-1">
      <Button size="sm" {...props} disabled={props.disabled || reason !== null} aria-describedby={reason ? reasonId : props["aria-describedby"]}>
        {children}
      </Button>
      {reason && (
        <p id={reasonId} className="text-micro text-muted-foreground">
          {reason}
        </p>
      )}
    </div>
  );
}

type IOSAction = "submit" | "release";

function IOSControls({ app, production, onActed }: ProductionControlsProps) {
  const { t } = useI18n();
  const r = (key: string, params?: Record<string, string | number>) => t(`operations.mobileReleases.production.ios.${key}`, params);
  const { busy, run, refusal, clearRefusal } = useStoreAction();
  const [confirm, setConfirm] = useState<IOSAction | null>(null);
  const [shown, setShown] = useState<IOSAction>("submit");
  const state = iosProductionState(app, production);
  const version = state.queued?.version || "—";
  const blocked: Record<IOSAction, IOSBlock | null> = { submit: state.submit, release: state.release };
  const reason = (action: IOSAction) => {
    const block = blocked[action];
    return block ? r(`${action}Blocked.${block}`, { version }) : null;
  };
  const ask = (action: IOSAction) => {
    clearRefusal();
    setShown(action);
    setConfirm(action);
  };

  const act = async (typed: string) => {
    if (blocked[shown] !== null) return false;
    const ok =
      shown === "submit"
        ? await run("submit", () => api.submitIOS(app.repository_id, typed), r("submitted"))
        : await run("release", () => api.releaseIOS(app.repository_id, typed), r("released"));
    if (ok) onActed();
    return ok;
  };

  return (
    <div className="flex flex-wrap items-start gap-3">
      <GatedButton reason={reason("submit")} disabled={busy !== null} onClick={() => ask("submit")}>
        <Send aria-hidden />
        {r("submit")}
      </GatedButton>
      <GatedButton variant="outline" reason={reason("release")} disabled={busy !== null} onClick={() => ask("release")}>
        <Upload aria-hidden />
        {r("release")}
      </GatedButton>
      <ConfirmDialog
        open={confirm !== null}
        onOpenChange={(open) => !open && setConfirm(null)}
        title={r(`${shown}Title`, { identifier: app.identifier })}
        description={r(`${shown}Description`, { identifier: app.identifier, version })}
        confirmLabel={r(shown)}
        confirmPhrase={app.repository_name}
        confirmDisabled={blocked[shown] !== null}
        variant="destructive"
        loading={busy !== null}
        error={refusal ? refusal.detail || refusal.title : null}
        errorTitle={refusal?.title}
        onConfirm={act}
      />
    </div>
  );
}

type AndroidAction = "rollout" | "halt" | "resume";

function AndroidControls({ app, production, onActed }: ProductionControlsProps) {
  const { t, lang } = useI18n();
  const r = (key: string, params?: Record<string, string | number>) => t(`operations.mobileReleases.production.android.${key}`, params);
  const inputId = useId();
  const hintId = useId();
  const { busy, run, refusal, clearRefusal } = useStoreAction();
  const state = androidRolloutState(app, production);
  const current = state.current;
  const [draft, setDraft] = useState("");
  const [confirm, setConfirm] = useState<AndroidAction | null>(null);
  const [shown, setShown] = useState<AndroidAction>("rollout");
  const ask = (action: AndroidAction) => {
    clearRefusal();
    setShown(action);
    setConfirm(action);
  };

  const target = parseRolloutTarget(draft);
  const valid = target !== null && isValidRolloutTarget(target.percent, current);
  // Re-derived on every render, so a poll that moves the rollout while the
  // dialog is open takes its confirm away rather than sending a stale share.
  const rolloutReady = state.change === null && valid;
  const completes = target?.fraction === 1;
  const targetText = valid && target ? formatPercent(target.percent, lang) : "—";
  const blockText = (block: RolloutBlock | null) => (block ? r(`disabled.${block}`) : null);
  const currentText = current === null ? "—" : formatPercent(current, lang);
  const version = production?.version || production?.build || "—";

  const act = async (typed: string) => {
    let ok = false;
    if (shown === "rollout") {
      if (!rolloutReady || !target) return false;
      const { fraction, percent } = target;
      ok = await run(
        "rollout",
        () => api.setAndroidRollout(app.repository_id, fraction, fraction === 1 ? typed : undefined),
        r("applied", { percent: formatPercent(percent, lang) }),
      );
      if (ok) setDraft("");
    } else if (shown === "halt") {
      ok = await run("halt", () => api.haltAndroid(app.repository_id, typed), r("haltDone"));
    } else {
      ok = await run("resume", () => api.resumeAndroid(app.repository_id), r("resumeDone"));
    }
    if (ok) onActed();
    return ok;
  };

  const dialog: Record<AndroidAction, { title: string; description: string; label: string; phrase?: string; destructive: boolean }> = {
    rollout: {
      title: r("rolloutTitle", { percent: targetText }),
      description: r(completes ? "rolloutCompleteDescription" : "rolloutDescription", {
        identifier: app.identifier,
        version,
        from: currentText,
        to: targetText,
      }),
      label: r("rolloutConfirm"),
      phrase: completes ? app.repository_name : undefined,
      destructive: completes,
    },
    halt: {
      title: r("haltTitle"),
      description: r("haltDescription", { identifier: app.identifier, version }),
      label: r("halt"),
      phrase: app.repository_name,
      destructive: true,
    },
    resume: {
      title: r("resumeTitle"),
      description: r("resumeDescription", { identifier: app.identifier, percent: currentText }),
      label: r("resume"),
      destructive: false,
    },
  };
  const open = dialog[shown];
  const presets = ROLLOUT_PRESETS.filter((p) => isValidRolloutTarget(p, current));

  return (
    <div className="flex flex-col gap-4 @3xl:flex-row @3xl:items-start @3xl:justify-between">
      <div className="min-w-0 space-y-2">
        <div className="flex flex-wrap items-end gap-2">
          <div className="space-y-1">
            <Label htmlFor={inputId} className="text-caption font-normal text-muted-foreground">
              {r("share")}
            </Label>
            <div className="flex items-center gap-1.5">
              <Input
                id={inputId}
                inputMode="decimal"
                className="h-8 w-20 font-mono tabular-nums"
                value={draft}
                placeholder={currentText}
                disabled={state.change !== null || busy !== null}
                aria-describedby={hintId}
                aria-invalid={draft.trim() !== "" && !valid}
                onChange={(e) => setDraft(e.target.value)}
              />
              <span className="text-caption text-muted-foreground">%</span>
            </div>
          </div>
          <GatedButton
            variant="outline"
            reason={null}
            aria-describedby={hintId}
            disabled={!rolloutReady || busy !== null}
            onClick={() => ask("rollout")}
          >
            <TrendingUp aria-hidden />
            {r("apply")}
          </GatedButton>
        </div>
        {state.change === null && presets.length > 0 && (
          <div className="flex flex-wrap gap-1" role="group" aria-label={r("presets")}>
            {presets.map((preset) => {
              const pressed = target?.percent === preset;
              return (
                <Button
                  key={preset}
                  size="sm"
                  variant="ghost"
                  className={cn("h-6 px-2 font-mono text-micro tabular-nums", pressed && "bg-accent text-accent-foreground ring-1 ring-ring")}
                  aria-pressed={pressed}
                  onClick={() => setDraft(String(preset))}
                >
                  {formatPercent(preset, lang)}%
                </Button>
              );
            })}
          </div>
        )}
        <p id={hintId} className="text-micro text-muted-foreground">
          {blockText(state.change) ??
            (draft.trim() !== "" && !valid ? r("invalid", { current: currentText }) : r("hint", { current: currentText }))}
        </p>
      </div>

      <div className="flex flex-wrap items-start gap-3">
        <GatedButton variant="destructive" reason={blockText(state.halt)} disabled={busy !== null} onClick={() => ask("halt")}>
          <CirclePause aria-hidden />
          {r("halt")}
        </GatedButton>
        <GatedButton variant="outline" reason={blockText(state.resume)} disabled={busy !== null} onClick={() => ask("resume")}>
          <Play aria-hidden />
          {r("resume")}
        </GatedButton>
      </div>

      <ConfirmDialog
        open={confirm !== null}
        onOpenChange={(next) => !next && setConfirm(null)}
        title={open.title}
        description={open.description}
        confirmLabel={open.label}
        confirmPhrase={open.phrase}
        confirmDisabled={shown === "rollout" && !rolloutReady}
        variant={open.destructive ? "destructive" : "default"}
        loading={busy !== null}
        error={refusal ? refusal.detail || refusal.title : null}
        errorTitle={refusal?.title}
        onConfirm={act}
      />
    </div>
  );
}
