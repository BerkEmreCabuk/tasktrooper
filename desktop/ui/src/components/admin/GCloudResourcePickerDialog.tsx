import { Boxes, ListChecks, PackageSearch, Plug, RefreshCw, Server, type LucideIcon } from "lucide-react";
import { useCallback, useEffect, useRef, useState } from "react";
import { Link } from "react-router-dom";
import { toast } from "sonner";
import {
  api,
  type GCloudFamilyListing,
  type GCloudGKEListing,
  type GCloudResourceBinding,
  type GCloudResourceListing,
  type GCloudResourceRef,
  type GCloudResourceType,
} from "@/api";
import { FormDialog } from "@/components/admin/FormDialog";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { EmptyState } from "@/components/ui/empty-state";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Notice } from "@/components/ui/notice";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Spinner } from "@/components/ui/spinner";
import { tStatic, useI18n } from "@/hooks/useI18n";

interface GCloudResourceListingState {
  listing: GCloudResourceListing | null;
  loading: boolean;
  /** The failed call's own sentence, never a stand-in for a project with nothing in it. */
  error: string;
  reload: () => void;
}

/**
 * Reads what the saved service account can see in the operator's Google Cloud
 * project, on demand.
 *
 * `enabled` keeps it on demand for the same reason the store listing does:
 * this reaches Google Cloud, so merely rendering a card that offers the button
 * must not fire it.
 */
export function useGCloudResourceListing(enabled: boolean): GCloudResourceListingState {
  // One slot for both halves of the answer: separate listing/error state made
  // the gap between "render wanted it" and "the effect started it" look like
  // "this key cannot enumerate", which is the one answer this must not invent.
  const [answer, setAnswer] = useState<{ listing: GCloudResourceListing | null; error: string } | null>(null);
  const [inFlight, setInFlight] = useState(false);
  const requestRef = useRef(0);

  const load = useCallback(async () => {
    if (!enabled) return;
    const seq = ++requestRef.current;
    setInFlight(true);
    try {
      const listing = await api.listGCloudResources();
      if (seq !== requestRef.current) return;
      setAnswer({ listing, error: "" });
    } catch (err) {
      if (seq !== requestRef.current) return;
      setAnswer({
        listing: null,
        error: err instanceof Error ? err.message : tStatic("settingsPages.integrations.gcloudResources.loadFailed"),
      });
    } finally {
      if (seq === requestRef.current) setInFlight(false);
    }
  }, [enabled]);

  useEffect(() => {
    void load();
  }, [load]);

  useEffect(() => {
    if (enabled) return;
    requestRef.current += 1;
    setAnswer(null);
    setInFlight(false);
  }, [enabled]);

  return {
    listing: answer?.listing ?? null,
    loading: enabled && (answer === null || inFlight),
    error: answer?.error ?? "",
    reload: () => void load(),
  };
}

/** Stable identity for a listed resource: a Cloud Run service and a cluster
 * can share a short name, so the type is part of the key. */
function resourceKey(resource: GCloudResourceRef): string {
  return `${resource.type}|${resource.name}`;
}

const EMPTY_FAMILY: GCloudFamilyListing = { listing_available: false };
const EMPTY_GKE: GCloudGKEListing = { listing_available: false, workloads_available: false };

function RetryButton({ onClick, disabled }: { onClick: () => void; disabled: boolean }) {
  const { t } = useI18n();
  return (
    <Button variant="outline" size="sm" className="gap-2" onClick={onClick} disabled={disabled}>
      <RefreshCw className={`h-3.5 w-3.5 ${disabled ? "animate-spin" : ""}`} />
      {t("settingsPages.integrations.gcloudResources.retry")}
    </Button>
  );
}

