/**
 * Longest token sequence one inference is given.
 *
 * The model accepts far longer input, but attention cost grows with the square
 * of the length on the wasm backend: a 1,725-token input already took over
 * three seconds, and a 32,000-character one overflowed an integer inside
 * onnxruntime and left the process holding 2 GB. The head of a code chunk (its
 * path, symbol, signature and opening lines) carries what retrieval needs.
 */
export const MAX_SEQUENCE_TOKENS = 2048;

export interface Encoding {
  ids: number[];
  attention_mask: number[];
  token_type_ids: number[];
}

/**
 * Cut an encoding to at most `max` tokens. The head is kept and the final
 * token, the closing special token the tokenizer appended, stays last, so the
 * model still sees a well-formed sequence. The three arrays are cut together.
 */
export function truncateEncoding(encoding: Encoding, max: number = MAX_SEQUENCE_TOKENS): Encoding {
  const n = encoding.ids.length;
  if (n <= max) return encoding;
  const keep = (values: number[]): number[] => [...values.slice(0, max - 1), values[n - 1] as number];
  return {
    ids: keep(encoding.ids),
    attention_mask: keep(encoding.attention_mask),
    token_type_ids: keep(encoding.token_type_ids),
  };
}

/**
 * Runs one task at a time, in arrival order. Concurrent inference on the wasm
 * backend only contends for the same threads, so a queue gives every request a
 * predictable turn instead of making all of them slow at once. A failed task
 * does not stop the ones behind it.
 */
export class SerialQueue {
  #tail: Promise<void> = Promise.resolve();

  run<T>(task: () => Promise<T>): Promise<T> {
    const result = this.#tail.then(task);
    this.#tail = result.then(
      () => undefined,
      () => undefined,
    );
    return result;
  }
}
