package notify

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/platform/tenant"
)

type sentPush struct {
	token string
	title string
	body  string
	data  map[string]string
}

// recordingSender captures what would have gone to APNs. Locked and channelled
// because every send here happens on a goroutine the caller does not wait for —
// which is the property under test as much as the payload is.
type recordingSender struct {
	mu   sync.Mutex
	sent []sentPush
	err  error
	done chan struct{}
}

func newRecordingSender() *recordingSender {
	return &recordingSender{done: make(chan struct{}, 8)}
}

func (s *recordingSender) Send(ctx context.Context, token, title, body string) (bool, error) {
	return s.SendData(ctx, token, title, body, nil)
}

func (s *recordingSender) SendData(_ context.Context, token, title, body string, data map[string]string) (bool, error) {
	s.mu.Lock()
	s.sent = append(s.sent, sentPush{token: token, title: title, body: body, data: data})
	err := s.err
	s.mu.Unlock()
	s.done <- struct{}{}
	return false, err
}

func (s *recordingSender) SendLiveActivity(context.Context, string, string, string, map[string]any, map[string]any, string, string) (bool, error) {
	return false, nil
}

// wait blocks until n sends have happened, so an assertion never races the
// goroutine it is about.
func (s *recordingSender) wait(t *testing.T, n int) {
	t.Helper()
	for range n {
		select {
		case <-s.done:
		case <-time.After(2 * time.Second):
			t.Fatal("timed out waiting for the push to be sent")
		}
	}
}

func (s *recordingSender) all() []sentPush {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]sentPush(nil), s.sent...)
}

// deviceStore is guarded because every send happens on a goroutine the caller
// does not wait for, so an assertion polling `deleted` races the delete that is
// filling it. The race is not new — `go test -race` simply had not been pointed
// at this package before.
type deviceStore struct {
	mu      sync.Mutex
	devices []domain.PushDevice
	err     error
	deleted []string
}

func (d *deviceStore) Upsert(context.Context, string, string) (domain.PushDevice, error) {
	return domain.PushDevice{}, nil
}
func (d *deviceStore) Delete(_ context.Context, token string) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.deleted = append(d.deleted, token)
	return nil
}

// deletedTokens snapshots, so a polling assertion never reads the slice the
// delete is appending to.
func (d *deviceStore) deletedTokens() []string {
	d.mu.Lock()
	defer d.mu.Unlock()
	return append([]string(nil), d.deleted...)
}
func (d *deviceStore) List(context.Context) ([]domain.PushDevice, error) {
	return d.devices, d.err
}

func oneDevice() *deviceStore {
	return &deviceStore{devices: []domain.PushDevice{{ID: "d1", DeviceToken: "tok-1"}}}
}

func parkedTask() domain.BoardTask {
	return domain.BoardTask{
		ID:           uuid.New(),
		RepositoryID: uuid.New(),
		Key:          "T-42",
		Title:        "Quota parked task",
		Column:       domain.TaskColumnInProgress,
	}
}

// The contract the iOS client was built against. type/task_id/repository_id are
// required — the tap cannot route without them, and the repository id
// specifically because the app fetches tasks per repository.
func TestTaskResumedSendsTheClientContract(t *testing.T) {
	sender, devices := newRecordingSender(), oneDevice()
	task := parkedTask()

	New(devices, sender).TaskResumed(context.Background(), task, domain.ResourceClaudeCodeQuota)
	sender.wait(t, 1)

	sent := sender.all()
	require.Len(t, sent, 1)
	assert.Equal(t, "T-42 devam ediyor", sent[0].title)
	assert.Equal(t, "Claude kullanım limiti sıfırlandı, görev kaldığı yerden sürüyor.", sent[0].body)
	assert.Equal(t, map[string]string{
		"type":             "task.resumed",
		"task_id":          task.ID.String(),
		"repository_id":    task.RepositoryID.String(),
		"task_key":         "T-42",
		"blocked_resource": domain.ResourceClaudeCodeQuota,
	}, sent[0].data)
}

