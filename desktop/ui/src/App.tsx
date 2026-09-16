import { BrowserRouter, Navigate, Route, Routes, useParams } from "react-router-dom";
import { Toaster } from "sonner";
import { ProtectedRoute } from "@/components/layout/ProtectedRoute";
import { SetupProvider } from "@/hooks/useSetup";
import { ThemeProvider } from "@/hooks/useTheme";
import { I18nProvider } from "@/hooks/useI18n";
import { SetupPage } from "@/pages/SetupPage";
import { WorkspaceAgentLayout } from "@/components/layout/WorkspaceAgentLayout";
import { AgentSettingsPage } from "@/pages/AgentSettingsPage";
import { AgentColumnsPage } from "@/pages/AgentColumnsPage";
import { ProjectSettingsPage } from "@/pages/ProjectSettingsPage";
import { DeploySettingsPage } from "@/pages/DeploySettingsPage";
import { IncidentsPage } from "@/pages/IncidentsPage";
import { DeploymentsPage } from "@/pages/DeploymentsPage";
import { MobileAppsPage } from "@/pages/MobileAppsPage";
import { OperationsLayout } from "@/components/layout/OperationsLayout";
import { FilesPage } from "@/pages/FilesPage";
import { RulesPage } from "@/pages/RulesPage";
import { IntegrationsSettingsPage } from "@/pages/IntegrationsSettingsPage";
import { MCPServersPage } from "@/pages/MCPServersPage";
import { SettingsLayout } from "@/components/layout/SettingsLayout";
import { SettingsPage } from "@/pages/SettingsPage";
import { BoardSettingsPage } from "@/pages/BoardSettingsPage";
import { AnalizAssignmentSettingsPage } from "@/pages/AnalizAssignmentSettingsPage";
import { LLMSettingsPage } from "@/pages/LLMSettingsPage";
import { UsageSettingsPage } from "@/pages/UsageSettingsPage";
import { SkillsPage } from "@/pages/SkillsPage";
import { ProjectsPage } from "@/pages/ProjectsPage";
import { WorkspaceLayout } from "@/components/layout/WorkspaceLayout";
import { BacklogPage } from "@/pages/BacklogPage";
import { ReleasedPage } from "@/pages/ReleasedPage";
import { BoardPage } from "@/pages/BoardPage";
import { AgentChatPage } from "@/pages/AgentChatPage";
import { AgentPerformancePage } from "@/pages/AgentPerformancePage";
import { AgentMemoryPage } from "@/pages/AgentMemoryPage";
import { SharedMemoryPage } from "@/pages/SharedMemoryPage";

