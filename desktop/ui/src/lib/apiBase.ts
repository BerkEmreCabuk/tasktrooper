// Where API calls go.
//
// In the desktop shell the server is a child process on a port picked at
// launch, so the shell states the base at runtime — "same origin" cannot work
// there, since `app://` has nothing behind it. In a browser it is
// `VITE_API_BASE`; an empty base means "same origin, no prefix", which is what
// the dev server's proxy serves.
export function getApiBase(): string {
  const host = typeof window === "undefined" ? undefined : window.__tasktrooperDesktop?.apiBase;
  if (host) return host.replace(/\/$/, "");

  const envBase = import.meta.env.VITE_API_BASE as string | undefined;
  if (envBase) {
    return envBase.replace(/\/$/, "");
  }
  return "";
}

export function apiUrl(path: string): string {
  const base = getApiBase();
  if (!base) return path;
  return `${base}${path}`;
}
