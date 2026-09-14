import { HelpCircle } from "lucide-react";
import type { ClarificationRequest } from "@/api";
import { Badge } from "@/components/ui/badge";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { useI18n } from "@/hooks/useI18n";
import { cn } from "@/lib/utils";

interface ClarificationSummaryProps {
  clarification: ClarificationRequest;
  /** Question id → the answer given, from `parseClarificationAnswers`. */
  answers: Record<string, string>;
  className?: string;
}

/**
 * The read-only form of a clarification, kept in the transcript after it is
 * answered. Previously the interactive card was the only rendering, and it
 * disappeared the moment the user replied — taking the questions with it, so
 * the conversation lost the reason its answer made sense.
 */
export function ClarificationSummary({
  clarification,
  answers,
  className,
}: ClarificationSummaryProps) {
  const { t } = useI18n();

  return (
    <Card className={cn("border-border bg-muted/30", className)}>
      <CardHeader className="pb-2">
        <div className="flex items-center justify-between gap-2">
          <CardTitle className="flex items-center gap-2 text-sm font-medium">
            <HelpCircle className="h-4 w-4 text-muted-foreground" />
            {t("chatArea.chat.clarification.title")}
          </CardTitle>
          <Badge variant="secondary">{t("chatArea.chat.clarification.answered")}</Badge>
        </div>
        {clarification.context && (
          <p className="text-sm text-muted-foreground">{clarification.context}</p>
        )}
      </CardHeader>
      <CardContent className="space-y-3">
        {clarification.questions.map((q) => (
          <div key={q.id} className="space-y-1">
            <p className="text-sm font-medium">{q.prompt}</p>
            {answers[q.id] ? (
              <p className="text-sm text-muted-foreground">
                <span className="mr-1 text-xs uppercase tracking-wide opacity-70">
                  {t("chatArea.chat.clarification.yourAnswer")}:
                </span>
                {answers[q.id]}
              </p>
            ) : (
              <div className="flex flex-wrap gap-1.5">
                {q.options.map((opt) => (
                  <Badge key={opt.id} variant="outline" className="font-normal">
                    {opt.label}
                  </Badge>
                ))}
              </div>
            )}
          </div>
        ))}
      </CardContent>
    </Card>
  );
}
