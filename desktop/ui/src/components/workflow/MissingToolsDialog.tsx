import { Button } from "@/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { useI18n } from "@/hooks/useI18n";

interface MissingToolsDialogProps {
  open: boolean;
  /** Keyed by agent_id, one entry per agent whose tool policy is short. */
  missingTools: Record<string, string[]> | null;
  labelFor: (agentId: string) => string;
  saving: boolean;
  onCancel: () => void;
  onConfirm: () => void;
}

/**
 * The 422 missing-tools confirm flow, generalized from the old
 * AnalizAssignmentSettingsPage dialog: a role/agent assignment that would
 * leave an agent short of `required_tools` comes back 422 instead of being
 * silently granted, and this dialog is the "grant them and save anyway" step.
 * Shared by RolesSettingsPage (role → agents) and AgentRolesSection (agent →
 * roles) — same response shape on both routes.
 */
export function MissingToolsDialog({
  open,
  missingTools,
  labelFor,
  saving,
  onCancel,
  onConfirm,
}: MissingToolsDialogProps) {
  const { t } = useI18n();
  return (
    <Dialog open={open} onOpenChange={(next) => !next && onCancel()}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>{t("settingsPages.roles.missingToolsTitle")}</DialogTitle>
        </DialogHeader>
        <div className="space-y-3 text-sm">
          <p className="text-muted-foreground">{t("settingsPages.roles.missingToolsBody")}</p>
          {missingTools &&
            Object.entries(missingTools).map(([agentId, tools]) => (
              <div key={agentId} className="rounded-lg border border-border p-3">
                <div className="text-xs font-medium uppercase tracking-wide text-muted-foreground">
                  {labelFor(agentId)}
                </div>
                <div className="mt-1 flex flex-wrap gap-1.5">
                  {tools.map((tool) => (
                    <code key={tool} className="rounded bg-muted px-1.5 py-0.5 text-xs">
                      {tool}
                    </code>
                  ))}
                </div>
              </div>
            ))}
        </div>
        <DialogFooter>
          <Button variant="outline" onClick={onCancel}>
            {t("common.cancel")}
          </Button>
          <Button onClick={onConfirm} disabled={saving}>
            {t("settingsPages.roles.grantTools")}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
