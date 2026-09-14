import { createHash } from "node:crypto";
import { createReadStream, createWriteStream, existsSync } from "node:fs";
import { mkdir, readFile, rename, rm, writeFile } from "node:fs/promises";
import path from "node:path";
import { Readable } from "node:stream";
import { pipeline } from "node:stream/promises";
import type { CachedModelFiles } from "./engine.js";

/**
 * Fetch the pinned model files, SHA-256-verify each one, and cache them under
 * `<cacheDir>`. Runs forever in the background with a capped exponential
 * backoff between attempts — nothing else in the app waits on this, and a
 * Mac that is offline at first launch just keeps trying.
 *
 * The commit is pinned rather than `resolve/main/...`: a moving `main` would
 * mean two Macs paired to the same tenant could silently embed with two
 * different model revisions, poisoning a shared index with vectors nothing
 * can compare against.
 */

const HF_COMMIT = "e9b6763023c676ca8431644204f50c2b100d9aab";
const HF_REPO = "nomic-ai/nomic-embed-text-v1.5";
const RESOLVE_BASE = `https://huggingface.co/${HF_REPO}/resolve/${HF_COMMIT}`;

/**
 * Backoff shape mirrored from `src/main/supervisor/child.ts`'s
 * `MIN_BACKOFF_MS`/`MAX_BACKOFF_MS`: a 1s floor, a 30s cap, doubling between
 * attempts. Local to this module rather than imported — that file is part of
 * the main-process bundle and this one is built and shipped separately.
 */
export const MIN_BACKOFF_MS = 1_000;
export const MAX_BACKOFF_MS = 30_000;

export interface PinnedFile {
  /** The file's name on disk, also its path segment on the HF commit for every file but the model itself. */
  name: string;
  url: string;
  sha256: string;
  sizeBytes: number;
}

/**
 * The six files this Mac needs from the pinned commit, and their SHA-256
 * hashes — computed by hand from a verified download during the go/no-go
 * prototype (see `/private/tmp/.../embedder-prototype/model/`), not copied
 * from anywhere else. `config.json`, `special_tokens_map.json` and
 * `vocab.txt` are not read by anything in `engine.ts` — `tokenizer.json`
 * carries everything `@huggingface/tokenizers` needs — but they are
 * downloaded and verified anyway: they are part of the pinned commit's
 * identity, and a partial verification would leave the other three
 * unauthenticated on disk.
 */
export const PINNED_FILES: readonly PinnedFile[] = [
  {
    name: "model_quantized.onnx",
    url: `${RESOLVE_BASE}/onnx/model_quantized.onnx`,
    sha256: "b4342336debaea79de872370664b0aaeb67dea4605513d00ee236ea871a81f27",
    sizeBytes: 137_296_292,
  },
  {
    name: "tokenizer.json",
    url: `${RESOLVE_BASE}/tokenizer.json`,
    sha256: "d241a60d5e8f04cc1b2b3e9ef7a4921b27bf526d9f6050ab90f9267a1f9e5c66",
    sizeBytes: 711_396,
  },
  {
    name: "tokenizer_config.json",
    url: `${RESOLVE_BASE}/tokenizer_config.json`,
    sha256: "d7e0000bcc80134debd2222220427e6bf5fa20a669f40a0d0d1409cc18e0a9bc",
    sizeBytes: 1_191,
  },
  {
    name: "config.json",
    url: `${RESOLVE_BASE}/config.json`,
    sha256: "9ab00bd92cee80a569f708140b7b6c1661a65891ff3765b1519e181ba2f2c92b",
    sizeBytes: 2_538,
  },
  {
    name: "special_tokens_map.json",
    url: `${RESOLVE_BASE}/special_tokens_map.json`,
    sha256: "5d5b662e421ea9fac075174bb0688ee0d9431699900b90662acd44b2a350503a",
    sizeBytes: 695,
  },
  {
    name: "vocab.txt",
    url: `${RESOLVE_BASE}/vocab.txt`,
    sha256: "07eced375cec144d27c900241f3e339478dec958f92fddbc551f295c992038a3",
    sizeBytes: 231_508,
  },
];

export interface DownloadLogger {
  info(msg: string): void;
  warn(msg: string): void;
}

/**
 * Download (or reuse) every pinned file, retrying the whole set forever with
 * a capped backoff until all six are cached and verified. Never throws — the
 * only way out is success, which is what lets `index.ts` `await` this
 * without a surrounding retry loop of its own.
 */
