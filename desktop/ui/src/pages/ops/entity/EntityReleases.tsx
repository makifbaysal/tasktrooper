import { RepositoryStoreReleases } from "@/components/operations/releases/RepositoryStoreReleases";
import { useOpsPageScope } from "@/components/operations/shell/OpsContext";
import { useI18n } from "@/hooks/useI18n";
import type { EntitySubPageProps } from "@/pages/ops/entity/types";

/** A mobile entity's Releases: its repository's App Store and Google Play apps,
 * the channels each build walks, the production rollout and the test builds.
 * Store state is read from the stores, so the time range and environment do
 * not apply here. */
export default function EntityReleases({ entity }: EntitySubPageProps) {
  const { t } = useI18n();
  useOpsPageScope({ time: false, env: false, note: t("operations.mobileReleases.scopeNote") });
  return (
    <RepositoryStoreReleases
      repositoryId={entity.repositoryId}
      repositoryName={entity.repositoryName}
      issuesTarget={{ entityId: entity.id }}
    />
  );
}
