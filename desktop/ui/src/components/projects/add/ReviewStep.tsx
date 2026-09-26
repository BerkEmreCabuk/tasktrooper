import { useCallback, useRef, useState } from "react";
import type { RepositoryModel } from "@/api";
import { RepoReviewCard } from "@/components/projects/add/RepoReviewCard";
import type { DoneStats, PendingRepo } from "@/components/projects/add/flow-types";
import { Button } from "@/components/ui/button";
import { Card, CardContent } from "@/components/ui/card";
import { Notice } from "@/components/ui/notice";
import { useI18n } from "@/hooks/useI18n";
import { requiredChecks } from "@/lib/project-model";

interface ReviewStepProps {
  repos: PendingRepo[];
  onFinish: (stats: DoneStats) => void;
}

type Reviewable = PendingRepo & { repositoryId: string };

function unreviewedLineKey(repo: PendingRepo): string {
  if (repo.status === "import_failed") return "addRepository.review.importFailedLine";
  if (repo.status === "importing") return "addRepository.review.stillImportingLine";
  if (repo.scanOutcome === "failed" || repo.scanOutcome === "not_started") return "addRepository.review.notAnalyzedLine";
  return "addRepository.review.stillScanningLine";
}

/**
 * Step 3: one card per repository whose scan succeeded; every other repo gets
 * one line saying why it has nothing to review. Finish waits only for the
 * cards' models (a model that failed to load counts as done), so a flow where
 * nothing was analyzed still finishes. The success notice shows once every
 * card has loaded with no review item left, so it never flashes on top of
 * cards still loading.
 */
export function ReviewStep({ repos, onFinish }: ReviewStepProps) {
  const { t } = useI18n();
  const reviewable = repos.filter(
    (r): r is Reviewable => r.status === "ready" && Boolean(r.repositoryId) && r.scanOutcome === "succeeded",
  );
  const reviewableIds = new Set(reviewable.map((r) => r.localId));
  const unreviewed = repos.filter((r) => !reviewableIds.has(r.localId));

  const [models, setModels] = useState<Record<string, RepositoryModel>>({});
  const [loadFailed, setLoadFailed] = useState<Record<string, boolean>>({});
  const initialReviewRef = useRef<Record<string, number>>({});

  const handleModelChange = useCallback((repositoryId: string, model: RepositoryModel) => {
    setModels((prev) => ({ ...prev, [repositoryId]: model }));
    if (!(repositoryId in initialReviewRef.current)) {
      initialReviewRef.current[repositoryId] = model.review.length;
    }
  }, []);

  const handleLoadError = useCallback((repositoryId: string) => {
    setLoadFailed((prev) => (prev[repositoryId] ? prev : { ...prev, [repositoryId]: true }));
  }, []);

  const allLoaded = reviewable.every((r) => Boolean(models[r.repositoryId]));
  const canFinish = reviewable.every((r) => Boolean(models[r.repositoryId]) || loadFailed[r.repositoryId]);
  const totalReview = Object.values(models).reduce((sum, m) => sum + m.review.length, 0);

  const handleFinish = () => {
    const values = Object.values(models);
    const reviewInitialTotal = Object.values(initialReviewRef.current).reduce((sum, n) => sum + n, 0);
    const stats: DoneStats = {
      components: values.reduce((sum, m) => sum + m.components.filter((c) => c.status === "active").length, 0),
      checks: values.reduce((sum, m) => sum + m.checks.length, 0),
      requiredChecks: values.reduce((sum, m) => sum + requiredChecks(m.checks), 0),
      links: values.reduce((sum, m) => sum + m.links.length, 0),
      reviewAnswered: reviewInitialTotal - totalReview,
    };
    onFinish(stats);
  };

  return (
    <div className="space-y-4">
      <h2 className="text-title font-semibold">{t("addRepository.review.heading")}</h2>

      {reviewable.length > 0 && allLoaded && totalReview === 0 && (
        <Notice variant="info" title={t("addRepository.review.nothingToAsk")} />
      )}

      <div className="space-y-4">
        {reviewable.map((r) => (
          <RepoReviewCard
            key={r.repositoryId}
            repositoryId={r.repositoryId}
            onModelChange={handleModelChange}
            onLoadError={handleLoadError}
          />
        ))}
      </div>

      {unreviewed.length > 0 && (
        <Card>
          <CardContent className="py-3">
            <ul className="divide-y divide-border">
              {unreviewed.map((r) => (
                <li key={r.localId} className="flex flex-wrap items-baseline gap-x-2 py-2 text-body">
                  <span className="truncate font-medium">{r.label}</span>
                  <span className="text-caption text-muted-foreground">{t(unreviewedLineKey(r))}</span>
                </li>
              ))}
            </ul>
          </CardContent>
        </Card>
      )}

      <div className="flex justify-end">
        <Button size="lg" disabled={!canFinish} onClick={handleFinish}>
          {t("addRepository.review.finish")}
        </Button>
      </div>
    </div>
  );
}
