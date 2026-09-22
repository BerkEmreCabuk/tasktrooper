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

type fakeHost struct {
	ios      bool
	android  bool
	sims     []domain.LocalSimulator
	avds     []string
	serials  map[string]string
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

func macHost() *fakeHost {
	h := newFakeHost()
	h.ios, h.android = true, true
	h.sims = []domain.LocalSimulator{
		{UDID: "11111111-2222-3333-4444-555555555555", Name: "iPhone 15", Runtime: "iOS 17.4", State: "Shutdown"},
	}
	h.avds = []string{"Pixel_7_API_34"}
	return h
}

func localEnvDevice() domain.MobileDevice {
	return domain.MobileDevice{HubURL: "http://127.0.0.1:4723"}
}

func localService(t *testing.T, host LocalHost) (*Service, *fakeStore) {
	t.Helper()
	store := &fakeStore{}

	svc := NewService(store, nil, localEnvDevice())
	svc.SetLocalHost(host)
	return svc, store
}

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

	assert.Equal(t, "17.4", got.PlatformVersion)

	assert.True(t, host.saw("boot 11111111-2222-3333-4444-555555555555"))
}

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

func TestIOSIsStillRefusedOnAHostWithNoSimulators(t *testing.T) {
	linux := newFakeHost()
	linux.android = true
	svc, store := localService(t, linux)

	_, err := svc.Add(context.Background(), domain.SaveMobileDeviceRequest{
		Name:       "iPhone 15",
		Kind:       domain.DeviceKindIOSSimulator,
		DeviceUDID: "11111111-2222-3333-4444-555555555555",
	})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "macOS")
	assert.Empty(t, store.devices)

	bare := NewService(&fakeStore{}, nil, localEnvDevice())
	_, err = bare.Add(context.Background(), domain.SaveMobileDeviceRequest{
		Name: "iPhone 15", Kind: domain.DeviceKindIOSSimulator,
		DeviceUDID: "11111111-2222-3333-4444-555555555555",
	})
	require.Error(t, err)
}

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

func TestUnknownKindIsRefused(t *testing.T) {
	svc, _ := localService(t, macHost())
	_, err := svc.Add(context.Background(), domain.SaveMobileDeviceRequest{
		Name: "What", Kind: "carrier_pigeon", DeviceAddr: "x",
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "carrier_pigeon")
}

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

	require.NoError(t, host.ShutdownSimulator(ctx, "11111111-2222-3333-4444-555555555555"))
	statuses, err = svc.Statuses(ctx)
	require.NoError(t, err)
	assert.False(t, statuses[0].DeviceOnline)
	assert.Empty(t, statuses[0].Detail)
}

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

func TestABlankKindIsTheBridgePhone(t *testing.T) {
	device := domain.MobileDevice{DeviceUDID: "127.0.0.1:5555"}
	assert.Equal(t, domain.DeviceKindRemoteADB, device.DeviceKind())
	assert.False(t, device.Local())
}

func TestLocalCatalogWithoutAHost(t *testing.T) {
	svc := NewService(&fakeStore{}, nil, localEnvDevice())
	catalog, err := svc.LocalCatalog(context.Background())
	require.NoError(t, err)
	assert.NotNil(t, catalog.IOS)
	assert.NotNil(t, catalog.Android)
	assert.Empty(t, catalog.IOS)
	assert.Empty(t, catalog.Android)
}

func TestLocalCatalogComesFromTheHost(t *testing.T) {
	svc, _ := localService(t, macHost())
	catalog, err := svc.LocalCatalog(context.Background())
	require.NoError(t, err)
	require.Len(t, catalog.IOS, 1)
	assert.Equal(t, "iPhone 15", catalog.IOS[0].Name)
	assert.Equal(t, []string{"Pixel_7_API_34"}, catalog.Android)
}

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
