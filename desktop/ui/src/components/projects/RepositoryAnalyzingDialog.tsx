import { CheckCircle2 } from "lucide-react";
import { useEffect, useRef, useState } from "react";
import { Dialog, DialogContent, DialogDescription, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import { Progress } from "@/components/ui/progress";
import { Spinner } from "@/components/ui/spinner";
import { useI18n } from "@/hooks/useI18n";

export interface AnalyzePhase {
  flow: "import" | "open";
  /** The repo name (import) or folder path (open) currently being analyzed. */
  label: string;
}

// How long the 100%/checkmark state lingers before the dialog unmounts, so
// "done" is actually seen rather than flashing straight to the next dialog.
const HOLD_MS = 350;
const TICK_MS = 200;

function stageKey(flow: AnalyzePhase["flow"], pct: number): string {
  if (pct >= 100) return "stageDone";
  if (pct >= 90) return "stageClassify";
  if (pct >= 30) return "stageScan";
  return flow === "open" ? "stageRead" : "stageClone";
}

/**
 * Fills the gap between "user clicked import/open" and the setup dialog
 * mounting: real detection is a synchronous filesystem scan with no
 * server-reported progress, so this is a staged, ever-slowing animation that
 * asymptotes at 90% no matter how long the underlying clone takes, and only
 * reaches 100% once the caller reports the work actually finished (`phase`
 * going back to null).
 */
export function RepositoryAnalyzingDialog({ phase }: { phase: AnalyzePhase | null }) {
  const { t } = useI18n();
  const [visible, setVisible] = useState(false);
  const [pct, setPct] = useState(0);
  const phaseRef = useRef<AnalyzePhase | null>(null);
  const holdTimer = useRef<ReturnType<typeof setTimeout> | null>(null);
  const tickTimer = useRef<ReturnType<typeof setInterval> | null>(null);

  useEffect(() => {
    if (phase) {
      if (holdTimer.current) {
        clearTimeout(holdTimer.current);
        holdTimer.current = null;
      }
      if (phaseRef.current?.label !== phase.label) {
        setPct(0);
      }
      phaseRef.current = phase;
      setVisible(true);
      if (!tickTimer.current) {
        tickTimer.current = setInterval(() => {
          setPct((p) => Math.min(90, p + Math.max(0.35, (90 - p) * 0.035)));
        }, TICK_MS);
      }
    } else {
      if (tickTimer.current) {
        clearInterval(tickTimer.current);
        tickTimer.current = null;
      }
      // Hide whether or not the bar has moved. A local import can finish inside
      // the first tick, and a bar still at 0 used to skip scheduling the hide,
      // so the dialog stayed open for good. A bar that moved shows 100% first.
      setPct((p) => (p === 0 ? p : 100));
      if (holdTimer.current) clearTimeout(holdTimer.current);
      holdTimer.current = setTimeout(() => {
        setVisible(false);
        phaseRef.current = null;
      }, HOLD_MS);
    }
    return () => {
      if (tickTimer.current) {
        clearInterval(tickTimer.current);
        tickTimer.current = null;
      }
    };
    // phase is a fresh object per start/end call; content (label/flow) is what
    // this effect reacts to, read directly off it rather than added as deps.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [phase]);

  useEffect(
    () => () => {
      if (holdTimer.current) clearTimeout(holdTimer.current);
    },
    [],
  );

  const display = phase ?? phaseRef.current;
  const label = display?.label ?? "";
  const flow = display?.flow ?? "import";
  const done = pct >= 100;

  return (
    <Dialog open={visible} onOpenChange={() => {}}>
      <DialogContent
        className="sm:max-w-md"
        hideCloseButton
        onPointerDownOutside={(event) => event.preventDefault()}
        onInteractOutside={(event) => event.preventDefault()}
        onEscapeKeyDown={(event) => event.preventDefault()}
      >
        <DialogHeader>
          <DialogTitle>{t("projectAdmin.analyzing.title")}</DialogTitle>
          <DialogDescription>
            {t(
              flow === "open" ? "projectAdmin.analyzing.descriptionOpen" : "projectAdmin.analyzing.descriptionImport",
              { name: label },
            )}
          </DialogDescription>
        </DialogHeader>
        <div className="space-y-3 py-2">
          <div className="flex items-center gap-2 text-sm text-muted-foreground">
            {done ? <CheckCircle2 className="h-4 w-4 text-primary" /> : <Spinner size="sm" />}
            <span>{t(`projectAdmin.analyzing.${stageKey(flow, pct)}`)}</span>
          </div>
          <Progress value={pct} />
          <p className="text-xs text-muted-foreground">{t("projectAdmin.analyzing.note")}</p>
        </div>
      </DialogContent>
    </Dialog>
  );
}
