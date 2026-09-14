package board

// The offline-Mac park and its release.
//
// This park is the one with no clock and no answer coming: a subscription
// reopens at an instant the CLI printed and a phone can be asked whether it is
// free, but the only thing that knows whether somebody's laptop is open is the
// control plane. Everything here is about that asymmetry — probing once per
// PERSON rather than once per card, treating an ambiguous probe as "back", and
// never letting a card sit forever on a probe that is wrong in a way that
// repeats.

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/platform/tenant"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

type stubRunnerParkStore struct {
	parked     []domain.BoardTask
	takenIDs   []uuid.UUID
	listErr    error
	takeMisses map[uuid.UUID]bool
}

func (s *stubRunnerParkStore) ListBlockedByResource(context.Context, string, int) ([]domain.BoardTask, error) {
	if s.listErr != nil {
		return nil, s.listErr
	}
	return s.parked, nil
}

func (s *stubRunnerParkStore) TakeBlockedResourceTask(_ context.Context, _ string, taskID uuid.UUID) (domain.BoardTask, bool, error) {
	if s.takeMisses[taskID] {
		return domain.BoardTask{}, false, nil
	}
	s.takenIDs = append(s.takenIDs, taskID)
	for _, t := range s.parked {
		if t.ID == taskID {
			return t, true, nil
		}
	}
	return domain.BoardTask{}, false, nil
}

// stubTransport answers preflight.report per member.
type stubTransport struct {
	byMember map[string]error
	calls    map[string]int
}

func (s *stubTransport) Configured() bool { return true }

func (s *stubTransport) Do(_ context.Context, memberUID string, call port.RunnerCall) (json.RawMessage, error) {
	if s.calls == nil {
		s.calls = map[string]int{}
	}
	s.calls[memberUID]++
	if err := s.byMember[memberUID]; err != nil {
		return nil, err
	}
	return []byte(`{"ok":true}`), nil
}

func runnerParkedTask(member string) domain.BoardTask {
	return domain.BoardTask{
		ID:             uuid.New(),
		RepositoryID:   uuid.New(),
		Column:         domain.TaskColumnInProgress,
		TaskType:       domain.TaskTypeTask,
		AssigneeUserID: member,
	}
}

func runnerSweepCtx() context.Context {
	return tenant.With(context.Background(), tenant.Identity{
		TenantID: uuid.New(), Role: tenant.RoleMember, UserID: "sweeper",
	})
}

// TestOneProbePerMemberNotPerCard is the design. Every card assigned to one
// person waits for exactly one laptop and they all become runnable at the same
// instant, so probing per card multiplies one fact by however many tasks that
// person has open — across every tenant on the fleet, forever, over somebody's
// home internet.
func TestOneProbePerMemberNotPerCard(t *testing.T) {
	a1, a2, a3 := runnerParkedTask("alice"), runnerParkedTask("alice"), runnerParkedTask("alice")
	b1 := runnerParkedTask("bob")
	store := &stubRunnerParkStore{parked: []domain.BoardTask{a1, a2, a3, b1}}
	tr := &stubTransport{byMember: map[string]error{
		"bob": &domain.RunnerBlock{MemberUID: "bob"},
	}}
	s := NewRunnerSweeper(store, tr, &Dispatcher{})
	require.NotNil(t, s)

	s.sweep(runnerSweepCtx())

	assert.Equal(t, 1, tr.calls["alice"], "three of alice's cards are one laptop, so one probe")
	assert.Equal(t, 1, tr.calls["bob"])
	assert.Len(t, store.takenIDs, 3, "every card of a member whose Mac is back is released at once")
	assert.NotContains(t, store.takenIDs, b1.ID)
}

