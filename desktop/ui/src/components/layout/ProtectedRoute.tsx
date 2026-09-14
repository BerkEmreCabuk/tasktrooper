import { Navigate, Outlet, useLocation } from "react-router-dom";
import { useSetup } from "@/hooks/useSetup";
import { SETUP_PATH } from "@/lib/setup";

/**
 * Not an auth gate — there is no sign-in. Its one job is the guided first-run
 * sequence: the desktop shell opens this app on /board, so without it a person
 * who has just installed it lands in a workspace with no Claude Code connected,
 * no GitHub and no repository, and is expected to find the three unrelated
 * screens that fix that.
 *
 * The whole policy — something DEFINITELY undone, not dismissed, and not during
 * the seconds a launching supervisor spends looking exactly like one nobody has
 * started — lives in `useSetup` as `redirectToSetup`, so this file cannot hold a
 * second opinion about it.
 */
export function ProtectedRoute() {
  const setup = useSetup();
  const location = useLocation();

  const onSetup = location.pathname.startsWith(SETUP_PATH);
  if (!onSetup && setup.redirectToSetup) {
    // The search string travels: the GitHub callback lands on /settings?github=…
    // and this is the hop that has to carry that answer to the step waiting for it.
    return <Navigate to={SETUP_PATH + location.search} replace />;
  }

  return <Outlet />;
}
