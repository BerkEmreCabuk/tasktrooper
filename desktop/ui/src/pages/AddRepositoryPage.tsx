import { X } from "lucide-react";
import { useState } from "react";
import { useLocation, useNavigate, useSearchParams } from "react-router-dom";
import { DoneStep } from "@/components/projects/add/DoneStep";
import type { SourceMode } from "@/components/projects/add/flow-types";
import { NewRepositoryDoneStep } from "@/components/projects/add/NewRepositoryDoneStep";
import { ReviewStep } from "@/components/projects/add/ReviewStep";
import { ScanStep } from "@/components/projects/add/ScanStep";
import { SourceStep } from "@/components/projects/add/SourceStep";
import { DONE_STEP, useAddRepositoryFlow } from "@/components/projects/add/useAddRepositoryFlow";
import { SetupWizardStepper, type WizardStep } from "@/components/projects/SetupWizardStepper";
import { Button } from "@/components/ui/button";
import { useI18n } from "@/hooks/useI18n";

/**
 * The full-page "Add repository" flow: Source → Scan → Review → Done for an
 * import, Source → Done for a repository created from scratch (nothing to
 * scan yet). Leaving mid-flow is fine — anything already imported/scanning
 * keeps running server-side; there is nothing here to save on unmount.
 */
export function AddRepositoryPage() {
  const { t } = useI18n();
  const navigate = useNavigate();
  const location = useLocation();
  const [searchParams] = useSearchParams();
  const initialProjectId = searchParams.get("project") ?? undefined;

  const flow = useAddRepositoryFlow();
  const [sourceMode, setSourceMode] = useState<SourceMode | null>(null);
  const created = flow.state.created;
  const createPath = created !== null || (flow.state.step === 0 && sourceMode === "new");

  const handleClose = () => {
    if (location.key === "default") navigate("/projects");
    else navigate(-1);
  };

  const steps: WizardStep[] = createPath
    ? [
        { key: "source", label: t("addRepository.steps.source") },
        { key: "done", label: t("addRepository.steps.done") },
      ]
    : [
        { key: "source", label: t("addRepository.steps.source") },
        { key: "scan", label: t("addRepository.steps.scan") },
        { key: "review", label: t("addRepository.steps.review") },
        { key: "done", label: t("addRepository.steps.done") },
      ];

  return (
    <div className="mx-auto flex w-full max-w-3xl flex-col gap-6">
      <div className="flex flex-col gap-4">
        <div className="flex items-center gap-3">
          <Button variant="ghost" size="icon" onClick={handleClose} aria-label={t("addRepository.close")}>
            <X className="h-4 w-4" />
          </Button>
          <h1 className="text-title font-semibold">{t("addRepository.title")}</h1>
        </div>
        <SetupWizardStepper
          steps={steps}
          current={created ? 1 : flow.state.step}
          onSelect={flow.setStep}
          disabled={created !== null}
          label={t("addRepository.stepperLabel")}
        />
      </div>

      {flow.state.step === 0 && (
        <SourceStep
          initialProjectId={initialProjectId}
          onScan={flow.startScan}
          onCreate={flow.createNewRepository}
          onModeChange={setSourceMode}
        />
      )}
      {flow.state.step === 1 && (
        <ScanStep repos={flow.state.repos} onRetry={flow.retryImport} onContinue={flow.continueToReview} />
      )}
      {flow.state.step === 2 && <ReviewStep repos={flow.state.repos} onFinish={flow.finish} />}
      {flow.state.step === DONE_STEP && created && (
        <NewRepositoryDoneStep result={created} projectId={flow.state.projectId ?? ""} projectName={flow.state.projectName} />
      )}
      {flow.state.step === DONE_STEP && !created && flow.state.doneStats && (
        <DoneStep
          repos={flow.state.repos}
          projectId={flow.state.projectId ?? ""}
          projectName={flow.state.projectName}
          stats={flow.state.doneStats}
        />
      )}
    </div>
  );
}
