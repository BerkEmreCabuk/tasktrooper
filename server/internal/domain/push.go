package domain

import "time"

// PushDevice is a registered APNs device token for the tenant's user.
type PushDevice struct {
	ID          string
	DeviceToken string
	Platform    string
	CreatedAt   time.Time
	UpdatedAt   time.Time
}
