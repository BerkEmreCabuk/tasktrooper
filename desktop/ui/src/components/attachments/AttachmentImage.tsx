import { ImageOff } from "lucide-react";
import { Spinner } from "@/components/ui/spinner";
import { useI18n } from "@/hooks/useI18n";
import { cn } from "@/lib/utils";
import { openAttachmentBlob, useAttachmentBlob } from "./useAttachmentBlob";

/**
 * Atom: one stored image, addressed by attachment id, rendered as a clickable
 * thumbnail that opens full size in a new tab.
 *
 * It is separate from AttachmentList (a molecule, which renders a grid of
 * AttachmentMeta with filenames, sizes and non-image fallbacks) because the
 * callers here have neither metadata nor a grid: an agent's screenshot arrives
 * in a run step as a bare id, and what the reader wants is the picture.
 */
export function AttachmentImage({
  id,
  alt,
  className,
}: {
  id: string;
  alt?: string;
  className?: string;
}) {
  const { t } = useI18n();
  const { url, loading } = useAttachmentBlob(id);

  if (loading) {
    return (
      <div className={cn("flex h-24 w-full items-center justify-center rounded border border-border bg-muted", className)}>
        <Spinner className="h-4 w-4" />
      </div>
    );
  }

  if (!url) {
    return (
      <div
        className={cn(
          "flex h-24 w-full items-center justify-center gap-1.5 rounded border border-border bg-muted text-micro text-muted-foreground",
          className,
        )}
      >
        <ImageOff className="h-3.5 w-3.5" />
        {t("frame.ui.attachments.imageUnavailable")}
      </div>
    );
  }

  return (
    <button
      type="button"
      onClick={() => void openAttachmentBlob(id)}
      className={cn(
        "block w-full overflow-hidden rounded border border-border bg-muted transition hover:border-primary",
        className,
      )}
      title={alt ?? t("frame.ui.attachments.openFullSize")}
    >
      <img src={url} alt={alt ?? ""} className="max-h-64 w-full object-contain" />
    </button>
  );
}
