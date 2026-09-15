import { describe, expect, it } from "vitest";
import { createEmbeddingsServer, HEADERS_TIMEOUT_MS, KEEP_ALIVE_TIMEOUT_MS } from "./server.js";

describe("createEmbeddingsServer", () => {
  it("keeps idle connections open longer than the Go client does", () => {
    const { server } = createEmbeddingsServer();
    expect(server.keepAliveTimeout).toBe(KEEP_ALIVE_TIMEOUT_MS);
    expect(server.headersTimeout).toBe(HEADERS_TIMEOUT_MS);
    expect(server.headersTimeout).toBeGreaterThan(server.keepAliveTimeout);
    expect(KEEP_ALIVE_TIMEOUT_MS).toBeGreaterThan(30_000);
  });
});
