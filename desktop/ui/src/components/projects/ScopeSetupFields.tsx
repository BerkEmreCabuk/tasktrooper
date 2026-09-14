import { Sparkles } from "lucide-react";
import {
  type DeployEnv,
  type RepoDocKind,
  type RepositoryDocs,
  type SaveDeployTargetInput,
  type SubRepoKind,
} from "@/api";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Notice } from "@/components/ui/notice";
import { useI18n } from "@/hooks/useI18n";

export const MOBILE_PLATFORMS = ["ios", "android", "cross_platform"] as const;
export type SelectablePlatform = (typeof MOBILE_PLATFORMS)[number];

/** What a mobile scope ships to when nothing was ever chosen — the old pair of
 * checkboxes started with both ticked, so this keeps that answer. */
export const DEFAULT_PLATFORM: SelectablePlatform = "cross_platform";

export const PLATFORM_LABEL_KEY: Record<SelectablePlatform, string> = {
  ios: "projectAdmin.initialSetup.platformIOS",
  android: "projectAdmin.initialSetup.platformAndroid",
  cross_platform: "projectAdmin.initialSetup.platformCrossPlatform",
};

// Mirrors domain.DefaultRepoDocPath for the three written documents. local_run
// is not one of them any more — see defaultDocPath.
const DEFAULT_MD_DOC_PATH: Record<Exclude<RepoDocKind, "local_run">, string> = {
  coding_standards: ".ai/coding-standards.md",
  test_standards: ".ai/test-standards.md",
  architecture: ".ai/architecture.md",
};

/**
 * Where a queued doc lands when the field was left blank. Every default is
 * relative to the scope it documents: two sub-repos sharing one repo-relative
 * default would write over each other inside the single docs PR. `local_run`
 * is no longer prose either — it is the executable that stands the project up
 * locally, so it defaults to a script next to the code it starts.
 */
export function defaultDocPath(kind: RepoDocKind, scopePath: string): string {
  const dir = scopePath && scopePath !== "." ? `${scopePath.replace(/\/+$/, "")}/` : "";
  if (kind === "local_run") return `${dir}scripts/dev.sh`;
  return `${dir}${DEFAULT_MD_DOC_PATH[kind]}`;
}

const DOC_FIELDS: { key: RepoDocKind; labelKey: string; hintKey?: string }[] = [
  { key: "coding_standards", labelKey: "projectAdmin.initialSetup.docsCodingStandards" },
  { key: "test_standards", labelKey: "projectAdmin.initialSetup.docsTestStandards" },
  { key: "architecture", labelKey: "projectAdmin.initialSetup.docsArchitecture" },
  {
    key: "local_run",
    labelKey: "projectAdmin.initialSetup.docsLocalRun",
    hintKey: "projectAdmin.initialSetup.docsLocalRunHint",
  },
];

/** Everything one configuration target (the repository itself, or one
 * sub-repo) collects before the wizard's single Save. */
export interface ScopeSetup {
  docs: RepositoryDocs;
  generate: RepoDocKind[];
  prodUrl: string;
  stageUrl: string;
  healthPath: string;
}

export function newScopeSetup(docs?: RepositoryDocs): ScopeSetup {
  return {
    docs: docs ?? {},
    generate: [],
    prodUrl: "",
    stageUrl: "",
    healthPath: "/health",
  };
}

// A hand-rolled target: the repo ships by some workflow this dialog knows
// nothing about, but where it answers is still worth recording. "custom" is
// the provider the backend accepts for exactly that.
function urlTarget(env: DeployEnv, baseURL: string, healthURL: string): SaveDeployTargetInput {
  return {
    env,
    provider: "custom",
    template_id: "",
    health_url: healthURL,
    base_url: baseURL.trim(),
    auto_rollback: false,
  };
}

// joinHealthURL builds the probe URL from the base address and a path, without
// producing a double slash on either side of the join.
function joinHealthURL(baseURL: string, path: string): string {
  const base = baseURL.trim().replace(/\/+$/, "");
  const suffix = path.trim();
  if (!base || !suffix) return "";
  return `${base}/${suffix.replace(/^\/+/, "")}`;
}

/**
 * The deploy targets one scope's answers amount to. Worker and mobile scopes
 * produce none: a worker has no address to expose, and a mobile app now ships
 * by connecting a store account and picking an app there, not through a
 * deploy target this wizard writes.
 */
export function scopeTargets(scope: ScopeSetup, kind: SubRepoKind, scopePath: string): SaveDeployTargetInput[] {
  const scoped = (target: SaveDeployTargetInput): SaveDeployTargetInput =>
    scopePath ? { ...target, sub_project_path: scopePath } : target;
  if (kind === "worker" || kind === "mobile") return [];
  const out: SaveDeployTargetInput[] = [];
  for (const [env, url] of [
    ["prod", scope.prodUrl],
    ["stage", scope.stageUrl],
  ] as [DeployEnv, string][]) {
    if (!url.trim()) continue;
    out.push(scoped(urlTarget(env, url, kind === "frontend" ? "" : joinHealthURL(url, scope.healthPath))));
  }
  return out;
}

/** One scope's queued docs as items for the single docs-bundle task. */
export function scopeDocsItems(
  scope: ScopeSetup,
  scopePath: string,
): Array<{ kind: RepoDocKind; sub_project_path?: string; path?: string }> {
  return scope.generate.map((kind) => ({
    kind,
    ...(scopePath ? { sub_project_path: scopePath } : {}),
    path: scope.docs[kind]?.trim() || defaultDocPath(kind, scopePath),
  }));
}