// All four parks resume through this, distinguished only by blocked_resource —
// and each says what actually ended, because "your task resumed" without a
// reason is a notification the reader cannot act on or dismiss with confidence.
func TestTaskResumedNamesEachResource(t *testing.T) {
	for resource, want := range map[string]string{
		domain.ResourceClaudeCodeQuota: "Claude kullanım limiti sıfırlandı",
		domain.ResourceMobileDevice:    "Beklediği test cihazı boşaldı",
		domain.ResourceDeployWatch:     "Beklediği deploy tamamlandı",
		domain.ResourceWorkOrder:       "Önce bitmesi gereken işler tamamlandı",
	} {
		sender, devices := newRecordingSender(), oneDevice()
		New(devices, sender).TaskResumed(context.Background(), parkedTask(), resource)
		sender.wait(t, 1)

		sent := sender.all()
		require.Len(t, sent, 1)
		assert.Contains(t, sent[0].body, want)
		assert.Contains(t, sent[0].body, "kaldığı yerden sürüyor",
			"every variant has to say the task is already moving without the user")
		assert.Equal(t, resource, sent[0].data["blocked_resource"])
	}
}

// A resource this build does not know still gets a push: the task IS moving
// again, which is the part the user needs. The informational key is omitted
// rather than sent empty when there is no resource at all.
func TestTaskResumedFallsBackForAnUnnamedResource(t *testing.T) {
	sender, devices := newRecordingSender(), oneDevice()

	New(devices, sender).TaskResumed(context.Background(), parkedTask(), "")
	sender.wait(t, 1)

	sent := sender.all()
	require.Len(t, sent, 1)
	assert.Equal(t, "Beklediği kaynak hazır, görev kaldığı yerden sürüyor.", sent[0].body)
	assert.NotContains(t, sent[0].data, "blocked_resource")
	assert.Equal(t, "task.resumed", sent[0].data["type"], "routing still works")
}

// One resume, one notification per device — never the resume push plus the
// generic column push.
func TestTaskResumedSendsExactlyOnePushPerDevice(t *testing.T) {
	sender := newRecordingSender()
	devices := &deviceStore{devices: []domain.PushDevice{
		{ID: "d1", DeviceToken: "tok-1"},
		{ID: "d2", DeviceToken: "tok-2"},
	}}
	task := parkedTask()
	// The column a quota-parked task most often comes back to that TaskMoved
	// would ALSO have notified on. Only the resume push may be sent.
	task.Column = domain.TaskColumnNeedRevision

	New(devices, sender).TaskResumed(context.Background(), task, domain.ResourceClaudeCodeQuota)
	sender.wait(t, 2)

	sent := sender.all()
	require.Len(t, sent, 2, "one per registered device and no more")
	for _, s := range sent {
		assert.Equal(t, "task.resumed", s.data["type"])
	}
	assert.ElementsMatch(t, []string{"tok-1", "tok-2"}, []string{sent[0].token, sent[1].token})
}

// APNs is not in the resume's critical path. The task is already out of
// `blocked` and running by the time this is called, so a dead APNs, an
// unreadable device list or a rejected token costs a notification and nothing
// else — it can neither block the caller nor put the task back.
func TestPushFailuresNeverReachTheResume(t *testing.T) {
	failing := newRecordingSender()
	failing.err = errors.New("apns unreachable")
	task := parkedTask()

	done := make(chan struct{})
	go func() {
		New(oneDevice(), failing).TaskResumed(context.Background(), task, domain.ResourceClaudeCodeQuota)
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("TaskResumed blocked the caller; delivery must happen in the background")
	}
	failing.wait(t, 1)
	assert.Len(t, failing.all(), 1, "the send was attempted and its failure swallowed")

	// An unreadable device list is the same non-event.
	broken := newRecordingSender()
	assert.NotPanics(t, func() {
		New(&deviceStore{err: errors.New("pool closed")}, broken).TaskResumed(context.Background(), task, domain.ResourceMobileDevice)
	})

	// No devices registered at all: nothing to send to, still no failure.
	empty := newRecordingSender()
	assert.NotPanics(t, func() {
		New(&deviceStore{}, empty).TaskResumed(context.Background(), task, domain.ResourceMobileDevice)
	})
}

