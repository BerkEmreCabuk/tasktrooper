package job

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/platform/urlguard"
)

// fakeJobStore records what Create was asked to store; nothing else here needs
// a real store.
type fakeJobStore struct {
	created     int
	callbackURL string
}

func (f *fakeJobStore) Create(_ context.Context, _ []byte, callbackURL string) (domain.Job, error) {
	f.created++
	f.callbackURL = callbackURL
	return domain.Job{ID: uuid.New(), CallbackURL: callbackURL}, nil
}
func (f *fakeJobStore) Get(context.Context, uuid.UUID) (domain.Job, error) {
	return domain.Job{}, nil
}
func (f *fakeJobStore) List(context.Context, string, int) ([]domain.Job, error) { return nil, nil }
func (f *fakeJobStore) UpdateStatus(context.Context, uuid.UUID, domain.JobStatus, []byte, string) error {
	return nil
}
func (f *fakeJobStore) Delete(context.Context, uuid.UUID) error           { return nil }
func (f *fakeJobStore) ClaimPending(context.Context) (*domain.Job, error) { return nil, nil }

func newTestService(store *fakeJobStore) *Service {
	return NewService(store, nil, nil, 1, time.Minute, domain.ToolPolicy{})
}

func loopbackPolicy() urlguard.Policy {
	p := urlguard.PublicOnly()
	p.AllowLoopback = true
	return p
}

// ─── F7: callback_url is an attacker-chosen destination ───────────────────────

// TestCreateRefusesInternalCallbackURLs is the finding at the front door:
// callback_url came off the request body and went straight to an outbound POST
// with no scheme check at all, and loopback — the interesting case, because it
// reaches this pod's own unauthenticated endpoints — is exactly what the cluster
// NetworkPolicy cannot see.
func TestCreateRefusesInternalCallbackURLs(t *testing.T) {
	for _, callback := range []string{
		"http://127.0.0.1:8080/admin/api-keys",
		"http://[::1]:8080/admin/api-keys",
		"http://169.254.169.254/computeMetadata/v1/",
		"http://10.4.0.9/",
		"http://192.168.1.1/",
		"http://100.64.0.1/",
		"file:///etc/passwd",
		"gopher://example.test/",
		"not-a-url-at-all",
	} {
		store := &fakeJobStore{}
		svc := newTestService(store)

		_, err := svc.Create(context.Background(), domain.JobRequest{CallbackURL: callback}, domain.ToolPolicy{})
		if err == nil {
			t.Fatalf("%s was accepted as a callback_url", callback)
		}
		if !strings.Contains(err.Error(), "callback_url") {
			t.Fatalf("%s: unhelpful error %v", callback, err)
		}
		if store.created != 0 {
			t.Fatalf("%s: the job was queued anyway", callback)
		}
	}
}

func TestCreateAcceptsPublicAndEmptyCallbackURLs(t *testing.T) {
	for _, callback := range []string{
		"",
		"https://hooks.example.com/jobs",
		"http://93.184.216.34/hook",
	} {
		store := &fakeJobStore{}
		svc := newTestService(store)

		if _, err := svc.Create(context.Background(), domain.JobRequest{CallbackURL: callback}, domain.ToolPolicy{}); err != nil {
			t.Fatalf("%q must still be accepted: %v", callback, err)
		}
		if store.created != 1 {
			t.Fatalf("%q was not queued", callback)
		}
	}
}

// TestFireCallbackRefusesInternalDestinations covers the rows a write-time check
// cannot: anything already stored, and anything whose name has changed answer
// since. The destination is resolved and checked again where the POST is made.
func TestFireCallbackRefusesInternalDestinations(t *testing.T) {
	hit := make(chan struct{}, 1)
	internal := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hit <- struct{}{}
		w.WriteHeader(http.StatusOK)
	}))
	defer internal.Close()

	svc := newTestService(&fakeJobStore{}) // production default: loopback off
	svc.fireCallback(context.Background(), internal.URL+"/admin", uuid.New(), domain.JobStatusCompleted, []byte(`{"a":1}`), "")

	select {
	case <-hit:
		t.Fatal("the callback reached this pod's own listener")
	case <-time.After(300 * time.Millisecond):
	}
}

// TestFireCallbackDoesNotFollowRedirects: a callback is a delivery, and the body
// carries the job result. Following a 302 would post the model's output wherever
// the callback host points next.
func TestFireCallbackDoesNotFollowRedirects(t *testing.T) {
	elsewhere := make(chan struct{}, 1)
	second := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		elsewhere <- struct{}{}
		w.WriteHeader(http.StatusOK)
	}))
	defer second.Close()

	first := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, second.URL+"/steal", http.StatusFound)
	}))
	defer first.Close()

	svc := newTestService(&fakeJobStore{})
	svc.SetURLPolicy(loopbackPolicy())
	svc.fireCallback(context.Background(), first.URL+"/hook", uuid.New(), domain.JobStatusCompleted, []byte(`{"secret":"model output"}`), "")

	select {
	case <-elsewhere:
		t.Fatal("the job result was posted to the redirect target")
	case <-time.After(300 * time.Millisecond):
	}
}

// A legitimate callback still gets its POST, with the body intact.
func TestFireCallbackStillDelivers(t *testing.T) {
	got := make(chan string, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		buf := make([]byte, 512)
		n, _ := r.Body.Read(buf)
		got <- string(buf[:n])
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	svc := newTestService(&fakeJobStore{})
	svc.SetURLPolicy(loopbackPolicy())
	id := uuid.New()
	svc.fireCallback(context.Background(), srv.URL+"/hook", id, domain.JobStatusCompleted, []byte(`{"a":1}`), "")

	select {
	case body := <-got:
		if !strings.Contains(body, id.String()) {
			t.Fatalf("callback body did not carry the job id: %s", body)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("a legitimate callback was never delivered")
	}
}
