import { type ReactNode } from "react";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { cn } from "@/lib/utils";

interface FormDialogProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  title: string;
  description?: string;
  children: ReactNode;
  footer: ReactNode;
  className?: string;
  /**
   * False for a dialog that must be finished, not dismissed — the initial
   * repository setup questions, which are no longer skippable by accident.
   * Defaults to true: outside clicks were already blocked for every caller
   * (see the two `preventDefault`s below, unconditional before this prop
   * existed), so the only things this turns off are Escape and the "X".
   */
  dismissable?: boolean;
}

export function FormDialog({
  open,
  onOpenChange,
  title,
  description,
  children,
  footer,
  className,
  dismissable = true,
}: FormDialogProps) {
  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent
        className={cn("sm:max-w-lg", className)}
        onPointerDownOutside={(event) => event.preventDefault()}
        onInteractOutside={(event) => event.preventDefault()}
        {...(!dismissable
          ? { onEscapeKeyDown: (event: KeyboardEvent) => event.preventDefault(), hideCloseButton: true }
          : {})}
      >
        <DialogHeader>
          <DialogTitle>{title}</DialogTitle>
          {description && <DialogDescription>{description}</DialogDescription>}
        </DialogHeader>
        <div className="max-h-[calc(90vh-10rem)] overflow-y-auto">
          <div className="space-y-4 py-2">{children}</div>
        </div>
        <DialogFooter>{footer}</DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
