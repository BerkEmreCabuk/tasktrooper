import type { LucideIcon } from "lucide-react";
import { Bot, FolderKanban, GitBranch } from "lucide-react";
import { useI18n } from "@/hooks/useI18n";
import { cn } from "@/lib/utils";

export type MentionKind = "agent" | "project" | "repository";

export interface MentionOption {
  id: string;
  name: string;
  kind: MentionKind;
  description?: string;
}

const kindIcon: Record<MentionKind, LucideIcon> = {
  agent: Bot,
  project: FolderKanban,
  repository: GitBranch,
};

interface MentionMenuProps {
  options: MentionOption[];
  activeIndex: number;
  onSelect: (option: MentionOption) => void;
  onHover: (index: number) => void;
}

export function MentionMenu({ options, activeIndex, onSelect, onHover }: MentionMenuProps) {
  const { t } = useI18n();
  if (options.length === 0) return null;
  return (
    <div className="absolute bottom-full left-0 right-0 z-50 mb-2 max-h-64 overflow-y-auto rounded-md border border-border bg-popover p-1 text-popover-foreground shadow-md">
      {options.map((option, index) => {
        const Icon = kindIcon[option.kind];
        const showHeader = index === 0 || options[index - 1].kind !== option.kind;
        return (
          <div key={`${option.kind}:${option.id}`}>
            {showHeader && (
              <div className="px-2 py-1 text-micro font-medium uppercase tracking-wide text-muted-foreground">
                {t(`chatArea.chat.composer.mention.${option.kind}`)}
              </div>
            )}
            <button
              type="button"
              className={cn(
                "flex w-full items-center gap-2 rounded-sm px-2 py-1.5 text-left text-sm",
                index === activeIndex ? "bg-accent text-accent-foreground" : "hover:bg-muted",
              )}
              // preventDefault keeps focus in the textarea so selection can
              // rewrite the token at the caret.
              onMouseDown={(e) => {
                e.preventDefault();
                onSelect(option);
              }}
              onMouseEnter={() => onHover(index)}
            >
              <Icon className="h-3.5 w-3.5 shrink-0 text-muted-foreground" />
              <span className="truncate">{option.name}</span>
              {option.description && (
                <span className="ml-auto min-w-0 truncate text-xs text-muted-foreground">{option.description}</span>
              )}
            </button>
          </div>
        );
      })}
    </div>
  );
}
