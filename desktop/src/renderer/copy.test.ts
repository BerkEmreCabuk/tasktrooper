import { describe, expect, it } from "vitest";
import { startFailureCopy, unreachableCopy } from "./copy";

describe("unreachableCopy", () => {
  it("says menu bar and this machine on macOS", () => {
    expect(unreachableCopy(true)).toContain("menu bar");
    expect(unreachableCopy(true)).toContain("this machine");
    expect(unreachableCopy(true)).not.toContain("this Mac");
  });

  it("says system tray on Windows/Linux", () => {
    expect(unreachableCopy(false)).toContain("system tray");
    expect(unreachableCopy(false)).not.toContain("menu bar");
  });
});

describe("startFailureCopy", () => {
  it("explains a Postgres download that could not reach Maven Central", () => {
    const description =
      'The local server exited before it opened a port. Its own last line: "embedded postgres failed to start error=start embedded postgres: unable to connect to https://repo1.maven.org/maven2"';
    expect(startFailureCopy(description)).toContain("internet connection");
  });

  it("explains a full disk", () => {
    expect(startFailureCopy("write /data/postgres/base: no space left on device")).toContain("disk is full");
  });

  it("explains a postgres binary the OS would not launch", () => {
    expect(
      startFailureCopy("start embedded postgres: exec: initdb.exe (the OS could not launch the postgres binary; …)"),
    ).toContain("Security software");
  });

  it("leaves unrecognized failures to the generic copy", () => {
    expect(startFailureCopy("migration 140 failed: syntax error")).toBeUndefined();
    expect(startFailureCopy(undefined)).toBeUndefined();
  });
});
