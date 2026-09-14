import { AlertTriangle } from "lucide-react";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { tStatic } from "@/hooks/useI18n";

/**
 * Shown instead of the app when this build has no bearer token for the local
 * server. In the desktop shell the token always arrives over the bridge, so
 * this is the browser-development case: `VITE_API_KEY` must be set and must
 * match the server's `SERVER_API_KEY`.
 */
export function ConfigErrorPage({ missing }: { missing: string[] }) {
  return (
    <div className="flex min-h-screen items-center justify-center bg-background p-4">
      <Card className="w-full max-w-md">
        <CardHeader className="text-center">
          <div className="mx-auto mb-2 flex h-12 w-12 items-center justify-center rounded-xl bg-destructive text-white">
            <AlertTriangle className="h-6 w-6" />
          </div>
          <CardTitle className="text-xl">{tStatic("common.configError.title")}</CardTitle>
          <CardDescription>{tStatic("common.configError.body")}</CardDescription>
        </CardHeader>
        <CardContent>
          <p className="text-xs text-muted-foreground">{tStatic("common.configError.missing")}</p>
          <p className="mt-1 break-words font-mono text-xs text-destructive">{missing.join(", ")}</p>
        </CardContent>
      </Card>
    </div>
  );
}
