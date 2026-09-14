import { useEffect, useState } from "react";
import { api } from "@/api";

/**
 * Fetches an attachment's bytes as a blob and exposes an object URL for
 * rendering. Needed because GET /v1/attachments/{id} requires the
 * Authorization header, which an <img src> cannot send. The object URL is
 * revoked on unmount / id change so previews never leak memory.
 */
export function useAttachmentBlob(id: string | null): { url: string | null; loading: boolean } {
  const [url, setUrl] = useState<string | null>(null);
  const [loading, setLoading] = useState(false);

  useEffect(() => {
    if (!id) {
      setUrl(null);
      return;
    }
    let objectUrl: string | null = null;
    let cancelled = false;
    setLoading(true);
    api
      .fetchAttachmentBlob(id)
      .then((blob) => {
        if (cancelled) return;
        objectUrl = URL.createObjectURL(blob);
        setUrl(objectUrl);
      })
      .catch(() => {
        if (!cancelled) setUrl(null);
      })
      .finally(() => {
        if (!cancelled) setLoading(false);
      });
    return () => {
      cancelled = true;
      if (objectUrl) URL.revokeObjectURL(objectUrl);
    };
  }, [id]);

  return { url, loading };
}

/**
 * Opens an attachment in a new tab through an authenticated blob fetch —
 * the click handler equivalent of useAttachmentBlob. The object URL is
 * revoked shortly after the tab takes ownership of it.
 */
export async function openAttachmentBlob(id: string): Promise<void> {
  const blob = await api.fetchAttachmentBlob(id);
  const objectUrl = URL.createObjectURL(blob);
  window.open(objectUrl, "_blank", "noopener");
  window.setTimeout(() => URL.revokeObjectURL(objectUrl), 60_000);
}
