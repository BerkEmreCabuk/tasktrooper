// Package mobiledevice owns the registered test devices: what is stored, what
// the settings page shows, and the pair/connect steps that used to require a
// shell inside the Appium pod.
//
// A device now has a KIND (domain.DeviceKind*), and the kind is what this
// package switches on. It exists because the same installation can be a cluster
// tenant driving a physical phone through the adb bridge, or a tenant running
// on somebody's Mac driving an iOS simulator and an Android emulator that live
// on that very machine. Registration, connect, status and removal mean
// different things in those two worlds; everything downstream — the eleven
// mobile_* tools, the lease, the parked-task sweep, the QA grounding gates —
// means exactly the same thing, and that is the property this design protects.
package mobiledevice

import (
	"context"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/rs/zerolog/log"

	"github.com/makifbaysal/tasktrooper/server/internal/adapter/deviceagent"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

// Store is the persisted registrations.
type Store interface {
	List(ctx context.Context) ([]domain.MobileDevice, error)
	Get(ctx context.Context, id uuid.UUID) (domain.MobileDevice, bool, error)
	Create(ctx context.Context, in domain.MobileDevice, pin, token *string) (domain.MobileDevice, error)
	Update(ctx context.Context, in domain.MobileDevice, pin, token *string) (domain.MobileDevice, error)
	MarkConnected(ctx context.Context, id uuid.UUID) error
	Delete(ctx context.Context, id uuid.UUID) error
}

// ReloadFunc rebuilds the live device sessions from the registrations.
//
// It exists for the same reason llmprovider has one: a value changed in the
// browser has to take effect without a pod restart, and the thing that has to
// change is not a config struct but live sessions other goroutines are already
// holding. The runtime owns that swap; this package only says when.
type ReloadFunc func(ctx context.Context, devices []domain.MobileDevice) error

// HubProbe answers whether a given phone is reachable and idle. Implemented by
// the live session pool, which is also the thing that would hold the lease — so
// "busy" here means a run has that phone, not that anything is wrong.
//
// Per UDID rather than per hub, and that is the correction this change is
// really about: one Appium hub with one adb server drives several UDIDs at
// once, so a single "is the hub free" answer would have parked every task the
// moment any one phone was taken.
type HubProbe interface {
	Configured() bool
	ProbeDevice(ctx context.Context, udid string) bool
	Busy(udid string) bool
}

// LocalHost is the machine this process runs on, as far as simulators and
// emulators are concerned. Implemented by adapter/localdevice.
//
// It is the local counterpart of the deviceagent bridge and is deliberately
// shaped the same way: ask what is there, attach one, ask whether it is up,
// detach it. A nil LocalHost is a host with nothing local on it, which is what
// every cluster node is — so every method below has to survive one.
type LocalHost interface {
	SupportsIOSSimulators(ctx context.Context) bool
	SupportsAndroidEmulators(ctx context.Context) bool
	Catalog(ctx context.Context) (domain.LocalDeviceCatalog, error)

	Simulator(ctx context.Context, udid string) (domain.LocalSimulator, bool, error)
	SimulatorBooted(ctx context.Context, udid string) (bool, error)
	BootSimulator(ctx context.Context, udid string) error
	ShutdownSimulator(ctx context.Context, udid string) error

	AVDExists(ctx context.Context, name string) (bool, error)
	EmulatorSerial(ctx context.Context, avd string) (string, error)
	StartEmulator(ctx context.Context, avd string) (string, error)
	StopEmulator(ctx context.Context, serial string) error
}

// Service is the settings-facing use case.
//
// The env-configured device stays supported and stays authoritative about one
// thing only: it is what runs when nothing has been registered. Once an
// operator registers a phone in the UI, the registrations win — someone who
// just attached devices in the browser and is told a stale env var is driving a
// different one has been given a setting that does not set anything.
type Service struct {
	store  Store
	bridge *deviceagent.Client
	reload ReloadFunc
	// envDevice is tools.mobile from config.yml, empty when unset.
	envDevice domain.MobileDevice
	// probe reports on the live sessions; nil before the runtime wires one.
	probe HubProbe
	// host answers for simulators and emulators on this machine; nil where
	// nothing was wired, which behaves identically to a host that has none.
	host LocalHost
}

func NewService(store Store, bridge *deviceagent.Client, envDevice domain.MobileDevice) *Service {
	if envDevice.Configured() {
		envDevice.Platform = domain.PlatformAndroid
		if envDevice.Name == "" {
			// Named rather than blank because it now appears in a list beside
			// devices that do have names, and an unlabelled row in a list of
			// phones is the one nobody can act on.
			envDevice.Name = "Ortam cihazı"
		}
	}
	return &Service{store: store, bridge: bridge, envDevice: envDevice}
}

func (s *Service) SetReloader(fn ReloadFunc) { s.reload = fn }
func (s *Service) SetProbe(p HubProbe)       { s.probe = p }

// SetLocalHost wires the simulators and emulators on this machine. A setter
// rather than a constructor argument for the same reason SetProbe is one: it is
// optional wiring the runtime adds, and every existing caller — the cluster
// path and eight tests — must keep constructing a service without it.
func (s *Service) SetLocalHost(h LocalHost) { s.host = h }

// LocalCatalog is what this host could drive: the simulators and AVDs it has.
// Two empty arrays where there is no host, which is the answer a Linux node
// gives and the one the settings UI renders as "nothing local here".
func (s *Service) LocalCatalog(ctx context.Context) (domain.LocalDeviceCatalog, error) {
	if s == nil || s.host == nil {
		return domain.EmptyLocalDeviceCatalog(), nil
	}
	return s.host.Catalog(ctx)
}

// Effective returns the devices the agents should actually be driving: the
// registered ones, or the environment's when nothing is registered.
//
// All-or-nothing rather than a union, deliberately. The env device and a
// registration normally describe the SAME phone — 127.0.0.1:5555 either way —
// so merging them would put one phone in the pool twice and let two runs take
// what they each believed was a free device.
func (s *Service) Effective(ctx context.Context) ([]domain.MobileDevice, string, error) {
	if s == nil || s.store == nil {
		return s.envFallback(), "env", nil
	}
	stored, err := s.store.List(ctx)
	if err != nil {
		return nil, "", err
	}
	var configured []domain.MobileDevice
	for _, d := range stored {
		if d.Configured() {
			configured = append(configured, d)
		}
	}
	if len(configured) > 0 {
		return configured, "settings", nil
	}
	return s.envFallback(), "env", nil
}

func (s *Service) envFallback() []domain.MobileDevice {
	if !s.envDevice.Configured() {
		return nil
	}
	return []domain.MobileDevice{s.envDevice}
}

// List is every device the settings page should show: the registrations, plus
// the env-configured one when there are none. A registration that is incomplete
// (saved but never connected, so the bridge has not told us its local address)
// is included rather than hidden — it is exactly the row whose "reconnect"
// button the operator has to press.
func (s *Service) List(ctx context.Context) ([]domain.MobileDevice, error) {
	if s.store == nil {
		return s.envFallback(), nil
	}
	stored, err := s.store.List(ctx)
	if err != nil {
		return nil, err
	}
	if len(stored) == 0 {
		return s.envFallback(), nil
	}
	return stored, nil
}

// Statuses is the settings page's whole read model: what is registered,
// whether the hub answers, and whether each phone is actually on the other end
// of it.
//
// The two probes are separate questions with separate fixes. An unreachable hub
// is a cluster problem; a reachable hub with no device is a phone problem —
// asleep, off the tailnet, or showing an allow-debugging prompt nobody tapped.
// Collapsing them into one green dot sends the operator to the wrong place.
func (s *Service) Statuses(ctx context.Context) ([]domain.MobileDeviceStatus, error) {
	devices, err := s.List(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]domain.MobileDeviceStatus, 0, len(devices))
	for _, d := range devices {
		out = append(out, s.statusOf(ctx, d))
	}
	return out, nil
}

