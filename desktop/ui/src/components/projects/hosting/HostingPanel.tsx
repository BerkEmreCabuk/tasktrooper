import { Cloud, Server, Triangle } from "lucide-react";
import { useCallback, useState } from "react";
import { GCloudHostingBody } from "@/components/projects/hosting/GCloudHostingBody";
import { VercelHostingBody } from "@/components/projects/hosting/VercelHostingBody";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { Notice } from "@/components/ui/notice";
import { useI18n } from "@/hooks/useI18n";
import { cn } from "@/lib/utils";

/** The providers a scope can be attached to. `soon` ones are named rather than
 *  hidden: "where is AWS" is a question the panel should answer by itself. */
type HostingProvider = "vercel" | "gcloud" | "aws";

const PROVIDERS: { id: HostingProvider; icon: typeof Cloud; soon?: boolean }[] = [
  { id: "vercel", icon: Triangle },
  { id: "gcloud", icon: Cloud },
  { id: "aws", icon: Server, soon: true },
];

interface HostingPanelProps {
  repositoryId: string;
  /** "" is the repository itself; a monorepo passes the sub-project's path. */
  subProjectPath?: string;
  /** What this scope is — the sub-project's kind, not the repository's. */
  scopeKind?: string;
  className?: string;
}

/**
 * HostingPanel — where one repository scope ships to, whichever provider that
 * is.
 *
 * One card, one scope, one provider at a time. The provider is a choice rather
 * than a fixed section, because a scope ships to exactly one of them and
 * stacking a card per vendor is what made this screen unreadable: the reader
 * had to scan past the providers they do not use to find the one they do.
 *
 * The scope itself belongs to the page, not to this card: the addresses below
 * it answer to the same scope, and two pickers for one question is one too
 * many.
 *
 * Both provider bodies stay mounted once opened, so switching back and forth
 * does not re-read the vendor's API, and each reports whether it is linked so
 * the buttons can carry that badge without a second request.
 */
export function HostingPanel({ repositoryId, subProjectPath = "", scopeKind = "", className }: HostingPanelProps) {
  const { t } = useI18n();
  const [provider, setProvider] = useState<HostingProvider>("vercel");
  const [linked, setLinked] = useState<Partial<Record<HostingProvider, boolean>>>({});
  // A body is mounted on first use and kept: re-reading Vercel or Google Cloud
  // on every switch is a live API call, not a cache hit.
  const [opened, setOpened] = useState<Set<HostingProvider>>(() => new Set(["vercel"]));

  // A mobile scope has no hosted runtime to point at — it publishes through a
  // store console, which is a different panel entirely. Offering Vercel or
  // Cloud Run here would be a dead end the server refuses anyway.
  const shipsThroughStore = scopeKind === "mobile";

  const openProvider = (next: HostingProvider) => {
    setProvider(next);
    setOpened((prev) => (prev.has(next) ? prev : new Set(prev).add(next)));
  };

  const markVercel = useCallback((isLinked: boolean) => {
    setLinked((prev) => (prev.vercel === isLinked ? prev : { ...prev, vercel: isLinked }));
  }, []);

  const markGCloud = useCallback((isLinked: boolean) => {
    setLinked((prev) => (prev.gcloud === isLinked ? prev : { ...prev, gcloud: isLinked }));
  }, []);

  return (
    <Card className={cn("w-full", className)}>
      <CardHeader className="gap-3">
        <div className="min-w-0">
          <CardTitle className="text-base">{t("projectAdmin.hosting.title")}</CardTitle>
          <CardDescription>{t("projectAdmin.hosting.subtitle")}</CardDescription>
        </div>

        {!shipsThroughStore && (
          <div className="flex flex-wrap gap-2">
            {PROVIDERS.map(({ id, icon: Icon, soon }) => (
              <Button
                key={id}
                type="button"
                size="sm"
                variant={provider === id ? "default" : "outline"}
                disabled={soon}
                onClick={() => openProvider(id)}
                className="gap-2"
              >
                <Icon className="h-4 w-4" aria-hidden />
                {t(`projectAdmin.hosting.providers.${id}`)}
                {soon && (
                  <Badge variant="outline" className="ml-1">
                    {t("projectAdmin.hosting.soon")}
                  </Badge>
                )}
                {linked[id] && (
                  <Badge variant="success" className="ml-1">
                    {t("projectAdmin.hosting.linked")}
                  </Badge>
                )}
              </Button>
            ))}
          </div>
        )}
      </CardHeader>

      <CardContent>
        {shipsThroughStore ? (
          <Notice variant="info" title={t("projectAdmin.prodOps.mobileStoreManagedTitle")}>
            {t("projectAdmin.prodOps.mobileStoreManagedNote")}
          </Notice>
        ) : (
          <>
            {opened.has("vercel") && (
              <div className={cn(provider !== "vercel" && "hidden")}>
                <VercelHostingBody
                  key={`vercel-${subProjectPath}`}
                  repositoryId={repositoryId}
                  subProjectPath={subProjectPath}
                  onStatusChange={markVercel}
                />
              </div>
            )}
            {opened.has("gcloud") && (
              <div className={cn(provider !== "gcloud" && "hidden")}>
                <GCloudHostingBody
                  key={`gcloud-${subProjectPath}`}
                  repositoryId={repositoryId}
                  subProjectPath={subProjectPath}
                  onStatusChange={markGCloud}
                />
              </div>
            )}
          </>
        )}
      </CardContent>
    </Card>
  );
}
