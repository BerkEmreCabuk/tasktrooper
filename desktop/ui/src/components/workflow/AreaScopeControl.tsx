import { useState } from "react";
import { ROLE_AREAS, type RoleArea } from "@/api";
import { Button } from "@/components/ui/button";
import { Checkbox } from "@/components/ui/checkbox";
import { Input } from "@/components/ui/input";
import { useI18n } from "@/hooks/useI18n";

interface AreaScopeControlProps {
  /** null = any area. */
  areas: RoleArea[] | null;
  onChange: (areas: RoleArea[] | null) => void;
  disabled?: boolean;
}

/**
 * Molecule: the "any area, or one or more areas" scope picker shared by role
 * assignments (RoleAssignmentEditor) and an agent's own role list
 * (AgentRolesSection) — both send/receive the same `areas: string[]|null`
 * shape (null = any area, the server's own meaning, not an empty array).
 * Areas are free-form strings, not an enum: the three repo kinds are shown as
 * presets, and any other area already picked (or typed in) is shown too.
 */
export function AreaScopeControl({ areas, onChange, disabled }: AreaScopeControlProps) {
  const { t } = useI18n();
  const [draftArea, setDraftArea] = useState("");
  const selected = areas ?? [];
  const customAreas = selected.filter((a) => !ROLE_AREAS.includes(a));

  const areaLabel = (area: string) => {
    const key = `settingsPages.roles.area.${area}`;
    const label = t(key);
    return label === key ? area : label;
  };

  const toggleArea = (area: RoleArea, checked: boolean) => {
    const next = checked ? [...selected, area] : selected.filter((a) => a !== area);
    onChange(next.length === 0 ? null : next);
  };

  const addArea = () => {
    const next = draftArea.trim();
    if (!next || selected.includes(next)) return;
    onChange([...selected, next]);
    setDraftArea("");
  };

  return (
    <div className="flex flex-wrap items-center gap-3">
      <label className="flex items-center gap-1.5 text-xs">
        <Checkbox checked={areas === null} onCheckedChange={(v) => v && onChange(null)} disabled={disabled} />
        {t("settingsPages.roles.areaAny")}
      </label>
      {ROLE_AREAS.map((area) => (
        <label key={area} className="flex items-center gap-1.5 text-xs">
          <Checkbox
            checked={areas !== null && selected.includes(area)}
            onCheckedChange={(v) => toggleArea(area, v === true)}
            disabled={disabled}
          />
          {areaLabel(area)}
        </label>
      ))}
      {customAreas.map((area) => (
        <span
          key={area}
          className="flex items-center gap-1 rounded-full border px-2 py-0.5 font-mono text-xs text-muted-foreground"
        >
          {area}
          <button
            type="button"
            aria-label={t("settingsPages.roles.removeArea")}
            onClick={() => toggleArea(area, false)}
            disabled={disabled}
            className="text-muted-foreground hover:text-destructive"
          >
            ×
          </button>
        </span>
      ))}
      {!disabled && areas !== null && (
        <label className="flex items-center gap-1.5">
          <Input
            value={draftArea}
            onChange={(e) => setDraftArea(e.target.value)}
            onKeyDown={(e) => {
              if (e.key === "Enter") {
                e.preventDefault();
                addArea();
              }
            }}
            placeholder={t("settingsPages.roles.areaCustomPlaceholder")}
            className="h-6 w-36 font-mono text-xs"
          />
          <Button type="button" variant="outline" size="sm" className="h-6 px-2 text-xs" onClick={addArea}>
            {t("settingsPages.roles.addArea")}
          </Button>
        </label>
      )}
    </div>
  );
}