func (s *Service) statusOf(ctx context.Context, device domain.MobileDevice) domain.MobileDeviceStatus {
	source := "settings"
	if device.Managed() {
		source = "env"
	}
	status := domain.MobileDeviceStatus{
		ID:              idString(device.ID),
		Name:            device.Name,
		Platform:        platformOr(device.Platform),
		Kind:            device.DeviceKind(),
		Managed:         device.Managed(),
		Configured:      device.Configured(),
		HubURL:          device.HubURL,
		DeviceUDID:      device.DeviceUDID,
		PlatformVersion: device.PlatformVersion,
		DeviceAddr:      device.DeviceAddr,
		HasPIN:          device.DevicePIN != "",
		HasToken:        device.HubToken != "",
		LastConnectedAt: device.LastConnectedAt,
		Source:          source,
		HubManaged:      s.envDevice.HubURL != "",
	}
	if status.HubURL == "" {
		// Not registered with a hub of its own: still tell the page which hub it
		// will get, rather than showing an empty promise.
		status.HubURL = s.envDevice.HubURL
	}
	if s.probe != nil && s.probe.Configured() && device.DeviceUDID != "" {
		// ProbeDevice reports "free", which is a stronger claim than
		// "reachable": it answered AND nothing holds it. A held device is
		// reachable too, which is why busy is asked separately instead of being
		// inferred from a failed probe.
		switch {
		case s.probe.ProbeDevice(ctx, device.DeviceUDID):
			status.HubReachable = true
		case s.probe.Busy(device.DeviceUDID):
			status.HubReachable, status.DeviceBusy = true, true
		default:
			// Not free and not held here: either another pod has it or the hub
			// is down, and adb is the only one that can tell those apart.
			status.HubReachable, status.DeviceBusy = s.hubHeld(ctx, device)
		}
	}
	// "Is the device actually there" is asked of whoever owns it. For a bridge
	// phone that is adb in the cluster; for a local device it is this machine,
	// and asking the bridge about a simulator UDID would report every simulator
	// as offline on a host that has no bridge at all.
	if device.Local() {
		status.DeviceOnline, status.Detail = s.localOnline(ctx, device)
		return status
	}
	if s.bridge.Configured() && device.DeviceAddr != "" {
		bridgeStatus, berr := s.bridge.Status(ctx, device.DeviceAddr)
		if berr != nil {
			status.Detail = berr.Error()
		} else {
			status.DeviceOnline = bridgeStatus.Online
			if !bridgeStatus.Online && bridgeStatus.Detail != "" {
				status.Detail = bridgeStatus.Detail
			}
		}
	}
	return status
}

