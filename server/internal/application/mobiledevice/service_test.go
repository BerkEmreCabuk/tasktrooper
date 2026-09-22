package mobiledevice

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"

	"github.com/makifbaysal/tasktrooper/server/internal/adapter/cloud/deviceagent"
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

func TestAddRefusesIOS(t *testing.T) {
	svc := NewService(&fakeStore{}, nil, envDevice())
	_, err := svc.Add(context.Background(), domain.SaveMobileDeviceRequest{
		Name: "iPhone", Platform: domain.PlatformIOS, DeviceAddr: "100.99.251.76:36745",
	})
	if err == nil {
		t.Fatal("an iOS device was accepted")
	}
}

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

func TestEnvDeviceCannotBeEdited(t *testing.T) {
	svc := NewService(&fakeStore{}, nil, envDevice())
	if _, err := svc.Update(context.Background(), uuid.Nil, domain.SaveMobileDeviceRequest{DeviceAddr: "1.2.3.4:5"}); err == nil {
		t.Fatal("the env device accepted an edit")
	}
	if _, err := svc.Remove(context.Background(), uuid.Nil); err == nil {
		t.Fatal("the env device accepted a removal")
	}
}
