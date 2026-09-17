import { FolderKanban, FolderOpen, FolderPlus } from "lucide-react";
import { useEffect, useState } from "react";
import {
  CreateRepositoryDialog,
  OpenRepositoryDialog,
} from "@/components/projects/RepositoryDialogs";
import { Button } from "@/components/ui/button";
import { Card } from "@/components/ui/card";
import { useI18n } from "@/hooks/useI18n";

type NoRepositoriesNoticeProps = {
  onRepositoryAdded: () => void;
  onReadyForTask?: () => void;
  className?: string;
  autoOpen?: "create" | "open" | null;
  onAutoOpenHandled?: () => void;
};

export function NoRepositoriesNotice({
  onRepositoryAdded,
  onReadyForTask,
  className,
  autoOpen,
  onAutoOpenHandled,
}: NoRepositoriesNoticeProps) {
  const { t } = useI18n();
  const [createOpen, setCreateOpen] = useState(false);
  const [openOpen, setOpenOpen] = useState(false);

  useEffect(() => {
    if (autoOpen === "create") {
      setCreateOpen(true);
      onAutoOpenHandled?.();
    }
    if (autoOpen === "open") {
      setOpenOpen(true);
      onAutoOpenHandled?.();
    }
  }, [autoOpen, onAutoOpenHandled]);

  const handleSuccess = () => {
    onRepositoryAdded();
    onReadyForTask?.();
  };

  return (
    <>
      <Card className={className ?? "border-warning/30 bg-warning/5 p-4"}>
        <div className="flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between">
          <div className="flex gap-3">
            <div className="flex h-9 w-9 shrink-0 items-center justify-center rounded-lg bg-warning/10">
              <FolderKanban className="h-4 w-4 text-warning" />
            </div>
            <div>
              <p className="text-body font-medium">{t("chatArea.workspace.noRepositories.title")}</p>
              <p className="mt-0.5 text-body text-muted-foreground">
                {t("chatArea.workspace.noRepositories.description")}
              </p>
            </div>
          </div>
          <div className="flex shrink-0 flex-wrap gap-2">
            <Button variant="outline" size="sm" className="gap-2" onClick={() => setOpenOpen(true)}>
              <FolderOpen className="h-4 w-4" />
              {t("chatArea.workspace.noRepositories.openRepo")}
            </Button>
            <Button size="sm" className="gap-2" onClick={() => setCreateOpen(true)}>
              <FolderPlus className="h-4 w-4" />
              {t("chatArea.workspace.noRepositories.createRepo")}
            </Button>
          </div>
        </div>
      </Card>

      <OpenRepositoryDialog open={openOpen} onOpenChange={setOpenOpen} onSuccess={handleSuccess} />
      <CreateRepositoryDialog open={createOpen} onOpenChange={setCreateOpen} onSuccess={handleSuccess} />
    </>
  );
}

export { NoRepositoriesNotice as NoProjectsNotice };