// localOnline answers the device-online question for a simulator or an emulator.
//
// "Not booted" is reported as offline with no Detail rather than as a fault: a
// shut-down simulator is the resting state of a registered device, and a red
// dot with an error string next to it would send the operator looking for a
// problem that does not exist.
func (s *Service) localOnline(ctx context.Context, device domain.MobileDevice) (online bool, detail string) {
	if s.host == nil {
		return false, "this host has no local simulators or emulators"
	}
	switch device.DeviceKind() {
	case domain.DeviceKindIOSSimulator:
		booted, err := s.host.SimulatorBooted(ctx, localTargetOf(device))
		if err != nil {
			return false, err.Error()
		}
		return booted, ""
	case domain.DeviceKindAndroidEmulator:
		serial, err := s.host.EmulatorSerial(ctx, localTargetOf(device))
		if err != nil {
			return false, err.Error()
		}
		return serial != "", ""
	}
	return false, ""
}

// localTargetOf is the operator-supplied identity of a local device: the
// simulator UDID or the AVD name. It lives in DeviceAddr — see
// domain.MobileDevice.DeviceAddr — with DeviceUDID as the fallback for a
// simulator, whose two fields hold the same string.
func localTargetOf(device domain.MobileDevice) string {
	if addr := strings.TrimSpace(device.DeviceAddr); addr != "" {
		return addr
	}
	return strings.TrimSpace(device.DeviceUDID)
}

