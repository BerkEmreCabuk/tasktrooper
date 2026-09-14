import {
  ChevronDown,
  ChevronUp,
  Link2,
  Loader2,
  Lock,
  Plus,
  Save,
  Smartphone,
  Trash2,
} from "lucide-react";
import { useCallback, useEffect, useState } from "react";
import { toast } from "sonner";
import { PageHeader } from "@/components/admin/PageHeader";
import { FormDialog } from "@/components/admin/FormDialog";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card } from "@/components/ui/card";
import { ConfirmDialog } from "@/components/ui/confirm-dialog";
import { EmptyState } from "@/components/ui/empty-state";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Notice } from "@/components/ui/notice";
import { Skeleton } from "@/components/ui/skeleton";
import { useI18n } from "@/hooks/useI18n";
import { api, type MobileDeviceStatus } from "@/api";

/**
 * The registered test phones.
 *
 * The hard part of this page has never been the list, it is that the values a
 * phone needs are not things an operator knows — they are things a phone is
 * currently displaying, on a screen most people have never opened, and one of
 * them (the pairing code) expires while its dialog is closed. So the ordered
 * walkthrough that made the single-device version usable is kept, split by the
 * scope each part actually has: the phone-side preparation is one shared card
 * above the list because it is identical for every phone, while the address and
 * pairing guidance now lives inside the row of the device it belongs to — with
 * two phones on a desk, "the pairing code" is ambiguous unless it is written
 * next to the name of the one you are holding.
 *
 * Rows are collapsed by default. The steady state of this page is "which phones
 * do I have and are they up", not "let me retype an address"; a list of expanded
 * forms answers the second question at the cost of the first.
 */
export function MobileDeviceSettingsPage() {
  const { t } = useI18n();
  const [devices, setDevices] = useState<MobileDeviceStatus[]>([]);
  const [loading, setLoading] = useState(true);
  const [adding, setAdding] = useState(false);
  // The Mac shell's simulator support (Appium, iOS/Android automation) is
  // gone — the desktop app is now a thin Claude Code bridge with nothing to
  // boot a simulator with (see desktop/CLAUDE.md). What remains here is
  // registering a real phone by pairing code, which needs no desktop host.

  const load = useCallback(async () => {
    setLoading(true);
    try {
      setDevices((await api.mobileDevices()).devices);
    } catch (e) {
      toast.error(e instanceof Error ? e.message : t("common.loadFailed"));
    } finally {
      setLoading(false);
    }
    // t is left out deliberately: switching language must not wipe input the
    // operator is halfway through typing.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  useEffect(() => {
    void load();
  }, [load]);

  // Every mutation answers with the whole list, so the page never patches a
  // single row: a pair or a delete can change what the server reports about the
  // others (which phone a lease sits on, whether the env fallback is still in
  // play), and a locally merged row would quietly disagree with it.
  const hubMissing = devices.length > 0 && devices.every((d) => !d.hub_managed);

  return (
    <div>
      <PageHeader
        title={t("settingsPages.mobileDevice.title")}
        description={t("settingsPages.mobileDevice.description")}
        action={
          <Button onClick={() => setAdding(true)}>
            <Plus className="mr-2 h-4 w-4" />
            {t("settingsPages.mobileDevice.add")}
          </Button>
        }
      />

      {hubMissing && (
        <Notice variant="error" title={t("settingsPages.mobileDevice.hub.missing")} className="mb-4" />
      )}

      <GuidanceCard
        title={t("settingsPages.mobileDevice.prep.title")}
        hint={t("settingsPages.mobileDevice.prep.hint")}
        body={t("settingsPages.mobileDevice.prep.body")}
      />

      <div className="mt-4 space-y-3">
        {loading ? (
          <Skeleton className="h-24 w-full" />
        ) : devices.length === 0 ? (
          <Card className="w-full">
            <EmptyState
              icon={Smartphone}
              title={t("settingsPages.mobileDevice.empty.title")}
              description={t("settingsPages.mobileDevice.empty.description")}
              action={
                <Button onClick={() => setAdding(true)}>
                  <Plus className="mr-2 h-4 w-4" />
                  {t("settingsPages.mobileDevice.add")}
                </Button>
              }
            />
          </Card>
        ) : (
          devices.map((device) => (
            // Keyed by id and name together so the env fallback, whose id is the
            // empty string, still gets a stable key next to a real device.
            <DeviceCard key={device.id || `env:${device.name}`} device={device} onDevices={setDevices} />
          ))
        )}
      </div>

      <GuidanceCard
        title={t("settingsPages.mobileDevice.repo.title")}
        hint={t("settingsPages.mobileDevice.repo.hint")}
        body={t("settingsPages.mobileDevice.repo.body")}
      />

      <AddDeviceDialog open={adding} onOpenChange={setAdding} onDevices={setDevices} />
    </div>
  );
}

