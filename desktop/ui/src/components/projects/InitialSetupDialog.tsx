import { Loader2, Plus, Trash2 } from "lucide-react";
import { useState, type ReactNode } from "react";
import { toast } from "sonner";
import {
  api,
  type MobilePlatform,
  type RepoKind,
  type Repository,
  type RepoSubProject,
  type SubRepoKind,
} from "@/api";
import { FormDialog } from "@/components/admin/FormDialog";
import { DirectoryPickerDialog } from "@/components/projects/DirectoryPickerDialog";
import {
  DEFAULT_PLATFORM,
  MOBILE_PLATFORMS,
  PLATFORM_LABEL_KEY,
  ScopeSetupFields,
  newScopeSetup,
  scopeDocsItems,
  scopeTargets,
  type ScopeSetup,
} from "@/components/projects/ScopeSetupFields";
import { SetupWizardStepper, type WizardStep } from "@/components/projects/SetupWizardStepper";
import { Button } from "@/components/ui/button";
import { Card, CardContent } from "@/components/ui/card";
import { Label } from "@/components/ui/label";
import { Notice } from "@/components/ui/notice";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { useI18n } from "@/hooks/useI18n";

const REPO_KINDS: RepoKind[] = ["backend", "frontend", "mobile", "worker", "monorepo"];
const SUB_PROJECT_KINDS: SubRepoKind[] = ["backend", "frontend", "mobile", "worker"];

/** The scope key of the repository itself. Sub-repos key by their path, and a
 * sub-repo living at the repository root is "." — never "". */
const ROOT_SCOPE = "";

/** One configuration target: the repository, or one sub-repo of a monorepo. */
interface ScopeStep {
  key: string;
  label: string;
  kind: SubRepoKind;
}

function SummaryRow({ label, children }: { label: string; children: ReactNode }) {
  return (
    <div className="flex items-start justify-between gap-4">
      <span className="shrink-0 text-muted-foreground">{label}</span>
      <span className="min-w-0 text-right font-medium">{children}</span>
    </div>
  );
}

interface InitialSetupDialogProps {
  repository: Repository;
  onClose: () => void;
}

/**
 * InitialSetupDialog asks for the deployment facts that were previously never
 * collected anywhere: a freshly imported repository landed with an unset kind
 * and no deploy targets, so the first QA run had no stage address to hit and
 * the deploy settings page had to be found by hand.
 *
 * It is a wizard rather than one long form because a monorepo has as many sets
 * of answers as it has sub-repos: those used to be collected in a second dialog
 * opened on top of this one, per sub-repo, which is both a dead end on a small
 * screen and impossible to review before saving. Now every sub-repo is a step,
 * and the last step is what the Save is about to do.
 *
 * Every FIELD is still optional — leaving one blank keeps the auto-detected
 * kind in place and the addresses empty, which agents fill in after the first
 * deploy with update_deploy_target. The DIALOG itself is not optional: the
 * one-repository-at-a-time import wizard does not offer the next import until
 * this closes via Save, so `dismissable={false}` blocks the backdrop, Escape
 * and the "X" alike.
 */
