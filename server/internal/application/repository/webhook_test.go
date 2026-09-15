package repository

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

// TestSeenDelivery covers the GitHub-retry replay cache: first sighting is
// fresh, a repeat inside the TTL is a duplicate, and an entry older than the
// TTL is forgotten so the id can legitimately come around again.
func TestSeenDelivery(t *testing.T) {
	s := &Service{}
	now := time.Now()

	if s.seenDelivery(context.Background(), "d1", now) {
		t.Fatal("first sighting of d1 must not be a duplicate")
	}
	if !s.seenDelivery(context.Background(), "d1", now.Add(time.Minute)) {
		t.Fatal("d1 inside the TTL must be a duplicate")
	}
	if s.seenDelivery(context.Background(), "d2", now) {
		t.Fatal("d2 is unrelated to d1")
	}
	// Past the TTL the cache prunes d1, so it reads as fresh again.
	if s.seenDelivery(context.Background(), "d1", now.Add(pushDeliveryTTL+2*time.Minute)) {
		t.Fatal("d1 after the TTL must be forgotten")
	}
	if s.seenDelivery(context.Background(), "", now) || s.seenDelivery(context.Background(), "", now) {
		t.Fatal("an absent delivery id must never register as a duplicate")
	}
}

// TestSchedulePushReindexDebounce walks the debounce ledger through its three
// promises: one pass in flight at a time, pushes during a pass collapse into a
// single rerun, and triggers inside the interval collapse into one timer.
func TestSchedulePushReindexDebounce(t *testing.T) {
	var runs atomic.Int32
	runCh := make(chan uuid.UUID, 16)
	s := &Service{pushReindexInterval: 50 * time.Millisecond}
	s.pushRunFn = func(id uuid.UUID) {
		runs.Add(1)
		runCh <- id
	}
	repoID := uuid.New()

	if got := s.schedulePushReindex(repoID); got != "reindex started" {
		t.Fatalf("first trigger: %q", got)
	}
	waitRun(t, runCh)

	// Pass still running (done not called yet): triggers only queue.
	if got := s.schedulePushReindex(repoID); got != "queued behind the running reindex" {
		t.Fatalf("trigger during run: %q", got)
	}
	if got := s.schedulePushReindex(repoID); got != "queued behind the running reindex" {
		t.Fatalf("second trigger during run: %q", got)
	}

	// Pass ends; the queued pushes must produce exactly one rerun.
	s.pushReindexDone(repoID)
	waitRun(t, runCh)
	s.pushReindexDone(repoID)
	if got := runs.Load(); got != 2 {
		t.Fatalf("queued pushes must collapse into one rerun, got %d runs", got)
	}

	// Inside the interval after the rerun: trigger schedules, repeat is a no-op.
	got := s.schedulePushReindex(repoID)
	if got == "reindex started" {
		t.Fatalf("trigger inside the interval must debounce, got %q", got)
	}
	if got := s.schedulePushReindex(repoID); got != "reindex already scheduled" {
		t.Fatalf("trigger with a timer pending: %q", got)
	}
	waitRun(t, runCh)
	s.pushReindexDone(repoID)
	if got := runs.Load(); got != 3 {
		t.Fatalf("debounced triggers must collapse into one run, got %d runs", got)
	}
}

// TestWebhookTargetURL pins the delivery URL shape: the public base with its
// trailing slash trimmed, plus the webhook path, and no query string.
func TestWebhookTargetURL(t *testing.T) {
	s := &Service{}
	s.SetPublicBaseURL("https://tasktrooper.ai/")
	if got := s.webhookTargetURL(); got != "https://tasktrooper.ai/v1/github/webhook" {
		t.Fatalf("url = %q", got)
	}
}

func waitRun(t *testing.T, ch <-chan uuid.UUID) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for a reindex run")
	}
}

// fakeWebhookRepositoryStore is the in-memory double the happy-path test needs:
// Get reflects whatever SetWebhook last stored, exactly like the real store's
// webhook_hook_id column does.
type fakeWebhookRepositoryStore struct {
	port.RepositoryStore
	repo   domain.Repository
	hookID int64
	secret string
}

func (s *fakeWebhookRepositoryStore) Get(context.Context, uuid.UUID) (domain.Repository, error) {
	repo := s.repo
	repo.WebhookInstalled = s.hookID != 0
	return repo, nil
}

func (s *fakeWebhookRepositoryStore) SetWebhook(_ context.Context, _ uuid.UUID, secret string, hookID int64) error {
	s.hookID = hookID
	s.secret = secret
	return nil
}