// hubHeld distinguishes "a run has the phone" from "the hub is not there". It
// leans on the bridge: if adb still lists the device, the hub is up and the
// lease is simply taken.
func (s *Service) hubHeld(ctx context.Context, device domain.MobileDevice) (reachable, busy bool) {
	if device.Local() {
		// The host is this kind's adb: if the simulator is booted or the AVD
		// has a serial, the hub answered and the lease is simply somebody
		// else's — a pod restart that lost the lease map, or Appium's own reap
		// window not yet elapsed.
		online, _ := s.localOnline(ctx, device)
		return online, online
	}
	if !s.bridge.Configured() || device.DeviceAddr == "" {
		return false, false
	}
	st, err := s.bridge.Status(ctx, device.DeviceAddr)
	if err != nil || !st.Online {
		return false, false
	}
	return true, true
}

func idString(id uuid.UUID) string {
	if id == uuid.Nil {
		return ""
	}
	return id.String()
}

func platformOr(p string) string {
	if p == "" {
		return domain.PlatformAndroid
	}
	return p
}

// Add registers a new phone.
//
// It does not take a UDID, and no caller may supply one: the local address is
// allocated by the bridge, which is the only party that knows which loopback
// ports it has already handed out. So the registration is written, the bridge
// is asked to connect, and whatever local address it reports back becomes the
// UDID Appium is driven with. A phone that cannot be reached yet is still
// stored — the address may simply be stale, and losing the operator's typing
// because a phone was asleep is worse than a row with a reconnect button.
// A local kind adds one step and removes another: the target has to EXIST on
// this host (a UDID nobody can boot is a registration that can only ever fail),
// and there is no bridge to allocate an address, so the UDID is known up front
// for a simulator and comes from the boot for an emulator.
func (s *Service) Add(ctx context.Context, req domain.SaveMobileDeviceRequest) ([]domain.MobileDeviceStatus, error) {
	if s.store == nil {
		return nil, fmt.Errorf("mobile device registration is not available in this build")
	}
	kind, ok := domain.ValidDeviceKind(req.Kind)
	if !ok {
		return nil, fmt.Errorf("unknown device kind %q — expected remote_adb, ios_simulator or android_emulator", req.Kind)
	}
	name := strings.TrimSpace(req.Name)
	if name == "" {
		return nil, fmt.Errorf("give the device a name")
	}
	device, err := s.resolveTarget(ctx, kind, req)
	if err != nil {
		return nil, err
	}
	hub, token, err := s.hubFor(req)
	if err != nil {
		return nil, err
	}
	device.Name = name
	device.HubURL = hub
	if v := strings.TrimSpace(req.PlatformVersion); v != "" {
		// An explicit version still wins over the one derived from the
		// simulator's runtime: an operator who typed one is testing something
		// about that version, and overwriting it would silently ignore them.
		device.PlatformVersion = v
	}
	created, err := s.store.Create(ctx, device, req.DevicePIN, token)
	if err != nil {
		return nil, err
	}
	if _, cerr := s.connect(ctx, created); cerr != nil {
		log.Info().Err(cerr).Str("device", created.Name).Msg("mobile device: registered but not reachable yet")
	}
	return s.reloadAndReport(ctx)
}

