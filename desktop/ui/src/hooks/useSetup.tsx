import {
  createContext,
  useCallback,
  useContext,
  useEffect,
  useMemo,
  useState,
  type ReactNode,
} from "react";
import {
  api,
  type AgentCLIState,
  type LLMProvidersResponse,
  type GitHubConnectionStatus,
  type InitiativeProject,
  type Repository,
} from "@/api";
import { useDesktopHost, useRunnerSnapshot } from "@/components/runner/useDesktopRunner";
import { usePolling } from "@/hooks/usePolling";
import type {
  DesktopPreflightReport,
  DesktopRunnerHost,
  DesktopRunnerSnapshot,
} from "@/lib/desktop-bridge";
import { readCache, writeCache } from "@/lib/uiCache";
import {
  SETUP_DISMISSED_KEY,
  activeSetupStep,
  setupComplete,
  setupNeedsWork,
  type SetupStepId,
  type SetupSteps,
} from "@/lib/setup";

/**
 * How often the shell re-runs the environment probe for the DERIVED state.
 *
 * Slow on purpose. `EnvironmentPreflight` polls faster while the setup screen
 * is open and pushes each report here through `reportEnvironment`, so this
 * timer only has to cover the case where nobody is looking at that screen —
 * the router gate deciding whether to open the sequence at all. Both write the
 * same state, and the non-forced call is answered from the shell's own cache.
 */
const PREFLIGHT_POLL_MS = 30_000;

/**
 * The longest the route gate holds the first screen while the server-backed
 * steps are still being read. Past it the workspace opens: a read that never
 * answers must not leave the app on a spinner. The desktop attaches this page
 * only once the backend answers /health, so the reads normally return in well
 * under a second.
 */
const GATE_DECIDE_TIMEOUT_MS = 8_000;

const SERVER_STEP_IDS = ["agent", "github", "project"] as const;

// The desktop registers its bundled embedder as the "local" OpenAI-compatible
// provider so retrieval works with no clicks. That row is configured on every
// install, so by itself it is not an API provider the user connected.
const BUNDLED_EMBEDDING_MODEL = "nomic-embed-text-v1.5";

/** What a piece of derived state can say. `null` = not read yet, not "absent". */
type Read<T> = { value: T; error: "" } | { value: null; error: string } | null;

interface SetupContextValue {
  steps: SetupSteps;
  /** The step the user is on: the first that is not done. Null when all are. */
  activeId: SetupStepId | null;
  complete: boolean;
  /** Something is DEFINITELY undone — never merely unreadable. Drives the gate. */
  needsWork: boolean;
  /** True inside the desktop shell, where every step can be performed. */
  inShell: boolean;
  /** The shell's local half, or null in a browser. */
  host: DesktopRunnerHost | null;
  snapshot: DesktopRunnerSnapshot | null;
  /** The shell's environment report, or null before it answers and in a browser. */
  preflightReport: DesktopPreflightReport | null;
  /** The connected CLI row, or null when it has not been read. */
  cliState: AgentCLIState | null;
  /** Everything the four steps read, again. Call after any step's own action. */
  refresh: () => Promise<void>;
  /**
   * Fold in a preflight report the environment step already fetched, so
   * pressing "Check again" there unlocks the next step immediately instead of
   * up to `PREFLIGHT_POLL_MS` later.
   */
  reportEnvironment: (result: { report: DesktopPreflightReport | null; error: string }) => void;
  /**
   * The same, for the GitHub status the step's own card already read. Without
   * it the card and this hook would each ask the server the same question on
   * the same screen, and disagree for as long as one of them was in flight.
   */
  reportGitHub: (result: { status: GitHubConnectionStatus | null; error: string }) => void;
  /** "I'll do this later" — suppresses the redirect only. Never marks anything done. */
  dismissed: boolean;
  dismiss: () => void;
  /**
   * The whole gate policy, in one place so `ProtectedRoute` cannot hold a
   * second opinion about it: something is undone and the sequence was not
   * dismissed.
   */
  redirectToSetup: boolean;
  /**
   * Not enough is known yet to choose between the setup sequence and the
   * workspace. `ProtectedRoute` holds the first screen while it is true, so a
   * first launch opens on setup instead of showing the board and then leaving it.
   */
  deciding: boolean;
}

const SetupContext = createContext<SetupContextValue | null>(null);

/**
 * The guided first-run sequence's state, derived from the real state of each
 * thing it asks for and shared by the router gate and the page.
 *
 * Nothing here is stored progress. Every step is a question asked of the thing
 * that would have to be true — `preflight().ready`, the connected CLI row, the
 * GitHub connection, a project with a repository in it — so a person who
 * disconnects GitHub is offered that step again, and a reload or an app
 * restart resumes exactly where the world actually is.
 */
