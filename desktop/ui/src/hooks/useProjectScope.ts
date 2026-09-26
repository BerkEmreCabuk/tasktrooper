import { useCallback, useEffect, useMemo } from "react";
import { useSearchParams } from "react-router-dom";
import { PROJECT_SCOPE_ALL, PROJECT_SCOPE_NONE, type ProjectScope } from "@/lib/project-board";

export const PROJECT_SCOPE_PARAM = "project";
export const PROJECT_SCOPE_STORAGE_KEY = "tt.board.projectScope";

// localStorage, not the session-scoped uiCache: the scope is a working choice
// ("I'm on Acme this week") that should survive a relaunch.
function readStoredScope(): string | null {
  try {
    return window.localStorage.getItem(PROJECT_SCOPE_STORAGE_KEY);
  } catch {
    return null;
  }
}

function writeStoredScope(scope: ProjectScope): void {
  try {
    window.localStorage.setItem(PROJECT_SCOPE_STORAGE_KEY, scope);
  } catch {
    /* private-mode storage or a quota error — the URL still carries the scope */
  }
}

export function resolveProjectScope(raw: string | null | undefined, projects: readonly { id: string }[]): ProjectScope {
  // With no project at all, "no project" is every task, and the picker does
  // not even offer it.
  if (!raw || raw === PROJECT_SCOPE_ALL || projects.length === 0) return PROJECT_SCOPE_ALL;
  if (raw === PROJECT_SCOPE_NONE) return PROJECT_SCOPE_NONE;
  return projects.some((project) => project.id === raw) ? raw : PROJECT_SCOPE_ALL;
}

/**
 * The project the board and the backlog are narrowed to, shared between them.
 * `?project=` wins; without it, the last choice made on either page. An id
 * that is no longer a project reads as "all" (and is never remembered), so a
 * deleted project cannot leave a page silently empty.
 */
export function useProjectScope(projects: readonly { id: string }[]) {
  const [searchParams, setSearchParams] = useSearchParams();
  const fromUrl = searchParams.get(PROJECT_SCOPE_PARAM);
  const raw = fromUrl ?? readStoredScope();
  const scope = useMemo(() => resolveProjectScope(raw, projects), [raw, projects]);

  // A link such as /board?project=<id> is a choice too: the sidebar's plain
  // /backlog link that follows must land on the same project.
  useEffect(() => {
    if (fromUrl && fromUrl === scope) writeStoredScope(scope);
  }, [fromUrl, scope]);

  // `dropParams` rides along in the same navigation: two functional
  // setSearchParams calls in one tick both start from the same params, so the
  // second would silently undo the first.
  const setScope = useCallback(
    (next: ProjectScope, dropParams: readonly string[] = []) => {
      writeStoredScope(next);
      setSearchParams(
        (prev) => {
          const params = new URLSearchParams(prev);
          for (const name of dropParams) params.delete(name);
          if (next === PROJECT_SCOPE_ALL) params.delete(PROJECT_SCOPE_PARAM);
          else params.set(PROJECT_SCOPE_PARAM, next);
          return params;
        },
        { replace: true },
      );
    },
    [setSearchParams],
  );

  return { scope, setScope };
}
