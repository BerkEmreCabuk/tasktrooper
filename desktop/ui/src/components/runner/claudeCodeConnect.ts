// Connecting a local agent CLI.
//
// One POST, on either surface: `POST /v1/agent-cli/:flavor/connect`, which
// verifies the binary, checks it is signed in, and installs the agent/skill
// catalog onto this machine's disk. The server is local — started by the
// desktop shell, or run by hand for browser development — so there is no
// machine to bring up first and nothing to wait for.
//
// It stays out of the component because two screens run it (the Claude Code
// card and the guided setup step) and a second copy of the failure mapping
// would answer the same refusal differently in one of them.
import { ApiError, type AgentCLIFlavor, type AgentCLIState } from "@/api";

/** Attempts and backoff for a server that may still be finishing its own boot. */
const CLI_CONNECT_ATTEMPTS = 4;
const CLI_CONNECT_DELAYS_MS = [1_500, 3_000, 6_000];

/**
 * Where the flow is, for the one narration line the card shows.
 *
 * Coarse on purpose: the two acts take tens of seconds each (the catalog
 * install walks every agent and skill), and silence reads as a hang.
 */
export type ConnectStep = "installing" | "disconnecting-cli";

/** A failure of this flow, reduced to what a screen renders. */
export interface ConnectFailure {
  message: string;
}

/**
 * How a failure reads to a person.
 *
 * The server's own sentence, not a generic one: it is the only text that knows
 * whether the binary is missing or merely signed out, and those are fixed in
 * different places.
 */
export function connectFailure(e: unknown): ConnectFailure {
  return { message: e instanceof Error ? e.message : String(e) };
}

/** The two calls this flow makes, injected so it stays testable. */
interface AgentCLIApi {
  connectAgentCLI(flavor: AgentCLIFlavor): Promise<AgentCLIState>;
  disconnectAgentCLI(flavor: AgentCLIFlavor): Promise<AgentCLIState>;
}

interface RetryOptions {
  attempts: number;
  /** Waits BETWEEN attempts, so it holds one fewer entry than `attempts`. */
  delaysMs: number[];
  shouldRetry: (error: unknown) => boolean;
}

/** Runs `fn`, retrying only the errors `shouldRetry` claims are worth retrying. */
async function withRetry<T>(fn: () => Promise<T>, opts: RetryOptions): Promise<T> {
  let lastError: unknown;
  for (let attempt = 0; attempt < opts.attempts; attempt += 1) {
    try {
      return await fn();
    } catch (e) {
      lastError = e;
      const last = attempt === opts.attempts - 1;
      if (last || !opts.shouldRetry(e)) throw e;
      const delay = opts.delaysMs[attempt] ?? opts.delaysMs.at(-1) ?? 0;
      await sleep(delay);
    }
  }
  throw lastError;
}

/**
 * Is this the server still coming up, or its real answer?
 *
 * Only the first is retried. A 502/503/504 or a network TypeError (fetch
 * failing before there was a response at all) is a server whose listener is
 * bound but whose migrations are still running. Everything else — a missing
 * binary, a signed-out CLI — is its considered answer, and repeating the
 * question changes nothing.
 */
function isStillBootingError(error: unknown): boolean {
  if (error instanceof ApiError) return error.status === 502 || error.status === 503 || error.status === 504;
  return error instanceof TypeError;
}

export interface ConnectAgentCliInput {
  flavor: AgentCLIFlavor;
  api: AgentCLIApi;
  onStep?: (step: ConnectStep) => void;
}

/** One press of Connect. Returns the CLI state the server ends up with. */
export function connectAgentCli({ flavor, api, onStep }: ConnectAgentCliInput): Promise<AgentCLIState> {
  onStep?.("installing");
  return withRetry(() => api.connectAgentCLI(flavor), {
    attempts: CLI_CONNECT_ATTEMPTS,
    delaysMs: CLI_CONNECT_DELAYS_MS,
    shouldRetry: isStillBootingError,
  });
}

/** The mirror image: drop the server-side row. The backend itself stays up. */
export function disconnectAgentCli({ flavor, api, onStep }: ConnectAgentCliInput): Promise<AgentCLIState> {
  onStep?.("disconnecting-cli");
  return api.disconnectAgentCLI(flavor);
}

function sleep(ms: number): Promise<void> {
  return new Promise((resolve) => setTimeout(resolve, ms));
}
