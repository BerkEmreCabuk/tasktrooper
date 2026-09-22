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

func TestParkJournalFallsBackToTheTasksOwnColumn(t *testing.T) {
	events := &parkEventStore{}
	task := journalTask()

	NewParkJournal(events, nil).Record(context.Background(), task.RepositoryID, task,
		"", domain.ResourceDeployWatch, domain.MoveReasonResourceBlocked)

	require.Len(t, events.payloads(), 1)
	assert.Equal(t, string(domain.TaskColumnInProgress), events.payloads()[0]["from_column"])
}

func TestParkJournalIgnoresAnAlreadyBlockedTask(t *testing.T) {
	events, spans := &parkEventStore{}, &parkSpanStore{}
	task := journalTask()
	task.Column = domain.TaskColumnBlocked

	NewParkJournal(events, spans).Record(context.Background(), task.RepositoryID, task,
		domain.TaskColumnBlocked, domain.ResourceClaudeCodeQuota, domain.MoveReasonQuotaExhausted)

	assert.Empty(t, events.all())
	assert.Empty(t, spans.recorded())
}

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
