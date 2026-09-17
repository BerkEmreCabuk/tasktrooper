import { useEffect, useState } from "react";
import { useNavigate } from "react-router-dom";
import { LayoutTemplate, Plus } from "lucide-react";
import { toast } from "sonner";
import { api, type AgentTemplate } from "@/api";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card } from "@/components/ui/card";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { useI18n } from "@/hooks/useI18n";

interface NewAgentDialogProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  onCreated?: () => void;
}

export function NewAgentDialog({ open, onOpenChange, onCreated }: NewAgentDialogProps) {
  const { t } = useI18n();
  const navigate = useNavigate();
  const [templates, setTemplates] = useState<AgentTemplate[]>([]);
  const [creatingFromId, setCreatingFromId] = useState<string | null>(null);

  useEffect(() => {
    if (!open) return;
    api
      .listAgentTemplates()
      .then((d) => setTemplates(d.templates ?? []))
      .catch(() => setTemplates([]));
  }, [open]);

  const createBlank = () => {
    onOpenChange(false);
    navigate("/agents/new/settings");
  };

  const createFromTemplate = async (tpl: AgentTemplate) => {
    setCreatingFromId(tpl.id);
    try {
      const agent = await api.createAgentFromTemplate(tpl.id);
      toast.success(t("chatArea.workspace.newAgent.createdFromTemplate", { name: agent.name }));
      onOpenChange(false);
      onCreated?.();
      navigate(`/agents/${agent.id}/settings`);
    } catch (e) {
      toast.error(e instanceof Error ? e.message : t("chatArea.workspace.newAgent.createFailed"));
    } finally {
      setCreatingFromId(null);
    }
  };

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="max-h-[80vh] overflow-y-auto sm:max-w-lg">
        <DialogHeader>
          <DialogTitle>{t("chatArea.workspace.newAgent.title")}</DialogTitle>
          <DialogDescription>
            {t("chatArea.workspace.newAgent.description")}
          </DialogDescription>
        </DialogHeader>

        <Button onClick={createBlank} className="w-full justify-start gap-2">
          <Plus className="h-4 w-4" />
          {t("chatArea.workspace.newAgent.createBlank")}
        </Button>

        <div className="flex items-center gap-2 pt-2 text-xs font-medium tracking-wide text-muted-foreground uppercase">
          <LayoutTemplate className="h-3.5 w-3.5" />
          {t("chatArea.workspace.newAgent.fromTemplate")}
        </div>

        {templates.length === 0 ? (
          <p className="py-2 text-sm text-muted-foreground">{t("chatArea.workspace.newAgent.noTemplates")}</p>
        ) : (
          <div className="space-y-2">
            {templates.map((tpl) => (
              <Card key={tpl.id} className="flex items-center justify-between gap-3 p-3">
                <div className="min-w-0">
                  <div className="flex items-center gap-2">
                    <span className="truncate text-sm font-medium">{tpl.name}</span>
                    {tpl.built_in && (
                      <Badge variant="secondary" className="text-micro">
                        {t("chatArea.workspace.newAgent.builtIn")}
                      </Badge>
                    )}
                  </div>
                  <p className="mt-0.5 line-clamp-2 text-xs text-muted-foreground">{tpl.description}</p>
                  <p className="mt-0.5 text-micro text-muted-foreground">
                    {t("chatArea.workspace.newAgent.skillsRules", { skills: tpl.skills.length, rules: tpl.rules.length })}
                  </p>
                </div>
                <Button
                  size="sm"
                  disabled={creatingFromId !== null}
                  onClick={() => createFromTemplate(tpl)}
                >
                  {creatingFromId === tpl.id ? t("chatArea.workspace.newAgent.creating") : t("chatArea.workspace.newAgent.create")}
                </Button>
              </Card>
            ))}
          </div>
        )}
      </DialogContent>
    </Dialog>
  );
}
