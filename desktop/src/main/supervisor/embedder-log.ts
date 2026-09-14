/**
 * Turns the embedder's stdout into the one fact the supervisor needs from it:
 * the loopback port it bound.
 *
 * Mirrors `runner-log.ts`'s approach to the Go runner's fixed status strings —
 * the embedder's first line of stdout is always exactly `EMBEDDER_LISTENING
 * <port>\n` (see `embedder/src/index.ts`), and that string is quoted here
 * rather than matched loosely for the same reason: a wrong match would hand
 * the runner a URL that does not answer, several layers from where anyone
 * would think to look.
 */

const LISTENING_PREFIX = "EMBEDDER_LISTENING ";

/** The port from an `EMBEDDER_LISTENING <port>` line, or `undefined` for any other line. */
export function parseEmbedderListening(line: string): number | undefined {
  const trimmed = line.trim();
  if (!trimmed.startsWith(LISTENING_PREFIX)) return undefined;
  const port = Number.parseInt(trimmed.slice(LISTENING_PREFIX.length).trim(), 10);
  return Number.isInteger(port) && port > 0 && port < 65_536 ? port : undefined;
}
