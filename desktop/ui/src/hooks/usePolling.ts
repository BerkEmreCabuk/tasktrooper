import { useEffect, useRef, useState } from "react";

/**
 * useDocumentVisible tracks whether the tab is in the foreground.
 *
 * Polling from a hidden tab is pure waste: nobody can see the result, and a
 * background tab has no reason to generate traffic at all.
 */
export function useDocumentVisible(): boolean {
  const [visible, setVisible] = useState(
    () => typeof document === "undefined" || document.visibilityState === "visible",
  );
  useEffect(() => {
    const onChange = () => setVisible(document.visibilityState === "visible");
    document.addEventListener("visibilitychange", onChange);
    onChange();
    return () => document.removeEventListener("visibilitychange", onChange);
  }, []);
  return visible;
}

/**
 * usePolling runs `callback` immediately and then every `intervalMs`, while
 * `enabled` is true AND the tab is visible. Hiding the tab stops the timer;
 * showing it again fires one immediate refresh (so the user never looks at
 * stale data) and restarts the timer.
 */
export function usePolling(callback: () => void | Promise<void>, intervalMs: number, enabled: boolean) {
  const savedCallback = useRef(callback);
  const visible = useDocumentVisible();

  useEffect(() => {
    savedCallback.current = callback;
  }, [callback]);

  useEffect(() => {
    if (!enabled || !visible) return;
    const tick = () => {
      void savedCallback.current();
    };
    tick();
    const id = setInterval(tick, intervalMs);
    return () => clearInterval(id);
  }, [intervalMs, enabled, visible]);
}
