package mobiledevice

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

// fakeHost stands in for the machine. It records what was asked of it, because
// the interesting assertions here are about which lifecycle call a kind makes —
// booting a simulator versus dialling the bridge — rather than about return
// values.
type fakeHost struct {
	ios      bool
	android  bool
	sims     []domain.LocalSimulator
	avds     []string
	serials  map[string]string // avd -> serial, populated by StartEmulator
	bootedID map[string]bool

	calls []string
}

func newFakeHost() *fakeHost {
	return &fakeHost{
		serials:  map[string]string{},
		bootedID: map[string]bool{},
	}
}

func (f *fakeHost) record(format string, args ...any) {
	f.calls = append(f.calls, fmt.Sprintf(format, args...))
}

func (f *fakeHost) saw(want string) bool {
	for _, c := range f.calls {
		if c == want {
			return true
		}
	}
	return false
}

func (f *fakeHost) SupportsIOSSimulators(context.Context) bool    { return f.ios }
func (f *fakeHost) SupportsAndroidEmulators(context.Context) bool { return f.android }
func (f *fakeHost) ShutdownSimulator(_ context.Context, u string) error {
	f.record("shutdown %s", u)
	f.bootedID[u] = false
	return nil
}

func (f *fakeHost) Catalog(context.Context) (domain.LocalDeviceCatalog, error) {
	out := domain.EmptyLocalDeviceCatalog()
	if f.ios {
		out.IOS = f.sims
	}
	if f.android {
		out.Android = f.avds
	}
	return out, nil
}

func (f *fakeHost) Simulator(_ context.Context, udid string) (domain.LocalSimulator, bool, error) {
	for _, s := range f.sims {
		if strings.EqualFold(s.UDID, udid) {
			return s, true, nil
		}
	}
	return domain.LocalSimulator{}, false, nil
}

func (f *fakeHost) SimulatorBooted(_ context.Context, udid string) (bool, error) {
	return f.bootedID[udid], nil
}

func (f *fakeHost) BootSimulator(_ context.Context, udid string) error {
	f.record("boot %s", udid)
	f.bootedID[udid] = true
	return nil
}

func (f *fakeHost) AVDExists(_ context.Context, name string) (bool, error) {
	for _, a := range f.avds {
		if a == name {
			return true, nil
		}
	}
	return false, nil
}

func (f *fakeHost) EmulatorSerial(_ context.Context, avd string) (string, error) {
	return f.serials[avd], nil
}

func (f *fakeHost) StartEmulator(_ context.Context, avd string) (string, error) {
	f.record("start %s", avd)
	serial := "emulator-5554"
	f.serials[avd] = serial
	return serial, nil
}

func (f *fakeHost) StopEmulator(_ context.Context, serial string) error {
	f.record("stop %s", serial)
	for avd, s := range f.serials {
		if s == serial {
			delete(f.serials, avd)
		}
	}
	return nil
}

var _ LocalHost = (*fakeHost)(nil)

// macHost is a host with both local toolchains, which is what a tenant running
// on somebody's laptop looks like.
func macHost() *fakeHost {
	h := newFakeHost()
	h.ios, h.android = true, true
	h.sims = []domain.LocalSimulator{
		{UDID: "11111111-2222-3333-4444-555555555555", Name: "iPhone 15", Runtime: "iOS 17.4", State: "Shutdown"},
	}
	h.avds = []string{"Pixel_7_API_34"}
	return h
}

// localEnvDevice is the env hub without an env device: exactly the shape a
// local install has, since MOBILE_APPIUM_HUB_URL points at a hub on the same
// machine and there is no phone to name.
func localEnvDevice() domain.MobileDevice {
	return domain.MobileDevice{HubURL: "http://127.0.0.1:4723"}
}

func localService(t *testing.T, host LocalHost) (*Service, *fakeStore) {
	t.Helper()
	store := &fakeStore{}
	// No bridge at all — MOBILE_BRIDGE_URL unset is the normal local case, and
	// its absence must not stop a simulator or an emulator being registered.
	svc := NewService(store, nil, localEnvDevice())
	svc.SetLocalHost(host)
	return svc, store
}

// A simulator is registered by its simctl UDID, and both address fields end up
// holding it: simctl's identity is stable, so unlike a bridge phone there is
// nothing to allocate and nothing for the two to disagree about.
func TestAddIOSSimulatorStoresTheSimctlIdentity(t *testing.T) {
	host := macHost()
	svc, store := localService(t, host)

	_, err := svc.Add(context.Background(), domain.SaveMobileDeviceRequest{
		Name:       "iPhone 15",
		Kind:       domain.DeviceKindIOSSimulator,
		DeviceUDID: "11111111-2222-3333-4444-555555555555",
	})
	require.NoError(t, err)

	require.Len(t, store.devices, 1)
	got := store.devices[0]
	assert.Equal(t, domain.DeviceKindIOSSimulator, got.DeviceKind())
	assert.Equal(t, domain.PlatformIOS, got.Platform)
	assert.Equal(t, "11111111-2222-3333-4444-555555555555", got.DeviceUDID)
	assert.Equal(t, "11111111-2222-3333-4444-555555555555", got.DeviceAddr)
	// The platform version comes from the runtime simctl reported, not from
	// MOBILE_PLATFORM_VERSION: the cluster-wide env value says nothing about
	// which simulator this is, and a mismatched version fails at capability
	// matching with a message about a number nobody typed.
	assert.Equal(t, "17.4", got.PlatformVersion)
	// Registering also connects, because a registration that leaves the device
	// down shows a green form above a red dot.
	assert.True(t, host.saw("boot 11111111-2222-3333-4444-555555555555"))
}

