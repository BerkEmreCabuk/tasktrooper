import { Layers, Pencil, Plus, Sparkles, Trash2 } from "lucide-react";
import { type ReactNode, useCallback, useEffect, useMemo, useState } from "react";
import { useParams } from "react-router-dom";
import { toast } from "sonner";
import { api, type Skill, type SkillInput, type TechStack, type TechStackInput } from "@/api";
import { FormDialog } from "@/components/admin/FormDialog";
import { PageHeader } from "@/components/admin/PageHeader";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card } from "@/components/ui/card";
import { ConfirmDialog } from "@/components/ui/confirm-dialog";
import { EmptyState } from "@/components/ui/empty-state";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Skeleton } from "@/components/ui/skeleton";
import { Switch } from "@/components/ui/switch";
import { Textarea } from "@/components/ui/textarea";
import { useI18n } from "@/hooks/useI18n";
import { commaListToArray } from "@/lib/utils";

// Radix Select cannot hold "" as an item value, so a general (tech_stack_id
// === null) skill is represented by this sentinel while it's in the form.
const GENERAL_STACK = "__general__";

const emptySkill = (techStackId: string | null = null): SkillInput => ({
  name: "",
  description: "",
  category: "",
  tags: [],
  content: "",
  enabled: true,
  tech_stack_id: techStackId,
});

const emptyTechStack = (): TechStackInput => ({
  name: "",
  description: "",
  position: 0,
});

interface TechStackSectionProps {
  title: string;
  description?: string;
  skills: Skill[];
  emptyText: string;
  onAddSkill: () => void;
  onEditSkill: (skill: Skill) => void;
  onDeleteSkill: (id: string) => void;
  /** Rename/delete controls for a real tech stack. Omitted for General. */
  headerActions?: ReactNode;
}

/** One collapsible-free section of the skills list: a stack's (or General's)
 * header plus its own skills table, so "add skill" always knows which stack
 * it's adding into. */