function ResourceIdentity({ resource }: { resource: GCloudResourceRef }) {
  const { t } = useI18n();
  const familyLabel =
    resource.type === "cloud_run_service"
      ? t("settingsPages.integrations.gcloudResources.cloudRunBadge")
      : t("settingsPages.integrations.gcloudResources.gkeBadge");
  return (
    <div className="flex min-w-0 flex-1 items-start justify-between gap-2">
      <div className="min-w-0">
        <p className="truncate text-sm font-medium">{resource.display_name || resource.name}</p>
        <p className="truncate font-mono text-xs text-muted-foreground">
          {resource.location}
          {resource.project_id ? ` · ${resource.project_id}` : ""}
        </p>
      </div>
      <div className="flex shrink-0 flex-wrap items-center justify-end gap-1.5">
        {/* The type is spelled out on every row, not only in the section
            heading: the two families are bound through different fields and a
            mis-read row is a mis-bound repository. */}
        <Badge variant="secondary">{familyLabel}</Badge>
        {/* No state, no badge — Google Cloud did not say, and an invented
            "unknown" pill reads as a real lifecycle answer. */}
        {resource.state ? <Badge variant="outline">{resource.state}</Badge> : null}
      </div>
    </div>
  );
}

interface GKEWorkloadsNoticeProps {
  /** `GKEClusterDetail.workloads_note` when a detail read supplied one — it
   * names the wall that specific cluster hit. The listing carries none. */
  note?: string;
}

/**
 * Why a cluster is listed but its workloads are not.
 *
 * `workloads_available` is hardcoded false server-side and that is a boundary,
 * not an outage: the shared agent-server sits outside the customer's VPC and a
 * private GKE control plane only answers from inside it. Rendered calmly for
 * exactly that reason — a red error would teach people to distrust the cluster
 * rows that *are* real.
 */
export function GKEWorkloadsNotice({ note }: GKEWorkloadsNoticeProps) {
  const { t } = useI18n();
  return (
    <Notice variant="info" title={t("settingsPages.integrations.gcloudResources.workloadsTitle")}>
      <p>{t("settingsPages.integrations.gcloudResources.workloadsBody")}</p>
      {note ? <p className="mt-1 font-mono text-xs">{note}</p> : null}
    </Notice>
  );
}

interface FamilySelection {
  /** Shared across both sections, so arrow keys walk the whole choice. */
  name: string;
  value: string;
  onChange: (value: string) => void;
}

interface ResourceFamilySectionProps {
  icon: LucideIcon;
  title: string;
  family: GCloudFamilyListing;
  resources: GCloudResourceRef[];
  unavailableBody: string;
  /** Absent for the read-only browser on the settings card. */
  select?: FamilySelection;
  children?: React.ReactNode;
}

/**
 * One family's rows and one family's verdict.
 *
 * The verdict is per family because the permissions are: a service account
 * holding only roles/run.viewer lists Cloud Run perfectly and 403s on GKE, and
 * collapsing that into one failure would hide a working picker.
 */
function ResourceFamilySection({
  icon: Icon,
  title,
  family,
  resources,
  unavailableBody,
  select,
  children,
}: ResourceFamilySectionProps) {
  const { t } = useI18n();
  const unreachable = family.unreachable_locations ?? [];

  return (
    <section className="space-y-2">
      <div className="flex items-center gap-2">
        <Icon className="h-4 w-4 shrink-0 text-muted-foreground" aria-hidden />
        <h4 className="text-sm font-medium">{title}</h4>
      </div>

      {!family.listing_available ? (
        <Notice variant="info" title={t("settingsPages.integrations.gcloudResources.familyUnavailableTitle")}>
          {unavailableBody}
        </Notice>
      ) : resources.length === 0 ? (
        <p className="text-xs text-muted-foreground">{t("settingsPages.integrations.gcloudResources.familyEmpty")}</p>
      ) : select ? (
        <fieldset className="rounded-lg border border-border">
          {/* sr-only legend, rows in their own wrapper: `divide-y` on the
              fieldset would count the legend as a child and draw a stray rule
              above the first row. */}
          <legend className="sr-only">{title}</legend>
          <div className="divide-y divide-border">
            {resources.map((resource) => {
              const key = resourceKey(resource);
              return (
                <label
                  key={key}
                  className="flex cursor-pointer items-start gap-3 px-3 py-2.5 transition-colors hover:bg-muted/50 has-[:checked]:bg-muted"
                >
                  <input
                    type="radio"
                    name={select.name}
                    className="mt-1 h-4 w-4 shrink-0 accent-primary focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2"
                    checked={select.value === key}
                    onChange={() => select.onChange(key)}
                  />
                  <ResourceIdentity resource={resource} />
                </label>
              );
            })}
          </div>
        </fieldset>
      ) : (
        <ul className="divide-y divide-border rounded-lg border border-border">
          {resources.map((resource) => (
            <li key={resourceKey(resource)} className="flex px-3 py-2.5">
              <ResourceIdentity resource={resource} />
            </li>
          ))}
        </ul>
      )}

      {unreachable.length > 0 && (
        <p className="text-xs text-muted-foreground">
          {t("settingsPages.integrations.gcloudResources.unreachableLocations", {
            locations: unreachable.join(", "),
          })}
        </p>
      )}

      {children}
    </section>
  );
}

