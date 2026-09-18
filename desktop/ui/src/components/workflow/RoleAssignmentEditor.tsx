import { Trash2 } from "lucide-react";
import { type Agent, type RoleAssignment } from "@/api";
import { Input } from "@/components/ui/input";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { AreaScopeControl } from "@/components/workflow/AreaScopeControl";
import { useI18n } from "@/hooks/useI18n";

interface RoleAssignmentEditorProps {
  agents: Agent[];
  assignments: RoleAssignment[];
  onChange: (assignments: RoleAssignment[]) => void;
  disabled?: boolean;
}

/**
 * Organism: one role's agent assignments — who may be picked for it, in
 * which area(s), and in what priority order (AgentForRole: exact area match
 * first, then "any", ties broken by priority). Used by RolesSettingsPage.
 */
export function RoleAssignmentEditor({ agents, assignments, onChange, disabled }: RoleAssignmentEditorProps) {
  const { t } = useI18n();
  const assignedIds = new Set(assignments.map((a) => a.agent_id));
  const availableAgents = agents.filter((a) => !assignedIds.has(a.id));
  const agentName = (id: string) => agents.find((a) => a.id === id)?.name ?? id;

  const update = (agentId: string, patch: Partial<RoleAssignment>) => {
    onChange(assignments.map((a) => (a.agent_id === agentId ? { ...a, ...patch } : a)));
  };
  const remove = (agentId: string) => onChange(assignments.filter((a) => a.agent_id !== agentId));
  const add = (agentId: string) => {
    if (!agentId || assignedIds.has(agentId)) return;
    onChange([...assignments, { agent_id: agentId, agent_name: agentName(agentId), areas: null, priority: 0 }]);
  };

  return (
    <div className="space-y-3">
      {assignments.length === 0 && (
        <p className="text-sm text-muted-foreground">{t("settingsPages.roles.noAssignments")}</p>
      )}
      {assignments.map((a) => (
        <div key={a.agent_id} className="space-y-2 rounded-lg border border-border p-3">
          <div className="flex items-center justify-between gap-2">
            <span className="text-sm font-medium">{a.agent_name ?? agentName(a.agent_id)}</span>
            <button
              type="button"
              className="text-muted-foreground hover:text-destructive"
              onClick={() => remove(a.agent_id)}
              disabled={disabled}
              title={t("settingsPages.roles.removeAssignment")}
            >
              <Trash2 className="h-4 w-4" />
            </button>
          </div>
          <AreaScopeControl areas={a.areas} onChange={(areas) => update(a.agent_id, { areas })} disabled={disabled} />
          <div className="flex items-center gap-2">
            <label className="text-xs text-muted-foreground">{t("settingsPages.roles.priority")}</label>
            <Input
              type="number"
              value={a.priority}
              onChange={(e) => update(a.agent_id, { priority: Number(e.target.value) || 0 })}
              className="h-8 w-20"
              disabled={disabled}
            />
          </div>
        </div>
      ))}
      {availableAgents.length > 0 && (
        <Select value="" onValueChange={add} disabled={disabled}>
          <SelectTrigger className="w-64">
            <SelectValue placeholder={t("settingsPages.roles.addAgent")} />
          </SelectTrigger>
          <SelectContent>
            {availableAgents.map((agent) => (
              <SelectItem key={agent.id} value={agent.id}>
                {agent.name}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
      )}
    </div>
  );
}