interface ScopeSetupFieldsProps {
  /** "" for the repository itself, otherwise the sub-repo's path. */
  scopePath: string;
  kind: SubRepoKind;
  value: ScopeSetup;
  onChange: (next: ScopeSetup) => void;
  disabled?: boolean;
}

/**
 * The questions one configuration target answers: which files document it, and
 * where it ships. Used for the repository itself and for every sub-repo of a
 * monorepo — the two used to be separate copies of the same form, one of them
 * inside a dialog opened on top of this one.
 */
export function ScopeSetupFields({
  scopePath,
  kind,
  value,
  onChange,
  disabled,
}: ScopeSetupFieldsProps) {
  const { t } = useI18n();
  const patch = (next: Partial<ScopeSetup>) => onChange({ ...value, ...next });

  const toggleGenerate = (key: RepoDocKind) => {
    const queued = value.generate.includes(key);
    const generate = queued ? value.generate.filter((k) => k !== key) : [...value.generate, key];
    const docs =
      !queued && !value.docs[key]?.trim()
        ? { ...value.docs, [key]: defaultDocPath(key, scopePath) }
        : value.docs;
    onChange({ ...value, generate, docs });
  };

  const urlWarning = (url: string) => url.trim() !== "" && !/^https?:\/\//i.test(url.trim());

  return (
    <div className="space-y-3">
      <Card>
        <CardHeader className="p-4 pb-2">
          <CardTitle className="text-sm">{t("projectAdmin.initialSetup.docsLabel")}</CardTitle>
          <p className="text-xs text-muted-foreground">{t("projectAdmin.initialSetup.docsSectionHint")}</p>
        </CardHeader>
        <CardContent className="space-y-3 p-4 pt-0">
          {DOC_FIELDS.map((f) => {
            const queued = value.generate.includes(f.key);
            return (
              <div key={f.key} className="space-y-1">
                <Label className="text-xs font-normal text-muted-foreground">{t(f.labelKey)}</Label>
                <div className="flex items-center gap-2">
                  <Input
                    value={value.docs[f.key] ?? ""}
                    onChange={(e) => patch({ docs: { ...value.docs, [f.key]: e.target.value } })}
                    placeholder={defaultDocPath(f.key, scopePath)}
                    disabled={disabled}
                    className="flex-1"
                  />
                  <Button
                    type="button"
                    variant={queued ? "default" : "outline"}
                    size="sm"
                    disabled={disabled}
                    onClick={() => toggleGenerate(f.key)}
                  >
                    <Sparkles className="mr-1.5 h-3.5 w-3.5" />
                    {queued
                      ? t("projectAdmin.initialSetup.docsQueued")
                      : t("projectAdmin.initialSetup.docsGenerate")}
                  </Button>
                </div>
                {f.hintKey && <p className="text-xs text-muted-foreground">{t(f.hintKey)}</p>}
              </div>
            );
          })}
        </CardContent>
      </Card>

      {kind === "worker" ? (
        <Notice variant="info" title={t("projectAdmin.initialSetup.workerNoteTitle")}>
          {t("projectAdmin.initialSetup.workerNote")}
        </Notice>
      ) : kind === "mobile" ? (
        <Notice variant="info" title={t("projectAdmin.initialSetup.mobileNoteTitle")}>
          {t("projectAdmin.initialSetup.mobileNote")}
        </Notice>
      ) : (
        <Card>
          <CardHeader className="p-4 pb-2">
            <CardTitle className="text-sm">{t("projectAdmin.initialSetup.deployLabel")}</CardTitle>
          </CardHeader>
          <CardContent className="space-y-3 p-4 pt-0">
            <div className="space-y-1">
              <Label>
                {kind === "frontend"
                  ? t("projectAdmin.initialSetup.prodUrl")
                  : t("projectAdmin.initialSetup.prodApiUrl")}
              </Label>
              <Input
                value={value.prodUrl}
                onChange={(e) => patch({ prodUrl: e.target.value })}
                placeholder="https://app.example.com"
                disabled={disabled}
              />
              {urlWarning(value.prodUrl) && (
                <p className="text-xs text-amber-600 dark:text-amber-400">
                  {t("projectAdmin.initialSetup.urlSchemeHint")}
                </p>
              )}
            </div>
            <div className="space-y-1">
              <Label>
                {kind === "frontend"
                  ? t("projectAdmin.initialSetup.stageUrl")
                  : t("projectAdmin.initialSetup.stageApiUrl")}
              </Label>
              <Input
                value={value.stageUrl}
                onChange={(e) => patch({ stageUrl: e.target.value })}
                placeholder="https://stage.example.com"
                disabled={disabled}
              />
              {urlWarning(value.stageUrl) && (
                <p className="text-xs text-amber-600 dark:text-amber-400">
                  {t("projectAdmin.initialSetup.urlSchemeHint")}
                </p>
              )}
            </div>
            {kind !== "frontend" && (
              <div className="space-y-1">
                <Label>{t("projectAdmin.initialSetup.healthPath")}</Label>
                <Input
                  value={value.healthPath}
                  onChange={(e) => patch({ healthPath: e.target.value })}
                  placeholder="/health"
                  disabled={disabled}
                />
                <p className="text-xs text-muted-foreground">{t("projectAdmin.initialSetup.healthPathHint")}</p>
              </div>
            )}
          </CardContent>
        </Card>
      )}
    </div>
  );
}