interface ResourceSectionsProps {
  listing: GCloudResourceListing;
  select?: FamilySelection;
}

function ResourceSections({ listing, select }: ResourceSectionsProps) {
  const { t } = useI18n();
  const resources = listing.resources ?? [];
  const cloudRun = listing.cloud_run ?? EMPTY_FAMILY;
  const gke = listing.gke ?? EMPTY_GKE;

  return (
    <div className="space-y-4">
      <ResourceFamilySection
        icon={Server}
        title={t("settingsPages.integrations.gcloudResources.cloudRunTitle")}
        family={cloudRun}
        resources={resources.filter((r) => r.type === "cloud_run_service")}
        unavailableBody={t("settingsPages.integrations.gcloudResources.cloudRunUnavailableBody")}
        select={select}
      />
      <ResourceFamilySection
        icon={Boxes}
        title={t("settingsPages.integrations.gcloudResources.gkeTitle")}
        family={gke}
        resources={resources.filter((r) => r.type === "gke_cluster")}
        unavailableBody={t("settingsPages.integrations.gcloudResources.gkeUnavailableBody")}
        select={select}
      >
        {gke.listing_available && !gke.workloads_available ? <GKEWorkloadsNotice /> : null}
      </ResourceFamilySection>
    </div>
  );
}

/** True when at least one family answered, so a half-privileged key still
 * gets the picker it earned instead of the manual field. */
function canEnumerate(listing: GCloudResourceListing | null): boolean {
  if (!listing) return false;
  return Boolean(listing.listing_available || listing.cloud_run?.listing_available || listing.gke?.listing_available);
}

/** No credential at all: neither a list nor a hand-typed name can be checked
 * against anything, so the answer is Integrations rather than a retry. */
function isNotConnected(listing: GCloudResourceListing | null): boolean {
  if (!listing || canEnumerate(listing)) return false;
  if (listing.reason === "not_connected") return true;
  return listing.cloud_run?.reason === "not_connected" && listing.gke?.reason === "not_connected";
}

interface GCloudResourcesBrowserProps {
  /** False hides the button entirely — there is nothing to enumerate with. */
  configured: boolean;
}

/**
 * The read-only half of the picker: "what can this service account actually
 * see?".
 *
 * It binds nothing, because Settings has no repository in hand — it is the
 * proof that the saved key reaches Google Cloud, and the one place the
 * per-family verdicts are explained.
 */