// resolveTarget validates what the request points at and returns the kind's
// share of the row: platform, kind, addr, udid and (for a simulator) the
// platform version its runtime implies.
//
// It is one function rather than three because the three cases have to stay
// visibly parallel — every one of them answers "what did the operator choose,
// and does it exist" — and because Update needs exactly the same answers when
// a registration is re-pointed at a different simulator or AVD.
func (s *Service) resolveTarget(ctx context.Context, kind string, req domain.SaveMobileDeviceRequest) (domain.MobileDevice, error) {
	device := domain.MobileDevice{Kind: kind, Platform: domain.PlatformForKind(kind)}
	switch kind {
	case domain.DeviceKindIOSSimulator:
		if s.host == nil || !s.host.SupportsIOSSimulators(ctx) {
			// The same refusal the cluster has always given, now stated as a
			// fact about THIS host: Appium's XCUITest driver needs macOS with
			// Xcode, and this machine is not one.
			return device, fmt.Errorf("this host has no iOS simulators: Appium's XCUITest driver needs a macOS host with Xcode, and this one is not")
		}
		udid := firstNonEmpty(req.DeviceUDID, req.DeviceAddr)
		if udid == "" {
			return device, fmt.Errorf("choose a simulator — its UDID comes from the local device catalog")
		}
		sim, found, err := s.host.Simulator(ctx, udid)
		if err != nil {
			return device, err
		}
		if !found {
			return device, fmt.Errorf("no simulator with UDID %s on this host", udid)
		}
		// Both fields hold the UDID: simctl's identity is stable, so there is
		// no bridge-style mapping step and nothing for them to disagree about.
		device.DeviceUDID, device.DeviceAddr = sim.UDID, sim.UDID
		device.PlatformVersion = runtimeVersion(sim.Runtime)
		return device, nil

	case domain.DeviceKindAndroidEmulator:
		if s.host == nil || !s.host.SupportsAndroidEmulators(ctx) {
			return device, fmt.Errorf("this host has no Android emulators — install the Android SDK's platform-tools and emulator, or attach a physical device instead")
		}
		avd := firstNonEmpty(req.DeviceAddr, req.DeviceUDID)
		if avd == "" {
			return device, fmt.Errorf("choose an AVD — its name comes from the local device catalog")
		}
		exists, err := s.host.AVDExists(ctx, avd)
		if err != nil {
			return device, err
		}
		if !exists {
			return device, fmt.Errorf("no AVD named %q on this host", avd)
		}
		// No UDID yet on purpose. It is the adb serial the emulator comes up
		// on, allocated by the emulator at boot, so inventing one here would be
		// the same mistake as guessing a bridge port.
		device.DeviceAddr = avd
		return device, nil

	default:
		platform, ok := domain.ValidPlatform(req.Platform)
		if !ok {
			return device, fmt.Errorf("only Android devices can be driven from this installation: Appium's iOS driver needs a macOS host, and the cluster has none")
		}
		addr := strings.TrimSpace(req.DeviceAddr)
		if addr == "" {
			return device, fmt.Errorf("the device address is required — it is the phone's Tailscale address plus the port on its wireless debugging screen")
		}
		device.Platform, device.DeviceAddr = platform, addr
		return device, nil
	}
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if t := strings.TrimSpace(v); t != "" {
			return t
		}
	}
	return ""
}

// runtimeVersion is the numeric half of a simctl runtime name — "iOS 17.4" →
// "17.4" — which is what Appium's platformVersion capability wants. Derived
// rather than typed: simctl knows the version exactly, and MOBILE_PLATFORM_VERSION
// is a cluster-wide value that says nothing about which simulator this is.
func runtimeVersion(runtime string) string {
	fields := strings.Fields(runtime)
	if len(fields) == 0 {
		return ""
	}
	return fields[len(fields)-1]
}

