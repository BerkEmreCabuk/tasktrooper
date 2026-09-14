package domain

import (
	"strings"
	"time"

	"github.com/google/uuid"
)

// PlatformAndroid is the platform a cluster-hosted installation can drive.
//
// Not an oversight and not a roadmap item that fell off: Appium's XCUITest
// driver has to run on a macOS host with Xcode on it, and the cluster has none.
// The value is stored anyway so the settings page can offer the choice and then
// say why one half of it is unavailable — a form that silently omits iOS reads
// as a product that forgot about it.
const PlatformAndroid = "android"

// PlatformIOS is drivable only where the HOST says so — see
// DeviceKindIOSSimulator. On a Linux cluster node it is still refused by name
// rather than as an unknown value, so the operator is told the reason instead
// of being told they typed something wrong.
const PlatformIOS = "ios"

// The device kinds. A kind is not a label: it is what every step of a device's
// life switches on — which binary attaches it, which Appium driver drives it,
// and whether the cluster's adb bridge is involved at all. It exists because
// the same eleven mobile_* tools now have to reach three different things.
const (
	// DeviceKindRemoteADB is a physical phone reached through the cluster's adb
	// bridge (adapter/deviceagent). The default, and what migration 102
	// backfills every existing row with: it is the only kind that existed when
	// those rows were written, so anything else would be a guess about
	// hardware nobody can see from here.
	DeviceKindRemoteADB = "remote_adb"

	// DeviceKindIOSSimulator is an `xcrun simctl` simulator on THIS host.
	// Available only where the host reports one — in practice a macOS machine
	// running the tenant locally, because Appium's XCUITest driver needs Xcode
	// and Xcode needs macOS. The bridge is never involved: there is no adb, and
	// the simulator is a process this host starts and stops itself.
	DeviceKindIOSSimulator = "ios_simulator"

	// DeviceKindAndroidEmulator is an Android SDK AVD on THIS host, reached
	// through the LOCAL adb rather than the bridge's. Same UiAutomator2 driver
	// and same capability set as a physical phone — from Appium's side an
	// emulator IS a phone — so only the attach/detach half differs.
	DeviceKindAndroidEmulator = "android_emulator"
)

// ValidDeviceKind normalises a requested kind. An empty value is remote_adb
// rather than an error, which is what keeps the settings API backward
// compatible: a client written before kinds existed sends no kind and still
// registers exactly the phone it always did.
func ValidDeviceKind(kind string) (string, bool) {
	switch strings.ToLower(strings.TrimSpace(kind)) {
	case "", DeviceKindRemoteADB:
		return DeviceKindRemoteADB, true
	case DeviceKindIOSSimulator:
		return DeviceKindIOSSimulator, true
	case DeviceKindAndroidEmulator:
		return DeviceKindAndroidEmulator, true
	default:
		return "", false
	}
}

// PlatformForKind is the platform a kind implies. The two are not independent:
// an iOS simulator is iOS and an AVD is Android, so asking the operator for
// both would only create a way to disagree with themselves.
func PlatformForKind(kind string) string {
	if kind == DeviceKindIOSSimulator {
		return PlatformIOS
	}
	return PlatformAndroid
}

// LocalDeviceKind reports whether a kind lives on this host rather than at the
// far end of the bridge. It is the one question most call sites actually have:
// local kinds are attached by exec'ing a binary here, remote ones by an HTTP
// call to the sidecar.
func LocalDeviceKind(kind string) bool {
	return kind == DeviceKindIOSSimulator || kind == DeviceKindAndroidEmulator
}

// LocalSimulator is one iOS simulator the host offers. Runtime is carried
// because it is where a simulator's platform version comes from: simctl knows
// it exactly, so making an operator retype MOBILE_PLATFORM_VERSION would only
// create a way to be wrong about a fact the machine already has.
type LocalSimulator struct {
	UDID    string `json:"udid"`
	Name    string `json:"name"`
	Runtime string `json:"runtime"`
	// State is simctl's own word — "Booted", "Shutdown". Reported rather than
	// reduced to a boolean so the UI can say what it is doing between the two.
	State string `json:"state,omitempty"`
	// PlatformVersion is the numeric half of Runtime, sent by a Mac that had
	// already split it (runner `mobile.devices`) and left empty by the local
	// host, which reports the runtime name only. Read it through
	// PlatformVersion(), never directly: the two sources fill different fields
	// with the same fact, and a caller that picked one would be right on one
	// machine and empty on the other.
	PlatformVersionField string `json:"platform_version,omitempty"`
}