// TestSetupWebhookHappyPathInstallsAndPersists is the full round trip QA could
// not reach live (no GitHub-connected sandbox): a repository with no hook yet,
// SetupWebhook installs one on a fake GitHub server and the store ends up with
// webhook_installed=true and the exact secret GitHub was given.
func TestSetupWebhookHappyPathInstallsAndPersists(t *testing.T) {
	var created map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/hooks"):
			json.NewEncoder(w).Encode([]map[string]any{})
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/hooks"):
			json.NewDecoder(r.Body).Decode(&created)
			w.WriteHeader(http.StatusCreated)
			json.NewEncoder(w).Encode(map[string]any{"id": 99})
		default:
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	repoID := uuid.New()
	store := &fakeWebhookRepositoryStore{repo: domain.Repository{ID: repoID, RemoteURL: "https://github.com/o/r"}}
	s := &Service{
		repos:         store,
		githubAPIBase: srv.URL,
		githubToken:   func(context.Context) (string, error) { return "tok", nil },
	}
	s.SetPublicBaseURL("https://bridge.example")

	got, err := s.SetupWebhook(context.Background(), repoID)
	if err != nil {
		t.Fatalf("SetupWebhook: %v", err)
	}
	if !got.WebhookInstalled {
		t.Fatal("webhook_installed must be true after a successful install")
	}
	if store.hookID != 99 {
		t.Fatalf("stored hook id = %d, want 99", store.hookID)
	}
	if store.secret == "" {
		t.Fatal("SetWebhook must be called with the generated secret")
	}
	cfg, _ := created["config"].(map[string]any)
	if cfg["secret"] != store.secret {
		t.Fatalf("GitHub was sent secret %q, store has %q", cfg["secret"], store.secret)
	}
}

// TestSetupWebhookFailsWithoutGitHubConnected is the other half of the same
// bug: no token configured must fail with the specific, actionable message —
// not the cipher error the original bug produced.
func TestSetupWebhookFailsWithoutGitHubConnected(t *testing.T) {
	repoID := uuid.New()
	store := &fakeWebhookRepositoryStore{repo: domain.Repository{ID: repoID, RemoteURL: "https://github.com/o/r"}}
	s := &Service{repos: store}
	s.SetPublicBaseURL("https://bridge.example")

	_, err := s.SetupWebhook(context.Background(), repoID)
	if err == nil || !strings.Contains(err.Error(), "GitHub is not connected") {
		t.Fatalf("err = %v, want a GitHub-not-connected message", err)
	}
}

// sharedDeliveryLedger is one github_webhook_deliveries table, seen by however
// many Services are pointed at it. MarkSeen is the whole mechanism and is
// modelled the way the real statement behaves: the INSERT decides, so exactly
// one caller is told it was first, however many arrive at once.
type sharedDeliveryLedger struct {
	mu   sync.Mutex
	seen map[string]bool
	fail error
}

func newSharedDeliveryLedger() *sharedDeliveryLedger {
	return &sharedDeliveryLedger{seen: map[string]bool{}}
}

func (l *sharedDeliveryLedger) MarkSeen(_ context.Context, id string, _ time.Duration) (bool, error) {
	if l.fail != nil {
		return false, l.fail
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.seen[id] {
		return false, nil
	}
	l.seen[id] = true
	return true, nil
}

// GitHub retries a delivery it did not get a 2xx for, and it retries to
// whichever pod the load balancer picks. The dedupe was a map on this Service,
// so the retry found an empty map on the second replica and the whole pass ran
// again: a second reindex over the same push, a second resolve of the same
// workflow run.
//
// Two Services, one ledger, both handed the same delivery at once: exactly one
// of them may treat it as new.
func TestDeliveryDedupeHoldsAcrossReplicas(t *testing.T) {
	ledger := newSharedDeliveryLedger()
	a := &Service{}
	b := &Service{}
	a.SetDeliveryLedger(ledger)
	b.SetDeliveryLedger(ledger)

	const delivery = "9f1c0f8e-0000-4000-8000-000000000001"
	var fresh atomic.Int32
	start := make(chan struct{})
	var wg sync.WaitGroup
	for _, svc := range []*Service{a, b} {
		wg.Add(1)
		go func(s *Service) {
			defer wg.Done()
			<-start
			if !s.seenDelivery(context.Background(), delivery, time.Now()) {
				fresh.Add(1)
			}
		}(svc)
	}
	close(start)
	wg.Wait()

	if got := fresh.Load(); got != 1 {
		t.Fatalf("%d replicas treated one delivery as new, want 1", got)
	}
}

// The ledger is the authority, but it must not be a single point of failure.
// A dropped delivery is a card that never moves; a duplicated one costs a
// reindex. So an unreachable database falls back to this process's own memory
// rather than refusing to handle the delivery at all.
func TestDeliveryDedupeFallsBackWhenTheLedgerIsDown(t *testing.T) {
	ledger := newSharedDeliveryLedger()
	ledger.fail = errors.New("connection refused")
	s := &Service{}
	s.SetDeliveryLedger(ledger)

	now := time.Now()
	if s.seenDelivery(context.Background(), "d9", now) {
		t.Fatal("a delivery must still be handled when the ledger is unreachable")
	}
	if !s.seenDelivery(context.Background(), "d9", now.Add(time.Minute)) {
		t.Fatal("and this process's own memory must still dedupe its own retries")
	}
}

func waitFor(t *testing.T, n *atomic.Int32) {
	t.Helper()
	for i := 0; i < 100; i++ {
		if n.Load() > 0 {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("the reindex pass never started")
}