/**
 * One phone: what it is, whether it is answering, and the two forms that fix it
 * when it is not.
 *
 * The env-configured device is rendered by the same component rather than a
 * read-only twin, because the thing an operator wants from it is exactly what
 * they want from the others — is it up, when did it last connect — and only the
 * mutations differ. Those are gated on the id being empty rather than on
 * `managed` alone: a device with no id has no per-device route to call at all,
 * so offering the buttons would produce requests that cannot be addressed.
 */
function DeviceCard({
  device,
  onDevices,
}: {
  device: MobileDeviceStatus;
  onDevices: (devices: MobileDeviceStatus[]) => void;
}) {
  const { t } = useI18n();
  const [open, setOpen] = useState(false);
  const [saving, setSaving] = useState(false);
  const [pairing, setPairing] = useState(false);
  const [connecting, setConnecting] = useState(false);
  const [removing, setRemoving] = useState(false);

  const [deviceAddr, setDeviceAddr] = useState(device.device_addr ?? "");
  // The PIN is never echoed back by the API, so the input stays empty and an
  // untouched field means "keep what is stored" (see the pointer field on
  // UpdateMobileDeviceRequest).
  const [pin, setPin] = useState("");
  const [pairAddr, setPairAddr] = useState("");
  const [pairCode, setPairCode] = useState("");

  const editable = device.id !== "";

  const save = async () => {
    setSaving(true);
    try {
      const res = await api.updateMobileDevice(device.id, {
        device_addr: deviceAddr.trim(),
        // null, not "": an empty box means "leave the stored secret alone".
        device_pin: pin === "" ? null : pin,
      });
      onDevices(res.devices);
      setPin("");
      toast.success(t("common.saved"));
    } catch (e) {
      toast.error(e instanceof Error ? e.message : t("common.saveFailed"));
    } finally {
      setSaving(false);
    }
  };

  const pair = async () => {
    setPairing(true);
    try {
      const res = await api.pairMobileDevice(device.id, {
        pair_addr: pairAddr.trim(),
        code: pairCode.trim(),
      });
      onDevices(res.devices);
      setPairCode("");
      toast.success(t("settingsPages.mobileDevice.paired"));
    } catch (e) {
      toast.error(e instanceof Error ? e.message : t("settingsPages.mobileDevice.pairFailed"));
    } finally {
      setPairing(false);
    }
  };

  const connect = async () => {
    setConnecting(true);
    try {
      onDevices((await api.connectMobileDevice(device.id)).devices);
      toast.success(t("settingsPages.mobileDevice.connected"));
    } catch (e) {
      toast.error(e instanceof Error ? e.message : t("settingsPages.mobileDevice.connectFailed"));
    } finally {
      setConnecting(false);
    }
  };

  const remove = async () => {
    try {
      onDevices((await api.deleteMobileDevice(device.id)).devices);
      toast.success(t("settingsPages.mobileDevice.unregistered"));
    } catch (e) {
      toast.error(e instanceof Error ? e.message : t("common.saveFailed"));
    } finally {
      setRemoving(false);
    }
  };

  return (
    <Card className="w-full p-4">
      <div className="flex flex-wrap items-start justify-between gap-3">
        <div className="min-w-0">
          <div className="flex flex-wrap items-center gap-2">
            <p className="text-sm font-medium text-foreground">{device.name}</p>
            <StatusBadge device={device} />
            {device.managed && (
              <Badge variant="secondary">
                <Lock className="mr-1 h-3 w-3" />
                {t("settingsPages.mobileDevice.managed.badge")}
              </Badge>
            )}
          </div>
          <p className="mt-1 text-xs text-muted-foreground">
            {device.device_addr ?? "—"} ·{" "}
            {device.last_connected_at
              ? `${t("settingsPages.mobileDevice.status.lastConnected")}: ${new Date(
                  device.last_connected_at,
                ).toLocaleString()}`
              : t("settingsPages.mobileDevice.status.lastConnectedNever")}
          </p>
          {device.detail && <p className="mt-1 text-xs text-muted-foreground">{device.detail}</p>}
          {!device.hub_reachable && (
            <p className="mt-1 text-xs text-destructive">{t("settingsPages.mobileDevice.status.hubDown")}</p>
          )}
          {device.managed && (
            <p className="mt-1 text-[11px] text-muted-foreground">
              {t("settingsPages.mobileDevice.managed.help")}
            </p>
          )}
        </div>
        <div className="flex shrink-0 gap-2">
          {editable && (
            <>
              <Button variant="outline" size="sm" onClick={connect} disabled={connecting}>
                {connecting ? (
                  <Loader2 className="mr-2 h-4 w-4 animate-spin" />
                ) : (
                  <Smartphone className="mr-2 h-4 w-4" />
                )}
                {t("settingsPages.mobileDevice.reconnect")}
              </Button>
              <Button variant="outline" size="sm" onClick={() => setOpen((v) => !v)}>
                {open ? <ChevronUp className="h-4 w-4" /> : <ChevronDown className="h-4 w-4" />}
              </Button>
              {!device.managed && (
                <Button variant="outline" size="sm" onClick={() => setRemoving(true)}>
                  <Trash2 className="h-4 w-4" />
                </Button>
              )}
            </>
          )}
        </div>
      </div>

      {open && editable && (
        <div className="mt-4 space-y-4 border-t pt-4">
          <Section
            title={t("settingsPages.mobileDevice.address.title")}
            hint={t("settingsPages.mobileDevice.address.hint")}
          >
            <div className="grid gap-3 sm:grid-cols-2">
              <Field
                label={t("settingsPages.mobileDevice.fields.deviceAddr")}
                help={t("settingsPages.mobileDevice.fields.deviceAddrHelp")}
              >
                <Input
                  value={deviceAddr}
                  onChange={(e) => setDeviceAddr(e.target.value)}
                  placeholder="100.84.12.7:37241"
                />
              </Field>
              <Field
                label={t("settingsPages.mobileDevice.fields.pin")}
                help={
                  device.has_pin
                    ? t("settingsPages.mobileDevice.fields.pinStored")
                    : t("settingsPages.mobileDevice.fields.pinHelp")
                }
              >
                <Input
                  type="password"
                  value={pin}
                  onChange={(e) => setPin(e.target.value)}
                  placeholder="••••"
                  autoComplete="off"
                />
              </Field>
            </div>
            <div className="mt-3">
              <Button size="sm" onClick={save} disabled={saving || !deviceAddr.trim()}>
                <Save className="mr-2 h-4 w-4" />
                {saving ? t("common.saving") : t("common.save")}
              </Button>
            </div>
          </Section>

          <Section
            title={t("settingsPages.mobileDevice.pairing.title")}
            hint={t("settingsPages.mobileDevice.pairing.hint")}
          >
            <div className="grid gap-3 sm:grid-cols-2">
              <Field
                label={t("settingsPages.mobileDevice.fields.pairAddr")}
                help={t("settingsPages.mobileDevice.fields.pairAddrHelp")}
              >
                <Input
                  value={pairAddr}
                  onChange={(e) => setPairAddr(e.target.value)}
                  placeholder="100.84.12.7:41234"
                />
              </Field>
              <Field
                label={t("settingsPages.mobileDevice.fields.pairCode")}
                help={t("settingsPages.mobileDevice.fields.pairCodeHelp")}
              >
                <Input
                  value={pairCode}
                  onChange={(e) => setPairCode(e.target.value)}
                  placeholder="123456"
                  inputMode="numeric"
                />
              </Field>
            </div>
            <div className="mt-3">
              <Button
                size="sm"
                onClick={pair}
                disabled={pairing || pairAddr.trim() === "" || pairCode.trim().length !== 6}
              >
                {pairing ? (
                  <Loader2 className="mr-2 h-4 w-4 animate-spin" />
                ) : (
                  <Link2 className="mr-2 h-4 w-4" />
                )}
                {t("settingsPages.mobileDevice.pair")}
              </Button>
            </div>
          </Section>
        </div>
      )}

      <ConfirmDialog
        open={removing}
        onOpenChange={setRemoving}
        title={t("settingsPages.mobileDevice.remove.title")}
        description={t("settingsPages.mobileDevice.remove.description", { name: device.name })}
        confirmLabel={t("settingsPages.mobileDevice.unregister")}
        onConfirm={remove}
      />
    </Card>
  );
}

