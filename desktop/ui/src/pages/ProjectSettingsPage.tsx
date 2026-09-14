import { AlertTriangle, ArrowLeft, FolderTree, Plus, RefreshCw, Rocket, Save, Square, Trash2 } from "lucide-react";
import { useCallback, useEffect, useRef, useState } from "react";
import { Link, useNavigate, useParams } from "react-router-dom";
import { toast } from "sonner";
import {
  api,
  type BoardTask,
  type DeployPackage,
  type DeployPackageStatus,
  type InitiativeProject,
  type MobilePlatform,
  type PipelineCategory,
  type PipelineConfigView,
  type RepoKind,
  type Repository,
  type RepositoryProfile,
  type RepoSubProject,
} from "@/api";
import { MultiSelectPicker } from "@/components/admin/MultiSelectPicker";
import { DeployTargetsSection } from "@/components/projects/DeployTargetsSection";
import { GCloudResourcePanel } from "@/components/projects/GCloudResourcePanel";
import { MobileStorePanel } from "@/components/projects/MobileStorePanel";
import { VercelProjectPanel } from "@/components/projects/VercelProjectPanel";
import { DirectoryPickerDialog } from "@/components/projects/DirectoryPickerDialog";
import { PIPELINE_CATEGORIES, PipelineSlots } from "@/components/projects/PipelineSlots";
import { RepoDocsCard } from "@/components/projects/RepoDocsCard";
import { MOBILE_PLATFORMS, SUB_REPO_KINDS, SubRepoSettingsPanel } from "@/components/projects/SubRepoSettingsPanel";
import { ProjectProfileCard } from "@/components/projects/ProjectProfileCard";
import { PageContent } from "@/components/layout/PageContent";
import { PageHeader } from "@/components/admin/PageHeader";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card } from "@/components/ui/card";
import { Checkbox } from "@/components/ui/checkbox";
import { ConfirmDialog } from "@/components/ui/confirm-dialog";
import { EmptyState } from "@/components/ui/empty-state";
import { HelpTooltip } from "@/components/ui/help-tooltip";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Notice } from "@/components/ui/notice";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Skeleton } from "@/components/ui/skeleton";
import { Switch } from "@/components/ui/switch";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { Textarea } from "@/components/ui/textarea";
import { useI18n } from "@/hooks/useI18n";
import { useIndexProgress } from "@/hooks/useIndexProgress";
import { indexProgressPercent } from "@/lib/project-board";

// Profile refresh runs in the background; poll every 5s until the timestamp
// moves, giving up after 3 minutes (the run may legitimately take longer —
// the card just stops spinning).
const PROFILE_POLL_INTERVAL_MS = 5000;
const PROFILE_POLL_TIMEOUT_MS = 3 * 60 * 1000;

const REPO_KINDS: RepoKind[] = ["backend", "frontend", "mobile", "worker", "monorepo"];
const slotKey = (path: string, sub: string, cat: string) => `${path}|${sub}|${cat}`;
// The repo-level tab: sub-repo paths are repo-relative, so "" can never collide
// with one.
const ROOT_TAB = "";

// A release train's status pill. releasing is amber rather than green: deploys
// are in flight and nothing has landed yet.
function deployPackageBadgeVariant(
  status: DeployPackageStatus,
): "default" | "secondary" | "destructive" | "outline" | "success" | "warning" {
  switch (status) {
    case "released":
      return "success";
    case "releasing":
      return "warning";
    case "failed":
      return "destructive";
    case "cancelled":
      return "secondary";
    default:
      return "outline";
  }
}

