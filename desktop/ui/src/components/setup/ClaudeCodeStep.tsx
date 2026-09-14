import { Loader2, Plug } from "lucide-react";
import { useState } from "react";
import { api, type AgentCLIFlavor } from "@/api";
import {
  type ConnectStep,
  connectAgentCli,
  connectFailure,
} from "@/components/runner/claudeCodeConnect";
import { connectStepLine } from "@/components/runner/LocalCliCard";
import { Button } from "@/components/ui/button";
import { Notice } from "@/components/ui/notice";
import { useI18n } from "@/hooks/useI18n";
import { useSetup } from "@/hooks/useSetup";
import type { SetupStepId } from "@/lib/setup";

/** The flavor this step connects. */
const CLAUDE_FLAVOR: AgentCLIFlavor = "claude";

/**
 * Step 2: connect Claude Code.
 *
 * The whole flow is `connectAgentCli` — `POST /v1/agent-cli/claude/connect`,
 * which verifies the binary and installs the catalog — exactly as the Claude
 * Code card on Settings → LLM Connection runs it, with the same retries, the
 * same narration (`connectStepLine`) and the same failure wording
 * (`connectFailure`). Nothing about connecting is reimplemented here; what this
 * adds is the gate in front of it and the plain verdict after it.
 *
 * Connect is drawn only when the environment step is green: the binary the
 * server looks for is the one the preflight just failed to find, so an enabled
 * button whose only outcome is that same refusal is worse than a sentence
 * naming the item to fix first.
 */
export function ClaudeCodeStep({ onContinue }: { onContinue: (next: SetupStepId) => void }) {
  const { t } = useI18n();
  const { steps, cliState, refresh } = useSetup();
  const step = steps["claude-code"];
  const [busy, setBusy] = useState(false);
  const [flowStep, setFlowStep] = useState<ConnectStep | null>(null);
  const [error, setError] = useState("");

  const environment = steps.environment;
  const connected = cliState?.connections?.find((c) => c.flavor === CLAUDE_FLAVOR) ?? null;

  const connect = async () => {
    setBusy(true);
    setError("");
    try {
      await connectAgentCli({ flavor: CLAUDE_FLAVOR, api, onStep: setFlowStep });
    } catch (e) {
      setError(connectFailure(e).message);
    } finally {
      setBusy(false);
      setFlowStep(null);
      // Re-derive rather than trust what the call returned.
      await refresh();
    }
  };

  return (
    <div className="space-y-3">
      <p className="text-sm text-muted-foreground">{t("setup.claudeCode.description")}</p>

      {step.state === "unknown" && (
        <Notice variant="warning" title={t("setup.state.unknown")}>
          <p>{step.error ?? t("setup.unknownHint")}</p>
          {step.error && <p className="mt-1">{t("setup.unknownHint")}</p>}
        </Notice>
      )}

      {step.state === "done" && (
        <Notice variant="info" title={t("setup.claudeCode.connectedTitle")}>
          {connectedLine(t, connected)}
        </Notice>
      )}

      {step.state === "todo" && !connected && (
        <Notice variant="warning" title={t("setup.claudeCode.disconnectedTitle")}>
          {t("setup.claudeCode.disconnectedBody")}
        </Notice>
      )}

      {busy && flowStep !== null && (
        <p className="flex items-center gap-2 text-xs text-muted-foreground">
          <Loader2 className="h-3.5 w-3.5 shrink-0 animate-spin" aria-hidden />
          {connectStepLine(flowStep, t)}
        </p>
      )}

      {error && (
        <Notice variant="error" title={t("setup.claudeCode.failed")}>
          {error}
        </Notice>
      )}

      {step.state === "done" ? (
        <Button onClick={() => onContinue("github")}>{t("setup.environment.continue")}</Button>
      ) : // Withheld only when the checklist POSITIVELY says a required item is
      // failing. A checklist that could not be read is not evidence of
      // anything, and refusing on it would strand someone behind a probe that
      // failed; the server still refuses for real, with its own sentence.
      environment.state !== "todo" ? (
        <Button onClick={() => void connect()} disabled={busy}>
          {busy ? <Loader2 className="mr-2 h-4 w-4 animate-spin" /> : <Plug className="mr-2 h-4 w-4" />}
          {busy ? t("setup.claudeCode.connecting") : t("setup.claudeCode.connect")}
        </Button>
      ) : (
        <p className="text-sm text-warning">
          {t("setup.claudeCode.blockedByEnvironment", {
            item: t(SETUP_ENVIRONMENT_LABEL),
          })}
        </p>
      )}
    </div>
  );
}

/**
 * What the environment step is called when this one names it as the blocker.
 *
 * The failing ITEM's own label lives inside `EnvironmentPreflight`'s report and
 * is not worth threading through two components for one sentence; naming the
 * step the user has to go back to is the actionable half either way.
 */
const SETUP_ENVIRONMENT_LABEL = "setup.environment.title";

/** The connect's own evidence: which binary answered, and how much catalog landed. */
function connectedLine(
  t: (key: string, params?: Record<string, string | number>) => string,
  connected: { binary_path: string; binary_version: string; agent_count: number; skill_count: number } | null,
): string {
  if (!connected) return t("setup.claudeCode.connectedTitle");
  return t("setup.claudeCode.connectedBody", {
    binary: connected.binary_path,
    version: connected.binary_version ? ` · ${connected.binary_version}` : "",
    agents: connected.agent_count,
    skills: connected.skill_count,
  });
}
