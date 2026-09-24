// Shared shapes for the add-repository flow (components/projects/add/**).
// Kept separate from useAddRepositoryFlow so step components can import types
// without pulling in the reducer.

export type PendingRepoStatus = "importing" | "import_failed" | "ready";

export interface GitHubImportRecipe {
  method: "github";
  owner: string;
  name: string;
  cloneUrl?: string;
}

export interface FolderImportRecipe {
  method: "folder";
  rootPath: string;
}

export interface EmptyImportRecipe {
  method: "empty";
  name: string;
  owner?: string;
}

export type ImportRecipe = GitHubImportRecipe | FolderImportRecipe | EmptyImportRecipe;

/** One repository queued in this flow. `recipe` is kept (not just the import
 * call's result) so Retry can redo exactly the call that failed. */
export interface PendingRepo {
  localId: string;
  label: string;
  recipe: ImportRecipe;
  status: PendingRepoStatus;
  repositoryId?: string;
  error?: string;
}

export type ProjectChoice =
  | { mode: "existing"; projectId: string; projectName: string }
  | { mode: "new"; name: string };

export interface GitHubSourceSelection {
  owner: string;
  repos: { name: string; cloneUrl?: string }[];
}

export interface SourceSelection {
  github: GitHubSourceSelection | null;
  folderPath: string;
  empty: { name: string; owner?: string } | null;
}

export interface DoneStats {
  components: number;
  checks: number;
  requiredChecks: number;
  links: number;
  reviewAnswered: number;
}