// Update rewrites one registration — in practice, the new port a rebooted phone
// came back on.
func (s *Service) Update(ctx context.Context, id uuid.UUID, req domain.SaveMobileDeviceRequest) ([]domain.MobileDeviceStatus, error) {
	device, err := s.registered(ctx, id)
	if err != nil {
		return nil, err
	}
	if name := strings.TrimSpace(req.Name); name != "" {
		device.Name = name
	}
	if addr := firstNonEmpty(req.DeviceAddr, req.DeviceUDID); addr != "" {
		if device.Local() {
			// Re-pointing a local registration at a different simulator or AVD
			// goes through the same existence check the registration did.
			// Editing is where a typo actually happens — the add form offers a
			// list, the edit form offers a text field — so the validation that
			// matters most is the one on this path.
			retargeted, rerr := s.resolveTarget(ctx, device.DeviceKind(), req)
			if rerr != nil {
				return nil, rerr
			}
			device.DeviceAddr, device.DeviceUDID = retargeted.DeviceAddr, retargeted.DeviceUDID
			if retargeted.PlatformVersion != "" {
				device.PlatformVersion = retargeted.PlatformVersion
			}
		} else {
			device.DeviceAddr = addr
		}
	}
	if v := strings.TrimSpace(req.PlatformVersion); v != "" {
		device.PlatformVersion = v
	}
	if hub := strings.TrimSpace(req.HubURL); hub != "" {
		device.HubURL = hub
	}
	if _, err := s.store.Update(ctx, device, req.DevicePIN, req.HubToken); err != nil {
		return nil, err
	}
	// Re-connect rather than only saving: the reason an address is edited is
	// almost always that the phone moved, and a save that leaves adb pointed at
	// the old port shows a green form above an offline device.
	if _, cerr := s.connect(ctx, device); cerr != nil {
		log.Info().Err(cerr).Str("device", device.Name).Msg("mobile device: saved but not reachable yet")
	}
	return s.reloadAndReport(ctx)
}

// Remove unregisters one phone and tells adb to let go of it.
func (s *Service) Remove(ctx context.Context, id uuid.UUID) ([]domain.MobileDeviceStatus, error) {
	device, err := s.registered(ctx, id)
	if err != nil {
		return nil, err
	}
	if derr := s.disconnect(ctx, device); derr != nil {
		log.Warn().Err(derr).Msg("mobile device: disconnect on unregister failed")
	}
	if err := s.store.Delete(ctx, id); err != nil {
		return nil, err
	}
	return s.reloadAndReport(ctx)
}

// Pair runs the one-time wireless-debugging pairing for one phone, then
// immediately connects it: a pairing that is not followed by a connect leaves
// the operator looking at a success message and an offline device, which reads
// as a failure.
func (s *Service) Pair(ctx context.Context, id uuid.UUID, req domain.PairMobileDeviceRequest) ([]domain.MobileDeviceStatus, error) {
	if !s.bridge.Configured() {
		return nil, fmt.Errorf("no device bridge is configured — pairing must be done on the Appium host")
	}
	device, err := s.registered(ctx, id)
	if err != nil {
		return nil, err
	}
	if device.Local() {
		// Wireless-debugging pairing is a thing a physical phone does. A
		// simulator and an emulator are processes this host starts, so there is
		// no dialog, no six-digit code, and nothing a pairing could achieve.
		return nil, fmt.Errorf("pairing applies to a physical phone — this device runs on the host and only needs connecting")
	}
	res, err := s.bridge.Pair(ctx, strings.TrimSpace(req.PairAddr), strings.TrimSpace(req.Code))
	if err != nil {
		return nil, err
	}
	if !res.OK {
		return nil, fmt.Errorf("pairing failed: %s", res.Detail)
	}
	if _, cerr := s.connect(ctx, device); cerr != nil {
		log.Warn().Err(cerr).Msg("mobile device: connect after pairing failed")
	}
	return s.reloadAndReport(ctx)
}

// Connect (re)attaches adb to an already-paired phone — the step needed after
// the device rebooted or dropped off the tailnet.
func (s *Service) Connect(ctx context.Context, id uuid.UUID) ([]domain.MobileDeviceStatus, error) {
	device, err := s.registered(ctx, id)
	if err != nil {
		return nil, err
	}
	if _, cerr := s.connect(ctx, device); cerr != nil {
		return nil, cerr
	}
	return s.reloadAndReport(ctx)
}

