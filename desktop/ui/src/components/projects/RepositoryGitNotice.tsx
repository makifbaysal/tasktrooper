import { DownloadCloud } from "lucide-react";
import { useCallback, useEffect, useState } from "react";
import { toast } from "sonner";
import { api, type Repository, type RepositoryRestore } from "@/api";
import { Button } from "@/components/ui/button";
import { Notice } from "@/components/ui/notice";
import { Spinner } from "@/components/ui/spinner";
import { useI18n } from "@/hooks/useI18n";
import { usePolling } from "@/hooks/usePolling";
import { keepEqual } from "@/lib/stableState";

const POLL_MS = 3000;

interface RepositoryGitNoticeProps {
  repository: Repository;
  /** Called once a restore has finished so the card can be re-read. */
  onRestored: () => void;
  className?: string;
}

/**
 * The warning on a repository card, plus the one action that can clear it.
 *
 * The machine running the workspace can change — a different Mac, a fresh
 * data directory — and
 * every repository registered before the move points at a folder that machine
 * does not have. The warning already said so; this adds the next step, because
 * each of those repositories also carries the remote it came from.
 *
 * The button appears only when the SERVER says the code can be fetched
 * (`git_restorable`): the folder is genuinely missing here and a remote is on
 * record. A folder that exists — or that holds something which is not a
 * repository — is never offered a clone, and nothing is ever deleted to make
 * room for one.
 *
 * The clone runs on the server and can take minutes, so this watches it rather
 * than waiting on the request that started it.
 */
export function RepositoryGitNotice({ repository, onRestored, className }: RepositoryGitNoticeProps) {
  const { t } = useI18n();
  const [restore, setRestore] = useState<RepositoryRestore | undefined>(repository.git_restore);
  const [starting, setStarting] = useState(false);

  // The server's ledger is the truth; a reload mid-clone must keep showing it.
  useEffect(() => {
    setRestore(repository.git_restore);
  }, [repository.git_restore]);

  const running = starting || restore?.status === "running";

  const settle = useCallback(
    (next: RepositoryRestore | undefined) => {
      setRestore((prev) => keepEqual(prev, next));
      if (next?.status === "completed") {
        toast.success(t("boardArea.repos.restoreDone", { name: repository.name }));
        onRestored();
      } else if (next?.status === "failed") {
        toast.error(next.error || t("boardArea.repos.restoreFailed"));
      }
    },
    [onRestored, repository.name, t],
  );

  const pollRestore = useCallback(
    () =>
      api
        .getRepository(repository.id)
        // A failed poll is not a failed clone — the clone is on the server, so
        // keep watching rather than reporting something that did not happen.
        .then((fresh) => settle(fresh.git_restore))
        .catch(() => undefined),
    [repository.id, settle],
  );
  usePolling(pollRestore, POLL_MS, restore?.status === "running", { leading: false });

  const handleRestore = async () => {
    setStarting(true);
    try {
      const updated = await api.restoreRepositoryWorkingCopy(repository.id);
      settle(updated.git_restore);
    } catch (e) {
      // The server's refusal is the explanation (folder already there, a
      // non-repository folder in the way, no remote recorded) — show it as-is.
      toast.error(e instanceof Error ? e.message : t("boardArea.repos.restoreFailed"));
    } finally {
      setStarting(false);
    }
  };

  const failed = restore?.status === "failed";
  // A finished restore leaves nothing to say: the warning it cleared is gone
  // from the reloaded repository, so an empty callout would be all that is left.
  if (!repository.git_warning && !running && !failed) return null;

  const title = failed
    ? t("boardArea.repos.restoreFailed")
    : repository.git_warning || t("boardArea.repos.restoring");

  return (
    <Notice
      variant={failed ? "error" : "warning"}
      title={title}
      className={className ?? "mb-4 rounded-lg px-3 py-2 text-xs shadow-none"}
    >
      {failed && restore?.error && <p className="mb-2 font-mono text-micro break-all">{restore.error}</p>}
      {running ? (
        <span className="flex items-center gap-2">
          <Spinner size="sm" className="h-3.5 w-3.5" />
          {t("boardArea.repos.restoring")}
        </span>
      ) : (
        repository.git_restorable && (
          <Button variant="outline" size="sm" className="mt-1 h-7 gap-1.5 text-xs" onClick={handleRestore}>
            <DownloadCloud className="h-3.5 w-3.5" />
            {failed ? t("boardArea.repos.restoreRetry") : t("boardArea.repos.restore")}
          </Button>
        )
      )}
    </Notice>
  );
}
