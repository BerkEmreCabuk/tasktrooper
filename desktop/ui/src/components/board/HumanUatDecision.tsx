import { useState } from "react";
import { toast } from "sonner";
import { api, type BoardTask } from "@/api";
import { Button } from "@/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogFooter,
  DialogHeader,
  DialogTitle,
  DialogDescription,
} from "@/components/ui/dialog";
import { Textarea } from "@/components/ui/textarea";
import { Label } from "@/components/ui/label";
import { useI18n } from "@/hooks/useI18n";

interface HumanUatDecisionProps {
  task: BoardTask;
  repositoryId: string;
  onUpdated: () => void;
}

// The stakeholder's decision on a card outranks every other field, but it only
// exists at all while the card is actually waiting on them — anywhere else the
// generic column select (TaskDetailDrawer) remains the only override.
export function HumanUatDecision({ task, repositoryId, onUpdated }: HumanUatDecisionProps) {
  const { t } = useI18n();
  const [saving, setSaving] = useState(false);
  const [declineOpen, setDeclineOpen] = useState(false);
  const [reason, setReason] = useState("");

  if (task.column !== "human_uat") return null;

  const approve = async () => {
    setSaving(true);
    try {
      await api.updateRepositoryTask(repositoryId, task.id, { column: "done" });
      onUpdated();
      toast.success(t("boardArea.components.taskDetail.humanUatApproved"));
    } catch (e) {
      toast.error(e instanceof Error ? e.message : t("boardArea.components.taskDetail.updateFailed"));
    } finally {
      setSaving(false);
    }
  };

  // A decline is two independently-idempotent calls, not one transaction: the
  // reason is only worth posting if it can also move the card, but a failed
  // move must never lose the reason the stakeholder already typed.
  const decline = async () => {
    const trimmed = reason.trim();
    if (!trimmed) return;
    setSaving(true);
    try {
      await api.createTaskComment(repositoryId, task.id, trimmed);
      await api.updateRepositoryTask(repositoryId, task.id, { column: "need_revision" });
      setDeclineOpen(false);
      setReason("");
      onUpdated();
      toast.success(t("boardArea.components.taskDetail.humanUatDeclined"));
    } catch (e) {
      toast.error(e instanceof Error ? e.message : t("boardArea.components.taskDetail.updateFailed"));
    } finally {
      setSaving(false);
    }
  };

  return (
    <section className="flex flex-wrap items-center gap-2 rounded-lg border border-primary/40 bg-primary/5 p-3">
      <Button size="sm" onClick={approve} disabled={saving}>
        {t("boardArea.components.taskDetail.humanUatApprove")}
      </Button>
      <Button size="sm" variant="destructive" onClick={() => setDeclineOpen(true)} disabled={saving}>
        {t("boardArea.components.taskDetail.humanUatDecline")}
      </Button>

      <Dialog open={declineOpen} onOpenChange={(open) => !saving && setDeclineOpen(open)}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>{t("boardArea.components.taskDetail.humanUatDeclineTitle")}</DialogTitle>
            <DialogDescription>
              {t("boardArea.components.taskDetail.humanUatDeclineDescription")}
            </DialogDescription>
          </DialogHeader>
          <div className="space-y-2">
            <Label htmlFor="human-uat-decline-reason">
              {t("boardArea.components.taskDetail.humanUatDeclineReasonLabel")}
            </Label>
            <Textarea
              id="human-uat-decline-reason"
              value={reason}
              onChange={(e) => setReason(e.target.value)}
              placeholder={t("boardArea.components.taskDetail.humanUatDeclinePlaceholder")}
              rows={4}
              disabled={saving}
            />
          </div>
          <DialogFooter>
            <Button variant="ghost" onClick={() => setDeclineOpen(false)} disabled={saving}>
              {t("common.cancel")}
            </Button>
            <Button variant="destructive" onClick={decline} disabled={saving || reason.trim().length === 0}>
              {t("boardArea.components.taskDetail.humanUatDeclineSubmit")}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </section>
  );
}
