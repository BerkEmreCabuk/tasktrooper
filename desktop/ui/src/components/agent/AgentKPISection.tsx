import { useCallback, useEffect, useMemo, useState } from "react";
import { toast } from "sonner";
import { api, type AgentKPI, type CreateKPIInput, type KPIMetricInfo } from "@/api";
import { Button } from "@/components/ui/button";
import { Card } from "@/components/ui/card";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { Switch } from "@/components/ui/switch";
import { useI18n } from "@/hooks/useI18n";

const emptyKPI = (): CreateKPIInput => ({
  metric_key: "",
  name: "",
  description: "",
  period: "weekly",
  target_full: 0,
  target_half: 0,
  weight: 1,
  enabled: true,
});

export function AgentKPISection({ agentId }: { agentId: string }) {
  const { t } = useI18n();
  const [kpis, setKpis] = useState<AgentKPI[]>([]);
  const [metrics, setMetrics] = useState<KPIMetricInfo[]>([]);
  const [draft, setDraft] = useState<CreateKPIInput>(emptyKPI());
  const [editingId, setEditingId] = useState<string | null>(null);
  const [savingKPI, setSavingKPI] = useState(false);

  const metricByKey = useMemo(() => new Map(metrics.map((m) => [m.key, m])), [metrics]);

  const load = useCallback(async () => {
    try {
      const [kpiData, metricData] = await Promise.all([
        api.listAgentKPIs(agentId),
        api.listKPIMetrics(),
      ]);
      setKpis(kpiData.kpis ?? []);
      setMetrics(metricData.metrics ?? []);
    } catch {
      setKpis([]);
    }
  }, [agentId]);

  useEffect(() => {
    void load();
  }, [load]);

  const startEdit = (k: AgentKPI) => {
    setEditingId(k.id);
    setDraft({
      metric_key: k.metric_key,
      name: k.name,
      description: k.description,
      period: k.period,
      target_full: k.target_full,
      target_half: k.target_half,
      weight: k.weight,
      enabled: k.enabled,
    });
  };

  const cancelEdit = () => {
    setEditingId(null);
    setDraft(emptyKPI());
  };

  const handleSubmit = async () => {
    if (!draft.metric_key) {
      toast.error(t("agentArea.components.kpi.toast.selectMetric"));
      return;
    }
    setSavingKPI(true);
    try {
      if (editingId) {
        await api.updateAgentKPI(agentId, editingId, draft);
        toast.success(t("agentArea.components.kpi.toast.updated"));
        setEditingId(null);
      } else {
        await api.createAgentKPI(agentId, draft);
        toast.success(t("agentArea.components.kpi.toast.added"));
      }
      setDraft(emptyKPI());
      await load();
    } catch (e) {
      toast.error(e instanceof Error ? e.message : t("agentArea.components.kpi.toast.saveFailed"));
    } finally {
      setSavingKPI(false);
    }
  };

  const handleDelete = async (kpiId: string) => {
    try {
      await api.deleteAgentKPI(agentId, kpiId);
      toast.success(t("agentArea.components.kpi.toast.deleted"));
      await load();
    } catch (e) {
      toast.error(e instanceof Error ? e.message : t("agentArea.components.kpi.toast.deleteFailed"));
    }
  };

  const handleToggle = async (kpi: AgentKPI, enabled: boolean) => {
    try {
      await api.updateAgentKPI(agentId, kpi.id, {
        metric_key: kpi.metric_key,
        name: kpi.name,
        description: kpi.description,
        period: kpi.period,
        target_full: kpi.target_full,
        target_half: kpi.target_half,
        weight: kpi.weight,
        enabled,
      });
      await load();
    } catch (e) {
      toast.error(e instanceof Error ? e.message : t("agentArea.components.kpi.toast.toggleFailed"));
    }
  };

  const draftMetric = metricByKey.get(draft.metric_key);

  return (
    <Card className="w-full space-y-4 p-6">
      <div>
        <h3 className="text-base font-semibold">{t("agentArea.components.kpi.heading")}</h3>
        <p className="text-sm text-muted-foreground">{t("agentArea.components.kpi.help")}</p>
      </div>
      {kpis.length > 0 ? (
        <div className="space-y-2">
          {kpis.map((k) => {
            const info = metricByKey.get(k.metric_key);
            return (
              <div key={k.id} className="flex items-center justify-between rounded-md border p-3">
                <div className="min-w-0">
                  <div className="text-sm font-medium">
                    {k.name || info?.label || k.metric_key}
                    <span className="ml-2 text-xs text-muted-foreground">
                      {k.metric_key} · {k.period}
                    </span>
                  </div>
                  <div className="text-xs text-muted-foreground">
                    {t("agentArea.components.kpi.targets", { full: k.target_full, half: k.target_half, weight: k.weight })}
                    {info
                      ? ` · ${info.direction === "lower_better" ? t("agentArea.components.kpi.lowerBetter") : t("agentArea.components.kpi.higherBetter")}`
                      : ""}
                  </div>
                </div>
                <div className="flex items-center gap-3">
                  <Switch checked={k.enabled} onCheckedChange={(v) => handleToggle(k, v)} />
                  <Button variant="ghost" size="sm" onClick={() => startEdit(k)}>
                    {t("agentArea.components.kpi.edit")}
                  </Button>
                  <Button variant="ghost" size="sm" onClick={() => handleDelete(k.id)}>
                    {t("agentArea.components.kpi.delete")}
                  </Button>
                </div>
              </div>
            );
          })}
        </div>
      ) : (
        <p className="text-sm text-muted-foreground">{t("agentArea.components.kpi.empty")}</p>
      )}
      <div className="space-y-3 rounded-md border p-3">
        <div className="text-sm font-medium">
          {editingId ? t("agentArea.components.kpi.editTitle") : t("agentArea.components.kpi.newTitle")}
        </div>
        <div className="grid gap-3 lg:grid-cols-6">
        <div className="space-y-1 lg:col-span-2">
          <Label className="text-xs">{t("agentArea.components.kpi.metric")}</Label>
          <Select
            value={draft.metric_key || undefined}
            onValueChange={(v) => {
              const info = metricByKey.get(v);
              setDraft((d) => ({ ...d, metric_key: v, name: info?.label ?? "" }));
            }}
          >
            <SelectTrigger>
              <SelectValue placeholder={t("agentArea.components.kpi.metricPlaceholder")} />
            </SelectTrigger>
            <SelectContent>
              {metrics.map((m) => (
                <SelectItem key={m.key} value={m.key}>
                  {m.label} ({m.direction === "lower_better" ? t("agentArea.components.kpi.lowerBetter") : t("agentArea.components.kpi.higherBetter")})
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
          {draftMetric ? (
            <p className="text-xs text-muted-foreground">{draftMetric.description}</p>
          ) : null}
        </div>
        <div className="space-y-1">
          <Label className="text-xs">{t("agentArea.components.kpi.period")}</Label>
          <Select
            value={draft.period}
            onValueChange={(v) => setDraft((d) => ({ ...d, period: v as CreateKPIInput["period"] }))}
          >
            <SelectTrigger>
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value="daily">{t("agentArea.components.kpi.daily")}</SelectItem>
              <SelectItem value="weekly">{t("agentArea.components.kpi.weekly")}</SelectItem>
              <SelectItem value="monthly">{t("agentArea.components.kpi.monthly")}</SelectItem>
            </SelectContent>
          </Select>
        </div>
        <div className="space-y-1">
          <Label className="text-xs">{t("agentArea.components.kpi.targetFull")}</Label>
          <Input
            type="number"
            value={draft.target_full}
            onChange={(e) => setDraft((d) => ({ ...d, target_full: Number(e.target.value) }))}
          />
        </div>
        <div className="space-y-1">
          <Label className="text-xs">{t("agentArea.components.kpi.targetHalf")}</Label>
          <Input
            type="number"
            value={draft.target_half}
            onChange={(e) => setDraft((d) => ({ ...d, target_half: Number(e.target.value) }))}
          />
        </div>
        <div className="space-y-1">
          <Label className="text-xs">{t("agentArea.components.kpi.weight")}</Label>
          <Input
            type="number"
            min={0}
            step={0.5}
            value={draft.weight}
            onChange={(e) => setDraft((d) => ({ ...d, weight: Number(e.target.value) }))}
          />
        </div>
        </div>
        <div className="flex items-center gap-2">
          <Button size="sm" onClick={handleSubmit} disabled={savingKPI}>
            {savingKPI ? t("common.saving") : editingId ? t("agentArea.components.kpi.update") : t("agentArea.components.kpi.add")}
          </Button>
          {editingId && (
            <Button size="sm" variant="ghost" onClick={cancelEdit} disabled={savingKPI}>
              {t("common.cancel")}
            </Button>
          )}
        </div>
      </div>
    </Card>
  );
}
