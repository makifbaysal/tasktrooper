import { useCallback, useReducer, useRef } from "react";
import { api, type NewRepositoryResponse, type Repository } from "@/api";
import type {
  DoneStats,
  ImportRecipe,
  NewRepositoryInput,
  PendingRepo,
  ProjectChoice,
  ScanOutcome,
  SourceSelection,
} from "@/components/projects/add/flow-types";
import { logEvent } from "@/lib/analytics";

interface FlowState {
  step: number;
  projectId: string | null;
  projectName: string;
  repos: PendingRepo[];
  doneStats: DoneStats | null;
  created: NewRepositoryResponse | null;
}

const initialState: FlowState = {
  step: 0,
  projectId: null,
  projectName: "",
  repos: [],
  doneStats: null,
  created: null,
};

type Action =
  | { type: "SET_STEP"; step: number }
  | { type: "START_SCAN"; projectId: string; projectName: string; repos: PendingRepo[] }
  | { type: "SET_IMPORTING"; localId: string }
  | { type: "IMPORT_SUCCEEDED"; localId: string; repositoryId: string }
  | { type: "IMPORT_FAILED"; localId: string; error: string }
  | { type: "CONTINUE_TO_REVIEW"; outcomes: Record<string, ScanOutcome> }
  | { type: "FINISH"; stats: DoneStats }
  | { type: "CREATED"; projectId: string; projectName: string; result: NewRepositoryResponse };

export const DONE_STEP = 3;

function reducer(state: FlowState, action: Action): FlowState {
  switch (action.type) {
    case "SET_STEP":
      return { ...state, step: action.step };
    case "START_SCAN":
      return { ...state, step: 1, projectId: action.projectId, projectName: action.projectName, repos: action.repos };
    case "SET_IMPORTING":
      return {
        ...state,
        repos: state.repos.map((r) => (r.localId === action.localId ? { ...r, status: "importing", error: undefined } : r)),
      };
    case "IMPORT_SUCCEEDED":
      return {
        ...state,
        repos: state.repos.map((r) =>
          r.localId === action.localId ? { ...r, status: "ready", repositoryId: action.repositoryId, error: undefined } : r,
        ),
      };
    case "IMPORT_FAILED":
      return {
        ...state,
        repos: state.repos.map((r) => (r.localId === action.localId ? { ...r, status: "import_failed", error: action.error } : r)),
      };
    case "CONTINUE_TO_REVIEW":
      return {
        ...state,
        step: 2,
        repos: state.repos.map((r) => ({ ...r, scanOutcome: action.outcomes[r.localId] ?? r.scanOutcome })),
      };
    case "FINISH":
      return { ...state, step: DONE_STEP, doneStats: action.stats };
    case "CREATED":
      return {
        ...state,
        step: DONE_STEP,
        projectId: action.projectId,
        projectName: action.projectName,
        created: action.result,
      };
    default:
      return state;
  }
}

function buildRepos(selection: SourceSelection): PendingRepo[] {
  const repos: PendingRepo[] = [];
  if (selection.github) {
    for (const repo of selection.github.repos) {
      repos.push({
        localId: crypto.randomUUID(),
        label: repo.name,
        recipe: { method: "github", owner: selection.github.owner, name: repo.name, cloneUrl: repo.cloneUrl },
        status: "importing",
      });
    }
  }
  const folderPath = selection.folderPath.trim();
  if (folderPath) {
    repos.push({
      localId: crypto.randomUUID(),
      label: folderPath,
      recipe: { method: "folder", rootPath: folderPath },
      status: "importing",
    });
  }
  return repos;
}

/** Every import call the flow can run, none of them passing a description —
 * the scan writes one after it runs, per the "never ask what the scan can
 * answer" rule. */
function runRecipe(recipe: ImportRecipe, projectId: string): Promise<Repository> {
  switch (recipe.method) {
    case "github":
      return api.importGitHubRepository({
        owner: recipe.owner,
        name: recipe.name,
        clone_url: recipe.cloneUrl,
        project_ids: [projectId],
      });
    case "folder":
      return api.openRepository(recipe.rootPath, undefined, [projectId]);
  }
}

