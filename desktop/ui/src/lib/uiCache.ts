// A last-known-good snapshot of what each page rendered, kept for the life of
// the tab.
//
// Why: every page in this app fetches on mount and shows a full-page skeleton
// until the first response lands, so clicking around the sidebar meant staring
// at skeletons. Rendering the previous payload immediately and refreshing
// behind it removes the skeleton from every visit but the first, without
// changing what the page eventually shows.
//
// Session-scoped on purpose. sessionStorage mirrors the Map so a reload (or the
// desktop shell reopening the same tab) still paints instantly, while a new tab
// or a new day starts clean. Nothing here is authoritative: it is only ever the
// first frame, and the fetch that follows overwrites it.

const memory = new Map<string, unknown>();

const STORAGE_PREFIX = "tt.cache.";
// Big payloads are the ones worth caching, but not at any price: a quota error
// on write would throw inside a state update. Anything over this is kept in
// memory only.
const MAX_PERSISTED_BYTES = 512 * 1024;

function storage(): Storage | null {
  try {
    return typeof window === "undefined" ? null : window.sessionStorage;
  } catch {
    // Safari private mode and friends: sessionStorage exists but throws.
    return null;
  }
}

export function readCache<T>(key: string): T | undefined {
  if (memory.has(key)) return memory.get(key) as T;
  const store = storage();
  if (!store) return undefined;
  try {
    const raw = store.getItem(STORAGE_PREFIX + key);
    if (raw === null) return undefined;
    const parsed = JSON.parse(raw) as T;
    memory.set(key, parsed);
    return parsed;
  } catch {
    return undefined;
  }
}

export function hasCache(key: string): boolean {
  return readCache(key) !== undefined;
}

export function writeCache<T>(key: string, value: T): void {
  memory.set(key, value);
  const store = storage();
  if (!store) return;
  try {
    const raw = JSON.stringify(value);
    if (raw.length > MAX_PERSISTED_BYTES) {
      store.removeItem(STORAGE_PREFIX + key);
      return;
    }
    store.setItem(STORAGE_PREFIX + key, raw);
  } catch {
    /* quota or an unserialisable value — the in-memory copy still stands */
  }
}

