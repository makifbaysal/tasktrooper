import { FolderX } from "lucide-react";
import { useEffect, useState } from "react";
import { useParams, useSearchParams } from "react-router-dom";
import { toast } from "sonner";
import { api } from "@/api";
import { ChecksTab } from "@/components/projects/repository/ChecksTab";
import { ComponentsTab } from "@/components/projects/repository/ComponentsTab";
import { DeployRuntimeTab } from "@/components/projects/repository/deploy/DeployRuntimeTab";
import { KnowledgeTab } from "@/components/projects/repository/KnowledgeTab";
import { LinksTab } from "@/components/projects/repository/LinksTab";
import { OverviewTab } from "@/components/projects/repository/OverviewTab";
import { RepositoryHeader } from "@/components/projects/repository/RepositoryHeader";
import { SettingsTab } from "@/components/projects/repository/SettingsTab";
import { PageContent } from "@/components/layout/PageContent";
import { Badge } from "@/components/ui/badge";
import { EmptyState } from "@/components/ui/empty-state";
import { Skeleton } from "@/components/ui/skeleton";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { useI18n } from "@/hooks/useI18n";
import { useRepositoryModel } from "@/hooks/useRepositoryModel";
import { useScanProgress } from "@/hooks/useScanProgress";
import { reviewCount } from "@/lib/project-model";

const TABS = ["overview", "components", "checks", "links", "deploy", "knowledge", "settings"] as const;
type RepositoryTab = (typeof TABS)[number];

function tabFromParam(raw: string | null): RepositoryTab {
  return (TABS as readonly string[]).includes(raw ?? "") ? (raw as RepositoryTab) : "overview";
}

export function RepositoryPage() {
  const { t } = useI18n();
  const { repositoryId } = useParams();
  const [searchParams, setSearchParams] = useSearchParams();
  const { model, loading, error, reload } = useRepositoryModel(repositoryId);

  const tab = tabFromParam(searchParams.get("tab"));
  const selectedComponentParam = searchParams.get("component");

  const setTab = (next: string) =>
    setSearchParams(
      (prev) => {
        const params = new URLSearchParams(prev);
        params.set("tab", next);
        return params;
      },
      { replace: true },
    );

  const setSelectedComponentId = (id: string | null) =>
    setSearchParams(
      (prev) => {
        const params = new URLSearchParams(prev);
        if (id) params.set("component", id);
        else params.delete("component");
        return params;
      },
      { replace: true },
    );

  const openComponent = (componentId: string) => {
    setSearchParams(
      (prev) => {
        const params = new URLSearchParams(prev);
        params.set("tab", "components");
        params.set("component", componentId);
        return params;
      },
      { replace: true },
    );
  };

  const [scanTriggered, setScanTriggered] = useState(false);
  const scanEnabled = scanTriggered || model?.latest_scan?.status === "running";
  const { scan, finished: scanFinished, refresh: refreshScan } = useScanProgress(repositoryId, { enabled: scanEnabled });

  useEffect(() => {
    if (scanEnabled && scanFinished) {
      setScanTriggered(false);
      void reload();
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [scanFinished]);

  const handleRescan = async () => {
    if (!repositoryId) return;
    try {
      await api.startRepositoryScan(repositoryId);
      setScanTriggered(true);
      await refreshScan();
    } catch (e) {
      toast.error(e instanceof Error ? e.message : t("repositoryPage.header.scanStartFailed"));
    }
  };

  const queryProjectId = searchParams.get("project");
  const fallbackProjectId = model?.repository.project_ids?.[0];
  const projectId = queryProjectId ?? fallbackProjectId;
  const [projectName, setProjectName] = useState<string | undefined>(undefined);

  useEffect(() => {
    if (!projectId) {
      setProjectName(undefined);
      return;
    }
    let cancelled = false;
    api
      .getInitiativeProject(projectId)
      .then((project) => {
        if (!cancelled) setProjectName(project.name);
      })
      .catch(() => {
        if (!cancelled) setProjectName(undefined);
      });
    return () => {
      cancelled = true;
    };
  }, [projectId]);

  if (loading && !model) {
    return (
      <PageContent className="space-y-4">
        <Skeleton className="h-20 w-full" />
        <Skeleton className="h-10 w-full" />
        <Skeleton className="h-64 w-full" />
      </PageContent>
    );
  }

  if (!model) {
    return (
      <PageContent>
        <EmptyState icon={FolderX} title={error ?? t("repositoryPage.title")} />
      </PageContent>
    );
  }

  const activeComponentCount = model.components.filter((c) => c.status === "active").length;
  const activeCheckCount = model.checks.filter((c) => c.status === "active").length;
  const review = reviewCount(model.review);

  return (
    <PageContent className="space-y-4">
      <RepositoryHeader
        repository={model.repository}
        model={model}
        projectId={projectId}
        projectName={projectName}
        scanning={scanEnabled}
        scan={scan}
        onRescan={handleRescan}
        onGitRestored={reload}
      />

      <Tabs value={tab} onValueChange={setTab}>
        <TabsList>
          <TabsTrigger value="overview">
            {t("repositoryPage.tabs.overview")}
            {review > 0 && (
              <Badge variant="warning" className="ml-1.5">
                {review}
              </Badge>
            )}
          </TabsTrigger>
          <TabsTrigger value="components">
            {t("repositoryPage.tabs.components")}
            <Badge variant="secondary" className="ml-1.5">
              {activeComponentCount}
            </Badge>
          </TabsTrigger>
          <TabsTrigger value="checks">
            {t("repositoryPage.tabs.checks")}
            <Badge variant="secondary" className="ml-1.5">
              {activeCheckCount}
            </Badge>
          </TabsTrigger>
          <TabsTrigger value="links">
            {t("repositoryPage.tabs.links")}
            {review > 0 && (
              <Badge variant="warning" className="ml-1.5">
                {review}
              </Badge>
            )}
          </TabsTrigger>
          <TabsTrigger value="deploy">{t("repositoryPage.tabs.deployRuntime")}</TabsTrigger>
          <TabsTrigger value="knowledge">{t("repositoryPage.tabs.knowledge")}</TabsTrigger>
          <TabsTrigger value="settings">{t("repositoryPage.tabs.settings")}</TabsTrigger>
        </TabsList>

        <TabsContent value="overview">
          <OverviewTab model={model} onReload={reload} onOpenComponent={openComponent} />
        </TabsContent>
        <TabsContent value="components">
          <ComponentsTab
            model={model}
            repositoryId={model.repository.id}
            selectedComponentId={selectedComponentParam}
            onSelectComponent={setSelectedComponentId}
            onReload={reload}
          />
        </TabsContent>
        <TabsContent value="checks">
          <ChecksTab model={model} selectedComponentId={selectedComponentParam} onSelectComponent={setSelectedComponentId} onReload={reload} />
        </TabsContent>
        <TabsContent value="links">
          <LinksTab model={model} selectedComponentId={selectedComponentParam} onSelectComponent={setSelectedComponentId} onReload={reload} />
        </TabsContent>
        <TabsContent value="deploy">
          <DeployRuntimeTab
            model={model}
            repositoryId={model.repository.id}
            selectedComponentId={selectedComponentParam}
            onSelectComponent={setSelectedComponentId}
            onReload={reload}
          />
        </TabsContent>
        <TabsContent value="knowledge">
          <KnowledgeTab model={model} repositoryId={model.repository.id} onReload={reload} />
        </TabsContent>
        <TabsContent value="settings">
          <SettingsTab model={model} repositoryId={model.repository.id} onReload={reload} />
        </TabsContent>
      </Tabs>
    </PageContent>
  );
}
