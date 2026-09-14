package board

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

// parkEventStore is the board_events table, remembered. Locked because a park
// is written from the run's own goroutine in the runner tests.
type parkEventStore struct {
	mu     sync.Mutex
	events []domain.BoardEvent
	err    error
}

func (s *parkEventStore) Create(_ context.Context, event domain.BoardEvent) (domain.BoardEvent, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.err != nil {
		return domain.BoardEvent{}, s.err
	}
	event.ID = uuid.New()
	event.CreatedAt = time.Date(2026, 8, 17, 9, 0, 0, 0, time.UTC)
	s.events = append(s.events, event)
	return event, nil
}

func (s *parkEventStore) ListRecent(context.Context, int) ([]domain.BoardEvent, error) {
	return s.all(), nil
}

func (s *parkEventStore) ListByTask(context.Context, uuid.UUID, int) ([]domain.BoardEvent, error) {
	return s.all(), nil
}

func (s *parkEventStore) all() []domain.BoardEvent {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]domain.BoardEvent(nil), s.events...)
}

// payloads decodes what was written, which is the only thing the UI ever sees.
func (s *parkEventStore) payloads() []map[string]interface{} {
	out := []map[string]interface{}{}
	for _, e := range s.all() {
		var p map[string]interface{}
		if err := json.Unmarshal(e.Payload, &p); err != nil {
			continue
		}
		out = append(out, p)
	}
	return out
}

type spanMove struct {
	taskID uuid.UUID
	column string
	at     time.Time
}

// parkSpanStore records only RecordMove; the rest of the ledger is not on the
// park path and answers emptily.
type parkSpanStore struct {
	mu    sync.Mutex
	moves []spanMove
	err   error
}

func (s *parkSpanStore) RecordMove(_ context.Context, _, taskID uuid.UUID, toColumn string, at time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.err != nil {
		return s.err
	}
	s.moves = append(s.moves, spanMove{taskID: taskID, column: toColumn, at: at})
	return nil
}

func (s *parkSpanStore) recorded() []spanMove {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]spanMove(nil), s.moves...)
}

func (s *parkSpanStore) AttachAgent(context.Context, uuid.UUID, uuid.UUID) error { return nil }
func (s *parkSpanStore) SetReviewVerdict(context.Context, uuid.UUID, string, string) error {
	return nil
}
func (s *parkSpanStore) OpenSpan(context.Context, uuid.UUID) (domain.TaskColumnSpan, bool, error) {
	return domain.TaskColumnSpan{}, false, nil
}
func (s *parkSpanStore) OwnersForTask(context.Context, uuid.UUID) (map[string]uuid.UUID, error) {
	return nil, nil
}
func (s *parkSpanStore) HasVisited(context.Context, uuid.UUID, string) (bool, error) {
	return false, nil
}
func (s *parkSpanStore) LatestVerdicts(context.Context, uuid.UUID) (map[string]string, error) {
	return nil, nil
}
func (s *parkSpanStore) ListByTask(context.Context, uuid.UUID) ([]domain.TaskColumnSpan, error) {
	return nil, nil
}
func (s *parkSpanStore) ColumnDurations(context.Context, uuid.UUID, time.Time, time.Time) (map[string]time.Duration, error) {
	return nil, nil
}
func (s *parkSpanStore) CleanTaskHours(context.Context, uuid.UUID, []string, time.Time, time.Time) ([]float64, error) {
	return nil, nil
}

func journalTask() domain.BoardTask {
	return domain.BoardTask{
		ID:           uuid.New(),
		RepositoryID: uuid.New(),
		Column:       domain.TaskColumnInProgress,
	}
}

// The park has to read on the board exactly like any other system move: same
// from/to columns, same actor, and a reason the timeline can render. Without
// this event the card jumped to `blocked` with nothing in its history to say
// when or why.
func TestParkJournalRecordsTheMoveTheDispatcherNeverWrites(t *testing.T) {
	events, spans := &parkEventStore{}, &parkSpanStore{}
	task := journalTask()
	agentID := uuid.New()
	task.AssigneeAgentID = &agentID

	NewParkJournal(events, spans).Record(context.Background(), task.RepositoryID, task,
		domain.TaskColumnInProgress, domain.ResourceClaudeCodeQuota, domain.MoveReasonQuotaExhausted)

	written := events.all()
	require.Len(t, written, 1)
	assert.Equal(t, domain.BoardEventTaskMoved, written[0].EventType)
	assert.Equal(t, task.ID, written[0].TaskID)
	assert.Equal(t, task.RepositoryID, written[0].RepositoryID)

	payload := events.payloads()[0]
	assert.Equal(t, string(domain.TaskColumnInProgress), payload["from_column"])
	assert.Equal(t, string(domain.TaskColumnBlocked), payload["to_column"])
	assert.Equal(t, string(domain.TaskColumnBlocked), payload["column"],
		"Dispatch stamps the destination as `column` on every event; a park that omits it renders half a row")
	assert.Equal(t, domain.ResourceClaudeCodeQuota, payload["resource"])
	assert.Equal(t, domain.EventActorSystem, payload[domain.EventPayloadActor],
		"a park attributed to nobody renders as `by User` — a move the human never made")
	assert.Equal(t, domain.MoveReasonQuotaExhausted, payload[domain.EventPayloadReason])
	assert.Equal(t, agentID.String(), payload["assignee_agent_id"])
}

