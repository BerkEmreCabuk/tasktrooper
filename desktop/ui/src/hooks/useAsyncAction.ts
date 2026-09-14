import { useCallback, useState } from "react";

export function useAsyncAction<T extends unknown[]>(action: (...args: T) => Promise<void>) {
  const [loading, setLoading] = useState(false);

  const run = useCallback(
    async (...args: T) => {
      setLoading(true);
      try {
        await action(...args);
      } finally {
        setLoading(false);
      }
    },
    [action],
  );

  return { loading, run };
}