// connect attaches one phone and records what the bridge mapped it onto.
//
// Storing the reported local address is the whole of multi-device wiring on
// this side: the bridge allocates a loopback port per phone, and a registration
// that guessed one would drive whichever device happens to answer there.
func (s *Service) connect(ctx context.Context, device domain.MobileDevice) (domain.MobileDevice, error) {
	switch device.DeviceKind() {
	case domain.DeviceKindIOSSimulator:
		return s.connectSimulator(ctx, device)
	case domain.DeviceKindAndroidEmulator:
		return s.connectEmulator(ctx, device)
	}
	if device.DeviceAddr == "" {
		return device, fmt.Errorf("no device address is registered")
	}
	if !s.bridge.Configured() {
		return device, fmt.Errorf("no device bridge is configured")
	}
	res, err := s.bridge.Connect(ctx, device.DeviceAddr)
	if err != nil {
		return device, err
	}
	if !res.OK {
		return device, fmt.Errorf("could not connect: %s", res.Detail)
	}
	if s.store == nil || device.Managed() {
		return device, nil
	}
	if local := strings.TrimSpace(res.LocalAddr); local != "" && local != device.DeviceUDID {
		device.DeviceUDID = local
		if _, err := s.store.Update(ctx, device, nil, nil); err != nil {
			return device, err
		}
	}
	if err := s.store.MarkConnected(ctx, device.ID); err != nil {
		log.Warn().Err(err).Msg("mobile device: stamping the connection failed")
	}
	return device, nil
}

// connectSimulator boots the simulator and, while it is at it, refreshes the
// platform version from the runtime simctl reports.
//
// The refresh matters because the version can change under a registration: an
// Xcode update replaces the runtime a simulator UDID belongs to, and a session
// asking for iOS 17.4 on a device that is now 18.0 fails at capability matching
// with a message about a version nobody typed.
func (s *Service) connectSimulator(ctx context.Context, device domain.MobileDevice) (domain.MobileDevice, error) {
	if s.host == nil {
		return device, fmt.Errorf("this host has no iOS simulators")
	}
	udid := localTargetOf(device)
	if udid == "" {
		return device, fmt.Errorf("no simulator is registered")
	}
	if err := s.host.BootSimulator(ctx, udid); err != nil {
		return device, err
	}
	// The UDID is both the identity and the address for a simulator, so a
	// registration written before the boot still has to end up with both set.
	changed := device.DeviceUDID != udid
	device.DeviceUDID = udid
	if sim, found, err := s.host.Simulator(ctx, udid); err == nil && found {
		if v := runtimeVersion(sim.Runtime); v != "" && v != device.PlatformVersion {
			device.PlatformVersion, changed = v, true
		}
	}
	return s.stampConnected(ctx, device, changed)
}

// connectEmulator starts the AVD if it is not already up and records the adb
// serial it came back on.
//
// Recording the serial is the whole of the emulator's wiring on this side, and
// it is the exact analogue of storing the bridge's local address: the serial is
// allocated at boot from the console-port range, so an AVD that was restarted is
// on a different one, and a registration that remembered the old serial would
// drive whichever emulator happens to be there now.
func (s *Service) connectEmulator(ctx context.Context, device domain.MobileDevice) (domain.MobileDevice, error) {
	if s.host == nil {
		return device, fmt.Errorf("this host has no Android emulators")
	}
	avd := localTargetOf(device)
	if avd == "" {
		return device, fmt.Errorf("no AVD is registered")
	}
	serial, err := s.host.StartEmulator(ctx, avd)
	if err != nil {
		return device, err
	}
	if serial == "" {
		return device, fmt.Errorf("emulator %q reported no adb serial", avd)
	}
	changed := serial != device.DeviceUDID
	device.DeviceUDID = serial
	return s.stampConnected(ctx, device, changed)
}

// stampConnected persists whatever the connect learned and marks the device as
// having actually answered. Shared by both local kinds because the difference
// between them ends the moment the device is up.
func (s *Service) stampConnected(ctx context.Context, device domain.MobileDevice, changed bool) (domain.MobileDevice, error) {
	if s.store == nil || device.Managed() {
		return device, nil
	}
	if changed {
		if _, err := s.store.Update(ctx, device, nil, nil); err != nil {
			return device, err
		}
	}
	if err := s.store.MarkConnected(ctx, device.ID); err != nil {
		log.Warn().Err(err).Msg("mobile device: stamping the connection failed")
	}
	return device, nil
}

