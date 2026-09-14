import type { DeployProvider, DeployTarget, DeployTemplate } from "@/api";

// Every provider domain.ValidDeployProvider accepts, in the order the picker
// should offer them: the hosted runtimes first, the two stores next, and
// "custom" last — it is the escape hatch for a repo that ships by a workflow
// nothing here described, not a default anybody should scroll past.
export const DEPLOY_PROVIDERS: DeployProvider[] = [
  "gcp_cloud_run",
  "gcp_gke",
  "aws_ecs",
  "aws_lambda",
  "vercel",
  "fly",
  "app_store",
  "google_play",
  "custom",
];

// The two providers that ship through a store console rather than to an
// address. They need a bundle ID / package name in vars before the server will
// save them at all (deploy.Service.SaveTarget), and they have no base URL.
export function isStoreProvider(provider: DeployProvider): boolean {
  return provider === "app_store" || provider === "google_play";
}

// A mobile repo ships through a store console, never to an address a hosted
// runtime would answer on — offering GCP/AWS/Vercel/Fly there is a dead end
// the server rejects anyway. Everything else ships through one of those
// runtimes and never through a store console, so the two store providers are
// hidden rather than merely de-emphasized.
export function providersForKind(repoKind: string): DeployProvider[] {
  return repoKind === "mobile"
    ? DEPLOY_PROVIDERS.filter((p) => isStoreProvider(p) || p === "custom")
    : DEPLOY_PROVIDERS.filter((p) => !isStoreProvider(p));
}

// A template's `kinds` is frontmatter the template author sets — most do, but
// nothing forces it. Treat "no kinds declared" as "fits every kind" rather
// than hiding the template everywhere, since an unfiltered template used to
// be the only behavior and silently deleting one from every picker is a worse
// failure than showing it one kind too many.
export function templatesForKind(templates: DeployTemplate[], repoKind: string): DeployTemplate[] {
  return templates.filter((tpl) => !tpl.kinds || tpl.kinds.length === 0 || tpl.kinds.includes(repoKind));
}

/**
 * isHttpUrl reports whether a value is a URL the server will accept as a
 * destination: absolute, http(s), with a host.
 *
 * It deliberately does not try to guess intent — "api.example.com" is rejected
 * rather than silently prefixed, because the server stores what it is given
 * and a health probe against a schemeless string fails at dial time, hours
 * later, as an incident.
 */
export function isHttpUrl(value: string): boolean {
  const trimmed = value.trim();
  if (!trimmed) return false;
  let parsed: URL;
  try {
    parsed = new URL(trimmed);
  } catch {
    return false;
  }
  return (parsed.protocol === "http:" || parsed.protocol === "https:") && parsed.hostname !== "";
}

/**
 * Whether this server understands `logs_url`.
 *
 * `logs_url` is `omitempty` on the wire, so "the response has no logs_url" is
 * ambiguous: it means either "empty" or "this server predates the field".
 * That ambiguity cannot be resolved by reading a GET, so support is treated as
 * a three-state fact:
 *
 * - `supported`   — some target came back carrying the key, which only a
 *                   server that knows the field can do.
 * - `unknown`     — nothing has proven it either way. The field is rendered
 *                   and editable: a new server with no logs URL saved yet
 *                   looks exactly like an old one, and hiding the input in
 *                   that state would make the field unreachable forever.
 * - `unsupported` — proven by a round trip: a non-empty logs_url was PUT and
 *                   the saved target came back without it, so the server
 *                   dropped it. The input goes read-only from then on.
 */
export type LogsUrlSupport = "supported" | "unknown" | "unsupported";

// Proven-unsupported is remembered for the SPA session, not just for the
// mounted component: the answer is a property of the server, so navigating
// between repositories must not re-offer a field already shown to be a
// dead end. It is never persisted past a reload — a reload is also how a
// freshly deployed server gets a second chance.
let sessionLogsUrlUnsupported = false;

export function markLogsUrlUnsupported(): void {
  sessionLogsUrlUnsupported = true;
}

/** Test/debug seam: forget what this session learned about the server. */
export function resetLogsUrlSupport(): void {
  sessionLogsUrlUnsupported = false;
}

export function detectLogsUrlSupport(targets: DeployTarget[]): LogsUrlSupport {
  if (targets.some((target) => typeof target.logs_url === "string")) return "supported";
  return sessionLogsUrlUnsupported ? "unsupported" : "unknown";
}

/**
 * logsUrlWasDropped reports whether a save proves the server ignores the
 * field. Only a non-empty value can prove anything: sending "" and getting
 * nothing back is what a server that fully supports the field also does.
 */
export function logsUrlWasDropped(sent: string, saved: DeployTarget): boolean {
  return sent.trim() !== "" && typeof saved.logs_url !== "string";
}