export function ProjectSettingsPage() {
  const { t } = useI18n();
  const { repositoryId } = useParams();
  const repoId = repositoryId;
  const navigate = useNavigate();
  const [repository, setRepository] = useState<Repository | null>(null);
  const [initiativeProjects, setInitiativeProjects] = useState<InitiativeProject[]>([]);
  const [name, setName] = useState("");
  const [description, setDescription] = useState("");
  const [projectIds, setProjectIds] = useState<string[]>([]);
  const [requireHumanReview, setRequireHumanReview] = useState(false);
  const [requireReviewChain, setRequireReviewChain] = useState(false);
  const [requireReleaseDeploy, setRequireReleaseDeploy] = useState(false);
  // Defaults to true, matching the server column default: this switch turns an
  // existing gate OFF, so an unloaded page must not read as "already off".
  const [requirePipelineForReview, setRequirePipelineForReview] = useState(true);
  // Whole-repo coverage: whether it blocks, and the bar. New-code coverage is
  // always enforced at 90% and is deliberately not exposed — it is the bar every
  // change is held to, not a per-repository choice.
  const [requireOverallCoverage, setRequireOverallCoverage] = useState(false);
  const [coverageThreshold, setCoverageThreshold] = useState("");
  // The mutation-score gate is the coverage gate's twin — same shape, different
  // measure — but it rides the repository PATCH rather than the gates PUT.
  const [mutationEnabled, setMutationEnabled] = useState(false);
  const [mutationThreshold, setMutationThreshold] = useState("");
  const [mobilePlatform, setMobilePlatform] = useState<MobilePlatform>("");
  const [pipelineConfig, setPipelineConfig] = useState<PipelineConfigView | null>(null);
  const [kind, setKind] = useState<RepoKind>("backend");
  const [subRepoKinds, setSubRepoKinds] = useState<string[]>([]);
  const [subProjectsList, setSubProjectsList] = useState<RepoSubProject[]>([]);
  const [subProjectPickerOpen, setSubProjectPickerOpen] = useState(false);
  const [savingSubProjects, setSavingSubProjects] = useState(false);
  const [activeTab, setActiveTab] = useState(ROOT_TAB);
  const [autoRelease, setAutoRelease] = useState(true);
  const [slots, setSlots] = useState<Record<string, string>>({});
  const [pipelineSaving, setPipelineSaving] = useState(false);
  const [creatingSetupTask, setCreatingSetupTask] = useState(false);
  const [loading, setLoading] = useState(true);
  const [saving, setSaving] = useState(false);
  const [watchingIndex, setWatchingIndex] = useState(false);
  const [settingUpWebhook, setSettingUpWebhook] = useState(false);
  const [deleteOpen, setDeleteOpen] = useState(false);
  const [deleting, setDeleting] = useState(false);
  const [profile, setProfile] = useState<RepositoryProfile | null>(null);
  const [profileRefreshing, setProfileRefreshing] = useState(false);
  const profilePollTimer = useRef<number | null>(null);
  // Deploy packages: release trains for a repository that turned per-task auto
  // release off. Listing them is what advances them server-side, so every
  // action here reloads rather than patching local state.
  const [deployPackages, setDeployPackages] = useState<DeployPackage[]>([]);
  const [repoTasks, setRepoTasks] = useState<BoardTask[]>([]);
  const [newPackageName, setNewPackageName] = useState("");
  const [newPackageDescription, setNewPackageDescription] = useState("");
  const [creatingPackage, setCreatingPackage] = useState(false);
  const [membershipPackageId, setMembershipPackageId] = useState<string | null>(null);
  const [membershipDraft, setMembershipDraft] = useState<string[]>([]);
  const [packageBusyId, setPackageBusyId] = useState<string | null>(null);
  const [releaseTarget, setReleaseTarget] = useState<DeployPackage | null>(null);
  const [releasing, setReleasing] = useState(false);
  const { index, percent, refresh: refreshIndex } = useIndexProgress(repoId, {
    forcePoll: watchingIndex,
    onIndexingFinished: () => setWatchingIndex(false),
  });

  const load = useCallback(async () => {
    if (!repoId) return;
    setLoading(true);
    try {
      const data = await api.getRepository(repoId);
      setRepository(data);
      setName(data.name);
      setDescription(data.description);
      setProjectIds(data.project_ids ?? []);
      setRequireHumanReview(data.require_human_review ?? false);
      setRequireReviewChain(data.require_review_chain ?? false);
      setRequireReleaseDeploy(data.require_release_deploy ?? false);
      setRequirePipelineForReview(data.require_pipeline_for_review ?? true);
      setRequireOverallCoverage(data.require_overall_coverage ?? false);
      // 0 means "use the default", which the placeholder shows — an empty box
      // says that better than a literal 0 the owner never typed.
      setCoverageThreshold(data.coverage_threshold ? String(data.coverage_threshold) : "");
      setMutationEnabled(data.mutation_enabled ?? false);
      setMutationThreshold(data.mutation_threshold ? String(data.mutation_threshold) : "");
      setMobilePlatform(data.mobile_platform ?? "");
      setSubProjectsList(data.sub_projects ?? []);
      const projects = await api.listInitiativeProjects();
      setInitiativeProjects(projects.projects ?? []);
    } catch (e) {
      toast.error(e instanceof Error ? e.message : t("projectAdmin.projectSettings.repoLoadFailed"));
    } finally {
      setLoading(false);
    }
  }, [repoId]);

  const applyPipelineConfig = useCallback((cfg: PipelineConfigView) => {
    setPipelineConfig(cfg);
    setKind(cfg.kind ?? "backend");
    setSubRepoKinds(cfg.sub_repo_kinds ?? []);
    setAutoRelease(cfg.auto_release_on_done);
    const savedByKey: Record<string, string> = {};
    for (const j of cfg.saved ?? []) savedByKey[slotKey(j.sub_project_path ?? "", j.sub_repo_kind, j.category)] = j.target_ref;
    const next: Record<string, string> = {};
    for (const s of cfg.suggestions ?? []) {
      const key = slotKey(s.sub_project_path ?? "", s.sub_repo_kind, s.category);
      next[key] = savedByKey[key] ?? s.auto ?? "";
    }
    setSlots(next);
  }, []);

  const loadPipelineConfig = useCallback(async () => {
    if (!repoId) return;
    try {
      const cfg = await api.getPipelineConfig(repoId);
      applyPipelineConfig(cfg);
    } catch (e) {
      toast.error(e instanceof Error ? e.message : t("projectAdmin.projectSettings.pipelineLoadFailed"));
    }
  }, [repoId, applyPipelineConfig]);

  const loadProfile = useCallback(async () => {
    if (!repoId) return null;
    try {
      const data = await api.getRepositoryProfile(repoId);
      setProfile(data);
      return data;
    } catch {
      return null;
    }
  }, [repoId]);

  const loadDeployPackages = useCallback(async () => {
    if (!repoId) return;
    try {
      const [packages, tasks] = await Promise.all([
        api.listDeployPackages(repoId),
        api.listRepositoryTasks(repoId),
      ]);
      setDeployPackages(packages.packages ?? []);
      setRepoTasks(tasks.tasks ?? []);
    } catch (e) {
      toast.error(e instanceof Error ? e.message : t("projectAdmin.projectSettings.deployPackages.loadFailed"));
    }
  }, [repoId]);

  useEffect(() => {
    load();
    loadPipelineConfig();
    loadProfile();
    loadDeployPackages();
    return () => {
      if (profilePollTimer.current !== null) window.clearTimeout(profilePollTimer.current);
    };
  }, [load, loadPipelineConfig, loadProfile, loadDeployPackages]);

  // Applying a proposal writes a repository setting, so the whole settings
  // view is reloaded after it lands — the repo kind or build command the user
  // just accepted is rendered by the form above, not by the profile card.
  const handleApplyProposal = async (proposalId: string) => {
    if (!repoId) return;
    try {
      await api.applyProfileProposal(repoId, proposalId);
      await Promise.all([load(), loadProfile(), loadPipelineConfig()]);
      toast.success(t("projectAdmin.projectSettings.proposalApplied"));
    } catch (e) {
      toast.error(e instanceof Error ? e.message : t("projectAdmin.projectSettings.proposalFailed"));
    }
  };

  const handleDismissProposal = async (proposalId: string) => {
    if (!repoId) return;
    try {
      await api.dismissProfileProposal(repoId, proposalId);
      await loadProfile();
    } catch (e) {
      toast.error(e instanceof Error ? e.message : t("projectAdmin.projectSettings.proposalFailed"));
    }
  };

  const handleRefreshProfile = async () => {
    if (!repoId || profileRefreshing) return;
    setProfileRefreshing(true);
    const before = profile?.profile_updated_at ?? null;
    try {
      const { status } = await api.refreshRepositoryProfile(repoId);
      toast.success(
        status === "already_running"
          ? t("projectAdmin.projectSettings.profileRefreshAlreadyRunning")
          : t("projectAdmin.projectSettings.profileRefreshStarted"),
      );
      const deadline = Date.now() + PROFILE_POLL_TIMEOUT_MS;
      const poll = async () => {
        const latest = await loadProfile();
        if (latest?.profile_updated_at && latest.profile_updated_at !== before) {
          setProfileRefreshing(false);
          toast.success(t("projectAdmin.projectSettings.profileUpdated"));
          return;
        }
        if (Date.now() > deadline) {
          setProfileRefreshing(false);
          toast(t("projectAdmin.projectSettings.profileRefreshTimeout"));
          return;
        }
        profilePollTimer.current = window.setTimeout(poll, PROFILE_POLL_INTERVAL_MS);
      };
      profilePollTimer.current = window.setTimeout(poll, PROFILE_POLL_INTERVAL_MS);
    } catch (e) {
      setProfileRefreshing(false);
      toast.error(e instanceof Error ? e.message : t("projectAdmin.projectSettings.profileRefreshFailed"));
    }
  };

  const handleCreatePackage = async () => {
    if (!repoId || !newPackageName.trim()) return;
    setCreatingPackage(true);
    try {
      await api.createDeployPackage(repoId, {
        name: newPackageName.trim(),
        description: newPackageDescription.trim() || undefined,
      });
      setNewPackageName("");
      setNewPackageDescription("");
      await loadDeployPackages();
      toast.success(t("projectAdmin.projectSettings.deployPackages.created"));
    } catch (e) {
      toast.error(e instanceof Error ? e.message : t("common.saveFailed"));
    } finally {
      setCreatingPackage(false);
    }
  };

  const handleSaveMembership = async (packageId: string) => {
    if (!repoId) return;
    setPackageBusyId(packageId);
    try {
      await api.setDeployPackageTasks(repoId, packageId, membershipDraft);
      setMembershipPackageId(null);
      await loadDeployPackages();
      toast.success(t("projectAdmin.projectSettings.deployPackages.membershipSaved"));
    } catch (e) {
      toast.error(e instanceof Error ? e.message : t("common.saveFailed"));
    } finally {
      setPackageBusyId(null);
    }
  };

  const handleReleasePackage = async () => {
    if (!repoId || !releaseTarget) return;
    setReleasing(true);
    try {
      await api.releaseDeployPackage(repoId, releaseTarget.id);
      setReleaseTarget(null);
      await loadDeployPackages();
      toast.success(t("projectAdmin.projectSettings.deployPackages.releaseStarted"));
    } catch (e) {
      toast.error(e instanceof Error ? e.message : t("projectAdmin.projectSettings.deployPackages.releaseFailed"));
    } finally {
      setReleasing(false);
    }
  };

  const handleCancelPackage = async (packageId: string) => {
    if (!repoId) return;
    setPackageBusyId(packageId);
    try {
      await api.updateDeployPackage(repoId, packageId, { status: "cancelled" });
      await loadDeployPackages();
    } catch (e) {
      toast.error(e instanceof Error ? e.message : t("common.saveFailed"));
    } finally {
      setPackageBusyId(null);
    }
  };

  const handleDeletePackage = async (packageId: string) => {
    if (!repoId) return;
    setPackageBusyId(packageId);
    try {
      await api.deleteDeployPackage(repoId, packageId);
      await loadDeployPackages();
    } catch (e) {
      toast.error(e instanceof Error ? e.message : t("common.saveFailed"));
    } finally {
      setPackageBusyId(null);
    }
  };

  const handleSave = async () => {
    if (!repoId) return;
    setSaving(true);
    try {
      const updated = await api.updateRepository(repoId, {
        name,
        description,
        require_human_review: requireHumanReview,
        mutation_enabled: mutationEnabled,
        // Blank means "use the default", which the server stores as 0.
        mutation_threshold: mutationThreshold.trim() === "" ? 0 : Number(mutationThreshold),
      });
      const withProjects = await api.setRepositoryProjects(repoId, projectIds);
      const withGates = await api.setLifecycleGates(repoId, {
        require_review_chain: requireReviewChain,
        require_release_deploy: requireReleaseDeploy,
        require_pipeline_for_review: requirePipelineForReview,
        require_overall_coverage: requireOverallCoverage,
        // Blank means "use the default", which the server stores as 0.
        coverage_threshold: coverageThreshold.trim() === "" ? 0 : Number(coverageThreshold),
      });
      setRepository({ ...updated, ...withGates, project_ids: withProjects.project_ids });
      toast.success(t("projectAdmin.projectSettings.repoUpdated"));
    } catch (e) {
      toast.error(e instanceof Error ? e.message : t("common.saveFailed"));
    } finally {
      setSaving(false);
    }
  };

  const handleSaveSubProjects = async () => {
    if (!repoId || !repository) return;
    setSavingSubProjects(true);
    try {
      const updated = await api.updateRepository(repoId, {
        name: repository.name,
        description: repository.description,
        sub_projects: subProjectsList,
      });
      setRepository((prev) => (prev ? { ...prev, sub_projects: updated.sub_projects } : prev));
      toast.success(t("projectAdmin.projectSettings.subProjectsSaved"));
      loadPipelineConfig();
    } catch (e) {
      toast.error(e instanceof Error ? e.message : t("common.saveFailed"));
    } finally {
      setSavingSubProjects(false);
    }
  };

  const subRepoLabel = (path: string) =>
    path === "." ? t("projectAdmin.initialSetup.subProjectRoot") : path;

  // A removed sub-repo must not leave the page on a tab that no longer exists.
  const currentTab =
    activeTab !== ROOT_TAB && !subProjectsList.some((sp) => sp.path === activeTab) ? ROOT_TAB : activeTab;

  // One group per real sub-project (by path) when the repo has any, so two
  // sub-projects sharing a kind get their own slots. Falls back to the
  // checkbox-driven kind set for a monorepo that has none yet — mirrors
  // GetPipelineConfig's own fallback server-side.
  const subProjects = repository?.sub_projects ?? [];
  type PipelineGroup = { path: string; kind: string; label: string };
  const activeGroups: PipelineGroup[] =
    kind !== "monorepo"
      ? [{ path: "", kind: "", label: "" }]
      : subProjects.length > 0
        ? subProjects.map((sp) => ({
            path: sp.path,
            kind: sp.kind,
            label: sp.path === "." ? t("projectAdmin.initialSetup.subProjectRoot") : sp.path,
          }))
        : subRepoKinds.map((k) => ({ path: "", kind: k, label: k }));

  const rootGroup: PipelineGroup = activeGroups[0] ?? { path: "", kind: "", label: "" };

  // A sub-repo added but not saved yet has no group of its own on the server,
  // so its slots are keyed by what the panel currently says it is.
  const groupFor = (sp: RepoSubProject): PipelineGroup =>
    activeGroups.find((g) => g.path === sp.path) ?? { path: sp.path, kind: sp.kind, label: sp.path };

  const suggestionsFor = (g: PipelineGroup) =>
    (pipelineConfig?.suggestions ?? []).filter(
      (s) => (s.sub_project_path ?? "") === g.path && s.sub_repo_kind === g.kind,
    );

  const slotValuesFor = (g: PipelineGroup) => {
    const values: Record<string, string> = {};
    for (const cat of PIPELINE_CATEGORIES) values[cat.value] = slots[slotKey(g.path, g.kind, cat.value)] ?? "";
    return values;
  };

  const changeSlot = (g: PipelineGroup) => (category: PipelineCategory, targetRef: string) =>
    setSlots((prev) => ({ ...prev, [slotKey(g.path, g.kind, category)]: targetRef }));

  const handleSavePipeline = async () => {
    if (!repoId) return;
    setPipelineSaving(true);
    try {
      const jobs = [];
      for (const g of activeGroups) {
        for (const cat of PIPELINE_CATEGORIES) {
          const ref = slots[slotKey(g.path, g.kind, cat.value)];
          if (!ref) continue;
          jobs.push({
            sub_project_path: g.path,
            sub_repo_kind: g.kind,
            category: cat.value,
            target_kind: cat.target,
            target_ref: ref,
          });
        }
      }
      const cfg = await api.savePipelineConfig(repoId, {
        kind,
        sub_repo_kinds: kind === "monorepo" ? subRepoKinds : [],
        auto_release_on_done: autoRelease,
        jobs,
      });
      // The platform is a repository column, not part of the pipeline config,
      // but it is asked for next to the repo type — so it saves with it.
      if (kind === "mobile" && repository) {
        const updated = await api.updateRepository(repoId, {
          name: repository.name,
          description: repository.description,
          mobile_platform: mobilePlatform,
        });
        setRepository((prev) => (prev ? { ...prev, mobile_platform: updated.mobile_platform } : prev));
      }
      applyPipelineConfig(cfg);
      toast.success(t("projectAdmin.projectSettings.pipelineSaved"));
    } catch (e) {
      toast.error(e instanceof Error ? e.message : t("projectAdmin.projectSettings.pipelineSaveFailed"));
    } finally {
      setPipelineSaving(false);
    }
  };

  const handleCreateSetupTask = async () => {
    if (!repoId) return;
    setCreatingSetupTask(true);
    try {
      await api.createWorkflowSetupTask(repoId);
      toast.success(t("projectAdmin.projectSettings.setupTaskCreated"));
    } catch (e) {
      toast.error(e instanceof Error ? e.message : t("projectAdmin.projectSettings.taskCreateFailed"));
    } finally {
      setCreatingSetupTask(false);
    }
  };

  const handleReindex = async () => {
    if (!repoId) return;
    setWatchingIndex(true);
    try {
      await api.reindexRepository(repoId);
      await refreshIndex();
      toast.success(t("projectAdmin.projectSettings.reindexStarted"));
    } catch (e) {
      setWatchingIndex(false);
      toast.error(e instanceof Error ? e.message : t("projectAdmin.projectSettings.reindexFailed"));
    }
  };

  // Stopping keeps the partial index: every file already processed has its
  // hash stored, so the next pass resumes from the first file without one.
  const handleStopIndex = async () => {
    if (!repoId) return;
    try {
      const { stopped } = await api.stopRepositoryIndex(repoId);
      setWatchingIndex(false);
      await refreshIndex();
      toast.success(
        stopped
          ? t("projectAdmin.projectSettings.indexStopped")
          : t("projectAdmin.projectSettings.indexNotRunning"),
      );
    } catch (e) {
      toast.error(e instanceof Error ? e.message : t("projectAdmin.projectSettings.indexStopFailed"));
    }
  };

  const handleSetupWebhook = async () => {
    if (!repoId) return;
    setSettingUpWebhook(true);
    try {
      const updated = await api.setupRepositoryWebhook(repoId);
      setRepository(updated);
      toast.success(t("projectAdmin.projectSettings.webhookSetupDone"));
    } catch (e) {
      toast.error(e instanceof Error ? e.message : t("projectAdmin.projectSettings.webhookSetupFailed"));
    } finally {
      setSettingUpWebhook(false);
    }
  };

  const handleDelete = async () => {
    if (!repoId) return;
    setDeleting(true);
    try {
      await api.deleteRepository(repoId);
      toast.success(t("projectAdmin.projectSettings.repoDeleted"));
      navigate("/projects");
    } catch (e) {
      toast.error(e instanceof Error ? e.message : t("projectAdmin.projectSettings.deleteFailed"));
    } finally {
      setDeleting(false);
    }
  };

  const displayPercent = index
    ? indexProgressPercent(index.files_processed, index.files_total, index.status)
    : percent;

  if (loading) {
    return (
      <PageContent className="grid gap-6 xl:grid-cols-2">
        <Skeleton className="h-64 w-full" />
        <Skeleton className="h-48 w-full" />
      </PageContent>
    );
  }

  return (
    <>
      <PageHeader
        title={repository?.name ?? t("projectAdmin.projectSettings.repoSettingsTitle")}
        description={t("projectAdmin.projectSettings.repoSettingsDesc")}
        action={
          <Button variant="outline" asChild className="gap-2">
            <Link to="/projects">
              <ArrowLeft className="h-4 w-4" />
              {t("frame.layout.sidebar.projects")}
            </Link>
          </Button>
        }
      />
      <PageContent className="grid gap-6 xl:grid-cols-2">
        <Card className="w-full space-y-4 p-6">
          <h2 className="font-semibold">{t("projectAdmin.projectSettings.repoInfo")}</h2>
          <div className="space-y-2">
            <Label>{t("projectAdmin.projectSettings.name")}</Label>
            <Input value={name} onChange={(e) => setName(e.target.value)} />
          </div>
          <div className="space-y-2">
            <Label>{t("projectAdmin.projectSettings.description")}</Label>
            <Textarea value={description} onChange={(e) => setDescription(e.target.value)} rows={4} />
          </div>
          <div className="space-y-2">
            <Label>{t("projectAdmin.projectSettings.folderPath")}</Label>
            <Input value={repository?.root_path ?? ""} readOnly className="font-mono text-sm" />
          </div>
          {initiativeProjects.length > 0 && (
            <MultiSelectPicker
              label={t("projectAdmin.projectSettings.linkedProjects")}
              options={initiativeProjects.map((p) => ({ value: p.id, label: p.name }))}
              selected={projectIds}
              onChange={setProjectIds}
            />
          )}
          <div className="flex items-center justify-between gap-4 rounded-md border p-3">
            <div className="flex items-center gap-1.5">
              <Label htmlFor="require-human-review">{t("projectAdmin.projectSettings.requireHumanReview")}</Label>
              <HelpTooltip text={t("projectAdmin.projectSettings.requireHumanReviewHelp")} />
            </div>
            <Switch id="require-human-review" checked={requireHumanReview} onCheckedChange={setRequireHumanReview} />
          </div>
          <div className="flex items-center justify-between gap-4 rounded-md border p-3">
            <div className="flex items-center gap-1.5">
              <Label htmlFor="require-review-chain">{t("projectAdmin.projectSettings.requireReviewChain")}</Label>
              <HelpTooltip text={t("projectAdmin.projectSettings.requireReviewChainHelp")} />
            </div>
            <Switch id="require-review-chain" checked={requireReviewChain} onCheckedChange={setRequireReviewChain} />
          </div>
          <div className="flex items-center justify-between gap-4 rounded-md border p-3">
            <div className="flex items-center gap-1.5">
              <Label htmlFor="require-release-deploy">{t("projectAdmin.projectSettings.requireReleaseDeploy")}</Label>
              <HelpTooltip text={t("projectAdmin.projectSettings.requireReleaseDeployHelp")} />
            </div>
            <Switch id="require-release-deploy" checked={requireReleaseDeploy} onCheckedChange={setRequireReleaseDeploy} />
          </div>
          <div className="flex items-center justify-between gap-4 rounded-md border p-3">
            <div className="flex items-center gap-1.5">
              <Label htmlFor="require-pipeline-for-review">
                {t("projectAdmin.projectSettings.requirePipelineForReview")}
              </Label>
              <HelpTooltip text={t("projectAdmin.projectSettings.requirePipelineForReviewHelp")} />
            </div>
            <Switch
              id="require-pipeline-for-review"
              checked={requirePipelineForReview}
              onCheckedChange={setRequirePipelineForReview}
            />
          </div>
          <div className="space-y-3 rounded-md border p-3">
            <div className="flex items-center justify-between gap-4">
              <div className="flex items-center gap-1.5">
                <Label htmlFor="require-overall-coverage">
                  {t("projectAdmin.projectSettings.requireOverallCoverage")}
                </Label>
                <HelpTooltip text={t("projectAdmin.projectSettings.requireOverallCoverageHelp")} />
              </div>
              <Switch
                id="require-overall-coverage"
                checked={requireOverallCoverage}
                onCheckedChange={setRequireOverallCoverage}
              />
            </div>
            {/* The bar is only meaningful once the gate is armed; showing the
                input while it is off invites setting a number that does nothing. */}
            {requireOverallCoverage && (
              <div className="space-y-1">
                <Label htmlFor="coverage-threshold">{t("projectAdmin.projectSettings.coverageThreshold")}</Label>
                <Input
                  id="coverage-threshold"
                  type="number"
                  min={0}
                  max={100}
                  placeholder="90"
                  value={coverageThreshold}
                  onChange={(e) => setCoverageThreshold(e.target.value)}
                  className="max-w-32"
                />
                <p className="text-xs text-muted-foreground">
                  {t("projectAdmin.projectSettings.coverageThresholdHelp")}
                </p>
              </div>
            )}
          </div>
          <div className="space-y-3 rounded-md border p-3">
            <div className="flex items-center justify-between gap-4">
              <div className="flex items-center gap-1.5">
                <Label htmlFor="mutation-enabled">{t("projectAdmin.projectSettings.mutationGate")}</Label>
                <HelpTooltip text={t("projectAdmin.projectSettings.mutationGateHelp")} />
              </div>
              <Switch id="mutation-enabled" checked={mutationEnabled} onCheckedChange={setMutationEnabled} />
            </div>
            {mutationEnabled && (
              <div className="space-y-1">
                <Label htmlFor="mutation-threshold">{t("projectAdmin.projectSettings.mutationThreshold")}</Label>
                <Input
                  id="mutation-threshold"
                  type="number"
                  min={0}
                  max={100}
                  placeholder="60"
                  value={mutationThreshold}
                  onChange={(e) => setMutationThreshold(e.target.value)}
                  className="max-w-32"
                />
                <p className="text-xs text-muted-foreground">
                  {t("projectAdmin.projectSettings.mutationThresholdHelp")}
                </p>
              </div>
            )}
          </div>
          <div className="flex justify-end">
            <Button onClick={handleSave} disabled={saving}>
              <Save className="mr-2 h-4 w-4" />
              {t("common.save")}
            </Button>
          </div>
        </Card>

        <Card className="w-full space-y-4 p-6">
          <div className="flex items-center justify-between gap-4">
            <h2 className="font-semibold">{t("projectAdmin.projectSettings.codeIndexing")}</h2>
            <div className="flex items-center gap-2">
              {watchingIndex && (
                <Button variant="outline" size="sm" onClick={handleStopIndex}>
                  <Square className="mr-2 h-4 w-4" />
                  {t("projectAdmin.projectSettings.stopIndex")}
                </Button>
              )}
              <Button variant="outline" size="sm" onClick={handleReindex} disabled={watchingIndex}>
                <RefreshCw className={watchingIndex ? "mr-2 h-4 w-4 animate-spin" : "mr-2 h-4 w-4"} />
                {t("projectAdmin.projectSettings.reindex")}
              </Button>
            </div>
          </div>
          {repository && !repository.webhook_installed && (
            <div className="flex items-center justify-between gap-3 rounded-md border border-amber-500/30 bg-amber-500/5 px-3 py-2 text-xs text-amber-700 dark:text-amber-400">
              <div className="flex items-center gap-2">
                <AlertTriangle className="h-4 w-4 shrink-0" />
                <span>{t("projectAdmin.projectSettings.webhookMissing")}</span>
              </div>
              <Button variant="outline" size="sm" className="shrink-0" onClick={handleSetupWebhook} disabled={settingUpWebhook}>
                {settingUpWebhook
                  ? t("projectAdmin.projectSettings.webhookSettingUp")
                  : t("projectAdmin.projectSettings.webhookSetup")}
              </Button>
            </div>
          )}
          {repository?.webhook_installed && (
            <p className="text-xs text-muted-foreground">{t("projectAdmin.projectSettings.webhookActive")}</p>
          )}
          {index ? (
            <>
              <div className="space-y-2">
                <div className="flex justify-between text-sm">
                  <span className="text-muted-foreground">{t("projectAdmin.projectSettings.statusLabel", { status: index.status })}</span>
                  <span className="font-medium">{displayPercent}%</span>
                </div>
                <div className="h-2 overflow-hidden rounded-full bg-muted">
                  <div
                    className="h-full bg-primary transition-all duration-300"
                    style={{ width: `${displayPercent}%` }}
                  />
                </div>
              </div>
              <div className="grid grid-cols-3 gap-3 text-center text-sm">
                <div>
                  <p className="text-muted-foreground">{t("projectAdmin.projectSettings.files")}</p>
                  <p className="font-semibold">{index.file_count}</p>
                </div>
                <div>
                  <p className="text-muted-foreground">{t("projectAdmin.projectSettings.chunks")}</p>
                  <p className="font-semibold">{index.chunk_count}</p>
                </div>
                <div>
                  <p className="text-muted-foreground">{t("projectAdmin.projectSettings.symbols")}</p>
                  <p className="font-semibold">{index.symbol_count}</p>
                </div>
              </div>
              {index.commit_sha && (
                <p className="text-xs text-muted-foreground">
                  {t("projectAdmin.projectSettings.indexedCommit", { sha: index.commit_sha.slice(0, 7) })}
                </p>
              )}
              {index.sync_warning && (
                <div className="flex items-start gap-2 rounded-md border border-amber-500/30 bg-amber-500/5 px-3 py-2 text-xs text-amber-700 dark:text-amber-400">
                  <AlertTriangle className="mt-0.5 h-4 w-4 shrink-0" />
                  <span>{t("projectAdmin.projectSettings.syncWarning", { reason: index.sync_warning })}</span>
                </div>
              )}
              {index.status === "failed" && index.error && (
                <p className="text-sm text-destructive">{index.error}</p>
              )}
            </>
          ) : (
            <p className="text-sm text-muted-foreground">{t("projectAdmin.projectSettings.noIndexYet")}</p>
          )}
        </Card>

        <ProjectProfileCard
          profile={profile}
          refreshing={profileRefreshing}
          onRefresh={handleRefreshProfile}
          onApplyProposal={handleApplyProposal}
          onDismissProposal={handleDismissProposal}
        />

        <div className="space-y-4 xl:col-span-2">
          <Card className="w-full space-y-4 p-6">
            <div className="flex flex-wrap items-start justify-between gap-3">
              <div>
                <h2 className="font-semibold">{t("projectAdmin.projectSettings.reposTitle")}</h2>
                <p className="text-sm text-muted-foreground">{t("projectAdmin.projectSettings.reposDesc")}</p>
              </div>
              <Button variant="outline" size="sm" onClick={loadPipelineConfig}>
                <RefreshCw className="mr-2 h-4 w-4" />
                {t("common.refresh")}
              </Button>
            </div>

            {pipelineConfig && !pipelineConfig.has_workflows && (
              <Notice variant="warning" title={t("projectAdmin.projectSettings.noWorkflowsTitle")}>
                <div className="flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between">
                  <span>{t("projectAdmin.projectSettings.noWorkflowsWarning")}</span>
                  <Button size="sm" onClick={handleCreateSetupTask} disabled={creatingSetupTask} className="shrink-0">
                    {t("projectAdmin.projectSettings.openSetupTask")}
                  </Button>
                </div>
              </Notice>
            )}

            <div className="grid gap-4 sm:grid-cols-2">
              <div className="space-y-2">
                <Label>{t("projectAdmin.projectSettings.repoType")}</Label>
                <Select value={kind} onValueChange={(v) => setKind(v as RepoKind)}>
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
              </div>
              {kind === "mobile" && (
                <div className="space-y-2">
                  <Label>{t("projectAdmin.projectSettings.mobilePlatform")}</Label>
                  <Select
                    value={mobilePlatform || "cross_platform"}
                    onValueChange={(v) => setMobilePlatform(v as MobilePlatform)}
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
              <div className="flex items-center gap-3 sm:pt-6">
                <Switch id="auto-release" checked={autoRelease} onCheckedChange={setAutoRelease} />
                <Label htmlFor="auto-release" className="cursor-pointer">
                  {t("projectAdmin.projectSettings.autoReleaseLabel")}
                </Label>
              </div>
            </div>

            <div className="flex justify-end">
              <Button size="sm" onClick={handleSavePipeline} disabled={pipelineSaving}>
                <Save className="mr-2 h-4 w-4" />
                {t("common.save")}
              </Button>
            </div>
          </Card>

          {kind === "monorepo" ? (
            <Tabs value={currentTab} onValueChange={setActiveTab}>
              <div className="flex flex-wrap items-end justify-between gap-2">
                <TabsList className="flex-1">
                  <TabsTrigger value={ROOT_TAB}>{t("projectAdmin.projectSettings.rootTab")}</TabsTrigger>
                  {subProjectsList.map((sp) => (
                    <TabsTrigger key={sp.path} value={sp.path}>
                      <span className="max-w-48 truncate font-mono text-xs" title={sp.path}>
                        {subRepoLabel(sp.path)}
                      </span>
                      <Badge variant="outline">{t(`projectAdmin.projectSettings.repoKinds.${sp.kind}`)}</Badge>
                    </TabsTrigger>
                  ))}
                </TabsList>
                <Button type="button" variant="outline" size="sm" onClick={() => setSubProjectPickerOpen(true)}>
                  <Plus className="mr-2 h-4 w-4" />
                  {t("projectAdmin.initialSetup.subProjectAdd")}
                </Button>
              </div>

              <TabsContent value={ROOT_TAB} className="grid gap-6 xl:grid-cols-2">
                {repoId && (
                  <RepoDocsCard repositoryId={repoId} title={t("projectAdmin.projectSettings.rootDocsTitle")} />
                )}

                {subProjectsList.length === 0 && (
                  <Card className="w-full p-6">
                    <EmptyState
                      icon={FolderTree}
                      title={t("projectAdmin.projectSettings.noSubReposTitle")}
                      description={t("projectAdmin.projectSettings.noSubReposDesc")}
                      action={
                        <Button type="button" size="sm" onClick={() => setSubProjectPickerOpen(true)}>
                          <Plus className="mr-2 h-4 w-4" />
                          {t("projectAdmin.initialSetup.subProjectAdd")}
                        </Button>
                      }
                    />
                  </Card>
                )}

                {/* The kind checkboxes are the pipeline's fallback routing for a
                    monorepo whose sub-projects were never recorded — the server
                    falls back the same way. Once real paths exist each one owns
                    its slots, in its own tab. */}
                {subProjects.length === 0 && (
                  <Card className="w-full space-y-4 p-6 xl:col-span-2">
                    <div>
                      <h3 className="font-semibold">{t("projectAdmin.projectSettings.pipelineTitle")}</h3>
                      <p className="text-sm text-muted-foreground">{t("projectAdmin.projectSettings.pipelineDesc")}</p>
                    </div>
                    <div className="space-y-2">
                      <Label>{t("projectAdmin.projectSettings.subProjects")}</Label>
                      <div className="flex flex-wrap gap-4">
                        {SUB_REPO_KINDS.map((sk) => (
                          <label key={sk} className="flex items-center gap-2 text-sm">
                            <Checkbox
                              checked={subRepoKinds.includes(sk)}
                              onCheckedChange={(checked) =>
                                setSubRepoKinds((prev) => (checked ? [...prev, sk] : prev.filter((k) => k !== sk)))
                              }
                            />
                            {t(`projectAdmin.projectSettings.repoKinds.${sk}`)}
                          </label>
                        ))}
                      </div>
                    </div>
                    {activeGroups.length === 0 ? (
                      <p className="text-sm text-muted-foreground">
                        {t("projectAdmin.projectSettings.selectSubProjectFirst")}
                      </p>
                    ) : (
                      activeGroups.map((g) => (
                        <div key={g.kind} className="space-y-3 rounded-md border border-border/60 p-4">
                          <h4 className="text-sm font-medium">
                            {t(`projectAdmin.projectSettings.repoKinds.${g.kind}`)}
                          </h4>
                          <PipelineSlots
                            suggestions={suggestionsFor(g)}
                            values={slotValuesFor(g)}
                            onChange={changeSlot(g)}
                          />
                        </div>
                      ))
                    )}
                    <div className="flex justify-end">
                      <Button size="sm" onClick={handleSavePipeline} disabled={pipelineSaving}>
                        <Save className="mr-2 h-4 w-4" />
                        {t("projectAdmin.projectSettings.savePipeline")}
                      </Button>
                    </div>
                  </Card>
                )}
              </TabsContent>

              {repoId &&
                subProjectsList.map((sp) => (
                  <TabsContent key={sp.path} value={sp.path}>
                    <SubRepoSettingsPanel
                      repositoryId={repoId}
                      subProject={sp}
                      label={subRepoLabel(sp.path)}
                      repoCoverageEnabled={requireOverallCoverage}
                      repoCoverageThreshold={coverageThreshold}
                      repoMutationEnabled={mutationEnabled}
                      repoMutationThreshold={mutationThreshold}
                      onChange={(next) =>
                        setSubProjectsList((prev) => prev.map((row) => (row.path === sp.path ? next : row)))
                      }
                      onRemove={() => setSubProjectsList((prev) => prev.filter((row) => row.path !== sp.path))}
                      onSaveMeta={handleSaveSubProjects}
                      savingMeta={savingSubProjects}
                      pipelineSuggestions={suggestionsFor(groupFor(sp))}
                      pipelineValues={slotValuesFor(groupFor(sp))}
                      onPipelineChange={changeSlot(groupFor(sp))}
                      onSavePipeline={handleSavePipeline}
                      savingPipeline={pipelineSaving}
                    />
                  </TabsContent>
                ))}
            </Tabs>
          ) : (
            <div className="grid gap-6 xl:grid-cols-2">
              {repoId && <RepoDocsCard repositoryId={repoId} />}
              <Card className="w-full space-y-4 p-6">
                <div>
                  <h3 className="font-semibold">{t("projectAdmin.projectSettings.pipelineTitle")}</h3>
                  <p className="text-sm text-muted-foreground">{t("projectAdmin.projectSettings.pipelineDesc")}</p>
                </div>
                <PipelineSlots
                  suggestions={suggestionsFor(rootGroup)}
                  values={slotValuesFor(rootGroup)}
                  onChange={changeSlot(rootGroup)}
                  className="lg:grid-cols-2"
                />
                <div className="flex justify-end">
                  <Button size="sm" onClick={handleSavePipeline} disabled={pipelineSaving}>
                    <Save className="mr-2 h-4 w-4" />
                    {t("projectAdmin.projectSettings.savePipeline")}
                  </Button>
                </div>
              </Card>
              {repoId && kind === "mobile" && (
                <MobileStorePanel
                  repositoryId={repoId}
                  mobilePlatform={mobilePlatform}
                  className="xl:col-span-2"
                />
              )}
              {repoId && kind === "frontend" && (
                <VercelProjectPanel repositoryId={repoId} className="xl:col-span-2" />
              )}
              {repoId && (kind === "backend" || kind === "worker") && (
                <GCloudResourcePanel repositoryId={repoId} className="xl:col-span-2" />
              )}
              {repoId && (
                <DeployTargetsSection
                  repositoryId={repoId}
                  className="xl:col-span-2"
                  moreHref={`/repositories/${repoId}/deploy`}
                  kind={kind}
                  mobilePlatform={mobilePlatform}
                />
              )}
            </div>
          )}

          {repoId && (
            <DirectoryPickerDialog
              open={subProjectPickerOpen}
              onOpenChange={setSubProjectPickerOpen}
              repositoryId={repoId}
              excludePaths={subProjectsList.map((sp) => sp.path)}
              onSelect={(path, detectedKind) => {
                if (subProjectsList.some((sp) => sp.path === path)) return;
                setSubProjectsList((prev) => [...prev, { path, kind: detectedKind }]);
                setActiveTab(path);
              }}
            />
          )}
        </div>

        <Card className="w-full space-y-4 p-6 xl:col-span-2">
          <div>
            <h2 className="font-semibold">{t("projectAdmin.projectSettings.deployPackages.title")}</h2>
            <p className="text-sm text-muted-foreground">
              {t("projectAdmin.projectSettings.deployPackages.subtitle")}
            </p>
          </div>

          {deployPackages.length === 0 ? (
            <p className="text-sm text-muted-foreground">
              {t("projectAdmin.projectSettings.deployPackages.empty")}
            </p>
          ) : (
            <div className="divide-y rounded-md border">
              {deployPackages.map((pkg) => {
                const editingMembership = membershipPackageId === pkg.id;
                const busy = packageBusyId === pkg.id;
                const releasable = pkg.status === "draft" || pkg.status === "failed";
                return (
                  <div key={pkg.id} className="space-y-3 px-4 py-3">
                    <div className="flex flex-wrap items-center justify-between gap-2">
                      <div className="min-w-0">
                        <div className="flex items-center gap-2">
                          <span className="font-medium">{pkg.name}</span>
                          <Badge variant={deployPackageBadgeVariant(pkg.status)}>
                            {t(`projectAdmin.projectSettings.deployPackages.status.${pkg.status}`)}
                          </Badge>
                          <span className="text-xs text-muted-foreground">
                            {t("projectAdmin.projectSettings.deployPackages.memberCount", {
                              count: pkg.tasks?.length ?? 0,
                            })}
                          </span>
                        </div>
                        {pkg.description && (
                          <p className="mt-1 text-sm text-muted-foreground">{pkg.description}</p>
                        )}
                        {pkg.status === "failed" && pkg.note && (
                          <p className="mt-1 text-xs text-destructive">{pkg.note}</p>
                        )}
                      </div>
                      <div className="flex shrink-0 items-center gap-2">
                        <Button
                          variant="outline"
                          size="sm"
                          disabled={busy}
                          onClick={() => {
                            if (editingMembership) {
                              setMembershipPackageId(null);
                            } else {
                              setMembershipDraft((pkg.tasks ?? []).map((m) => m.task_id));
                              setMembershipPackageId(pkg.id);
                            }
                          }}
                        >
                          {editingMembership
                            ? t("common.cancel")
                            : t("projectAdmin.projectSettings.deployPackages.editMembers")}
                        </Button>
                        {releasable && (
                          <Button size="sm" disabled={busy} onClick={() => setReleaseTarget(pkg)}>
                            <Rocket className="mr-2 h-3.5 w-3.5" />
                            {t("projectAdmin.projectSettings.deployPackages.release")}
                          </Button>
                        )}
                        {pkg.status === "draft" && (
                          <Button
                            variant="outline"
                            size="sm"
                            disabled={busy}
                            onClick={() => handleCancelPackage(pkg.id)}
                          >
                            {t("projectAdmin.projectSettings.deployPackages.cancelPackage")}
                          </Button>
                        )}
                        <Button
                          variant="ghost"
                          size="icon"
                          className="h-8 w-8 text-muted-foreground hover:text-destructive"
                          disabled={busy}
                          onClick={() => handleDeletePackage(pkg.id)}
                        >
                          <Trash2 className="h-3.5 w-3.5" />
                        </Button>
                      </div>
                    </div>

                    {editingMembership ? (
                      <div className="space-y-3">
                        <MultiSelectPicker
                          label={t("projectAdmin.projectSettings.deployPackages.membersLabel")}
                          options={repoTasks.map((task) => ({
                            value: task.id,
                            label: `${task.key} · ${task.title}`,
                            description: task.column,
                          }))}
                          selected={membershipDraft}
                          onChange={setMembershipDraft}
                          emptyText={t("projectAdmin.projectSettings.deployPackages.noTasks")}
                        />
                        <Button size="sm" disabled={busy} onClick={() => handleSaveMembership(pkg.id)}>
                          <Save className="mr-2 h-3.5 w-3.5" />
                          {t("projectAdmin.projectSettings.deployPackages.saveMembers")}
                        </Button>
                      </div>
                    ) : (
                      (pkg.tasks ?? []).length > 0 && (
                        <div className="flex flex-wrap gap-1.5">
                          {(pkg.tasks ?? []).map((member) => (
                            <Badge key={member.task_id} variant={member.released ? "success" : "outline"}>
                              {member.key ?? member.task_id}
                            </Badge>
                          ))}
                        </div>
                      )
                    )}
                  </div>
                );
              })}
            </div>
          )}

          <div className="space-y-3 rounded-md border border-dashed p-4">
            <div className="space-y-2">
              <Label htmlFor="deploy-package-name">
                {t("projectAdmin.projectSettings.deployPackages.nameLabel")}
              </Label>
              <Input
                id="deploy-package-name"
                value={newPackageName}
                onChange={(e) => setNewPackageName(e.target.value)}
                placeholder={t("projectAdmin.projectSettings.deployPackages.namePlaceholder")}
              />
            </div>
            <div className="space-y-2">
              <Label htmlFor="deploy-package-description">
                {t("projectAdmin.projectSettings.deployPackages.descriptionLabel")}
              </Label>
              <Textarea
                id="deploy-package-description"
                value={newPackageDescription}
                onChange={(e) => setNewPackageDescription(e.target.value)}
                rows={2}
                placeholder={t("projectAdmin.projectSettings.deployPackages.descriptionPlaceholder")}
              />
            </div>
            <Button
              onClick={handleCreatePackage}
              disabled={creatingPackage || !newPackageName.trim()}
              className="gap-2"
            >
              <Plus className="h-4 w-4" />
              {t("projectAdmin.projectSettings.deployPackages.create")}
            </Button>
          </div>
        </Card>

        <Card className="w-full space-y-4 border-destructive/30 p-6 xl:col-span-2">
          <h2 className="font-semibold text-destructive">{t("projectAdmin.projectSettings.dangerZone")}</h2>
          <p className="text-sm text-muted-foreground">
            {t("projectAdmin.projectSettings.deleteRepoWarning")}
          </p>
          <Button variant="destructive" onClick={() => setDeleteOpen(true)} className="gap-2">
            <Trash2 className="h-4 w-4" />
            {t("projectAdmin.projectSettings.deleteRepo")}
          </Button>
        </Card>
      </PageContent>

      <ConfirmDialog
        open={releaseTarget !== null}
        onOpenChange={(open) => !open && setReleaseTarget(null)}
        title={t("projectAdmin.projectSettings.deployPackages.releaseConfirmTitle")}
        description={t("projectAdmin.projectSettings.deployPackages.releaseConfirmDesc", {
          name: releaseTarget?.name ?? "",
          count: releaseTarget?.tasks?.length ?? 0,
        })}
        confirmLabel={t("projectAdmin.projectSettings.deployPackages.release")}
        variant="default"
        loading={releasing}
        onConfirm={handleReleasePackage}
      />

      <ConfirmDialog
        open={deleteOpen}
        onOpenChange={setDeleteOpen}
        title={t("projectAdmin.projectSettings.deleteRepoConfirmTitle")}
        description={t("projectAdmin.projectSettings.deleteRepoConfirmDesc", { name: repository?.name ?? "" })}
        confirmLabel={t("projectAdmin.projectSettings.delete")}
        loading={deleting}
        onConfirm={handleDelete}
      />
    </>
  );
}
