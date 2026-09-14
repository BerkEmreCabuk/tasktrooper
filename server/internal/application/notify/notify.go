// Package notify pushes board events to the user's registered devices (APNs).
// Delivery is best-effort and fully async: a failed push never affects board
// flow, and dead tokens (APNs 410) are pruned on the spot.
package notify

import (
	"context"
	"fmt"
	"time"

	"github.com/rs/zerolog/log"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/platform/tenant"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

const sendTimeout = 15 * time.Second

// notifiedColumns are the moves worth interrupting a human for: the two
// human gates, revision bounces, and completion.
var notifiedColumns = map[domain.TaskColumn]string{
	domain.TaskColumnAnalizReview: "Analysis awaiting approval",
	domain.TaskColumnHumanUAT:     "Awaiting your approval (UAT)",
	domain.TaskColumnNeedRevision: "Sent back for revision",
	domain.TaskColumnBlocked:      "Blocked on your answer",
	domain.TaskColumnDone:         "Done",
}

// activityAttributesType is the ActivityAttributes struct name on the iOS side.
const activityAttributesType = "TaskRunAttributes"

// columnLabels are the labels shown in the Live Activity content state.
var columnLabels = map[domain.TaskColumn]string{
	domain.TaskColumnInProgress:   "Agent working",
	domain.TaskColumnCodeReview:   "In code review",
	domain.TaskColumnReadyForQA:   "Ready for QA",
	domain.TaskColumnInQA:         "In QA",
	domain.TaskColumnNeedRevision: "In revision",
	domain.TaskColumnAnalizReview: "Analysis in review",
	domain.TaskColumnPMUAT:        "PM UAT",
	domain.TaskColumnHumanUAT:     "In approval",
	domain.TaskColumnBlocked:      "Waiting for your answer",
	domain.TaskColumnDone:         "Done",
}

// terminalColumns ends the task's Live Activity.
var terminalColumns = map[domain.TaskColumn]bool{
	domain.TaskColumnDone:     true,
	domain.TaskColumnReleased: true,
}

type Service struct {
	devices    port.PushDeviceStore
	sender     port.PushSender
	activities port.LiveActivityTokenStore
}

func New(devices port.PushDeviceStore, sender port.PushSender) *Service {
	return &Service{devices: devices, sender: sender}
}

// SetLiveActivities enables iOS Live Activity updates; nil leaves them off.
func (s *Service) SetLiveActivities(store port.LiveActivityTokenStore) {
	s.activities = store
}

// Alert pushes a free-form message (production incidents, recoveries) to the
// user's devices. Like every push here it is best-effort and non-blocking.
// ctx is taken for the tenant on it, not for its lifetime: the send outlives
// the incident that triggered it, and the device list it reads is per-tenant.
// See tenant.Detach.
func (s *Service) Alert(ctx context.Context, title, body string) {
	go s.broadcast(tenant.Detach(ctx), title, body)
}

// TaskMoved fires a push when the destination column is one a human cares
// about, and drives the task's Live Activity. It returns immediately;
// delivery happens in the background.
func (s *Service) TaskMoved(ctx context.Context, task domain.BoardTask) {
	ctx = tenant.Detach(ctx)
	if reason, ok := notifiedColumns[task.Column]; ok {
		title := fmt.Sprintf("%s · %s", task.Key, reason)
		go s.broadcast(ctx, title, task.Title)
	}
	if s.activities != nil {
		go s.driveLiveActivity(ctx, task)
	}
}

// TaskResumed fires the push for a task a sweeper released from `blocked`, and
// drives the Live Activity exactly as TaskMoved does.
//
// It is the ALTERNATIVE to TaskMoved, never an addition — the dispatcher calls
// one or the other for a given move (see board.Dispatcher), so a resume produces
// exactly one notification. Which matters most where the two would otherwise
// overlap: a task resuming into need_revision or done lands in a notified
// column, and would have sent "Sent back for revision" beside this one. It also
// says the more accurate thing, since nothing was newly rejected or finished —
// the card simply started moving again.
//
// Unlike TaskMoved this is unconditional on the column. The park is what the
// user was told about; being told it ended is the other half of that, whichever
// column the task went back to.
//
// resource is the blocked_resource the task was parked on, and rides along so
// the client can say WHICH wait ended. Returns immediately; delivery is
// best-effort and cannot affect the resume, which is already committed.
func (s *Service) TaskResumed(ctx context.Context, task domain.BoardTask, resource string) {
	ctx = tenant.Detach(ctx)
	data := map[string]string{
		// type/task_id/repository_id are required by the client: the tap cannot
		// route without them, and the repository id specifically because tasks
		// are fetched per repository.
		"type":          pushTypeTaskResumed,
		"task_id":       task.ID.String(),
		"repository_id": task.RepositoryID.String(),
		"task_key":      task.Key,
	}
	// Informational, and omitted rather than sent empty: an unparked-looking
	// resume is a bug worth seeing as an absent key, not as "".
	if resource != "" {
		data["blocked_resource"] = resource
	}
	go s.broadcastData(ctx, taskResumedTitle(task.Key), taskResumedBody(resource), data)
	if s.activities != nil {
		go s.driveLiveActivity(ctx, task)
	}
}

// driveLiveActivity starts the lock-screen card when work begins, updates it
// on every move, and ends it on terminal columns.
func (s *Service) driveLiveActivity(parent context.Context, task domain.BoardTask) {
	ctx, cancel := context.WithTimeout(parent, sendTimeout)
	defer cancel()

	label := columnLabels[task.Column]
	if label == "" {
		label = string(task.Column)
	}
	state := map[string]any{"column": string(task.Column), "label": label}

	if task.Column == domain.TaskColumnInProgress {
		tokens, err := s.activities.StartTokens(ctx)
		if err != nil {
			log.Warn().Err(err).Msg("live activity: start tokens query failed")
			return
		}
		// taskID lets iOS bind its update token to the right task.
		attrs := map[string]any{"taskKey": task.Key, "taskTitle": task.Title, "taskID": task.ID.String()}
		for _, t := range tokens {
			unregistered, err := s.sender.SendLiveActivity(
				ctx, t, "start", activityAttributesType, attrs, state, "", "")
			s.pruneOnDead(ctx, t, unregistered, err, "start")
		}
		return
	}

	tokens, err := s.activities.UpdateTokensForTask(ctx, task.ID.String())
	if err != nil {
		log.Warn().Err(err).Msg("live activity: update tokens query failed")
		return
	}
	event := "update"
	if terminalColumns[task.Column] {
		event = "end"
	}
	for _, t := range tokens {
		unregistered, err := s.sender.SendLiveActivity(ctx, t, event, "", nil, state, "", "")
		s.pruneOnDead(ctx, t, unregistered, err, event)
		if event == "end" {
			_ = s.activities.Delete(ctx, t)
		}
	}
}

func (s *Service) pruneOnDead(ctx context.Context, token string, unregistered bool, err error, event string) {
	if err != nil {
		log.Warn().Err(err).Str("event", event).Msg("live activity: send failed")
		return
	}
	if unregistered {
		if err := s.activities.Delete(ctx, token); err != nil {
			log.Warn().Err(err).Msg("live activity: prune failed")
		}
	}
}

func (s *Service) broadcast(ctx context.Context, title, body string) {
	s.broadcastData(ctx, title, body, nil)
}

// broadcastData sends one alert to every registered device, carrying data keys
// for the notifications a client has to route on. A failure — the device list,
// one device's send — is logged and skipped: this runs behind `go` on a board
// action that has already committed, so there is nothing it could usefully
// fail.
func (s *Service) broadcastData(parent context.Context, title, body string, data map[string]string) {
	ctx, cancel := context.WithTimeout(parent, sendTimeout)
	defer cancel()
	devices, err := s.devices.List(ctx)
	if err != nil {
		log.Warn().Err(err).Msg("push: device list failed")
		return
	}
	for _, d := range devices {
		unregistered, err := s.sender.SendData(ctx, d.DeviceToken, title, body, data)
		if err != nil {
			log.Warn().Err(err).Str("device", d.ID).Msg("push: send failed")
			continue
		}
		if unregistered {
			if err := s.devices.Delete(ctx, d.DeviceToken); err != nil {
				log.Warn().Err(err).Str("device", d.ID).Msg("push: prune failed")
			}
		}
	}
}
