import { GitBranch, RefreshCw, Unlink } from "lucide-react";
import { useCallback, useEffect, useRef, useState } from "react";
import { toast } from "sonner";
import { api, type GitHubConnectionStatus } from "@/api";
import { Button } from "@/components/ui/button";
import { Card } from "@/components/ui/card";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { useI18n } from "@/hooks/useI18n";
import { cn } from "@/lib/utils";

interface GitHubCardProps {
  /**
   * Every outcome this card learns, for a caller deriving something from it.
   * A null status with a message is a check that could not run, which is never
   * the same as "not connected" — see the guided setup, which renders the two
   * differently.
   */
  onStatusChange?: (result: { status: GitHubConnectionStatus | null; error: string }) => void;
  className?: string;
}

/**
 * Connecting the GitHub account the agents push with.
 *
 * A pasted personal access token, not an OAuth hop: there is no gateway to hold
 * an OAuth app's client secret, and a local server cannot receive GitHub's
 * callback. The server verifies the token against the API before storing it
 * encrypted, so a typo fails here rather than at the first push.
 *
 * One component for two screens — Settings → Integrations and step 3 of the
 * guided setup — because it is one connection, and a second affordance for it
 * would be a second place to read a different answer from.
 */
export function GitHubCard({ onStatusChange, className }: GitHubCardProps) {
  const { t } = useI18n();
  const [status, setStatus] = useState<GitHubConnectionStatus | null>(null);
  const [checking, setChecking] = useState(false);
  const [saving, setSaving] = useState(false);
  const [token, setToken] = useState("");

  // Held in a ref, not a dependency: callers pass an inline arrow, and making
  // `check` depend on it would restart the mount effect on every render of
  // whatever owns this card.
  const onStatusChangeRef = useRef(onStatusChange);
  onStatusChangeRef.current = onStatusChange;

  const apply = useCallback((next: GitHubConnectionStatus | null, error: string) => {
    setStatus(next);
    onStatusChangeRef.current?.({ status: next, error });
  }, []);

  const check = useCallback(async () => {
    setChecking(true);
    try {
      apply(await api.githubStatus(), "");
    } catch (e) {
      // Null is "could not tell", not "not connected" — the render below says
      // exactly that, and the caller is handed the sentence to say it too.
      apply(null, e instanceof Error ? e.message : String(e));
    } finally {
      setChecking(false);
    }
  }, [apply]);

  useEffect(() => {
    void check();
  }, [check]);

  const handleConnect = async () => {
    const value = token.trim();
    if (!value) return;
    setSaving(true);
    try {
      apply(await api.setGitHubToken(value), "");
      setToken("");
      toast.success(t("settings.github.connectedToast"));
    } catch (e) {
      // The server's own sentence: it names what GitHub refused, which a
      // generic failure cannot.
      toast.error(e instanceof Error ? e.message : t("settings.github.connectFailedToast"));
    } finally {
      setSaving(false);
    }
  };

  const handleDisconnect = async () => {
    setSaving(true);
    try {
      apply(await api.disconnectGitHub(), "");
      toast.success(t("settings.github.disconnectedToast"));
    } catch (e) {
      toast.error(e instanceof Error ? e.message : t("common.actionFailed"));
    } finally {
      setSaving(false);
    }
  };

  return (
    <Card className={cn("mt-4 w-full space-y-3 p-6", className)}>
      <div className="flex items-center justify-between">
        <Label className="flex items-center gap-2">
          <GitBranch className="h-4 w-4" />
          GitHub
        </Label>
        <Button variant="outline" size="sm" onClick={() => void check()} disabled={checking}>
          <RefreshCw className={`mr-2 h-3 w-3 ${checking ? "animate-spin" : ""}`} />
          {t("common.refresh")}
        </Button>
      </div>
      {status === null ? (
        <p className="text-sm text-muted-foreground">{t("settings.github.statusUnavailable")}</p>
      ) : status.connected ? (
        <div className="flex items-center justify-between rounded-md border border-border/60 px-3 py-2.5">
          <p className="text-sm">{t("settings.github.connected", { login: status.login ?? "" })}</p>
          <Button variant="outline" size="sm" onClick={() => void handleDisconnect()} disabled={saving}>
            <Unlink className="mr-2 h-3 w-3" />
            {t("settings.github.disconnect")}
          </Button>
        </div>
      ) : (
        <div className="space-y-2">
          {status.detail && <p className="text-sm text-destructive">{status.detail}</p>}
          <Input
            type="password"
            autoComplete="off"
            placeholder={t("settings.github.tokenPlaceholder")}
            value={token}
            onChange={(e) => setToken(e.target.value)}
          />
          <Button onClick={() => void handleConnect()} disabled={saving || !token.trim()}>
            <GitBranch className="mr-2 h-3.5 w-3.5" />
            {t("settings.github.connect")}
          </Button>
          <p className="text-xs text-muted-foreground">{t("settings.github.tokenHelp")}</p>
        </div>
      )}
    </Card>
  );
}
