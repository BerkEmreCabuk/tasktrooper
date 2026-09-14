import { describe, expect, it } from "vitest";
import { ValidationError, validateOverrides, validateRestartChild, validateReveal } from "./validate.js";

/**
 * The three payloads that reach something with consequences: a child id that
 * selects a process to restart, a name that opens Finder, and a path this app
 * will hand to `spawn`.
 */
describe("validateRestartChild", () => {
  it("accepts the children this supervisor actually runs", () => {
    for (const child of ["embedder", "agent-server", "appium"]) {
      expect(validateRestartChild({ child }).child).toBe(child);
    }
  });

  it("refuses anything else, including the children this app used to have", () => {
    const refused: unknown[] = [
      undefined,
      null,
      "agent-server",
      [],
      {},
      { child: "" },
      // Gone with the tunnel; a payload naming it must not resolve to a child.
      { child: "runner" },
      { child: "database" },
      { child: 3 },
    ];
    for (const payload of refused) {
      expect(() => validateRestartChild(payload), JSON.stringify(payload) ?? "undefined").toThrow(ValidationError);
    }
  });
});

describe("validateReveal", () => {
  /** A NAME, not a path: the main process supplies the directory. */
  it("accepts the three names and refuses a path", () => {
    expect(validateReveal({ what: "workspace" }).what).toBe("workspace");
    expect(validateReveal({ what: "logs" }).what).toBe("logs");
    expect(() => validateReveal({ what: "/etc" })).toThrow(ValidationError);
    expect(() => validateReveal({ what: "~/Documents" })).toThrow(ValidationError);
  });
});

describe("validateOverrides", () => {
  it("keeps an absolute path and lets an empty string clear one", () => {
    expect(validateOverrides({ claudeBin: "/opt/homebrew/bin/claude" })).toEqual({
      claudeBin: "/opt/homebrew/bin/claude",
    });
    expect(validateOverrides({ gitBin: "  " })).toEqual({ gitBin: "" });
  });

  it("refuses a relative path, a newline, and anything that is not a string", () => {
    expect(() => validateOverrides({ claudeBin: "claude" })).toThrow(ValidationError);
    expect(() => validateOverrides({ claudeBin: "/bin/sh\n/bin/evil" })).toThrow(ValidationError);
    expect(() => validateOverrides({ chromeBin: 7 })).toThrow(ValidationError);
  });
});
