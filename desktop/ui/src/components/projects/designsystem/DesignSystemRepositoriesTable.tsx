import { Link } from "react-router-dom";
import type { DesignSystemRepositorySummary } from "@/api";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { useI18n } from "@/hooks/useI18n";
import { repositoryDesignSystemPath } from "@/lib/design-system";

interface DesignSystemRepositoriesTableProps {
  projectId: string;
  repositories: DesignSystemRepositorySummary[];
  /** Whether the project has an approved base; without one "another project" means nothing. */
  hasBase: boolean;
}

/** The project's repositories and how each one relates to its base: layer, pending layer, which base it builds on. */
export function DesignSystemRepositoriesTable({ projectId, repositories, hasBase }: DesignSystemRepositoriesTableProps) {
  const { t } = useI18n();
  if (repositories.length === 0) return null;

  return (
    <Card className="overflow-x-auto p-0">
      <CardHeader className="pb-3">
        <CardTitle className="text-heading">{t("designSystem.repositories.title")}</CardTitle>
        <CardDescription>{t("designSystem.repositories.description")}</CardDescription>
      </CardHeader>
      <table className="w-full border-t border-border text-left text-body">
        <thead className="border-b border-border bg-muted/20 text-caption uppercase text-muted-foreground">
          <tr>
            <th className="px-4 py-2 font-medium">{t("designSystem.repositories.repository")}</th>
            <th className="px-4 py-2 font-medium">{t("designSystem.repositories.layer")}</th>
            <th className="px-4 py-2 font-medium">{t("designSystem.repositories.base")}</th>
            <th className="px-4 py-2" />
          </tr>
        </thead>
        <tbody className="divide-y divide-border">
          {repositories.map((repo) => {
            const path = repositoryDesignSystemPath(repo.id, projectId);
            return (
              <tr key={repo.id} className="relative transition-colors hover:bg-muted/40">
                <td className="px-4 py-3">
                  <Link to={path} className="font-medium after:absolute after:inset-0">
                    {repo.name}
                  </Link>
                  {repo.kind && <p className="text-caption text-muted-foreground">{repo.kind}</p>}
                </td>
                <td className="px-4 py-3">
                  <div className="flex flex-wrap items-center gap-1.5">
                    {repo.layer ? (
                      <Badge variant="outline" className="font-mono">
                        {t("designSystem.version", { version: repo.layer.version })}
                      </Badge>
                    ) : (
                      <span className="text-muted-foreground">{t("designSystem.repositories.noLayer")}</span>
                    )}
                    {repo.pending_layer && <Badge variant="warning">{t("designSystem.repositories.pendingLayer")}</Badge>}
                  </div>
                </td>
                <td className="px-4 py-3">
                  {repo.ambiguous ? (
                    <Badge variant="warning">{t("designSystem.repositories.ambiguous")}</Badge>
                  ) : repo.builds_on_this_project ? (
                    <Badge variant="success">{t("designSystem.repositories.buildsOnThis")}</Badge>
                  ) : hasBase ? (
                    <span className="text-muted-foreground">{t("designSystem.repositories.otherProject")}</span>
                  ) : (
                    <span className="text-muted-foreground">—</span>
                  )}
                </td>
                <td className="px-4 py-3 text-right">
                  <Button size="sm" variant="outline" asChild className="relative z-10">
                    <Link to={path}>{t("designSystem.repositories.open")}</Link>
                  </Button>
                </td>
              </tr>
            );
          })}
        </tbody>
      </table>
    </Card>
  );
}
