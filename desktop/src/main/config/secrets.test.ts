import { describe, expect, it } from "vitest";
import { asSecrets } from "./secrets.js";

/**
 * The parser is the whole of the file's contract with its own past: a
 * `local.bin` written by an older build, or a half-written one, must read as
 * "no secrets yet" so the next `ensure()` generates a working pair — never as a
 * pair with an empty key, which starts the backend and fails at the first
 * encrypted row.
 */
describe("asSecrets", () => {
  it("accepts a complete pair", () => {
    expect(asSecrets({ api_token: "t", mcp_secrets_key: "k", created_at: "2026-09-14T00:00:00Z" })).toEqual({
      api_token: "t",
      mcp_secrets_key: "k",
      created_at: "2026-09-14T00:00:00Z",
    });
  });

  it("tolerates a missing created_at, which is metadata and not a secret", () => {
    expect(asSecrets({ api_token: "t", mcp_secrets_key: "k" })?.created_at).toBe("");
  });

  it("refuses anything that would start the backend with an empty key", () => {
    for (const raw of [
      null,
      undefined,
      "",
      42,
      {},
      { api_token: "t" },
      { mcp_secrets_key: "k" },
      { api_token: "", mcp_secrets_key: "k" },
      { api_token: "t", mcp_secrets_key: "" },
      { api_token: 1, mcp_secrets_key: "k" },
      // The pairing bundle this file used to hold. Nothing in it is usable now.
      { tenant_id: "t", runner_token: "r", tm_base_url: "https://example.invalid" },
    ]) {
      expect(asSecrets(raw), JSON.stringify(raw) ?? "undefined").toBeNull();
    }
  });
});