export function GCloudResourcesBrowser({ configured }: GCloudResourcesBrowserProps) {
  const { t } = useI18n();
  const [browsing, setBrowsing] = useState(false);
  const { listing, loading, error, reload } = useGCloudResourceListing(browsing);

  if (!configured) {
    return <p className="text-xs text-muted-foreground">{t("settingsPages.integrations.gcloudResources.connectFirst")}</p>;
  }

  if (!browsing) {
    return (
      <Button variant="outline" size="sm" className="gap-2" onClick={() => setBrowsing(true)}>
        <ListChecks className="h-3.5 w-3.5" />
        {t("settingsPages.integrations.gcloudResources.list")}
      </Button>
    );
  }

  const enumerable = canEnumerate(listing);
  const bothAvailable = Boolean(listing?.cloud_run?.listing_available && listing?.gke?.listing_available);
  const empty = enumerable && bothAvailable && (listing?.resources?.length ?? 0) === 0;

  return (
    <div className="space-y-2">
      <div className="flex items-center justify-between gap-2">
        <p className="text-sm font-medium">{t("settingsPages.integrations.gcloudResources.title")}</p>
        <RetryButton onClick={reload} disabled={loading} />
      </div>

      {loading ? (
        <div className="flex items-center gap-2 py-2 text-sm text-muted-foreground">
          <Spinner size="sm" />
          {t("settingsPages.integrations.gcloudResources.loading")}
        </div>
      ) : error ? (
        <Notice variant="error" title={t("settingsPages.integrations.gcloudResources.loadFailed")}>
          {error}
        </Notice>
      ) : isNotConnected(listing) ? (
        <Notice variant="info" title={t("settingsPages.integrations.gcloudResources.notConnectedTitle")}>
          {t("settingsPages.integrations.gcloudResources.notConnectedBody")}
        </Notice>
      ) : !enumerable ? (
        <Notice variant="info" title={t("settingsPages.integrations.gcloudResources.unavailableTitle")}>
          {t("settingsPages.integrations.gcloudResources.unavailableBody")}
        </Notice>
      ) : empty ? (
        <EmptyState
          className="py-6"
          icon={PackageSearch}
          title={t("settingsPages.integrations.gcloudResources.emptyTitle")}
          description={t("settingsPages.integrations.gcloudResources.emptyBody")}
        />
      ) : listing ? (
        <ResourceSections listing={listing} />
      ) : null}
    </div>
  );
}

export interface GCloudResourcePickerDialogProps {
  repositoryId: string;
  subProjectPath?: string;
  open: boolean;
  onOpenChange: (open: boolean) => void;
  onBound?: (binding: GCloudResourceBinding) => void;
}

/**
 * Binds a repository scope to one Cloud Run service or GKE cluster.
 *
 * Three answers, told apart by the server rather than guessed from an empty
 * array: no credential (Integrations, and no manual field — there is nothing
 * to verify a typed name against), a credential that cannot enumerate (the
 * resource name is typed by hand), and a credential that can (the list, split
 * per family so one missing role does not hide the other half).
 */