// TestASleepingMacKeepsItsCardsParked. The control plane's 409 is the one
// unambiguous "there is no laptop", and it is the only answer that leaves the
// cards where they are.
func TestASleepingMacKeepsItsCardsParked(t *testing.T) {
	task := runnerParkedTask("alice")
	store := &stubRunnerParkStore{parked: []domain.BoardTask{task}}
	tr := &stubTransport{byMember: map[string]error{
		"alice": &domain.RunnerBlock{MemberUID: "alice", Detail: "no runner session"},
	}}

	NewRunnerSweeper(store, tr, &Dispatcher{}).sweep(runnerSweepCtx())

	assert.Empty(t, store.takenIDs)
}

// TestAnAmbiguousProbeCountsAsAttached, and the asymmetry is deliberate.
// `preflight.report` can legitimately answer `not_ready` (the supervisor has
// not pushed a report yet), and a tunnel can break mid-probe — both mean
// SOMETHING answered. A wrong "yes" costs one dispatch that parks again in
// seconds; a wrong "no" strands a working machine's whole board until somebody
// notices.
func TestAnAmbiguousProbeCountsAsAttached(t *testing.T) {
	for name, probeErr := range map[string]error{
		"not_ready":       &domain.RunnerBlock{MemberUID: "alice", NotReady: true},
		"runner failure":  errors.New("runner: preflight.report failed (internal): boom"),
		"tunnel mid-read": errors.New("runner: the Mac's connection ended before the call finished"),
	} {
		t.Run(name, func(t *testing.T) {
			task := runnerParkedTask("alice")
			store := &stubRunnerParkStore{parked: []domain.BoardTask{task}}
			tr := &stubTransport{byMember: map[string]error{"alice": probeErr}}

			NewRunnerSweeper(store, tr, &Dispatcher{}).sweep(runnerSweepCtx())

			require.Len(t, store.takenIDs, 1, "something answered, so the Mac is treated as back")
		})
	}
}

// TestAnUnassignedParkedCardIsLeftForAHuman. Nothing here can ever release it —
// there is no member to probe — so it is left alone rather than resumed into a
// dispatch that has no Mac to aim at.
func TestAnUnassignedParkedCardIsLeftForAHuman(t *testing.T) {
	orphan := runnerParkedTask("")
	store := &stubRunnerParkStore{parked: []domain.BoardTask{orphan}}
	tr := &stubTransport{}

	NewRunnerSweeper(store, tr, &Dispatcher{}).sweep(runnerSweepCtx())

	assert.Empty(t, tr.calls, "there is nobody to probe")
	assert.Empty(t, store.takenIDs)
}

// TestLosingTheClaimIsNormal: another replica sweeping, or a human dragging the
// card out of blocked. Both are fine and neither is an error.
func TestRunnerSweepToleratesLosingTheClaim(t *testing.T) {
	task := runnerParkedTask("alice")
	store := &stubRunnerParkStore{
		parked:     []domain.BoardTask{task},
		takeMisses: map[uuid.UUID]bool{task.ID: true},
	}
	NewRunnerSweeper(store, &stubTransport{}, &Dispatcher{}).sweep(runnerSweepCtx()) // must not panic or dispatch
}

// TestNoSweeperWithoutATransport. On a self-hosted or desktop install nothing
// can park on an absent Mac, so a sweeper for it would be a query per pass
// forever for a state that cannot occur.
func TestNoSweeperWithoutATransport(t *testing.T) {
	store := &stubRunnerParkStore{}
	assert.Nil(t, NewRunnerSweeper(store, nil, &Dispatcher{}))
	assert.Nil(t, NewRunnerSweeper(store, unconfiguredTransport{}, &Dispatcher{}))
	assert.Nil(t, NewRunnerSweeper(nil, &stubTransport{}, &Dispatcher{}))
	assert.Nil(t, NewRunnerSweeper(store, &stubTransport{}, nil))
	// And a nil sweeper's Start is a no-op rather than a panic, because that is
	// what runtime.go relies on to register nothing.
	(*RunnerSweeper)(nil).Start(context.Background(), time.Second)
}

type unconfiguredTransport struct{}

func (unconfiguredTransport) Configured() bool { return false }
func (unconfiguredTransport) Do(context.Context, string, port.RunnerCall) (json.RawMessage, error) {
	return nil, errors.New("not configured")
}

