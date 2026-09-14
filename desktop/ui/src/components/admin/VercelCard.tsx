import { ExternalLink, Link2, RefreshCw, Triangle, Unlink } from "lucide-react";
import { useCallback, useEffect, useRef, useState } from "react";
import { toast } from "sonner";
import {
  api,
  type DeployProvider,
  type HostingAreaDetection,
  type HostingDetection,
  type Repository,
  type VercelConnectionStatus,
  type VercelProject,
  type VercelTeam,
} from "@/api";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card } from "@/components/ui/card";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Notice } from "@/components/ui/notice";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Skeleton } from "@/components/ui/skeleton";
import { useI18n } from "@/hooks/useI18n";
import { DEPLOY_PROVIDERS } from "@/lib/deployTargets";

// Radix Select cannot hold "" as an item value, and both "personal account"
// (team_id "") and "nothing chosen yet" need one.
const PERSONAL = "__personal__";
const NONE = "__none__";

function projectLabel(p: VercelProject): string {
  const parts = [p.name];
  if (p.root_directory) parts.push(`· ${p.root_directory}`);
  if (p.framework) parts.push(`· ${p.framework}`);
  return parts.join(" ");
}

interface AreaCardProps {
  repositoryId: string;
  area: HostingAreaDetection;
  vercelConnected: boolean;
  /** null = not fetched yet; the chooser asks for it via onNeedProjects. */
  projects: VercelProject[] | null;
  loadingProjects: boolean;
  onNeedProjects: () => void;
  onChanged: () => void;
}

/**
 * AreaCard is one area's answer: what is recorded, what the tree says, and —
 * when nothing is recorded — either the detected project with a one-click
 * confirm, or a chooser when detection could not decide. The chooser always
 * offers "it lives elsewhere" too: an area that is not on Vercel should be
 * recorded as such, so it is not asked about on every visit.
 */
