package mobile

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

// The kind picks the driver. Getting this wrong is not a degraded session, it
// is no session at all: Appium matches capabilities before it does anything
// else, so a simulator handed UiAutomator2 fails at creation with a message
// about a driver nobody chose.
func TestIOSSimulatorGetsTheXCUITestDriver(t *testing.T) {
	caps := capabilitiesFor(Config{
		Kind:            domain.DeviceKindIOSSimulator,
		DeviceUDID:      "11111111-2222-3333-4444-555555555555",
		PlatformVersion: "17.4",
	}, "")

	assert.Equal(t, "iOS", caps["platformName"])
	assert.Equal(t, "XCUITest", caps["appium:automationName"])
	assert.Equal(t, "11111111-2222-3333-4444-555555555555", caps["appium:udid"])
	assert.Equal(t, "17.4", caps["appium:platformVersion"])
}

// bundleId, not appPackage. appPackage is a UiAutomator2 capability XCUITest
// ignores, which leaves a session that opens nothing and a run that photographs
// the home screen and reports the app as broken.
func TestIOSNamesTheAppByBundleID(t *testing.T) {
	caps := capabilitiesFor(Config{
		Kind:       domain.DeviceKindIOSSimulator,
		DeviceUDID: "11111111-2222-3333-4444-555555555555",
	}, "ai.tasktrooper.app.stage")

	assert.Equal(t, "ai.tasktrooper.app.stage", caps["appium:bundleId"])
	_, hasPackage := caps["appium:appPackage"]
	assert.False(t, hasPackage, "appPackage is Android-only and XCUITest would ignore it")
}

// A simulator has no lock screen to unlock and no permission grants adb could
// force. Sending Android-only capabilities to XCUITest is at best ignored and
// at worst a rejected session, so a PIN that survives on the registration must
// not reach an iOS session.
func TestIOSSendsNoAndroidOnlyCapabilities(t *testing.T) {
	caps := capabilitiesFor(Config{
		Kind:       domain.DeviceKindIOSSimulator,
		DeviceUDID: "11111111-2222-3333-4444-555555555555",
		DevicePIN:  "4321",
	}, "")

	for _, androidOnly := range []string{
		"appium:autoGrantPermissions",
		"appium:unlockType",
		"appium:unlockKey",
		"appium:unlockStrategy",
		"appium:skipUnlock",
	} {
		_, present := caps[androidOnly]
		assert.False(t, present, "%s is an Android capability and must not reach XCUITest", androidOnly)
	}
}

// From Appium's side an emulator IS a phone: same driver, same capabilities,
// same everything. A difference here would be a way for a bug to reproduce on
// one and not on the other, which is the opposite of what emulator support is
// for.
func TestAnEmulatorIsDrivenExactlyLikeAPhone(t *testing.T) {
	phone := capabilitiesFor(Config{
		Kind:            domain.DeviceKindRemoteADB,
		DeviceUDID:      "127.0.0.1:5555",
		PlatformVersion: "14",
		DevicePIN:       "4321",
	}, "ai.tasktrooper.app")
	emulator := capabilitiesFor(Config{
		Kind:            domain.DeviceKindAndroidEmulator,
		DeviceUDID:      "emulator-5554",
		PlatformVersion: "14",
		DevicePIN:       "4321",
	}, "ai.tasktrooper.app")

	require.Equal(t, len(phone), len(emulator))
	for key, want := range phone {
		if key == "appium:udid" {
			continue // the one thing that legitimately differs
		}
		assert.Equal(t, want, emulator[key], "capability %s differs between a phone and an emulator", key)
	}
	assert.Equal(t, "emulator-5554", emulator["appium:udid"])
}

// An empty kind is a Config built by code written before kinds existed — and
// before migration 102, every row in the table. It has to keep producing the
// exact session it always did.
func TestABlankKindStillProducesTheAndroidSession(t *testing.T) {
	caps := capabilitiesFor(Config{DeviceUDID: "127.0.0.1:5555", DevicePIN: "4321"}, "ai.tasktrooper.app")

	assert.Equal(t, "Android", caps["platformName"])
	assert.Equal(t, "UiAutomator2", caps["appium:automationName"])
	assert.Equal(t, true, caps["appium:autoGrantPermissions"])
	assert.Equal(t, "pin", caps["appium:unlockType"])
	assert.Equal(t, "4321", caps["appium:unlockKey"])
	assert.Equal(t, "ai.tasktrooper.app", caps["appium:appPackage"])
}

// The watchdog is the only thing that frees a device when this pod is killed
// mid-run, so it belongs to every kind rather than to the Android branch it
// used to live in.
func TestEveryKindKeepsTheHubSideWatchdog(t *testing.T) {
	for _, kind := range []string{
		domain.DeviceKindRemoteADB,
		domain.DeviceKindAndroidEmulator,
		domain.DeviceKindIOSSimulator,
	} {
		caps := capabilitiesFor(Config{Kind: kind, DeviceUDID: "11111111-2222-3333-4444-555555555555"}, "")
		assert.Equal(t, int(idleRelease.Seconds())+60, caps["appium:newCommandTimeout"], "kind %s", kind)
		assert.Equal(t, true, caps["appium:noReset"], "kind %s", kind)
	}
}

// The pool allocates across kinds without knowing they exist: sessions are
// matched by UDID, which is unique per device whatever the device is. This is
// the check that a mixed installation — a phone, a simulator and an emulator —
// is three leases rather than one confused one.
func TestPoolHoldsDevicesOfDifferentKinds(t *testing.T) {
	p := NewPool()
	p.Reconfigure([]Config{
		{HubURL: "http://hub", Kind: domain.DeviceKindRemoteADB, DeviceUDID: "127.0.0.1:5555"},
		{HubURL: "http://hub", Kind: domain.DeviceKindAndroidEmulator, DeviceUDID: "emulator-5554"},
		{HubURL: "http://hub", Kind: domain.DeviceKindIOSSimulator, DeviceUDID: "11111111-2222-3333-4444-555555555555"},
	})

	assert.Equal(t,
		[]string{"127.0.0.1:5555", "emulator-5554", "11111111-2222-3333-4444-555555555555"},
		p.Devices(), "registration order is the allocation order, whatever the kinds are")
	assert.True(t, p.Configured())

	// Re-applying the same registrations must reuse the sessions rather than
	// rebuild them — a kind changing nothing about identity is the point.
	before := p.snapshot()
	p.Reconfigure([]Config{
		{HubURL: "http://hub", Kind: domain.DeviceKindRemoteADB, DeviceUDID: "127.0.0.1:5555"},
		{HubURL: "http://hub", Kind: domain.DeviceKindAndroidEmulator, DeviceUDID: "emulator-5554"},
		{HubURL: "http://hub", Kind: domain.DeviceKindIOSSimulator, DeviceUDID: "11111111-2222-3333-4444-555555555555"},
	})
	assert.Equal(t, before, p.snapshot())
}