// PlatformVersion is what `appium:platformVersion` wants: "17.4", not
// "iOS 17.4".
//
// Derived from Runtime when the reporter did not split it, because simctl knows
// the version exactly and making an operator retype it would only create a way
// to be wrong about a fact the machine already has. Absence stays absence: a
// simulator whose runtime name says nothing numeric gets "", and the capability
// is then simply omitted rather than guessed.
func (s LocalSimulator) PlatformVersion() string {
	if v := strings.TrimSpace(s.PlatformVersionField); v != "" {
		return v
	}
	fields := strings.Fields(s.Runtime)
	if len(fields) == 0 {
		return ""
	}
	return fields[len(fields)-1]
}

// LocalDeviceCatalog is what this host could drive if somebody registered it:
// the simulators simctl lists and the AVDs the Android SDK lists.
//
// Both slices are always non-nil. On a Linux node the answer is two empty
// arrays, and an empty array is a different statement from a null: it says the
// host was asked and has none, which is exactly what the settings page has to
// render.
type LocalDeviceCatalog struct {
	IOS     []LocalSimulator `json:"ios"`
	Android []string         `json:"android"`
}

// EmptyLocalDeviceCatalog is the answer for a host with nothing to offer.
func EmptyLocalDeviceCatalog() LocalDeviceCatalog {
	return LocalDeviceCatalog{IOS: []LocalSimulator{}, Android: []string{}}
}

// MobileDevice is one registered Android test device.
//
// It is the DB-backed twin of MobileConfig (config.go): same fields, but set
// from the settings UI instead of the environment. Registered devices win over
// the environment's — an operator who just attached a phone in the browser
// expects that to be the phone, and a stale env var driving a different one is
// a setting that does not set anything.
type MobileDevice struct {
	// ID is empty for the env-configured device, which has no row to have an id
	// in. That emptiness is load-bearing rather than sloppy: it is exactly what
	// says "this one is cluster configuration", which is why it cannot be
	// renamed or removed from the UI.
	ID uuid.UUID `json:"id,omitempty"`
	// Name is what the operator calls it. Required for registered devices,
	// because a list of three phones with no names names nothing.
	Name     string `json:"name,omitempty"`
	Platform string `json:"platform,omitempty"`
	// Kind is remote_adb, ios_simulator or android_emulator; empty means
	// remote_adb (see DeviceKind). Use DeviceKind rather than reading this
	// field: rows written before migration 102 have it blank in memory even
	// though the column defaults, and every switch has to treat blank as the
	// phone it always was.
	Kind            string `json:"kind,omitempty"`
	HubURL          string `json:"hub_url"`
	DeviceUDID      string `json:"device_udid"`
	PlatformVersion string `json:"platform_version,omitempty"`
	// DeviceAddr is WHERE THE DEVICE IS, in whatever terms its kind makes that
	// question answerable, and DeviceUDID is what Appium is finally handed. The
	// two are separate for one reason that holds across all three kinds: the
	// operator supplies a stable identity, and something else allocates the
	// address Appium drives.
	//
	//	remote_adb        addr = the phone's tailnet host:port (Android
	//	                  reassigns that port on every reboot, which is why the
	//	                  operator has to keep supplying it)
	//	                  udid = the loopback address the BRIDGE mapped it onto
	//	                  (127.0.0.1:5555, :5556, …). The bridge decides it and
	//	                  reports it; nothing here may invent one.
	//	android_emulator  addr = the AVD name, which is the only stable name an
	//	                  emulator has
	//	                  udid = the adb serial it booted on (emulator-5554),
	//	                  allocated by the emulator, not chosen here
	//	ios_simulator     addr = udid = the simulator's own UDID. simctl's
	//	                  identity is stable and needs no mapping step, so the
	//	                  two collapse — as they also do for a phone reached off
	//	                  a userspace tailnet with no bridge tunnel.
	DeviceAddr string `json:"device_addr,omitempty"`
	// DevicePIN and HubToken are never serialised outward — the API returns
	// MobileDeviceStatus instead, which reports only whether they are set.
	DevicePIN string `json:"-"`
	HubToken  string `json:"-"`

	LastConnectedAt *time.Time `json:"last_connected_at,omitempty"`
	UpdatedAt       time.Time  `json:"updated_at,omitempty"`
}

// Configured mirrors MobileConfig.Configured: a hub and a device, or nothing.
func (d MobileDevice) Configured() bool {
	return d.HubURL != "" && d.DeviceUDID != ""
}

// Managed says the device came from the cluster's own configuration rather than
// from a registration. Those are read-only in the UI: renaming or removing one
// would ask the browser to edit a value that lives in a pod's environment.
func (d MobileDevice) Managed() bool { return d.ID == uuid.Nil }

// DeviceKind is the kind with the pre-102 blank normalised to remote_adb.
// Every switch in the codebase goes through this rather than reading Kind, so
// a row from before the column existed behaves as the phone it has always been
// instead of falling into a default branch by accident.
func (d MobileDevice) DeviceKind() string {
	if kind, ok := ValidDeviceKind(d.Kind); ok {
		return kind
	}
	return DeviceKindRemoteADB
}

