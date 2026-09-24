import { FolderKanban, Plus } from "lucide-react";
import { Link } from "react-router-dom";
import { Button } from "@/components/ui/button";
import { Card } from "@/components/ui/card";
import { useI18n } from "@/hooks/useI18n";

type NoRepositoriesNoticeProps = {
  className?: string;
};

export function NoRepositoriesNotice({ className }: NoRepositoriesNoticeProps) {
  const { t } = useI18n();

  return (
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
        <Button asChild size="sm" className="shrink-0 gap-2">
          <Link to="/projects/new">
            <Plus className="h-4 w-4" />
            {t("projectsHub.actions.addRepository")}
          </Link>
        </Button>
      </div>
    </Card>
  );
}

export { NoRepositoriesNotice as NoProjectsNotice };
