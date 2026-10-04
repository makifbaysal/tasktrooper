import { Outlet, useLocation } from "react-router-dom";
import { useCallback, useEffect, useRef, useState } from "react";
import { api, type Agent, type WorkspaceConfig } from "@/api";
import { WorkspaceShell } from "@/components/layout/WorkspaceShell";
import { useCachedState, useFirstLoad } from "@/hooks/useCachedState";
import type { WorkspaceOutletContext } from "@/hooks/useWorkspaceOutlet";
import { resolveLeadAgent } from "@/lib/leadAgent";
import { CACHE_CONFIG } from "@/lib/project-board";
import { cn } from "@/lib/utils";

// Boot syncs the role catalog in the background (no embedding calls, and
// nothing in `agents` while it runs) — a brand-new install has zero agents
// until that sync lands, which is the empty state WorkspaceSidebar renders.
// `seeding` still briefly covers that sync (and general boot), so poll until
// the backend reports it done rather than painting the roster mid-boot. Cap
// the polling so a stuck/unreachable boot step can't spin the sidebar forever.
const SEED_POLL_INTERVAL_MS = 1500;
const SEED_POLL_MAX_ATTEMPTS = 20;

// The sidebar shows only enabled agents, so it caches that filtered list under
// its own key rather than sharing the board's full roster.
const SIDEBAR_AGENTS_CACHE = "workspace.sidebarAgents";
const LEAD_AGENT_CACHE = "workspace.leadAgentId";
// The server gives the first sync 10 minutes; polling a little past that covers
// it without spinning forever on an install whose catalog never records a run.
const FIRST_SYNC_POLL_INTERVAL_MS = 4000;
const FIRST_SYNC_POLL_MAX_ATTEMPTS = 165;

export function WorkspaceLayout() {
  const { pathname } = useLocation();
  const isChat = pathname.includes("/agents/") && pathname.includes("/chat");
  const isHome = pathname === "/home";
  const isBoard = pathname === "/board";
  const isBacklog = pathname === "/backlog";
  const isReleased = pathname === "/released";
  const isAnalysisReview = /^\/repositories\/[^/]+\/tasks\/[^/]+\/analysis\/?$/.test(pathname);
  const fullBleed = isHome || isChat || isBoard || isBacklog || isReleased || isAnalysisReview;
  // The sidebar's roster and column config come back from cache first: a
  // reload (or the desktop shell restoring a tab) renders the nav immediately
  // instead of holding it on skeletons until the server answers.
  const [config, setConfig] = useCachedState<WorkspaceConfig | null>(CACHE_CONFIG, null);
  const [agents, setAgents] = useCachedState<Agent[]>(SIDEBAR_AGENTS_CACHE, []);
  const [leadAgentId, setLeadAgentId] = useCachedState<string | null>(LEAD_AGENT_CACHE, null);
  // The lead's key gates loading too: /home must not read "no lead cached yet"
  // as "no lead" and send the first launch after an upgrade to the board.
  const [loading, setLoading] = useFirstLoad(CACHE_CONFIG, SIDEBAR_AGENTS_CACHE, LEAD_AGENT_CACHE);
  const [teamPreparing, setTeamPreparing] = useState(false);
  const pollTimer = useRef<ReturnType<typeof setTimeout> | undefined>(undefined);

  const load = useCallback(async () => {
    clearTimeout(pollTimer.current);

    // A failed refresh keeps whatever the sidebar is already showing: emptying
    // it would take the user's nav away over a blip.
    const fail = () => {
      setLoading(false);
      setTeamPreparing(false);
    };

    const attempt = async (tries: number): Promise<void> => {
      const [cfg, catalog, rolesResult, catalogStatus] = await Promise.all([
        api.getWorkspaceConfig(),
        api.listAgents(),
        api.listRoles().catch(() => null),
        api.getCatalogStatus().catch(() => null),
      ]);
      setConfig(cfg);
      if (catalog.seeding && tries < SEED_POLL_MAX_ATTEMPTS) {
        // The retry is fire-and-forget, so it must handle its own rejection —
        // the try/catch below only covers attempt(0). A blip on any later poll
        // would otherwise leave the sidebar spinning forever.
        pollTimer.current = setTimeout(() => {
          void attempt(tries + 1).catch(fail);
        }, SEED_POLL_INTERVAL_MS);
        return;
      }
      const enabledAgents = (catalog.agents ?? []).filter((a) => a.enabled);
      const lead = resolveLeadAgent(enabledAgents, rolesResult?.roles ?? null);
      setAgents(enabledAgents);
      setLeadAgentId(lead?.id ?? null);
      setLoading(false);
      // A fresh install's first catalog sync creates the agents one at a time,
      // each after its skills are embedded — minutes, long after `seeding` has
      // gone false (the boot step only launches the sync). Until it records its
      // first run, keep refreshing so the team fills in and the lead appears.
      const firstSyncRunning = catalogStatus?.configured === true && !catalogStatus.state?.last_sync_at;
      const keepPolling = firstSyncRunning && tries < FIRST_SYNC_POLL_MAX_ATTEMPTS;
      setTeamPreparing(keepPolling && !lead);
      if (keepPolling) {
        pollTimer.current = setTimeout(() => {
          void attempt(tries + 1).catch(fail);
        }, FIRST_SYNC_POLL_INTERVAL_MS);
      }
    };

    try {
      await attempt(0);
    } catch {
      fail();
    }
  }, [setConfig, setAgents, setLeadAgentId, setLoading]);

  useEffect(() => {
    load();
    return () => clearTimeout(pollTimer.current);
  }, [load]);

  const leadAgent = agents.find((a) => a.id === leadAgentId) ?? null;
  const outletContext: WorkspaceOutletContext = {
    config,
    agents,
    refreshWorkspace: load,
    leadAgent,
    workspaceLoading: loading,
    teamPreparing,
  };

  return (
    <WorkspaceShell
      config={config}
      agents={agents}
      leadAgent={leadAgent}
      loading={loading}
      fullBleed={fullBleed}
      onRefresh={load}
    >
      <div className={cn("flex min-h-0 flex-1 flex-col", fullBleed ? "h-full overflow-hidden" : "")}>
        <Outlet context={outletContext} />
      </div>
    </WorkspaceShell>
  );
}
