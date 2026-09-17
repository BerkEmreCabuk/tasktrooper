import { Brain, Pencil, Plus, Sparkles, Trash2 } from "lucide-react";
import { useCallback, useEffect, useMemo, useState } from "react";
import { toast } from "sonner";
import {
  api,
  type AgentMemory,
  type MemoryInput,
  type MemoryPromotionCandidate,
  type MemoryScopeFilter,
  type Repository,
} from "@/api";
import { FormDialog } from "@/components/admin/FormDialog";
import { PageHeader } from "@/components/admin/PageHeader";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card } from "@/components/ui/card";
import { Checkbox } from "@/components/ui/checkbox";
import { ConfirmDialog } from "@/components/ui/confirm-dialog";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { EmptyState } from "@/components/ui/empty-state";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { Skeleton } from "@/components/ui/skeleton";
import { Textarea } from "@/components/ui/textarea";
import { useI18n } from "@/hooks/useI18n";
import { cn } from "@/lib/utils";

const emptyForm = (): MemoryInput => ({ content: "", category: "" });

/** Sentinel for "not bound to a repository" in the scope selects. */
const GLOBAL = "__global__";
/** Sentinel for "every scope" in the filter select. */
const ALL = "__all__";

function fmtDate(iso: string) {
  return new Date(iso).toLocaleString("tr-TR", {
    day: "2-digit",
    month: "short",
    year: "numeric",
    hour: "2-digit",
    minute: "2-digit",
  });
}

/** Turns the single filter select into the repository dimension of the query. */
function scopeFilter(selection: string): MemoryScopeFilter {
  if (selection === ALL) return {};
  if (selection === GLOBAL) return { repoScope: "global" };
  return { repositoryId: selection, repoScope: "project" };
}

interface MemoryManagerProps {
  mode: "agent" | "shared";
  agentId?: string;
}

