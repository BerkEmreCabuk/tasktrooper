import { ChevronRight, Pencil, Trash2 } from "lucide-react";
import { Fragment } from "react";
import type { MCPServerView } from "@/api";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Switch } from "@/components/ui/switch";
import { useI18n } from "@/hooks/useI18n";
import { cn } from "@/lib/utils";

function formatToolName(fullName: string, serverId: string): string {
  const prefix = `mcp_${serverId}_`;
  if (fullName.startsWith(prefix)) return fullName.slice(prefix.length);
  return fullName;
}

function statusVariant(status: MCPServerView["status"]) {
  if (status === "connected") return "success" as const;
  if (status === "disabled") return "secondary" as const;
  return "destructive" as const;
}

interface MCPServerTableRowProps {
  server: MCPServerView;
  expanded: boolean;
  onToggleExpand: () => void;
  toggling: boolean;
  onToggleEnabled: (enabled: boolean) => void;
  onEdit: () => void;
  onDelete: () => void;
}

export function MCPServerTableRow({
  server,
  expanded,
  onToggleExpand,
  toggling,
  onToggleEnabled,
  onEdit,
  onDelete,
}: MCPServerTableRowProps) {
  const { t } = useI18n();
  const tools = server.tools ?? [];
  const canExpand = tools.length > 0;

  const statusLabel = (status: MCPServerView["status"]) => {
    if (status === "connected") return t("frame.admin.mcpRow.connected");
    if (status === "disabled") return t("frame.admin.mcpRow.disabled");
    return t("frame.admin.mcpRow.error");
  };

  return (
    <Fragment>
      <tr
        className={cn(
          "border-b border-border hover:bg-muted/30",
          expanded && "border-b-0 bg-muted/20",
        )}
      >
        <td className="px-4 py-3">
          <div className="font-medium">{server.id}</div>
        </td>
        <td className="px-4 py-3">
          <Badge variant="outline">{server.transport}</Badge>
        </td>
        <td className="px-4 py-3">
          <div className="flex max-w-xs flex-col gap-1">
            <Badge variant={statusVariant(server.status)}>{statusLabel(server.status)}</Badge>
            {server.status === "error" && (
              <p
                className="text-xs leading-snug text-destructive"
                title={server.last_error || t("frame.admin.mcpRow.unknownError")}
              >
                {server.last_error || t("frame.admin.mcpRow.connectFailed")}
              </p>
            )}
          </div>
        </td>
        <td className="px-4 py-3">
          {canExpand ? (
            <button
              type="button"
              onClick={onToggleExpand}
              className="flex items-center gap-1.5 text-left text-muted-foreground hover:text-foreground"
            >
              <ChevronRight
                className={cn("h-3.5 w-3.5 shrink-0 transition-transform", expanded && "rotate-90")}
              />
              <span>{server.tool_count}</span>
            </button>
          ) : (
            <span className="text-muted-foreground">{server.tool_count}</span>
          )}
        </td>
        <td className="px-4 py-3">
          <Switch checked={server.enabled} disabled={toggling} onCheckedChange={onToggleEnabled} />
        </td>
        <td className="px-4 py-3">
          <div className="flex gap-1">
            <Button variant="ghost" size="icon" onClick={onEdit}>
              <Pencil className="h-4 w-4" />
            </Button>
            <Button variant="ghost" size="icon" onClick={onDelete}>
              <Trash2 className="h-4 w-4 text-destructive" />
            </Button>
          </div>
        </td>
      </tr>
      {expanded && (
        <tr className="border-b border-border bg-muted/10">
          <td colSpan={6} className="px-4 py-3">
            <div className="text-xs font-medium text-muted-foreground">
              {t("frame.admin.mcpRow.tools", { count: tools.length })}
            </div>
            <div className="mt-2 flex flex-wrap gap-2">
              {tools.map((tool) => (
                <Badge key={tool} variant="secondary" className="font-mono text-[11px] font-normal" title={tool}>
                  {formatToolName(tool, server.id)}
                </Badge>
              ))}
            </div>
          </td>
        </tr>
      )}
    </Fragment>
  );
}
