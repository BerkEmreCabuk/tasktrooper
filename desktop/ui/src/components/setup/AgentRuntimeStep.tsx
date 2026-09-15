import { KeyRound, Loader2, Plug, Unplug } from "lucide-react";
import { useState } from "react";
import { useNavigate } from "react-router-dom";
import { api, type AgentCLIConnection, type AgentCLIFlavor } from "@/api";
import {
  type ConnectStep,
  connectAgentCli,
  connectFailure,
  disconnectAgentCli,
} from "@/components/runner/claudeCodeConnect";
import { connectStepLine } from "@/components/runner/LocalCliCard";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card } from "@/components/ui/card";
import { Notice } from "@/components/ui/notice";
import { useI18n } from "@/hooks/useI18n";
import { useSetup } from "@/hooks/useSetup";
import type { SetupStepId } from "@/lib/setup";

/** The preflight item that says whether each CLI is installed on this Mac. */
const PREFLIGHT_ID: Record<AgentCLIFlavor, string> = {
  claude: "claude",
  cursor: "cursor-agent",
  antigravity: "agy",
  opencode: "opencode",
};

/** Used until the server's own flavor list has been read. */
const FALLBACK_FLAVORS: { flavor: AgentCLIFlavor; label: string; available: boolean }[] = [
  { flavor: "claude", label: "Claude Code", available: true },
  { flavor: "cursor", label: "Cursor", available: true },
  { flavor: "antigravity", label: "Antigravity", available: true },
  { flavor: "opencode", label: "OpenCode", available: true },
];

type T = (key: string, params?: Record<string, string | number>) => string;

/**
 * Step 2: give the agents something to run on.
 *
 * Every local agent CLI is offered and any of them can be connected, one or
 * several; the connect itself is the same `connectAgentCli` flow the cards on
 * Settings → LLM Connection run. An API provider with the user's own key is the
 * other way through, and it lives on that settings page, so it is linked rather
 * than rebuilt here.
 */
export function AgentRuntimeStep({ onContinue }: { onContinue: (next: SetupStepId) => void }) {
  const { t } = useI18n();
  const navigate = useNavigate();
  const { steps, cliState, preflightReport, refresh } = useSetup();
  const step = steps.agent;
  const [busy, setBusy] = useState<AgentCLIFlavor | null>(null);
  const [flowStep, setFlowStep] = useState<ConnectStep | null>(null);
  const [error, setError] = useState("");

  const flavors = cliState?.flavors?.length ? cliState.flavors : FALLBACK_FLAVORS;

  const run = async (flavor: AgentCLIFlavor, action: typeof connectAgentCli) => {
    setBusy(flavor);
    setError("");
    try {
      await action({ flavor, api, onStep: setFlowStep });
    } catch (e) {
      setError(connectFailure(e).message);
    } finally {
      setBusy(null);
      setFlowStep(null);
      await refresh();
    }
  };

  return (
    <div className="space-y-3">
      <p className="text-sm text-muted-foreground">{t("setup.agent.description")}</p>

      {step.state === "unknown" && (
        <Notice variant="warning" title={t("setup.state.unknown")}>
          <p>{step.error ?? t("setup.unknownHint")}</p>
          {step.error && <p className="mt-1">{t("setup.unknownHint")}</p>}
        </Notice>
      )}

      {step.state === "todo" && (
        <Notice variant="warning" title={t("setup.agent.noneTitle")}>
          {t("setup.agent.noneBody")}
        </Notice>
      )}

      <div className="space-y-2">
        {flavors.map(({ flavor, label, available }) => {
          const connection = cliState?.connections?.find((c) => c.flavor === flavor) ?? null;
          const item = preflightReport?.items.find((i) => i.id === PREFLIGHT_ID[flavor]);
          const missing = item !== undefined && item.status !== "ok";
          return (
            <Card key={flavor} className="flex flex-wrap items-center justify-between gap-3 p-3">
              <div className="min-w-0 flex-1">
                <p className="text-sm font-medium">{label}</p>
                <p className="truncate text-xs text-muted-foreground">
                  {rowDetail(t, connection, missing, item?.command, item?.version)}
                </p>
              </div>
              <div className="flex shrink-0 items-center gap-2">
                {connection ? (
                  <Badge variant="success">{t("setup.agent.connected")}</Badge>
                ) : missing ? (
                  <Badge variant="outline">{t("setup.agent.notInstalled")}</Badge>
                ) : null}
                {connection ? (
                  <Button
                    size="sm"
                    variant="outline"
                    onClick={() => void run(flavor, disconnectAgentCli)}
                    disabled={busy !== null}
                  >
                    <Unplug className="mr-1.5 h-3.5 w-3.5" />
                    {t("setup.agent.disconnect")}
                  </Button>
                ) : (
                  <Button
                    size="sm"
                    onClick={() => void run(flavor, connectAgentCli)}
                    disabled={busy !== null || missing || !available}
                  >
                    {busy === flavor ? (
                      <Loader2 className="mr-1.5 h-3.5 w-3.5 animate-spin" />
                    ) : (
                      <Plug className="mr-1.5 h-3.5 w-3.5" />
                    )}
                    {busy === flavor ? t("setup.agent.connecting") : t("setup.agent.connect")}
                  </Button>
                )}
              </div>
            </Card>
          );
        })}
      </div>

      {busy !== null && flowStep !== null && (
        <p className="flex items-center gap-2 text-xs text-muted-foreground">
          <Loader2 className="h-3.5 w-3.5 shrink-0 animate-spin" aria-hidden />
          {connectStepLine(flowStep, t)}
        </p>
      )}

      {error && (
        <Notice variant="error" title={t("setup.agent.failed")}>
          {error}
        </Notice>
      )}

      <Notice variant="info" title={t("setup.agent.apiKeyTitle")}>
        <p>{t("setup.agent.apiKeyBody")}</p>
        <Button size="sm" variant="outline" className="mt-2" onClick={() => navigate("/settings/llm")}>
          <KeyRound className="mr-1.5 h-3.5 w-3.5" />
          {t("setup.agent.apiKeyAction")}
        </Button>
      </Notice>

      {step.state === "done" && (
        <Button onClick={() => onContinue("github")}>{t("setup.environment.continue")}</Button>
      )}
    </div>
  );
}

function rowDetail(
  t: T,
  connection: AgentCLIConnection | null,
  missing: boolean,
  command: string | undefined,
  version: string | undefined,
): string {
  if (connection) {
    return t("setup.agent.connectedBody", {
      binary: connection.binary_path,
      version: connection.binary_version ? ` · ${connection.binary_version}` : "",
      agents: connection.agent_count,
      skills: connection.skill_count,
    });
  }
  if (missing) return command ? t("setup.agent.installWith", { command }) : t("setup.agent.notInstalledBody");
  return version ?? "";
}
