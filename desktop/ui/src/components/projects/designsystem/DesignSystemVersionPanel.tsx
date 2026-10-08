import { useState, type ReactNode } from "react";
import type { DesignSystemVersion, DesignTokenTree } from "@/api";
import { MarkdownContent } from "@/components/markdown/MarkdownContent";
import { DesignLintChecks } from "@/components/projects/designsystem/DesignLintChecks";
import { DesignSystemStatusBadge } from "@/components/projects/designsystem/DesignSystemStatusBadge";
import { DesignTaskLink } from "@/components/projects/designsystem/DesignTaskLink";
import { DesignTokensPreview } from "@/components/projects/designsystem/DesignTokensPreview";
import { Badge } from "@/components/ui/badge";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { useI18n } from "@/hooks/useI18n";
import { formatRelativeTime } from "@/lib/utils";

type PanelTab = "overview" | "tokens" | "components";

interface DesignSystemVersionPanelProps {
  version: DesignSystemVersion;
  title: string;
  /** Token paths a layer overrides, marked in the Tokens tab. */
  overriddenPaths?: string[];
  /** Shown under the header, above the tabs (e.g. a layer's overrides). */
  children?: ReactNode;
  /** Header actions (e.g. "show the current version"). */
  actions?: ReactNode;
}

/** One design system version: its identity and status, then DESIGN.md, tokens and component inventory. */
export function DesignSystemVersionPanel({
  version,
  title,
  overriddenPaths,
  children,
  actions,
}: DesignSystemVersionPanelProps) {
  const { t, lang } = useI18n();
  const [tab, setTab] = useState<PanelTab>("overview");

  const isLayer = version.scope === "repository";
  const tokens: DesignTokenTree = version.tokens ?? {};
  const designMd = version.design_md ?? "";
  const inventoryMd = version.inventory_md ?? "";
  const rationale = version.rationale ?? "";

  return (
    <Card>
      <CardHeader className="gap-2 space-y-0 pb-4">
        <div className="flex flex-wrap items-center gap-2">
          <CardTitle className="text-heading">{title}</CardTitle>
          <Badge variant="outline" className="font-mono">
            {t("designSystem.version", { version: version.version })}
          </Badge>
          <DesignSystemStatusBadge status={version.status} />
          {actions && <div className="ml-auto flex flex-wrap gap-2">{actions}</div>}
        </div>
        <div className="flex flex-wrap items-center gap-x-3 gap-y-1 text-caption text-muted-foreground">
          {version.source_task_id && (
            <span className="inline-flex items-center gap-1">
              {t("designSystem.panel.sourceTask")}:
              <DesignTaskLink
                taskId={version.source_task_id}
                taskKey={version.source_task_key}
                repositoryId={version.source_task_repository_id}
                className="font-mono text-foreground hover:underline"
              />
            </span>
          )}
          <span>
            {version.approved_at
              ? t("designSystem.panel.approvedAt", { date: formatRelativeTime(version.approved_at, lang) })
              : t("designSystem.panel.createdAt", { date: formatRelativeTime(version.created_at, lang) })}
          </span>
        </div>
      </CardHeader>
      <CardContent className="space-y-4">
        {isLayer && rationale.trim() && (
          <div className="space-y-1">
            <p className="text-caption font-medium text-muted-foreground">{t("designSystem.panel.rationale")}</p>
            <MarkdownContent content={rationale} />
          </div>
        )}
        {children}
        <DesignLintChecks findings={version.lint} />
        <Tabs value={tab} onValueChange={(v) => setTab(v as PanelTab)} variant="pill">
          <TabsList>
            <TabsTrigger value="overview">{t("designSystem.panel.tabs.overview")}</TabsTrigger>
            <TabsTrigger value="tokens">{t("designSystem.panel.tabs.tokens")}</TabsTrigger>
            <TabsTrigger value="components">{t("designSystem.panel.tabs.components")}</TabsTrigger>
          </TabsList>
          <TabsContent value="overview">
            {designMd.trim() ? (
              <MarkdownContent content={designMd} />
            ) : (
              <p className="text-body text-muted-foreground">{t("designSystem.panel.noOverview")}</p>
            )}
          </TabsContent>
          <TabsContent value="tokens">
            <DesignTokensPreview tokens={tokens} overriddenPaths={overriddenPaths} />
          </TabsContent>
          <TabsContent value="components">
            {inventoryMd.trim() ? (
              <MarkdownContent content={inventoryMd} />
            ) : (
              <p className="text-body text-muted-foreground">{t("designSystem.panel.noInventory")}</p>
            )}
          </TabsContent>
        </Tabs>
      </CardContent>
    </Card>
  );
}
