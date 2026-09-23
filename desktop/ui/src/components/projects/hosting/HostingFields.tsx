import { ExternalLink } from "lucide-react";
import { useI18n } from "@/hooks/useI18n";
import { cn } from "@/lib/utils";

interface FieldProps {
  label: string;
  children: React.ReactNode;
}

/** One labelled read-only fact, as every provider body prints them. */
export function Field({ label, children }: FieldProps) {
  return (
    <div className="min-w-0 space-y-0.5">
      <p className="text-xs text-muted-foreground">{label}</p>
      <div className="min-w-0 text-sm">{children}</div>
    </div>
  );
}

interface ExternalLinkTextProps {
  href: string;
  label: string;
  mono?: boolean;
}

/** An outward link that names where it goes and says it opens elsewhere. */
export function ExternalLinkText({ href, label, mono }: ExternalLinkTextProps) {
  const { t } = useI18n();
  return (
    <a
      href={href}
      target="_blank"
      rel="noreferrer"
      aria-label={`${label} (${t("projectAdmin.hosting.openInNewTab")})`}
      className={cn("inline-flex min-w-0 items-center gap-1 underline underline-offset-2", mono && "font-mono text-xs")}
    >
      <span className="truncate">{label}</span>
      <ExternalLink className="h-3 w-3 shrink-0" aria-hidden />
    </a>
  );
}
