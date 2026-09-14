import path from "node:path";
import { defineConfig } from "vitest/config";

/**
 * Its own config rather than a `test` block in vite.config.ts, because that
 * file's `root` is `src/renderer` — the renderer bundle is built from there and
 * has to be, for `base: "./"` to resolve under `file://`. Sharing it would make
 * the test runner look for suites in one quarter of the app and find none.
 *
 * The environment is Node, not jsdom: what has tests today is the main process,
 * which has no DOM (see tsconfig.main.json — it deliberately omits the `dom`
 * lib), and Electron itself is mocked per suite. A renderer suite that wants a
 * DOM can ask for one with a `// @vitest-environment jsdom` docblock.
 */
export default defineConfig({
  resolve: {
    alias: {
      "@": path.resolve(import.meta.dirname, "./src/renderer"),
      "@shared": path.resolve(import.meta.dirname, "./src/shared"),
      "@ipc": path.resolve(import.meta.dirname, "./src/ipc"),
    },
  },
  test: {
    environment: "node",
    include: ["src/**/*.test.ts", "src/**/*.test.tsx"],
    restoreMocks: true,
  },
});
