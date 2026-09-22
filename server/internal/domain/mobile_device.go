package domain

import (
	"strings"
	"time"

	"github.com/google/uuid"
)

// PlatformAndroid is the platform a cluster-hosted installation can drive. Not
// an oversight: Appium's XCUITest driver needs a macOS host with Xcode, which
// the cluster has none of. The value is stored anyway so the settings page can
// offer the choice and say why half of it is unavailable.
const PlatformAndroid = "android"

// PlatformIOS is drivable only where the HOST says so (DeviceKindIOSSimulator);
// on a Linux node it is refused by name so the operator is told the reason.
const PlatformIOS = "ios"

// The device kinds. A kind is what every step of a device's life switches on —
// which binary attaches it, which Appium driver drives it, whether the adb
// bridge is involved — because the same eleven mobile_* tools reach three
// different things.
const (
	// DeviceKindRemoteADB is a physical phone reached through the cluster's adb
	// bridge; the default, which migration 102 backfills every existing row
	// with.
	DeviceKindRemoteADB = "remote_adb"

	// DeviceKindIOSSimulator is an `xcrun simctl` simulator on THIS host, only
	// where the host reports one (a macOS machine, in practice): XCUITest needs
	// Xcode. The bridge is never involved — no adb, and the host starts and
	// stops the simulator itself.
	DeviceKindIOSSimulator = "ios_simulator"

	// DeviceKindAndroidEmulator is an Android SDK AVD on THIS host, reached
	// through the LOCAL adb. Same UiAutomator2 driver and capability set as a
	// physical phone — only the attach/detach half differs.
	DeviceKindAndroidEmulator = "android_emulator"
)

// ValidDeviceKind normalises a requested kind; an empty value is remote_adb
// rather than an error, which is what keeps clients written before kinds
// existed registering exactly the phone they always did.
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

// PlatformForKind is the platform a kind implies; the two are not independent,
// so asking the operator for both would only create a way to disagree with
// themselves.
func PlatformForKind(kind string) string {
	if kind == DeviceKindIOSSimulator {
		return PlatformIOS
	}
	return PlatformAndroid
}

// LocalDeviceKind reports whether a kind lives on this host rather than at the
// far end of the bridge: local kinds attach by exec'ing a binary here, remote
// ones by an HTTP call to the sidecar.
func LocalDeviceKind(kind string) bool {
	return kind == DeviceKindIOSSimulator || kind == DeviceKindAndroidEmulator
}

// LocalSimulator is one iOS simulator the host offers. Runtime is carried
// because it is where the platform version comes from — simctl knows it
// exactly, so making an operator retype it would only create a way to be wrong.
type LocalSimulator struct {
	UDID    string `json:"udid"`
	Name    string `json:"name"`
	Runtime string `json:"runtime"`
	// State is simctl's own word — "Booted", "Shutdown" — reported rather than
	// reduced to a boolean so the UI can say what is happening between the two.
	State string `json:"state,omitempty"`
	// PlatformVersionField is the numeric half of Runtime, filled by a Mac that
	// already split it and left empty by the local host. Read it only through
	// PlatformVersion(): the two sources fill different fields with the same
	// fact.
	PlatformVersionField string `json:"platform_version,omitempty"`
}

// PlatformVersion is what `appium:platformVersion` wants: "17.4", not "iOS
// 17.4". Derived from Runtime when the reporter did not split it; absence stays
// absence ("" → capability omitted, never guessed).
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
// the simulators simctl lists and the AVDs the Android SDK lists. Both slices
// are always non-nil — an empty array says "asked, has none", which is not the
// same statement as a null.
type LocalDeviceCatalog struct {
	IOS     []LocalSimulator `json:"ios"`
	Android []string         `json:"android"`
}

// EmptyLocalDeviceCatalog is the answer for a host with nothing to offer.
func EmptyLocalDeviceCatalog() LocalDeviceCatalog {
	return LocalDeviceCatalog{IOS: []LocalSimulator{}, Android: []string{}}
}

