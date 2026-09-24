import { useCallback, useReducer, useRef } from "react";
import { api, type Repository } from "@/api";
import type { DoneStats, ImportRecipe, PendingRepo, ProjectChoice, SourceSelection } from "@/components/projects/add/flow-types";

interface FlowState {
  step: number;
  projectId: string | null;
  projectName: string;
  repos: PendingRepo[];
  doneStats: DoneStats | null;
}

const initialState: FlowState = {
  step: 0,
  projectId: null,
  projectName: "",
  repos: [],
  doneStats: null,
};

type Action =
  | { type: "SET_STEP"; step: number }
  | { type: "START_SCAN"; projectId: string; projectName: string; repos: PendingRepo[] }
  | { type: "SET_IMPORTING"; localId: string }
  | { type: "IMPORT_SUCCEEDED"; localId: string; repositoryId: string }
  | { type: "IMPORT_FAILED"; localId: string; error: string }
  | { type: "FINISH"; stats: DoneStats };

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
    case "FINISH":
      return { ...state, step: 3, doneStats: action.stats };
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
  const emptyName = selection.empty?.name.trim();
  if (emptyName) {
    repos.push({
      localId: crypto.randomUUID(),
      label: emptyName,
      recipe: { method: "empty", name: emptyName, owner: selection.empty?.owner },
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
    case "empty":
      return api.createRepository(recipe.name, "", undefined, [projectId], recipe.owner || undefined);
  }
}

/**
 * Orchestrates the add-repository flow's state: which step is active, the
 * chosen/created project, and every queued repository's import.
 *
 * GitHub imports run one at a time (each is a synchronous clone on the
 * server — running several at once would just queue behind each other
 * anyway and makes the per-row order confusing); a folder open or an empty
 * repository create has no clone to wait on, so those start immediately and
 * in parallel with the GitHub queue.
 */
export function useAddRepositoryFlow() {
  const [state, dispatch] = useReducer(reducer, initialState);
  const stateRef = useRef(state);
  stateRef.current = state;

  const setStep = useCallback((step: number) => dispatch({ type: "SET_STEP", step }), []);

  const runOne = useCallback((repo: PendingRepo, projectId: string) => {
    return runRecipe(repo.recipe, projectId)
      .then((created) => dispatch({ type: "IMPORT_SUCCEEDED", localId: repo.localId, repositoryId: created.id }))
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
      const projectId =
        choice.mode === "existing" ? choice.projectId : (await api.createInitiativeProject({ name: choice.name })).id;
      const projectName = choice.mode === "existing" ? choice.projectName : choice.name;
      const repos = buildRepos(selection);
      dispatch({ type: "START_SCAN", projectId, projectName, repos });

      const githubRepos = repos.filter((r) => r.recipe.method === "github");
      const otherRepos = repos.filter((r) => r.recipe.method !== "github");

      void (async () => {
        for (const repo of githubRepos) await runOne(repo, projectId);
      })();
      for (const repo of otherRepos) void runOne(repo, projectId);
    },
    [runOne],
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

  const finish = useCallback((stats: DoneStats) => dispatch({ type: "FINISH", stats }), []);

  return { state, setStep, startScan, retryImport, finish };
}