function TechStackSection({
  title,
  description,
  skills,
  emptyText,
  onAddSkill,
  onEditSkill,
  onDeleteSkill,
  headerActions,
}: TechStackSectionProps) {
  const { t } = useI18n();
  return (
    <Card className="overflow-hidden">
      <div className="flex flex-wrap items-start justify-between gap-3 border-b border-border p-4">
        <div>
          <h2 className="font-semibold">{title}</h2>
          {description && <p className="text-sm text-muted-foreground">{description}</p>}
        </div>
        <div className="flex shrink-0 items-center gap-1">
          <Button size="sm" variant="outline" className="gap-2" onClick={onAddSkill}>
            <Plus className="h-4 w-4" />
            {t("content.skills.addSkill")}
          </Button>
          {headerActions}
        </div>
      </div>
      {skills.length === 0 ? (
        <p className="p-4 text-sm text-muted-foreground">{emptyText}</p>
      ) : (
        <div className="overflow-x-auto">
          <table className="w-full text-sm">
            <thead>
              <tr className="border-b border-border bg-muted/50">
                <th className="px-4 py-3 text-left font-medium">{t("content.skills.colName")}</th>
                <th className="px-4 py-3 text-left font-medium">{t("content.skills.colCategory")}</th>
                <th className="px-4 py-3 text-left font-medium">{t("content.skills.colTags")}</th>
                <th className="px-4 py-3 text-left font-medium">{t("content.skills.colStatus")}</th>
                <th className="px-4 py-3 w-24" />
              </tr>
            </thead>
            <tbody>
              {skills.map((skill) => (
                <tr key={skill.id} className="border-b border-border last:border-0 hover:bg-muted/30">
                  <td className="px-4 py-3 font-medium">{skill.name}</td>
                  <td className="px-4 py-3 text-muted-foreground">{skill.category}</td>
                  <td className="px-4 py-3">
                    <div className="flex flex-wrap gap-1">
                      {(skill.tags ?? []).map((tag) => (
                        <Badge key={tag} variant="secondary" className="text-micro">
                          {tag}
                        </Badge>
                      ))}
                    </div>
                  </td>
                  <td className="px-4 py-3">
                    <Badge variant={skill.enabled ? "success" : "secondary"}>
                      {skill.enabled ? t("content.skills.statusEnabled") : t("content.skills.statusDisabled")}
                    </Badge>
                  </td>
                  <td className="px-4 py-3">
                    <div className="flex gap-1">
                      <Button variant="ghost" size="icon" onClick={() => onEditSkill(skill)}>
                        <Pencil className="h-4 w-4" />
                      </Button>
                      <Button variant="ghost" size="icon" onClick={() => onDeleteSkill(skill.id)}>
                        <Trash2 className="h-4 w-4 text-destructive" />
                      </Button>
                    </div>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
    </Card>
  );
}

export function SkillsPage() {
  const { t } = useI18n();
  const { agentId } = useParams();
  const [skills, setSkills] = useState<Skill[]>([]);
  const [techStacks, setTechStacks] = useState<TechStack[]>([]);
  const [loading, setLoading] = useState(true);

  const [formOpen, setFormOpen] = useState(false);
  const [form, setForm] = useState<SkillInput>(emptySkill());
  const [editingId, setEditingId] = useState<string | null>(null);
  const [saving, setSaving] = useState(false);
  const [deleteId, setDeleteId] = useState<string | null>(null);
  const [deleting, setDeleting] = useState(false);

  const [stackFormOpen, setStackFormOpen] = useState(false);
  const [stackForm, setStackForm] = useState<TechStackInput>(emptyTechStack());
  const [editingStackId, setEditingStackId] = useState<string | null>(null);
  const [stackSaving, setStackSaving] = useState(false);
  const [stackDeleteId, setStackDeleteId] = useState<string | null>(null);
  const [stackDeleting, setStackDeleting] = useState(false);

  const refresh = useCallback(async () => {
    if (!agentId || agentId === "new") return;
    setLoading(true);
    try {
      const [skillsRes, stacksRes] = await Promise.all([
        api.listAgentSkills(agentId),
        api.listAgentTechStacks(agentId),
      ]);
      setSkills(skillsRes.skills ?? []);
      setTechStacks(stacksRes.tech_stacks ?? []);
    } catch (e) {
      toast.error(e instanceof Error ? e.message : t("content.skills.loadFailed"));
    } finally {
      setLoading(false);
    }
  }, [agentId, t]);

  useEffect(() => {
    refresh();
  }, [refresh]);

  // The tags field is edited as raw text and only parsed on save. Round-tripping
  // it through the array on every keystroke swallowed the separator itself —
  // typing "," produced no entry, so a second tag could never be started.
  const [tagsText, setTagsText] = useState("");

  const openCreate = (techStackId: string | null) => {
    setForm(emptySkill(techStackId));
    setTagsText("");
    setEditingId(null);
    setFormOpen(true);
  };

  const openEdit = (skill: Skill) => {
    setEditingId(skill.id);
    setForm({
      name: skill.name,
      description: skill.description,
      category: skill.category,
      tags: skill.tags ?? [],
      content: skill.content,
      enabled: skill.enabled,
      tech_stack_id: skill.tech_stack_id,
    });
    setTagsText((skill.tags ?? []).join(", "));
    setFormOpen(true);
  };

  const closeForm = () => {
    setFormOpen(false);
    setForm(emptySkill());
    setTagsText("");
    setEditingId(null);
  };

  const handleSave = async () => {
    if (!form.name.trim()) return;
    setSaving(true);
    const payload = { ...form, tags: commaListToArray(tagsText) ?? [] };
    try {
      if (editingId) {
        await api.updateAgentSkill(agentId!, editingId, payload);
        toast.success(t("content.skills.updatedToast"));
      } else {
        await api.createAgentSkill(agentId!, payload);
        toast.success(t("content.skills.createdToast"));
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
      await api.deleteAgentSkill(agentId!, deleteId);
      toast.success(t("content.skills.deletedToast"));
      await refresh();
    } catch (e) {
      toast.error(e instanceof Error ? e.message : t("content.skills.deleteFailed"));
    } finally {
      setDeleting(false);
      setDeleteId(null);
    }
  };

  const openCreateStack = () => {
    setStackForm({ ...emptyTechStack(), position: techStacks.length });
    setEditingStackId(null);
    setStackFormOpen(true);
  };

  const openEditStack = (stack: TechStack) => {
    setEditingStackId(stack.id);
    setStackForm({ name: stack.name, description: stack.description, position: stack.position });
    setStackFormOpen(true);
  };

  const closeStackForm = () => {
    setStackFormOpen(false);
    setStackForm(emptyTechStack());
    setEditingStackId(null);
  };

  const handleSaveStack = async () => {
    if (!stackForm.name.trim()) return;
    setStackSaving(true);
    try {
      if (editingStackId) {
        await api.updateAgentTechStack(agentId!, editingStackId, {
          name: stackForm.name,
          description: stackForm.description,
        });
        toast.success(t("content.skills.techStackUpdatedToast"));
      } else {
        await api.createAgentTechStack(agentId!, stackForm);
        toast.success(t("content.skills.techStackCreatedToast"));
      }
      closeStackForm();
      await refresh();
    } catch (e) {
      toast.error(e instanceof Error ? e.message : t("content.skills.techStackSaveFailed"));
    } finally {
      setStackSaving(false);
    }
  };

  const handleDeleteStack = async () => {
    if (!stackDeleteId) return;
    setStackDeleting(true);
    try {
      await api.deleteAgentTechStack(agentId!, stackDeleteId);
      toast.success(t("content.skills.techStackDeletedToast"));
      await refresh();
    } catch (e) {
      toast.error(e instanceof Error ? e.message : t("content.skills.techStackDeleteFailed"));
    } finally {
      setStackDeleting(false);
      setStackDeleteId(null);
    }
  };

  const sortedStacks = useMemo(() => [...techStacks].sort((a, b) => a.position - b.position), [techStacks]);
  const generalSkills = useMemo(() => skills.filter((s) => !s.tech_stack_id), [skills]);
  const skillsForStack = useCallback(
    (stackId: string) => skills.filter((s) => s.tech_stack_id === stackId),
    [skills],
  );
  const deletingStack = stackDeleteId ? (techStacks.find((s) => s.id === stackDeleteId) ?? null) : null;
  const isEmpty = !loading && skills.length === 0 && techStacks.length === 0;

  return (
    <>
      <PageHeader
        title={t("content.skills.title")}
        description={t("content.skills.description")}
        action={
          <Button onClick={openCreateStack} className="gap-2">
            <Layers className="h-4 w-4" />
            {t("content.skills.addTechStack")}
          </Button>
        }
      />

      {loading ? (
        <div className="space-y-2">
          {Array.from({ length: 3 }).map((_, i) => (
            <Skeleton key={i} className="h-14 w-full" />
          ))}
        </div>
      ) : isEmpty ? (
        <EmptyState
          icon={Sparkles}
          title={t("content.skills.emptyTitle")}
          description={t("content.skills.emptyDescription")}
          action={
            <Button onClick={() => openCreate(null)} className="gap-2">
              <Plus className="h-4 w-4" />
              {t("content.skills.addSkill")}
            </Button>
          }
        />
      ) : (
        <div className="space-y-6">
          <TechStackSection
            title={t("content.skills.generalTitle")}
            description={t("content.skills.generalDescription")}
            skills={generalSkills}
            emptyText={t("content.skills.generalEmptyDescription")}
            onAddSkill={() => openCreate(null)}
            onEditSkill={openEdit}
            onDeleteSkill={setDeleteId}
          />
          {sortedStacks.map((stack) => (
            <TechStackSection
              key={stack.id}
              title={stack.name}
              description={stack.description}
              skills={skillsForStack(stack.id)}
              emptyText={t("content.skills.techStackEmptyDescription")}
              onAddSkill={() => openCreate(stack.id)}
              onEditSkill={openEdit}
              onDeleteSkill={setDeleteId}
              headerActions={
                <>
                  <Button variant="ghost" size="icon" onClick={() => openEditStack(stack)}>
                    <Pencil className="h-4 w-4" />
                  </Button>
                  <Button variant="ghost" size="icon" onClick={() => setStackDeleteId(stack.id)}>
                    <Trash2 className="h-4 w-4 text-destructive" />
                  </Button>
                </>
              }
            />
          ))}
        </div>
      )}

      <FormDialog
        open={formOpen}
        onOpenChange={(open) => !open && closeForm()}
        title={editingId ? t("content.skills.editTitle") : t("content.skills.newSkill")}
        description={editingId ? t("content.skills.editDescription") : t("content.skills.createDescription")}
        footer={
          <>
            <Button variant="outline" onClick={closeForm}>
              {t("common.cancel")}
            </Button>
            <Button onClick={handleSave} disabled={!form.name.trim() || saving}>
              {saving ? t("common.saving") : editingId ? t("content.skills.update") : t("content.skills.create")}
            </Button>
          </>
        }
      >
        <div className="grid gap-4 sm:grid-cols-2">
          <div className="space-y-2">
            <Label>{t("content.skills.fieldName")}</Label>
            <Input value={form.name} onChange={(e) => setForm((f) => ({ ...f, name: e.target.value }))} />
          </div>
          <div className="space-y-2">
            <Label>{t("content.skills.fieldCategory")}</Label>
            <Input value={form.category} onChange={(e) => setForm((f) => ({ ...f, category: e.target.value }))} />
          </div>
        </div>
        <div className="space-y-2">
          <Label>{t("content.skills.fieldTechStack")}</Label>
          <Select
            value={form.tech_stack_id ?? GENERAL_STACK}
            onValueChange={(v) => setForm((f) => ({ ...f, tech_stack_id: v === GENERAL_STACK ? null : v }))}
          >
            <SelectTrigger>
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value={GENERAL_STACK}>{t("content.skills.techStackGeneralOption")}</SelectItem>
              {techStacks.map((stack) => (
                <SelectItem key={stack.id} value={stack.id}>
                  {stack.name}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
        </div>
        <div className="space-y-2">
          <Label>{t("content.skills.fieldDescription")}</Label>
          <Input value={form.description} onChange={(e) => setForm((f) => ({ ...f, description: e.target.value }))} />
        </div>
        <div className="space-y-2">
          <Label>{t("content.skills.fieldTags")}</Label>
          <Input value={tagsText} onChange={(e) => setTagsText(e.target.value)} />
        </div>
        <div className="space-y-2">
          <Label>{t("content.skills.fieldContent")}</Label>
          <Textarea rows={6} value={form.content} onChange={(e) => setForm((f) => ({ ...f, content: e.target.value }))} />
        </div>
        <div className="flex items-center gap-2">
          <Switch checked={form.enabled} onCheckedChange={(v) => setForm((f) => ({ ...f, enabled: v }))} />
          <Label>{t("content.skills.fieldEnabled")}</Label>
        </div>
      </FormDialog>

      <ConfirmDialog
        open={deleteId !== null}
        onOpenChange={(open) => !open && setDeleteId(null)}
        title={t("content.skills.deleteTitle")}
        description={t("content.skills.deleteDescription")}
        confirmLabel={t("content.skills.deleteConfirm")}
        loading={deleting}
        onConfirm={handleDelete}
      />

      <FormDialog
        open={stackFormOpen}
        onOpenChange={(open) => !open && closeStackForm()}
        title={editingStackId ? t("content.skills.editTechStackTitle") : t("content.skills.newTechStackTitle")}
        description={
          editingStackId
            ? t("content.skills.editTechStackDescription")
            : t("content.skills.createTechStackDescription")
        }
        footer={
          <>
            <Button variant="outline" onClick={closeStackForm}>
              {t("common.cancel")}
            </Button>
            <Button onClick={handleSaveStack} disabled={!stackForm.name.trim() || stackSaving}>
              {stackSaving
                ? t("common.saving")
                : editingStackId
                  ? t("content.skills.techStackUpdate")
                  : t("content.skills.techStackCreate")}
            </Button>
          </>
        }
      >
        <div className="space-y-2">
          <Label>{t("content.skills.techStackFieldName")}</Label>
          <Input value={stackForm.name} onChange={(e) => setStackForm((f) => ({ ...f, name: e.target.value }))} />
        </div>
        <div className="space-y-2">
          <Label>{t("content.skills.techStackFieldDescription")}</Label>
          <Input
            value={stackForm.description}
            onChange={(e) => setStackForm((f) => ({ ...f, description: e.target.value }))}
          />
        </div>
      </FormDialog>

      <ConfirmDialog
        open={stackDeleteId !== null}
        onOpenChange={(open) => !open && setStackDeleteId(null)}
        title={t("content.skills.deleteTechStackTitle")}
        description={t("content.skills.deleteTechStackDescription", { name: deletingStack?.name ?? "" })}
        confirmLabel={t("content.skills.deleteTechStackConfirm")}
        loading={stackDeleting}
        onConfirm={handleDeleteStack}
      />
    </>
  );
}
