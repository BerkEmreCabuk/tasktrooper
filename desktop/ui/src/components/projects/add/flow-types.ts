// Shared shapes for the add-repository flow (components/projects/add/**).
// Kept separate from useAddRepositoryFlow so step components can import types
// without pulling in the reducer.
import type { NewRepositoryRequest } from "@/api";

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

export type ImportRecipe = GitHubImportRecipe | FolderImportRecipe;

/** How far a ready repository's scan got by the time the Scan step was left.
 * `not_started` and `slow` are client-side give-ups (no scan appeared / it ran
 * past the cap); the server keeps whatever it was doing. */
export type ScanOutcome = "pending" | "succeeded" | "failed" | "not_started" | "slow";

/** One repository queued in this flow. `recipe` is kept (not just the import
 * call's result) so Retry can redo exactly the call that failed. */
export interface PendingRepo {
  localId: string;
  label: string;
  recipe: ImportRecipe;
  status: PendingRepoStatus;
  repositoryId?: string;
  error?: string;
  scanOutcome?: ScanOutcome;
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
}

export type SourceMode = "folder" | "github" | "new";

export type NewRepositoryInput = Omit<NewRepositoryRequest, "project_ids">;

export interface DoneStats {
  components: number;
  checks: number;
  requiredChecks: number;
  links: number;
  reviewAnswered: number;
}
