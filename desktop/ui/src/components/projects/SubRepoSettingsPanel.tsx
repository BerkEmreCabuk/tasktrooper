import { Save, Trash2 } from "lucide-react";
import type { MobilePlatform, PipelineCategory, PipelineCategorySuggestion, RepoSubProject, SubRepoKind } from "@/api";
import { DeployTargetsSection } from "@/components/projects/DeployTargetsSection";
import { GCloudResourcePanel } from "@/components/projects/GCloudResourcePanel";
import { MobileStorePanel } from "@/components/projects/MobileStorePanel";
import { PipelineSlots } from "@/components/projects/PipelineSlots";
import { RepoDocsCard } from "@/components/projects/RepoDocsCard";
import { VercelProjectPanel } from "@/components/projects/VercelProjectPanel";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card } from "@/components/ui/card";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Switch } from "@/components/ui/switch";
import { useI18n } from "@/hooks/useI18n";

export const SUB_REPO_KINDS: SubRepoKind[] = ["backend", "frontend", "mobile", "worker"];
export const MOBILE_PLATFORMS: Exclude<MobilePlatform, "">[] = ["ios", "android", "cross_platform"];

interface TestGateProps {
  id: string;
  label: string;
  /** Repo-level value, used when this sub-repo has no override of its own. */
  inheritedEnabled: boolean;
  inheritedThreshold: string;
  enabled?: boolean;
  threshold?: number;
  onEnabledChange: (value: boolean) => void;
  onThresholdChange: (value: number | undefined) => void;
  thresholdLabel: string;
}

function TestGate({
  id,
  label,
  inheritedEnabled,
  inheritedThreshold,
  enabled,
  threshold,
  onEnabledChange,
  onThresholdChange,
  thresholdLabel,
}: TestGateProps) {
  const { t } = useI18n();
  const effectiveEnabled = enabled ?? inheritedEnabled;

  return (
    <div className="space-y-3 rounded-md border p-3">
      <div className="flex items-center justify-between gap-4">
        <div className="flex items-center gap-2">
          <Label htmlFor={id}>{label}</Label>
          {enabled === undefined && (
            <Badge variant="outline">{t("projectAdmin.projectSettings.gateInherited")}</Badge>
          )}
        </div>
        <Switch id={id} checked={effectiveEnabled} onCheckedChange={onEnabledChange} />
      </div>
      {effectiveEnabled && (
        <div className="space-y-1">
          <Label htmlFor={`${id}-threshold`} className="text-xs font-normal text-muted-foreground">
            {thresholdLabel}
          </Label>
          <Input
            id={`${id}-threshold`}
            type="number"
            min={0}
            max={100}
            className="max-w-32"
            placeholder={inheritedThreshold || "90"}
            value={threshold === undefined ? "" : String(threshold)}
            onChange={(e) => {
              const raw = e.target.value.trim();
              const parsed = Number(raw);
              onThresholdChange(raw === "" || Number.isNaN(parsed) ? undefined : parsed);
            }}
          />
        </div>
      )}
    </div>
  );
}

interface SubRepoSettingsPanelProps {
  repositoryId: string;
  subProject: RepoSubProject;
  /** What this sub-repo is called on screen — the path, or "repository root". */
  label: string;
  repoCoverageEnabled: boolean;
  repoCoverageThreshold: string;
  repoMutationEnabled: boolean;
  repoMutationThreshold: string;
  onChange: (next: RepoSubProject) => void;
  onRemove: () => void;
  onSaveMeta: () => void;
  savingMeta: boolean;
  pipelineSuggestions: PipelineCategorySuggestion[];
  pipelineValues: Record<string, string>;
  onPipelineChange: (category: PipelineCategory, targetRef: string) => void;
  onSavePipeline: () => void;
  savingPipeline: boolean;
}

/**
 * Everything one sub-repo of a monorepo is configured with, in the order the
 * questions come up: what it is, what its tests must clear, which docs describe
 * it, which workflows build it and where it lands.
 *
 * The three saves stay separate because the endpoints behind them are: the meta
 * row and the gates ride the repository's sub_projects PATCH, the slots ride the
 * pipeline PUT, and each environment saves its own deploy target.
 */