export function SetupProvider({ children }: { children: ReactNode }) {
  const host = useDesktopHost();
  const snapshot = useRunnerSnapshot(host);

  const [preflight, setPreflight] = useState<Read<DesktopPreflightReport>>(null);
  const [cli, setCli] = useState<Read<AgentCLIState>>(null);
  const [providers, setProviders] = useState<Read<LLMProvidersResponse>>(null);
  const [github, setGithub] = useState<Read<GitHubConnectionStatus>>(null);
  const [work, setWork] = useState<Read<{ projects: InitiativeProject[]; repositories: Repository[] }>>(null);
  const [dismissed, setDismissed] = useState(() => readCache<boolean>(SETUP_DISMISSED_KEY) ?? false);

  // Is there a server to ask? In the shell the supervisor is the witness — it
  // started the backend and reports every transition. In a browser the server
  // is whatever `VITE_API_BASE` points at, which is already up or the page
  // would not be rendering, so the question does not arise.
  const backendUp = host
    ? snapshot !== null &&
      (snapshot.phase === "connected" || snapshot.phase === "degraded") &&
      snapshot.children.find((c) => c.id === "agent-server")?.state !== "failed"
    : true;

  // Has the shell said anything FINAL about the backend? Everything before the
  // first non-transitional snapshot is a reading in flight, which is `unknown`
  // and never `todo`.
  const settledHost = host
    ? snapshot !== null && snapshot.phase !== "connecting" && snapshot.phase !== "stopping"
    : true;

  const loadPreflight = useCallback(
    async (force = false) => {
      if (!host || typeof host.preflight !== "function") return;
      try {
        setPreflight({ value: await host.preflight(force), error: "" });
      } catch (e) {
        setPreflight({ value: null, error: e instanceof Error ? e.message : String(e) });
      }
    },
    [host],
  );
  usePolling(() => loadPreflight(), PREFLIGHT_POLL_MS, host !== null);

  // The three server reads, together: they are all cheap, they all need the
  // same precondition, and any one of them failing on its own still has to
  // leave the other two readable.
  const loadServer = useCallback(async () => {
    if (!backendUp) return;
    await Promise.all([
      api.getAgentCLIState().then(
        (value) => setCli({ value, error: "" }),
        (e: unknown) => setCli({ value: null, error: e instanceof Error ? e.message : String(e) }),
      ),
      api.listLLMProviders().then(
        (value) => setProviders({ value, error: "" }),
        (e: unknown) => setProviders({ value: null, error: e instanceof Error ? e.message : String(e) }),
      ),
      api.githubStatus().then(
        (value) => setGithub({ value, error: "" }),
        (e: unknown) => setGithub({ value: null, error: e instanceof Error ? e.message : String(e) }),
      ),
      Promise.all([api.listInitiativeProjects(), api.listRepositories()]).then(
        ([p, r]) => setWork({ value: { projects: p.projects ?? [], repositories: r.repositories ?? [] }, error: "" }),
        (e: unknown) => setWork({ value: null, error: e instanceof Error ? e.message : String(e) }),
      ),
    ]);
  }, [backendUp]);

  // One read per transition to "up", not a timer. Everything these three answer
  // changes because of something the user did on the setup screen, and that
  // calls `refresh()` itself.
  useEffect(() => {
    if (backendUp) void loadServer();
  }, [backendUp, loadServer]);

  // Refreshing on focus, not on a timer: the CLI connection flag is a stored
  // row (cheap to re-read) that can go stale behind the user's back — signed
  // out in another window, expired mid-session — so coming back to this
  // window is the one moment worth re-reading it, without polling the way
  // loadServer's own comment says not to.
  useEffect(() => {
    if (!backendUp) return;
    const onFocus = () => {
      if (document.visibilityState === "hidden") return;
      void loadServer();
    };
    window.addEventListener("focus", onFocus);
    document.addEventListener("visibilitychange", onFocus);
    return () => {
      window.removeEventListener("focus", onFocus);
      document.removeEventListener("visibilitychange", onFocus);
    };
  }, [backendUp, loadServer]);

  const refresh = useCallback(async () => {
    await Promise.all([loadPreflight(), loadServer()]);
  }, [loadPreflight, loadServer]);

  const reportEnvironment = useCallback((result: { report: DesktopPreflightReport | null; error: string }) => {
    setPreflight(result.report ? { value: result.report, error: "" } : { value: null, error: result.error });
  }, []);

  const reportGitHub = useCallback((result: { status: GitHubConnectionStatus | null; error: string }) => {
    setGithub(result.status ? { value: result.status, error: "" } : { value: null, error: result.error });
  }, []);

  const dismiss = useCallback(() => {
    writeCache(SETUP_DISMISSED_KEY, true);
    setDismissed(true);
  }, []);

  const [decideTimedOut, setDecideTimedOut] = useState(false);
  useEffect(() => {
    const id = window.setTimeout(() => setDecideTimedOut(true), GATE_DECIDE_TIMEOUT_MS);
    return () => window.clearTimeout(id);
  }, []);

  const steps = useMemo<SetupSteps>(() => {
    // 1. Environment. Only the shell can see it. In a browser it is inferred
    //    from the CLI being connected — the shell REFUSES Connect while a
    //    required item is failing, so a live connection is proof the checklist
    //    was green — and stays `unknown` otherwise rather than claiming a
    //    verdict about a machine this tab has never probed.
    const cliConnected = cliIsConnected(cli);
    const canProbe = host !== null && typeof host.preflight === "function";
    const environment: SetupSteps["environment"] = canProbe
      ? preflight === null
        ? { id: "environment", state: "unknown", actionable: true }
        : preflight.value
          ? { id: "environment", state: preflight.value.ready ? "done" : "todo", actionable: true }
          : { id: "environment", state: "unknown", actionable: true, error: preflight.error }
      : cliConnected
        ? { id: "environment", state: "done", actionable: false }
        : { id: "environment", state: "unknown", actionable: false };

    // 2. Claude Code. Connecting is one call to the local server on either
    //    surface, so it is actionable in a browser too. A backend that has not
    //    reported yet is `unknown` and not `todo`: every desktop launch passes
    //    through exactly that state on its way up, and calling it "not done"
    //    would open the sequence in front of someone whose server is seconds
    //    from answering.
    const apiProviderConfigured =
      (providers?.value?.endpoints?.length ?? 0) > 0 ||
      (providers?.value?.providers ?? []).some(
        (p) =>
          !p.definition.host_executed &&
          p.config.configured &&
          !(p.definition.type === "local" && p.config.default_model === BUNDLED_EMBEDDING_MODEL),
      );
    const agent: SetupSteps["agent"] = !settledHost || !backendUp
      ? { id: "agent", state: "unknown", actionable: true }
      : cliConnected || apiProviderConfigured
        ? { id: "agent", state: "done", actionable: true }
        : cli === null || providers === null
          ? { id: "agent", state: "unknown", actionable: true }
          : cli.value && providers.value
            ? { id: "agent", state: "todo", actionable: true }
            : { id: "agent", state: "unknown", actionable: true, error: cli.error || providers.error };

    const githubStep: SetupSteps["github"] = !backendUp
      ? { id: "github", state: "unknown", actionable: true }
      : github === null
        ? { id: "github", state: "unknown", actionable: true }
        : github.value
          ? { id: "github", state: github.value.connected ? "done" : "todo", actionable: true }
          : { id: "github", state: "unknown", actionable: true, error: github.error };

    const project: SetupSteps["project"] = !backendUp
      ? { id: "project", state: "unknown", actionable: true }
      : work === null
        ? { id: "project", state: "unknown", actionable: true }
        : work.value
          ? { id: "project", state: hasProjectWithRepository(work.value) ? "done" : "todo", actionable: true }
          : { id: "project", state: "unknown", actionable: true, error: work.error };

    return { environment, agent, github: githubStep, project };
  }, [host, backendUp, settledHost, preflight, cli, providers, github, work]);

  const complete = setupComplete(steps);
  const needsWork = setupNeedsWork(steps);
  // Only the server-backed steps are waited for. The environment probe runs
  // local binaries and can take seconds, and a missing CLI already surfaces as
  // an undone Claude Code step, so the gate loses nothing by not waiting on it.
  const deciding =
    !dismissed &&
    !needsWork &&
    !decideTimedOut &&
    SERVER_STEP_IDS.some((id) => steps[id].state === "unknown" && !steps[id].error);

  const value = useMemo<SetupContextValue>(
    () => ({
      steps,
      activeId: activeSetupStep(steps),
      complete,
      needsWork,
      inShell: host !== null,
      host,
      snapshot,
      preflightReport: preflight?.value ?? null,
      cliState: cli?.value ?? null,
      refresh,
      reportEnvironment,
      reportGitHub,
      dismissed,
      dismiss,
      redirectToSetup: !dismissed && needsWork,
      deciding,
    }),
    [
      steps,
      complete,
      host,
      snapshot,
      preflight,
      cli,
      refresh,
      reportEnvironment,
      reportGitHub,
      dismissed,
      dismiss,
      needsWork,
      deciding,
    ],
  );

  return <SetupContext.Provider value={value}>{children}</SetupContext.Provider>;
}

export function useSetup() {
  const ctx = useContext(SetupContext);
  if (!ctx) throw new Error("useSetup must be used within SetupProvider");
  return ctx;
}

/**
 * Is any agent CLI connected server-side? Any flavor will do: which CLI an
 * agent runs on is chosen per agent, and one connected CLI is enough to run.
 */
function cliIsConnected(cli: Read<AgentCLIState>): boolean {
  return (cli?.value?.connections?.length ?? 0) > 0;
}

/**
 * "A project exists, and a repository was imported into it."
 *
 * Both halves, because either alone is the half-finished state the sequence
 * exists to get people out of: an empty project runs nothing, and a repository
 * linked to no live project is what the old standalone Repositories page used
 * to leave behind.
 */
function hasProjectWithRepository({
  projects,
  repositories,
}: {
  projects: InitiativeProject[];
  repositories: Repository[];
}): boolean {
  if (projects.length === 0) return false;
  const live = new Set(projects.map((p) => p.id));
  return repositories.some((repo) => (repo.project_ids ?? []).some((id) => live.has(id)));
}
