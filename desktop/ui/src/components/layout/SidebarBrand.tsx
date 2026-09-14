import { Bot } from "lucide-react";
import { useI18n } from "@/hooks/useI18n";
import { cn } from "@/lib/utils";

interface SidebarBrandProps {
  collapsed?: boolean;
  className?: string;
}

export function SidebarBrand({ collapsed = false, className }: SidebarBrandProps) {
  const { t } = useI18n();
  if (collapsed) {
    return (
      <div className={cn("flex h-8 w-8 shrink-0 items-center justify-center rounded-lg bg-primary text-primary-foreground", className)}>
        <Bot className="h-4 w-4" />
      </div>
    );
  }

  return (
    <div className={cn("flex min-w-0 items-center gap-2", className)}>
      <div className="flex h-8 w-8 shrink-0 items-center justify-center rounded-lg bg-primary text-primary-foreground">
        <Bot className="h-4 w-4" />
      </div>
      <div className="min-w-0">
        <p className="whitespace-nowrap text-sm font-semibold text-sidebar-accent-foreground">TaskTrooper Bridge</p>
        <p className="whitespace-nowrap text-xs text-muted-foreground">{t("frame.layout.brand.subtitle")}</p>
      </div>
    </div>
  );
}