export function SubRepoSettingsPanel({
  repositoryId,
  subProject,
  label,
  repoCoverageEnabled,
  repoCoverageThreshold,
  repoMutationEnabled,
  repoMutationThreshold,
  onChange,
  onRemove,
  onSaveMeta,
  savingMeta,
  pipelineSuggestions,
  pipelineValues,
  onPipelineChange,
  onSavePipeline,
  savingPipeline,
}: SubRepoSettingsPanelProps) {
  const { t } = useI18n();
  const isMobile = subProject.kind === "mobile";
  const isFrontend = subProject.kind === "frontend";
  const runsOnGCloud = subProject.kind === "backend" || subProject.kind === "worker";

  return (
    <div className="grid gap-6 xl:grid-cols-2">
      <Card className="w-full space-y-4 p-6">
        <div className="flex items-start justify-between gap-3">
          <div>
            <h3 className="font-semibold">{t("projectAdmin.projectSettings.subRepoMeta")}</h3>
            <p className="text-sm text-muted-foreground">{label}</p>
          </div>
          <Button
            variant="ghost"
            size="icon"
            aria-label={t("projectAdmin.initialSetup.subProjectRemove")}
            onClick={onRemove}
          >
            <Trash2 className="h-4 w-4" />
          </Button>
        </div>

        <div className="grid gap-4 sm:grid-cols-2">
          <div className="space-y-2">
            <Label>{t("projectAdmin.projectSettings.subRepoPath")}</Label>
            <Input value={subProject.path} readOnly className="font-mono text-sm" />
          </div>
          <div className="space-y-2">
            <Label>{t("projectAdmin.projectSettings.subRepoKind")}</Label>
            <Select
              value={subProject.kind}
              onValueChange={(v) => onChange({ ...subProject, kind: v as SubRepoKind })}
            >
              <SelectTrigger>
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                {SUB_REPO_KINDS.map((k) => (
                  <SelectItem key={k} value={k}>
                    {t(`projectAdmin.projectSettings.repoKinds.${k}`)}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          </div>
          {isMobile && (
            <div className="space-y-2">
              <Label>{t("projectAdmin.projectSettings.mobilePlatform")}</Label>
              <Select
                value={subProject.mobile_platform || "cross_platform"}
                onValueChange={(v) => onChange({ ...subProject, mobile_platform: v as MobilePlatform })}
              >
                <SelectTrigger>
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  {MOBILE_PLATFORMS.map((p) => (
                    <SelectItem key={p} value={p}>
                      {t(`projectAdmin.projectSettings.mobilePlatforms.${p}`)}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </div>
          )}
        </div>

        <div className="space-y-3">
          <div>
            <h4 className="text-sm font-medium">{t("projectAdmin.projectSettings.testGates")}</h4>
            <p className="text-xs text-muted-foreground">{t("projectAdmin.projectSettings.testGatesInheritHint")}</p>
          </div>
          <TestGate
            id={`coverage-${subProject.path}`}
            label={t("projectAdmin.projectSettings.coverageGate")}
            thresholdLabel={t("projectAdmin.projectSettings.coverageThreshold")}
            inheritedEnabled={repoCoverageEnabled}
            inheritedThreshold={repoCoverageThreshold}
            enabled={subProject.coverage_enabled}
            threshold={subProject.coverage_threshold}
            onEnabledChange={(v) => onChange({ ...subProject, coverage_enabled: v })}
            onThresholdChange={(v) => onChange({ ...subProject, coverage_threshold: v })}
          />
          <TestGate
            id={`mutation-${subProject.path}`}
            label={t("projectAdmin.projectSettings.mutationGate")}
            thresholdLabel={t("projectAdmin.projectSettings.mutationThreshold")}
            inheritedEnabled={repoMutationEnabled}
            inheritedThreshold={repoMutationThreshold}
            enabled={subProject.mutation_enabled}
            threshold={subProject.mutation_threshold}
            onEnabledChange={(v) => onChange({ ...subProject, mutation_enabled: v })}
            onThresholdChange={(v) => onChange({ ...subProject, mutation_threshold: v })}
          />
        </div>

        <div className="flex justify-end">
          <Button size="sm" onClick={onSaveMeta} disabled={savingMeta}>
            <Save className="mr-2 h-4 w-4" />
            {t("common.save")}
          </Button>
        </div>
      </Card>

      <RepoDocsCard repositoryId={repositoryId} subProjectPath={subProject.path} title={label} />

      {isMobile && (
        <MobileStorePanel
          repositoryId={repositoryId}
          mobilePlatform={subProject.mobile_platform ?? ""}
          className="xl:col-span-2"
        />
      )}

      {isFrontend && (
        <VercelProjectPanel
          repositoryId={repositoryId}
          subProjectPath={subProject.path}
          className="xl:col-span-2"
        />
      )}

      {runsOnGCloud && (
        <GCloudResourcePanel
          repositoryId={repositoryId}
          subProjectPath={subProject.path}
          className="xl:col-span-2"
        />
      )}

      <Card className="w-full space-y-4 p-6 xl:col-span-2">
        <div>
          <h3 className="font-semibold">{t("projectAdmin.projectSettings.pipelineTitle")}</h3>
          <p className="text-sm text-muted-foreground">{t("projectAdmin.projectSettings.pipelineDesc")}</p>
        </div>
        <PipelineSlots
          suggestions={pipelineSuggestions}
          values={pipelineValues}
          onChange={onPipelineChange}
        />
        <div className="flex justify-end">
          <Button size="sm" onClick={onSavePipeline} disabled={savingPipeline}>
            <Save className="mr-2 h-4 w-4" />
            {t("projectAdmin.projectSettings.savePipeline")}
          </Button>
        </div>
      </Card>

      <DeployTargetsSection
        repositoryId={repositoryId}
        className="xl:col-span-2"
        subProjectPath={subProject.path}
        title={t("projectAdmin.prodOps.deployTitle")}
        kind={subProject.kind}
        mobilePlatform={subProject.mobile_platform ?? ""}
      />
    </div>
  );
}
