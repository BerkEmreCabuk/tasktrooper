import { useCallback, useState } from "react";
import { ScanRepoRow } from "@/components/projects/add/ScanRepoRow";
import type { PendingRepo, ScanOutcome } from "@/components/projects/add/flow-types";
import { Button } from "@/components/ui/button";
import { useI18n } from "@/hooks/useI18n";

interface ScanStepProps {
  repos: PendingRepo[];
  onRetry: (localId: string) => void;
  onContinue: (outcomes: Record<string, ScanOutcome>) => void;
  noScanTimeoutMs?: number;
  scanCapMs?: number;
}

/**
 * Step 2: one row per queued repository, each driving its own import +
 * scan. "Continue to review" only needs every row settled — a failed
 * import, a failed scan, a scan that never started or one past the client
 * cap all count, since each can be retried/re-run later from the repository
 * page. "Continue without waiting" leaves the rest running server-side.
 */
export function ScanStep({ repos, onRetry, onContinue, noScanTimeoutMs, scanCapMs }: ScanStepProps) {
  const { t } = useI18n();
  const [outcomes, setOutcomes] = useState<Record<string, ScanOutcome>>({});

  const handleOutcomeChange = useCallback((localId: string, outcome: ScanOutcome) => {
    setOutcomes((prev) => (prev[localId] === outcome ? prev : { ...prev, [localId]: outcome }));
  }, []);

  const settled = (r: PendingRepo) => {
    if (r.status === "import_failed") return true;
    const outcome = outcomes[r.localId];
    return r.status === "ready" && outcome !== undefined && outcome !== "pending";
  };
  const allSettled = repos.length > 0 && repos.every(settled);
  const names = repos.map((r) => r.label).join(", ");

  return (
    <div className="space-y-4">
      <div>
        <h2 className="text-title font-semibold">{t("addRepository.scan.heading", { names })}</h2>
        <p className="text-caption text-muted-foreground">{t("addRepository.scan.subheading")}</p>
      </div>

      <div className="space-y-3">
        {repos.map((repo) => (
          <ScanRepoRow
            key={repo.localId}
            repo={repo}
            onRetry={onRetry}
            onOutcomeChange={handleOutcomeChange}
            noScanTimeoutMs={noScanTimeoutMs}
            scanCapMs={scanCapMs}
          />
        ))}
      </div>

      <div className="flex flex-wrap items-center justify-end gap-3">
        <span className="text-caption text-muted-foreground">{t("addRepository.scan.continueHint")}</span>
        {!allSettled && repos.length > 0 && (
          <Button size="lg" variant="outline" onClick={() => onContinue(outcomes)}>
            {t("addRepository.scan.skipWaiting")}
          </Button>
        )}
        <Button size="lg" disabled={!allSettled} onClick={() => onContinue(outcomes)}>
          {t("addRepository.scan.continue")}
        </Button>
      </div>
    </div>
  );
}
