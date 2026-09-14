import { useCallback, useState } from "react";
import { api, type HealthResponse } from "@/api";
import { tStatic } from "@/hooks/useI18n";
import { usePolling } from "@/hooks/usePolling";

export function useHealth() {
  const [health, setHealth] = useState<HealthResponse | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);

  const refresh = useCallback(async () => {
    try {
      const data = await api.health();
      setHealth(data);
      setError(null);
    } catch (e) {
      setError(e instanceof Error ? e.message : tStatic("frame.layout.health.checkFailed"));
      setHealth(null);
    } finally {
      setLoading(false);
    }
  }, []);

  // Visibility-gated (usePolling): a hidden tab must not keep polling /health.
  usePolling(refresh, 30000, true);

  return { health, loading, error, refresh };
}