/**
 * The platform question comes first and is answered with two visible tiles
 * rather than a single-option select, because the question people actually
 * arrive with is "can I attach my iPhone" — and a form that only ever offers
 * Android reads as an unfinished feature, so they go and ask. The disabled iOS
 * tile answers it in place: not yet, and here is the reason it cannot be
 * arranged locally.
 */
function AddDeviceDialog({
  open,
  onOpenChange,
  onDevices,
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  onDevices: (devices: MobileDeviceStatus[]) => void;
}) {
  const { t } = useI18n();
  const [name, setName] = useState("");
  const [deviceAddr, setDeviceAddr] = useState("");
  const [pin, setPin] = useState("");
  const [saving, setSaving] = useState(false);

  // Reset on close so a half-typed device never bleeds into the next one.
  useEffect(() => {
    if (!open) {
      setName("");
      setDeviceAddr("");
      setPin("");
    }
  }, [open]);

  const submit = async () => {
    setSaving(true);
    try {
      const res = await api.createMobileDevice({
        name: name.trim(),
        platform: "android",
        device_addr: deviceAddr.trim(),
        // Same pointer semantics as the edit form: an empty box stores no PIN
        // rather than storing an empty one.
        device_pin: pin === "" ? null : pin,
      });
      onDevices(res.devices);
      onOpenChange(false);
      toast.success(t("settingsPages.mobileDevice.added"));
    } catch (e) {
      toast.error(e instanceof Error ? e.message : t("settingsPages.mobileDevice.addFailed"));
    } finally {
      setSaving(false);
    }
  };

  return (
    <FormDialog
      open={open}
      onOpenChange={onOpenChange}
      title={t("settingsPages.mobileDevice.addDialog.title")}
      description={t("settingsPages.mobileDevice.addDialog.description")}
      footer={
        <Button onClick={submit} disabled={saving || !name.trim() || !deviceAddr.trim()}>
          {saving ? <Loader2 className="mr-2 h-4 w-4 animate-spin" /> : <Plus className="mr-2 h-4 w-4" />}
          {t("settingsPages.mobileDevice.addDialog.submit")}
        </Button>
      }
    >
      <div className="space-y-2">
        <Label className="text-xs">{t("settingsPages.mobileDevice.addDialog.platform")}</Label>
        <div className="grid gap-2 sm:grid-cols-2">
          <Button variant="default" className="justify-start" aria-pressed>
            <Smartphone className="mr-2 h-4 w-4" />
            {t("settingsPages.mobileDevice.addDialog.android")}
          </Button>
          <Button variant="outline" className="justify-start" disabled>
            <Smartphone className="mr-2 h-4 w-4" />
            {t("settingsPages.mobileDevice.addDialog.ios")}
            <Badge variant="secondary" className="ml-2">
              {t("settingsPages.mobileDevice.addDialog.iosSoon")}
            </Badge>
          </Button>
        </div>
        <Notice variant="info" title={t("settingsPages.mobileDevice.addDialog.ios")}>
          {t("settingsPages.mobileDevice.addDialog.iosWhy")}
        </Notice>
      </div>

      <Field
        label={t("settingsPages.mobileDevice.fields.name")}
        help={t("settingsPages.mobileDevice.fields.nameHelp")}
      >
        <Input
          value={name}
          onChange={(e) => setName(e.target.value)}
          placeholder={t("settingsPages.mobileDevice.fields.namePlaceholder")}
        />
      </Field>
      <Field
        label={t("settingsPages.mobileDevice.fields.deviceAddr")}
        help={t("settingsPages.mobileDevice.fields.deviceAddrHelp")}
      >
        <Input
          value={deviceAddr}
          onChange={(e) => setDeviceAddr(e.target.value)}
          placeholder="100.84.12.7:37241"
        />
      </Field>
      <Field
        label={t("settingsPages.mobileDevice.fields.pin")}
        help={t("settingsPages.mobileDevice.fields.pinHelp")}
      >
        <Input
          type="password"
          value={pin}
          onChange={(e) => setPin(e.target.value)}
          placeholder="••••"
          autoComplete="off"
        />
      </Field>
    </FormDialog>
  );
}