// disconnect lets go of one device, by whatever means its kind has.
//
// It is best-effort at every call site: the device is being unregistered, and a
// simulator that will not shut down must not leave a row nobody can delete.
func (s *Service) disconnect(ctx context.Context, device domain.MobileDevice) error {
	switch device.DeviceKind() {
	case domain.DeviceKindIOSSimulator:
		if s.host == nil {
			return nil
		}
		return s.host.ShutdownSimulator(ctx, localTargetOf(device))
	case domain.DeviceKindAndroidEmulator:
		if s.host == nil {
			return nil
		}
		serial := strings.TrimSpace(device.DeviceUDID)
		if serial == "" {
			// Never connected, or the serial was lost with a restart: ask the
			// host which serial the AVD is on rather than leaving a running
			// emulator behind because a column was blank.
			found, err := s.host.EmulatorSerial(ctx, localTargetOf(device))
			if err != nil {
				return err
			}
			serial = found
		}
		if serial == "" {
			return nil
		}
		return s.host.StopEmulator(ctx, serial)
	}
	if !s.bridge.Configured() || device.DeviceAddr == "" {
		return nil
	}
	_, err := s.bridge.Disconnect(ctx, device.DeviceAddr)
	return err
}

// registered resolves an id the UI sent. The env device is refused by name:
// its values live in a pod's environment, so a browser cannot edit them and an
// endpoint that pretended otherwise would silently discard the edit.
func (s *Service) registered(ctx context.Context, id uuid.UUID) (domain.MobileDevice, error) {
	if s.store == nil {
		return domain.MobileDevice{}, fmt.Errorf("mobile device registration is not available in this build")
	}
	if id == uuid.Nil {
		return domain.MobileDevice{}, fmt.Errorf("this device comes from the cluster's own configuration and cannot be changed here")
	}
	device, ok, err := s.store.Get(ctx, id)
	if err != nil {
		return domain.MobileDevice{}, err
	}
	if !ok {
		return domain.MobileDevice{}, fmt.Errorf("no such registered device")
	}
	return device, nil
}

// hubFor supplies the cluster's Appium hub when the request does not carry one.
//
// The hub address and its token are cluster wiring, not operator input: the
// Appium Service name is fixed by the deploy manifest and the token lives in
// the `appium-device` secret this pod already reads. Asking a person to retype
// either one only creates a way to get them wrong, so the form omits both. An
// explicit value in the request still wins, which keeps the endpoint usable
// outside the standard install.
func (s *Service) hubFor(req domain.SaveMobileDeviceRequest) (string, *string, error) {
	hub := strings.TrimSpace(req.HubURL)
	if hub == "" {
		hub = strings.TrimSpace(s.envDevice.HubURL)
	}
	if hub == "" {
		return "", nil, fmt.Errorf("the Appium hub address is not configured in this installation — set MOBILE_APPIUM_HUB_URL on the tenant")
	}
	token := req.HubToken
	if token == nil && s.envDevice.HubToken != "" {
		// nil means "keep whatever is stored", which for a value the cluster
		// owns would let a rotated secret stay shadowed by a stale copy in the
		// database.
		env := s.envDevice.HubToken
		token = &env
	}
	return hub, token, nil
}

func (s *Service) reloadAndReport(ctx context.Context) ([]domain.MobileDeviceStatus, error) {
	devices, _, err := s.Effective(ctx)
	if err == nil {
		s.applyReload(ctx, devices)
	}
	return s.Statuses(ctx)
}

func (s *Service) applyReload(ctx context.Context, devices []domain.MobileDevice) {
	if s.reload == nil {
		return
	}
	if err := s.reload(ctx, devices); err != nil {
		log.Warn().Err(err).Msg("mobile device: applying the new registrations failed")
	}
}