// A UDID that is not on this host is refused at registration rather than at the
// first QA run — the point of the local catalog is that the value cannot be
// invented, and a row nobody can boot is worse than a rejected form.
func TestAddIOSSimulatorRefusesAnUnknownUDID(t *testing.T) {
	svc, store := localService(t, macHost())

	_, err := svc.Add(context.Background(), domain.SaveMobileDeviceRequest{
		Name:       "Ghost",
		Kind:       domain.DeviceKindIOSSimulator,
		DeviceUDID: "99999999-9999-9999-9999-999999999999",
	})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "no simulator")
	assert.Empty(t, store.devices)
}

// THE degradation test. On a host with no simulators — every Linux cluster node,
// and any Mac without Xcode — iOS is refused with the reason, and it is a fact
// about the machine rather than about the operator's typing.
func TestIOSIsStillRefusedOnAHostWithNoSimulators(t *testing.T) {
	linux := newFakeHost()
	linux.android = true // adb exists; simulators do not
	svc, store := localService(t, linux)

	_, err := svc.Add(context.Background(), domain.SaveMobileDeviceRequest{
		Name:       "iPhone 15",
		Kind:       domain.DeviceKindIOSSimulator,
		DeviceUDID: "11111111-2222-3333-4444-555555555555",
	})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "macOS")
	assert.Empty(t, store.devices)

	// And with no host wired at all — which is what the cluster runtime had
	// before this change — the refusal is identical rather than a nil panic.
	bare := NewService(&fakeStore{}, nil, localEnvDevice())
	_, err = bare.Add(context.Background(), domain.SaveMobileDeviceRequest{
		Name: "iPhone 15", Kind: domain.DeviceKindIOSSimulator,
		DeviceUDID: "11111111-2222-3333-4444-555555555555",
	})
	require.Error(t, err)
}

// An emulator is registered by AVD name and has NO udid until it boots: the adb
// serial is allocated from the console-port range at start time, so inventing
// one is the same mistake as guessing a bridge port.
func TestAddAndroidEmulatorTakesItsSerialFromTheBoot(t *testing.T) {
	host := macHost()
	svc, store := localService(t, host)

	_, err := svc.Add(context.Background(), domain.SaveMobileDeviceRequest{
		Name:       "Pixel 7",
		Kind:       domain.DeviceKindAndroidEmulator,
		DeviceAddr: "Pixel_7_API_34",
	})
	require.NoError(t, err)

	require.Len(t, store.devices, 1)
	got := store.devices[0]
	assert.Equal(t, domain.DeviceKindAndroidEmulator, got.DeviceKind())
	assert.Equal(t, domain.PlatformAndroid, got.Platform)
	assert.Equal(t, "Pixel_7_API_34", got.DeviceAddr)
	assert.Equal(t, "emulator-5554", got.DeviceUDID, "the serial the emulator came up on is what Appium is handed")
	assert.True(t, host.saw("start Pixel_7_API_34"))
}

func TestAddAndroidEmulatorRefusesAnUnknownAVD(t *testing.T) {
	svc, store := localService(t, macHost())

	_, err := svc.Add(context.Background(), domain.SaveMobileDeviceRequest{
		Name: "Nope", Kind: domain.DeviceKindAndroidEmulator, DeviceAddr: "Nexus_One",
	})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "no AVD")
	assert.Empty(t, store.devices)
}

