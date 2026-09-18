import { useCallback, useEffect, useState } from "react";
import { useParams } from "react-router-dom";
import { Loader2, Save } from "lucide-react";
import { toast } from "sonner";
import { api, type Agent, type AgentColumnSubscription, type BoardColumn } from "@/api";
import { AgentRolesSection } from "@/components/agent/AgentRolesSection";
import { Card } from "@/components/ui/card";
import { Button } from "@/components/ui/button";
import { Checkbox } from "@/components/ui/checkbox";
import { Skeleton } from "@/components/ui/skeleton";
import { MultiSelectPicker } from "@/components/admin/MultiSelectPicker";
import { useI18n } from "@/hooks/useI18n";
import { useTaskTypes } from "@/hooks/useTaskTypes";
import { taskTypeOptions } from "@/lib/project-board";

export function AgentColumnsPage() {
  const { t } = useI18n();
  const { agentId } = useParams();
  const { taskTypes } = useTaskTypes();
  const [agent, setAgent] = useState<Agent | null>(null);
  const [columns, setColumns] = useState<BoardColumn[]>([]);
  // column_slug -> null (every task type) | string[] (filtered to these types)
  const [subs, setSubs] = useState<Map<string, string[] | null>>(new Map());
  const [loading, setLoading] = useState(true);
  const [saving, setSaving] = useState(false);

  const load = useCallback(async () => {
    if (!agentId) return;
    setLoading(true);
    try {
      const [cols, subsRes, agentRes] = await Promise.all([
        api.listBoardColumns(),
        api.getAgentSubscriptions(agentId),
        api.getAgent(agentId),
      ]);
      setColumns((cols.columns ?? []).filter((c) => !c.is_backlog));
      const next = new Map<string, string[] | null>();
      // subscriptions carries the detailed (column_slug, task_types) shape;
      // column_slugs is the legacy fallback for a server that hasn't gained it.
      if (subsRes.subscriptions && subsRes.subscriptions.length > 0) {
        for (const s of subsRes.subscriptions) next.set(s.column_slug, s.task_types);
      } else {
        for (const slug of subsRes.column_slugs ?? []) next.set(slug, null);
      }
      setSubs(next);
      setAgent(agentRes);
    } catch (e) {
      toast.error(e instanceof Error ? e.message : t("agentArea.columns.toast.loadFailed"));
    } finally {
      setLoading(false);
    }
  }, [agentId, t]);

  useEffect(() => {
    load();
  }, [load]);

  const toggleColumn = (slug: string) => {
    setSubs((prev) => {
      const next = new Map(prev);
      if (next.has(slug)) next.delete(slug);
      else next.set(slug, null);
      return next;
    });
  };

  const setColumnTypes = (slug: string, types: string[] | null) => {
    setSubs((prev) => {
      const next = new Map(prev);
      next.set(slug, types);
      return next;
    });
  };

  const save = async () => {
    if (!agentId) return;
    setSaving(true);
    try {
      const subscriptions: AgentColumnSubscription[] = [...subs.entries()].map(([column_slug, types]) => ({
        column_slug,
        task_types: types,
      }));
      await api.setAgentSubscriptions(agentId, subscriptions);
      toast.success(t("agentArea.columns.toast.saved"));
    } catch (e) {
      toast.error(e instanceof Error ? e.message : t("agentArea.columns.toast.saveFailed"));
    } finally {
      setSaving(false);
    }
  };

  const typeOptions = taskTypeOptions(taskTypes);

  if (loading) {
    return (
      <div className="space-y-2">
        <Skeleton className="h-8 w-64" />
        <Skeleton className="h-40 w-full" />
      </div>
    );
  }

  return (
    <div className="max-w-2xl space-y-6">
      <div>
        <h2 className="font-semibold">{t("agentArea.columns.heading")}</h2>
        <p className="mt-1 text-sm text-muted-foreground">{t("agentArea.columns.help")}</p>
      </div>

      <Card className="divide-y divide-border">
        {columns.map((col) => {
          const checked = subs.has(col.slug);
          const types = subs.get(col.slug) ?? null;
          return (
            <div key={col.slug} className="space-y-2 px-4 py-3">
              <label className="flex cursor-pointer items-center gap-3">
                <Checkbox checked={checked} onCheckedChange={() => toggleColumn(col.slug)} />
                <span className="text-sm font-medium">{col.label}</span>
                <span className="ml-auto font-mono text-xs text-muted-foreground">{col.slug}</span>
              </label>
              {checked && (
                <div className="pl-7">
                  <MultiSelectPicker
                    label={t("agentArea.columns.taskTypeFilterLabel")}
                    options={typeOptions.map((o) => ({ value: o.value, label: o.label }))}
                    selected={types ?? []}
                    onChange={(values) => setColumnTypes(col.slug, values.length === 0 ? null : values)}
                    emptyText={t("agentArea.columns.taskTypeFilterEmpty")}
                  />
                  <p className="mt-1 text-xs text-muted-foreground">
                    {types === null
                      ? t("agentArea.columns.taskTypeFilterAll")
                      : t("agentArea.columns.taskTypeFilterSome")}
                  </p>
                </div>
              )}
            </div>
          );
        })}
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

      {agentId && <AgentRolesSection agentId={agentId} agentName={agent?.name ?? agentId} />}
    </div>
  );
}