/** Phone-side or repository-side guidance that belongs to no single device. */
function GuidanceCard({ title, hint, body }: { title: string; hint: string; body: string }) {
  return (
    <Card className="mt-4 w-full p-6">
      <p className="text-sm font-medium text-foreground">{title}</p>
      <p className="text-xs text-muted-foreground">{hint}</p>
      <p className="mt-3 text-sm text-muted-foreground">{body}</p>
    </Card>
  );
}

/** One titled block of the per-device form, carrying its where-to-look hint. */
function Section({ title, hint, children }: { title: string; hint: string; children: React.ReactNode }) {
  return (
    <div>
      <p className="text-sm font-medium text-foreground">{title}</p>
      <p className="mb-3 text-xs text-muted-foreground">{hint}</p>
      {children}
    </div>
  );
}

function Field({ label, help, children }: { label: string; help: string; children: React.ReactNode }) {
  return (
    <div className="space-y-1">
      <Label className="text-xs">{label}</Label>
      {children}
      <p className="text-[11px] text-muted-foreground">{help}</p>
    </div>
  );
}

/**
 * Busy is deliberately not offline. A leased phone is a healthy phone that
 * someone else's run is holding, and collapsing the two teaches operators to go
 * and physically poke a device that is working exactly as intended.
 */
function StatusBadge({ device }: { device: MobileDeviceStatus }) {
  const { t } = useI18n();
  if (!device.configured) {
    return <Badge variant="secondary">{t("settingsPages.mobileDevice.status.notConfigured")}</Badge>;
  }
  if (device.device_busy) {
    return <Badge variant="secondary">{t("settingsPages.mobileDevice.status.busy")}</Badge>;
  }
  if (device.device_online) {
    return <Badge variant="success">{t("settingsPages.mobileDevice.status.online")}</Badge>;
  }
  return <Badge variant="destructive">{t("settingsPages.mobileDevice.status.offline")}</Badge>;
}
