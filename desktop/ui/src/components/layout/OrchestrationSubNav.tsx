import { Bot, ScrollText, Sparkles } from "lucide-react";
import { NavLink } from "react-router-dom";
import { cn } from "@/lib/utils";

const tabs = [
  { to: "/orchestration/skills", label: "Yetenekler", icon: Sparkles },
  { to: "/orchestration/agents", label: "Ajanlar", icon: Bot },
  { to: "/orchestration/rules", label: "Kurallar", icon: ScrollText },
];

export function OrchestrationSubNav() {
  return (
    <nav className="mb-6 flex flex-wrap gap-1 border-b border-border pb-px">
      {tabs.map(({ to, label, icon: Icon }) => (
        <NavLink
          key={to}
          to={to}
          className={({ isActive }) =>
            cn(
              "inline-flex items-center gap-2 border-b-2 px-4 py-2.5 text-sm font-medium transition-colors -mb-px",
              isActive
                ? "border-primary text-primary"
                : "border-transparent text-muted-foreground hover:border-border hover:text-foreground",
            )
          }
        >
          <Icon className="h-4 w-4 shrink-0" />
          {label}
        </NavLink>
      ))}
    </nav>
  );
}
