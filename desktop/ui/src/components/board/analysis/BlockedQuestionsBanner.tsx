import { useEffect, useState } from "react";
import { Link } from "react-router-dom";
import { api, type BoardTask, type TaskQuestion } from "@/api";
import { Button } from "@/components/ui/button";
import { useI18n } from "@/hooks/useI18n";
import { analysisReviewPath, pendingBlockingQuestions } from "@/lib/analysis-review";

interface BlockedQuestionsBannerProps {
  task: Pick<BoardTask, "id">;
  repositoryId: string;
}

/**
 * The read-only slice of a question-blocked task shown in the drawer's parked
 * banner (TaskDetailDrawer): which blocking questions are still unanswered,
 * and a link to the analysis report page — the only place an answer can be
 * typed, since it needs the full report for context.
 */
export function BlockedQuestionsBanner({ task, repositoryId }: BlockedQuestionsBannerProps) {
  const { t } = useI18n();
  const [questions, setQuestions] = useState<TaskQuestion[]>([]);

  useEffect(() => {
    let cancelled = false;
    setQuestions([]);
    api
      .listTaskQuestions(repositoryId, task.id)
      .then((data) => {
        if (!cancelled) setQuestions(data.questions ?? []);
      })
      .catch(() => {
        /* the banner still offers the link to the report even if this read failed */
      });
    return () => {
      cancelled = true;
    };
  }, [repositoryId, task.id]);

  const pending = pendingBlockingQuestions(questions);

  return (
    <div className="space-y-2">
      {pending.length > 0 && (
        <ul className="space-y-1">
          {pending.map((question) => (
            <li key={question.id} className="text-sm text-foreground">
              <span className="font-mono text-xs font-semibold text-amber-700 dark:text-amber-400">
                {question.key}
              </span>{" "}
              {question.prompt}
            </li>
          ))}
        </ul>
      )}
      <Button asChild size="sm" variant="outline">
        <Link to={analysisReviewPath(repositoryId, task.id)}>
          {t("boardArea.components.taskDetail.blockedAnswerInReport")}
        </Link>
      </Button>
    </div>
  );
}