/**
 * Orchestrates the add-repository flow's state: which step is active, the
 * chosen/created project, and either every queued import or the one
 * repository created from scratch.
 *
 * GitHub imports run one at a time (each is a synchronous clone on the
 * server — running several at once would just queue behind each other
 * anyway and makes the per-row order confusing); a folder open has no clone
 * to wait on, so it starts immediately and in parallel with the GitHub queue.
 */
export function useAddRepositoryFlow() {
  const [state, dispatch] = useReducer(reducer, initialState);
  const stateRef = useRef(state);
  stateRef.current = state;
  // A new-repository create that fails after its new project was made must
  // not make a second project with the same name on Retry.
  const createdProjectRef = useRef<{ name: string; id: string } | null>(null);

  const setStep = useCallback((step: number) => dispatch({ type: "SET_STEP", step }), []);

  const resolveProject = useCallback(async (choice: ProjectChoice) => {
    if (choice.mode === "existing") return { projectId: choice.projectId, projectName: choice.projectName };
    const reused = createdProjectRef.current;
    if (reused && reused.name === choice.name) return { projectId: reused.id, projectName: choice.name };
    const project = await api.createInitiativeProject({ name: choice.name });
    logEvent("project_create", {});
    createdProjectRef.current = { name: choice.name, id: project.id };
    return { projectId: project.id, projectName: choice.name };
  }, []);

  const runOne = useCallback((repo: PendingRepo, projectId: string) => {
    return runRecipe(repo.recipe, projectId)
      .then((created) => {
        logEvent("repo_add", { provider: repo.recipe.method });
        dispatch({ type: "IMPORT_SUCCEEDED", localId: repo.localId, repositoryId: created.id });
      })
      .catch((e) =>
        dispatch({ type: "IMPORT_FAILED", localId: repo.localId, error: e instanceof Error ? e.message : String(e) }),
      );
  }, []);

  /** Resolves once every repo is queued (project resolved, rows created) —
   * NOT once every import finishes; those keep running and report back via
   * dispatch, which is what the Scan step polls for. Throws (and queues
   * nothing) if creating a brand-new project fails. */
  const startScan = useCallback(
    async (choice: ProjectChoice, selection: SourceSelection) => {
      const { projectId, projectName } = await resolveProject(choice);
      const repos = buildRepos(selection);
      dispatch({ type: "START_SCAN", projectId, projectName, repos });

      const githubRepos = repos.filter((r) => r.recipe.method === "github");
      const otherRepos = repos.filter((r) => r.recipe.method !== "github");

      void (async () => {
        for (const repo of githubRepos) await runOne(repo, projectId);
      })();
      for (const repo of otherRepos) void runOne(repo, projectId);
    },
    [resolveProject, runOne],
  );

  /** Creates a repository from scratch. There is no code to scan yet, so this
   * skips Scan and Review and lands on Done with the bootstrap task. Throws on
   * any failure so the caller can show it next to the form it came from. */
  const createNewRepository = useCallback(
    async (choice: ProjectChoice, input: NewRepositoryInput) => {
      const { projectId, projectName } = await resolveProject(choice);
      const result = await api.createNewRepository({ ...input, project_ids: [projectId] });
      logEvent("repo_add", { provider: "new" });
      dispatch({ type: "CREATED", projectId, projectName, result });
    },
    [resolveProject],
  );

  const retryImport = useCallback(
    (localId: string) => {
      const current = stateRef.current;
      const repo = current.repos.find((r) => r.localId === localId);
      if (!repo || !current.projectId) return;
      dispatch({ type: "SET_IMPORTING", localId });
      void runOne({ ...repo, status: "importing" }, current.projectId);
    },
    [runOne],
  );

  const continueToReview = useCallback(
    (outcomes: Record<string, ScanOutcome>) => dispatch({ type: "CONTINUE_TO_REVIEW", outcomes }),
    [],
  );

  const finish = useCallback((stats: DoneStats) => dispatch({ type: "FINISH", stats }), []);

  return { state, setStep, startScan, createNewRepository, retryImport, continueToReview, finish };
}
