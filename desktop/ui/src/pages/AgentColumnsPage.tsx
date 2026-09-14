import { useCallback, useEffect, useState } from "react";
import { useParams } from "react-router-dom";
import { Loader2, Save } from "lucide-react";
import { toast } from "sonner";
import { api, type BoardColumn } from "@/api";
import { Button } from "@/components/ui/button";
import { Card } from "@/components/ui/card";
import { Checkbox } from "@/components/ui/checkbox";
import { Skeleton } from "@/components/ui/skeleton";
import { useI18n } from "@/hooks/useI18n";

export function AgentColumnsPage() {
  const { t } = useI18n();
  const { agentId } = useParams();
  const [columns, setColumns] = useState<BoardColumn[]>([]);
  const [selected, setSelected] = useState<Set<string>>(new Set());
  const [loading, setLoading] = useState(true);
  const [saving, setSaving] = useState(false);

  const load = useCallback(async () => {
    if (!agentId) return;
    setLoading(true);
    try {
      const [cols, subs] = await Promise.all([
        api.listBoardColumns(),
        api.getAgentSubscriptions(agentId),
      ]);
      setColumns((cols.columns ?? []).filter((c) => !c.is_backlog));
      setSelected(new Set(subs.column_slugs ?? []));
    } catch (e) {
      toast.error(e instanceof Error ? e.message : t("agentArea.columns.toast.loadFailed"));
    } finally {
      setLoading(false);
    }
  }, [agentId, t]);

  useEffect(() => {
    load();
  }, [load]);

  const toggle = (slug: string) => {
    setSelected((prev) => {
      const next = new Set(prev);
      if (next.has(slug)) next.delete(slug);
      else next.add(slug);
      return next;
    });
  };

  const save = async () => {
    if (!agentId) return;
    setSaving(true);
    try {
      await api.setAgentSubscriptions(agentId, [...selected]);
      toast.success(t("agentArea.columns.toast.saved"));
    } catch (e) {
      toast.error(e instanceof Error ? e.message : t("agentArea.columns.toast.saveFailed"));
    } finally {
      setSaving(false);
    }
  };

  if (loading) {
    return (
      <div className="space-y-2">
        <Skeleton className="h-8 w-64" />
        <Skeleton className="h-40 w-full" />
      </div>
    );
  }

  return (
    <div className="max-w-2xl space-y-4">
      <div>
        <h2 className="font-semibold">{t("agentArea.columns.heading")}</h2>
        <p className="mt-1 text-sm text-muted-foreground">{t("agentArea.columns.help")}</p>
      </div>

      <Card className="divide-y divide-border">
        {columns.map((col) => (
          <label
            key={col.slug}
            className="flex cursor-pointer items-center gap-3 px-4 py-3 hover:bg-muted/30"
          >
            <Checkbox
              checked={selected.has(col.slug)}
              onCheckedChange={() => toggle(col.slug)}
            />
            <span className="text-sm font-medium">{col.label}</span>
            <span className="ml-auto font-mono text-xs text-muted-foreground">{col.slug}</span>
          </label>
        ))}
        {columns.length === 0 && (
          <p className="px-4 py-6 text-center text-sm text-muted-foreground">
            {t("agentArea.columns.empty")}
          </p>
        )}
      </Card>

      <Button onClick={save} disabled={saving} className="gap-2">
        {saving ? <Loader2 className="h-4 w-4 animate-spin" /> : <Save className="h-4 w-4" />}
        {t("common.save")}
      </Button>
    </div>
  );
}