export function MemoryManager({ mode, agentId }: MemoryManagerProps) {
  const { t } = useI18n();
  const sourceLabels: Record<string, string> = {
    agent: t("agentArea.components.memory.source.agent"),
    reflection: t("agentArea.components.memory.source.reflection"),
    user: t("agentArea.components.memory.source.user"),
  };
  const [memories, setMemories] = useState<AgentMemory[]>([]);
  const [repositories, setRepositories] = useState<Repository[]>([]);
  const [filter, setFilter] = useState<string>(ALL);
  const [loading, setLoading] = useState(true);
  const [formOpen, setFormOpen] = useState(false);
  const [form, setForm] = useState<MemoryInput>(emptyForm());
  const [formScope, setFormScope] = useState<string>(GLOBAL);
  const [editingId, setEditingId] = useState<string | null>(null);
  const [saving, setSaving] = useState(false);
  const [deleteId, setDeleteId] = useState<string | null>(null);
  const [deleting, setDeleting] = useState(false);
  const [promoting, setPromoting] = useState(false);
  const [planning, setPlanning] = useState(false);
  const [planOpen, setPlanOpen] = useState(false);
  const [candidates, setCandidates] = useState<MemoryPromotionCandidate[]>([]);
  const [selectedIds, setSelectedIds] = useState<Set<string>>(new Set());
  const [scanned, setScanned] = useState(0);

  const repoNames = useMemo(() => {
    const map: Record<string, string> = {};
    for (const r of repositories) map[r.id] = r.name;
    return map;
  }, [repositories]);

  const refresh = useCallback(async () => {
    setLoading(true);
    try {
      const data =
        mode === "shared"
          ? await api.listSharedMemories(scopeFilter(filter))
          : await api.listAgentMemories(agentId!, "agent", scopeFilter(filter));
      setMemories(data.memories ?? []);
    } catch (e) {
      toast.error(e instanceof Error ? e.message : t("agentArea.components.memory.toast.loadFailed"));
    } finally {
      setLoading(false);
    }
  }, [mode, agentId, filter, t]);

  useEffect(() => {
    if (mode === "agent" && !agentId) return;
    void refresh();
  }, [mode, agentId, refresh]);

  useEffect(() => {
    // Repository names label every project memory, so they load once and stay
    // independent of the memory list itself.
    api
      .listRepositories()
      .then((d) => setRepositories(d.repositories ?? []))
      .catch(() => setRepositories([]));
  }, []);

  const resetForm = () => {
    setForm(emptyForm());
    setFormScope(filter === ALL || filter === GLOBAL ? GLOBAL : filter);
    setEditingId(null);
  };

  const openCreate = () => {
    resetForm();
    setFormOpen(true);
  };

  const openEdit = (memory: AgentMemory) => {
    setEditingId(memory.id);
    setForm({ content: memory.content, category: memory.category ?? "" });
    setFormScope(memory.repository_id ?? GLOBAL);
    setFormOpen(true);
  };

  const closeForm = () => {
    setFormOpen(false);
    resetForm();
  };

  const handleSave = async () => {
    if (!form.content.trim()) return;
    setSaving(true);
    try {
      const body: MemoryInput = {
        content: form.content.trim(),
        category: form.category?.trim() ?? "",
      };
      if (formScope !== GLOBAL) body.repository_id = formScope;
      if (mode === "shared") {
        if (editingId) {
          await api.updateSharedMemory(editingId, body);
          toast.success(t("agentArea.components.memory.toast.teamUpdated"));
        } else {
          await api.createSharedMemory(body);
          toast.success(t("agentArea.components.memory.toast.teamAdded"));
        }
      } else {
        if (editingId) {
          await api.updateAgentMemory(agentId!, editingId, body);
          toast.success(t("agentArea.components.memory.toast.updated"));
        } else {
          await api.createAgentMemory(agentId!, body);
          toast.success(t("agentArea.components.memory.toast.added"));
        }
      }
      closeForm();
      await refresh();
    } catch (e) {
      toast.error(e instanceof Error ? e.message : t("common.saveFailed"));
    } finally {
      setSaving(false);
    }
  };

  const handleDelete = async () => {
    if (!deleteId) return;
    setDeleting(true);
    try {
      if (mode === "shared") {
        await api.deleteSharedMemory(deleteId);
      } else {
        await api.deleteAgentMemory(agentId!, deleteId);
      }
      toast.success(t("agentArea.components.memory.toast.deleted"));
      await refresh();
    } catch (e) {
      toast.error(e instanceof Error ? e.message : t("agentArea.components.memory.toast.deleteFailed"));
    } finally {
      setDeleting(false);
      setDeleteId(null);
    }
  };

  // Two steps, because the first one is an LLM's judgement about what to delete.
  // The button asks the server what it WOULD do; the dialog shows each proposed
  // skill next to the memory it consumes, and only what is still checked when
  // the operator confirms is written.
  const handlePlanPromotion = async () => {
    setPlanning(true);
    try {
      const plan = await api.planSharedMemoryPromotion();
      setScanned(plan.scanned);
      if (plan.candidates.length === 0) {
        toast.info(
          t("agentArea.components.memory.promotePlanEmpty", { scanned: String(plan.scanned) }),
        );
        return;
      }
      setCandidates(plan.candidates);
      setSelectedIds(new Set(plan.candidates.map((c) => c.memory_id)));
      setPlanOpen(true);
    } catch (e) {
      toast.error(e instanceof Error ? e.message : t("agentArea.components.memory.toast.promoteFailed"));
    } finally {
      setPlanning(false);
    }
  };

  const handleApplyPromotion = async () => {
    const approved = candidates.filter((c) => selectedIds.has(c.memory_id));
    if (approved.length === 0) {
      toast.error(t("agentArea.components.memory.promotePlanNoneSelected"));
      return;
    }
    setPromoting(true);
    try {
      const result = await api.promoteSharedMemories(approved);
      if (result.promoted.length === 0) {
        toast.info(t("agentArea.components.memory.toast.promoteNone"));
      } else {
        toast.success(
          t("agentArea.components.memory.toast.promoteDone", {
            count: String(result.promoted.length),
            skills: result.promoted.map((p) => p.skill_name).join(", "),
          }),
        );
      }
      setPlanOpen(false);
      setCandidates([]);
      await refresh();
    } catch (e) {
      toast.error(e instanceof Error ? e.message : t("agentArea.components.memory.toast.promoteFailed"));
    } finally {
      setPromoting(false);
    }
  };

  const toggleCandidate = (memoryId: string) => {
    setSelectedIds((prev) => {
      const next = new Set(prev);
      if (next.has(memoryId)) next.delete(memoryId);
      else next.add(memoryId);
      return next;
    });
  };

  const scopeBadge = (memory: AgentMemory) => (
    <Badge variant="outline">
      {memory.repository_id
        ? (repoNames[memory.repository_id] ?? t("agentArea.components.memory.scopeProject"))
        : t("agentArea.components.memory.scopeGlobal")}
    </Badge>
  );

  const title = mode === "shared" ? t("agentArea.components.memory.titleShared") : t("agentArea.components.memory.titleAgent");
  const description =
    mode === "shared"
      ? t("agentArea.components.memory.descShared")
      : t("agentArea.components.memory.descAgent");

  return (
    <div className="space-y-6">
      <PageHeader
        title={title}
        description={description}
        action={
          <div className="flex items-center gap-2">
            <Select value={filter} onValueChange={setFilter}>
              <SelectTrigger className="w-[200px]">
                <SelectValue placeholder={t("agentArea.components.memory.scopeAll")} />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value={ALL}>{t("agentArea.components.memory.scopeAll")}</SelectItem>
                <SelectItem value={GLOBAL}>{t("agentArea.components.memory.scopeGlobal")}</SelectItem>
                {repositories.map((r) => (
                  <SelectItem key={r.id} value={r.id}>
                    {r.name}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
            {mode === "shared" && (
              <Button
                size="sm"
                variant="outline"
                className="gap-2"
                onClick={handlePlanPromotion}
                disabled={planning || promoting || loading || memories.length === 0}
                title={t("agentArea.components.memory.promoteHint")}
              >
                <Sparkles className="h-4 w-4" />
                {planning
                  ? t("agentArea.components.memory.planning")
                  : promoting
                  ? t("agentArea.components.memory.promoting")
                  : t("agentArea.components.memory.promote")}
              </Button>
            )}
            <Button size="sm" className="gap-2" onClick={openCreate}>
              <Plus className="h-4 w-4" />
              {t("agentArea.components.memory.add")}
            </Button>
          </div>
        }
      />

      {loading ? (
        <div className="space-y-2">
          {Array.from({ length: 3 }).map((_, i) => (
            <Skeleton key={i} className="h-20 w-full" />
          ))}
        </div>
      ) : memories.length === 0 ? (
        <EmptyState
          icon={Brain}
          title={t("agentArea.components.memory.emptyTitle")}
          description={
            mode === "shared"
              ? t("agentArea.components.memory.emptyDescShared")
              : t("agentArea.components.memory.emptyDescAgent")
          }
          action={
            <Button size="sm" className="gap-2" onClick={openCreate}>
              <Plus className="h-4 w-4" />
              {t("agentArea.components.memory.addFirst")}
            </Button>
          }
        />
      ) : (
        <div className="space-y-2">
          {memories.map((m) => (
            <Card key={m.id} className="flex items-start justify-between gap-3 p-3">
              <div className="min-w-0 flex-1 space-y-1">
                <p className="text-sm whitespace-pre-wrap">{m.content}</p>
                <div className="flex flex-wrap items-center gap-2 text-xs text-muted-foreground">
                  {scopeBadge(m)}
                  {m.category ? <Badge variant="secondary">{m.category}</Badge> : null}
                  <span>{sourceLabels[m.source] ?? m.source}</span>
                  <span>·</span>
                  <span>{fmtDate(m.created_at)}</span>
                </div>
              </div>
              <div className="flex shrink-0 gap-1">
                <Button variant="ghost" size="sm" onClick={() => openEdit(m)}>
                  <Pencil className="h-4 w-4" />
                </Button>
                <Button variant="ghost" size="sm" onClick={() => setDeleteId(m.id)}>
                  <Trash2 className="h-4 w-4 text-destructive" />
                </Button>
              </div>
            </Card>
          ))}
        </div>
      )}

      <FormDialog
        open={formOpen}
        onOpenChange={(open) => {
          if (!open) closeForm();
          else setFormOpen(true);
        }}
        title={editingId ? t("agentArea.components.memory.formTitleEdit") : t("agentArea.components.memory.formTitleNew")}
        description={mode === "shared" ? t("agentArea.components.memory.formDescShared") : t("agentArea.components.memory.formDescAgent")}
        footer={
          <>
            <Button variant="outline" onClick={closeForm} disabled={saving}>
              {t("common.cancel")}
            </Button>
            <Button onClick={handleSave} disabled={saving || !form.content.trim()}>
              {saving ? t("common.saving") : editingId ? t("agentArea.components.memory.update") : t("agentArea.components.memory.add")}
            </Button>
          </>
        }
      >
        <div className="space-y-2">
          <Label htmlFor="memory-scope">{t("agentArea.components.memory.scopeLabel")}</Label>
          <Select value={formScope} onValueChange={setFormScope} disabled={editingId !== null}>
            <SelectTrigger id="memory-scope">
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value={GLOBAL}>{t("agentArea.components.memory.scopeGlobalOption")}</SelectItem>
              {repositories.map((r) => (
                <SelectItem key={r.id} value={r.id}>
                  {r.name}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
          <p className="text-xs text-muted-foreground">
            {editingId
              ? t("agentArea.components.memory.scopeLocked")
              : t("agentArea.components.memory.scopeHint")}
          </p>
        </div>
        <div className="space-y-2">
          <Label htmlFor="memory-content">{t("agentArea.components.memory.contentLabel")}</Label>
          <Textarea
            id="memory-content"
            value={form.content}
            onChange={(e) => setForm((f) => ({ ...f, content: e.target.value }))}
            rows={4}
            placeholder={t("agentArea.components.memory.contentPlaceholder")}
          />
        </div>
        <div className="space-y-2">
          <Label htmlFor="memory-category">{t("agentArea.components.memory.categoryLabel")}</Label>
          <Input
            id="memory-category"
            value={form.category ?? ""}
            onChange={(e) => setForm((f) => ({ ...f, category: e.target.value }))}
            placeholder={t("agentArea.components.memory.categoryPlaceholder")}
          />
        </div>
      </FormDialog>

      {/* What the promotion would do, before it does it: one row per proposed
          skill, the memory it consumes underneath it, and the agents it lands
          on. Unchecking a row leaves that memory exactly as it is. */}
      <Dialog open={planOpen} onOpenChange={(open) => !promoting && setPlanOpen(open)}>
        <DialogContent className="flex max-h-[85vh] max-w-2xl flex-col gap-0 overflow-hidden p-0">
          <DialogHeader className="space-y-2 border-b border-border p-6">
            <DialogTitle>{t("agentArea.components.memory.promotePlanTitle")}</DialogTitle>
            <DialogDescription>
              {t("agentArea.components.memory.promotePlanDescription", {
                count: String(candidates.length),
                scanned: String(scanned),
              })}
            </DialogDescription>
          </DialogHeader>

          <div className="min-h-0 flex-1 space-y-3 overflow-y-auto p-6">
            {candidates.map((candidate) => {
              const checked = selectedIds.has(candidate.memory_id);
              return (
                <Card
                  key={candidate.memory_id}
                  className={cn("p-4", !checked && "opacity-60")}
                >
                  <div className="flex items-start gap-3">
                    <Checkbox
                      id={`promote-${candidate.memory_id}`}
                      checked={checked}
                      onCheckedChange={() => toggleCandidate(candidate.memory_id)}
                      className="mt-1"
                    />
                    <div className="min-w-0 flex-1 space-y-2">
                      <Label
                        htmlFor={`promote-${candidate.memory_id}`}
                        className="cursor-pointer text-sm font-semibold"
                      >
                        {candidate.skill_name}
                      </Label>
                      {candidate.description && (
                        <p className="text-sm text-muted-foreground">{candidate.description}</p>
                      )}
                      <div className="flex flex-wrap items-center gap-1">
                        <span className="text-micro uppercase tracking-wide text-muted-foreground">
                          {t("agentArea.components.memory.promotePlanAgents")}
                        </span>
                        {candidate.agents.map((agent) => (
                          <Badge key={agent} variant="secondary" className="text-micro">
                            {agent}
                          </Badge>
                        ))}
                      </div>
                      <div className="rounded-md border border-border/60 bg-muted/30 p-3">
                        <p className="text-micro uppercase tracking-wide text-muted-foreground">
                          {t("agentArea.components.memory.promotePlanFrom")}
                        </p>
                        <p className="mt-1 text-sm whitespace-pre-wrap">{candidate.memory_content}</p>
                      </div>
                      <details className="text-sm">
                        <summary className="cursor-pointer text-xs text-muted-foreground">
                          {t("agentArea.components.memory.contentLabel")}
                        </summary>
                        <p className="mt-2 whitespace-pre-wrap text-sm">{candidate.content}</p>
                      </details>
                    </div>
                  </div>
                </Card>
              );
            })}
          </div>

          <DialogFooter className="gap-2 border-t border-border p-4">
            <Button variant="outline" onClick={() => setPlanOpen(false)} disabled={promoting}>
              {t("agentArea.components.memory.promotePlanCancel")}
            </Button>
            <Button onClick={handleApplyPromotion} disabled={promoting || selectedIds.size === 0}>
              {promoting
                ? t("agentArea.components.memory.promoting")
                : t("agentArea.components.memory.promotePlanConfirm", {
                    count: String(selectedIds.size),
                  })}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      <ConfirmDialog
        open={deleteId !== null}
        onOpenChange={(open) => {
          if (!open) setDeleteId(null);
        }}
        title={t("agentArea.components.memory.deleteTitle")}
        description={t("agentArea.components.memory.deleteDesc")}
        confirmLabel={t("agentArea.components.memory.deleteConfirm")}
        loading={deleting}
        onConfirm={handleDelete}
      />
    </div>
  );
}