export function InitialSetupDialog({ repository, onClose }: InitialSetupDialogProps) {
  const { t } = useI18n();
  const [step, setStep] = useState(0);
  const [kind, setKind] = useState<RepoKind>(repository.kind ?? "backend");
  const [platform, setPlatform] = useState<MobilePlatform>(repository.mobile_platform || DEFAULT_PLATFORM);
  const [subProjects, setSubProjects] = useState<RepoSubProject[]>(repository.sub_projects ?? []);
  const [pickerOpen, setPickerOpen] = useState(false);
  const [scopes, setScopes] = useState<Record<string, ScopeSetup>>(() => {
    const initial: Record<string, ScopeSetup> = { [ROOT_SCOPE]: newScopeSetup(repository.docs ?? {}) };
    for (const sp of repository.sub_projects ?? []) initial[sp.path] = newScopeSetup(sp.docs ?? {});
    return initial;
  });
  const [saving, setSaving] = useState(false);

  const isMonorepo = kind === "monorepo";
  const scopeOf = (key: string): ScopeSetup => scopes[key] ?? newScopeSetup();
  const setScope = (key: string, next: ScopeSetup) => setScopes((prev) => ({ ...prev, [key]: next }));

  const scopeSteps: ScopeStep[] = isMonorepo
    ? subProjects.map((sp) => ({
        key: sp.path,
        label: sp.path === "." ? t("projectAdmin.initialSetup.subProjectRoot") : sp.path,
        kind: sp.kind,
      }))
    : [
        {
          key: ROOT_SCOPE,
          label: t("projectAdmin.initialSetup.stepSettings"),
          kind: kind as SubRepoKind,
        },
      ];

  const steps: WizardStep[] = [
    { key: "repo", label: t("projectAdmin.initialSetup.stepRepo") },
    ...scopeSteps.map((s) => ({ key: `scope:${s.key}`, label: s.label })),
    { key: "summary", label: t("projectAdmin.initialSetup.stepSummary") },
  ];

  // Removing the last sub-repo (or leaving the monorepo kind) shrinks the step
  // list under a cursor already past its end, so the index is clamped on read
  // rather than repaired by an effect after a frame of nothing.
  const stepIndex = Math.min(step, steps.length - 1);
  const isSummary = stepIndex === steps.length - 1;
  const activeScope: ScopeStep | undefined = stepIndex > 0 && !isSummary ? scopeSteps[stepIndex - 1] : undefined;

  const plannedTargets = scopeSteps.flatMap((s) => scopeTargets(scopeOf(s.key), s.kind, s.key));
  const plannedDocs = scopeSteps.flatMap((s) => scopeDocsItems(scopeOf(s.key), s.key));

  const updateSub = (path: string, patch: Partial<RepoSubProject>) =>
    setSubProjects((prev) => prev.map((row) => (row.path === path ? { ...row, ...patch } : row)));

  const removeSub = (path: string) => {
    setSubProjects((prev) => prev.filter((row) => row.path !== path));
    setScopes((prev) => {
      const next = { ...prev };
      delete next[path];
      return next;
    });
  };

  const stepTitle = isSummary
    ? t("projectAdmin.initialSetup.stepSummary")
    : activeScope
      ? activeScope.label
      : t("projectAdmin.initialSetup.stepRepo");
  const stepDescription = isSummary
    ? t("projectAdmin.initialSetup.stepSummaryDesc")
    : activeScope
      ? activeScope.key === ROOT_SCOPE
        ? t("projectAdmin.initialSetup.stepSettingsDesc")
        : t("projectAdmin.initialSetup.stepSubRepoDesc", { path: activeScope.label })
      : t("projectAdmin.initialSetup.stepRepoDesc");

  const handleSave = async () => {
    setSaving(true);
    let failed = 0;
    try {
      // Sub-projects only mean something for a monorepo: switching away from it
      // clears whatever was curated, so a stale list can't linger under a kind
      // that no longer has one. Each carries the docs and platform collected on
      // its own step.
      const subProjectsForSave: RepoSubProject[] = isMonorepo
        ? subProjects.map((sp) => ({
            ...sp,
            mobile_platform: sp.kind === "mobile" ? sp.mobile_platform || DEFAULT_PLATFORM : "",
            docs: scopes[sp.path]?.docs ?? sp.docs,
          }))
        : [];
      // name/description are echoed back on purpose: PATCH applies both
      // unconditionally, so omitting the description would clear it.
      await api.updateRepository(repository.id, {
        name: repository.name,
        description: repository.description,
        kind,
        mobile_platform: kind === "mobile" ? platform : "",
        sub_projects: subProjectsForSave,
        docs: isMonorepo ? (repository.docs ?? {}) : scopeOf(ROOT_SCOPE).docs,
      });
    } catch (e) {
      toast.error(e instanceof Error ? e.message : t("common.actionFailed"));
      setSaving(false);
      return;
    }
    for (const target of plannedTargets) {
      try {
        await api.saveDeployTarget(repository.id, target);
      } catch (e) {
        failed += 1;
        const label = target.sub_project_path ? `${target.sub_project_path}/${target.env}` : target.env;
        toast.error(`${label}: ${e instanceof Error ? e.message : t("common.actionFailed")}`);
      }
    }
    // One task for every queued doc across every scope: they are authored on a
    // single branch and land as one PR, merged later from the settings page.
    // It runs last because it records the chosen path onto the repository (or
    // sub-project) record the PATCH above has just written.
    if (plannedDocs.length > 0) {
      try {
        await api.createRepoDocsBundleTask(repository.id, plannedDocs);
      } catch (e) {
        failed += 1;
        toast.error(
          `${t("projectAdmin.initialSetup.docsTaskFailed")}: ${e instanceof Error ? e.message : t("common.actionFailed")}`,
        );
      }
    }
    setSaving(false);
    if (failed === 0) {
      toast.success(
        plannedDocs.length > 0
          ? t("projectAdmin.initialSetup.savedWithDocs", { count: plannedDocs.length })
          : t("projectAdmin.initialSetup.saved"),
      );
    }
    onClose();
  };

  return (
    <FormDialog
      open
      onOpenChange={(open) => {
        if (!open) onClose();
      }}
      dismissable={false}
      title={t("projectAdmin.initialSetup.title")}
      description={t("projectAdmin.initialSetup.description", { name: repository.name })}
      footer={
        <div className="flex w-full gap-2">
          <Button
            variant="outline"
            className="flex-1"
            disabled={saving || stepIndex === 0}
            onClick={() => setStep(stepIndex - 1)}
          >
            {t("projectAdmin.initialSetup.back")}
          </Button>
          <Button
            className="flex-1"
            disabled={saving}
            onClick={() => (isSummary ? void handleSave() : setStep(stepIndex + 1))}
          >
            {saving && <Loader2 className="mr-2 h-4 w-4 animate-spin" />}
            {isSummary ? t("common.save") : t("projectAdmin.initialSetup.next")}
          </Button>
        </div>
      }
    >
      <SetupWizardStepper
        steps={steps}
        current={stepIndex}
        onSelect={setStep}
        disabled={saving}
        label={t("projectAdmin.initialSetup.stepProgress", { current: stepIndex + 1, total: steps.length })}
      />
      <div className="space-y-1">
        <p className="text-sm font-medium">{stepTitle}</p>
        <p className="text-xs text-muted-foreground">{stepDescription}</p>
      </div>

      <div className="min-h-[20rem] space-y-3">
        {stepIndex === 0 && (
          <>
            <div className="space-y-2">
              <Label>{t("projectAdmin.initialSetup.kindLabel")}</Label>
              <Select value={kind} onValueChange={(v) => setKind(v as RepoKind)} disabled={saving}>
                <SelectTrigger>
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  {REPO_KINDS.map((k) => (
                    <SelectItem key={k} value={k}>
                      {t(`projectAdmin.projectSettings.repoKinds.${k}`)}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
              <p className="text-xs text-muted-foreground">{t("projectAdmin.initialSetup.kindHint")}</p>
            </div>

            {kind === "mobile" && (
              <div className="space-y-2">
                <Label>{t("projectAdmin.initialSetup.platformLabel")}</Label>
                <Select
                  value={platform || DEFAULT_PLATFORM}
                  onValueChange={(v) => setPlatform(v as MobilePlatform)}
                  disabled={saving}
                >
                  <SelectTrigger>
                    <SelectValue />
                  </SelectTrigger>
                  <SelectContent>
                    {MOBILE_PLATFORMS.map((p) => (
                      <SelectItem key={p} value={p}>
                        {t(PLATFORM_LABEL_KEY[p])}
                      </SelectItem>
                    ))}
                  </SelectContent>
                </Select>
                <p className="text-xs text-muted-foreground">{t("projectAdmin.initialSetup.platformHint")}</p>
              </div>
            )}

            {isMonorepo && (
              <div className="space-y-2">
                <Label>{t("projectAdmin.initialSetup.subProjectsLabel")}</Label>
                <p className="text-xs text-muted-foreground">{t("projectAdmin.initialSetup.subProjectsHint")}</p>
                {subProjects.length === 0 ? (
                  <p className="text-xs text-muted-foreground">{t("projectAdmin.initialSetup.subProjectsEmpty")}</p>
                ) : (
                  <div className="space-y-2">
                    {subProjects.map((sp) => (
                      <Card key={sp.path}>
                        <CardContent className="space-y-2 p-3">
                          <div className="flex items-center gap-2">
                            <span className="min-w-0 flex-1 truncate font-mono text-xs" title={sp.path}>
                              {sp.path === "." ? t("projectAdmin.initialSetup.subProjectRoot") : sp.path}
                            </span>
                            <Button
                              variant="ghost"
                              size="icon"
                              disabled={saving}
                              aria-label={t("projectAdmin.initialSetup.subProjectRemove")}
                              onClick={() => removeSub(sp.path)}
                            >
                              <Trash2 className="h-4 w-4" />
                            </Button>
                          </div>
                          <div className="flex gap-2">
                            <Select
                              value={sp.kind}
                              disabled={saving}
                              onValueChange={(v) => {
                                const next = v as SubRepoKind;
                                updateSub(sp.path, {
                                  kind: next,
                                  ...(next === "mobile" && !sp.mobile_platform
                                    ? { mobile_platform: DEFAULT_PLATFORM }
                                    : {}),
                                });
                              }}
                            >
                              <SelectTrigger className="flex-1">
                                <SelectValue />
                              </SelectTrigger>
                              <SelectContent>
                                {SUB_PROJECT_KINDS.map((k) => (
                                  <SelectItem key={k} value={k}>
                                    {t(`projectAdmin.projectSettings.repoKinds.${k}`)}
                                  </SelectItem>
                                ))}
                              </SelectContent>
                            </Select>
                            {sp.kind === "mobile" && (
                              <Select
                                value={sp.mobile_platform || DEFAULT_PLATFORM}
                                disabled={saving}
                                onValueChange={(v) => updateSub(sp.path, { mobile_platform: v as MobilePlatform })}
                              >
                                <SelectTrigger className="flex-1">
                                  <SelectValue />
                                </SelectTrigger>
                                <SelectContent>
                                  {MOBILE_PLATFORMS.map((p) => (
                                    <SelectItem key={p} value={p}>
                                      {t(PLATFORM_LABEL_KEY[p])}
                                    </SelectItem>
                                  ))}
                                </SelectContent>
                              </Select>
                            )}
                          </div>
                        </CardContent>
                      </Card>
                    ))}
                  </div>
                )}
                <Button
                  type="button"
                  variant="outline"
                  size="sm"
                  disabled={saving}
                  onClick={() => setPickerOpen(true)}
                >
                  <Plus className="mr-2 h-4 w-4" />
                  {t("projectAdmin.initialSetup.subProjectAdd")}
                </Button>
                <DirectoryPickerDialog
                  open={pickerOpen}
                  onOpenChange={setPickerOpen}
                  repositoryId={repository.id}
                  excludePaths={subProjects.map((sp) => sp.path)}
                  onSelect={(path, detected) => {
                    if (subProjects.some((sp) => sp.path === path)) return;
                    setSubProjects((prev) => [
                      ...prev,
                      {
                        path,
                        kind: detected,
                        ...(detected === "mobile" ? { mobile_platform: DEFAULT_PLATFORM } : {}),
                      },
                    ]);
                    setScopes((prev) => (prev[path] ? prev : { ...prev, [path]: newScopeSetup() }));
                  }}
                />
              </div>
            )}
          </>
        )}

        {activeScope && (
          <ScopeSetupFields
            key={activeScope.key}
            scopePath={activeScope.key}
            kind={activeScope.kind}
            value={scopeOf(activeScope.key)}
            onChange={(next) => setScope(activeScope.key, next)}
            disabled={saving}
          />
        )}

        {isSummary && (
          <>
            <Card>
              <CardContent className="space-y-2 p-4 text-sm">
                <SummaryRow label={t("projectAdmin.initialSetup.kindLabel")}>
                  {t(`projectAdmin.projectSettings.repoKinds.${kind}`)}
                </SummaryRow>
                {kind === "mobile" && (
                  <SummaryRow label={t("projectAdmin.initialSetup.platformLabel")}>
                    {t(PLATFORM_LABEL_KEY[platform || DEFAULT_PLATFORM])}
                  </SummaryRow>
                )}
                {isMonorepo && (
                  <SummaryRow label={t("projectAdmin.initialSetup.subProjectsLabel")}>
                    {subProjects.length === 0 ? (
                      t("projectAdmin.initialSetup.subProjectsEmpty")
                    ) : (
                      <span className="flex flex-col gap-0.5">
                        {subProjects.map((sp) => (
                          <span key={sp.path} className="truncate">
                            <span className="font-mono text-xs">
                              {sp.path === "." ? t("projectAdmin.initialSetup.subProjectRoot") : sp.path}
                            </span>
                            <span className="text-muted-foreground">
                              {" · "}
                              {t(`projectAdmin.projectSettings.repoKinds.${sp.kind}`)}
                              {sp.kind === "mobile" &&
                                ` · ${t(PLATFORM_LABEL_KEY[sp.mobile_platform || DEFAULT_PLATFORM])}`}
                            </span>
                          </span>
                        ))}
                      </span>
                    )}
                  </SummaryRow>
                )}
                <SummaryRow label={t("projectAdmin.initialSetup.docsLabel")}>
                  {plannedDocs.length === 0
                    ? t("projectAdmin.initialSetup.summaryNoDocs")
                    : t("projectAdmin.initialSetup.docsQueuedCount", { count: plannedDocs.length })}
                </SummaryRow>
                <SummaryRow label={t("projectAdmin.initialSetup.summaryTargets")}>
                  {plannedTargets.length === 0
                    ? t("projectAdmin.initialSetup.summaryNoTargets")
                    : t("projectAdmin.initialSetup.summaryTargetsCount", { count: plannedTargets.length })}
                </SummaryRow>
              </CardContent>
            </Card>
            {plannedDocs.length > 0 && (
              <Notice variant="info" title={t("projectAdmin.initialSetup.docsSinglePrTitle")}>
                {t("projectAdmin.initialSetup.docsSinglePrNote")}
              </Notice>
            )}
            <p className="text-xs text-muted-foreground">{t("projectAdmin.initialSetup.footerNote")}</p>
          </>
        )}
      </div>
    </FormDialog>
  );
}
