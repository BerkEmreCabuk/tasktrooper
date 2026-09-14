import { useRef, useState } from "react";
import { Paperclip } from "lucide-react";
import { toast } from "sonner";
import { api, type AttachmentMeta } from "@/api";
import { Button } from "@/components/ui/button";
import { Spinner } from "@/components/ui/spinner";
import { useI18n } from "@/hooks/useI18n";
import { cn } from "@/lib/utils";

/**
 * Uploads a batch of files as binary attachments. Shared by the dropzone and
 * by callers that receive files elsewhere (e.g. the chat composer's textarea
 * paste), so the upload path exists exactly once.
 */
export async function uploadAttachmentFiles(files: File[], repositoryId?: string): Promise<AttachmentMeta[]> {
  const metas: AttachmentMeta[] = [];
  for (const file of files) {
    metas.push(await api.uploadAttachment(file, repositoryId));
  }
  return metas;
}

interface AttachmentDropzoneProps {
  /** Emitted once per accepted batch, after all files uploaded. */
  onUploaded: (metas: AttachmentMeta[]) => void;
  /** Stamped onto the uploads so they belong to the repository. */
  repositoryId?: string;
  disabled?: boolean;
  /** "button": just the picker button (composer); "block": full drop area. */
  variant?: "block" | "button";
  className?: string;
}

/**
 * Molecule: hidden file input + picker button with drag-drop highlight and
 * paste handling on its container. Uploads immediately (spinner while busy)
 * and emits the created AttachmentMeta[].
 */
export function AttachmentDropzone({
  onUploaded,
  repositoryId,
  disabled = false,
  variant = "block",
  className,
}: AttachmentDropzoneProps) {
  const { t } = useI18n();
  const inputRef = useRef<HTMLInputElement>(null);
  const [uploading, setUploading] = useState(false);
  const [dragOver, setDragOver] = useState(false);

  const upload = async (files: File[]) => {
    if (files.length === 0 || uploading || disabled) return;
    setUploading(true);
    try {
      onUploaded(await uploadAttachmentFiles(files, repositoryId));
    } catch (e) {
      toast.error(e instanceof Error ? e.message : t("frame.ui.attachments.uploadFailed"));
    } finally {
      setUploading(false);
    }
  };

  const input = (
    <input
      ref={inputRef}
      type="file"
      multiple
      className="hidden"
      onChange={(e) => {
        void upload(Array.from(e.target.files ?? []));
        // Same file can be picked again later.
        e.target.value = "";
      }}
    />
  );

  const pickButton = (
    <Button
      type="button"
      variant="outline"
      size={variant === "button" ? "icon" : "sm"}
      className={variant === "block" ? "gap-1" : undefined}
      disabled={disabled || uploading}
      title={t("frame.ui.attachments.add")}
      onClick={() => inputRef.current?.click()}
    >
      {uploading ? <Spinner size="sm" /> : <Paperclip className="h-3.5 w-3.5" />}
      {variant === "block" && t("frame.ui.attachments.add")}
    </Button>
  );

  if (variant === "button") {
    return (
      <span className={className}>
        {input}
        {pickButton}
      </span>
    );
  }

  return (
    <div
      className={cn(
        "flex flex-col items-center gap-2 rounded-lg border border-dashed p-4 text-center transition-colors",
        dragOver ? "border-primary bg-primary/5" : "border-border",
        disabled && "opacity-60",
        className,
      )}
      onDragOver={(e) => {
        e.preventDefault();
        setDragOver(true);
      }}
      onDragLeave={() => setDragOver(false)}
      onDrop={(e) => {
        e.preventDefault();
        setDragOver(false);
        void upload(Array.from(e.dataTransfer.files ?? []));
      }}
      onPaste={(e) => {
        const files = Array.from(e.clipboardData?.files ?? []);
        if (files.length > 0) {
          e.preventDefault();
          void upload(files);
        }
      }}
    >
      {input}
      {pickButton}
      <p className="text-xs text-muted-foreground">{t("frame.ui.attachments.dropHint")}</p>
    </div>
  );
}
