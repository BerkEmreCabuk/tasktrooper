import { Check, Copy } from "lucide-react";
import { useState } from "react";
import { Button } from "@/components/ui/button";
import { Notice } from "@/components/ui/notice";
import { useI18n } from "@/hooks/useI18n";
import type { DesktopBlocker } from "@/lib/desktop-bridge";

/**
 * A blocker is the only failure that stops the user and comes with its own
 * fix. It names what is missing, why it matters on this machine, and the
 * exact command — shown in full, never hidden behind a button that runs
 * something unread. Rendered on the Claude Code card (Settings → LLM
 * Connection); it is the last piece of the old "This Mac…" details that
 * survived, because it is the one a person can act on.
 */
export function BlockerNotice({ blocker }: { blocker: DesktopBlocker }) {
  const { t } = useI18n();
  const [copied, setCopied] = useState(false);

  return (
    <Notice variant="warning" className="mt-4" title={blocker.title}>
      <p>{blocker.because}</p>
      <p className="mt-1 font-medium">{blocker.remediation}</p>
      {blocker.command && (
        <>
          <code className="mt-2 block break-all rounded bg-background/60 px-2 py-1.5 font-mono text-xs">
            {blocker.command}
          </code>
          <Button
            variant="outline"
            size="sm"
            className="mt-2"
            onClick={() => {
              void navigator.clipboard.writeText(blocker.command).then(() => setCopied(true));
            }}
          >
            {copied ? <Check className="mr-2 h-4 w-4" /> : <Copy className="mr-2 h-4 w-4" />}
            {copied
              ? t("settingsPages.llm.claudeCode.preflight.copied")
              : t("settingsPages.llm.claudeCode.preflight.copyCommand")}
          </Button>
        </>
      )}
    </Notice>
  );
}
