import { useEffect, useState } from "react";
import { api, type MobileStoreApp } from "@/api";
import { SimulatorRunPanel } from "@/components/board/SimulatorRunPanel";
import { TaskStoreBuildsPanel } from "@/components/board/TaskStoreBuildsPanel";
import { isStoreAppLinked } from "@/components/operations/StoreReleaseControls";
import { STORE_PLATFORMS } from "@/components/operations/storeTestBuilds";

interface TaskMobileTestingProps {
  repositoryId: string;
  taskId: string;
}

/**
 * The human UAT box's mobile half: store test builds and a simulator run.
 * Both exist only for a repository with a linked store app, so nothing renders
 * (and nothing else is asked) until that is known.
 */
export function TaskMobileTesting({ repositoryId, taskId }: TaskMobileTestingProps) {
  const [apps, setApps] = useState<MobileStoreApp[] | null>(null);

  useEffect(() => {
    let cancelled = false;
    setApps(null);
    api
      .listStoreApps(repositoryId)
      .then((list) => {
        if (cancelled) return;
        const linked = (list ?? []).filter((app) => isStoreAppLinked(app));
        setApps(
          STORE_PLATFORMS.flatMap((platform) => linked.find((app) => app.platform === platform) ?? []),
        );
      })
      .catch(() => {
        if (!cancelled) setApps([]);
      });
    return () => {
      cancelled = true;
    };
  }, [repositoryId]);

  if (!apps || apps.length === 0) return null;

  return (
    <>
      <TaskStoreBuildsPanel key={taskId} repositoryId={repositoryId} taskId={taskId} apps={apps} />
      <SimulatorRunPanel
        key={`sim-${taskId}`}
        repositoryId={repositoryId}
        taskId={taskId}
        platforms={apps.map((app) => app.platform)}
      />
    </>
  );
}
