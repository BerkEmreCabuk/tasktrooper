package http

import (
	"time"

	"github.com/gofiber/fiber/v2"
)

const dayLayout = "2006-01-02"

// UsageSummary serves token usage aggregates for the cost dashboard.
// GET /v1/usage?days=30&tz=Europe/Istanbul
func (h *Handler) UsageSummary(c *fiber.Ctx) error {
	if h.usageStore == nil {
		return fiber.NewError(fiber.StatusNotImplemented, "usage tracking is disabled")
	}
	days, loc, since, from, to := usageWindow(c.QueryInt("days", 30), c.Query("tz"), time.Now())

	summary, err := h.usageStore.Summary(c.UserContext(), since, loc)
	if err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, err.Error())
	}
	summary.Days = days
	summary.From = from
	summary.To = to
	return c.JSON(summary)
}

// usageWindow turns the request's days/tz query params into the store's query
// window. since is local midnight of today in loc, minus (days-1) days, so
// days=7 returns exactly 7 local calendar days including today — a pure
// function so the tz/day-boundary arithmetic is unit-testable without a store.
func usageWindow(requestedDays int, tz string, now time.Time) (days int, loc *time.Location, since time.Time, from, to string) {
	days = requestedDays
	if days < 1 || days > 365 {
		days = 30
	}
	loc = time.UTC
	// "Local" resolves in Go but means nothing to Postgres' AT TIME ZONE, and
	// the server's own zone is not the viewer's anyway.
	if tz != "" && tz != "Local" {
		if resolved, err := time.LoadLocation(tz); err == nil {
			loc = resolved
		}
	}
	nowLocal := now.In(loc)
	today := time.Date(nowLocal.Year(), nowLocal.Month(), nowLocal.Day(), 0, 0, 0, 0, loc)
	since = today.AddDate(0, 0, -(days - 1))
	return days, loc, since, since.Format(dayLayout), today.Format(dayLayout)
}