// The span ledger has to close the visit the run was in. Left open, a task that
// waited six hours for a quota reads as six hours of in_progress work in every
// time-in-column KPI.
func TestParkJournalClosesTheColumnSpan(t *testing.T) {
	events, spans := &parkEventStore{}, &parkSpanStore{}
	task := journalTask()

	NewParkJournal(events, spans).Record(context.Background(), task.RepositoryID, task,
		domain.TaskColumnInQA, domain.ResourceMobileDevice, domain.MoveReasonResourceBlocked)

	moves := spans.recorded()
	require.Len(t, moves, 1)
	assert.Equal(t, task.ID, moves[0].taskID)
	assert.Equal(t, string(domain.TaskColumnBlocked), moves[0].column)
	assert.Equal(t, events.all()[0].CreatedAt, moves[0].at,
		"the span is stamped with the event's time, so the ledger and the history agree")
}

// The store answers "" for a task that no longer exists, and a park that raced a
// delete must still not invent a column. The task's own column is the one the
// run was looking at, so it is the honest fallback.
func TestParkJournalFallsBackToTheTasksOwnColumn(t *testing.T) {
	events := &parkEventStore{}
	task := journalTask()

	NewParkJournal(events, nil).Record(context.Background(), task.RepositoryID, task,
		"", domain.ResourceDeployWatch, domain.MoveReasonResourceBlocked)

	require.Len(t, events.payloads(), 1)
	assert.Equal(t, string(domain.TaskColumnInProgress), events.payloads()[0]["from_column"])
}

// Nothing moved, so nothing is recorded: a task parked while already blocked
// (re-parked by a second run) would otherwise litter the timeline with
// blocked → blocked rows. repository.Service guards its own move event the same
// way.
func TestParkJournalIgnoresAnAlreadyBlockedTask(t *testing.T) {
	events, spans := &parkEventStore{}, &parkSpanStore{}
	task := journalTask()
	task.Column = domain.TaskColumnBlocked

	NewParkJournal(events, spans).Record(context.Background(), task.RepositoryID, task,
		domain.TaskColumnBlocked, domain.ResourceClaudeCodeQuota, domain.MoveReasonQuotaExhausted)

	assert.Empty(t, events.all())
	assert.Empty(t, spans.recorded())
}

// The park is already durable by the time the journal runs. A history write that
// failed must cost a timeline row and nothing else — never a panic in the run's
// goroutine, and never an unwind of the park.
func TestParkJournalSurvivesItsOwnFailures(t *testing.T) {
	task := journalTask()
	ctx := context.Background()

	assert.NotPanics(t, func() {
		var nilJournal *ParkJournal
		nilJournal.Record(ctx, task.RepositoryID, task, domain.TaskColumnInProgress, domain.ResourceWorkOrder, domain.MoveReasonResourceBlocked)
	}, "a build with no journal wired parks exactly as it did before")

	assert.NotPanics(t, func() {
		NewParkJournal(nil, nil).Record(ctx, task.RepositoryID, task, domain.TaskColumnInProgress, domain.ResourceWorkOrder, domain.MoveReasonResourceBlocked)
	})

	failing := &parkEventStore{err: errors.New("pool closed")}
	spans := &parkSpanStore{}
	assert.NotPanics(t, func() {
		NewParkJournal(failing, spans).Record(ctx, task.RepositoryID, task, domain.TaskColumnInProgress, domain.ResourceWorkOrder, domain.MoveReasonResourceBlocked)
	})
	assert.Empty(t, spans.recorded(), "no event means no timestamp to stamp the span with")

	assert.NotPanics(t, func() {
		NewParkJournal(&parkEventStore{}, &parkSpanStore{err: errors.New("pool closed")}).
			Record(ctx, task.RepositoryID, task, domain.TaskColumnInProgress, domain.ResourceWorkOrder, domain.MoveReasonResourceBlocked)
	})
}