// A dead token is pruned on the resume push exactly as it is on any other, so
// one uninstalled phone does not accumulate failures forever.
func TestTaskResumedPrunesDeadTokens(t *testing.T) {
	sender := &unregisteringSender{done: make(chan struct{}, 1)}
	devices := oneDevice()

	New(devices, sender).TaskResumed(context.Background(), parkedTask(), domain.ResourceClaudeCodeQuota)
	<-sender.done
	// The prune happens after the send returns, on the same goroutine.
	assert.Eventually(t, func() bool { return len(devices.deletedTokens()) == 1 }, 2*time.Second, 10*time.Millisecond)
	assert.Equal(t, []string{"tok-1"}, devices.deletedTokens())
}

type unregisteringSender struct{ done chan struct{} }

func (s *unregisteringSender) Send(context.Context, string, string, string) (bool, error) {
	return true, nil
}
func (s *unregisteringSender) SendData(context.Context, string, string, string, map[string]string) (bool, error) {
	s.done <- struct{}{}
	return true, nil
}
func (s *unregisteringSender) SendLiveActivity(context.Context, string, string, string, map[string]any, map[string]any, string, string) (bool, error) {
	return false, nil
}

// tenantDeviceStore is the push_devices table as row-level security presents
// it: each tenant sees only its own devices, and a context with no tenant is
// refused rather than defaulted.
type tenantDeviceStore struct {
	mu       sync.Mutex
	byTenant map[uuid.UUID][]domain.PushDevice
	unscoped int
}

func newTenantDeviceStore() *tenantDeviceStore {
	return &tenantDeviceStore{byTenant: map[uuid.UUID][]domain.PushDevice{}}
}

func (d *tenantDeviceStore) Upsert(context.Context, string, string) (domain.PushDevice, error) {
	return domain.PushDevice{}, nil
}
func (d *tenantDeviceStore) Delete(context.Context, string) error { return nil }

func (d *tenantDeviceStore) List(ctx context.Context) ([]domain.PushDevice, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	id, ok := tenant.ID(ctx)
	if !ok {
		d.unscoped++
		return nil, tenant.ErrNoTenant
	}
	return d.byTenant[id], nil
}

func (d *tenantDeviceStore) unscopedReads() int {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.unscoped
}

// A notification actually reaches a device, and reaches the right tenant's.
//
// It did not before: the send is a goroutine, and it detached onto
// context.Background(), so the device list read answered tenant.ErrNoTenant and
// the function returned after logging "push: device list failed". Nobody's
// phone rang, for any tenant, and the only trace was a Warn line nobody was
// watching for.
func TestTaskMovedReachesTheCallingTenantsDevice(t *testing.T) {
	sender := newRecordingSender()
	devices := newTenantDeviceStore()
	a, b := uuid.New(), uuid.New()
	devices.byTenant[a] = []domain.PushDevice{{ID: "da", DeviceToken: "tok-a"}}
	devices.byTenant[b] = []domain.PushDevice{{ID: "db", DeviceToken: "tok-b"}}

	task := parkedTask()
	// need_revision is one of the columns a human is notified about.
	task.Column = domain.TaskColumnNeedRevision

	ctxA := tenant.With(context.Background(), tenant.Identity{TenantID: a, Role: tenant.RoleOwner})
	New(devices, sender).TaskMoved(ctxA, task)
	sender.wait(t, 1)

	sent := sender.all()
	require.Len(t, sent, 1, "the move must produce exactly one push")
	assert.Equal(t, "tok-a", sent[0].token, "the push must go to the calling tenant's device")
	assert.Zero(t, devices.unscopedReads(), "the device list must never be read without a tenant")
}

// And the resume half, which is the one a parked card depends on.
func TestTaskResumedReachesTheCallingTenantsDevice(t *testing.T) {
	sender := newRecordingSender()
	devices := newTenantDeviceStore()
	a, b := uuid.New(), uuid.New()
	devices.byTenant[a] = []domain.PushDevice{{ID: "da", DeviceToken: "tok-a"}}
	devices.byTenant[b] = []domain.PushDevice{{ID: "db", DeviceToken: "tok-b"}}

	ctxB := tenant.With(context.Background(), tenant.Identity{TenantID: b, Role: tenant.RoleOwner})
	New(devices, sender).TaskResumed(ctxB, parkedTask(), domain.ResourceClaudeCodeQuota)
	sender.wait(t, 1)

	sent := sender.all()
	require.Len(t, sent, 1)
	assert.Equal(t, "tok-b", sent[0].token, "tenant B's resume must reach tenant B's device, not tenant A's")
	assert.Zero(t, devices.unscopedReads())
}
