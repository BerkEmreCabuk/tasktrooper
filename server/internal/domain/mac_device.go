package domain

import "strings"

// This file is the shape of what a MEMBER'S MAC says about the devices on it —
// the remote twin of LocalDeviceCatalog. A Linux pod cannot run an iOS
// simulator, so the machine that can answer "which simulators are here" is the
// laptop, reached over the tunnel (adapter/runner, `mobile.devices`).
// MacDeviceCatalog EMBEDS LocalDeviceCatalog rather than restating it, so `ios`
// and `android` decode into the same fields with the same meaning wherever they
// came from.

// MacRunningEmulator is an AVD that is up right now, and the SERIAL it came up
// on — the only place a serial appears in a catalog, because it is the only
// fact that is allocated rather than declared: the emulator picks a console
// port at boot, so a caller that remembered `emulator-5554` would drive
// whichever emulator happens to be on that port now.
type MacRunningEmulator struct {
	AVD    string `json:"avd"`
	Serial string `json:"serial"`
}

// MacCapability is one thing a Mac can or cannot do, with the sentence to put
// in front of a person when it cannot. Detail is not decoration: "Unavailable"
// alone sends somebody looking in the wrong place; a false capability always
// carries one, and this side passes it through rather than paraphrasing it,
// because the Mac is the only party that knows WHICH half is missing.
type MacCapability struct {
	Available bool   `json:"available"`
	Detail    string `json:"detail,omitempty"`
}

// MacAppium is the hub's two independent facts. Configured and Reachable are
// separate because their remedies are separate: not configured means no Appium
// is installed (an install); configured and unreachable means one is not
// answering (a restart). Collapsing them would send half the people to the
// wrong instruction. DrivePath is reported by the Mac so no caller holds a copy
// of the runner's routing table.
type MacAppium struct {
	Configured bool   `json:"configured"`
	Reachable  bool   `json:"reachable"`
	BaseURL    string `json:"base_url,omitempty"`
	DrivePath  string `json:"drive_path,omitempty"`
	Detail     string `json:"detail,omitempty"`
}

// Usable reports whether a session can be created against this Mac's hub at
// all — both halves, because a hub that is installed and not answering drives
// nothing.
func (a MacAppium) Usable() bool { return a.Configured && a.Reachable }

// MacCapabilities is the capability block of a `mobile.devices` answer.
type MacCapabilities struct {
	IOSSimulators    MacCapability `json:"ios_simulators"`
	AndroidEmulators MacCapability `json:"android_emulators"`
	Appium           MacAppium     `json:"appium"`
}

// MacDeviceCatalog is one Mac's answer to "what can you drive". Every slice is
// non-nil after Normalize: `[]` says "this Mac was asked and has none", which
// is a different statement from a null. A Mac whose Android SDK is broken still
// reports its simulators — one half failing never fails the other — so a caller
// must read the capability block rather than inferring absence from an empty
// list.
type MacDeviceCatalog struct {
	LocalDeviceCatalog
	AndroidRunning []MacRunningEmulator `json:"android_running"`
	Capabilities   MacCapabilities      `json:"capabilities"`
}

// Normalize fills the nils a decode can leave behind, called on every answer
// from a Mac: a runner that omitted a field, or an older desktop build, produce
// a null where the contract promises an array — and a nil slice RENDERED is
// "unknown" where the truth is "none".
func (c MacDeviceCatalog) Normalize() MacDeviceCatalog {
	if c.IOS == nil {
		c.IOS = []LocalSimulator{}
	}
	if c.Android == nil {
		c.Android = []string{}
	}
	if c.AndroidRunning == nil {
		c.AndroidRunning = []MacRunningEmulator{}
	}
	return c
}

// SerialFor returns the adb serial an AVD is currently running on, if it is
// running at all. Absence is absence: no serial means the emulator is down, not
// that it is on some default port.
func (c MacDeviceCatalog) SerialFor(avd string) (string, bool) {
	avd = strings.TrimSpace(avd)
	for _, running := range c.AndroidRunning {
		if running.AVD == avd && running.Serial != "" {
			return running.Serial, true
		}
	}
	return "", false
}

// SimulatorState is simctl's own word for one simulator, or "" when this Mac
// does not have that UDID at all. Reported rather than reduced to a boolean for
// the same reason LocalSimulator.State is.
func (c MacDeviceCatalog) SimulatorState(udid string) string {
	udid = strings.TrimSpace(udid)
	for _, sim := range c.IOS {
		if strings.EqualFold(sim.UDID, udid) {
			return sim.State
		}
	}
	return ""
}

// SimulatorBootedState is the one value of State that means "drivable now".
const SimulatorBootedState = "Booted"

// MacBootedDevice is what `mobile.boot` answers. UDID is the field that method
// exists for and it is NOT the id that was asked for: for a simulator the two
// collapse (simctl's identity is stable), but for an emulator the AVD is a name
// and the serial is allocated at boot, so this is the only value that may go
// into `appium:udid`.
type MacBootedDevice struct {
	Kind            string `json:"kind"`
	ID              string `json:"id"`
	UDID            string `json:"udid"`
	PlatformVersion string `json:"platform_version,omitempty"`
	Name            string `json:"name,omitempty"`
	// AlreadyBooted says the device was up before this call. It is a success,
	// never a conflict: the caller's intent is a state, not a transition, and
	// booting at the start of every run is what makes a run safe to retry.
	AlreadyBooted bool  `json:"already_booted"`
	DurationMS    int64 `json:"duration_ms,omitempty"`
}
