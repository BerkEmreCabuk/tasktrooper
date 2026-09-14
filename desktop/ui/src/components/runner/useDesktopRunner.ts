import { useEffect, useState } from "react";
import { desktopRunner } from "@/lib/desktop-bridge";
import type { DesktopRunnerHost, DesktopRunnerSnapshot } from "@/lib/desktop-bridge";

/**
 * The desktop shell's local capabilities, as React sees them.
 *
 * Everything here returns null or an empty value in a browser, which is what
 * lets one screen serve both surfaces: the desktop-only parts of the Claude
 * Code card (Settings → LLM Connection) and the guided setup steps that drive
 * this Mac are simply not rendered when there is no host, rather than rendered
 * disabled. A control a browser tab can never enable is a control that should
 * not be drawn.
 */
export function useDesktopHost(): DesktopRunnerHost | null {
  // The preload installs the marker before any of this app's code runs, so one
  // read at mount is enough — it cannot appear or vanish later.
  const [host] = useState<DesktopRunnerHost | null>(() => desktopRunner());
  return host;
}

/**
 * The supervisor's state, as a subscription rather than a poll.
 *
 * The shell's main process is the only thing that knows what the four
 * processes are doing, and it already emits on every transition. Polling would
 * add latency to the one thing the user is watching for — did it attach? — and
 * would still miss transitions that happen between polls.
 */
export function useRunnerSnapshot(host: DesktopRunnerHost | null): DesktopRunnerSnapshot | null {
  const [snapshot, setSnapshot] = useState<DesktopRunnerSnapshot | null>(null);

  useEffect(() => {
    if (!host) return;
    let live = true;
    void host.snapshot().then((initial) => {
      if (live) setSnapshot(initial);
    });
    const off = host.subscribe(setSnapshot);
    return () => {
      live = false;
      off();
    };
  }, [host]);

  return snapshot;
}