// MobileDevice is one registered Android test device, the DB-backed twin of
// MobileConfig. Registered devices win over the environment's: an operator who
// just attached a phone in the browser expects that to be the phone.
type MobileDevice struct {
	// ID is empty for the env-configured device, which has no row; that
	// emptiness says "this one is cluster configuration" and is why it cannot
	// be renamed or removed from the UI.
	ID uuid.UUID `json:"id,omitempty"`
	// Name is what the operator calls it; required for registered devices, or a
	// list of three phones names nothing.
	Name     string `json:"name,omitempty"`
	Platform string `json:"platform,omitempty"`
	// Kind is remote_adb, ios_simulator or android_emulator; rows written
	// before migration 102 carry it blank in memory, so use DeviceKind() which
	// normalises blank to remote_adb.
	Kind            string `json:"kind,omitempty"`
	HubURL          string `json:"hub_url"`
	DeviceUDID      string `json:"device_udid"`
	PlatformVersion string `json:"platform_version,omitempty"`
	// DeviceAddr is WHERE THE DEVICE IS in its kind's terms; DeviceUDID is what
	// Appium is finally handed. The operator supplies a stable identity, and
	// something else allocates the address Appium drives: the bridge maps a
	// physical phone onto 127.0.0.1, the AVD is named and its adb serial
	// auto-allocated, and simctl's UDID needs no mapping at all — addr = udid.
	DeviceAddr string `json:"device_addr,omitempty"`
	// DevicePIN and HubToken are never serialised outward; the API returns
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

// Managed says the device came from the cluster's own configuration rather
// than from a registration; those are read-only in the UI.
func (d MobileDevice) Managed() bool { return d.ID == uuid.Nil }

// DeviceKind is the kind with the pre-102 blank normalised to remote_adb;
// every switch in the codebase goes through this rather than reading Kind.
func (d MobileDevice) DeviceKind() string {
	if kind, ok := ValidDeviceKind(d.Kind); ok {
		return kind
	}
	return DeviceKindRemoteADB
}

// Local reports whether this device lives on the host running this process.
func (d MobileDevice) Local() bool { return LocalDeviceKind(d.DeviceKind()) }

// ValidPlatform refuses anything a bridge-attached installation can actually
// drive, iOS by name so the operator is told why. It governs remote_adb ONLY:
// a local iOS simulator is gated on the host having simulators, which is a
// question about the machine, not the requested value.
func ValidPlatform(p string) (string, bool) {
	switch strings.ToLower(strings.TrimSpace(p)) {
	case "", PlatformAndroid:
		return PlatformAndroid, true
	default:
		return "", false
	}
}

// MobileDeviceStatus is one row of the settings page. It carries no secret:
// the PIN and token come back as booleans, because a form that has to display
// a credential to re-submit it is a form that displays a credential.
type MobileDeviceStatus struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	// Platform is "android" for both Android kinds and "ios" for a simulator.
	Platform string `json:"platform"`
	// Kind is always populated on the way out, never blank: a client reading a
	// list must be able to switch on it without re-implementing the
	// empty-means-remote_adb rule.
	Kind string `json:"kind"`
	// Managed marks the env-configured device — shown, drivable, not editable.
	Managed         bool   `json:"managed"`
	Configured      bool   `json:"configured"`
	HubURL          string `json:"hub_url,omitempty"`
	DeviceUDID      string `json:"device_udid,omitempty"`
	PlatformVersion string `json:"platform_version,omitempty"`
	DeviceAddr      string `json:"device_addr,omitempty"`
	HasPIN          bool   `json:"has_pin"`
	HasToken        bool   `json:"has_token"`
	// HubManaged says the hub address and token come from the cluster's own
	// configuration, so the page shows them read-only.
	HubManaged      bool       `json:"hub_managed"`
	LastConnectedAt *time.Time `json:"last_connected_at,omitempty"`
	// HubReachable / DeviceOnline are probed live, not stored: is the bridge
	// up, and is this phone answering it.
	HubReachable bool `json:"hub_reachable"`
	DeviceOnline bool `json:"device_online"`
	// DeviceBusy reports a phone answering but leased by a run — "in use" is a
	// healthy state and must not read as a broken connection.
	DeviceBusy bool `json:"device_busy"`
	// Detail carries whatever the bridge said when something is wrong, so the
	// operator sees "device unauthorized" rather than a red dot.
	Detail string `json:"detail,omitempty"`
	// Source says where the device came from: "settings" or "env", so an
	// operator whose UI form looks right while an env var quietly wins is told
	// which one the agents use.
	Source string `json:"source,omitempty"`
}

// SaveMobileDeviceRequest is the add/edit form. The two credentials are
// pointers so an omitted field keeps what is stored while an explicit "" clears
// it — the form never has to echo a secret back to change an address. HubURL/
// HubToken and Kind are optional: the hub normally comes from the pod's own
// configuration, and an absent kind means remote_adb.
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
// wireless-debugging pairing dialog is open — the code expires with the dialog,
// so the phone holder can type it where they are.
type PairMobileDeviceRequest struct {
	// PairAddr is the host:port from the pairing dialog, NOT the same port as
	// the debugging connection — Android shows a fresh one per pairing.
	PairAddr string `json:"pair_addr"`
	Code     string `json:"code"`
}
