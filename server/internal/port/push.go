package port

import (
	"context"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

// PushDeviceStore persists APNs device registrations.
type PushDeviceStore interface {
	Upsert(ctx context.Context, deviceToken, platform string) (domain.PushDevice, error)
	Delete(ctx context.Context, deviceToken string) error
	List(ctx context.Context) ([]domain.PushDevice, error)
}

// PushSender delivers one notification to one device.
type PushSender interface {
	// Send returns domain-level ErrPushUnregistered (see adapter) semantics via
	// Unregistered==true when APNs reports the token dead.
	Send(ctx context.Context, deviceToken, title, body string) (unregistered bool, err error)
	// SendData is Send with application data delivered beside the alert, for the
	// notifications the client has to act on rather than merely display: the
	// keys travel as siblings of the APNs `aps` dictionary and are what a tap
	// routes on. Send is the same call with no data.
	SendData(ctx context.Context, deviceToken, title, body string, data map[string]string) (unregistered bool, err error)
	// SendLiveActivity drives an iOS Live Activity: event "start" (push-to-start,
	// needs attributesType+attributes), "update", or "end".
	SendLiveActivity(
		ctx context.Context, deviceToken, event, attributesType string,
		attributes, contentState map[string]any, alertTitle, alertBody string,
	) (unregistered bool, err error)
}

// LiveActivityTokenStore persists Live Activity push tokens.
type LiveActivityTokenStore interface {
	// UpsertStartToken saves a per-device push-to-start token (task-agnostic).
	UpsertStartToken(ctx context.Context, token string) error
	// UpsertUpdateToken binds a running activity's update token to a task.
	UpsertUpdateToken(ctx context.Context, token string, taskID string) error
	Delete(ctx context.Context, token string) error
	StartTokens(ctx context.Context) ([]string, error)
	UpdateTokensForTask(ctx context.Context, taskID string) ([]string, error)
}
