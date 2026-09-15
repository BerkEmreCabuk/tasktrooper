import { Save, Trash2 } from "lucide-react";
import type { MobilePlatform, PipelineCategory, PipelineCategorySuggestion, RepoSubProject, SubRepoKind } from "@/api";
import { MobileStorePanel } from "@/components/projects/MobileStorePanel";
import { PipelineSlots } from "@/components/projects/PipelineSlots";
import { RepoDocsCard } from "@/components/projects/RepoDocsCard";
import { VercelProjectPanel } from "@/components/projects/VercelProjectPanel";
import { Button } from "@/components/ui/button";
import { Card } from "@/components/ui/card";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { useI18n } from "@/hooks/useI18n";

export const SUB_REPO_KINDS: SubRepoKind[] = ["backend", "frontend", "mobile", "worker"];
export const MOBILE_PLATFORMS: Exclude<MobilePlatform, "">[] = ["ios", "android", "cross_platform"];

interface SubRepoSettingsPanelProps {
  repositoryId: string;
  subProject: RepoSubProject;
  /** What this sub-repo is called on screen — the path, or "repository root". */
  label: string;
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
 * questions come up: what it is, which docs describe it and which workflows
 * build it.
 *
 * The saves stay separate because the endpoints behind them are: the meta row
 * rides the repository's sub_projects PATCH and the slots ride the pipeline PUT.
 */
export function SubRepoSettingsPanel({
  repositoryId,
  subProject,
  label,
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

    </div>
  );
}
