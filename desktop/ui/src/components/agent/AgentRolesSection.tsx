import { Loader2, Save, Trash2 } from "lucide-react";
import { useCallback, useEffect, useState } from "react";
import { toast } from "sonner";
import { api, type AgentRoleRef, type Role } from "@/api";
import { Button } from "@/components/ui/button";
import { Card } from "@/components/ui/card";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { Skeleton } from "@/components/ui/skeleton";
import { AreaScopeControl } from "@/components/workflow/AreaScopeControl";
import { MissingToolsDialog } from "@/components/workflow/MissingToolsDialog";
import { useI18n } from "@/hooks/useI18n";

interface AgentRolesSectionProps {
  agentId: string;
  agentName: string;
}

/**
 * Organism: which roles this agent holds, and in which area(s) — the mirror
 * image of RoleAssignmentEditor (that page edits a role's agent list; this
 * edits one agent's role list). Same 422/confirm-grant-tools shape.
 */
export function AgentRolesSection({ agentId, agentName }: AgentRolesSectionProps) {
  const { t } = useI18n();
  const [loading, setLoading] = useState(true);
  const [allRoles, setAllRoles] = useState<Role[]>([]);
  const [assigned, setAssigned] = useState<AgentRoleRef[]>([]);
  const [saving, setSaving] = useState(false);
  const [missingTools, setMissingTools] = useState<Record<string, string[]> | null>(null);

  const load = useCallback(async () => {
    if (!agentId) return;
    setLoading(true);
    try {
      const [rolesRes, agentRolesRes] = await Promise.all([api.listRoles(), api.getAgentRoles(agentId)]);
      setAllRoles(rolesRes.roles ?? []);
      setAssigned(agentRolesRes.roles ?? []);
    } catch (e) {
      toast.error(e instanceof Error ? e.message : t("agentArea.roles.toast.loadFailed"));
    } finally {
      setLoading(false);
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [agentId]);

  useEffect(() => {
    load();
  }, [load]);

  const update = (roleId: string, patch: Partial<AgentRoleRef>) => {
    setAssigned((prev) => prev.map((r) => (r.role_id === roleId ? { ...r, ...patch } : r)));
  };
  const remove = (roleId: string) => setAssigned((prev) => prev.filter((r) => r.role_id !== roleId));
  const add = (roleId: string) => {
    if (!roleId || assigned.some((r) => r.role_id === roleId)) return;
    const role = allRoles.find((r) => r.id === roleId);
    if (!role) return;
    setAssigned((prev) => [...prev, { role_id: roleId, key: role.key, name: role.name, areas: null }]);
  };

  const save = async (confirm: boolean) => {
    setSaving(true);
    try {
      const result = await api.setAgentRoles(agentId, {
        roles: assigned.map((r) => ({ role_id: r.role_id, areas: r.areas })),
        confirm_grant_tools: confirm,
      });
      if (!result.saved) {
        setMissingTools(result.missing_tools ?? {});
        return;
      }
      setMissingTools(null);
      if (result.roles) setAssigned(result.roles);
      toast.success(t("agentArea.roles.toast.saved"));
    } catch (e) {
      toast.error(e instanceof Error ? e.message : t("agentArea.roles.toast.saveFailed"));
    } finally {
      setSaving(false);
    }
  };

  const availableRoles = allRoles.filter((r) => !assigned.some((a) => a.role_id === r.id));

  if (loading) {
    return <Skeleton className="h-48 rounded-xl" />;
  }

  return (
    <Card className="space-y-4 p-6">
      <div>
        <h3 className="text-sm font-semibold">{t("agentArea.roles.heading")}</h3>
        <p className="mt-0.5 text-xs text-muted-foreground">{t("agentArea.roles.help")}</p>
      </div>

      <div className="space-y-3">
        {assigned.map((r) => (
          <div key={r.role_id} className="space-y-2 rounded-lg border border-border p-3">
            <div className="flex items-center justify-between gap-2">
              <span className="text-sm font-medium">{r.name}</span>
              <button
                type="button"
                className="text-muted-foreground hover:text-destructive"
                onClick={() => remove(r.role_id)}
                disabled={saving}
              >
                <Trash2 className="h-4 w-4" />
              </button>
            </div>
            <AreaScopeControl areas={r.areas} onChange={(areas) => update(r.role_id, { areas })} disabled={saving} />
          </div>
        ))}
        {assigned.length === 0 && <p className="text-sm text-muted-foreground">{t("agentArea.roles.empty")}</p>}
      </div>

      {availableRoles.length > 0 && (
        <Select value="" onValueChange={add} disabled={saving}>
          <SelectTrigger className="w-64">
            <SelectValue placeholder={t("agentArea.roles.addRole")} />
          </SelectTrigger>
          <SelectContent>
            {availableRoles.map((role) => (
              <SelectItem key={role.id} value={role.id}>
                {role.name}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
      )}

      <Button onClick={() => save(false)} disabled={saving} className="gap-2">
        {saving ? <Loader2 className="h-4 w-4 animate-spin" /> : <Save className="h-4 w-4" />}
        {t("common.save")}
      </Button>

      <MissingToolsDialog
        open={missingTools !== null}
        missingTools={missingTools}
        labelFor={() => agentName}
        saving={saving}
        onCancel={() => setMissingTools(null)}
        onConfirm={() => save(true)}
      />
    </Card>
  );
}
