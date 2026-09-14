import { Plus, Trash2 } from "lucide-react";
import { useEffect, useRef, useState } from "react";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { useI18n } from "@/hooks/useI18n";

interface KeyValueEditorProps {
  label: string;
  description?: string;
  entries: Record<string, string>;
  secretKeys?: Set<string>;
  secretDrafts?: Record<string, string>;
  storedSecrets?: Set<string>;
  secretKeyPrefix?: string;
  onEntriesChange: (entries: Record<string, string>) => void;
  onSecretDraftChange?: (key: string, value: string) => void;
}

// A row needs an identity that survives editing its key. Keying rows by the key
// text itself remounted the input on every keystroke (losing focus and the
// caret) and reordered the row to the end, because a Record has no stable order.
interface Row {
  id: number;
  key: string;
  value: string;
}

let nextRowID = 0;

function toRows(entries: Record<string, string>): Row[] {
  return Object.entries(entries).map(([key, value]) => ({ id: nextRowID++, key, value }));
}

// Rows with a blank key are held in local state so a key can be cleared and
// retyped, but they are not part of the emitted record.
function toEntries(rows: Row[]): Record<string, string> {
  const out: Record<string, string> = {};
  for (const row of rows) {
    const key = row.key.trim();
    if (key) out[key] = row.value;
  }
  return out;
}

function sameEntries(a: Record<string, string>, b: Record<string, string>): boolean {
  const aKeys = Object.keys(a);
  if (aKeys.length !== Object.keys(b).length) return false;
  return aKeys.every((k) => Object.prototype.hasOwnProperty.call(b, k) && a[k] === b[k]);
}

export function KeyValueEditor({
  label,
  description,
  entries,
  secretKeys = new Set(),
  secretDrafts = {},
  storedSecrets = new Set(),
  secretKeyPrefix = "",
  onEntriesChange,
  onSecretDraftChange,
}: KeyValueEditorProps) {
  const { t } = useI18n();
  const [rows, setRows] = useState<Row[]>(() => toRows(entries));
  // What we last handed upward. An incoming `entries` equal to this is our own
  // change echoed back, and must not clobber in-progress edits (a half-typed
  // key, a row whose key is momentarily blank).
  const emitted = useRef<Record<string, string>>(entries);

  useEffect(() => {
    if (sameEntries(entries, emitted.current)) return;
    emitted.current = entries;
    setRows(toRows(entries));
  }, [entries]);

  const commit = (next: Row[]) => {
    setRows(next);
    const record = toEntries(next);
    emitted.current = record;
    onEntriesChange(record);
  };

  const updateKey = (id: number, newKey: string) => {
    commit(rows.map((row) => (row.id === id ? { ...row, key: newKey } : row)));
  };

  const updateValue = (id: number, key: string, value: string) => {
    const secretKey = `${secretKeyPrefix}${key}`;
    if (secretKeys.has(secretKey)) {
      onSecretDraftChange?.(secretKey, value);
      return;
    }
    commit(rows.map((row) => (row.id === id ? { ...row, value } : row)));
  };

  const removeRow = (id: number, key: string) => {
    commit(rows.filter((row) => row.id !== id));
    const secretKey = `${secretKeyPrefix}${key}`;
    if (secretKeys.has(secretKey)) {
      onSecretDraftChange?.(secretKey, "");
    }
  };

  const addRow = () => {
    // Test key presence, not value truthiness: a fresh row's value is "", so the
    // old check treated every new row's name as free and added nothing.
    const taken = new Set(rows.map((row) => row.key.trim()));
    let key = "KEY";
    let index = 1;
    while (taken.has(key)) {
      index += 1;
      key = `KEY_${index}`;
    }
    // Not committed: an empty row carries no value yet, and emitting it now would
    // write a blank entry into the parent's record.
    setRows([...rows, { id: nextRowID++, key, value: "" }]);
  };

  return (
    <div className="space-y-2">
      <div>
        <Label>{label}</Label>
        {description && <p className="text-xs text-muted-foreground">{description}</p>}
      </div>
      {rows.length === 0 ? (
        <p className="text-xs text-muted-foreground">{t("frame.admin.keyValue.empty")}</p>
      ) : (
        <div className="space-y-2">
          {rows.map((row) => {
            const secretKey = `${secretKeyPrefix}${row.key}`;
            const isSecret = secretKeys.has(secretKey);
            const hasStored = storedSecrets.has(secretKey);
            const displayValue = isSecret ? (secretDrafts[secretKey] ?? "") : row.value;

            return (
              <div key={row.id} className="flex gap-2">
                <Input
                  value={row.key}
                  onChange={(e) => updateKey(row.id, e.target.value)}
                  placeholder={t("frame.admin.keyValue.keyPlaceholder")}
                  className="w-1/3"
                />
                <Input
                  type={isSecret ? "password" : "text"}
                  value={displayValue}
                  onChange={(e) => updateValue(row.id, row.key, e.target.value)}
                  placeholder={
                    isSecret && hasStored
                      ? t("frame.admin.keyValue.storedSecret")
                      : t("frame.admin.keyValue.valuePlaceholder")
                  }
                  className="flex-1"
                />
                <Button type="button" variant="ghost" size="icon" onClick={() => removeRow(row.id, row.key)}>
                  <Trash2 className="h-4 w-4 text-destructive" />
                </Button>
              </div>
            );
          })}
        </div>
      )}
      <Button type="button" variant="outline" size="sm" className="gap-1" onClick={addRow}>
        <Plus className="h-3 w-3" />
        {t("frame.admin.keyValue.addField")}
      </Button>
    </div>
  );
}
