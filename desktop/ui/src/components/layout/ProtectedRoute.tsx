import { Navigate, Outlet, useLocation } from "react-router-dom";
import { Spinner } from "@/components/ui/spinner";
import { useSetup } from "@/hooks/useSetup";
import { ADD_REPOSITORY_PATH, SETUP_PATH } from "@/lib/setup";

/**
 * Not an auth gate — there is no sign-in. Its one job is the guided first-run
 * sequence: the desktop shell opens this app on /home, so without it a person
 * who has just installed it lands in a workspace with no Claude Code connected,
 * no GitHub and no repository, and is expected to find the three unrelated
 * screens that fix that.
 *
 * The whole policy — something DEFINITELY undone and not dismissed — lives in
 * `useSetup` as `redirectToSetup`, so this file cannot hold a second opinion
 * about it. Until that is known (`deciding`) the first screen waits rather than
 * rendering a workspace the gate may be about to leave.
 */
export function ProtectedRoute() {
  const setup = useSetup();
  const location = useLocation();

  const onSetup = location.pathname.startsWith(SETUP_PATH);
  // Settings is where several steps are finished — an API key, a provider — and
  // the add-repository flow is where the project step is, so the sequence links
  // to both and must not bounce the user straight back.
  const exempt =
    onSetup || location.pathname.startsWith("/settings") || location.pathname.startsWith(ADD_REPOSITORY_PATH);
  if (!exempt && setup.redirectToSetup) {
    // The search string travels: the GitHub callback lands on /settings?github=…
    // and this is the hop that has to carry that answer to the step waiting for it.
    return <Navigate to={SETUP_PATH + location.search} replace />;
  }
  if (!exempt && setup.deciding) {
    return (
      <div className="flex h-screen items-center justify-center">
        <Spinner size="lg" />
      </div>
    );
  }

  return <Outlet />;
}
