/**
 * Client half of the OpenAI-compatible chat stream the bridge speaks. The
 * contract's source is apps/agent-server/internal/adapter/http/handler.go
 * (`sessionMessageStream` / `chatCompletionsStream`): "\n\n"-separated events,
 * a `data:` line per event, a `[DONE]` sentinel, and a
 * {choices:[{delta:{content}}]} body. Same contract the iOS parser implements
 * (apps/mobile/TaskTrooper/Networking/SSEStreamParser.swift).
 *
 * Deliberately transport-free: the decoder only ever sees already-decoded text,
 * so framing is separable from auth, wake-retry and abort handling (api.ts).
 */

/** Wire shape of one `chat.completion.chunk` frame. */
interface ChatStreamFrame {
  choices?: { delta?: { content?: string; phase?: string }; finish_reason?: string }[];
  /**
   * Set only on a frame that reports a failed run. The backend also repeats the
   * message in `delta.content` for clients that predate this field, so without
   * reading `error` an error is indistinguishable from the agent's own output —
   * which is exactly how error text used to end up rendered as a reply.
   */
  error?: { message?: string; type?: string };
}

export type ChatStreamEvent =
  /** A piece of the assistant's reply. */
  | { kind: "token"; token: string }
  /** `finish_reason: "stop"` — the reply is complete. */
  | { kind: "finished" }
  /** The run failed server-side; nothing more is coming. */
  | { kind: "error"; message: string; type: string }
  /**
   * `delta.phase: "reasoning_end"` — every token since the last such frame was
   * the model's reasoning ahead of a tool call, not the reply. It carries no
   * text: the tokens already arrived as `token` events, and this only says where
   * that stretch ends so a client can present it apart from the answer.
   */
  | { kind: "reasoning_end" }
  /**
   * The `[DONE]` terminator. Not proof the reply completed: the error path ends
   * with `[DONE]` too, so only `finished` says the answer is whole. It exists so
   * the reader can stop and hang up instead of waiting on a socket a proxy may
   * hold open.
   */
  | { kind: "done" };

// Frames are separated by a blank line. The bridge writes "\n\n", but the SSE
// spec allows CRLF and some proxies re-frame with it, so both are accepted —
// matching on the pair also means a "\r" left at a chunk boundary simply stays
// buffered until its "\n" arrives.
const FRAME_SEPARATOR = /\r?\n\r?\n/;

export interface ChatStreamDecoder {
  /** Feeds one decoded network chunk and returns the events it completed. */
  push(chunk: string): ChatStreamEvent[];
  /** End of body: decodes a trailing frame that arrived without its "\n\n". */
  flush(): ChatStreamEvent[];
}

/**
 * Stateful for the lifetime of one response — a frame is routinely split across
 * network chunks, so the remainder has to survive between calls. Create one per
 * request, not per chunk.
 */
export function createChatStreamDecoder(): ChatStreamDecoder {
  let buffer = "";

  return {
    push(chunk) {
      buffer += chunk;
      const frames = buffer.split(FRAME_SEPARATOR);
      // The tail is either empty (the chunk ended on a frame boundary) or a
      // half-received frame; either way it waits for the next chunk.
      buffer = frames.pop() ?? "";
      const events: ChatStreamEvent[] = [];
      for (const frame of frames) decodeFrame(frame, events);
      return events;
    },
    flush() {
      const rest = buffer;
      buffer = "";
      if (!rest.trim()) return [];
      const events: ChatStreamEvent[] = [];
      decodeFrame(rest, events);
      return events;
    },
  };
}

function decodeFrame(frame: string, out: ChatStreamEvent[]): void {
  // An SSE event may carry several `data:` lines, which concatenate with "\n".
  // Every other field (`event:`, `id:`, `retry:`, and `:` heartbeat comments a
  // proxy may inject) is ignored rather than mistaken for a body.
  const dataLines: string[] = [];
  for (const rawLine of frame.split("\n")) {
    const line = rawLine.endsWith("\r") ? rawLine.slice(0, -1) : rawLine;
    if (!line.startsWith("data:")) continue;
    dataLines.push(line.slice(5).replace(/^ /, ""));
  }
  if (dataLines.length === 0) return;

  const data = dataLines.join("\n");
  if (data === "[DONE]") {
    out.push({ kind: "done" });
    return;
  }

  let parsed: ChatStreamFrame;
  try {
    parsed = JSON.parse(data) as ChatStreamFrame;
  } catch {
    // A frame we cannot read is not worth discarding an already half-delivered
    // reply over.
    return;
  }

  if (parsed.error) {
    // The same frame's delta repeats the message for older clients; emitting it
    // as a token too would paint the error into the assistant's bubble.
    out.push({
      kind: "error",
      message: parsed.error.message ?? "",
      type: parsed.error.type ?? "",
    });
    return;
  }

  const choice = parsed.choices?.[0];
  if (!choice) return;
  const token = choice.delta?.content;
  if (token) out.push({ kind: "token", token });
  if (choice.delta?.phase === "reasoning_end") out.push({ kind: "reasoning_end" });
  if (choice.finish_reason === "stop") out.push({ kind: "finished" });
}
