import type { ClarificationQuestion, ClarificationRequest, SessionMessage } from "@/api";

export function findOpenClarification(messages: SessionMessage[]): ClarificationRequest | null {
  let lastIndex = -1;
  for (let i = 0; i < messages.length; i++) {
    if (messages[i].role === "assistant" && messages[i].clarification) {
      lastIndex = i;
    }
  }
  if (lastIndex < 0) {
    return null;
  }
  for (let i = lastIndex + 1; i < messages.length; i++) {
    if (messages[i].role === "user") {
      return null;
    }
  }
  return messages[lastIndex].clarification ?? null;
}

/**
 * Finds the reply that answered a clarification: the first user message after
 * it. Returns null while the question is still open.
 */
export function findClarificationAnswer(
  messages: SessionMessage[],
  messageId: string,
): string | null {
  const index = messages.findIndex((m) => m.id === messageId);
  if (index < 0) return null;
  for (let i = index + 1; i < messages.length; i++) {
    if (messages[i].role === "user") return messages[i].content;
  }
  return null;
}

/**
 * Splits an answer back into per-question text.
 *
 * The clarification card submits one `"<prompt>: <answer>"` line per question,
 * so the transcript can show each answer under the question it belongs to
 * instead of one opaque blob. Anything that does not match that shape (a typed
 * reply, an edited answer) falls back to the whole text on the first question.
 */
export function parseClarificationAnswers(
  questions: ClarificationQuestion[],
  answer: string | null,
): Record<string, string> {
  const parsed: Record<string, string> = {};
  if (!answer) return parsed;

  const lines = answer.split("\n").map((line) => line.trim()).filter(Boolean);
  let matched = 0;
  for (const q of questions) {
    const prefix = `${q.prompt}:`;
    const line = lines.find((l) => l.startsWith(prefix));
    if (line) {
      parsed[q.id] = line.slice(prefix.length).trim();
      matched += 1;
    }
  }
  if (matched === 0 && questions.length > 0) {
    parsed[questions[0].id] = answer.trim();
  }
  return parsed;
}
