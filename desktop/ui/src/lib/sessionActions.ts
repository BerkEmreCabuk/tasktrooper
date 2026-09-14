import type { SessionAction, SessionMessage } from "@/api";

/**
 * Attaches each recorded board action to the message it produced.
 *
 * An action is written while a run is in flight, so it always predates the
 * assistant message that reports it. Anchoring on the first assistant message
 * at or after the action's timestamp puts the card directly under the reply
 * that describes it. User bubbles are skipped as anchors on purpose: the
 * optimistic one is stamped with the browser's clock, and a skewed clock would
 * float the card above the request that triggered it.
 *
 * Actions with no later assistant message (the reply is not persisted yet) are
 * returned separately so they still render at the end of the transcript rather
 * than vanishing.
 */
export function groupActionsByMessage(
  messages: SessionMessage[],
  actions: SessionAction[],
): { byMessageId: Map<string, SessionAction[]>; trailing: SessionAction[] } {
  const byMessageId = new Map<string, SessionAction[]>();
  const trailing: SessionAction[] = [];
  if (actions.length === 0) return { byMessageId, trailing };

  const anchors = messages
    .filter((m) => m.role === "assistant")
    .map((m) => ({ id: m.id, at: new Date(m.created_at).getTime() }))
    .filter((m) => Number.isFinite(m.at))
    .sort((a, b) => a.at - b.at);

  for (const action of actions) {
    const at = new Date(action.created_at).getTime();
    const anchor = Number.isFinite(at) ? anchors.find((m) => m.at >= at) : undefined;
    if (!anchor) {
      trailing.push(action);
      continue;
    }
    const existing = byMessageId.get(anchor.id);
    if (existing) existing.push(action);
    else byMessageId.set(anchor.id, [action]);
  }
  return { byMessageId, trailing };
}
