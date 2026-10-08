import { Loader2, Palette, Plus, Sparkles } from "lucide-react";
import { useId, useState } from "react";
import { Link } from "react-router-dom";
import { toast } from "sonner";
import { ApiError, type DesignSystemGenerateResult, type DesignSystemScope, type DesignTaskRef } from "@/api";
import { DesignTaskLink } from "@/components/projects/designsystem/DesignTaskLink";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { EmptyState } from "@/components/ui/empty-state";
import { Label } from "@/components/ui/label";
import { Notice } from "@/components/ui/notice";
import { Textarea } from "@/components/ui/textarea";
import { useI18n } from "@/hooks/useI18n";
import { columnLabel, DEFAULT_BOARD_COLUMNS } from "@/lib/project-board";

type TaskSummary = Pick<DesignTaskRef, "id" | "repository_id" | "key" | "title" | "column">;

interface DesignSystemGenerateCardProps {
  scope: DesignSystemScope;
  /** "create" draws the empty state; "update" a compact card once a design system exists. */
  mode: "create" | "update";
  /** The latest design task the tab opened. */
  request?: DesignTaskRef;
  onGenerate: (notes: string) => Promise<DesignSystemGenerateResult>;
  /** The project to add a repository to when the server answers 409. */
  projectId?: string;
  /** Overrides the compact card's button/title and description (a repository with a base but no layer yet). */
  label?: string;
  description?: string;
}

/**
 * The call to action that opens a `design` task for the designer agent, and
 * the state of that task once it exists: created, already running, or waiting
 * in review.
 */
export function DesignSystemGenerateCard({
  scope,
  mode,
  request,
  onGenerate,
  projectId,
  label,
  description,
}: DesignSystemGenerateCardProps) {
  const { t } = useI18n();
  const notesId = useId();
  const [notes, setNotes] = useState("");
  const [busy, setBusy] = useState(false);
  const [result, setResult] = useState<DesignSystemGenerateResult | null>(null);
  const [noRepository, setNoRepository] = useState(false);

  const isProject = scope === "project";
  const ctaLabel =
    label ??
    (mode === "update"
      ? t("designSystem.generate.update")
      : isProject
        ? t("designSystem.generate.createProject")
        : t("designSystem.generate.createRepository"));

  // A result stays on screen until the view reports its task closed, so the
  // card falls back to the form once the design is approved or dropped. While
  // both describe the same task the view's copy wins: its column is live.
  const live = request?.open ? request : undefined;
  const fresh = result && (!request || request.id !== result.task.id || request.open) ? result : null;
  const task: TaskSummary | undefined = live && fresh && live.id === fresh.task.id ? live : (fresh?.task ?? live);

  const submit = async () => {
    setBusy(true);
    setNoRepository(false);
    try {
      const next = await onGenerate(notes);
      setResult(next);
      setNotes("");
    } catch (e) {
      if (e instanceof ApiError && e.status === 409) setNoRepository(true);
      else toast.error(e instanceof Error ? e.message : t("designSystem.generate.failed"));
    } finally {
      setBusy(false);
    }
  };

  const form = (
    <div className="w-full max-w-xl space-y-3 text-left">
      {noRepository && (
        <Notice variant="error" title={t("designSystem.generate.noRepositoryTitle")}>
          <div className="space-y-2">
            <p>{t("designSystem.generate.noRepositoryBody")}</p>
            {projectId && (
              <Button size="sm" variant="outline" asChild>
                <Link to={`/projects/new?project=${projectId}`}>
                  <Plus />
                  {t("designSystem.generate.addRepository")}
                </Link>
              </Button>
            )}
          </div>
        </Notice>
      )}
      <div className="space-y-1.5">
        <Label htmlFor={notesId}>{t("designSystem.generate.notesLabel")}</Label>
        <Textarea
          id={notesId}
          value={notes}
          onChange={(e) => setNotes(e.target.value)}
          placeholder={t("designSystem.generate.notesPlaceholder")}
          rows={mode === "create" ? 3 : 2}
          disabled={busy}
        />
      </div>
      <Button onClick={() => void submit()} disabled={busy}>
        {busy ? <Loader2 className="animate-spin" /> : <Sparkles />}
        {ctaLabel}
      </Button>
    </div>
  );

  const status = task && (
    <DesignTaskStatus
      task={task}
      title={
        task.column === "analiz_review"
          ? t("designSystem.generate.inReview")
          : fresh
            ? fresh.created
              ? t("designSystem.generate.created")
              : t("designSystem.generate.alreadyRunning")
            : t("designSystem.generate.running")
      }
    />
  );

  if (mode === "create") {
    return (
      <Card className="p-0">
        <EmptyState
          icon={Palette}
          title={isProject ? t("designSystem.generate.emptyTitleProject") : t("designSystem.generate.emptyTitleRepository")}
          description={isProject ? t("designSystem.generate.explainProject") : t("designSystem.generate.explainRepository")}
          action={status || form}
        />
      </Card>
    );
  }

  return (
    <Card>
      <CardHeader className="pb-3">
        <CardTitle className="text-heading">{ctaLabel}</CardTitle>
        <CardDescription>
          {description ??
            (isProject
              ? t("designSystem.generate.updateDescriptionProject")
              : t("designSystem.generate.updateDescriptionRepository"))}
        </CardDescription>
      </CardHeader>
      <CardContent>{status || form}</CardContent>
    </Card>
  );
}

function DesignTaskStatus({ task, title }: { task: TaskSummary; title: string }) {
  const { t } = useI18n();
  const inReview = task.column === "analiz_review";
  return (
    <Notice variant={inReview ? "warning" : "info"} title={title} className="w-full max-w-xl text-left">
      <div className="space-y-2">
        <p>
          <span className="font-mono">{task.key}</span> · {task.title}
        </p>
        <p className="text-caption text-muted-foreground">
          {t("designSystem.generate.taskColumn", { column: columnLabel(task.column, DEFAULT_BOARD_COLUMNS) })}
        </p>
        {inReview ? (
          <Button size="sm" asChild>
            <DesignTaskLink taskId={task.id} taskKey={task.key} repositoryId={task.repository_id}>
              {t("designSystem.generate.reviewAndApprove")}
            </DesignTaskLink>
          </Button>
        ) : (
          <Button size="sm" variant="outline" asChild>
            <Link to={`/board?task=${encodeURIComponent(task.id)}`}>{t("designSystem.generate.openTask")}</Link>
          </Button>
        )}
      </div>
    </Notice>
  );
}
