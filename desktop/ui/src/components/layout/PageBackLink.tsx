import { ArrowLeft } from "lucide-react";
import { Link } from "react-router-dom";
import { Button } from "@/components/ui/button";
import { cn } from "@/lib/utils";

interface PageBackLinkProps {
  to: string;
  label: string;
  className?: string;
}

export function PageBackLink({ to, label, className }: PageBackLinkProps) {
  return (
    <Button variant="ghost" size="sm" className={cn("-ml-2 mb-4 gap-1.5 text-muted-foreground", className)} asChild>
      <Link to={to}>
        <ArrowLeft className="h-4 w-4" />
        {label}
      </Link>
    </Button>
  );
}
