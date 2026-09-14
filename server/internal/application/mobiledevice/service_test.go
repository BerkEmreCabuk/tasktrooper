package mobiledevice

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"

	"github.com/makifbaysal/tasktrooper/server/internal/adapter/deviceagent"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

type fakeStore struct {
	devices []domain.MobileDevice
}

func (f *fakeStore) List(context.Context) ([]domain.MobileDevice, error) {
	return append([]domain.MobileDevice(nil), f.devices...), nil
}

func (f *fakeStore) Get(_ context.Context, id uuid.UUID) (domain.MobileDevice, bool, error) {
	for _, d := range f.devices {
		if d.ID == id {
			return d, true, nil
		}
	}
	return domain.MobileDevice{}, false, nil
}

func (f *fakeStore) Create(_ context.Context, in domain.MobileDevice, pin, _ *string) (domain.MobileDevice, error) {
	in.ID = uuid.New()
	if pin != nil {
		in.DevicePIN = *pin
	}
	f.devices = append(f.devices, in)
	return in, nil
}

func (f *fakeStore) Update(_ context.Context, in domain.MobileDevice, _, _ *string) (domain.MobileDevice, error) {
	for i, d := range f.devices {
		if d.ID == in.ID {
			f.devices[i] = in
			return in, nil
		}
	}
	return domain.MobileDevice{}, nil
}

func (f *fakeStore) MarkConnected(context.Context, uuid.UUID) error { return nil }

func (f *fakeStore) Delete(_ context.Context, id uuid.UUID) error {
	kept := f.devices[:0]
	for _, d := range f.devices {
		if d.ID != id {
			kept = append(kept, d)
		}
	}
	f.devices = kept
	return nil
}

func envDevice() domain.MobileDevice {
	return domain.MobileDevice{
		HubURL:     "http://appium.tenants.svc.cluster.local:4723",
		DeviceUDID: "127.0.0.1:5555",
		HubToken:   "hub",
	}
}

// bridgeSaying stands in for the device-agent sidecar, which is the only party
// that knows which loopback port a phone was mapped onto.
func bridgeSaying(t *testing.T, localAddr string) *deviceagent.Client {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/connect":
			_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "local_addr": localAddr, "detail": "connected to " + localAddr})
		case "/status":
			_ = json.NewEncoder(w).Encode(map[string]any{"online": true, "local_addr": localAddr})
		default:
			_ = json.NewEncoder(w).Encode(map[string]any{"ok": true})
		}
	}))
	t.Cleanup(srv.Close)
	return deviceagent.New(srv.URL, "")
}

// The UDID is allocated by the bridge, one loopback port per phone. A server
// that guessed it would be right about the first device and would point every
// later one at a phone somebody else is driving.
func TestAddTakesTheUDIDFromTheBridge(t *testing.T) {
	store := &fakeStore{}
	svc := NewService(store, bridgeSaying(t, "127.0.0.1:5556"), envDevice())

	if _, err := svc.Add(context.Background(), domain.SaveMobileDeviceRequest{
		Name:       "Pixel 7",
		DeviceAddr: "100.99.251.76:36745",
	}); err != nil {
		t.Fatalf("add: %v", err)
	}
	if len(store.devices) != 1 {
		t.Fatalf("stored %d devices, want 1", len(store.devices))
	}
	got := store.devices[0]
	if got.DeviceUDID != "127.0.0.1:5556" {
		t.Fatalf("stored UDID is %q, want the address the bridge reported", got.DeviceUDID)
	}
	if got.DeviceAddr != "100.99.251.76:36745" {
		t.Fatalf("stored address is %q, want the phone's tailnet address", got.DeviceAddr)
	}
	if got.Name != "Pixel 7" || got.Platform != domain.PlatformAndroid {
		t.Fatalf("stored %+v, want the submitted name and android", got)
	}
}

// Several phones, several rows, several local ports. The failure this guards
// against is the one that looks like success: two registrations collapsing onto
// one device.
func TestAddSecondDeviceKeepsTheFirst(t *testing.T) {
	store := &fakeStore{}
	ctx := context.Background()

	svc := NewService(store, bridgeSaying(t, "127.0.0.1:5555"), envDevice())
	if _, err := svc.Add(ctx, domain.SaveMobileDeviceRequest{Name: "Pixel", DeviceAddr: "100.99.251.76:36745"}); err != nil {
		t.Fatalf("add first: %v", err)
	}
	svc = NewService(store, bridgeSaying(t, "127.0.0.1:5556"), envDevice())
	if _, err := svc.Add(ctx, domain.SaveMobileDeviceRequest{Name: "Galaxy", DeviceAddr: "100.64.10.9:41003"}); err != nil {
		t.Fatalf("add second: %v", err)
	}
	if len(store.devices) != 2 {
		t.Fatalf("stored %d devices, want 2", len(store.devices))
	}
	if store.devices[0].DeviceUDID == store.devices[1].DeviceUDID {
		t.Fatalf("both devices share the UDID %q", store.devices[0].DeviceUDID)
	}
}