function AreaCard({
  repositoryId,
  area,
  vercelConnected,
  projects,
  loadingProjects,
  onNeedProjects,
  onChanged,
}: AreaCardProps) {
  const { t } = useI18n();
  const candidates = area.candidates ?? [];
  const hints = area.hints ?? [];
  const top = candidates[0];
  const exact = area.confidence === "exact" && top !== undefined;
  const [choice, setChoice] = useState<string>("");
  const [elsewhere, setElsewhere] = useState<DeployProvider | "">("");
  const [busy, setBusy] = useState(false);

  // A re-detect rebuilds the area object; drafts belong to the old answer.
  useEffect(() => {
    setChoice("");
    setElsewhere("");
  }, [area.area, area.confidence, top?.project.id, area.existing?.id]);

  const needsChooser = vercelConnected && !exact && !area.existing;
  useEffect(() => {
    if (needsChooser && projects === null && !loadingProjects) onNeedProjects();
  }, [needsChooser, projects, loadingProjects, onNeedProjects]);

  const link = useCallback(
    async (projectId: string, source: "detected" | "user", evidence: string) => {
      setBusy(true);
      try {
        const saved = await api.saveHostingLink(repositoryId, {
          area: area.area || "root",
          provider: "vercel",
          external_id: projectId,
          source,
          evidence,
        });
        toast.success(t("settings.vercel.apps.linkSaved", { name: saved.external_name ?? "" }));
        onChanged();
      } catch (err) {
        toast.error(err instanceof Error ? err.message : t("common.actionFailed"));
      } finally {
        setBusy(false);
      }
    },
    [area.area, onChanged, repositoryId, t],
  );

  const record = useCallback(async () => {
    if (!elsewhere) return;
    setBusy(true);
    try {
      await api.saveHostingLink(repositoryId, { area: area.area || "root", provider: elsewhere, source: "user" });
      toast.success(t("settings.vercel.apps.recorded"));
      onChanged();
    } catch (err) {
      toast.error(err instanceof Error ? err.message : t("common.actionFailed"));
    } finally {
      setBusy(false);
    }
  }, [area.area, elsewhere, onChanged, repositoryId, t]);

  const unlink = useCallback(async () => {
    setBusy(true);
    try {
      await api.deleteHostingLink(repositoryId, area.area);
      toast.success(t("settings.vercel.apps.unlinked"));
      onChanged();
    } catch (err) {
      toast.error(err instanceof Error ? err.message : t("common.actionFailed"));
    } finally {
      setBusy(false);
    }
  }, [area.area, onChanged, repositoryId, t]);

  const candidateIds = new Set(candidates.map((c) => c.project.id));
  const rest = (projects ?? []).filter((p) => !candidateIds.has(p.id));
  const existing = area.existing;

  return (
    <div className="space-y-3 rounded-md border border-border/60 p-4">
      <div className="flex flex-wrap items-center gap-2">
        <h3 className="text-sm font-medium">{t(`settings.vercel.apps.area.${area.area || "root"}`)}</h3>
        <Badge variant="outline" className="font-mono text-[11px]">
          {area.kind}
        </Badge>
        {area.directory && (
          <span className="font-mono text-xs text-muted-foreground">
            {t("settings.vercel.apps.directory")}: {area.directory}
          </span>
        )}
      </div>

      {existing && (
        <div className="flex flex-wrap items-center justify-between gap-2 rounded-md border border-border/60 px-3 py-2.5">
          <div className="min-w-0 space-y-0.5 text-sm">
            <p className="flex items-center gap-2">
              <Link2 className="h-3.5 w-3.5 shrink-0" />
              {existing.provider === "vercel"
                ? t("settings.vercel.apps.linked", { name: existing.external_name ?? existing.external_id ?? "" })
                : t("settings.vercel.apps.linkedProvider", {
                    provider: t(`projectAdmin.prodOps.providers.${existing.provider}`),
                  })}
              <span className="text-xs text-muted-foreground">
                · {t(`settings.vercel.apps.linkedBy.${existing.source}`)}
              </span>
            </p>
            {existing.production_url && (
              <a
                href={existing.production_url}
                target="_blank"
                rel="noreferrer"
                className="flex items-center gap-1 font-mono text-xs text-muted-foreground underline"
              >
                {existing.production_url}
                <ExternalLink className="h-3 w-3" />
              </a>
            )}
          </div>
          <Button variant="outline" size="sm" onClick={() => void unlink()} disabled={busy}>
            <Unlink className="mr-2 h-3 w-3" />
            {t("settings.vercel.apps.unlink")}
          </Button>
        </div>
      )}

      {hints.length > 0 && (
        <div className="space-y-1">
          <p className="text-xs font-medium text-muted-foreground">{t("settings.vercel.apps.hints")}</p>
          <ul className="space-y-0.5 text-xs text-muted-foreground">
            {hints.map((h) => (
              <li key={`${h.provider}-${h.evidence ?? h.name}`} className="flex flex-wrap gap-1">
                <span className="text-foreground">{h.name}</span>
                {h.detail && <span>— {h.detail}</span>}
                {h.evidence && <code className="font-mono">({h.evidence})</code>}
              </li>
            ))}
          </ul>
        </div>
      )}

      {!existing && !vercelConnected && (
        <p className="text-xs text-muted-foreground">{t("settings.vercel.apps.notConnected")}</p>
      )}

      {!existing && vercelConnected && exact && top && (
        <div className="space-y-2">
          <p className="text-sm">{t("settings.vercel.apps.detected", { name: projectLabel(top.project) })}</p>
          <p className="text-xs text-muted-foreground">
            {t("settings.vercel.apps.detectedHelp", { reason: t(`settings.vercel.apps.reasons.${top.reason}`) })}
          </p>
          <Button
            size="sm"
            disabled={busy}
            onClick={() => void link(top.project.id, "detected", t(`settings.vercel.apps.reasons.${top.reason}`))}
          >
            <Link2 className="mr-2 h-3.5 w-3.5" />
            {t("settings.vercel.apps.confirm")}
          </Button>
        </div>
      )}

      {!existing && vercelConnected && !exact && (
        <div className="space-y-3">
          <p className="text-sm text-muted-foreground">
            {area.confidence === "ambiguous" ? t("settings.vercel.apps.ambiguous") : t("settings.vercel.apps.none")}
          </p>
          <div className="grid gap-3 sm:grid-cols-[1fr_auto]">
            <div className="space-y-1">
              <Label htmlFor={`project-${area.area || "root"}`}>{t("settings.vercel.apps.pickProject")}</Label>
              <Select
                value={choice || NONE}
                onValueChange={(v) => {
                  setChoice(v === NONE ? "" : v);
                  if (v !== NONE) setElsewhere("");
                }}
              >
                <SelectTrigger id={`project-${area.area || "root"}`}>
                  <SelectValue placeholder={t("settings.vercel.apps.pickProjectPlaceholder")} />
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value={NONE}>{t("settings.vercel.apps.pickProjectPlaceholder")}</SelectItem>
                  {candidates.map((c) => (
                    <SelectItem key={c.project.id} value={c.project.id}>
                      {projectLabel(c.project)}
                      <span className="ml-1.5 text-muted-foreground">
                        — {t(`settings.vercel.apps.reasons.${c.reason}`)}
                      </span>
                    </SelectItem>
                  ))}
                  {rest.map((p) => (
                    <SelectItem key={p.id} value={p.id}>
                      {projectLabel(p)}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
              {loadingProjects && (
                <p className="text-xs text-muted-foreground">{t("settings.vercel.apps.loadingProjects")}</p>
              )}
              {projects !== null && projects.length === 0 && candidates.length === 0 && (
                <p className="text-xs text-muted-foreground">{t("settings.vercel.apps.noProjects")}</p>
              )}
            </div>
            <div className="flex items-end">
              <Button size="sm" disabled={!choice || busy} onClick={() => void link(choice, "user", "")}>
                <Link2 className="mr-2 h-3.5 w-3.5" />
                {t("settings.vercel.apps.confirm")}
              </Button>
            </div>
          </div>
          <div className="grid gap-3 sm:grid-cols-[1fr_auto]">
            <div className="space-y-1">
              <Label htmlFor={`elsewhere-${area.area || "root"}`}>{t("settings.vercel.apps.elsewhere")}</Label>
              <Select
                value={elsewhere || NONE}
                onValueChange={(v) => {
                  setElsewhere(v === NONE ? "" : (v as DeployProvider));
                  if (v !== NONE) setChoice("");
                }}
              >
                <SelectTrigger id={`elsewhere-${area.area || "root"}`}>
                  <SelectValue placeholder={t("settings.vercel.apps.elsewherePlaceholder")} />
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value={NONE}>{t("settings.vercel.apps.elsewherePlaceholder")}</SelectItem>
                  {DEPLOY_PROVIDERS.filter((p) => p !== "vercel").map((p) => (
                    <SelectItem key={p} value={p}>
                      {t(`projectAdmin.prodOps.providers.${p}`)}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </div>
            <div className="flex items-end">
              <Button size="sm" variant="outline" disabled={!elsewhere || busy} onClick={() => void record()}>
                {t("settings.vercel.apps.record")}
              </Button>
            </div>
          </div>
        </div>
      )}
    </div>
  );
}

/**
 * AppLinksPanel — pick an app, see where each of its web areas ships, link
 * what detection found or choose by hand.
 */
function AppLinksPanel({ status }: { status: VercelConnectionStatus }) {
  const { t } = useI18n();
  const [repos, setRepos] = useState<Repository[] | null>(null);
  const [repoId, setRepoId] = useState("");
  const [detection, setDetection] = useState<HostingDetection | null>(null);
  const [detecting, setDetecting] = useState(false);
  const [projects, setProjects] = useState<VercelProject[] | null>(null);
  const [loadingProjects, setLoadingProjects] = useState(false);
  // Several AreaCards mount at once and each asks for the list in the same
  // tick, before the loading state has re-rendered; the ref is what makes
  // that one request rather than one per area.
  const loadingRef = useRef(false);

  useEffect(() => {
    api
      .listRepositories()
      .then((r) => setRepos(r.repositories ?? []))
      .catch(() => setRepos([]));
  }, []);

  const detect = useCallback(
    async (id: string) => {
      if (!id) {
        setDetection(null);
        return;
      }
      setDetecting(true);
      try {
        setDetection(await api.detectHosting(id));
      } catch (err) {
        setDetection(null);
        toast.error(err instanceof Error ? err.message : t("settings.vercel.apps.detectFailed"));
      } finally {
        setDetecting(false);
      }
    },
    [t],
  );

  useEffect(() => {
    void detect(repoId);
  }, [repoId, detect]);

  // The project list is per scope: a team switch invalidates it.
  useEffect(() => {
    setProjects(null);
    loadingRef.current = false;
  }, [status.team_id]);

  const loadProjects = useCallback(async () => {
    if (loadingRef.current) return;
    loadingRef.current = true;
    setLoadingProjects(true);
    try {
      const r = await api.vercelProjects();
      setProjects(r.projects ?? []);
    } catch (err) {
      setProjects([]);
      toast.error(err instanceof Error ? err.message : t("common.actionFailed"));
    } finally {
      setLoadingProjects(false);
    }
  }, [t]);

  const areas = detection?.areas ?? [];
  const warnings = detection?.warnings ?? [];

  return (
    <div className="space-y-3 border-t border-border/60 pt-4">
      <div>
        <h3 className="text-sm font-semibold">{t("settings.vercel.apps.title")}</h3>
        <p className="text-xs text-muted-foreground">{t("settings.vercel.apps.subtitle")}</p>
      </div>

      {repos === null ? (
        <Skeleton className="h-10 w-full max-w-md" />
      ) : repos.length === 0 ? (
        <p className="text-sm text-muted-foreground">{t("settings.vercel.apps.noApps")}</p>
      ) : (
        <div className="flex flex-wrap items-center gap-2">
          <Select value={repoId || NONE} onValueChange={(v) => setRepoId(v === NONE ? "" : v)}>
            <SelectTrigger className="max-w-md">
              <SelectValue placeholder={t("settings.vercel.apps.selectApp")} />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value={NONE}>{t("settings.vercel.apps.selectApp")}</SelectItem>
              {repos.map((r) => (
                <SelectItem key={r.id} value={r.id}>
                  {r.name}
                  {r.kind && <span className="ml-1.5 text-muted-foreground">· {r.kind}</span>}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
          {repoId && (
            <Button variant="outline" size="sm" onClick={() => void detect(repoId)} disabled={detecting}>
              <RefreshCw className={`mr-2 h-3 w-3 ${detecting ? "animate-spin" : ""}`} />
              {t("common.refresh")}
            </Button>
          )}
        </div>
      )}

      {repoId && detecting && !detection && (
        <p className="text-sm text-muted-foreground">{t("settings.vercel.apps.detecting")}</p>
      )}

      {repoId && detection && (
        <div className="space-y-3">
          {areas.length === 0 && <Notice variant="info" title={t("settings.vercel.apps.noAreas")} />}
          {areas.map((area) => (
            <AreaCard
              key={area.area || "root"}
              repositoryId={repoId}
              area={area}
              vercelConnected={detection.vercel_connected}
              projects={projects}
              loadingProjects={loadingProjects}
              onNeedProjects={() => void loadProjects()}
              onChanged={() => void detect(repoId)}
            />
          ))}
          {warnings.length > 0 && (
            <Notice variant="info" title={t("settings.vercel.apps.warnings")}>
              <ul className="list-disc space-y-0.5 pl-4 text-xs">
                {warnings.map((w) => (
                  <li key={w}>{w}</li>
                ))}
              </ul>
            </Notice>
          )}
        </div>
      )}
    </div>
  );
}

/**
 * VercelCard — the Vercel connection (a pasted access token, verified and
 * stored encrypted server-side) and, once connected, the per-app hosting
 * links. Mounted on the general settings page beside GitHub.
 */
export function VercelCard() {
  const { t } = useI18n();
  const [status, setStatus] = useState<VercelConnectionStatus | null>(null);
  const [checking, setChecking] = useState(false);
  const [token, setToken] = useState("");
  const [connecting, setConnecting] = useState(false);
  const [saving, setSaving] = useState(false);
  const [teams, setTeams] = useState<VercelTeam[]>([]);

  const check = useCallback(async () => {
    setChecking(true);
    try {
      setStatus(await api.vercelStatus());
    } catch {
      setStatus(null);
    } finally {
      setChecking(false);
    }
  }, []);

  useEffect(() => {
    void check();
  }, [check]);

  useEffect(() => {
    if (!status?.connected) {
      setTeams([]);
      return;
    }
    api
      .vercelTeams()
      .then((r) => setTeams(r.teams ?? []))
      .catch(() => setTeams([]));
  }, [status?.connected]);

  const connect = async () => {
    setConnecting(true);
    try {
      setStatus(await api.connectVercel({ token: token.trim() }));
      setToken("");
      toast.success(t("settings.vercel.connectedToast"));
    } catch (err) {
      toast.error(err instanceof Error ? err.message : t("settings.vercel.connectFailedToast"));
    } finally {
      setConnecting(false);
    }
  };

  const setTeam = async (value: string) => {
    setSaving(true);
    try {
      setStatus(await api.connectVercel({ team_id: value === PERSONAL ? "" : value }));
      toast.success(t("settings.vercel.teamSaved"));
    } catch (err) {
      toast.error(err instanceof Error ? err.message : t("common.actionFailed"));
    } finally {
      setSaving(false);
    }
  };

  const disconnect = async () => {
    setSaving(true);
    try {
      setStatus(await api.disconnectVercel());
      toast.success(t("settings.vercel.disconnectedToast"));
    } catch (err) {
      toast.error(err instanceof Error ? err.message : t("common.actionFailed"));
    } finally {
      setSaving(false);
    }
  };

  return (
    <Card className="mt-4 w-full space-y-3 p-6">
      <div className="flex items-center justify-between">
        <Label className="flex items-center gap-2">
          <Triangle className="h-4 w-4" />
          Vercel
        </Label>
        <Button variant="outline" size="sm" onClick={() => void check()} disabled={checking}>
          <RefreshCw className={`mr-2 h-3 w-3 ${checking ? "animate-spin" : ""}`} />
          {t("common.refresh")}
        </Button>
      </div>

      {status === null ? (
        <p className="text-sm text-muted-foreground">{t("settings.vercel.statusUnavailable")}</p>
      ) : status.connected ? (
        <div className="space-y-4">
          <div className="flex flex-wrap items-center justify-between gap-2 rounded-md border border-border/60 px-3 py-2.5">
            <p className="text-sm">
              {t("settings.vercel.connected", { login: status.username ?? "" })}
              {status.email && <span className="ml-1 text-muted-foreground">({status.email})</span>}
            </p>
            <Button variant="outline" size="sm" onClick={() => void disconnect()} disabled={saving}>
              <Unlink className="mr-2 h-3 w-3" />
              {t("settings.vercel.disconnect")}
            </Button>
          </div>

          <div className="max-w-sm space-y-1">
            <Label htmlFor="vercel-team">{t("settings.vercel.team")}</Label>
            <Select value={status.team_id || PERSONAL} onValueChange={(v) => void setTeam(v)} disabled={saving}>
              <SelectTrigger id="vercel-team">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value={PERSONAL}>{t("settings.vercel.personalAccount")}</SelectItem>
                {/* A stored team the token no longer lists stays selectable
                    so opening the page does not silently rewrite it. */}
                {status.team_id && !teams.some((tm) => tm.id === status.team_id) && (
                  <SelectItem value={status.team_id}>{status.team_slug || status.team_id}</SelectItem>
                )}
                {teams.map((tm) => (
                  <SelectItem key={tm.id} value={tm.id}>
                    {tm.name}
                    <span className="ml-1.5 text-muted-foreground">· {tm.slug}</span>
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          </div>

          <AppLinksPanel status={status} />
        </div>
      ) : (
        <div className="space-y-3">
          {status.detail && <p className="text-sm text-destructive">{status.detail}</p>}
          <Label htmlFor="vercel-token">{t("settings.vercel.tokenLabel")}</Label>
          <div className="flex flex-wrap items-center gap-2">
            <Input
              id="vercel-token"
              type="password"
              autoComplete="off"
              value={token}
              onChange={(e) => setToken(e.target.value)}
              placeholder={t("settings.vercel.tokenPlaceholder")}
              className="max-w-md"
            />
            <Button onClick={() => void connect()} disabled={connecting || !token.trim()}>
              <Triangle className="mr-2 h-3.5 w-3.5" />
              {t("settings.vercel.connect")}
            </Button>
          </div>
          <p className="text-xs text-muted-foreground">{t("settings.vercel.tokenHelp")}</p>
        </div>
      )}
    </Card>
  );
}
