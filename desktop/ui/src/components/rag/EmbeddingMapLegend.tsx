import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { OTHER_GROUP_ID, type EmbeddingGroup } from "@/lib/embeddingMap";
import { cn } from "@/lib/utils";

export interface EmbeddingMapLegendProps {
  /** The groups worth listing — already capped and sorted by point count. */
  legend: EmbeddingGroup[];
  /** Points folded into the "other" bucket; 0 hides the bucket row. */
  otherPointCount: number;
  otherGroupCount: number;
  otherColor: string;
  /** Already-translated label for the folded bucket. */
  otherLabel: string;
  /** Group currently highlighted on the plot (hovered or pinned). */
  activeGroupId: string | null;
  /** Group pinned by click — rendered as pressed. */
  pinnedGroupId: string | null;
  onHoverGroup: (groupId: string | null) => void;
  onToggleGroup: (groupId: string) => void;
  title: string;
  hint: string;
}

interface LegendRowProps {
  id: string;
  label: string;
  count: number;
  color: string;
  active: boolean;
  pinned: boolean;
  onHoverGroup: (groupId: string | null) => void;
  onToggleGroup: (groupId: string) => void;
}

function LegendRow({
  id,
  label,
  count,
  color,
  active,
  pinned,
  onHoverGroup,
  onToggleGroup,
}: LegendRowProps) {
  return (
    <Button
      type="button"
      variant="ghost"
      size="sm"
      aria-pressed={pinned}
      onMouseEnter={() => onHoverGroup(id)}
      onMouseLeave={() => onHoverGroup(null)}
      onFocus={() => onHoverGroup(id)}
      onBlur={() => onHoverGroup(null)}
      onClick={() => onToggleGroup(id)}
      className={cn(
        "h-auto w-full justify-start gap-2 px-2 py-1 text-left font-normal",
        active && "bg-accent text-accent-foreground",
      )}
    >
      <span
        aria-hidden
        className="h-2.5 w-2.5 shrink-0 rounded-full"
        style={{ backgroundColor: color }}
      />
      <span className="min-w-0 flex-1 truncate font-mono" title={label}>
        {label}
      </span>
      <Badge variant="outline" className="shrink-0 px-1.5 py-0 text-[10px]">
        {count}
      </Badge>
    </Button>
  );
}

export function EmbeddingMapLegend({
  legend,
  otherPointCount,
  otherGroupCount,
  otherColor,
  otherLabel,
  activeGroupId,
  pinnedGroupId,
  onHoverGroup,
  onToggleGroup,
  title,
  hint,
}: EmbeddingMapLegendProps) {
  if (legend.length === 0) return null;

  return (
    <div className="space-y-2">
      <div className="flex flex-wrap items-baseline justify-between gap-2">
        <h3 className="text-sm font-medium">{title}</h3>
        <p className="text-xs text-muted-foreground">{hint}</p>
      </div>
      <div className="max-h-56 space-y-0.5 overflow-y-auto rounded-lg border border-border p-1.5">
        {legend.map((group) => (
          <LegendRow
            key={group.id}
            id={group.id}
            label={group.label}
            count={group.count}
            color={group.color}
            active={activeGroupId === group.id}
            pinned={pinnedGroupId === group.id}
            onHoverGroup={onHoverGroup}
            onToggleGroup={onToggleGroup}
          />
        ))}
        {otherGroupCount > 0 && (
          <LegendRow
            id={OTHER_GROUP_ID}
            label={otherLabel}
            count={otherPointCount}
            color={otherColor}
            active={activeGroupId === OTHER_GROUP_ID}
            pinned={pinnedGroupId === OTHER_GROUP_ID}
            onHoverGroup={onHoverGroup}
            onToggleGroup={onToggleGroup}
          />
        )}
      </div>
    </div>
  );
}
