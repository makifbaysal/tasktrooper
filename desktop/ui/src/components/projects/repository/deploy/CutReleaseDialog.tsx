import { useCallback, useEffect, useState } from "react";
import { toast } from "sonner";
import { api, type Release, type ReleaseCutPreview } from "@/api";
import { FormDialog } from "@/components/admin/FormDialog";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Skeleton } from "@/components/ui/skeleton";
import { Textarea } from "@/components/ui/textarea";
import { useI18n } from "@/hooks/useI18n";

interface CutReleaseDialogProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  releaseId: string;
  repositoryName: string;
  /** The batch component's tag pattern, for the live tag preview as the
   * version is typed; "" falls back to "v{version}" (domain.ReleaseTag). */
  tagPattern?: string;
  onCut: (release: Release) => void;
}

/** Mirrors domain.ValidReleaseVersion exactly, so a mistake is caught before
 * the round trip; the server's own 400 message is shown verbatim for
 * whatever this misses. */
function versionError(version: string, t: (key: string) => string): string | null {
  const v = version.trim();
  if (v === "") return t("release.cutDialog.errors.versionRequired");
  if (v.length > 64) return t("release.cutDialog.errors.versionTooLong");
  if (v.startsWith("-") || v.startsWith(".")) return t("release.cutDialog.errors.versionLeadingChar");
  if (!/^[0-9A-Za-z.+_-]+$/.test(v)) return t("release.cutDialog.errors.versionChars");
  return null;
}

function releaseTag(pattern: string | undefined, version: string): string {
  const p = pattern?.trim() || "v{version}";
  return p.replaceAll("{version}", version.trim());
}

/**
 * Cuts a batch component's draft release: loads the preview (suggested
 * version, previous version, commit, generated notes, the tasks it carries),
 * lets a human pick the version and edit the notes, then submits with the
 * same confirm-by-repository-name guardrail every production write here uses.
 */
export function CutReleaseDialog({ open, onOpenChange, releaseId, repositoryName, tagPattern, onCut }: CutReleaseDialogProps) {
  const { t } = useI18n();
  const [preview, setPreview] = useState<ReleaseCutPreview | null>(null);
  const [loadError, setLoadError] = useState<string | null>(null);
  const [version, setVersion] = useState("");
  const [notes, setNotes] = useState("");
  const [typed, setTyped] = useState("");
  const [submitting, setSubmitting] = useState(false);
  const [submitError, setSubmitError] = useState<string | null>(null);

  const load = useCallback(async () => {
    setPreview(null);
    setLoadError(null);
    try {
      const res = await api.getReleaseCutPreview(releaseId);
      setPreview(res);
      setVersion(res.suggested_version);
      setNotes(res.notes);
    } catch (e) {
      setLoadError(e instanceof Error ? e.message : t("release.cutDialog.loadFailed"));
    }
  }, [releaseId, t]);

  useEffect(() => {
    if (!open) return;
    setTyped("");
    setSubmitError(null);
    void load();
  }, [open, load]);

  const versionProblem = version ? versionError(version, t) : null;
  const confirmed = typed.trim() === repositoryName;
  const canSubmit = Boolean(preview) && !versionProblem && confirmed && !submitting;

  const submit = async () => {
    if (!preview || versionProblem || !confirmed) return;
    setSubmitting(true);
    setSubmitError(null);
    try {
      const release = await api.cutRelease(releaseId, repositoryName, version.trim(), notes);
      toast.success(t("release.cutDialog.succeeded"));
      onCut(release);
      onOpenChange(false);
    } catch (e) {
      const message = e instanceof Error ? e.message : t("release.cutDialog.cutFailed");
      setSubmitError(message);
      toast.error(message);
    } finally {
      setSubmitting(false);
    }
  };

  return (
    <FormDialog
      open={open}
      onOpenChange={onOpenChange}
      title={t("release.cutDialog.title")}
      description={t("release.cutDialog.description")}
      className="sm:max-w-2xl"
      footer={
        <>
          <Button variant="outline" onClick={() => onOpenChange(false)} disabled={submitting}>
            {t("common.cancel")}
          </Button>
          <Button onClick={() => void submit()} disabled={!canSubmit}>
            {submitting ? t("release.cutDialog.cutting") : t("release.cutDialog.cut")}
          </Button>
        </>
      }
    >
      {loadError ? (
        <p className="text-caption text-destructive">{loadError}</p>
      ) : !preview ? (
        <div className="space-y-3">
          <Skeleton className="h-8 w-full" />
          <Skeleton className="h-24 w-full" />
        </div>
      ) : (
        <div className="space-y-4">
          <div className="space-y-1.5">
            <Label htmlFor="cut-release-version">{t("release.cutDialog.version")}</Label>
            <Input
              id="cut-release-version"
              value={version}
              onChange={(e) => setVersion(e.target.value)}
              placeholder={preview.suggested_version}
              autoComplete="off"
            />
            {versionProblem && <p className="text-micro text-destructive">{versionProblem}</p>}
            {preview.previous_version && (
              <p className="text-micro text-muted-foreground">
                {t("release.cutDialog.previousVersion")}: <span className="font-mono">{preview.previous_version}</span>
              </p>
            )}
          </div>

          <dl className="grid grid-cols-2 gap-x-4 gap-y-1.5 text-caption">
            <div>
              <dt className="text-muted-foreground">{t("release.cutDialog.tag")}</dt>
              <dd className="font-mono">{version.trim() ? releaseTag(tagPattern, version) : "—"}</dd>
            </div>
            <div>
              <dt className="text-muted-foreground">{t("release.cutDialog.commit")}</dt>
              <dd className="font-mono">{preview.commit_sha.slice(0, 12)}</dd>
            </div>
          </dl>

          <div className="space-y-1.5">
            <Label>{t("release.cutDialog.tasks")}</Label>
            <div className="flex flex-wrap gap-2">
              {preview.tasks.map((task) => (
                <span key={task.id} className="rounded-md border border-border px-2 py-1 text-caption">
                  <span className="font-mono">{task.key ?? task.id.slice(0, 8)}</span>
                  {task.title && <span className="ml-1.5 text-muted-foreground">{task.title}</span>}
                </span>
              ))}
            </div>
          </div>

          <div className="space-y-1.5">
            <Label htmlFor="cut-release-notes">{t("release.cutDialog.notes")}</Label>
            <Textarea
              id="cut-release-notes"
              value={notes}
              onChange={(e) => setNotes(e.target.value)}
              rows={8}
              className="font-mono text-caption"
            />
          </div>

          <div className="space-y-1.5">
            <Label htmlFor="cut-release-confirm">{t("frame.ui.confirm.typeToConfirm", { phrase: repositoryName })}</Label>
            <Input
              id="cut-release-confirm"
              value={typed}
              onChange={(e) => setTyped(e.target.value)}
              placeholder={repositoryName}
              autoComplete="off"
            />
          </div>

          {submitError && <p className="text-caption text-destructive">{submitError}</p>}
        </div>
      )}
    </FormDialog>
  );
}
