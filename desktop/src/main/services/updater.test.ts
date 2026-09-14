import { mkdirSync, mkdtempSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import path from "node:path";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { UpdateStatus } from "../../ipc/types.js";
import {
  CHECK_INTERVAL_MS,
  FIRST_CHECK_DELAY_MS,
  UpdateService,
  feedUrlIsUsable,
  resolveFeed,
  type UpdaterBackend,
  type UpdaterEvents,
  type UpdaterLogger,
} from "./updater.js";

/**
 * The update state machine, without a packaged app.
 *
 * What can be tested here and what cannot is worth being explicit about,
 * because the interesting half of this feature lives in Squirrel.Mac and
 * Squirrel.Mac needs a real signed bundle. So:
 *
 *   tested here — which feed a launch resolves to and why, that an unusable
 *   URL is refused rather than fetched, that the phases move in the order a
 *   title bar renders, that a failed check surfaces as an error instead of
 *   silence, that nothing installs unless something is staged, and that the
 *   schedule is a delayed first check plus an interval.
 *
 *   not tested here — that Squirrel accepts a correctly signed update and
 *   refuses a wrong one. That is exercised against a real packaged app driven
 *   at a local feed; see README, "Verifying the updater against a local feed".
 */

/** A stand-in for electron-updater's `autoUpdater`, driveable event by event. */
class FakeBackend implements UpdaterBackend {
  autoDownload = false;
  autoInstallOnAppQuit = false;
  allowDowngrade = true;
  logger: UpdaterLogger | null = null;

  checks = 0;
  installs = 0;
  /** Set to make `checkForUpdates` reject, the way a dead feed does. */
  checkError: Error | null = null;

  readonly #listeners = new Map<string, ((...args: never[]) => void)[]>();

  on<K extends keyof UpdaterEvents>(event: K, listener: UpdaterEvents[K]): this {
    const list = this.#listeners.get(event) ?? [];
    list.push(listener as (...args: never[]) => void);
    this.#listeners.set(event, list);
    return this;
  }

  emit<K extends keyof UpdaterEvents>(event: K, ...args: Parameters<UpdaterEvents[K]>): void {
    for (const listener of this.#listeners.get(event) ?? []) {
      (listener as (...a: unknown[]) => void)(...args);
    }
  }

  checkForUpdates(): Promise<unknown> {
    this.checks += 1;
    if (this.checkError) return Promise.reject(this.checkError);
    return Promise.resolve(null);
  }

  quitAndInstall(): void {
    this.installs += 1;
  }
}

let dir: string;

beforeEach(() => {
  dir = mkdtempSync(path.join(tmpdir(), "tt-updater-"));
});

afterEach(() => {
  rmSync(dir, { recursive: true, force: true });
});

function paths(overrides: Partial<{ packaged: boolean; resourcesPath: string }> = {}) {
  return { packaged: true, resourcesPath: path.join(dir, "Resources"), ...overrides };
}

function bundledConfig(body: string): void {
  const resources = path.join(dir, "Resources");
  mkdirSync(resources, { recursive: true });
  writeFileSync(path.join(resources, "app-update.yml"), body, "utf8");
}

describe("feedUrlIsUsable", () => {
  it("accepts https anywhere", () => {
    expect(feedUrlIsUsable("https://storage.googleapis.com/some-bucket/mac/")).toBe(true);
  });

  it("accepts http only on the loopback interface", () => {
    expect(feedUrlIsUsable("http://127.0.0.1:8080/")).toBe(true);
    expect(feedUrlIsUsable("http://localhost:8080/")).toBe(true);
    // The whole point of the rule: a feed anyone on the path can rewrite.
    expect(feedUrlIsUsable("http://updates.example.com/")).toBe(false);
  });

  it("refuses anything that is not a URL, and anything that is not http(s)", () => {
    expect(feedUrlIsUsable("")).toBe(false);
    expect(feedUrlIsUsable("storage.googleapis.com/bucket")).toBe(false);
    expect(feedUrlIsUsable("file:///tmp/feed/")).toBe(false);
    expect(feedUrlIsUsable("gs://bucket/mac/")).toBe(false);
  });
});

describe("resolveFeed", () => {
  it("has no feed in a development run", () => {
    const feed = resolveFeed(paths({ packaged: false }));
    expect(feed.kind).toBe("none");
    expect(feed.kind === "none" && feed.reason).toMatch(/development build/);
  });

  it("has no feed when the build was packaged without one", () => {
    const feed = resolveFeed(paths());
    expect(feed.kind).toBe("none");
    expect(feed.kind === "none" && feed.reason).toMatch(/no update feed/);
  });

  it("uses the feed the build was packaged with", () => {
    bundledConfig("provider: generic\nurl: https://example.com/mac/\nupdaterCacheDirName: tasktrooper-desktop-updater\n");
    expect(resolveFeed(paths())).toEqual({ kind: "bundled", url: "https://example.com/mac/" });
  });

  /**
   * electron-builder infers a `provider: github` config from the git remote
   * when no publish configuration is set. Nobody chose it, so it must not be
   * followed — this is the check at the reading end, and `publish: null` is the
   * one at the writing end.
   */
  it("refuses a feed that was inferred rather than chosen", () => {
    bundledConfig("provider: github\nowner: someone\nrepo: desktop\n");
    const feed = resolveFeed(paths());
    expect(feed.kind).toBe("none");
    expect(feed.kind === "none" && feed.reason).toMatch(/inferred/);
  });

  /** Plain http to a remote host is a feed anybody on the path can rewrite. */
  it("refuses a bundled feed whose URL could never be trusted", () => {
    bundledConfig("provider: generic\nurl: http://updates.example.com/\n");
    const feed = resolveFeed(paths());
    expect(feed.kind).toBe("none");
    expect(feed.kind === "none" && feed.reason).toMatch(/not a usable URL/);
  });
});

describe("UpdateService", () => {
  function service(feedKind: "ok" | "none" = "ok") {
    const backend = new FakeBackend();
    const seen: UpdateStatus[] = [];
    const svc = new UpdateService({
      backend,
      feed: feedKind === "ok" ? { kind: "bundled", url: "https://example.com/mac/" } : { kind: "none", reason: "no feed here" },
      onStatus: (status) => seen.push(status),
      now: () => 1_700_000_000_000,
    });
    return { backend, svc, seen };
  }

  it("is unsupported, silent and inert without a feed", async () => {
    const { backend, svc, seen } = service("none");
    svc.start();
    expect(svc.status).toEqual({ phase: "unsupported", detail: "no feed here" });
    // No listeners, no timers, no requests: an app that cannot update should
    // not be holding any of the three.
    expect(backend.logger).toBeNull();
    await svc.check();
    expect(backend.checks).toBe(0);
    expect(seen).toEqual([]);
    expect(svc.install()).toBe(false);
  });

  it("configures the backend to download in the background and stage eagerly", () => {
    const { backend, svc } = service();
    svc.start();
    expect(backend.autoDownload).toBe(true);
    // On macOS this hands the download to Squirrel as soon as it lands — which
    // is where the signature is checked — and leaves ShipIt to swap the bundle
    // when this process exits, without relaunching. It is not a quit hook;
    // MacUpdater has none. See services/updater.ts.
    expect(backend.autoInstallOnAppQuit).toBe(true);
    expect(backend.allowDowngrade).toBe(false);
  });

  it("walks idle → checking → available → ready as the download progresses", async () => {
    const { backend, svc } = service();
    svc.start();
    expect(svc.status.phase).toBe("idle");

    backend.emit("checking-for-update");
    expect(svc.status.phase).toBe("checking");

    backend.emit("update-available", { version: "0.2.0" });
    expect(svc.status).toMatchObject({ phase: "available", version: "0.2.0", percent: 0 });

    backend.emit("download-progress", { percent: 41.6 });
    expect(svc.status.percent).toBe(42);

    backend.emit("update-downloaded", { version: "0.2.0" });
    expect(svc.status).toMatchObject({ phase: "ready", version: "0.2.0", percent: 100 });
    expect(svc.ready).toBe(true);
    expect(svc.status.detail).toContain("0.2.0");

    // A late progress line must not un-ready a button somebody is reaching for.
    backend.emit("download-progress", { percent: 99 });
    expect(svc.status.phase).toBe("ready");
    // And a staged update is not re-checked; a second download over a finished
    // one is the failure that would produce.
    await svc.check();
    expect(backend.checks).toBe(0);
  });

  it("reports being up to date without holding on to a stale version", () => {
    const { backend, svc } = service();
    svc.start();
    backend.emit("update-available", { version: "0.2.0" });
    backend.emit("update-not-available", { version: "0.1.0" });
    expect(svc.status).toMatchObject({ phase: "current", checkedAt: 1_700_000_000_000 });
    expect(svc.status.version).toBeUndefined();
  });

  it("surfaces a failed check as one sentence rather than a stack", async () => {
    const { backend, svc } = service();
    svc.start();
    backend.checkError = new Error("net::ERR_NAME_NOT_RESOLVED\n    at Object.<anonymous> (/x/y.js:1:1)");
    await svc.check();
    expect(svc.status.phase).toBe("error");
    expect(svc.status.detail).toBe("net::ERR_NAME_NOT_RESOLVED");
    expect(svc.status.checkedAt).toBe(1_700_000_000_000);
  });

  it("surfaces an error the backend emits, including one that arrives after staging", () => {
    const { backend, svc } = service();
    svc.start();
    backend.emit("update-downloaded", { version: "0.2.0" });
    expect(svc.ready).toBe(true);
    // Squirrel rejecting the signature of a staged build arrives this way, and
    // it must take the restart button away rather than leave it offering a
    // restart that cannot install anything.
    backend.emit("error", new Error("Code signature at URL … did not pass validation"));
    expect(svc.status.phase).toBe("error");
    expect(svc.ready).toBe(false);
    expect(svc.install()).toBe(false);
  });

  it("installs only what has been staged", () => {
    const { backend, svc } = service();
    svc.start();
    expect(svc.install()).toBe(false);
    expect(backend.installs).toBe(0);

    backend.emit("update-downloaded", { version: "0.2.0" });
    expect(svc.install()).toBe(true);
    expect(backend.installs).toBe(1);
  });

  it("checks once shortly after launch and then on an interval, and stops when told to", () => {
    vi.useFakeTimers();
    try {
      const { backend, svc } = service();
      svc.start();
      expect(backend.checks).toBe(0);

      vi.advanceTimersByTime(FIRST_CHECK_DELAY_MS);
      expect(backend.checks).toBe(1);

      vi.advanceTimersByTime(CHECK_INTERVAL_MS);
      expect(backend.checks).toBe(2);

      svc.stop();
      vi.advanceTimersByTime(CHECK_INTERVAL_MS * 3);
      expect(backend.checks).toBe(2);
    } finally {
      vi.useRealTimers();
    }
  });

  it("only starts once, so one event is one status push", () => {
    const { backend, svc, seen } = service();
    svc.start();
    svc.start();
    backend.emit("checking-for-update");
    // A second start would register a second set of listeners, and every
    // status change would then be pushed to the title bar twice.
    expect(seen).toHaveLength(1);
  });
});
