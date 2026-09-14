import { Bold, Eye, Heading2, Italic, Link2, List, ListOrdered, Pencil } from "lucide-react";
import { useRef, useState } from "react";
import { MarkdownContent } from "@/components/markdown/MarkdownContent";
import { Button } from "@/components/ui/button";
import { Textarea } from "@/components/ui/textarea";
import { useI18n } from "@/hooks/useI18n";
import { cn } from "@/lib/utils";

interface MarkdownFieldProps {
  id?: string;
  value: string;
  onChange: (value: string) => void;
  placeholder?: string;
  rows?: number;
  className?: string;
}

type WrapFn = (before: string, after: string, placeholder?: string) => void;

function ToolbarButton({
  label,
  onClick,
  children,
}: {
  label: string;
  onClick: () => void;
  children: React.ReactNode;
}) {
  return (
    <Button type="button" variant="ghost" size="icon" className="h-7 w-7" title={label} onClick={onClick}>
      {children}
    </Button>
  );
}

export function MarkdownField({
  id,
  value,
  onChange,
  placeholder,
  rows = 6,
  className,
}: MarkdownFieldProps) {
  const { t } = useI18n();
  const [mode, setMode] = useState<"edit" | "preview">("edit");
  const textareaRef = useRef<HTMLTextAreaElement>(null);

  const wrapSelection: WrapFn = (before, after, insertPlaceholder) => {
    const el = textareaRef.current;
    if (!el) return;
    const start = el.selectionStart;
    const end = el.selectionEnd;
    const selected = value.slice(start, end) || insertPlaceholder || "";
    const next = value.slice(0, start) + before + selected + after + value.slice(end);
    onChange(next);
    requestAnimationFrame(() => {
      el.focus();
      const cursor = start + before.length + selected.length;
      el.setSelectionRange(cursor, cursor);
    });
  };

  const prefixLines = (prefix: string) => {
    const el = textareaRef.current;
    if (!el) return;
    const start = el.selectionStart;
    const end = el.selectionEnd;
    const before = value.slice(0, start);
    const selected = value.slice(start, end);
    const after = value.slice(end);
    const block = selected || "";
    const lines = block.split("\n").map((line) => (line ? `${prefix}${line}` : prefix.trimEnd()));
    const next = before + lines.join("\n") + after;
    onChange(next);
  };

  return (
    <div className={cn("overflow-hidden rounded-lg border border-border", className)}>
      <div className="flex flex-wrap items-center justify-between gap-1 border-b border-border bg-muted/30 px-2 py-1">
        <div className="flex flex-wrap items-center gap-0.5">
          <ToolbarButton label={t("chatArea.markdown.field.bold")} onClick={() => wrapSelection("**", "**", t("chatArea.markdown.field.boldPlaceholder"))}>
            <Bold className="h-3.5 w-3.5" />
          </ToolbarButton>
          <ToolbarButton label={t("chatArea.markdown.field.italic")} onClick={() => wrapSelection("*", "*", t("chatArea.markdown.field.italicPlaceholder"))}>
            <Italic className="h-3.5 w-3.5" />
          </ToolbarButton>
          <ToolbarButton label={t("chatArea.markdown.field.heading")} onClick={() => prefixLines("## ")}>
            <Heading2 className="h-3.5 w-3.5" />
          </ToolbarButton>
          <ToolbarButton label={t("chatArea.markdown.field.bulletList")} onClick={() => prefixLines("- ")}>
            <List className="h-3.5 w-3.5" />
          </ToolbarButton>
          <ToolbarButton label={t("chatArea.markdown.field.numberedList")} onClick={() => prefixLines("1. ")}>
            <ListOrdered className="h-3.5 w-3.5" />
          </ToolbarButton>
          <ToolbarButton label={t("chatArea.markdown.field.link")} onClick={() => wrapSelection("[", "](url)", t("chatArea.markdown.field.linkPlaceholder"))}>
            <Link2 className="h-3.5 w-3.5" />
          </ToolbarButton>
        </div>
        <div className="flex items-center gap-0.5">
          <Button
            type="button"
            variant={mode === "edit" ? "secondary" : "ghost"}
            size="sm"
            className="h-7 gap-1 px-2 text-xs"
            onClick={() => setMode("edit")}
          >
            <Pencil className="h-3 w-3" />
            {t("chatArea.markdown.field.edit")}
          </Button>
          <Button
            type="button"
            variant={mode === "preview" ? "secondary" : "ghost"}
            size="sm"
            className="h-7 gap-1 px-2 text-xs"
            onClick={() => setMode("preview")}
          >
            <Eye className="h-3 w-3" />
            {t("chatArea.markdown.field.preview")}
          </Button>
        </div>
      </div>
      {mode === "edit" ? (
        <Textarea
          id={id}
          ref={textareaRef}
          value={value}
          onChange={(e) => onChange(e.target.value)}
          placeholder={placeholder}
          rows={rows}
          className="min-h-[8rem] resize-y rounded-none border-0 bg-background focus-visible:ring-0"
        />
      ) : (
        <div className="min-h-[8rem] bg-background p-3">
          <MarkdownContent content={value} />
        </div>
      )}
    </div>
  );
}