// TestAFailedListEndsThePassQuietly. A database blip must not take the board
// down; the next tick asks again.
func TestAFailedListEndsThePassQuietly(t *testing.T) {
	store := &stubRunnerParkStore{listErr: errors.New("connection reset")}
	tr := &stubTransport{}
	NewRunnerSweeper(store, tr, &Dispatcher{}).sweep(runnerSweepCtx())
	assert.Empty(t, tr.calls)
}

// ------------------------------------------------------- the livelock brake

// The park with no clock is the one that can cycle forever. A control plane
// answering "attached" for a Mac that then refuses every call, or a card
// assigned to somebody who no longer has a machine on this workspace, produces
// a loop in which every individual step is correct: the sweeper releases, the
// run parks, the sweeper releases. Nothing else in the board would ever notice.
//
// runnerParkStreak is the brake, and it reads the SUMMARY rather than a column
// because a park leaves no distinguishing one — which is exactly why
// parkOnRunner writes a fixed sentence from RunnerBlock.BoardDetail instead of
// free text. These tests pin the two halves of that coupling.

func parkedRun(summary string) domain.TaskAgentRun {
	return domain.TaskAgentRun{ID: uuid.New(), Status: domain.TaskAgentRunStatusCompleted, Summary: summary}
}

func TestRunnerParkStreakCountsOnlyTheUnbrokenRunOfParks(t *testing.T) {
	waiting := (&domain.RunnerBlock{}).BoardDetail()
	notReady := (&domain.RunnerBlock{NotReady: true}).BoardDetail()
	current := uuid.New()

	// Newest first, and the count stops at the first run that did anything else.
	runs := []domain.TaskAgentRun{
		{ID: current},
		parkedRun(waiting),
		parkedRun(waiting),
		parkedRun("Implemented the login form and opened a PR."),
		parkedRun(waiting),
	}
	assert.Equal(t, 2, runnerParkStreak(runs, current.String()),
		"a run that did real work resets the streak; the older parks are history, not a pattern")

	// The not-ready wording is a park too, so it must count.
	assert.Equal(t, 1, runnerParkStreak([]domain.TaskAgentRun{parkedRun(notReady), parkedRun("shipped")}, current.String()))

	assert.Equal(t, 0, runnerParkStreak(nil, current.String()))
}

// TestEveryParkSentenceIsRecognisedAsAPark is the coupling test. If somebody
// rewords BoardDetail without touching the reader, the streak silently becomes
// zero forever and the brake stops existing — a failure that shows up as a
// board that never fails a card, which nobody reports as a bug.
func TestEveryParkSentenceIsRecognisedAsAPark(t *testing.T) {
	for _, block := range []*domain.RunnerBlock{
		{},
		{NotReady: true},
		{MemberUID: "alice", Detail: "no session"},
		nil,
	} {
		assert.True(t, isRunnerParkSummary(block.BoardDetail()),
			"BoardDetail wrote a sentence the streak counter does not recognise: %q", block.BoardDetail())
	}
	assert.False(t, isRunnerParkSummary("Waiting for the reviewer."),
		"a summary about something else must not be counted as an offline-Mac park")
	assert.False(t, isRunnerParkSummary(""))
}

// TestTheCapIsReachedBeforeTheBoardGivesUpOnItsOwn: twenty parks at the
// sweeper's five-minute interval is most of two hours of continuous absence, so
// an ordinary closed laptop never trips it and a genuinely stuck card does.
func TestTheCapLeavesRoomForAnOrdinaryClosedLaptop(t *testing.T) {
	require.Greater(t, maxConsecutiveRunnerParks, 5,
		"an offline laptop is ordinary and a spent subscription is not; this cap must be looser than the quota's")
	assert.GreaterOrEqual(t, time.Duration(maxConsecutiveRunnerParks)*RunnerSweeperInterval, 90*time.Minute,
		"a person on a long meeting must find their tasks waiting, not failed")
}
