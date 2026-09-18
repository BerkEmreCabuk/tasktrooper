import { AlertTriangle, Plus, Trash2 } from "lucide-react";
import { type Role, type StageParticipant, type StageParticipantMode } from "@/api";
import { Button } from "@/components/ui/button";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { Textarea } from "@/components/ui/textarea";
import { useI18n } from "@/hooks/useI18n";

const MODES: StageParticipantMode[] = ["worker", "approver"];

interface ParticipantListProps {
  roles: Role[];
  participants: StageParticipant[];
  onChange: (next: StageParticipant[]) => void;
  /** True when at least one agent holding this role subscribes to the stage's column. */
  hasSubscriber: (roleId: string) => boolean;
  disabled?: boolean;
}

/**
 * Molecule: a stage's participants — at most one `worker` and one `approver`
 * role (server-enforced; this UI mirrors the limit so a client never submits
 * a shape the 422 would reject). Flags a role with nobody subscribed to the
 * stage's column, since B does not yet gate dispatch on participants — an
 * unsubscribed role here silently never gets the work.
 */
export function ParticipantList({ roles, participants, onChange, hasSubscriber, disabled }: ParticipantListProps) {
  const { t } = useI18n();
  const roleName = (id: string) => roles.find((r) => r.id === id)?.name ?? id;

  const update = (index: number, patch: Partial<StageParticipant>) => {
    onChange(participants.map((p, i) => (i === index ? { ...p, ...patch } : p)));
  };
  const remove = (index: number) => onChange(participants.filter((_, i) => i !== index));
  const addMode = (mode: StageParticipantMode) => {
    if (participants.some((p) => p.mode === mode) || roles.length === 0) return;
    onChange([...participants, { role_id: roles[0].id, mode, instructions: "", position: participants.length }]);
  };

  return (
    <div className="space-y-2">
      {participants.map((p, index) => (
        <div key={`${p.mode}-${index}`} className="space-y-1.5 rounded-lg border border-border/60 p-2.5">
          <div className="flex items-center gap-2">
            <span className="w-20 shrink-0 text-xs font-medium uppercase text-muted-foreground">
              {t(`settingsPages.workflows.participantMode.${p.mode}`)}
            </span>
            <Select value={p.role_id} onValueChange={(v) => update(index, { role_id: v })} disabled={disabled}>
              <SelectTrigger className="h-8">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                {roles.map((role) => (
                  <SelectItem key={role.id} value={role.id}>
                    {role.name}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
            <button
              type="button"
              className="text-muted-foreground hover:text-destructive"
              onClick={() => remove(index)}
              disabled={disabled}
              title={t("settingsPages.workflows.removeParticipant")}
            >
              <Trash2 className="h-4 w-4" />
            </button>
          </div>
          {p.role_id && !hasSubscriber(p.role_id) && (
            <p className="flex items-center gap-1.5 text-xs text-warning">
              <AlertTriangle className="h-3.5 w-3.5 shrink-0" />
              {t("settingsPages.workflows.noSubscriberWarning", { role: roleName(p.role_id) })}
            </p>
          )}
          <Textarea
            value={p.instructions}
            onChange={(e) => update(index, { instructions: e.target.value })}
            placeholder={t("settingsPages.workflows.participantInstructionsPlaceholder")}
            rows={2}
            disabled={disabled}
          />
        </div>
      ))}
      <div className="flex gap-2">
        {MODES.filter((mode) => !participants.some((p) => p.mode === mode)).map((mode) => (
          <Button
            key={mode}
            type="button"
            variant="outline"
            size="sm"
            className="gap-1.5"
            onClick={() => addMode(mode)}
            disabled={disabled || roles.length === 0}
          >
            <Plus className="h-3.5 w-3.5" />
            {t(`settingsPages.workflows.addParticipant.${mode}`)}
          </Button>
        ))}
      </div>
    </div>
  );
}