export function GCloudResourcePickerDialog({
  repositoryId,
  subProjectPath,
  open,
  onOpenChange,
  onBound,
}: GCloudResourcePickerDialogProps) {
  const { t } = useI18n();
  const { listing, loading, error, reload } = useGCloudResourceListing(open);
  const [selected, setSelected] = useState("");
  const [manualType, setManualType] = useState<GCloudResourceType>("cloud_run_service");
  const [manualName, setManualName] = useState("");
  const [binding, setBinding] = useState(false);

  // A reopen starts over: the previous pick belongs to the previous question,
  // and silently re-binding it is the worst possible default.
  useEffect(() => {
    if (!open) return;
    setSelected("");
    setManualName("");
    setManualType("cloud_run_service");
  }, [open]);

  const enumerable = canEnumerate(listing);
  const notConnected = isNotConnected(listing);
  const resources = listing?.resources ?? [];
  const chosen = enumerable ? resources.find((r) => resourceKey(r) === selected) : undefined;
  const typedName = manualName.trim();
  const canBind = notConnected ? false : enumerable ? Boolean(chosen) : typedName !== "";
  const bothAvailable = Boolean(listing?.cloud_run?.listing_available && listing?.gke?.listing_available);
  const empty = enumerable && bothAvailable && resources.length === 0;

  const bind = useCallback(async () => {
    const resourceType = chosen?.type ?? manualType;
    const resourceName = chosen?.name ?? typedName;
    if (!resourceName) return;
    setBinding(true);
    try {
      const saved = await api.bindGCloudResource(repositoryId, {
        resource_type: resourceType,
        resource_name: resourceName,
        // Never `undefined`: the API layer defaults this to "" by spreading
        // the object, so an explicit undefined would drop it from the body.
        sub_project_path: subProjectPath ?? "",
      });
      toast.success(t("settingsPages.integrations.gcloudPicker.bound"));
      onBound?.(saved);
      onOpenChange(false);
    } catch (err) {
      toast.error(err instanceof Error ? err.message : t("settingsPages.integrations.gcloudPicker.bindFailed"));
    } finally {
      setBinding(false);
    }
  }, [chosen, manualType, typedName, repositoryId, subProjectPath, onBound, onOpenChange, t]);

  return (
    <FormDialog
      open={open}
      onOpenChange={onOpenChange}
      title={t("settingsPages.integrations.gcloudPicker.title")}
      description={t("settingsPages.integrations.gcloudPicker.description")}
      footer={
        <>
          <Button variant="outline" onClick={() => onOpenChange(false)} disabled={binding}>
            {t("common.cancel")}
          </Button>
          <Button onClick={() => void bind()} disabled={binding || !canBind}>
            {binding
              ? t("settingsPages.integrations.gcloudPicker.binding")
              : t("settingsPages.integrations.gcloudPicker.bind")}
          </Button>
        </>
      }
    >
      {loading ? (
        <div className="flex items-center gap-2 py-4 text-sm text-muted-foreground">
          <Spinner size="sm" />
          {t("settingsPages.integrations.gcloudResources.loading")}
        </div>
      ) : error ? (
        <div className="space-y-3">
          <Notice variant="error" title={t("settingsPages.integrations.gcloudResources.loadFailed")}>
            {error}
          </Notice>
          <RetryButton onClick={reload} disabled={loading} />
        </div>
      ) : notConnected ? (
        <EmptyState
          className="py-6"
          icon={Plug}
          title={t("settingsPages.integrations.gcloudResources.notConnectedTitle")}
          description={t("settingsPages.integrations.gcloudResources.notConnectedBody")}
          action={
            <Button asChild>
              <Link to="/settings/integrations">
                {t("settingsPages.integrations.gcloudResources.notConnectedAction")}
              </Link>
            </Button>
          }
        />
      ) : enumerable && listing ? (
        empty ? (
          <div className="space-y-3">
            <EmptyState
              className="py-6"
              icon={PackageSearch}
              title={t("settingsPages.integrations.gcloudResources.emptyTitle")}
              description={t("settingsPages.integrations.gcloudResources.emptyBody")}
            />
            <div className="flex justify-center">
              <RetryButton onClick={reload} disabled={loading} />
            </div>
          </div>
        ) : (
          <ResourceSections
            listing={listing}
            select={{ name: "gcloud-resource-choice", value: selected, onChange: setSelected }}
          />
        )
      ) : (
        <div className="space-y-3">
          <Notice variant="info" title={t("settingsPages.integrations.gcloudResources.unavailableTitle")}>
            {t("settingsPages.integrations.gcloudResources.unavailableBody")}
          </Notice>
          <div className="space-y-1">
            <Label htmlFor="gcloud-resource-type">{t("settingsPages.integrations.gcloudPicker.typeLabel")}</Label>
            <Select value={manualType} onValueChange={(v) => setManualType(v as GCloudResourceType)}>
              <SelectTrigger id="gcloud-resource-type">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value="cloud_run_service">
                  {t("settingsPages.integrations.gcloudPicker.typeCloudRun")}
                </SelectItem>
                <SelectItem value="gke_cluster">
                  {t("settingsPages.integrations.gcloudPicker.typeGke")}
                </SelectItem>
              </SelectContent>
            </Select>
          </div>
          <div className="space-y-1">
            <Label htmlFor="gcloud-resource-name">{t("settingsPages.integrations.gcloudPicker.manualLabel")}</Label>
            <Input
              id="gcloud-resource-name"
              value={manualName}
              onChange={(e) => setManualName(e.target.value)}
              placeholder={t("settingsPages.integrations.gcloudPicker.manualPlaceholder")}
              className="font-mono text-xs"
            />
            <p className="text-xs text-muted-foreground">
              {t("settingsPages.integrations.gcloudPicker.manualHint")}
            </p>
          </div>
          <RetryButton onClick={reload} disabled={loading} />
        </div>
      )}
    </FormDialog>
  );
}