// Local reports whether this device lives on the host running this process.
func (d MobileDevice) Local() bool { return LocalDeviceKind(d.DeviceKind()) }

// ValidPlatform refuses anything a bridge-attached installation can actually
// drive. iOS is refused by name rather than as an unknown value, so the
// operator is told the reason instead of being told they typed something wrong.
//
// It governs remote_adb ONLY. A local iOS simulator is not refused here — it is
// gated on the host actually having simulators, which is a question about the
// machine rather than about the requested value, and lives in the mobiledevice
// service where the host can be asked.
func ValidPlatform(p string) (string, bool) {
	switch strings.ToLower(strings.TrimSpace(p)) {
	case "", PlatformAndroid:
		return PlatformAndroid, true
	default:
		return "", false
	}
}

// MobileDeviceStatus is one row of the settings page. It carries no secret:
// the PIN and the token are reported as booleans, because a form that has to
// display a credential to re-submit it is a form that displays a credential.
type MobileDeviceStatus struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	// Platform is "android" for both Android kinds and "ios" for a simulator.
	Platform string `json:"platform"`
	// Kind is always populated on the way out, never blank: a client reading a
	// list has to be able to switch on it without re-implementing the
	// empty-means-remote_adb rule the server already applied.
	Kind string `json:"kind"`
	// Managed marks the env-configured device — shown, drivable, but not
	// editable from here.
	Managed         bool   `json:"managed"`
	Configured      bool   `json:"configured"`
	HubURL          string `json:"hub_url,omitempty"`
	DeviceUDID      string `json:"device_udid,omitempty"`
	PlatformVersion string `json:"platform_version,omitempty"`
	DeviceAddr      string `json:"device_addr,omitempty"`
	HasPIN          bool   `json:"has_pin"`
	HasToken        bool   `json:"has_token"`
	// HubManaged says the hub address and its token come from the cluster's own
	// configuration, so the settings page shows them read-only instead of
	// asking for them. False means this installation has no env-supplied hub and
	// the registration has to carry one.
	HubManaged      bool       `json:"hub_managed"`
	LastConnectedAt *time.Time `json:"last_connected_at,omitempty"`

	// HubReachable and DeviceOnline are probed live, not stored. They are the
	// two questions the operator actually has: is the bridge up, and is this
	// phone answering it.
	HubReachable bool `json:"hub_reachable"`
	DeviceOnline bool `json:"device_online"`
	// DeviceBusy reports a phone that is answering but currently leased by a
	// run. Distinct from offline on purpose — "in use" is a healthy state and
	// must not read as a broken connection. Per device now: with several phones
	// "busy" was never a fact about the installation.
	DeviceBusy bool `json:"device_busy"`
	// Detail carries whatever the bridge said when something is wrong, so the
	// operator sees "device unauthorized" rather than a red dot.
	Detail string `json:"detail,omitempty"`
	// Source says where this device came from: "settings" for a UI
	// registration, "env" for config.yml/environment. An operator whose UI form
	// looks right while an env var quietly wins needs to be told which one the
	// agents are using.
	Source string `json:"source,omitempty"`
}

// SaveMobileDeviceRequest is the add/edit form. The two credentials are
// pointers so an omitted field keeps what is stored while an explicit "" clears
// it — the form never has to echo a secret back to change an address.
//
// HubURL and HubToken are optional and normally absent: they describe the
// cluster's Appium hub, which the pod already has in its own configuration, so
// the UI does not ask for them. A request that does carry them overrides that.
//
// Kind is likewise optional: absent means remote_adb, so every client written
// before local devices existed keeps registering exactly the phone it did.
// Which of DeviceUDID / DeviceAddr carries the operator's choice depends on the
// kind — see MobileDevice.DeviceAddr for the table.
type SaveMobileDeviceRequest struct {
	Name            string  `json:"name"`
	Platform        string  `json:"platform"`
	Kind            string  `json:"kind"`
	HubURL          string  `json:"hub_url"`
	DeviceUDID      string  `json:"device_udid"`
	PlatformVersion string  `json:"platform_version"`
	DeviceAddr      string  `json:"device_addr"`
	DevicePIN       *string `json:"device_pin"`
	HubToken        *string `json:"hub_token"`
}

// PairMobileDeviceRequest carries the six-digit code Android shows while its
// wireless-debugging pairing dialog is open. The code expires with the dialog,
// which is exactly why this exists as an endpoint: the person holding the
// phone can type it where they are, instead of relaying it to whoever has a
// shell on the cluster.
type PairMobileDeviceRequest struct {
	// PairAddr is the host:port from the pairing dialog. It is NOT the same
	// port as the debugging connection — Android shows a fresh one per pairing.
	PairAddr string `json:"pair_addr"`
	Code     string `json:"code"`
}
