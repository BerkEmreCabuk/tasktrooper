import { ArrowUp, Folder, FolderCheck } from "lucide-react";
import { useEffect, useState } from "react";
import { toast } from "sonner";
import { api, type RepositoryDirectoryListing, type SubRepoKind } from "@/api";
import { Button } from "@/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { ScrollArea } from "@/components/ui/scroll-area";
import { Spinner } from "@/components/ui/spinner";
import { useI18n } from "@/hooks/useI18n";

interface DirectoryPickerDialogProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  repositoryId: string;
  /** Paths already added as a sub-project — offered but disabled, not hidden,
   * so the picker still shows where they live in the tree. */
  excludePaths: string[];
  onSelect: (path: string, kind: SubRepoKind) => void;
}

/** Browses a repository's real directory tree one level at a time, backing
 * "add a sub-project manually" for a monorepo whose detection missed one. */
export function DirectoryPickerDialog({
  open,
  onOpenChange,
  repositoryId,
  excludePaths,
  onSelect,
}: DirectoryPickerDialogProps) {
  const { t } = useI18n();
  const [currentPath, setCurrentPath] = useState("");
  const [listing, setListing] = useState<RepositoryDirectoryListing | null>(null);
  const [loading, setLoading] = useState(false);

  useEffect(() => {
    if (!open) return;
    setCurrentPath("");
  }, [open]);

  useEffect(() => {
    if (!open) return;
    let cancelled = false;
    setLoading(true);
    api
      .listRepositoryDirectories(repositoryId, currentPath || undefined)
      .then((d) => {
        if (!cancelled) setListing(d);
      })
      .catch((e) => {
        if (!cancelled) {
          toast.error(e instanceof Error ? e.message : t("projectAdmin.initialSetup.subProjectPickerLoadFailed"));
          onOpenChange(false);
        }
      })
      .finally(() => {
        if (!cancelled) setLoading(false);
      });
    return () => {
      cancelled = true;
    };
  }, [open, repositoryId, currentPath, t, onOpenChange]);

  const selectedPath = currentPath === "" ? "." : currentPath;
  const alreadyAdded = excludePaths.includes(selectedPath);

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="sm:max-w-md">
        <DialogHeader>
          <DialogTitle>{t("projectAdmin.initialSetup.subProjectPickerTitle")}</DialogTitle>
          <DialogDescription>
            {currentPath === "" ? t("projectAdmin.initialSetup.subProjectRoot") : currentPath}
          </DialogDescription>
        </DialogHeader>
        <div className="space-y-2">
          <Button
            variant="outline"
            size="sm"
            disabled={listing?.parent === null || listing?.parent === undefined || loading}
            onClick={() => setCurrentPath(listing?.parent ?? "")}
          >
            <ArrowUp className="mr-2 h-4 w-4" />
            {t("projectAdmin.initialSetup.subProjectPickerUp")}
          </Button>
          <ScrollArea className="h-64 rounded-md border">
            {loading ? (
              <div className="flex h-full items-center justify-center p-8">
                <Spinner size="sm" />
              </div>
            ) : !listing || listing.entries.length === 0 ? (
              <p className="p-4 text-sm text-muted-foreground">
                {t("projectAdmin.initialSetup.subProjectPickerEmpty")}
              </p>
            ) : (
              <div className="divide-y divide-border/60">
                {listing.entries.map((entry) => (
                  <button
                    key={entry.path}
                    type="button"
                    className="flex w-full items-center gap-2 px-3 py-2 text-left text-sm hover:bg-muted/40"
                    onClick={() => setCurrentPath(entry.path)}
                  >
                    <Folder className="h-4 w-4 shrink-0 text-muted-foreground" />
                    <span className="truncate">{entry.name}</span>
                  </button>
                ))}
              </div>
            )}
          </ScrollArea>
        </div>
        <DialogFooter>
          <Button variant="outline" onClick={() => onOpenChange(false)}>
            {t("common.cancel")}
          </Button>
          <Button
            disabled={loading || alreadyAdded}
            onClick={() => {
              onSelect(selectedPath, listing?.kind ?? "backend");
              onOpenChange(false);
            }}
          >
            <FolderCheck className="mr-2 h-4 w-4" />
            {t("projectAdmin.initialSetup.subProjectPickerSelect")}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
