import { useCallback, useEffect, useState } from "react";
import { toast } from "sonner";
import { api, type BillingPlan, type BillingStatus, type ModelPrice, type UsageSummary } from "@/api";
import { Button } from "@/components/ui/button";
import { Card } from "@/components/ui/card";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Skeleton } from "@/components/ui/skeleton";
import { useI18n } from "@/hooks/useI18n";
import { cn } from "@/lib/utils";

const RANGES = [7, 30, 90] as const;

function formatTokens(n: number): string {
  if (n >= 1_000_000) return `${(n / 1_000_000).toFixed(1)}M`;
  if (n >= 1_000) return `${(n / 1_000).toFixed(1)}K`;
  return String(n);
}

function formatDate(iso: string): string {
  const d = new Date(iso);
  return Number.isNaN(d.getTime()) ? "-" : d.toLocaleDateString();
}

// BillingPanel — the plan and its token budget, both served by the local
// server (`/v1/billing`, `/admin/billing/*`). Editing is offered unless the
// server declares the plan read-only.
function BillingPanel() {
  const { t } = useI18n();
  const [status, setStatus] = useState<BillingStatus | null>(null);
  const [plan, setPlan] = useState<BillingPlan | null>(null);
  const [prices, setPrices] = useState<ModelPrice[]>([]);
  const [showAdmin, setShowAdmin] = useState(false);
  const [newPrice, setNewPrice] = useState<ModelPrice>({ model: "", usd_per_1m_prompt: 0, usd_per_1m_completion: 0 });

  const load = useCallback(async () => {
    try {
      setStatus(await api.billingStatus());
    } catch {
      /* billing disabled → panel gizli kalır */
    }
  }, []);

  const loadAdmin = useCallback(async () => {
    try {
      const [p, pr] = await Promise.all([api.getBillingPlan(), api.listModelPrices()]);
      setPlan(p);
      setPrices(pr.prices ?? []);
    } catch (err) {
      toast.error(err instanceof Error ? err.message : t("settingsPages.usage.planLoadFailed"));
    }
  }, [t]);

  useEffect(() => {
    void load();
  }, [load]);

  const canManagePlan = status !== null && !status.managed;

  useEffect(() => {
    if (showAdmin && canManagePlan && !plan) void loadAdmin();
  }, [showAdmin, canManagePlan, plan, loadAdmin]);

  const savePlan = async () => {
    if (!plan) return;
    try {
      const updated = await api.updateBillingPlan({
        name: plan.name,
        usd_budget: plan.usd_budget,
        max_concurrent_tasks: plan.max_concurrent_tasks,
        period_days: plan.period_days,
        display_token_rate: plan.display_token_rate,
      });
      setPlan(updated);
      await load();
      toast.success(t("settingsPages.usage.planUpdated"));
    } catch (err) {
      toast.error(err instanceof Error ? err.message : t("settingsPages.usage.planSaveFailed"));
    }
  };

  const saveNewPrice = async () => {
    if (!newPrice.model.trim()) return;
    try {
      await api.upsertModelPrice(newPrice);
      setNewPrice({ model: "", usd_per_1m_prompt: 0, usd_per_1m_completion: 0 });
      await loadAdmin();
      await load();
      toast.success(t("settingsPages.usage.priceSaved"));
    } catch (err) {
      toast.error(err instanceof Error ? err.message : t("settingsPages.usage.priceSaveFailed"));
    }
  };

  const removePrice = async (model: string) => {
    try {
      await api.deleteModelPrice(model);
      await loadAdmin();
      await load();
    } catch (err) {
      toast.error(err instanceof Error ? err.message : t("settingsPages.usage.priceDeleteFailed"));
    }
  };

  if (!status) return null;

  const pct = status.token_budget > 0 ? Math.min(100, (status.token_budget_used / status.token_budget) * 100) : 0;

  return (
    <Card className="space-y-4 p-4">
      <div className="flex items-center justify-between">
        <h3 className="text-sm font-semibold">{t("settingsPages.usage.planLimitTitle")}</h3>
        {canManagePlan && (
          <Button size="sm" variant="outline" onClick={() => setShowAdmin((v) => !v)}>
            {showAdmin ? t("settingsPages.usage.close") : t("settingsPages.usage.manage")}
          </Button>
        )}
      </div>

      {status.unlimited ? (
        <p className="text-sm text-muted-foreground">{t("settingsPages.usage.unlimited")}</p>
      ) : (
        <div className="space-y-3">
          <div className="flex flex-wrap items-baseline justify-between gap-2">
            <span className="text-sm">
              {t("settingsPages.usage.packageLabel")}{" "}
              <span className="font-medium">{status.plan_name}</span>
            </span>
            <span className={cn("text-sm tabular-nums", status.exhausted && "text-destructive")}>
              {t("settingsPages.usage.budgetUsage", {
                used: formatTokens(status.token_budget_used),
                budget: formatTokens(status.token_budget),
              })}
            </span>
          </div>
          <div className="h-2 overflow-hidden rounded-full bg-muted">
            <div
              className={cn("h-full rounded-full", status.exhausted ? "bg-destructive" : "bg-primary")}
              style={{ width: `${Math.max(2, pct)}%` }}
            />
          </div>
          <div className="flex flex-wrap gap-x-6 gap-y-1 text-xs text-muted-foreground">
            <span>{t("settingsPages.usage.remaining", { tokens: formatTokens(status.token_remaining) })}</span>
            <span>{t("settingsPages.usage.renewal", { date: formatDate(status.reset_at) })}</span>
            <span>{t("settingsPages.usage.concurrentTasks", { count: status.max_concurrent_tasks })}</span>
          </div>
          {status.exhausted && (
            <p className="text-xs text-destructive">
              {t("settingsPages.usage.exhausted", { date: formatDate(status.reset_at) })}
            </p>
          )}
        </div>
      )}

      {showAdmin && plan && (
        <div className="space-y-4 border-t border-border pt-4">
          <div className="grid gap-3 sm:grid-cols-2">
            <div className="space-y-1">
              <Label className="text-xs">{t("settingsPages.usage.planNameLabel")}</Label>
              <Input value={plan.name} onChange={(e) => setPlan({ ...plan, name: e.target.value })} />
            </div>
            <div className="space-y-1">
              <Label className="text-xs">{t("settingsPages.usage.usdBudgetLabel")}</Label>
              <Input
                type="number"
                value={plan.usd_budget}
                onChange={(e) => setPlan({ ...plan, usd_budget: Number(e.target.value) })}
              />
            </div>
            <div className="space-y-1">
              <Label className="text-xs">{t("settingsPages.usage.concurrentTasksLabel")}</Label>
              <Input
                type="number"
                value={plan.max_concurrent_tasks}
                onChange={(e) => setPlan({ ...plan, max_concurrent_tasks: Number(e.target.value) })}
              />
            </div>
            <div className="space-y-1">
              <Label className="text-xs">{t("settingsPages.usage.periodDaysLabel")}</Label>
              <Input
                type="number"
                value={plan.period_days}
                onChange={(e) => setPlan({ ...plan, period_days: Number(e.target.value) })}
              />
            </div>
            <div className="space-y-1">
              <Label className="text-xs">{t("settingsPages.usage.tokenRateLabel")}</Label>
              <Input
                type="number"
                step="0.0000001"
                value={plan.display_token_rate}
                onChange={(e) => setPlan({ ...plan, display_token_rate: Number(e.target.value) })}
              />
            </div>
          </div>
          <Button size="sm" onClick={savePlan}>
            {t("settingsPages.usage.savePlan")}
          </Button>

          <div className="space-y-2">
            <h4 className="text-xs font-semibold">{t("settingsPages.usage.modelPricesTitle")}</h4>
            {prices.length > 0 && (
              <div className="overflow-x-auto">
                <table className="w-full text-xs">
                  <thead>
                    <tr className="border-b border-border text-left text-muted-foreground">
                      <th className="pb-1 font-medium">{t("settingsPages.usage.modelColumn")}</th>
                      <th className="pb-1 text-right font-medium">{t("settingsPages.usage.inputColumn")}</th>
                      <th className="pb-1 text-right font-medium">{t("settingsPages.usage.outputColumn")}</th>
                      <th className="pb-1" />
                    </tr>
                  </thead>
                  <tbody>
                    {prices.map((p) => (
                      <tr key={p.model} className="border-b border-border/50 last:border-0">
                        <td className="py-1 font-mono">{p.model}</td>
                        <td className="py-1 text-right tabular-nums">{p.usd_per_1m_prompt}</td>
                        <td className="py-1 text-right tabular-nums">{p.usd_per_1m_completion}</td>
                        <td className="py-1 text-right">
                          <Button size="sm" variant="ghost" onClick={() => removePrice(p.model)}>
                            {t("settingsPages.usage.delete")}
                          </Button>
                        </td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>
            )}
            <div className="grid gap-2 sm:grid-cols-4">
              <Input
                placeholder={t("settingsPages.usage.modelPlaceholder")}
                value={newPrice.model}
                onChange={(e) => setNewPrice({ ...newPrice, model: e.target.value })}
              />
              <Input
                type="number"
                placeholder={t("settingsPages.usage.inputPlaceholder")}
                value={newPrice.usd_per_1m_prompt || ""}
                onChange={(e) => setNewPrice({ ...newPrice, usd_per_1m_prompt: Number(e.target.value) })}
              />
              <Input
                type="number"
                placeholder={t("settingsPages.usage.outputPlaceholder")}
                value={newPrice.usd_per_1m_completion || ""}
                onChange={(e) => setNewPrice({ ...newPrice, usd_per_1m_completion: Number(e.target.value) })}
              />
              <Button size="sm" onClick={saveNewPrice}>
                {t("settingsPages.usage.addOrUpdate")}
              </Button>
            </div>
          </div>
        </div>
      )}
    </Card>
  );
}

// LLM sağlayıcı token kullanımı: toplamlar, model kırılımı ve günlük döküm.
// Maliyet hesabı bilinçli olarak yok — fiyatlar sağlayıcıya/moda göre değişiyor;
// ham token sayısı faturayla birebir karşılaştırılabilir tek veri.
export function UsageSettingsPage() {
  const { t } = useI18n();
  const [days, setDays] = useState<number>(30);
  const [summary, setSummary] = useState<UsageSummary | null>(null);
  const [loading, setLoading] = useState(true);

  const load = useCallback(async (range: number) => {
    setLoading(true);
    try {
      setSummary(await api.usageSummary(range));
    } catch (err) {
      toast.error(err instanceof Error ? err.message : t("settingsPages.usage.usageLoadFailed"));
    } finally {
      setLoading(false);
    }
  }, [t]);

  useEffect(() => {
    void load(days);
  }, [days, load]);

  if (loading && !summary) {
    return (
      <div className="space-y-4">
        <BillingPanel />
        <Skeleton className="h-24 w-full" />
        <Skeleton className="h-48 w-full" />
      </div>
    );
  }

  const total = summary?.total ?? { calls: 0, prompt_tokens: 0, completion_tokens: 0 };
  const byModel = summary?.by_model ?? [];
  const daily = [...(summary?.daily ?? [])].reverse();
  const maxDayTokens = Math.max(1, ...daily.map((d) => d.prompt_tokens + d.completion_tokens));

  return (
    <div className="space-y-6">
      <BillingPanel />
      <div className="flex items-center gap-2">
        {RANGES.map((r) => (
          <Button
            key={r}
            size="sm"
            variant={days === r ? "default" : "outline"}
            onClick={() => setDays(r)}
          >
            {t("settingsPages.usage.lastNDays", { days: r })}
          </Button>
        ))}
      </div>

      <div className="grid gap-4 sm:grid-cols-3">
        <Card className="p-4">
          <p className="text-xs text-muted-foreground">{t("settingsPages.usage.llmCalls")}</p>
          <p className="mt-1 text-2xl font-semibold">{total.calls}</p>
        </Card>
        <Card className="p-4">
          <p className="text-xs text-muted-foreground">{t("settingsPages.usage.inputTokens")}</p>
          <p className="mt-1 text-2xl font-semibold">{formatTokens(total.prompt_tokens)}</p>
        </Card>
        <Card className="p-4">
          <p className="text-xs text-muted-foreground">{t("settingsPages.usage.outputTokens")}</p>
          <p className="mt-1 text-2xl font-semibold">{formatTokens(total.completion_tokens)}</p>
        </Card>
      </div>

      <Card className="p-4">
        <h3 className="mb-3 text-sm font-semibold">{t("settingsPages.usage.byModel")}</h3>
        {byModel.length === 0 ? (
          <p className="text-sm text-muted-foreground">{t("settingsPages.usage.noUsage")}</p>
        ) : (
          <div className="overflow-x-auto">
            <table className="w-full text-sm">
              <thead>
                <tr className="border-b border-border text-left text-xs text-muted-foreground">
                  <th className="pb-2 font-medium">{t("settingsPages.usage.modelColumn")}</th>
                  <th className="pb-2 text-right font-medium">{t("settingsPages.usage.callsColumn")}</th>
                  <th className="pb-2 text-right font-medium">{t("settingsPages.usage.inputColumn")}</th>
                  <th className="pb-2 text-right font-medium">{t("settingsPages.usage.outputColumn")}</th>
                </tr>
              </thead>
              <tbody>
                {byModel.map((m) => (
                  <tr key={m.model} className="border-b border-border/50 last:border-0">
                    <td className="py-2 font-mono text-xs">{m.model}</td>
                    <td className="py-2 text-right">{m.calls}</td>
                    <td className="py-2 text-right">{formatTokens(m.prompt_tokens)}</td>
                    <td className="py-2 text-right">{formatTokens(m.completion_tokens)}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        )}
      </Card>

      <Card className="p-4">
        <h3 className="mb-3 text-sm font-semibold">{t("settingsPages.usage.daily")}</h3>
        {daily.length === 0 ? (
          <p className="text-sm text-muted-foreground">{t("settingsPages.usage.noUsage")}</p>
        ) : (
          <div className="space-y-1.5">
            {daily.map((d) => {
              const tokens = d.prompt_tokens + d.completion_tokens;
              return (
                <div key={d.day} className="flex items-center gap-3 text-xs">
                  <span className="w-20 shrink-0 text-muted-foreground">{d.day}</span>
                  <div className="h-2 flex-1 overflow-hidden rounded-full bg-muted">
                    <div
                      className={cn("h-full rounded-full bg-primary")}
                      style={{ width: `${Math.max(2, (tokens / maxDayTokens) * 100)}%` }}
                    />
                  </div>
                  <span className="w-16 shrink-0 text-right tabular-nums">
                    {formatTokens(tokens)}
                  </span>
                </div>
              );
            })}
          </div>
        )}
      </Card>
    </div>
  );
}
