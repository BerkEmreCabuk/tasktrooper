import { Bot, User } from "lucide-react";
import { Label } from "@/components/ui/label";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { useI18n } from "@/hooks/useI18n";
import type { PickableMember } from "@/hooks/useTenantMembers";

// Radix will not take "" as an item value, so "nobody" needs a sentinel — the
// same one the rest of the board already uses for an empty select.
const NONE = "none";

/**
 * Enough of a uid to tell two unnamed people apart, and no more.
 *
 * A candidate the label source could not name still has to be pickable — the
 * roster is the contract, the name is a nicety — so the row is rendered as
 * "unnamed member" plus this. Without it every unnamed row reads identically
 * and the picker offers a list of indistinguishable strangers; with the whole
 * uid it offers an identity number.
 */
export function shortUid(userID: string): string {
  return userID.slice(0, 8);
}

/**
 * Is a person picker worth showing at all?
 *
 * On a solo workspace it is pure noise: every card already runs on the one
 * member's Mac, the server leaves the field empty, and the dispatcher narrows
 * nothing when there are no owners and no assignees — so a control whose only
 * two options are "unassigned" and "me" asks a question with no consequence.
 * A workspace of one is therefore not asked.
 *
 * The second half is what keeps that honest: a card that ALREADY names
 * somebody shows the picker regardless. A teammate can be removed, and a
 * roster can shrink back to one — hiding the control then would hide a field
 * the backend still routes on, with no way to see or clear it.
 */
export function showMemberAssignee(
  members: PickableMember[],
  assigned: string | null | undefined,
): boolean {
  return members.length > 1 || Boolean(assigned);
}

interface AgentOption {
  id: string;
  name: string;
}

interface TaskAssigneeFieldsProps {
  /** The agents this board may hand the card to. Empty hides the agent half. */
  agents: AgentOption[];
  agentValue?: string | null;
  /** "" unassigns. */
  onAgentChange: (agentId: string) => void;
  /**
   * Name for an `agentValue` that is not in `agents` — an agent that has since
   * left the board still has to be readable in the trigger rather than showing
   * a blank one.
   */
  agentFallbackName?: string | null;
  members: PickableMember[];
  personValue?: string | null;
  /** "" unassigns. */
  onPersonChange: (userId: string) => void;
  disabled?: boolean;
}

/**
 * The two assignments a board task carries, in one block.
 *
 * They are NOT two of the same thing and are deliberately not rendered as two
 * lookalike dropdowns: the agent is what works the card, the person is whose
 * Mac it runs on and whose agents may pick it up. Someone who confuses them
 * finds out when a run fails on a Mac that was never asked to do anything, so
 * each half carries its own icon and its own sentence.
 */
export function TaskAssigneeFields({
  agents,
  agentValue,
  onAgentChange,
  agentFallbackName,
  members,
  personValue,
  onPersonChange,
  disabled,
}: TaskAssigneeFieldsProps) {
  const { t } = useI18n();
  const showAgent = agents.length > 0;
  const showPerson = showMemberAssignee(members, personValue);
  // Both halves on screen at once is what needs explaining; one on its own is
  // just a field.
  const both = showAgent && showPerson;
  if (!showAgent && !showPerson) return null;

  const agentMissing = Boolean(agentValue) && !agents.some((a) => a.id === agentValue);
  // A uid the roster cannot name — the member was removed, or the roster call
  // failed. Naming it "Unknown member" beats printing an identity number, and
  // keeping it as an option is what makes the value clearable at all.
  const personMissing = Boolean(personValue) && !members.some((m) => m.user_id === personValue);

  const agentField = showAgent ? (
    <div className="space-y-1.5">
      <Label className="flex items-center gap-2 text-xs text-muted-foreground">
        <Bot className="h-3.5 w-3.5" />
        {t("boardArea.components.memberAssignee.agentLabel")}
      </Label>
      <Select
        value={agentValue || NONE}
        onValueChange={(value) => onAgentChange(value === NONE ? "" : value)}
        disabled={disabled}
      >
        <SelectTrigger>
          <SelectValue />
        </SelectTrigger>
        <SelectContent>
          <SelectItem value={NONE}>{t("boardArea.components.memberAssignee.unassigned")}</SelectItem>
          {agentMissing && agentValue && (
            <SelectItem value={agentValue}>
              {agentFallbackName || t("boardArea.components.memberAssignee.unknown")}
            </SelectItem>
          )}
          {agents.map((agent) => (
            <SelectItem key={agent.id} value={agent.id}>
              {agent.name}
            </SelectItem>
          ))}
        </SelectContent>
      </Select>
      {both && (
        <p className="text-[11px] text-muted-foreground">
          {t("boardArea.components.memberAssignee.agentHint")}
        </p>
      )}
    </div>
  ) : null;

  const personField = showPerson ? (
    <div className="space-y-1.5">
      <Label className="flex items-center gap-2 text-xs text-muted-foreground">
        <User className="h-3.5 w-3.5" />
        {t("boardArea.components.memberAssignee.label")}
      </Label>
      <Select
        value={personValue || NONE}
        onValueChange={(value) => onPersonChange(value === NONE ? "" : value)}
        disabled={disabled}
      >
        <SelectTrigger>
          <SelectValue />
        </SelectTrigger>
        <SelectContent>
          <SelectItem value={NONE}>{t("boardArea.components.memberAssignee.unassigned")}</SelectItem>
          {personMissing && personValue && (
            <SelectItem value={personValue}>
              {t("boardArea.components.memberAssignee.unknown")}
            </SelectItem>
          )}
          {members.map((member) => (
            <SelectItem key={member.user_id} value={member.user_id}>
              {member.label || t("boardArea.components.memberAssignee.unnamed", { id: shortUid(member.user_id) })}
            </SelectItem>
          ))}
        </SelectContent>
      </Select>
      <p className="text-[11px] text-muted-foreground">
        {t("boardArea.components.memberAssignee.hint")}
      </p>
    </div>
  ) : null;

  // Only one half on offer — a solo workspace, or a board with no agents — and
  // there is nothing to tell apart: no heading, no box, no hint. The frame
  // exists to say that the two fields below it are DIFFERENT, so it appears
  // exactly when both of them do.
  if (!both) {
    return <div className="space-y-2">{agentField}{personField}</div>;
  }

  return (
    <div className="space-y-3 rounded-lg border border-border p-3">
      <Label className="text-xs font-medium">{t("boardArea.components.memberAssignee.groupLabel")}</Label>
      {agentField}
      {personField}
    </div>
  );
}
