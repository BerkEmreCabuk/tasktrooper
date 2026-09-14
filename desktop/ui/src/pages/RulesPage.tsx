import { Pencil, Plus, ScrollText, Trash2 } from "lucide-react";
import { useCallback, useEffect, useState } from "react";
import { useParams } from "react-router-dom";
import { toast } from "sonner";
import { api, type OrchestratorRule, type OrchestratorRuleInput } from "@/api";
import { FormDialog } from "@/components/admin/FormDialog";
import { PageHeader } from "@/components/admin/PageHeader";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card } from "@/components/ui/card";
import { ConfirmDialog } from "@/components/ui/confirm-dialog";
import { EmptyState } from "@/components/ui/empty-state";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Skeleton } from "@/components/ui/skeleton";
import { Switch } from "@/components/ui/switch";
import { Textarea } from "@/components/ui/textarea";
import { useI18n } from "@/hooks/useI18n";
import { formatDate } from "@/lib/utils";

const emptyRule = (): OrchestratorRuleInput => ({
  name: "",
  content: "",
  priority: 0,
  enabled: true,
});

export function RulesPage() {
  const { t } = useI18n();
  const { agentId } = useParams();
  const [rules, setRules] = useState<OrchestratorRule[]>([]);
  const [loading, setLoading] = useState(true);
  const [formOpen, setFormOpen] = useState(false);
  const [form, setForm] = useState<OrchestratorRuleInput>(emptyRule());
  const [editingId, setEditingId] = useState<string | null>(null);
  const [saving, setSaving] = useState(false);
  const [deleteId, setDeleteId] = useState<string | null>(null);
  const [deleting, setDeleting] = useState(false);

  const refresh = useCallback(async () => {
    if (!agentId || agentId === "new") return;
    setLoading(true);
    try {
      const data = await api.listAgentRules(agentId);
      setRules(data.orchestrator_rules ?? []);
    } catch (e) {
      toast.error(e instanceof Error ? e.message : t("content.rules.loadFailed"));
    } finally {
      setLoading(false);
    }
  }, [agentId, t]);

  useEffect(() => {
    refresh();
  }, [refresh]);

  const resetForm = () => {
    setForm(emptyRule());
    setEditingId(null);
  };

  const openCreate = () => {
    resetForm();
    setFormOpen(true);
  };

  const openEdit = (rule: OrchestratorRule) => {
    setEditingId(rule.id);
    setForm({
      name: rule.name,
      content: rule.content,
      priority: rule.priority,
      enabled: rule.enabled,
    });
    setFormOpen(true);
  };

  const closeForm = () => {
    setFormOpen(false);
    resetForm();
  };

  const handleSave = async () => {
    if (!form.name.trim()) return;
    setSaving(true);
    try {
      if (editingId) {
        await api.updateAgentRule(agentId!, editingId, form);
        toast.success(t("content.rules.updatedToast"));
      } else {
        await api.createAgentRule(agentId!, form);
        toast.success(t("content.rules.createdToast"));
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
      await api.deleteAgentRule(agentId!, deleteId);
      toast.success(t("content.rules.deletedToast"));
      await refresh();
    } catch (e) {
      toast.error(e instanceof Error ? e.message : t("content.rules.deleteFailed"));
    } finally {
      setDeleting(false);
      setDeleteId(null);
    }
  };

  return (
    <>
      <PageHeader
        title={t("content.rules.title")}
        description={t("content.rules.description")}
        action={
          <Button onClick={openCreate} className="gap-2">
            <Plus className="h-4 w-4" />
            {t("content.rules.newRule")}
          </Button>
        }
      />

      {loading ? (
        <div className="space-y-2">
          {Array.from({ length: 3 }).map((_, i) => (
            <Skeleton key={i} className="h-14 w-full" />
          ))}
        </div>
      ) : rules.length === 0 ? (
        <EmptyState
          icon={ScrollText}
          title={t("content.rules.emptyTitle")}
          description={t("content.rules.emptyDescription")}
          action={
            <Button onClick={openCreate} className="gap-2">
              <Plus className="h-4 w-4" />
              {t("content.rules.addRule")}
            </Button>
          }
        />
      ) : (
        <Card className="overflow-hidden">
          <div className="overflow-x-auto">
            <table className="w-full text-sm">
              <thead>
                <tr className="border-b border-border bg-muted/50">
                  <th className="px-4 py-3 text-left font-medium">{t("content.rules.colName")}</th>
                  <th className="px-4 py-3 text-left font-medium">{t("content.rules.colPriority")}</th>
                  <th className="px-4 py-3 text-left font-medium">{t("content.rules.colStatus")}</th>
                  <th className="px-4 py-3 text-left font-medium">{t("content.rules.colCreated")}</th>
                  <th className="px-4 py-3 w-24" />
                </tr>
              </thead>
              <tbody>
                {rules.map((rule) => (
                  <tr key={rule.id} className="border-b border-border last:border-0 hover:bg-muted/30">
                    <td className="px-4 py-3 font-medium">{rule.name}</td>
                    <td className="px-4 py-3 text-muted-foreground">{rule.priority}</td>
                    <td className="px-4 py-3">
                      <Badge variant={rule.enabled ? "success" : "secondary"}>
                        {rule.enabled ? t("content.rules.statusEnabled") : t("content.rules.statusDisabled")}
                      </Badge>
                    </td>
                    <td className="px-4 py-3 text-muted-foreground">{formatDate(rule.created_at)}</td>
                    <td className="px-4 py-3">
                      <div className="flex gap-1">
                        <Button variant="ghost" size="icon" onClick={() => openEdit(rule)}>
                          <Pencil className="h-4 w-4" />
                        </Button>
                        <Button variant="ghost" size="icon" onClick={() => setDeleteId(rule.id)}>
                          <Trash2 className="h-4 w-4 text-destructive" />
                        </Button>
                      </div>
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        </Card>
      )}

      <FormDialog
        open={formOpen}
        onOpenChange={(open) => !open && closeForm()}
        title={editingId ? t("content.rules.editTitle") : t("content.rules.newRule")}
        description={editingId ? t("content.rules.editDescription") : t("content.rules.createDescription")}
        footer={
          <>
            <Button variant="outline" onClick={closeForm}>
              {t("common.cancel")}
            </Button>
            <Button onClick={handleSave} disabled={!form.name.trim() || saving}>
              {saving ? t("common.saving") : editingId ? t("content.rules.update") : t("content.rules.create")}
            </Button>
          </>
        }
      >
        <div className="grid gap-4 sm:grid-cols-2">
          <div className="space-y-2">
            <Label>{t("content.rules.fieldName")}</Label>
            <Input value={form.name} onChange={(e) => setForm((f) => ({ ...f, name: e.target.value }))} />
          </div>
          <div className="space-y-2">
            <Label>{t("content.rules.fieldPriority")}</Label>
            <Input
              type="number"
              value={form.priority}
              onChange={(e) => setForm((f) => ({ ...f, priority: Number(e.target.value) }))}
            />
          </div>
        </div>
        <div className="space-y-2">
          <Label>{t("content.rules.fieldContent")}</Label>
          <Textarea rows={6} value={form.content} onChange={(e) => setForm((f) => ({ ...f, content: e.target.value }))} />
        </div>
        <div className="flex items-center gap-2">
          <Switch checked={form.enabled} onCheckedChange={(v) => setForm((f) => ({ ...f, enabled: v }))} />
          <Label>{t("content.rules.fieldEnabled")}</Label>
        </div>
      </FormDialog>

      <ConfirmDialog
        open={deleteId !== null}
        onOpenChange={(open) => !open && setDeleteId(null)}
        title={t("content.rules.deleteTitle")}
        description={t("content.rules.deleteDescription")}
        confirmLabel={t("content.rules.deleteConfirm")}
        loading={deleting}
        onConfirm={handleDelete}
      />
    </>
  );
}
