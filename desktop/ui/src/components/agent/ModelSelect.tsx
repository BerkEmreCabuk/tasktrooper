import { Search } from "lucide-react";
import { useMemo, useRef, useState, type ReactNode } from "react";
import { Badge } from "@/components/ui/badge";
import { Input } from "@/components/ui/input";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { useI18n } from "@/hooks/useI18n";
import { modelCapabilities, type ModelCapability } from "@/lib/modelCapabilities";

interface ModelSelectProps {
  value?: string;
  onValueChange: (value: string) => void;
  models: string[];
  disabled?: boolean;
  placeholder?: string;
  /** Rendered above the model list — used for the "use the default model" row. */
  extraItems?: ReactNode;
}

const CAPABILITY_STYLES: Record<ModelCapability, string> = {
  vision: "border-emerald-500/40 text-emerald-600 dark:text-emerald-400",
  reasoning: "border-violet-500/40 text-violet-600 dark:text-violet-400",
  embedding: "border-sky-500/40 text-sky-600 dark:text-sky-400",
  code: "border-amber-500/40 text-amber-600 dark:text-amber-400",
};

/**
 * The model picker: a filterable list whose rows say what each model can do.
 *
 * Provider model lists run to hundreds of ids, so scrolling to find one is the
 * common case, not the edge case — hence the filter. The capability badges are
 * read off the id (see modelCapabilities) and are advisory: they never remove a
 * model from the list, because a name-based guess must not be able to hide the
 * model somebody actually wants.
 */
export function ModelSelect({
  value,
  onValueChange,
  models,
  disabled,
  placeholder,
  extraItems,
}: ModelSelectProps) {
  const { t } = useI18n();
  const [query, setQuery] = useState("");
  const searchRef = useRef<HTMLInputElement>(null);

  const filtered = useMemo(() => {
    const q = query.trim().toLowerCase();
    if (!q) return models;
    return models.filter((id) => id.toLowerCase().includes(q));
  }, [models, query]);

  return (
    <Select
      value={value || undefined}
      onValueChange={onValueChange}
      disabled={disabled}
      onOpenChange={(open) => {
        if (!open) setQuery("");
      }}
    >
      <SelectTrigger>
        <SelectValue placeholder={placeholder} />
      </SelectTrigger>
      <SelectContent>
        <div className="sticky top-0 z-10 bg-popover p-1">
          <div className="relative">
            <Search className="pointer-events-none absolute left-2 top-1/2 h-3.5 w-3.5 -translate-y-1/2 text-muted-foreground" />
            <Input
              ref={searchRef}
              value={query}
              onChange={(e) => setQuery(e.target.value)}
              placeholder={t("agentArea.settings.model.searchPlaceholder")}
              className="h-8 pl-7 text-xs"
              // Radix Select moves focus with typed characters; without this the
              // keystrokes would jump the highlighted row instead of filtering.
              onKeyDown={(e) => e.stopPropagation()}
            />
          </div>
        </div>
        {extraItems}
        {filtered.length === 0 && (
          <div className="px-2 py-3 text-center text-xs text-muted-foreground">
            {t("agentArea.settings.model.noMatch")}
          </div>
        )}
        {filtered.map((id) => {
          const caps = modelCapabilities(id);
          return (
            <SelectItem key={id} value={id}>
              <span className="flex items-center gap-2">
                <span>{id}</span>
                {caps.map((cap) => (
                  <Badge key={cap} variant="outline" className={`px-1 py-0 text-[10px] ${CAPABILITY_STYLES[cap]}`}>
                    {t(`agentArea.settings.model.capability.${cap}`)}
                  </Badge>
                ))}
              </span>
            </SelectItem>
          );
        })}
      </SelectContent>
    </Select>
  );
}
