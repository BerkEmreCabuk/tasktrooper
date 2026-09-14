import { useEffect, useState } from "react";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { useI18n } from "@/hooks/useI18n";

interface ConfirmDialogProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  title: string;
  description: string;
  confirmLabel?: string;
  cancelLabel?: string;
  variant?: "default" | "destructive";
  loading?: boolean;
  onConfirm: () => void | Promise<void>;
  /**
   * When set, the user must type this exact string before confirming.
   * Absent, the dialog behaves exactly as before — this is the only brake
   * on production actions, since there is no role system to lean on.
   */
  confirmPhrase?: string;
}

export function ConfirmDialog({
  open,
  onOpenChange,
  title,
  description,
  confirmLabel,
  cancelLabel,
  variant = "destructive",
  loading = false,
  onConfirm,
  confirmPhrase,
}: ConfirmDialogProps) {
  const { t } = useI18n();
  const [typed, setTyped] = useState("");
  const phraseRequired = Boolean(confirmPhrase);
  const phraseOK = !phraseRequired || typed === confirmPhrase;

  // Reset between openings so a previously satisfied phrase never carries
  // over into the next, different confirmation.
  useEffect(() => {
    if (!open) setTyped("");
  }, [open]);

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>{title}</DialogTitle>
          <DialogDescription>{description}</DialogDescription>
        </DialogHeader>
        {phraseRequired && (
          <div className="space-y-2">
            <Label htmlFor="confirm-phrase">
              {t("frame.ui.confirm.typeToConfirm", { phrase: confirmPhrase! })}
            </Label>
            <Input
              id="confirm-phrase"
              value={typed}
              onChange={(e) => setTyped(e.target.value)}
              autoComplete="off"
              placeholder={confirmPhrase}
            />
          </div>
        )}
        <DialogFooter>
          <Button variant="outline" onClick={() => onOpenChange(false)} disabled={loading}>
            {cancelLabel ?? t("common.cancel")}
          </Button>
          <Button
            variant={variant}
            disabled={loading || !phraseOK}
            onClick={async () => {
              await onConfirm();
              onOpenChange(false);
            }}
          >
            {loading ? t("frame.ui.confirm.processing") : confirmLabel ?? t("frame.ui.confirm.confirm")}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