export default function App() {
  return (
    <ThemeProvider>
      <I18nProvider>
        {/* The guided first-run sequence's state, above the router because the
            route gate in ProtectedRoute and the /setup page itself both read it
            and must not disagree. It derives everything and stores nothing. */}
        <SetupProvider>
        <BrowserRouter>
          <Routes>
            <Route path="/" element={<Navigate to="/board" replace />} />
            <Route element={<ProtectedRoute />}>
              <Route path="/setup" element={<SetupPage />} />
              <Route element={<WorkspaceLayout />}>
                <Route path="board" element={<BoardPage />} />
                <Route path="backlog" element={<BacklogPage />} />
                <Route path="released" element={<ReleasedPage />} />
                {/* The standalone Repositories page is gone — every repository
                    now lives under the project(s) it is linked to. This
                    redirect stays for old bookmarks and links. */}
                <Route path="repositories" element={<Navigate to="/projects" replace />} />
                <Route path="projects" element={<ProjectsPage />} />
                <Route path="agents/:agentId/chat" element={<AgentChatPage />} />
                <Route path="agents/:agentId/chat/:sessionId" element={<AgentChatPage />} />
                <Route path="agents/new" element={<WorkspaceAgentLayout />}>
                  <Route index element={<Navigate to="settings" replace />} />
                  <Route path="settings" element={<AgentSettingsPage />} />
                </Route>
                <Route path="agents/:agentId" element={<WorkspaceAgentLayout />}>
                  <Route index element={<Navigate to="settings" replace />} />
                  <Route path="settings" element={<AgentSettingsPage />} />
                  <Route path="skills" element={<SkillsPage />} />
                  <Route path="rules" element={<RulesPage />} />
                  <Route path="columns" element={<AgentColumnsPage />} />
                  <Route path="memory" element={<AgentMemoryPage />} />
                  <Route path="performance" element={<AgentPerformancePage />} />
                </Route>
                <Route path="repositories/:repositoryId" element={<Navigate to="settings" replace />} />
                <Route path="repositories/:repositoryId/settings" element={<ProjectSettingsPage />} />
                <Route path="repositories/:repositoryId/deploy" element={<DeploySettingsPage />} />
                <Route path="operations" element={<OperationsLayout />}>
                  <Route index element={<Navigate to="deployments" replace />} />
                  <Route path="deployments" element={<DeploymentsPage />} />
                  <Route path="apps" element={<MobileAppsPage />} />
                  <Route path="incidents" element={<IncidentsPage />} />
                </Route>
                <Route path="incidents" element={<Navigate to="/operations/incidents" replace />} />
                <Route path="files" element={<FilesPage />} />
                <Route path="memory" element={<SharedMemoryPage />} />
                <Route path="settings" element={<SettingsLayout />}>
                  <Route index element={<SettingsPage />} />
                  <Route path="board" element={<BoardSettingsPage />} />
                  <Route path="analiz-assignment" element={<AnalizAssignmentSettingsPage />} />
                  <Route path="llm" element={<LLMSettingsPage />} />
                  <Route path="memory" element={<Navigate to="/memory" replace />} />
                  <Route path="mcp" element={<MCPServersPage />} />
                  <Route path="integrations" element={<IntegrationsSettingsPage />} />
                  <Route path="device" element={<Navigate to="/settings" replace />} />
                  {/* The Local Runner page is gone — everything it offered now lives on
                      the Claude Code card under LLM Connection. This redirect stays
                      because old bookmarks and already-installed desktop builds (whose
                      tray still points here) must land somewhere real. */}
                  <Route path="runner" element={<Navigate to="/settings/llm" replace />} />
                  <Route path="team" element={<Navigate to="/settings" replace />} />
                  <Route path="usage" element={<UsageSettingsPage />} />
                </Route>
                <Route path="workspace-settings" element={<Navigate to="/settings/board" replace />} />
              </Route>
              <Route path="/teams" element={<Navigate to="/board" replace />} />
              <Route path="/teams/*" element={<LegacyTeamRedirect />} />
              <Route path="/chat" element={<Navigate to="/board" replace />} />
              <Route path="/chat/:sessionId" element={<Navigate to="/board" replace />} />
              <Route path="/orchestration" element={<Navigate to="/board" replace />} />
              <Route path="/orchestration/*" element={<LegacyOrchestrationRedirect />} />
              <Route path="/skills" element={<Navigate to="/board" replace />} />
              <Route path="/agents" element={<Navigate to="/board" replace />} />
              <Route path="/rules" element={<Navigate to="/board" replace />} />
              <Route path="/mcp" element={<Navigate to="/settings/mcp" replace />} />
              <Route path="/profile" element={<Navigate to="/board" replace />} />
              <Route path="/llm" element={<Navigate to="/settings/llm" replace />} />
            </Route>
            <Route path="*" element={<Navigate to="/board" replace />} />
          </Routes>
        </BrowserRouter>
        <Toaster richColors position="top-right" closeButton />
        </SetupProvider>
      </I18nProvider>
    </ThemeProvider>
  );
}

// Old /teams/:teamId/... URLs map onto the single-workspace equivalents.
function LegacyTeamRedirect() {
  const { "*": rest } = useParams();
  const parts = (rest ?? "").split("/").filter(Boolean);
  // parts[0] is the old teamId; everything after maps to a root path.
  const tail = parts.slice(1).join("/");
  if (!tail) return <Navigate to="/board" replace />;
  if (tail === "settings") return <Navigate to="/workspace-settings" replace />;
  return <Navigate to={`/${tail}`} replace />;
}

// Old /orchestration/agents/:id/... URLs now live under /agents/:id/...
function LegacyOrchestrationRedirect() {
  const { "*": rest } = useParams();
  const tail = (rest ?? "").replace(/^agents\//, "");
  if (!tail || tail === "agents") return <Navigate to="/board" replace />;
  return <Navigate to={`/agents/${tail}`} replace />;
}
