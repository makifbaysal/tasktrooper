import { useState, type ReactNode } from "react";
import { useLocation } from "react-router-dom";
import type { Agent, WorkspaceConfig } from "@/api";
import { Header } from "@/components/layout/Header";
import { WorkspaceSidebar } from "@/components/layout/WorkspaceSidebar";
import { useAgentUnread } from "@/hooks/useAgentUnread";

interface WorkspaceShellProps {
  config: WorkspaceConfig | null;
  agents: Agent[];
  loading: boolean;
  fullBleed?: boolean;
  onRefresh: () => void;
  children: ReactNode;
}

export function WorkspaceShell({
  config,
  agents,
  loading,
  fullBleed = false,
  onRefresh,
  children,
}: WorkspaceShellProps) {
  const [collapsed, setCollapsed] = useState(false);
  const [mobileOpen, setMobileOpen] = useState(false);

  // Shared with the header's notification center: an agent-comment
  // notification read there must clear the same sidebar dot, which only
  // works if both surfaces read the one hook instance's state.
  const location = useLocation();
  const activeAgentMatch = /^\/agents\/([^/]+)\/chat/.exec(location.pathname);
  const activeAgentId = activeAgentMatch ? activeAgentMatch[1] : null;
  const { unread: unreadAgentIds, markAgentSeen } = useAgentUnread(agents, activeAgentId);

  return (
    <div className="flex h-screen flex-col overflow-hidden bg-background">
      <Header
        onMenuClick={() => setMobileOpen(true)}
        sidebarCollapsed={collapsed}
        onAgentSeen={markAgentSeen}
      />
      <div className="flex min-h-0 flex-1 overflow-hidden">
        <WorkspaceSidebar
          config={config}
          agents={agents}
          loading={loading}
          collapsed={collapsed}
          onToggle={() => setCollapsed((v) => !v)}
          mobileOpen={mobileOpen}
          onMobileClose={() => setMobileOpen(false)}
          onRefresh={onRefresh}
          unreadAgentIds={unreadAgentIds}
        />
        <main
          className={
            fullBleed
              ? "flex min-h-0 w-full min-w-0 flex-1 flex-col overflow-hidden"
              : "w-full min-w-0 flex-1 overflow-auto p-page scrollbar-thin"
          }
        >
          {children}
        </main>
      </div>
    </div>
  );
}