// An unknown kind is refused by name. It is a client bug rather than an
// operator one, and an unrecognised value silently falling back to "phone"
// would register a simulator UDID as a tailnet address.
func TestUnknownKindIsRefused(t *testing.T) {
	svc, _ := localService(t, macHost())
	_, err := svc.Add(context.Background(), domain.SaveMobileDeviceRequest{
		Name: "What", Kind: "carrier_pigeon", DeviceAddr: "x",
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "carrier_pigeon")
}

// Unregistering a local device takes the device down with it. A simulator or
// emulator left running after its registration is gone is a window on the
// operator's desktop that nothing in the product will ever close.
func TestRemoveShutsTheLocalDeviceDown(t *testing.T) {
	host := macHost()
	svc, store := localService(t, host)
	ctx := context.Background()

	_, err := svc.Add(ctx, domain.SaveMobileDeviceRequest{
		Name: "iPhone 15", Kind: domain.DeviceKindIOSSimulator,
		DeviceUDID: "11111111-2222-3333-4444-555555555555",
	})
	require.NoError(t, err)
	_, err = svc.Add(ctx, domain.SaveMobileDeviceRequest{
		Name: "Pixel 7", Kind: domain.DeviceKindAndroidEmulator, DeviceAddr: "Pixel_7_API_34",
	})
	require.NoError(t, err)
	require.Len(t, store.devices, 2)

	for _, d := range append([]domain.MobileDevice(nil), store.devices...) {
		_, err := svc.Remove(ctx, d.ID)
		require.NoError(t, err)
	}
	assert.True(t, host.saw("shutdown 11111111-2222-3333-4444-555555555555"))
	assert.True(t, host.saw("stop emulator-5554"))
}

// Device-online is asked of whoever owns the device. Asking the bridge about a
// simulator UDID would report every simulator as offline on a host that has no
// bridge at all — which is every host that has simulators.
func TestStatusAsksTheHostAboutALocalDevice(t *testing.T) {
	host := macHost()
	svc, _ := localService(t, host)
	ctx := context.Background()

	_, err := svc.Add(ctx, domain.SaveMobileDeviceRequest{
		Name: "iPhone 15", Kind: domain.DeviceKindIOSSimulator,
		DeviceUDID: "11111111-2222-3333-4444-555555555555",
	})
	require.NoError(t, err)

	statuses, err := svc.Statuses(ctx)
	require.NoError(t, err)
	require.Len(t, statuses, 1)
	assert.Equal(t, domain.DeviceKindIOSSimulator, statuses[0].Kind)
	assert.True(t, statuses[0].DeviceOnline, "the simulator was booted by the registration")

	// Shut it down and the same read model says so, with no Detail: a
	// shut-down simulator is a resting state, not a fault to explain.
	require.NoError(t, host.ShutdownSimulator(ctx, "11111111-2222-3333-4444-555555555555"))
	statuses, err = svc.Statuses(ctx)
	require.NoError(t, err)
	assert.False(t, statuses[0].DeviceOnline)
	assert.Empty(t, statuses[0].Detail)
}

// Pairing is a physical phone's wireless-debugging dance. There is no dialog on
// a simulator and no six-digit code, so offering it would be offering a step
// that can only fail.
func TestPairIsRefusedForALocalDevice(t *testing.T) {
	host := macHost()
	store := &fakeStore{}
	svc := NewService(store, bridgeSaying(t, "127.0.0.1:5555"), localEnvDevice())
	svc.SetLocalHost(host)
	ctx := context.Background()

	_, err := svc.Add(ctx, domain.SaveMobileDeviceRequest{
		Name: "iPhone 15", Kind: domain.DeviceKindIOSSimulator,
		DeviceUDID: "11111111-2222-3333-4444-555555555555",
	})
	require.NoError(t, err)

	_, err = svc.Pair(ctx, store.devices[0].ID, domain.PairMobileDeviceRequest{PairAddr: "x:1", Code: "123456"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "physical phone")
}

// A registration written before kinds existed has a blank column in memory and
// must behave as the phone it has always been, not fall into a default branch.
func TestABlankKindIsTheBridgePhone(t *testing.T) {
	device := domain.MobileDevice{DeviceUDID: "127.0.0.1:5555"}
	assert.Equal(t, domain.DeviceKindRemoteADB, device.DeviceKind())
	assert.False(t, device.Local())
}

// The catalog is what the settings page offers. Without a host it is two empty
// arrays rather than an error: "this machine has none" is an answer.
func TestLocalCatalogWithoutAHost(t *testing.T) {
	svc := NewService(&fakeStore{}, nil, localEnvDevice())
	catalog, err := svc.LocalCatalog(context.Background())
	require.NoError(t, err)
	assert.NotNil(t, catalog.IOS)
	assert.NotNil(t, catalog.Android)
	assert.Empty(t, catalog.IOS)
	assert.Empty(t, catalog.Android)
}

// With one, it is what the host reports — and it is the same list the
// registration validates against, which is the property that makes a chosen
// value always a registrable one.
func TestLocalCatalogComesFromTheHost(t *testing.T) {
	svc, _ := localService(t, macHost())
	catalog, err := svc.LocalCatalog(context.Background())
	require.NoError(t, err)
	require.Len(t, catalog.IOS, 1)
	assert.Equal(t, "iPhone 15", catalog.IOS[0].Name)
	assert.Equal(t, []string{"Pixel_7_API_34"}, catalog.Android)
}

// Local devices are effective devices like any other. This is what makes the
// mobile_* tools register on a host with no MOBILE_BRIDGE_URL: registration is
// driven by what is attached, and the bridge only ever served one of the kinds.
func TestLocalDevicesAreEffectiveWithoutABridge(t *testing.T) {
	svc, _ := localService(t, macHost())
	ctx := context.Background()

	_, err := svc.Add(ctx, domain.SaveMobileDeviceRequest{
		Name: "iPhone 15", Kind: domain.DeviceKindIOSSimulator,
		DeviceUDID: "11111111-2222-3333-4444-555555555555",
	})
	require.NoError(t, err)

	devices, source, err := svc.Effective(ctx)
	require.NoError(t, err)
	assert.Equal(t, "settings", source)
	require.Len(t, devices, 1)
	assert.True(t, devices[0].Configured(), "a simulator with a hub and a UDID is drivable")
}
