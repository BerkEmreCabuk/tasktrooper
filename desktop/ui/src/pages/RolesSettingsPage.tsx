import { Loader2, Plus, Save, Trash2 } from "lucide-react";
import { useCallback, useEffect, useMemo, useState } from "react";
import { toast } from "sonner";
import {
  api,
  type Agent,
  type Role,
  type RoleAssignment,
  type RolePurposeAssignment,
  type RolePurposeKey,
  ROLE_PURPOSE_KEYS,
} from "@/api";
import { Button } from "@/components/ui/button";
import { Card } from "@/components/ui/card";
import { ConfirmDialog } from "@/components/ui/confirm-dialog";
import {
  Dialog,
  DialogContent,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
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
import { MissingToolsDialog } from "@/components/workflow/MissingToolsDialog";
import { RoleAssignmentEditor } from "@/components/workflow/RoleAssignmentEditor";
import { useI18n } from "@/hooks/useI18n";
import { cn } from "@/lib/utils";

export function RolesSettingsPage() {
  const { t } = useI18n();
  const [loading, setLoading] = useState(true);
  const [roles, setRoles] = useState<Role[]>([]);
  const [agents, setAgents] = useState<Agent[]>([]);
  const [purposes, setPurposes] = useState<RolePurposeAssignment[]>([]);
  const [selectedId, setSelectedId] = useState<string | null>(null);

  const [draftName, setDraftName] = useState("");
  const [draftDescription, setDraftDescription] = useState("");
  const [draftTools, setDraftTools] = useState("");
  const [draftAssignments, setDraftAssignments] = useState<RoleAssignment[]>([]);
  const [savingRole, setSavingRole] = useState(false);
  const [savingAssignments, setSavingAssignments] = useState(false);
  const [missingTools, setMissingTools] = useState<Record<string, string[]> | null>(null);

  const [createOpen, setCreateOpen] = useState(false);
  const [newKey, setNewKey] = useState("");
  const [newName, setNewName] = useState("");
  const [creating, setCreating] = useState(false);
  const [deleteTarget, setDeleteTarget] = useState<Role | null>(null);
  const [deleting, setDeleting] = useState(false);
  const [savingPurpose, setSavingPurpose] = useState<RolePurposeKey | null>(null);

  const load = useCallback(async () => {
    setLoading(true);
    try {
      const [rolesRes, agentsRes, purposesRes] = await Promise.all([
        api.listRoles(),
        api.listAgents(),
        api.listRolePurposes(),
      ]);
      setRoles(rolesRes.roles ?? []);
      setAgents((agentsRes.agents ?? []).filter((a) => a.enabled));
      setPurposes(purposesRes.purposes ?? []);
    } catch (e) {
      toast.error(e instanceof Error ? e.message : t("settingsPages.roles.loadFailed"));
    } finally {
      setLoading(false);
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  useEffect(() => {
    load();
  }, [load]);

  const selected = useMemo(() => roles.find((r) => r.id === selectedId) ?? null, [roles, selectedId]);

  useEffect(() => {
    if (!selected) return;
    setDraftName(selected.name);
    setDraftDescription(selected.description);
    setDraftTools(selected.required_tools.join("\n"));
    setDraftAssignments(selected.assignments);
    setMissingTools(null);
  }, [selected]);

  const agentName = (id: string) => agents.find((a) => a.id === id)?.name ?? id;

  const saveRole = async () => {
    if (!selected) return;
    setSavingRole(true);
    try {
      const updated = await api.updateRole(selected.id, {
        name: draftName.trim(),
        description: draftDescription.trim(),
        required_tools: draftTools
          .split("\n")
          .map((s) => s.trim())
          .filter(Boolean),
      });
      setRoles((prev) => prev.map((r) => (r.id === updated.id ? { ...updated, assignments: r.assignments } : r)));
      toast.success(t("settingsPages.roles.savedToast"));
    } catch (e) {
      toast.error(e instanceof Error ? e.message : t("settingsPages.roles.saveFailed"));
    } finally {
      setSavingRole(false);
    }
  };

  const saveAssignments = async (confirm: boolean) => {
    if (!selected) return;
    setSavingAssignments(true);
    try {
      const result = await api.setRoleAssignments(selected.id, {
        assignments: draftAssignments.map((a) => ({ agent_id: a.agent_id, areas: a.areas, priority: a.priority })),
        confirm_grant_tools: confirm,
      });
      if (!result.saved) {
        setMissingTools(result.missing_tools ?? {});
        return;
      }
      setMissingTools(null);
      if (result.role) {
        setRoles((prev) => prev.map((r) => (r.id === result.role!.id ? result.role! : r)));
      }
      toast.success(t("settingsPages.roles.assignmentsSavedToast"));
    } catch (e) {
      toast.error(e instanceof Error ? e.message : t("settingsPages.roles.saveFailed"));
    } finally {
      setSavingAssignments(false);
    }
  };

  const cancelMissingTools = () => {
    setMissingTools(null);
  };

  const createRole = async () => {
    const key = newKey.trim();
    const name = newName.trim();
    if (!key || !name) return;
    setCreating(true);
    try {
      const role = await api.createRole({ key, name });
      setRoles((prev) => [...prev, role]);
      setSelectedId(role.id);
      setCreateOpen(false);
      setNewKey("");
      setNewName("");
      toast.success(t("settingsPages.roles.createdToast"));
    } catch (e) {
      toast.error(e instanceof Error ? e.message : t("settingsPages.roles.saveFailed"));
    } finally {
      setCreating(false);
    }
  };

  const deleteRole = async () => {
    if (!deleteTarget) return;
    setDeleting(true);
    try {
      await api.deleteRole(deleteTarget.id);
      setRoles((prev) => prev.filter((r) => r.id !== deleteTarget.id));
      if (selectedId === deleteTarget.id) setSelectedId(null);
      toast.success(t("settingsPages.roles.deletedToast"));
    } catch (e) {
      toast.error(e instanceof Error ? e.message : t("settingsPages.roles.deleteFailed"));
    } finally {
      setDeleting(false);
      setDeleteTarget(null);
    }
  };

  const setPurpose = async (purpose: RolePurposeKey, roleId: string | null) => {
    setSavingPurpose(purpose);
    try {
      await api.setRolePurpose(purpose, roleId);
      setPurposes((prev) => {
        const next = prev.filter((p) => p.purpose !== purpose);
        next.push({ purpose, role_id: roleId });
        return next;
      });
      toast.success(t("settingsPages.roles.purposeSavedToast"));
    } catch (e) {
      toast.error(e instanceof Error ? e.message : t("settingsPages.roles.saveFailed"));
    } finally {
      setSavingPurpose(null);
    }
  };

  if (loading) {
    return (
      <div className="space-y-4">
        <Skeleton className="h-10 w-full" />
        <Skeleton className="h-64 rounded-xl" />
      </div>
    );
  }

  return (
    <div className="space-y-6 pb-8">
      <div className="flex flex-wrap items-start justify-between gap-3">
        <div>
          <h2 className="font-semibold">{t("settingsPages.roles.title")}</h2>
          <p className="mt-0.5 text-sm text-muted-foreground">{t("settingsPages.roles.subtitle")}</p>
        </div>
        <Button onClick={() => setCreateOpen(true)} className="gap-2">
          <Plus className="h-4 w-4" />
          {t("settingsPages.roles.newRole")}
        </Button>
      </div>

      <div className="grid gap-6 lg:grid-cols-[18rem_1fr]">
        <Card className="divide-y divide-border">
          {roles.map((role) => (
            <button
              key={role.id}
              type="button"
              onClick={() => setSelectedId(role.id)}
              className={cn(
                "flex w-full items-center justify-between gap-2 px-4 py-3 text-left hover:bg-muted/30",
                selectedId === role.id && "bg-muted/50",
              )}
            >
              <span className="min-w-0">
                <span className="block truncate text-sm font-medium">{role.name}</span>
                <span className="block truncate font-mono text-xs text-muted-foreground">{role.key}</span>
              </span>
              <span className="shrink-0 text-xs text-muted-foreground">
                {t("settingsPages.roles.agentCount", { count: role.assignments.length })}
              </span>
            </button>
          ))}
          {roles.length === 0 && (
            <p className="px-4 py-6 text-center text-sm text-muted-foreground">{t("settingsPages.roles.empty")}</p>
          )}
        </Card>

        {selected ? (
          <div className="space-y-6">
            <Card className="space-y-4 p-6">
              <div className="flex items-start justify-between gap-3">
                <h3 className="text-sm font-semibold">{t("settingsPages.roles.detailsTitle")}</h3>
                <button
                  type="button"
                  className="text-muted-foreground hover:text-destructive"
                  onClick={() => setDeleteTarget(selected)}
                  title={t("settingsPages.roles.deleteRole")}
                >
                  <Trash2 className="h-4 w-4" />
                </button>
              </div>
              <div className="space-y-2">
                <Label htmlFor="role-name">{t("settingsPages.roles.nameLabel")}</Label>
                <Input id="role-name" value={draftName} onChange={(e) => setDraftName(e.target.value)} />
              </div>
              <div className="space-y-2">
                <Label htmlFor="role-description">{t("settingsPages.roles.descriptionLabel")}</Label>
                <Textarea
                  id="role-description"
                  value={draftDescription}
                  onChange={(e) => setDraftDescription(e.target.value)}
                  rows={2}
                />
              </div>
              <div className="space-y-2">
                <Label htmlFor="role-tools">{t("settingsPages.roles.toolsLabel")}</Label>
                <Textarea
                  id="role-tools"
                  value={draftTools}
                  onChange={(e) => setDraftTools(e.target.value)}
                  placeholder={t("settingsPages.roles.toolsPlaceholder")}
                  rows={4}
                />
                <p className="text-xs text-muted-foreground">{t("settingsPages.roles.toolsHelp")}</p>
              </div>
              <Button onClick={saveRole} disabled={savingRole} className="gap-2">
                {savingRole ? <Loader2 className="h-4 w-4 animate-spin" /> : <Save className="h-4 w-4" />}
                {t("common.save")}
              </Button>
            </Card>

            <Card className="space-y-4 p-6">
              <div>
                <h3 className="text-sm font-semibold">{t("settingsPages.roles.assignmentsTitle")}</h3>
                <p className="mt-0.5 text-xs text-muted-foreground">{t("settingsPages.roles.assignmentsSubtitle")}</p>
              </div>
              <RoleAssignmentEditor
                agents={agents}
                assignments={draftAssignments}
                onChange={setDraftAssignments}
                disabled={savingAssignments}
              />
              <Button onClick={() => saveAssignments(false)} disabled={savingAssignments} className="gap-2">
                {savingAssignments ? <Loader2 className="h-4 w-4 animate-spin" /> : <Save className="h-4 w-4" />}
                {t("settingsPages.roles.saveAssignments")}
              </Button>
            </Card>
          </div>
        ) : (
          <Card className="flex items-center justify-center p-10 text-sm text-muted-foreground">
            {t("settingsPages.roles.selectRole")}
          </Card>
        )}
      </div>

      <Card className="space-y-4 p-6">
        <div>
          <h3 className="text-sm font-semibold">{t("settingsPages.roles.dutiesTitle")}</h3>
          <p className="mt-0.5 text-xs text-muted-foreground">{t("settingsPages.roles.dutiesSubtitle")}</p>
        </div>
        <div className="space-y-3">
          {ROLE_PURPOSE_KEYS.map((purpose) => {
            const current = purposes.find((p) => p.purpose === purpose)?.role_id ?? null;
            return (
              <div key={purpose} className="flex flex-wrap items-center gap-3">
                <span className="w-48 shrink-0 text-sm font-medium">
                  {t(`settingsPages.roles.duty.${purpose}`)}
                </span>
                <Select
                  value={current ?? "none"}
                  onValueChange={(v) => setPurpose(purpose, v === "none" ? null : v)}
                  disabled={savingPurpose === purpose}
                >
                  <SelectTrigger className="w-64">
                    <SelectValue />
                  </SelectTrigger>
                  <SelectContent>
                    <SelectItem value="none">{t("settingsPages.roles.dutyNone")}</SelectItem>
                    {roles.map((role) => (
                      <SelectItem key={role.id} value={role.id}>
                        {role.name}
                      </SelectItem>
                    ))}
                  </SelectContent>
                </Select>
              </div>
            );
          })}
        </div>
      </Card>

      <Dialog open={createOpen} onOpenChange={setCreateOpen}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>{t("settingsPages.roles.newRole")}</DialogTitle>
          </DialogHeader>
          <div className="space-y-3">
            <div className="space-y-2">
              <Label htmlFor="new-role-key">{t("settingsPages.roles.keyLabel")}</Label>
              <Input
                id="new-role-key"
                value={newKey}
                onChange={(e) => setNewKey(e.target.value)}
                placeholder={t("settingsPages.roles.keyPlaceholder")}
                autoFocus
              />
              <p className="text-xs text-muted-foreground">{t("settingsPages.roles.keyHelp")}</p>
            </div>
            <div className="space-y-2">
              <Label htmlFor="new-role-name">{t("settingsPages.roles.nameLabel")}</Label>
              <Input id="new-role-name" value={newName} onChange={(e) => setNewName(e.target.value)} />
            </div>
          </div>
          <DialogFooter>
            <Button variant="outline" onClick={() => setCreateOpen(false)}>
              {t("common.cancel")}
            </Button>
            <Button onClick={createRole} disabled={creating || !newKey.trim() || !newName.trim()}>
              {t("settingsPages.roles.create")}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      <ConfirmDialog
        open={deleteTarget !== null}
        onOpenChange={(open) => !open && setDeleteTarget(null)}
        title={t("settingsPages.roles.deleteRole")}
        description={t("settingsPages.roles.deleteRoleDescription", { name: deleteTarget?.name ?? "" })}
        loading={deleting}
        onConfirm={deleteRole}
      />

      <MissingToolsDialog
        open={missingTools !== null}
        missingTools={missingTools}
        labelFor={agentName}
        saving={savingAssignments}
        onCancel={cancelMissingTools}
        onConfirm={() => saveAssignments(true)}
      />
    </div>
  );
}
