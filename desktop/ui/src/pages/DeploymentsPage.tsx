import { useCallback, useEffect, useState } from "react";
import { toast } from "sonner";
import { api, type MatrixCell, type MatrixRepo, type MatrixView } from "@/api";
import { PageHeader } from "@/components/admin/PageHeader";
import { DeploymentDetailDrawer } from "@/components/operations/DeploymentDetailDrawer";
import { DeploymentMatrix } from "@/components/operations/DeploymentMatrix";
import { Skeleton } from "@/components/ui/skeleton";
import { useCachedState, useFirstLoad } from "@/hooks/useCachedState";
import { useI18n } from "@/hooks/useI18n";

// Deploy runs finish while the tab sits open and there is no websocket; a
// 15s poll keeps the grid honest without one.
const REFRESH_INTERVAL_MS = 15_000;

const CACHE_MATRIX = "operations.deployMatrix";

export function DeploymentsPage() {
  const { t } = useI18n();
  const [view, setView] = useCachedState<MatrixView | null>(CACHE_MATRIX, null);
  const [loading, setLoading] = useFirstLoad(CACHE_MATRIX);
  const [selected, setSelected] = useState<{ repo: MatrixRepo; cell: MatrixCell } | null>(null);

  const load = useCallback(async () => {
    try {
      setView(await api.getDeployMatrix());
    } catch (err) {
      toast.error(err instanceof Error ? err.message : t("common.actionFailed"));
    } finally {
      setLoading(false);
    }
  }, [t, setView, setLoading]);

  // Only the first load shows the skeleton — `loading` is never set back to
  // true, so the 15s background refresh updates the grid in place.
  useEffect(() => {
    void load();
    const id = setInterval(() => void load(), REFRESH_INTERVAL_MS);
    return () => clearInterval(id);
  }, [load]);

  if (loading) return <Skeleton className="h-64 w-full" />;

  return (
    <>
      <PageHeader title={t("operations.deployments.title")} description={t("operations.deployments.description")} />
      <DeploymentMatrix
        view={view ?? { repos: [], envs: [] }}
        onSelect={(repo, cell) => setSelected({ repo, cell })}
      />
      {selected && (
        <DeploymentDetailDrawer
          repo={selected.repo}
          cell={selected.cell}
          open
          onOpenChange={(open) => !open && setSelected(null)}
          onActed={load}
        />
      )}
    </>
  );
}
