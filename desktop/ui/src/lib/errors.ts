/**
 * Tells "we cancelled this ourselves" apart from a real failure: an aborted
 * fetch rejects with a DOMException named AbortError, and a cancelled request
 * must not raise a toast the user never caused. Structural rather than
 * `instanceof DOMException` — the rejection can also travel through wrappers
 * that only preserve the name.
 */
export function isAbortError(error: unknown): boolean {
  return (
    typeof error === "object" &&
    error !== null &&
    (error as { name?: unknown }).name === "AbortError"
  );
}

export function formatApiError(message: string): string {
  if (message.includes("no enabled agents available for planning")) {
    return "Orchestration needs at least one active agent. Add or enable an agent from the Agents page.";
  }
  return message;
}
