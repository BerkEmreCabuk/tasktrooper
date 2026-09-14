package http

import (
	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

// The mobile device registrations: what used to be an env var plus a kubectl
// exec into the Appium pod, as the endpoints the settings page drives.
//
// Every one of them returns the full LIST rather than an acknowledgement or the
// one device it touched. Attaching a phone is a sequence where each step changes
// what the next one should say — paired but not connected, connected but the
// hub is down — and with several phones an action on one changes what is true
// of the others: the device that just took the last free local port is the
// reason another one now reads as busy. A UI that has to re-fetch to find that
// out shows a stale list between the two calls.
func (h *Handler) registerMobileDeviceRoutes(app *fiber.App) {
	app.Get("/v1/settings/mobile-devices", h.MobileDevices)
	// Before the :id routes in source order, though it does not collide with
	// any of them today — none of them is a GET. Kept adjacent to the list so
	// the pair reads as "what is registered" / "what could be".
	app.Get("/v1/settings/mobile-devices/local-catalog", h.LocalDeviceCatalog)
	app.Post("/v1/settings/mobile-devices", h.AddMobileDevice)
	app.Put("/v1/settings/mobile-devices/:id", h.UpdateMobileDevice)
	app.Delete("/v1/settings/mobile-devices/:id", h.DeleteMobileDevice)
	app.Post("/v1/settings/mobile-devices/:id/pair", h.PairMobileDevice)
	app.Post("/v1/settings/mobile-devices/:id/connect", h.ConnectMobileDevice)
}

// mobileDeviceList is the one response shape every endpoint here returns.
type mobileDeviceList struct {
	Devices []domain.MobileDeviceStatus `json:"devices"`
}

func (h *Handler) mobileDeviceUnavailable(c *fiber.Ctx) error {
	return c.Status(fiber.StatusServiceUnavailable).JSON(errorResponse{
		Error: errorDetail{Message: "mobile device registration not enabled", Type: "service_unavailable"},
	})
}

func (h *Handler) mobileDeviceID(c *fiber.Ctx) (uuid.UUID, bool) {
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return uuid.Nil, false
	}
	return id, true
}

func (h *Handler) MobileDevices(c *fiber.Ctx) error {
	if h.mobileDeviceSvc == nil {
		return h.mobileDeviceUnavailable(c)
	}
	out, err := h.mobileDeviceSvc.Statuses(h.enrichContext(c))
	if err != nil {
		return internalError(c, err)
	}
	return c.JSON(mobileDeviceList{Devices: out})
}

// LocalDeviceCatalog reports what THIS host could drive: the iOS simulators
// `xcrun simctl` lists and the AVDs the Android SDK lists.
//
// It exists because a local device cannot be typed in. A phone is registered by
// an address a human reads off its screen, but a simulator is registered by a
// UUID and an AVD by an exact SDK name — values nobody can recall and both of
// which fail late and unhelpfully when mistyped. So the server offers the list
// and the registration validates against the same source.
//
// Two empty arrays on a host with neither, which is every Linux cluster node.
// That is a 200 rather than a 404 on purpose: "this host has no simulators" is
// an answer, and a UI that has to distinguish it from a missing endpoint will
// get it wrong.
func (h *Handler) LocalDeviceCatalog(c *fiber.Ctx) error {
	if h.mobileDeviceSvc == nil {
		return h.mobileDeviceUnavailable(c)
	}
	out, err := h.mobileDeviceSvc.LocalCatalog(h.enrichContext(c))
	if err != nil {
		return internalError(c, err)
	}
	return c.JSON(out)
}

func (h *Handler) AddMobileDevice(c *fiber.Ctx) error {
	if h.mobileDeviceSvc == nil {
		return h.mobileDeviceUnavailable(c)
	}
	var req domain.SaveMobileDeviceRequest
	if err := c.BodyParser(&req); err != nil {
		return badRequest(c, "invalid request body")
	}
	out, err := h.mobileDeviceSvc.Add(h.enrichContext(c), req)
	if err != nil {
		// The failures here are all operator input — a missing name, an iOS
		// device this installation cannot drive, a cipher that cannot encrypt
		// the PIN — so they are 400s with the service's own wording rather than
		// an opaque 500.
		return badRequest(c, err.Error())
	}
	return c.JSON(mobileDeviceList{Devices: out})
}

func (h *Handler) UpdateMobileDevice(c *fiber.Ctx) error {
	if h.mobileDeviceSvc == nil {
		return h.mobileDeviceUnavailable(c)
	}
	id, ok := h.mobileDeviceID(c)
	if !ok {
		return badRequest(c, "invalid device id")
	}
	var req domain.SaveMobileDeviceRequest
	if err := c.BodyParser(&req); err != nil {
		return badRequest(c, "invalid request body")
	}
	out, err := h.mobileDeviceSvc.Update(h.enrichContext(c), id, req)
	if err != nil {
		return badRequest(c, err.Error())
	}
	return c.JSON(mobileDeviceList{Devices: out})
}

func (h *Handler) DeleteMobileDevice(c *fiber.Ctx) error {
	if h.mobileDeviceSvc == nil {
		return h.mobileDeviceUnavailable(c)
	}
	id, ok := h.mobileDeviceID(c)
	if !ok {
		return badRequest(c, "invalid device id")
	}
	out, err := h.mobileDeviceSvc.Remove(h.enrichContext(c), id)
	if err != nil {
		return badRequest(c, err.Error())
	}
	return c.JSON(mobileDeviceList{Devices: out})
}

func (h *Handler) PairMobileDevice(c *fiber.Ctx) error {
	if h.mobileDeviceSvc == nil {
		return h.mobileDeviceUnavailable(c)
	}
	id, ok := h.mobileDeviceID(c)
	if !ok {
		return badRequest(c, "invalid device id")
	}
	var req domain.PairMobileDeviceRequest
	if err := c.BodyParser(&req); err != nil {
		return badRequest(c, "invalid request body")
	}
	out, err := h.mobileDeviceSvc.Pair(h.enrichContext(c), id, req)
	if err != nil {
		// A wrong or expired pairing code is the common case and is the
		// operator's to fix — the code is only valid while the phone's dialog
		// is open, so "try again with the code showing now" is the answer.
		return badRequest(c, err.Error())
	}
	return c.JSON(mobileDeviceList{Devices: out})
}

func (h *Handler) ConnectMobileDevice(c *fiber.Ctx) error {
	if h.mobileDeviceSvc == nil {
		return h.mobileDeviceUnavailable(c)
	}
	id, ok := h.mobileDeviceID(c)
	if !ok {
		return badRequest(c, "invalid device id")
	}
	out, err := h.mobileDeviceSvc.Connect(h.enrichContext(c), id)
	if err != nil {
		return badRequest(c, err.Error())
	}
	return c.JSON(mobileDeviceList{Devices: out})
}
