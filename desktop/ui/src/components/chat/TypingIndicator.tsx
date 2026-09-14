import { useI18n } from "@/hooks/useI18n";

export function TypingIndicator() {
  const { t } = useI18n();
  return (
    <div className="py-1" aria-label={t("chatArea.chat.typing.ariaLabel")}>
      <div className="flex items-center gap-1">
        <span className="h-2 w-2 animate-bounce rounded-full bg-muted-foreground/60 [animation-delay:0ms]" />
        <span className="h-2 w-2 animate-bounce rounded-full bg-muted-foreground/60 [animation-delay:150ms]" />
        <span className="h-2 w-2 animate-bounce rounded-full bg-muted-foreground/60 [animation-delay:300ms]" />
      </div>
    </div>
  );
}
