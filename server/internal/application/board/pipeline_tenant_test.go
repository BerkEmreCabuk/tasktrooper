package board

// The pipeline queue severs the triggering request from the worker that drains
// it, exactly as the run queue does — and until the tenant travelled on the job
// the same way RunJob.Tenant does, every board run logged
//
//	pipeline freshness check failed; proceeding  error="get task pipeline: tenant: no tenant in context"
//	mark pipeline running failed  pipeline_id=00000000-0000-0000-0000-000000000000
//	pipeline execution failed
//
// on a shared database, where "no tenant" is a fail-closed refusal rather than
// the harmless nothing it used to be.

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/platform/tenant"
)

// tenantRecordingPipelineStore notes which tenant each store call arrived
// with — the question the worker used to get wrong.
type tenantRecordingPipelineStore struct {
	*fakePipelineStore
	mu   sync.Mutex
	seen []uuid.UUID
	got  chan struct{}
}

func newTenantRecordingPipelineStore() *tenantRecordingPipelineStore {
	return &tenantRecordingPipelineStore{
		fakePipelineStore: newFakePipelineStore(),
		got:               make(chan struct{}, 8),
	}
}

func (t *tenantRecordingPipelineStore) record(ctx context.Context) {
	id, _ := tenant.ID(ctx)
	t.mu.Lock()
	t.seen = append(t.seen, id)
	t.mu.Unlock()
	select {
	case t.got <- struct{}{}:
	default:
	}
}

func (t *tenantRecordingPipelineStore) Get(ctx context.Context, id uuid.UUID) (domain.TaskPipeline, error) {
	t.record(ctx)
	return t.fakePipelineStore.Get(ctx, id)
}

func (t *tenantRecordingPipelineStore) Update(ctx context.Context, p domain.TaskPipeline) (domain.TaskPipeline, error) {
	t.record(ctx)
	return t.fakePipelineStore.Update(ctx, p)
}

func (t *tenantRecordingPipelineStore) tenants() []uuid.UUID {
	t.mu.Lock()
	defer t.mu.Unlock()
	return append([]uuid.UUID(nil), t.seen...)
}

func tenantCtx(id uuid.UUID) context.Context {
	return tenant.With(context.Background(), tenant.Identity{TenantID: id, Role: tenant.RoleMember})
}

func TestTriggerCarriesTheTriggeringTenantOnTheJob(t *testing.T) {
	runner := newRunnerForTrigger(newFakePipelineStore())
	tenantID := uuid.New()

	_, err := runner.Trigger(tenantCtx(tenantID), uuid.New(), domain.BoardTask{ID: uuid.New()}, domain.PipelineTriggerReadyForQA)
	require.NoError(t, err)

	job := <-runner.queue
	assert.Equal(t, tenantID, job.Tenant.TenantID)

	scoped, ok := tenant.ID(job.scope(context.Background()))
	require.True(t, ok, "the job re-attaches the tenant to a worker's bare context")
	assert.Equal(t, tenantID, scoped)
}

// The whole path, driven the way boot drives it: workers started on the process
// context, work handed to them from a tenant-scoped request.
func TestPipelineWorkerExecutesOnTheTriggeringTenant(t *testing.T) {
	store := newTenantRecordingPipelineStore()
	// No git client: execute reaches the freshness check and the running-mark
	// (the two calls that failed) and then finishes the pipeline for want of a
	// workspace, which is all this test needs and all it can have without one.
	runner := NewPipelineRunner(PipelineRunnerDeps{Store: store})
	tenantID := uuid.New()

	_, err := runner.Trigger(tenantCtx(tenantID), uuid.New(), domain.BoardTask{ID: uuid.New()}, domain.PipelineTriggerReadyForQA)
	require.NoError(t, err)

	runner.Start(context.Background())
	defer runner.Stop()

	deadline := time.After(5 * time.Second)
	for len(store.tenants()) < 2 {
		select {
		case <-store.got:
		case <-deadline:
			t.Fatalf("worker made %d store calls in time, want at least 2", len(store.tenants()))
		}
	}
	for i, got := range store.tenants() {
		assert.Equalf(t, tenantID, got, "store call %d ran with no tenant on its context", i)
	}
}

// Self-hosted, the desktop bundle and every existing test: one tenant, already
// on the context or absent altogether. A job carrying none must leave the
// worker's context exactly as it found it rather than attach a nil tenant.
func TestPipelineJobWithNoTenantLeavesTheContextAlone(t *testing.T) {
	runner := newRunnerForTrigger(newFakePipelineStore())

	_, err := runner.Trigger(context.Background(), uuid.New(), domain.BoardTask{ID: uuid.New()}, domain.PipelineTriggerReadyForQA)
	require.NoError(t, err)

	job := <-runner.queue
	assert.Equal(t, uuid.Nil, job.Tenant.TenantID)

	local := tenantCtx(uuid.New())
	assert.Equal(t, local, job.scope(local))
}
