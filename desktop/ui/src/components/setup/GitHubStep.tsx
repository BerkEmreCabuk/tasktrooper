import { GitHubCard } from "@/components/admin/GitHubCard";
import { Button } from "@/components/ui/button";
import { Notice } from "@/components/ui/notice";
import { useI18n } from "@/hooks/useI18n";
import { useSetup } from "@/hooks/useSetup";
import type { SetupStepId } from "@/lib/setup";

/**
 * Step 3: connect GitHub.
 *
 * The mechanism is the settings affordance itself — `GitHubCard`, the same
 * component Settings → General renders — surfaced here rather than copied. It
 * is one connection per tenant, so a second button for it would be a second
 * place to read a different answer from.
 *
 */
export function GitHubStep({ onContinue }: { onContinue: (next: SetupStepId) => void }) {
  const { t } = useI18n();
  const { steps, reportGitHub } = useSetup();
  const step = steps.github;

  return (
    <div className="space-y-3">
      <p className="text-sm text-muted-foreground">{t("setup.github.description")}</p>

      {step.state === "unknown" && (
        <Notice variant="warning" title={t("setup.state.unknown")}>
          <p>{step.error ?? t("setup.unknownHint")}</p>
          {step.error && <p className="mt-1">{t("setup.unknownHint")}</p>}
        </Notice>
      )}

      {step.state === "todo" && (
        <Notice variant="warning" title={t("setup.github.todoTitle")}>
          {t("setup.github.todoBody")}
        </Notice>
      )}

      {step.state === "done" && (
        <Notice variant="info" title={t("setup.github.doneTitle")}>
          {t("setup.github.doneBody")}
        </Notice>
      )}

      <GitHubCard onStatusChange={reportGitHub} className="mt-0" />

      {step.state === "done" && (
        <Button onClick={() => onContinue("project")}>{t("setup.github.continue")}</Button>
      )}
    </div>
  );
}