export async function downloadAllWithRetry(cacheDir: string, log: DownloadLogger): Promise<CachedModelFiles> {
  let backoff = MIN_BACKOFF_MS;
  for (;;) {
    try {
      const paths = new Map<string, string>();
      for (const file of PINNED_FILES) {
        paths.set(file.name, await ensureCached(cacheDir, file, log));
      }
      const modelPath = paths.get("model_quantized.onnx");
      const tokenizerJsonPath = paths.get("tokenizer.json");
      const tokenizerConfigPath = paths.get("tokenizer_config.json");
      if (!modelPath || !tokenizerJsonPath || !tokenizerConfigPath) {
        // Unreachable: PINNED_FILES always contains these three names. Kept
        // as a guard rather than a non-null assertion so a future edit that
        // drops one of them fails loudly here instead of at a call site.
        throw new Error("a required pinned file is missing from PINNED_FILES");
      }
      return { modelPath, tokenizerJsonPath, tokenizerConfigPath };
    } catch (err) {
      const delay = backoff;
      backoff = Math.min(backoff * 2, MAX_BACKOFF_MS);
      log.warn(`download failed: ${describe(err)}; retrying in ${Math.round(delay / 1000)}s`);
      await sleep(delay);
    }
  }
}

/**
 * Make sure one pinned file is on disk at `<cacheDir>/<name>`, downloading
 * and verifying it if it is not there yet (or does not match), and return
 * its final path.
 *
 * A `.sha256` sidecar is written next to the file after a verified success,
 * and trusted on the next call without re-hashing ~131MB — a restart must
 * not pay that cost on every launch. The sidecar is only ever trusted when
 * it matches the CURRENT pin: if the file is on disk but the sidecar is
 * missing or its content is stale, the file itself is re-hashed once (cheap
 * next to a re-download) before falling back to fetching it again.
 */
async function ensureCached(cacheDir: string, file: PinnedFile, log: DownloadLogger): Promise<string> {
  const finalPath = path.join(cacheDir, file.name);
  const sidecarPath = `${finalPath}.sha256`;

  if (existsSync(finalPath) && (await sidecarMatches(sidecarPath, file.sha256))) {
    return finalPath;
  }

  if (existsSync(finalPath)) {
    const actual = await sha256File(finalPath);
    if (actual === file.sha256) {
      await writeSidecar(sidecarPath, actual);
      return finalPath;
    }
    log.warn(`${file.name}: on-disk content does not match the pinned hash; re-downloading.`);
  }

  await mkdir(cacheDir, { recursive: true });
  const tempPath = `${finalPath}.download-${process.pid}-${Date.now()}`;
  try {
    await downloadToFile(file.url, tempPath);
    const actual = await sha256File(tempPath);
    if (actual !== file.sha256) {
      throw new Error(`${file.name} hashed to ${actual}, expected ${file.sha256}`);
    }
    await rename(tempPath, finalPath);
    await writeSidecar(sidecarPath, actual);
    log.info(`${file.name}: downloaded and verified.`);
    return finalPath;
  } catch (err) {
    await rm(tempPath, { force: true }).catch(() => undefined);
    throw err;
  }
}

async function sidecarMatches(sidecarPath: string, expected: string): Promise<boolean> {
  if (!existsSync(sidecarPath)) return false;
  try {
    return (await readFile(sidecarPath, "utf8")).trim() === expected;
  } catch {
    return false;
  }
}

async function writeSidecar(sidecarPath: string, hash: string): Promise<void> {
  const tempPath = `${sidecarPath}.tmp-${process.pid}`;
  await writeFile(tempPath, hash, "utf8");
  await rename(tempPath, sidecarPath);
}

async function sha256File(filePath: string): Promise<string> {
  const hash = createHash("sha256");
  await pipeline(createReadStream(filePath), hash);
  return hash.digest("hex");
}

/**
 * Stream the URL straight to a temp file. `fetch`'s Web `ReadableStream` body
 * is adapted to a Node stream so the download is piped rather than buffered
 * whole in memory — the model file alone is ~131MB.
 */
async function downloadToFile(url: string, destPath: string): Promise<void> {
  const response = await fetch(url);
  if (!response.ok || !response.body) {
    throw new Error(`GET ${url} answered ${response.status}`);
  }
  const source = Readable.fromWeb(response.body);
  await pipeline(source, createWriteStream(destPath));
}

function sleep(ms: number): Promise<void> {
  return new Promise((resolve) => setTimeout(resolve, ms));
}

function describe(err: unknown): string {
  return err instanceof Error ? err.message : String(err);
}
