import { Outlet, useLocation } from "react-router-dom";
import { useCallback, useEffect, useRef } from "react";
import { api, type Agent, type WorkspaceConfig } from "@/api";
import { WorkspaceShell } from "@/components/layout/WorkspaceShell";
import { useCachedState, useFirstLoad } from "@/hooks/useCachedState";
import { CACHE_CONFIG } from "@/lib/project-board";
import { cn } from "@/lib/utils";

// A brand-new tenant seeds its default agents in the background (each one
// runs its own LLM skill-embedding calls), so the roster can take a few
// seconds to finish. Rather than showing agents one at a time as they're
// inserted, poll until the backend reports seeding is done and reveal the
// full roster together. Cap the polling so a stuck/unreachable LLM can't
// spin the sidebar forever.
const SEED_POLL_INTERVAL_MS = 1500;
const SEED_POLL_MAX_ATTEMPTS = 20;

// The sidebar shows only enabled agents, so it caches that filtered list under
// its own key rather than sharing the board's full roster.
const SIDEBAR_AGENTS_CACHE = "workspace.sidebarAgents";

export function WorkspaceLayout() {
  const { pathname } = useLocation();
  const isChat = pathname.includes("/agents/") && pathname.includes("/chat");
  const isBoard = pathname === "/board";
  const isBacklog = pathname === "/backlog";
  const isReleased = pathname === "/released";
  const fullBleed = isChat || isBoard || isBacklog || isReleased;
  // The sidebar's roster and column config come back from cache first: a
  // reload (or the desktop shell restoring a tab) renders the nav immediately
  // instead of holding it on skeletons until the tenant answers.
  const [config, setConfig] = useCachedState<WorkspaceConfig | null>(CACHE_CONFIG, null);
  const [agents, setAgents] = useCachedState<Agent[]>(SIDEBAR_AGENTS_CACHE, []);
  const [loading, setLoading] = useFirstLoad(CACHE_CONFIG, SIDEBAR_AGENTS_CACHE);
  const pollTimer = useRef<ReturnType<typeof setTimeout> | undefined>(undefined);

  const load = useCallback(async () => {
    clearTimeout(pollTimer.current);

    // A failed refresh keeps whatever the sidebar is already showing: emptying
    // it would take the user's nav away over a blip.
    const fail = () => {
      setLoading(false);
    };

    const attempt = async (tries: number): Promise<void> => {
      const [cfg, catalog] = await Promise.all([api.getWorkspaceConfig(), api.listAgents()]);
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
      setAgents((catalog.agents ?? []).filter((a) => a.enabled));
      setLoading(false);
    };

    try {
      await attempt(0);
    } catch {
      fail();
    }
  }, [setConfig, setAgents, setLoading]);

  useEffect(() => {
    load();
    return () => clearTimeout(pollTimer.current);
  }, [load]);

  return (
    <WorkspaceShell config={config} agents={agents} loading={loading} fullBleed={fullBleed} onRefresh={load}>
      <div className={cn("flex min-h-0 flex-1 flex-col", fullBleed ? "h-full overflow-hidden" : "")}>
        <Outlet context={{ config, agents, refreshWorkspace: load }} />
      </div>
    </WorkspaceShell>
  );
}
