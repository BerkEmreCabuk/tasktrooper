import type { ShellBridge } from "@ipc/channels.js";
import { SHELL_BRIDGE_KEY } from "@ipc/channels.js";

/**
 * The renderer's single door to the main process.
 *
 * Everything privileged goes through here. If a component imports `node:fs`,
 * `child_process` or `electron`, it will not even bundle — the renderer is
 * sandboxed with no node integration, which is the point.
 */

declare global {
  interface Window {
    [SHELL_BRIDGE_KEY]?: ShellBridge;
  }
}

const bridge = window[SHELL_BRIDGE_KEY];

if (!bridge) {
  // Only reachable if the preload failed to load, which means every action in
  // the UI would fail one at a time with a different message. Better to say it
  // once, plainly.
  throw new Error("The TaskTrooper preload bridge is missing — the app cannot talk to its own main process.");
}

export const api: ShellBridge = bridge;

/**
 * IPC rejections arrive as Errors whose message the main process wrote for a
 * user. Unwrapping them here keeps every catch site from re-deriving that.
 */
export function errorMessage(err: unknown): string {
  if (err instanceof Error) {
    // Electron prefixes IPC errors with "Error invoking remote method '…':".
    const match = /Error invoking remote method '[^']+': (?:Error: )?(.*)/s.exec(err.message);
    return (match?.[1] ?? err.message).trim();
  }
  return String(err);
}
