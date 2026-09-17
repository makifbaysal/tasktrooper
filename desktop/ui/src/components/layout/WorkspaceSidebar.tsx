import { useState } from "react";
import { useLocation } from "react-router-dom";
import { Bot, Brain, FileText, Inbox, Kanban, Layers, ListChecks, MessageSquare, PanelLeftClose, PanelLeftOpen, Plus, Rocket, Settings } from "lucide-react";
import type { Agent, WorkspaceConfig } from "@/api";
import { Button } from "@/components/ui/button";
import { SidebarNavLink } from "@/components/layout/SidebarNavLink";
import { Spinner } from "@/components/ui/spinner";
import { NewAgentDialog } from "@/components/workspace/NewAgentDialog";
import { useAgentUnread } from "@/hooks/useAgentUnread";
import { useI18n } from "@/hooks/useI18n";
import { useSetup } from "@/hooks/useSetup";
import { SETUP_PATH } from "@/lib/setup";
import { cn } from "@/lib/utils";

interface WorkspaceSidebarProps {
  config: WorkspaceConfig | null;
  agents: Agent[];
  loading: boolean;
  collapsed: boolean;
  onToggle: () => void;
  mobileOpen: boolean;
  onMobileClose: () => void;
  onRefresh: () => void;
}

export function WorkspaceSidebar({
  agents,
  loading,
  collapsed,
  onToggle,
  mobileOpen,
  onMobileClose,
  onRefresh,
}: WorkspaceSidebarProps) {
  const { t } = useI18n();
  const [newAgentOpen, setNewAgentOpen] = useState(false);
  // The way back into the guided sequence for anyone who skipped ahead — and
  // the only way in at all from a browser, where the route gate deliberately
  // does not redirect. Shown on `needsWork` and not on `!complete`: a step
  // this surface could not READ is not something to nag about.
  const { needsWork } = useSetup();

  const location = useLocation();
  const activeAgentMatch = /^\/agents\/([^/]+)\/chat/.exec(location.pathname);
  const activeAgentId = activeAgentMatch ? activeAgentMatch[1] : null;
  const unreadAgentIds = useAgentUnread(agents, activeAgentId);

  return (
    <>
      {mobileOpen && (
        <div
          className="fixed top-14 right-0 bottom-0 left-0 z-40 bg-black/50 lg:hidden"
          onClick={onMobileClose}
          aria-hidden
        />
      )}
      <aside
        className={cn(
          "fixed top-14 bottom-0 left-0 z-50 flex flex-col overflow-hidden border-r border-sidebar-border bg-sidebar transition-[width] duration-300 lg:static lg:z-auto lg:h-full lg:shrink-0",
          collapsed ? "w-[68px]" : "w-64",
          mobileOpen ? "translate-x-0" : "-translate-x-full lg:translate-x-0",
        )}
      >
        <div className="flex min-h-0 flex-1 flex-col overflow-hidden">
          {!collapsed && (
            <div className="p-2">
              <div className="rounded-lg border border-sidebar-border bg-sidebar-accent/30 px-3 py-2">
                {loading ? (
                  <Spinner size="sm" />
                ) : (
                  <p className="truncate text-body font-semibold text-sidebar-accent-foreground">
                    {t("frame.layout.sidebar.workspace")}
                  </p>
                )}
              </div>
            </div>
          )}

          <nav className="min-h-0 flex-1 space-y-1 overflow-y-auto px-2 pb-2 pt-2">
            {needsWork && (
              <SidebarNavLink
                to={SETUP_PATH}
                label={t("setup.nav.finishSetup")}
                icon={ListChecks}
                end={true}
                collapsed={collapsed}
                onClick={onMobileClose}
              />
            )}
            <SidebarNavLink
              to="/board"
              label="Board"
              icon={Kanban}
              end={true}
              collapsed={collapsed}
              onClick={onMobileClose}
            />
            <SidebarNavLink
              to="/backlog"
              label="Backlog"
              icon={Inbox}
              end={true}
              collapsed={collapsed}
              onClick={onMobileClose}
            />
            <SidebarNavLink
              to="/operations"
              label={t("operations.nav")}
              icon={Rocket}
              end={false}
              collapsed={collapsed}
              onClick={onMobileClose}
            />
            <SidebarNavLink
              to="/projects"
              label={t("frame.layout.sidebar.projects")}
              icon={Layers}
              end={true}
              collapsed={collapsed}
              onClick={onMobileClose}
            />

            {!collapsed ? (
              <div className="flex items-center justify-between px-3 pt-4 pb-1">
                <span className="text-micro font-medium tracking-wide text-muted-foreground uppercase">
                  {t("frame.layout.sidebar.agentChats")}
                </span>
                <button
                  type="button"
                  className="flex h-5 w-5 items-center justify-center rounded text-muted-foreground hover:bg-sidebar-accent hover:text-sidebar-accent-foreground"
                  title={t("frame.layout.sidebar.newAgent")}
                  onClick={() => setNewAgentOpen(true)}
                >
                  <Plus className="h-3.5 w-3.5" />
                </button>
              </div>
            ) : (
              <button
                type="button"
                className="mt-2 flex w-full items-center justify-center rounded-lg px-2 py-2 text-muted-foreground hover:bg-sidebar-accent hover:text-sidebar-accent-foreground"
                title={t("frame.layout.sidebar.newAgent")}
                onClick={() => setNewAgentOpen(true)}
              >
                <Plus className="h-4 w-4" />
              </button>
            )}
            {agents.map((agent) => (
              <SidebarNavLink
                key={agent.id}
                to={`/agents/${agent.id}/chat`}
                label={agent.name}
                icon={collapsed ? Bot : MessageSquare}
                end={false}
                collapsed={collapsed}
                onClick={onMobileClose}
                trailing={
                  unreadAgentIds.has(agent.id) ? (
                    // A dot, not a Badge: there is no count to show (the
                    // server has no per-viewer read state to count against,
                    // see useAgentUnread), only "something changed here".
                    <span
                      className="h-2 w-2 shrink-0 rounded-full bg-primary"
                      title={t("frame.layout.sidebar.unreadAgent")}
                    />
                  ) : null
                }
              />
            ))}

            {!collapsed && (
              <p className="px-3 pt-4 pb-1 text-micro font-medium tracking-wide text-muted-foreground uppercase">
                {t("frame.layout.sidebar.management")}
              </p>
            )}
            <SidebarNavLink
              to="/memory"
              label={t("frame.layout.sidebar.teamMemory")}
              icon={Brain}
              end={true}
              collapsed={collapsed}
              onClick={onMobileClose}
            />
            <SidebarNavLink
              to="/files"
              label="RAG"
              icon={FileText}
              end={true}
              collapsed={collapsed}
              onClick={onMobileClose}
            />
          </nav>

          <div className="shrink-0 space-y-1 border-t border-sidebar-border p-2">
            <SidebarNavLink
              to="/settings"
              label={t("frame.layout.sidebar.settings")}
              icon={Settings}
              end={false}
              collapsed={collapsed}
              onClick={onMobileClose}
            />
            <Button
              variant="ghost"
              size={collapsed ? "icon" : "default"}
              className={cn("hidden w-full lg:flex", !collapsed && "justify-start gap-3 px-3")}
              onClick={onToggle}
              title={collapsed ? t("frame.layout.sidebar.expandMenu") : t("frame.layout.sidebar.collapseMenu")}
            >
              {collapsed ? (
                <PanelLeftOpen className="h-4 w-4" />
              ) : (
                <>
                  <PanelLeftClose className="h-4 w-4 shrink-0" />
                  <span className="text-sm font-medium">{t("frame.layout.sidebar.collapse")}</span>
                </>
              )}
            </Button>
          </div>
        </div>
      </aside>

      <NewAgentDialog open={newAgentOpen} onOpenChange={setNewAgentOpen} onCreated={onRefresh} />
    </>
  );
}
