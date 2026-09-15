import { describe, expect, it } from "vitest";
import { MAX_SEQUENCE_TOKENS, SerialQueue, truncateEncoding } from "./limits.js";

describe("truncateEncoding", () => {
  it("leaves a sequence within the cap untouched", () => {
    const encoding = { ids: [101, 7, 8, 102], attention_mask: [1, 1, 1, 1], token_type_ids: [0, 0, 0, 0] };
    expect(truncateEncoding(encoding, 8)).toBe(encoding);
  });

  it("keeps the head and the closing special token of an over-long sequence", () => {
    const n = 10_000;
    const ids = Array.from({ length: n }, (_, i) => (i === 0 ? 101 : i === n - 1 ? 102 : 1000 + i));
    const out = truncateEncoding({ ids, attention_mask: ids.map(() => 1), token_type_ids: ids.map(() => 0) });
    expect(out.ids).toHaveLength(MAX_SEQUENCE_TOKENS);
    expect(out.attention_mask).toHaveLength(MAX_SEQUENCE_TOKENS);
    expect(out.token_type_ids).toHaveLength(MAX_SEQUENCE_TOKENS);
    expect(out.ids[0]).toBe(101);
    expect(out.ids[MAX_SEQUENCE_TOKENS - 2]).toBe(1000 + MAX_SEQUENCE_TOKENS - 2);
    expect(out.ids[MAX_SEQUENCE_TOKENS - 1]).toBe(102);
  });
});

describe("SerialQueue", () => {
  it("never runs two tasks at once and keeps arrival order", async () => {
    const queue = new SerialQueue();
    let running = 0;
    let peak = 0;
    const order: number[] = [];
    const task = (id: number, ms: number) => async () => {
      running += 1;
      peak = Math.max(peak, running);
      await new Promise((resolve) => setTimeout(resolve, ms));
      order.push(id);
      running -= 1;
      return id;
    };
    const results = await Promise.all([queue.run(task(1, 30)), queue.run(task(2, 5)), queue.run(task(3, 1))]);
    expect(results).toEqual([1, 2, 3]);
    expect(order).toEqual([1, 2, 3]);
    expect(peak).toBe(1);
  });

  it("keeps serving after a task fails", async () => {
    const queue = new SerialQueue();
    await expect(
      queue.run(async () => {
        throw new Error("boom");
      }),
    ).rejects.toThrow("boom");
    await expect(queue.run(async () => "ok")).resolves.toBe("ok");
  });
});
