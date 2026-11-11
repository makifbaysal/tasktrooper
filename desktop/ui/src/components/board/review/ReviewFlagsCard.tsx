import { ChevronDown, ChevronRight, Check, FileSearch, UserRoundSearch } from "lucide-react";
import { useState } from "react";
import type { ReviewFlag } from "@/api";
import { ExternalLinkAnchor } from "@/components/board/review/ExternalLinkAnchor";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { useI18n } from "@/hooks/useI18n";
import { formatRelativeTime } from "@/lib/utils";

interface ReviewFlagsCardProps {
  flags: ReviewFlag[];
  onMarkReviewed: (flag: ReviewFlag) => Promise<void> | void;
  /** The human-approval column: the card is the first thing on the page there. */
  prominent?: boolean;
}

const KNOWN_AREAS = new Set(["auth", "security", "payments", "migrations", "ci", "deploy", "infra"]);

export function flagText(flag: ReviewFlag, t: ReturnType<typeof useI18n>["t"]): string {
  if (flag.source === "agent") return flag.reason ?? "";
  const base = "boardArea.components.taskDetail.reviewFlags.codes";
  switch (flag.code) {
    case "sensitive_path": {
      const area = KNOWN_AREAS.has(flag.reason ?? "")
        ? t(`boardArea.components.taskDetail.reviewFlags.areas.${flag.reason}`)
        : (flag.reason ?? "");
      return t(`${base}.sensitive_path`, { area, count: flag.count ?? 0 });
    }
    case "qa_no_evidence":
      return t(`${base}.qa_no_evidence`, { criterion: flag.reason ?? "" });
    default:
      return t(`${base}.${flag.code}`, { count: flag.count ?? 0 });
  }
}

/**
 * The places reviewers (and the server itself, from real signals) want a
 * person to look at. Informational: nothing here blocks a move.
 */
export function ReviewFlagsCard({ flags, onMarkReviewed, prominent = false }: ReviewFlagsCardProps) {
  const { t, lang } = useI18n();
  const [busy, setBusy] = useState<string | null>(null);
  const [showReviewed, setShowReviewed] = useState(false);
  if (flags.length === 0) return null;
  const open = flags.filter((f) => !f.reviewed_at);
  const reviewed = flags.filter((f) => f.reviewed_at);

  const mark = async (flag: ReviewFlag) => {
    setBusy(flag.id);
    try {
      await onMarkReviewed(flag);
    } finally {
      setBusy(null);
    }
  };

  return (
    <section
      data-testid="review-flags-card"
      className={
        open.length > 0
          ? `space-y-3 rounded-lg border-2 border-amber-500/60 bg-amber-500/10 ${prominent ? "p-4 shadow-sm" : "p-3"}`
          : "space-y-2 rounded-lg border border-border p-3"
      }
    >
      <div className="flex items-center gap-2">
        <UserRoundSearch className="h-4 w-4 shrink-0 text-amber-600 dark:text-amber-400" />
        <h3 className={`${prominent ? "text-base" : "text-sm"} font-semibold`}>
          {t("boardArea.components.taskDetail.reviewFlags.title")}
        </h3>
        {open.length > 0 && (
          <Badge variant="warning" data-testid="review-flags-count">
            {t("boardArea.components.taskDetail.reviewFlags.openCount", { count: open.length })}
          </Badge>
        )}
      </div>
      {open.length > 0 && (
        <p className="text-xs text-muted-foreground">{t("boardArea.components.taskDetail.reviewFlags.intro")}</p>
      )}
      <ul className="space-y-2">
        {open.map((flag) => (
          <FlagRow key={flag.id} flag={flag} busy={busy === flag.id} onMark={() => void mark(flag)} />
        ))}
      </ul>
      {reviewed.length > 0 && (
        <div className="space-y-2">
          <Button
            type="button"
            variant="ghost"
            size="sm"
            className="h-6 gap-1 px-1 text-muted-foreground"
            aria-expanded={showReviewed}
            onClick={() => setShowReviewed((v) => !v)}
          >
            {showReviewed ? <ChevronDown className="h-3 w-3" /> : <ChevronRight className="h-3 w-3" />}
            {t("boardArea.components.taskDetail.reviewFlags.reviewedList", { count: reviewed.length })}
          </Button>
          {showReviewed && (
            <ul className="space-y-1.5">
              {reviewed.map((flag) => (
                <li key={flag.id} className="text-xs text-muted-foreground">
                  <span>{flagText(flag, t)}</span>{" "}
                  <span>
                    {t("boardArea.components.taskDetail.reviewFlags.reviewedBy", {
                      name: flag.reviewed_by ?? "",
                      when: flag.reviewed_at ? formatRelativeTime(flag.reviewed_at, lang) : "",
                    })}
                  </span>
                </li>
              ))}
            </ul>
          )}
        </div>
      )}
    </section>
  );
}

function FlagRow({ flag, busy, onMark }: { flag: ReviewFlag; busy: boolean; onMark: () => void }) {
  const { t } = useI18n();
  const where = flag.file ? (flag.line ? `${flag.file}:${flag.line}` : flag.file) : "";
  return (
    <li className="space-y-1 rounded-md border border-border bg-card p-2" data-testid="review-flag">
      <p className="text-sm">{flagText(flag, t)}</p>
      {flag.question && (
        <p className="text-xs text-muted-foreground">
          {t("boardArea.components.taskDetail.reviewFlags.question", { text: flag.question })}
        </p>
      )}
      {where && <p className="break-all font-mono text-caption text-muted-foreground">{where}</p>}
      {flag.files && flag.files.length > 1 && (
        <p className="break-all font-mono text-caption text-muted-foreground">
          {flag.files.slice(0, 5).join(", ")}
          {(flag.count ?? flag.files.length) > 5 ? " …" : ""}
        </p>
      )}
      <div className="flex flex-wrap items-center gap-2">
        <Badge variant={flag.source === "agent" ? "info" : "outline"}>
          {flag.source === "agent"
            ? t("boardArea.components.taskDetail.reviewFlags.flaggedBy", { name: flag.agent_name ?? "" })
            : t("boardArea.components.taskDetail.reviewFlags.automatic")}
        </Badge>
        {flag.file_url && (
          <ExternalLinkAnchor href={flag.file_url} className="inline-flex items-center gap-1 text-xs text-primary hover:underline">
            <FileSearch className="h-3 w-3" />
            {t("boardArea.components.taskDetail.reviewFlags.openFile")}
          </ExternalLinkAnchor>
        )}
        <Button type="button" size="sm" variant="outline" className="ml-auto h-7 gap-1" disabled={busy} onClick={onMark}>
          <Check className="h-3 w-3" />
          {t("boardArea.components.taskDetail.reviewFlags.markReviewed")}
        </Button>
      </div>
    </li>
  );
}
