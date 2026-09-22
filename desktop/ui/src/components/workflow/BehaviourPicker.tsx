import { type BehaviourRef, type BehaviourSpec, type BoardColumn } from "@/api";
import { Checkbox } from "@/components/ui/checkbox";
import { Input } from "@/components/ui/input";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { Switch } from "@/components/ui/switch";
import { useI18n } from "@/hooks/useI18n";

interface BehaviourPickerProps {
  /** Already filtered to the scope this picker is for ("stage" or "type"). */
  registry: BehaviourSpec[];
  /** Backs "column"-typed params. */
  columns: BoardColumn[];
  selected: BehaviourRef[];
  onChange: (next: BehaviourRef[]) => void;
  disabled?: boolean;
}

/**
 * Organism: toggles a set of behaviours on/off and edits each one's params,
 * both entirely driven by GET /v1/workflow/behaviours — nothing here hardcodes
 * a behaviour key, so a server-added behaviour shows up with no UI change.
 */
export function BehaviourPicker({ registry, columns, selected, onChange, disabled }: BehaviourPickerProps) {
  const { t } = useI18n();
  const byKey = new Map(selected.map((b) => [b.key, b]));

  // The registry's label/description are the server's own (English) prose,
  // read as a fallback only for a behaviour the locale files haven't caught
  // up with yet — see settingsPages.workflows.behaviour.<key> for the
  // translated pair every behaviour ships with today.
  const localizedLabel = (spec: BehaviourSpec) => {
    const key = `settingsPages.workflows.behaviour.${spec.key}.label`;
    const translated = t(key);
    return translated === key ? spec.label : translated;
  };
  const localizedDescription = (spec: BehaviourSpec) => {
    const key = `settingsPages.workflows.behaviour.${spec.key}.description`;
    const translated = t(key);
    return translated === key ? spec.description : translated;
  };

  const toggle = (spec: BehaviourSpec, checked: boolean) => {
    onChange(checked ? [...selected, { key: spec.key, params: {} }] : selected.filter((b) => b.key !== spec.key));
  };

  const setParam = (key: string, name: string, value: string) => {
    onChange(selected.map((b) => (b.key === key ? { ...b, params: { ...b.params, [name]: value } } : b)));
  };

  if (registry.length === 0) {
    return <p className="text-sm text-muted-foreground">{t("settingsPages.workflows.noBehaviours")}</p>;
  }

  return (
    <div className="space-y-2">
      {registry.map((spec) => {
        const ref = byKey.get(spec.key);
        const checked = ref !== undefined;
        return (
          <div key={spec.key} className="rounded-lg border border-border/60 p-2.5">
            <label className="flex cursor-pointer items-start gap-2">
              <Checkbox
                checked={checked}
                onCheckedChange={(v) => toggle(spec, v === true)}
                disabled={disabled}
                className="mt-0.5"
              />
              <span className="min-w-0 flex-1">
                <span className="block text-sm font-medium">{localizedLabel(spec)}</span>
                {spec.description && (
                  <span className="block text-xs text-muted-foreground">{localizedDescription(spec)}</span>
                )}
              </span>
            </label>
            {checked && spec.params.length > 0 && (
              <div className="mt-2 space-y-2 border-t border-border/60 pl-6 pt-2">
                {spec.params.map((param) => {
                  const value = ref?.params?.[param.name] ?? "";
                  return (
                    <div key={param.name} className="space-y-1">
                      <span className="text-xs text-muted-foreground">
                        {param.name}
                        {param.required && <span className="text-destructive"> *</span>}
                      </span>
                      {param.type === "bool" ? (
                        <div>
                          <Switch
                            checked={value === "true"}
                            onCheckedChange={(v) => setParam(spec.key, param.name, v ? "true" : "false")}
                            disabled={disabled}
                          />
                        </div>
                      ) : param.type === "column" ? (
                        <Select value={value} onValueChange={(v) => setParam(spec.key, param.name, v)} disabled={disabled}>
                          <SelectTrigger className="h-8">
                            <SelectValue placeholder={param.name} />
                          </SelectTrigger>
                          <SelectContent>
                            {columns.map((col) => (
                              <SelectItem key={col.slug} value={col.slug}>
                                {col.label}
                              </SelectItem>
                            ))}
                          </SelectContent>
                        </Select>
                      ) : param.type === "enum" ? (
                        <Select value={value} onValueChange={(v) => setParam(spec.key, param.name, v)} disabled={disabled}>
                          <SelectTrigger className="h-8">
                            <SelectValue placeholder={param.name} />
                          </SelectTrigger>
                          <SelectContent>
                            {(param.options ?? []).map((opt) => (
                              <SelectItem key={opt} value={opt}>
                                {opt}
                              </SelectItem>
                            ))}
                          </SelectContent>
                        </Select>
                      ) : (
                        <Input
                          value={value}
                          onChange={(e) => setParam(spec.key, param.name, e.target.value)}
                          className="h-8"
                          disabled={disabled}
                        />
                      )}
                    </div>
                  );
                })}
              </div>
            )}
          </div>
        );
      })}
    </div>
  );
}
