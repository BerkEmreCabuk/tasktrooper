import { useCallback, useEffect, useMemo } from "react";
import { api, type AssignableMember, type TenantRole } from "@/api";
import { useCachedState } from "@/hooks/useCachedState";
import { CACHE_MEMBERS, memberLabel } from "@/lib/project-board";

// One request even though several components ask at once: the board paints the
// card badges, its create dialog and its detail drawer all want the same list
// in the same frame. Shared only WHILE in flight, so a later mount still
// refetches.
let candidatesInflight: Promise<AssignableMember[]> | null = null;

function fetchCandidates(): Promise<AssignableMember[]> {
  if (!candidatesInflight) {
    candidatesInflight = api
      .listAssignableMembers()
      .then((data) => data.members ?? [])
      .finally(() => {
        candidatesInflight = null;
      });
  }
  return candidatesInflight;
}

/** One person a board task may be given to. */
export interface PickableMember {
  user_id: string;
  role: TenantRole;
  /** `""` when the roster could not name them — the caller decides what to show. */
  label: string;
}

export interface TenantMembersState {
  /** Everyone a task may be given to. Empty until the first read lands. */
  members: PickableMember[];
  /** What to render for a uid, or null when nothing can name it. */
  labelFor: (uid: string | undefined | null) => string | null;
  /** Re-read the list — worth doing after the server refuses an assignment. */
  refresh: () => void;
}

/**
 * The people a board task may be assigned to — `GET /v1/tenant/members`, the
 * server's own roster, which is by construction exactly who `resolveAssignee`
 * accepts.
 *
 * A candidate the roster cannot name keeps its seat with an empty label;
 * losing a person because their name was blank would be the worse failure, and
 * it is the picker that says what an unnamed row looks like.
 */
export function useTenantMembers(enabled = true): TenantMembersState {
  const [candidates, setCandidates] = useCachedState<AssignableMember[]>(CACHE_MEMBERS, []);

  const refresh = useCallback(() => {
    void fetchCandidates()
      .then(setCandidates)
      .catch(() => {
        /* a failed read says nothing about who is in the workspace — keep the last one */
      });
  }, [setCandidates]);

  useEffect(() => {
    if (!enabled) return;
    refresh();
  }, [enabled, refresh]);

  const members = useMemo(
    () =>
      candidates.map<PickableMember>((member) => ({
        user_id: member.user_id,
        role: member.role,
        label: memberLabel(member, ""),
      })),
    [candidates],
  );

  const byUid = useMemo(
    () => new Map(members.filter((m) => m.label).map((m) => [m.user_id, m.label])),
    [members],
  );

  const labelFor = useCallback(
    (uid: string | undefined | null) => (uid ? (byUid.get(uid) ?? null) : null),
    [byUid],
  );

  return { members, labelFor, refresh };
}
