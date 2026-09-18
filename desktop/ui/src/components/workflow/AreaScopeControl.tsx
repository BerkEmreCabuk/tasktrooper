import { ROLE_AREAS, type RoleArea } from "@/api";
import { Checkbox } from "@/components/ui/checkbox";
import { useI18n } from "@/hooks/useI18n";

interface AreaScopeControlProps {
  /** null = any area. */
  areas: RoleArea[] | null;
  onChange: (areas: RoleArea[] | null) => void;
  disabled?: boolean;
}

/**
 * Molecule: the backend/frontend/mobile-or-"any" scope picker shared by
 * role assignments (RoleAssignmentEditor) and an agent's own role list
 * (AgentRolesSection) — both send/receive the same `areas: string[]|null`
 * shape (null = any area, the server's own meaning, not an empty array).
 */
export function AreaScopeControl({ areas, onChange, disabled }: AreaScopeControlProps) {
  const { t } = useI18n();
  const isAny = areas === null;

  const toggleArea = (area: RoleArea, checked: boolean) => {
    const current = areas ?? [];
    const next = checked ? [...current, area] : current.filter((a) => a !== area);
    onChange(next.length === 0 ? null : next);
  };

  return (
    <div className="flex flex-wrap items-center gap-3">
      <label className="flex items-center gap-1.5 text-xs">
        <Checkbox checked={isAny} onCheckedChange={(v) => v && onChange(null)} disabled={disabled} />
        {t("settingsPages.roles.areaAny")}
      </label>
      {ROLE_AREAS.map((area) => (
        <label key={area} className="flex items-center gap-1.5 text-xs">
          <Checkbox
            checked={!isAny && (areas ?? []).includes(area)}
            onCheckedChange={(v) => toggleArea(area, v === true)}
            disabled={disabled}
          />
          {t(`settingsPages.roles.area.${area}`)}
        </label>
      ))}
    </div>
  );
}
