import { useCallback, useState } from "react";
import { ScanRepoRow } from "@/components/projects/add/ScanRepoRow";
import type { PendingRepo } from "@/components/projects/add/flow-types";
import { Button } from "@/components/ui/button";
import { useI18n } from "@/hooks/useI18n";

interface ScanStepProps {
  repos: PendingRepo[];
  onRetry: (localId: string) => void;
  onContinue: () => void;
}

/**
 * Step 2: one row per queued repository, each driving its own import +
 * scan. "Continue to review" only needs every row settled — a failed
 * import or a failed scan both count as settled, since either can be
 * retried/re-run later without blocking the rest of the flow.
 */
export function ScanStep({ repos, onRetry, onContinue }: ScanStepProps) {
  const { t } = useI18n();
  const [settled, setSettled] = useState<Record<string, boolean>>({});

  const handleSettledChange = useCallback((localId: string, isSettled: boolean) => {
    setSettled((prev) => (prev[localId] === isSettled ? prev : { ...prev, [localId]: isSettled }));
  }, []);

  const allSettled = repos.length > 0 && repos.every((r) => r.status === "import_failed" || settled[r.localId] === true);
  const names = repos.map((r) => r.label).join(", ");

  return (
    <div className="space-y-4">
      <div>
        <h2 className="text-title font-semibold">{t("addRepository.scan.heading", { names })}</h2>
        <p className="text-caption text-muted-foreground">{t("addRepository.scan.subheading")}</p>
      </div>

      <div className="space-y-3">
        {repos.map((repo) => (
          <ScanRepoRow key={repo.localId} repo={repo} onRetry={onRetry} onSettledChange={handleSettledChange} />
        ))}
      </div>

      <div className="flex items-center justify-end gap-3">
        <span className="text-caption text-muted-foreground">{t("addRepository.scan.continueHint")}</span>
        <Button size="lg" disabled={!allSettled} onClick={onContinue}>
          {t("addRepository.scan.continue")}
        </Button>
      </div>
    </div>
  );
}
