// The credential this UI sends to the local server, as `Authorization: Bearer`.
//
// There is no sign-in screen and no account: a single person runs a server on
// their own machine and this app talks to it. Two ways the token gets here:
//
//   desktop shell — the app generated it on first run, keeps it in the OS
//     keychain, passes it to the server it spawns, and states it here.
//   browser (development) — `VITE_API_KEY`, which must match the server's
//     `SERVER_API_KEY`.
//
// Read at call time, not captured: the shell's preload installs the marker
// before any of this app's code runs, but a live lookup keeps the browser case
// a plain `null` rather than a module-load-order question.
export function getApiToken(): string | null {
  const fromShell = typeof window === "undefined" ? undefined : window.__tasktrooperDesktop?.apiToken;
  if (fromShell) return fromShell;
  const fromEnv = import.meta.env.VITE_API_KEY as string | undefined;
  return fromEnv ? fromEnv : null;
}