// A reboot is the common case and must be one edit: the same phone, a new port,
// nothing else touched.
func TestUpdateAcceptsANewPortForTheSamePhone(t *testing.T) {
	store := &fakeStore{}
	svc := NewService(store, bridgeSaying(t, "127.0.0.1:5555"), envDevice())
	ctx := context.Background()

	if _, err := svc.Add(ctx, domain.SaveMobileDeviceRequest{Name: "Pixel", DeviceAddr: "100.99.251.76:36745"}); err != nil {
		t.Fatalf("add: %v", err)
	}
	id := store.devices[0].ID
	if _, err := svc.Update(ctx, id, domain.SaveMobileDeviceRequest{DeviceAddr: "100.99.251.76:41003"}); err != nil {
		t.Fatalf("update after reboot: %v", err)
	}
	got := store.devices[0]
	if got.DeviceAddr != "100.99.251.76:41003" || got.DeviceUDID != "127.0.0.1:5555" || got.Name != "Pixel" {
		t.Fatalf("after a reboot the registration is %+v, want only the address changed", got)
	}
}

// iOS is refused with the reason, not as an unknown value: the operator is being
// told about the cluster, not about their typing.
func TestAddRefusesIOS(t *testing.T) {
	svc := NewService(&fakeStore{}, nil, envDevice())
	_, err := svc.Add(context.Background(), domain.SaveMobileDeviceRequest{
		Name: "iPhone", Platform: domain.PlatformIOS, DeviceAddr: "100.99.251.76:36745",
	})
	if err == nil {
		t.Fatal("an iOS device was accepted")
	}
}

// The env-configured device is a supported install, not dead code: it is what
// drives an installation nobody has registered anything on.
func TestEffectiveFallsBackToTheEnvironment(t *testing.T) {
	svc := NewService(&fakeStore{}, nil, envDevice())
	devices, source, err := svc.Effective(context.Background())
	if err != nil {
		t.Fatalf("effective: %v", err)
	}
	if source != "env" || len(devices) != 1 || devices[0].DeviceUDID != "127.0.0.1:5555" {
		t.Fatalf("effective returned %v from %q, want the env device", devices, source)
	}
	if !devices[0].Managed() {
		t.Fatal("the env device reports as a registration, so the UI would offer to delete a value it cannot touch")
	}
}

// Once anything is registered the environment stops contributing. A union would
// list one phone twice — the env device and the registration normally describe
// the same 127.0.0.1:5555 — and two runs would each take what they believed was
// a free device.
func TestRegistrationsReplaceTheEnvironment(t *testing.T) {
	store := &fakeStore{}
	svc := NewService(store, bridgeSaying(t, "127.0.0.1:5556"), envDevice())
	ctx := context.Background()

	if _, err := svc.Add(ctx, domain.SaveMobileDeviceRequest{Name: "Pixel", DeviceAddr: "100.99.251.76:36745"}); err != nil {
		t.Fatalf("add: %v", err)
	}
	devices, source, err := svc.Effective(ctx)
	if err != nil {
		t.Fatalf("effective: %v", err)
	}
	if source != "settings" || len(devices) != 1 || devices[0].Name != "Pixel" {
		t.Fatalf("effective returned %v from %q, want only the registration", devices, source)
	}
	// …and removing it hands the installation back to the environment rather
	// than leaving it with no phone at all.
	if _, err := svc.Remove(ctx, devices[0].ID); err != nil {
		t.Fatalf("remove: %v", err)
	}
	devices, source, err = svc.Effective(ctx)
	if err != nil {
		t.Fatalf("effective after remove: %v", err)
	}
	if source != "env" || len(devices) != 1 {
		t.Fatalf("after removing the registration effective is %v from %q, want the env device", devices, source)
	}
}

// The env device has no row, so nothing about it can be edited from a browser.
// Saying so is the point: silently discarding the edit would leave an operator
// convinced they changed something.
func TestEnvDeviceCannotBeEdited(t *testing.T) {
	svc := NewService(&fakeStore{}, nil, envDevice())
	if _, err := svc.Update(context.Background(), uuid.Nil, domain.SaveMobileDeviceRequest{DeviceAddr: "1.2.3.4:5"}); err == nil {
		t.Fatal("the env device accepted an edit")
	}
	if _, err := svc.Remove(context.Background(), uuid.Nil); err == nil {
		t.Fatal("the env device accepted a removal")
	}
}
