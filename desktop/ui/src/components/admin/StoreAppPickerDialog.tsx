import { ListChecks, PackageSearch, Plug, RefreshCw } from "lucide-react";
import { useCallback, useEffect, useRef, useState } from "react";
import { Link } from "react-router-dom";
import { toast } from "sonner";
import {
  api,
  type MobileStoreApp,
  type MobileStorePlatform,
  type StoreAppListing,
  type StoreAppRef,
  type StoreCredentialProvider,
} from "@/api";
import { FormDialog } from "@/components/admin/FormDialog";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { EmptyState } from "@/components/ui/empty-state";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Notice } from "@/components/ui/notice";
import { Spinner } from "@/components/ui/spinner";
import { tStatic, useI18n } from "@/hooks/useI18n";

interface StoreAppListingState {
  listing: StoreAppListing | null;
  loading: boolean;
  /** The failed call's own sentence, never a stand-in for an empty account. */
  error: string;
  reload: () => void;
}

/**
 * Reads the apps a saved store credential can see, on demand.
 *
 * `enabled` is what makes it on demand: this call reaches the store console,
 * so it must not fire merely because a card that offers the button rendered.
 */
export function useStoreAppListing(provider: StoreCredentialProvider, enabled: boolean): StoreAppListingState {
  // One slot for both halves of the answer, and `null` until there is one. The
  // request starts in the effect below — a commit later than the render that
  // wanted it — and separate `listing`/`error` state made that gap render as
  // "this credential cannot list apps", the wrong one of the two answers this
  // hook exists to keep apart.
  const [answer, setAnswer] = useState<{ listing: StoreAppListing | null; error: string } | null>(null);
  const [inFlight, setInFlight] = useState(false);
  const requestRef = useRef(0);

  const load = useCallback(async () => {
    if (!enabled) return;
    const seq = ++requestRef.current;
    setInFlight(true);
    try {
      const listing = await api.listStoreCredentialApps(provider);
      if (seq !== requestRef.current) return;
      setAnswer({ listing, error: "" });
    } catch (err) {
      if (seq !== requestRef.current) return;
      setAnswer({
        listing: null,
        error: err instanceof Error ? err.message : tStatic("settingsPages.integrations.stores.loadFailed"),
      });
    } finally {
      if (seq === requestRef.current) setInFlight(false);
    }
  }, [provider, enabled]);

  useEffect(() => {
    void load();
  }, [load]);

  useEffect(() => {
    if (enabled) return;
    // Going quiet drops whatever is in flight: re-enabling asks again rather
    // than showing the previous account's answer for a frame.
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

/** Stable identity for a listed app: Play rows carry no store_app_id. */
function appKey(app: StoreAppRef): string {
  return `${app.store_app_id}|${app.identifier}`;
}

function StoreAppIdentity({ app }: { app: StoreAppRef }) {
  return (
    <div className="flex min-w-0 flex-1 items-start justify-between gap-2">
      <div className="min-w-0">
        <p className="truncate text-sm font-medium">{app.name || app.identifier}</p>
        <p className="truncate font-mono text-xs text-muted-foreground">{app.identifier}</p>
      </div>
      {/* No state, no badge: the store did not say, and an invented "unknown"
          pill reads as a real lifecycle answer. */}
      {app.state ? (
        <Badge variant="outline" className="shrink-0">
          {app.state}
        </Badge>
      ) : null}
    </div>
  );
}

function RetryButton({ onClick, disabled }: { onClick: () => void; disabled: boolean }) {
  const { t } = useI18n();
  return (
    <Button variant="outline" size="sm" className="gap-2" onClick={onClick} disabled={disabled}>
      <RefreshCw className={`h-3.5 w-3.5 ${disabled ? "animate-spin" : ""}`} />
      {t("settingsPages.integrations.stores.retry")}
    </Button>
  );
}

interface StoreAppsBrowserProps {
  provider: StoreCredentialProvider;
  /** False hides the button entirely — there is nothing to enumerate with. */
  configured: boolean;
}

/**
 * The read-only half of the picker: "what can this credential actually see?".
 *
 * It links nothing, because Settings has no repository in hand — it is the
 * proof that the saved key reaches the store console, and the place the
 * "listing is not available" answer is explained once.
 */
export function StoreAppsBrowser({ provider, configured }: StoreAppsBrowserProps) {
  const { t } = useI18n();
  const [browsing, setBrowsing] = useState(false);
  const { listing, loading, error, reload } = useStoreAppListing(provider, browsing);

  if (!configured) {
    return <p className="text-xs text-muted-foreground">{t("settingsPages.integrations.stores.connectFirst")}</p>;
  }

  if (!browsing) {
    return (
      <Button variant="outline" size="sm" className="gap-2" onClick={() => setBrowsing(true)}>
        <ListChecks className="h-3.5 w-3.5" />
        {t("settingsPages.integrations.stores.listApps")}
      </Button>
    );
  }

  return (
    <div className="space-y-2">
      <div className="flex items-center justify-between gap-2">
        <p className="text-sm font-medium">{t("settingsPages.integrations.stores.appsTitle")}</p>
        <RetryButton onClick={reload} disabled={loading} />
      </div>

      {loading ? (
        <div className="flex items-center gap-2 py-2 text-sm text-muted-foreground">
          <Spinner size="sm" />
          {t("settingsPages.integrations.stores.loading")}
        </div>
      ) : error ? (
        <Notice variant="error" title={t("settingsPages.integrations.stores.loadFailed")}>
          {error}
        </Notice>
      ) : listing && !listing.listing_available ? (
        <Notice variant="info" title={t("settingsPages.integrations.stores.unavailableTitle")}>
          {t("settingsPages.integrations.stores.unavailableBody")}
        </Notice>
      ) : (listing?.apps?.length ?? 0) === 0 ? (
        <EmptyState
          className="py-6"
          icon={PackageSearch}
          title={t("settingsPages.integrations.stores.emptyTitle")}
          description={t("settingsPages.integrations.stores.emptyBody")}
        />
      ) : (
        <ul className="max-h-56 divide-y divide-border overflow-y-auto rounded-lg border border-border">
          {(listing?.apps ?? []).map((app) => (
            <li key={appKey(app)} className="flex px-3 py-2.5">
              <StoreAppIdentity app={app} />
            </li>
          ))}
        </ul>
      )}
    </div>
  );
}

export interface StoreAppPickerDialogProps {
  provider: StoreCredentialProvider;
  platform: MobileStorePlatform;
  /** The repository the chosen app is bound to. */
  repositoryId: string;
  open: boolean;
  onOpenChange: (open: boolean) => void;
  /** The row the server created for the link. */
  onLinked?: (app: MobileStoreApp) => void;
}

/**
 * Binds a repository to one app in the connected store account.
 *
 * Two ways in, and which one is offered is the server's answer, not a guess
 * from an empty array: `listing_available` false means this credential cannot
 * enumerate at all, so a list would be a lie and the identifier is typed by
 * hand instead. An account that genuinely holds no apps is the other case and
 * gets its own empty state.
 */
export function StoreAppPickerDialog({
  provider,
  platform,
  repositoryId,
  open,
  onOpenChange,
  onLinked,
}: StoreAppPickerDialogProps) {
  const { t } = useI18n();
  const { listing, loading, error, reload } = useStoreAppListing(provider, open);
  const [selected, setSelected] = useState("");
  const [manual, setManual] = useState("");
  const [linking, setLinking] = useState(false);

  // A reopen starts over: the previous pick belongs to the previous question,
  // and silently re-linking it is the worst possible default.
  useEffect(() => {
    if (!open) return;
    setSelected("");
    setManual("");
  }, [open]);

  const apps = listing?.apps ?? [];
  const canEnumerate = listing?.listing_available ?? false;
  // Not the same emptiness as "this credential cannot enumerate": there is no
  // credential at all, so the manual identifier field has nothing to verify
  // against and linking has to stay closed until Integrations is answered.
  const notConnected = listing?.reason === "not_connected";
  const chosen = canEnumerate ? apps.find((app) => appKey(app) === selected) : undefined;
  const manualId = manual.trim();
  const canLink = notConnected ? false : canEnumerate ? Boolean(chosen) : manualId !== "";

  const link = useCallback(async () => {
    const ref: StoreAppRef = chosen ?? { store_app_id: "", identifier: manualId, name: "" };
    if (!ref.identifier) return;
    setLinking(true);
    try {
      const app = await api.linkStoreApp(repositoryId, platform, ref);
      toast.success(t("settingsPages.integrations.picker.linked"));
      onLinked?.(app);
      onOpenChange(false);
    } catch (err) {
      toast.error(err instanceof Error ? err.message : t("settingsPages.integrations.picker.linkFailed"));
    } finally {
      setLinking(false);
    }
  }, [chosen, manualId, repositoryId, platform, onLinked, onOpenChange, t]);

  return (
    <FormDialog
      open={open}
      onOpenChange={onOpenChange}
      title={t("settingsPages.integrations.picker.title")}
      description={t("settingsPages.integrations.picker.description")}
      footer={
        <>
          <Button variant="outline" onClick={() => onOpenChange(false)} disabled={linking}>
            {t("common.cancel")}
          </Button>
          <Button onClick={() => void link()} disabled={linking || !canLink}>
            {linking
              ? t("settingsPages.integrations.picker.linking")
              : t("settingsPages.integrations.picker.link")}
          </Button>
        </>
      }
    >
      {loading ? (
        <div className="flex items-center gap-2 py-4 text-sm text-muted-foreground">
          <Spinner size="sm" />
          {t("settingsPages.integrations.stores.loading")}
        </div>
      ) : error ? (
        <div className="space-y-3">
          <Notice variant="error" title={t("settingsPages.integrations.stores.loadFailed")}>
            {error}
          </Notice>
          <RetryButton onClick={reload} disabled={loading} />
        </div>
      ) : notConnected ? (
        <EmptyState
          className="py-6"
          icon={Plug}
          title={t("settingsPages.integrations.stores.notConnectedTitle")}
          description={t("settingsPages.integrations.stores.notConnectedBody")}
          action={
            <Button asChild>
              <Link to="/settings/integrations">
                {t("settingsPages.integrations.stores.notConnectedAction")}
              </Link>
            </Button>
          }
        />
      ) : canEnumerate ? (
        apps.length === 0 ? (
          <div className="space-y-3">
            <EmptyState
              className="py-6"
              icon={PackageSearch}
              title={t("settingsPages.integrations.stores.emptyTitle")}
              description={t("settingsPages.integrations.stores.emptyBody")}
            />
            <div className="flex justify-center">
              <RetryButton onClick={reload} disabled={loading} />
            </div>
          </div>
        ) : (
          <fieldset className="rounded-lg border border-border">
            {/* sr-only legend, and the rows in their own wrapper: `divide-y` on
                the fieldset would count the legend as a child and draw a stray
                rule above the first app. */}
            <legend className="sr-only">{t("settingsPages.integrations.picker.appsLabel")}</legend>
            <div className="divide-y divide-border">
              {apps.map((app) => {
                const key = appKey(app);
                return (
                  <label
                    key={key}
                    className="flex cursor-pointer items-start gap-3 px-3 py-2.5 transition-colors hover:bg-muted/50 has-[:checked]:bg-muted"
                  >
                    <input
                      type="radio"
                      name="store-app-choice"
                      className="mt-1 h-4 w-4 shrink-0 accent-primary focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2"
                      checked={selected === key}
                      onChange={() => setSelected(key)}
                    />
                    <StoreAppIdentity app={app} />
                  </label>
                );
              })}
            </div>
          </fieldset>
        )
      ) : (
        <div className="space-y-3">
          <Notice variant="info" title={t("settingsPages.integrations.stores.unavailableTitle")}>
            {t("settingsPages.integrations.stores.unavailableBody")}
          </Notice>
          <div className="space-y-1">
            <Label htmlFor="store-app-identifier">{t("settingsPages.integrations.picker.manualLabel")}</Label>
            <Input
              id="store-app-identifier"
              value={manual}
              onChange={(e) => setManual(e.target.value)}
              placeholder={t("settingsPages.integrations.picker.manualPlaceholder")}
              className="font-mono text-xs"
            />
            <p className="text-xs text-muted-foreground">{t("settingsPages.integrations.picker.manualHint")}</p>
          </div>
          <RetryButton onClick={reload} disabled={loading} />
        </div>
      )}
    </FormDialog>
  );
}
