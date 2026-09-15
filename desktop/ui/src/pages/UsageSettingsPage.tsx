import { useCallback, useEffect, useState } from "react";
import { toast } from "sonner";
import { api, type UsageSummary } from "@/api";
import { Button } from "@/components/ui/button";
import { Card } from "@/components/ui/card";
import { Skeleton } from "@/components/ui/skeleton";
import { useI18n } from "@/hooks/useI18n";
import { cn } from "@/lib/utils";

const RANGES = [7, 30, 90] as const;

function formatTokens(n: number): string {
  if (n >= 1_000_000) return `${(n / 1_000_000).toFixed(1)}M`;
  if (n >= 1_000) return `${(n / 1_000).toFixed(1)}K`;
  return String(n);
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
