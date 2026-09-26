import { Folder, FolderPlus, GitBranch, type LucideIcon } from "lucide-react";
import type { SourceMode } from "@/components/projects/add/flow-types";
import { useI18n } from "@/hooks/useI18n";
import { cn } from "@/lib/utils";

interface SourceModePickerProps {
  value: SourceMode | null;
  onChange: (mode: SourceMode) => void;
  disabled?: boolean;
}

const MODES: { mode: SourceMode; icon: LucideIcon; key: string }[] = [
  { mode: "folder", icon: Folder, key: "Folder" },
  { mode: "github", icon: GitBranch, key: "Github" },
  { mode: "new", icon: FolderPlus, key: "New" },
];

/** The Source step's three exclusive ways in, as large radio cards. They stay
 * on screen after a pick so switching is one click, with the pick highlighted. */
export function SourceModePicker({ value, onChange, disabled }: SourceModePickerProps) {
  const { t } = useI18n();
  return (
    <div role="radiogroup" aria-label={t("addRepository.source.modeLabel")} className="grid gap-3 sm:grid-cols-3">
      {MODES.map(({ mode, icon: Icon, key }) => {
        const selected = value === mode;
        return (
          <button
            key={mode}
            type="button"
            role="radio"
            aria-checked={selected}
            disabled={disabled}
            onClick={() => onChange(mode)}
            className={cn(
              "flex flex-col items-start gap-2 rounded-xl border bg-card p-4 text-left transition-colors",
              "shadow-[var(--shadow-raised)] hover:bg-muted/40",
              "focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring",
              "disabled:cursor-not-allowed disabled:opacity-60",
              selected ? "border-primary bg-primary/5 ring-2 ring-primary/20" : "border-border",
            )}
          >
            <Icon className={cn("h-5 w-5", selected ? "text-primary" : "text-muted-foreground")} aria-hidden />
            <span className="font-medium">{t(`addRepository.source.mode${key}Title`)}</span>
            <span className="text-caption text-muted-foreground">{t(`addRepository.source.mode${key}Line`)}</span>
          </button>
        );
      })}
    </div>
  );
}
