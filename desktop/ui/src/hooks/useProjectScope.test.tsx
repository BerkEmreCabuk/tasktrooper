import { act, renderHook } from "@testing-library/react";
import type { ReactNode } from "react";
import { MemoryRouter, useLocation } from "react-router-dom";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { PROJECT_SCOPE_STORAGE_KEY, useProjectScope } from "@/hooks/useProjectScope";

const projects = [{ id: "proj-a" }, { id: "proj-b" }];

function renderScope(url: string, list: readonly { id: string }[] = projects) {
  const wrapper = ({ children }: { children: ReactNode }) => (
    <MemoryRouter initialEntries={[url]}>{children}</MemoryRouter>
  );
  return renderHook(
    () => {
      const { scope, setScope } = useProjectScope(list);
      return { scope, setScope, search: useLocation().search };
    },
    { wrapper },
  );
}

describe("useProjectScope", () => {
  beforeEach(() => {
    window.localStorage.clear();
  });

  it("defaults to every project", () => {
    expect(renderScope("/board").result.current.scope).toBe("all");
  });

  it("lets the URL win over the saved choice, and remembers it", () => {
    window.localStorage.setItem(PROJECT_SCOPE_STORAGE_KEY, "proj-b");
    const { result } = renderScope("/board?project=proj-a");
    expect(result.current.scope).toBe("proj-a");
    expect(window.localStorage.getItem(PROJECT_SCOPE_STORAGE_KEY)).toBe("proj-a");
  });

  it("falls back to the saved choice when the URL has none", () => {
    window.localStorage.setItem(PROJECT_SCOPE_STORAGE_KEY, "proj-b");
    expect(renderScope("/backlog").result.current.scope).toBe("proj-b");
    window.localStorage.setItem(PROJECT_SCOPE_STORAGE_KEY, "none");
    expect(renderScope("/backlog").result.current.scope).toBe("none");
  });

  it("reads a project that no longer exists as all, without remembering it", () => {
    window.localStorage.setItem(PROJECT_SCOPE_STORAGE_KEY, "proj-b");
    expect(renderScope("/board?project=proj-gone").result.current.scope).toBe("all");
    expect(window.localStorage.getItem(PROJECT_SCOPE_STORAGE_KEY)).toBe("proj-b");

    window.localStorage.setItem(PROJECT_SCOPE_STORAGE_KEY, "proj-gone");
    expect(renderScope("/board").result.current.scope).toBe("all");
  });

  it("reads everything as all while there is no project at all", () => {
    expect(renderScope("/board?project=none", []).result.current.scope).toBe("all");
  });

  it("writes a new choice to both the URL and storage", () => {
    const { result } = renderScope("/board?task=t-1");

    act(() => result.current.setScope("proj-b"));
    expect(result.current.scope).toBe("proj-b");
    expect(new URLSearchParams(result.current.search).get("project")).toBe("proj-b");
    expect(new URLSearchParams(result.current.search).get("task")).toBe("t-1");
    expect(window.localStorage.getItem(PROJECT_SCOPE_STORAGE_KEY)).toBe("proj-b");

    act(() => result.current.setScope("all"));
    expect(result.current.scope).toBe("all");
    expect(new URLSearchParams(result.current.search).has("project")).toBe(false);
    expect(window.localStorage.getItem(PROJECT_SCOPE_STORAGE_KEY)).toBe("all");
  });

  it("keeps working when storage throws", () => {
    vi.spyOn(window.localStorage, "getItem").mockImplementation(() => {
      throw new Error("denied");
    });
    vi.spyOn(window.localStorage, "setItem").mockImplementation(() => {
      throw new Error("denied");
    });
    const { result } = renderScope("/board");
    expect(result.current.scope).toBe("all");

    act(() => result.current.setScope("proj-a"));
    expect(result.current.scope).toBe("proj-a");
  });
});
