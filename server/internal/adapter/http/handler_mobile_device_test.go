package http

import (
	"context"
	"encoding/json"
	"io"
	nethttp "net/http"
	"net/http/httptest"
	"testing"

	"github.com/gofiber/fiber/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/makifbaysal/tasktrooper/server/internal/application/mobiledevice"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

// stubLocalHost is a host with a fixed answer. Only Catalog is exercised here;
// the lifecycle half is covered where it lives, in application/mobiledevice.
type stubLocalHost struct {
	catalog domain.LocalDeviceCatalog
}

var _ mobiledevice.LocalHost = stubLocalHost{}

func (s stubLocalHost) SupportsIOSSimulators(context.Context) bool { return len(s.catalog.IOS) > 0 }
func (s stubLocalHost) SupportsAndroidEmulators(context.Context) bool {
	return len(s.catalog.Android) > 0
}

func (s stubLocalHost) Catalog(context.Context) (domain.LocalDeviceCatalog, error) {
	return s.catalog, nil
}

func (s stubLocalHost) Simulator(context.Context, string) (domain.LocalSimulator, bool, error) {
	return domain.LocalSimulator{}, false, nil
}
func (s stubLocalHost) SimulatorBooted(context.Context, string) (bool, error)  { return false, nil }
func (s stubLocalHost) BootSimulator(context.Context, string) error            { return nil }
func (s stubLocalHost) ShutdownSimulator(context.Context, string) error        { return nil }
func (s stubLocalHost) AVDExists(context.Context, string) (bool, error)        { return false, nil }
func (s stubLocalHost) EmulatorSerial(context.Context, string) (string, error) { return "", nil }
func (s stubLocalHost) StartEmulator(context.Context, string) (string, error)  { return "", nil }
func (s stubLocalHost) StopEmulator(context.Context, string) error             { return nil }

func mobileCatalogApp(t *testing.T, host mobiledevice.LocalHost) *fiber.App {
	t.Helper()
	svc := mobiledevice.NewService(nil, nil, domain.MobileDevice{})
	if host != nil {
		svc.SetLocalHost(host)
	}
	h := &Handler{mobileDeviceSvc: svc}
	app := fiber.New()
	h.registerMobileDeviceRoutes(app)
	return app
}

func getCatalogJSON(t *testing.T, app *fiber.App, path string) (int, []byte) {
	t.Helper()
	req := httptest.NewRequest(nethttp.MethodGet, path, nil)
	resp, err := app.Test(req, -1)
	require.NoError(t, err)
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	return resp.StatusCode, body
}

// The endpoint exists so a local device can be CHOSEN rather than typed: a
// simulator is identified by a UUID and an AVD by an exact SDK name, and both
// fail late and unhelpfully when mistyped.
func TestLocalCatalogListsWhatTheHostOffers(t *testing.T) {
	app := mobileCatalogApp(t, stubLocalHost{catalog: domain.LocalDeviceCatalog{
		IOS: []domain.LocalSimulator{
			{UDID: "11111111-2222-3333-4444-555555555555", Name: "iPhone 15", Runtime: "iOS 17.4", State: "Shutdown"},
		},
		Android: []string{"Pixel_7_API_34"},
	}})

	status, body := getCatalogJSON(t, app, "/v1/settings/mobile-devices/local-catalog")

	require.Equal(t, fiber.StatusOK, status)
	var out domain.LocalDeviceCatalog
	require.NoError(t, json.Unmarshal(body, &out))
	require.Len(t, out.IOS, 1)
	assert.Equal(t, "iPhone 15", out.IOS[0].Name)
	assert.Equal(t, "iOS 17.4", out.IOS[0].Runtime)
	assert.Equal(t, []string{"Pixel_7_API_34"}, out.Android)
}

// A Linux node answers 200 with two empty arrays, not a 404. "This host has no
// simulators" is an answer, and a UI that has to tell it from a missing
// endpoint will get it wrong.
func TestLocalCatalogIsEmptyArraysWithoutAHost(t *testing.T) {
	app := mobileCatalogApp(t, nil)

	status, body := getCatalogJSON(t, app, "/v1/settings/mobile-devices/local-catalog")

	require.Equal(t, fiber.StatusOK, status)
	// Asserted on the raw JSON rather than after decoding, because `null` and
	// `[]` decode into the same Go nil slice and the difference is exactly what
	// the browser sees.
	assert.JSONEq(t, `{"ios":[],"android":[]}`, string(body))
}

// The catalog route must not be swallowed by the :id routes it sits beside. It
// is a GET and they are not, so it cannot be today — this is the check that
// keeps it true when somebody adds GET /v1/settings/mobile-devices/:id.
func TestLocalCatalogRouteIsNotShadowed(t *testing.T) {
	app := mobileCatalogApp(t, stubLocalHost{catalog: domain.EmptyLocalDeviceCatalog()})

	status, body := getCatalogJSON(t, app, "/v1/settings/mobile-devices/local-catalog")

	require.Equal(t, fiber.StatusOK, status)
	assert.JSONEq(t, `{"ios":[],"android":[]}`, string(body))
}
