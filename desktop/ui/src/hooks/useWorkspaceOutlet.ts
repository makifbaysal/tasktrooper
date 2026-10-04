import { useOutletContext } from "react-router-dom";
import type { Agent, WorkspaceConfig } from "@/api";

/** What WorkspaceLayout hands every page rendered in its <Outlet />. */
export interface WorkspaceOutletContext {
  config: WorkspaceConfig | null;
  agents: Agent[];
  refreshWorkspace: () => void;
  /** The agent the stakeholder talks to (see lib/leadAgent); null when nobody can lead. */
  leadAgent: Agent | null;
  /** True until the first roster load settles — "no lead yet" is not "no lead". */
  workspaceLoading: boolean;
  /** The catalog's first sync is still creating agents and the lead is not among them yet. */
  teamPreparing: boolean;
}

/** Undefined outside WorkspaceLayout (e.g. a page rendered alone in a test). */
export function useWorkspaceOutlet(): WorkspaceOutletContext | undefined {
  return useOutletContext<WorkspaceOutletContext | undefined>();
}
