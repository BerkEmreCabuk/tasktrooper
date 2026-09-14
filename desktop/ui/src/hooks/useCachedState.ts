import { useCallback, useRef, useState, type Dispatch, type SetStateAction } from "react";
import { hasCache, readCache, writeCache } from "@/lib/uiCache";

/**
 * useState whose value survives navigation: it starts from the tab's last
 * snapshot for `key` (see lib/uiCache) and writes every update back.
 *
 * Pages use it for the payloads they fetch on mount, so revisiting a page
 * paints the previous data immediately instead of a skeleton while the
 * refresh runs behind it.
 */
export function useCachedState<T>(key: string, fallback: T): [T, Dispatch<SetStateAction<T>>] {
  const [value, setValue] = useState<T>(() => readCache<T>(key) ?? fallback);
  const keyRef = useRef(key);
  keyRef.current = key;

  const set = useCallback<Dispatch<SetStateAction<T>>>((next) => {
    setValue((prev) => {
      const resolved = typeof next === "function" ? (next as (p: T) => T)(prev) : next;
      writeCache(keyRef.current, resolved);
      return resolved;
    });
  }, []);

  return [value, set];
}

/**
 * The initial value for a page's `loading` flag: true only when nothing has
 * been cached for these keys yet. A revisit renders its snapshot straight away
 * and never shows the full-page skeleton again.
 */
export function useFirstLoad(...keys: string[]): [boolean, Dispatch<SetStateAction<boolean>>] {
  return useState(() => !keys.every((key) => hasCache(key)));
}
